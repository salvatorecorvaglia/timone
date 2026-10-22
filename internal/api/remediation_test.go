package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
	"timone/internal/config"
	"timone/internal/model"
	"timone/internal/provider"
	"timone/internal/service"
)

type namespaceProvider struct{ provider.Provider }

func (p namespaceProvider) List(ctx context.Context, kind, ns string) ([]model.Object, bool, error) {
	if kind != "Namespace" {
		return p.Provider.List(ctx, kind, ns)
	}
	out := []model.Object{}
	for i := range 201 {
		out = append(out, model.Object{"kind": "Namespace", "metadata": map[string]any{"name": fmt.Sprintf("ns-%03d", i)}})
	}
	return out, false, nil
}
func TestNamespaceEndpointSupportsAllPages(t *testing.T) {
	h := New(service.New(namespaceProvider{provider.NewDemo()}), config.Config{Mode: "demo", Listen: "127.0.0.1:8080"}, nil, NewBroker())
	for _, tc := range []struct {
		path          string
		count, offset int
	}{{"/api/v1/namespaces", 200, 0}, {"/api/v1/namespaces?limit=200&offset=200", 1, 200}} {
		w := perform(h, "GET", tc.path, "", "")
		var p model.Page
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || len(p.Items) != tc.count || p.Total != 201 || p.Offset != tc.offset {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := perform(h, "GET", "/api/v1/namespaces?limit=201", "", ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

type slowStopProvider struct {
	provider.Provider
	release <-chan struct{}
}

func (p slowStopProvider) Name() string                        { return "one" }
func (p slowStopProvider) Watch(ctx context.Context, _ func()) { <-ctx.Done(); <-p.release }
func TestSlowRetirementCannotCancelPublishedWorkspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	_ = os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\ncontexts:\n- name: one\n  context: {}\n- name: two\n  context: {}\ncurrent-context: one\n"), 0600)
	release := make(chan struct{})
	rt := NewRuntime(t.Context(), service.New(slowStopProvider{provider.NewDemo(), release}), config.Config{Mode: "local", Listen: "127.0.0.1:8080", Kubeconfig: path}, nil, NewBroker(), slog.New(slog.NewTextHandler(io.Discard, nil)), func(config.Config) (*service.Service, error) { return service.New(provider.NewDemo()), nil })
	defer func() { close(release); rt.Close() }()
	start := time.Now()
	w := perform(rt, "POST", "/api/v1/workspace", `{"context":"two","namespace":"","revision":1,"confirm":true}`, "")
	if w.Code != 200 || rt.state.revision != 2 || rt.state.ctx.Err() != nil || time.Since(start) > time.Second {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = perform(rt, "GET", "/api/v1/resources?kind=Pod", "", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestLoginLimitsSeparatePeersAndRejectForwardingSpoof(t *testing.T) {
	var limits loginLimiter
	now := time.Now()
	request := func(peer, forwarded string) *http.Request {
		r := httptest.NewRequest("POST", "http://localhost/api/v1/session", nil)
		r.RemoteAddr = peer
		r.Header.Set("X-Forwarded-For", forwarded)
		return r
	}
	for range 20 {
		if !limits.allow(request("192.0.2.1:1", ""), nil, now) {
			t.Fatal("early throttle")
		}
	}
	if limits.allow(request("192.0.2.1:2", "198.51.100.2"), nil, now) {
		t.Fatal("untrusted forwarding bypass")
	}
	if !limits.allow(request("192.0.2.2:1", ""), nil, now) {
		t.Fatal("one client locked out another")
	}
	if !limits.allow(request("192.0.2.1:1", ""), nil, now.Add(time.Minute)) {
		t.Fatal("expired throttle retained")
	}
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}
	r := request("10.0.0.1:80", "203.0.113.9, 192.0.2.5, 10.0.0.2")
	if got := loginPeer(r, trusted); got.String() != "192.0.2.5" {
		t.Fatal("leftmost spoof selected", got)
	}
	for i := 0; i < 1500; i++ {
		limits.allow(request(fmt.Sprintf("198.18.%d.%d:1", i/256, i%256), ""), nil, now)
	}
	if len(limits.peers) > 1024 {
		t.Fatal("unbounded peer map")
	}
}
func TestScopedBrokerRetainsCoalescedEventsAndFallsBack(t *testing.T) {
	b := NewBroker()
	ch, stop, _ := b.Subscribe()
	defer stop()
	b.NotifyScope(model.ResourceChange{Kind: "Pod", Namespace: "production"})
	b.NotifyScope(model.ResourceChange{Kind: "Node"})
	<-ch
	v := b.Since(0)
	if v.Full || len(v.Scopes) != 2 || v.Revision != 2 {
		t.Fatal(v)
	}
	b.Notify()
	if !b.Since(2).Full {
		t.Fatal("global change lost")
	}
	for range 300 {
		b.NotifyScope(model.ResourceChange{Kind: "Pod"})
	}
	if !b.Since(0).Full {
		t.Fatal("overflow lost changes")
	}
	if len(b.changes) > 256 {
		t.Fatal("unbounded journal")
	}
}
func TestAssetCachingAndShellRevalidation(t *testing.T) {
	files := fstest.MapFS{"index.html": {Data: []byte("<html>Timone</html>")}, "assets/index-abc123.js": {Data: []byte("console.log('ok')")}}
	h := New(service.New(provider.NewDemo()), config.Config{Mode: "demo", Listen: "127.0.0.1:8080"}, files, NewBroker())
	w := perform(h, "GET", "/assets/index-abc123.js", "", "")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") || w.Header().Get("ETag") == "" {
		t.Fatal(w.Code, w.Header())
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:8080/assets/index-abc123.js", nil)
	r.Header.Set("If-None-Match", w.Header().Get("ETag"))
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	if out.Code != 304 {
		t.Fatal(out.Code)
	}
	if w = perform(h, "GET", "/resources", "", ""); w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Header())
	}
}
func TestAdmissionBoundsHTTPRequests(t *testing.T) {
	s := service.New(provider.NewDemo())
	var releases []func()
	for range 8 {
		release, ok := s.TryAdmit()
		if !ok {
			t.Fatal("early admission rejection")
		}
		releases = append(releases, release)
	}
	h := New(s, config.Config{Mode: "demo", Listen: "127.0.0.1:8080"}, nil, NewBroker())
	w := perform(h, "GET", "/api/v1/resources?kind=Pod", "", "")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal(w.Code)
	}
	for _, release := range releases {
		release()
	}
	if w = perform(h, "GET", "/api/v1/resources?kind=Pod", "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
}

// Deliberately bypass provider-side projection to verify the public service boundary.
type unprojectedProvider struct{ provider.Provider }

func (p unprojectedProvider) Get(_ context.Context, kind, ns, name string) (model.Object, error) {
	return model.Object{"apiVersion": "argoproj.io/v1alpha1", "kind": kind, "metadata": map[string]any{"name": name, "namespace": ns, "annotations": map[string]any{"token": "private-marker"}}, "spec": map[string]any{"source": map[string]any{"targetRevision": "main", "helm": map[string]any{"values": "private-marker"}}}}, nil
}
func TestHTTPDetailsProjectGitOpsCredentials(t *testing.T) {
	h := New(service.New(unprojectedProvider{provider.NewDemo()}), config.Config{Mode: "demo", Listen: "127.0.0.1:8080"}, nil, NewBroker())
	w := perform(h, "GET", "/api/v1/resources/Application/production/app", "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-marker") || !strings.Contains(w.Body.String(), "main") {
		t.Fatal(w.Code, w.Body.String())
	}
}
