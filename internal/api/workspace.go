package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"k8s.io/client-go/tools/clientcmd"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"sync"
	"time"
	"timone/internal/auth"
	"timone/internal/config"
	"timone/internal/provider"
	"timone/internal/service"
)

type workspaceState struct {
	service  *service.Service
	handler  http.Handler
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	revision uint64
}
type WorkspaceFactory func(config.Config) (*service.Service, error)
type Runtime struct {
	epoch     int64
	closed    bool
	mu        sync.RWMutex
	switching sync.Mutex
	state     *workspaceState
	retired   []*workspaceState
	config    config.Config
	factory   WorkspaceFactory
	assets    fs.FS
	broker    *Broker
	log       *slog.Logger
	authn     *auth.Manager
	ctx       context.Context
}

func NewRuntime(ctx context.Context, s *service.Service, c config.Config, assets fs.FS, b *Broker, log *slog.Logger, factory WorkspaceFactory) *Runtime {
	rt := &Runtime{epoch: time.Now().UnixNano(), config: c, factory: factory, assets: assets, broker: b, log: log, ctx: ctx}
	rt.state = rt.makeState(s, 1)
	return rt
}
func (rt *Runtime) makeState(s *service.Service, rev uint64) *workspaceState {
	ctx, cancel := context.WithCancel(rt.ctx)
	state := &workspaceState{service: s, ctx: ctx, cancel: cancel, done: make(chan struct{}), revision: rev}
	state.handler = newWithRuntime(s, rt.config, rt.assets, rt.broker, rt.log, rt, rt.authn)
	go func() {
		defer close(state.done)
		maintenanceDone := make(chan struct{})
		go func() { defer close(maintenanceDone); s.Maintain(ctx) }()
		defer func() { <-maintenanceDone }()
		if p, ok := s.Provider.(provider.ScopedWatcher); ok {
			p.WatchScoped(ctx, rt.broker.NotifyScope)
		} else {
			s.Provider.Watch(ctx, rt.broker.Notify)
		}
	}()
	return state
}
func (rt *Runtime) Close() {
	rt.switching.Lock()
	defer rt.switching.Unlock()
	rt.mu.RLock()
	s := rt.state
	rt.mu.RUnlock()
	rt.closed = true
	s.cancel()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for _, state := range append(rt.retired, s) {
		state.cancel()
		select {
		case <-state.done:
		case <-deadline.C:
			rt.log.Warn("Workspace shutdown exceeded watcher grace")
			return
		}
	}
	clear(rt.retired)
	rt.retired = nil
}
func (rt *Runtime) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt.mu.RLock()
	s := rt.state
	rt.mu.RUnlock()
	w.Header().Set("X-Timone-Workspace", fmt.Sprintf("%d:%d", rt.epoch, s.revision))
	if r.URL.Path != "/api/v1/workspace" {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		stop := context.AfterFunc(s.ctx, cancel)
		defer stop()
		if s.ctx.Err() != nil {
			cancel()
		}
		r = r.WithContext(ctx)
	}
	s.handler.ServeHTTP(w, r)
}
func (rt *Runtime) enabled() bool {
	host, _, _ := net.SplitHostPort(rt.config.Listen)
	ip := net.ParseIP(host)
	return rt.config.Mode == "local" && rt.config.AuthMode != "proxy" && ip != nil && ip.IsLoopback() && rt.factory != nil
}
func (rt *Runtime) contexts() ([]string, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if rt.config.Kubeconfig != "" {
		rules.Precedence = filepath.SplitList(rt.config.Kubeconfig)
		rules.ExplicitPath = ""
	}
	raw, err := rules.Load()
	if err != nil {
		return nil, &provider.Error{Code: 503, Message: "Cannot read kubeconfig contexts"}
	}
	names := []string{}
	for name := range raw.Contexts {
		if len(name) <= 253 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > 200 {
		return nil, &provider.Error{Code: 413, Message: "Kubeconfig exceeds 200 contexts"}
	}
	return names, nil
}
func (a *Server) workspaceInfo(w http.ResponseWriter, r *http.Request) {
	if a.workspace == nil {
		write(w, 200, workspaceResponse{Enabled: false, Context: a.service.Provider.Name(), Namespace: a.service.Provider.Namespace(), Revision: 1, Contexts: []string{}, Message: "Runtime switching requires local loopback operator mode."})
		return
	}
	rt := a.workspace
	rt.mu.RLock()
	state := rt.state
	rt.mu.RUnlock()
	w.Header().Set("X-Timone-Workspace", fmt.Sprintf("%d:%d", rt.epoch, state.revision))
	names := []string{}
	var err error
	if rt.enabled() {
		names, err = rt.contexts()
		if err != nil {
			respond(w, nil, err)
			return
		}
	}
	message := "Switching replaces the local workspace for all connected tabs, cancels old requests/watchers and clears observed history. No kubeconfig file is modified."
	if !rt.enabled() {
		message = "Runtime switching requires local loopback operator mode. Demo and shared deployments keep a fixed workspace."
	}
	write(w, 200, workspaceResponse{Enabled: rt.enabled(), Context: state.service.Provider.Name(), Namespace: state.service.Provider.Namespace(), Revision: state.revision, Contexts: names, Message: message})
}
func (a *Server) switchWorkspace(w http.ResponseWriter, r *http.Request) {
	rt := a.workspace
	if rt == nil || !rt.enabled() {
		fail(w, 403, "Context switching requires local loopback operator mode")
		return
	}
	var input workspaceSwitchRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4097))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF || !input.Confirm || input.Context == "" || input.Namespace == nil {
		fail(w, 400, "Select a context, namespace ceiling and current revision, and confirm the workspace reset")
		return
	}
	q := service.Query{Namespace: *input.Namespace}
	if err := q.Validate(); err != nil {
		respond(w, nil, err)
		return
	}
	rt.switching.Lock()
	defer rt.switching.Unlock()
	if rt.closed {
		fail(w, 503, "Workspace is stopping")
		return
	}
	rt.mu.RLock()
	old := rt.state
	rt.mu.RUnlock()
	if input.Revision != old.revision {
		fail(w, 409, "Workspace changed; refresh before switching")
		return
	}
	names, err := rt.contexts()
	if err != nil {
		respond(w, nil, err)
		return
	}
	found := false
	for _, name := range names {
		found = found || name == input.Context
	}
	if !found {
		fail(w, 400, "Context is not in the configured kubeconfig")
		return
	}
	// Bound unfinished retirements without invalidating the current workspace.
	retired := rt.retired[:0]
	for _, state := range rt.retired {
		select {
		case <-state.done:
		default:
			retired = append(retired, state)
		}
	}
	clear(rt.retired[len(retired):])
	rt.retired = retired
	if len(retired) >= 2 {
		fail(w, 503, "Previous workspace watchers are still stopping; current workspace remains available")
		return
	}
	c := rt.config
	c.Context = input.Context
	c.Namespace = *input.Namespace
	c.NamespaceExplicit = true
	next, err := rt.factory(c)
	if err != nil {
		fail(w, 503, "Cannot initialize selected context; current workspace is unchanged")
		return
	}
	if r.Context().Err() != nil {
		return
	}
	// Publish a usable replacement atomically. A slow retirement must never
	// leave the published workspace canceled or report a failed transaction.
	state := rt.makeState(next, old.revision+1)
	rt.mu.Lock()
	rt.state = state
	rt.mu.Unlock()
	old.cancel()
	rt.retired = append(rt.retired, old)
	go func() {
		<-old.done
		rt.switching.Lock()
		defer rt.switching.Unlock()
		for i, state := range rt.retired {
			if state == old {
				copy(rt.retired[i:], rt.retired[i+1:])
				rt.retired[len(rt.retired)-1] = nil
				rt.retired = rt.retired[:len(rt.retired)-1]
				break
			}
		}
	}()
	rt.log.Info("Workspace switched", "revision", state.revision)
	rt.broker.Notify()
	a.workspaceInfo(w, r)
}
