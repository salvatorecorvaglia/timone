package cluster_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"timone/internal/provider"
	"timone/internal/service"
)

// A byte-forwarding TCP relay preserves the real API's TLS and authorization.
// It never inspects or emulates Kubernetes responses.
type faultProxy struct {
	listener    net.Listener
	upstream    string
	mu          sync.Mutex
	blocked     bool
	connections map[net.Conn]bool
	workers     sync.WaitGroup
}

func newFaultProxy(t *testing.T, upstream string) *faultProxy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("cannot bind acceptance fault relay")
	}
	p := &faultProxy{listener: l, upstream: upstream, connections: map[net.Conn]bool{}}
	p.workers.Add(1)
	go func() {
		defer p.workers.Done()
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			p.mu.Lock()
			if p.blocked {
				p.mu.Unlock()
				_ = c.Close()
				continue
			}
			p.connections[c] = true
			p.workers.Add(1)
			p.mu.Unlock()
			go p.forward(c)
		}
	}()
	t.Cleanup(func() { _ = l.Close(); p.disconnect(true); p.workers.Wait() })
	return p
}
func (p *faultProxy) address() string { return p.listener.Addr().String() }
func (p *faultProxy) disconnect(block bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.blocked = block
	for c := range p.connections {
		_ = c.Close()
	}
}
func (p *faultProxy) forward(c net.Conn) {
	defer p.workers.Done()
	defer func() { _ = c.Close(); p.mu.Lock(); delete(p.connections, c); p.mu.Unlock() }()
	u, err := net.Dial("tcp", p.upstream)
	if err != nil {
		return
	}
	defer u.Close()
	p.mu.Lock()
	if p.blocked {
		p.mu.Unlock()
		return
	}
	p.connections[u] = true
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.connections, u); p.mu.Unlock() }()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(u, c); _ = u.Close(); close(done) }()
	_, _ = io.Copy(c, u)
	_ = c.Close()
	<-done
}
func writeToken(t *testing.T, path, token string) {
	t.Helper()
	if err := os.WriteFile(path+".next", []byte(token), 0600); err != nil {
		t.Fatal("write private projected token")
	}
	if err := os.Rename(path+".next", path); err != nil {
		t.Fatal("rotate projected token atomically")
	}
}
func watching(p provider.Provider, ctx context.Context, ns string) bool {
	for _, stream := range p.Connection(ctx).Streams {
		if stream.Kind == "Pod" && stream.Namespace == ns && stream.State == "watching" {
			return true
		}
	}
	return false
}
func exportCoverage(t *testing.T, ctx context.Context, svc *service.Service) {
	t.Helper()
	dir := os.Getenv("TIMONE_ACCEPTANCE_ARTIFACTS")
	if dir == "" {
		return
	}
	overview, err := svc.Overview(ctx, "timone-acceptance")
	if err != nil {
		t.Fatal("collect actual partial overview")
	}
	warnings := overview.Warnings
	if len(warnings) == 0 {
		t.Fatal("restricted real overview must report incomplete coverage")
	}
	data, err := json.Marshal(overview)
	if err != nil {
		t.Fatal("encode safe coverage fixture")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "live-overview.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
