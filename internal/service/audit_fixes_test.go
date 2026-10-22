package service

import (
	"context"
	"fmt"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

func TestLostPVCGeneratesCriticalIssue(t *testing.T) {
	o := model.Object{"kind": "PersistentVolumeClaim", "metadata": map[string]any{"name": "data", "namespace": "production", "uid": "claim"}, "status": map[string]any{"phase": "Lost"}}
	s := New(auditFixtureProvider{Provider: provider.NewDemo(), objects: []model.Object{o}})
	issues, err := s.Issues(context.Background(), "production")
	if err != nil {
		t.Fatal(err)
	}
	items := issues.Items
	if len(items) != 1 || items[0].Severity != "critical" {
		t.Fatalf("lost claim omitted: %+v", items)
	}
	overview, err := s.Overview(context.Background(), "production")
	if err != nil || overview.Health != "critical" {
		t.Fatalf("overview: %v %v", overview, err)
	}
}

func TestTopologyFiltersBeforeNodeLimit(t *testing.T) {
	objects := []model.Object{}
	for i := range 205 {
		objects = append(objects, model.Object{"kind": "ConfigMap", "metadata": map[string]any{"name": fmt.Sprintf("cfg-%03d", i), "namespace": "production"}})
	}
	objects = append(objects, model.Object{"kind": "Deployment", "metadata": map[string]any{"name": "app", "namespace": "production"}})
	s := New(auditFixtureProvider{Provider: provider.NewDemo(), objects: objects})
	graph, err := s.TopologyWithGroup(context.Background(), Query{Namespace: "production"}, "workloads")
	if err != nil || len(graph.Nodes) != 1 || graph.Nodes[0].Kind != "Deployment" || graph.Truncated {
		t.Fatalf("workloads hidden: %+v %v", graph, err)
	}
	graph, err = s.TopologyWithGroup(context.Background(), Query{Namespace: "production"}, "configuration")
	if err != nil || len(graph.Nodes) != 200 || !graph.Truncated {
		t.Fatal("configuration limit changed", err)
	}
	if _, err = s.TopologyWithGroup(context.Background(), Query{}, "invalid"); provider.Status(err) != 400 {
		t.Fatal("invalid group accepted")
	}
}

type driftLimitProvider struct {
	provider.Provider
	desired, live model.Object
}

type driftWithMissingTarget struct{ driftLimitProvider }

func (p driftWithMissingTarget) DesiredManifests(ctx context.Context, kind, ns, name string) (model.DesiredBundle, error) {
	bundle, err := p.driftLimitProvider.DesiredManifests(ctx, kind, ns, name)
	bundle.Manifests = append(bundle.Manifests, model.Object{"kind": "ConfigMap", "apiVersion": "v1", "metadata": map[string]any{"name": "unobserved-audit-target", "namespace": "production"}})
	return bundle, err
}

func (p driftLimitProvider) DesiredManifests(ctx context.Context, kind, ns, name string) (model.DesiredBundle, error) {
	o, err := p.Provider.Get(ctx, kind, ns, name)
	return model.DesiredBundle{ControllerUID: model.Str(o, "metadata", "uid"), Generation: int64(model.Num(o, "metadata", "generation")), Revision: "target", Manifests: []model.Object{p.desired}}, err
}
func (p driftLimitProvider) Get(ctx context.Context, kind, ns, name string) (model.Object, error) {
	if kind == "Service" {
		return p.live, nil
	}
	return p.Provider.Get(ctx, kind, ns, name)
}
func TestDriftReportsInnerDifferenceTruncation(t *testing.T) {
	for _, count := range []int{999, 1000, 1100} {
		a, b := map[string]any{}, map[string]any{}
		for i := range count {
			key := fmt.Sprintf("label-%04d", i)
			a[key] = "before"
			b[key] = "after"
		}
		object := func(labels map[string]any) model.Object {
			return model.Object{"kind": "Service", "apiVersion": "v1", "metadata": map[string]any{"name": "app", "namespace": "production", "labels": labels}}
		}
		r, err := New(driftLimitProvider{provider.NewDemo(), object(a), object(b)}).GitOpsDrift(context.Background(), "Application", "platform", "checkout-api")
		if err != nil || len(r.Changes) != min(count, 1000) || r.ChangesTruncated != (count > 1000) || (len(r.Warnings) > 0) != (count > 1000) {
			t.Fatalf("count %d: differences=%d truncated=%v warnings=%d error=%v", count, len(r.Changes), r.ChangesTruncated, len(r.Warnings), err)
		}
		if count == 1000 {
			mixed, err := New(driftWithMissingTarget{driftLimitProvider{provider.NewDemo(), object(a), object(b)}}).GitOpsDrift(context.Background(), "Application", "platform", "checkout-api")
			if err != nil || len(mixed.Changes) != 1000 || !mixed.ChangesTruncated || len(mixed.Warnings) == 0 || mixed.Checked != 2 {
				t.Fatalf("missing target clipping not reported: checked=%d differences=%d truncated=%v error=%v", mixed.Checked, len(mixed.Changes), mixed.ChangesTruncated, err)
			}
		}
	}
}

func TestRecentChangeIndexRespectsUIDWindowAndLimit(t *testing.T) {
	now := time.Now().UTC()
	o := newObservations()
	for i := range 25 {
		o.changes = append(o.changes, Change{ID: fmt.Sprint(i), Resource: model.Resource{ID: "Pod:ns:app"}, UID: "current", Type: "HealthChanged", ObservedAt: now.Add(time.Duration(i-25) * time.Second)})
	}
	for _, c := range []Change{{UID: "old", Type: "HealthChanged", ObservedAt: now}, {UID: "current", Type: "Observed", ObservedAt: now}, {UID: "current", Type: "HealthChanged", ObservedAt: now.Add(time.Second)}, {UID: "current", Type: "HealthChanged", ObservedAt: now.Add(-16 * time.Minute)}} {
		c.Resource.ID = "Pod:ns:app"
		o.changes = append(o.changes, c)
	}
	got := o.relatedIndex(now)[changeIdentity{"Pod:ns:app", "current"}]
	if len(got) != 20 || got[0].ID != "24" || got[19].ID != "5" {
		t.Fatalf("incorrect recent history: %+v", got)
	}
}

func BenchmarkDiagnosticsWithRetainedHistory(b *testing.B) {
	now := time.Now().UTC()
	snap := model.Snapshot{ObservedAt: now}
	s := New(provider.NewDemo())
	for i := range 5000 {
		name := fmt.Sprintf("app-%04d", i)
		o := model.Object{"kind": "Pod", "metadata": map[string]any{"name": name, "namespace": "bench", "uid": name}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app"}}}, "status": map[string]any{"phase": "Running", "containerStatuses": []any{map[string]any{"name": "app", "state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}}}}}}
		snap.Resources = append(snap.Resources, model.Summarize(o))
	}
	for i := range 10000 {
		s.observations.changes = append(s.observations.changes, Change{Resource: model.Resource{ID: fmt.Sprintf("Pod:bench:other-%04d", i)}, UID: "other", Type: "HealthChanged", ObservedAt: now})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		s.detectIssues(snap)
	}
}
