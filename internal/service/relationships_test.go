package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"timone/internal/model"
	"timone/internal/provider"
)

func denseRelationships(n int) []model.Object {
	objects := make([]model.Object, 0, n*2)
	for i := 0; i < n; i++ {
		objects = append(objects, model.Object{"kind": "Service", "metadata": map[string]any{"name": fmt.Sprintf("svc-%04d", i), "namespace": "production"}, "spec": map[string]any{"selector": map[string]any{"app": "shared"}}})
		objects = append(objects, model.Object{"kind": "Pod", "metadata": map[string]any{"name": fmt.Sprintf("pod-%04d", i), "namespace": "production", "labels": map[string]any{"app": "shared", "app.kubernetes.io/part-of": "shared"}}})
	}
	return objects
}
func relationshipSnapshot(objects []model.Object) model.Snapshot {
	snap := model.Snapshot{}
	for _, o := range objects {
		snap.Resources = append(snap.Resources, model.Summarize(o))
	}
	return snap
}
func TestDenseTopologyBoundsAndCancellation(t *testing.T) {
	s := New(auditFixtureProvider{Provider: provider.NewDemo(), objects: denseRelationships(90)})
	graph, err := s.Topology(context.Background(), Query{Namespace: "production"})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 180 || len(graph.Edges) != 2000 || !graph.Truncated {
		t.Fatalf("unbounded graph: nodes=%d edges=%d truncated=%v", len(graph.Nodes), len(graph.Edges), graph.Truncated)
	}
	snap := relationshipSnapshot(denseRelationships(10))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	count := 0
	err = visitDeclaredEdges(ctx, snap, resourceIndex(snap.Resources), func(model.Edge) bool { count++; cancel(); return true })
	if !errors.Is(err, context.Canceled) || count != 1 {
		t.Fatalf("cancellation ignored: count=%d err=%v", count, err)
	}
}
func TestDenseApplicationsBoundMembershipAndRelationships(t *testing.T) {
	s := New(auditFixtureProvider{Provider: provider.NewDemo(), objects: denseRelationships(300)})
	page, err := s.Applications(context.Background(), Query{Namespace: "production"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("application groups=%d", len(page.Items))
	}
	app := page.Items[0]
	if len(app.Resources) > 500 || len(app.Relationships) > 1000 || !app.Truncated {
		t.Fatalf("unbounded application: resources=%d edges=%d truncated=%v", len(app.Resources), len(app.Relationships), app.Truncated)
	}
}
func TestConcurrentDenseTopology(t *testing.T) {
	s := New(auditFixtureProvider{Provider: provider.NewDemo(), objects: denseRelationships(5000)})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			graph, err := s.Topology(context.Background(), Query{Namespace: "production"})
			if err != nil || len(graph.Nodes) > 200 || len(graph.Edges) > 2000 {
				t.Errorf("invalid concurrent graph: %v", err)
			}
		})
	}
	wg.Wait()
}
func BenchmarkBoundedDenseTopology(b *testing.B) {
	snap := relationshipSnapshot(denseRelationships(250))
	all := resourceIndex(snap.Resources)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		c := newEdgeCollector(2000, nil)
		if err := visitDeclaredEdges(context.Background(), snap, all, c.add); err != nil || len(c.edges) != 2000 || !c.truncated {
			b.Fatal("relationship budget not enforced", err)
		}
	}
}
func BenchmarkDenseApplications(b *testing.B) {
	s := New(auditFixtureProvider{Provider: provider.NewDemo(), objects: denseRelationships(250)})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.Applications(context.Background(), Query{Namespace: "production"}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestApplicationsReportRelationshipWorkLimit(t *testing.T) {
	s := New(auditFixtureProvider{Provider: provider.NewDemo(), objects: denseRelationships(600)})
	page, err := s.Applications(context.Background(), Query{Namespace: "production"})
	if err != nil {
		t.Fatal(err)
	}
	limited := false
	for _, w := range page.Warnings {
		if w.Kind == "Application" {
			limited = true
		}
	}
	if !limited || len(page.Items) != 1 || !page.Items[0].Truncated {
		t.Fatal("relationship work limit did not report partial application coverage")
	}
}
