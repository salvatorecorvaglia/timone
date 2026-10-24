package service

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

// Opt in because this exercises thousands of resources across concurrent callers.
// Run under /usr/bin/time -l to record peak RSS as well as the Go heap samples.
func TestConcurrentInvestigationLoad(t *testing.T) {
	if os.Getenv("TIMONE_LOAD_TEST") != "1" {
		t.Skip("set TIMONE_LOAD_TEST=1 for concurrent scale acceptance")
	}
	objects := make([]model.Object, 0, 5000)
	for i := 0; i < 2500; i++ {
		name := fmt.Sprintf("app-%04d", i)
		objects = append(objects, model.Object{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": name, "namespace": "production", "uid": name}, "spec": map[string]any{"replicas": float64(1)}, "status": map[string]any{"availableReplicas": float64(1)}})
		objects = append(objects, model.Object{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": name, "namespace": "production", "uid": "pod-" + name, "ownerReferences": []any{map[string]any{"kind": "Deployment", "name": name, "controller": true}}}, "status": map[string]any{"phase": "Running"}})
	}
	s := New(auditFixtureProvider{Provider: provider.NewDemo(), objects: objects})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	maintained := make(chan struct{})
	go func() { defer close(maintained); s.Maintain(ctx) }()
	defer func() { cancel(); <-maintained }()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Go(func() {
			for i := 0; i < 10; i++ {
				page, err := s.Applications(ctx, Query{Namespace: "production", Limit: 20, Offset: (i % 5) * 20})
				if err != nil {
					t.Error(err)
					return
				}
				if page.Total != 2500 || len(page.Items) != 20 {
					t.Errorf("unexpected page total=%d count=%d", page.Total, len(page.Items))
					return
				}
			}
		})
	}
	wg.Wait()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(s)
	// Bound allocation churn and retained growth, not wall time on shared CI hosts.
	// Baseline: about 27 MiB/investigation and 4 MiB retained growth for this fixture.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 80*(40<<20) {
		t.Errorf("investigation allocation budget exceeded: %d bytes across 80 calls", allocated)
	}
	if after.HeapAlloc > before.HeapAlloc+(32<<20) {
		t.Errorf("retained heap growth exceeded 32 MiB: before=%d after=%d", before.HeapAlloc, after.HeapAlloc)
	}
	t.Logf("5000 resources, 8 workers, 80 investigations, elapsed=%s, heap before=%d after=%d, allocated=%d", time.Since(start), before.HeapAlloc, after.HeapAlloc, after.TotalAlloc-before.TotalAlloc)
}
