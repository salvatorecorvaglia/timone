package service

import (
	"context"
	"testing"
	"timone/internal/model"
	"timone/internal/provider"
)

func TestAdvancedLogSelectionValidation(t *testing.T) {
	s := New(provider.NewDemo())
	for _, tc := range []struct {
		ns, container, since string
		tail                 int
	}{
		{"production", "", "invalid", 100}, {"production", "", "2999-01-01T00:00:00Z", 100}, {"production", "../escape", "", 100}, {"production", "", "", 1001},
	} {
		if _, err := s.LogsWithOptions(context.Background(), tc.ns, "pod", tc.container, tc.tail, true, tc.since); err == nil {
			t.Fatalf("accepted invalid selection %+v", tc)
		}
	}
}
func TestOperationalSignalsDistinguishConstraints(t *testing.T) {
	quota := model.Resource{Kind: "ResourceQuota", Object: model.Object{"status": map[string]any{"hard": map[string]any{"pods": "10"}, "used": map[string]any{"pods": "10"}}}}
	if len(operationalSignals(quota)) != 1 {
		t.Fatal("exhausted quota not reported")
	}
	pdb := model.Resource{Kind: "PodDisruptionBudget", Object: model.Object{"status": map[string]any{"disruptionsAllowed": 0}}}
	if len(operationalSignals(pdb)) != 0 {
		t.Fatal("PDB constraint treated as workload failure")
	}
	hpa := model.Resource{Kind: "HorizontalPodAutoscaler", Object: model.Object{"status": map[string]any{"conditions": []any{map[string]any{"type": "ScalingActive", "status": "False", "reason": "FailedGetResourceMetric"}}}}}
	if len(operationalSignals(hpa)) != 1 {
		t.Fatal("missing autoscaler diagnostic")
	}
}
func TestOperationalRelatedResources(t *testing.T) {
	s := New(provider.NewDemo())
	related, warnings := s.OperationalContext(context.Background(), "Deployment", "production", "checkout-api")
	if len(related) < 3 || len(warnings) != 0 {
		t.Fatalf("related=%+v warnings=%+v", related, warnings)
	}
	for _, r := range related {
		if r.Namespace != "production" {
			t.Fatal("cross namespace related resource")
		}
	}
}
func TestEndpointSliceDeclaredEdges(t *testing.T) {
	objects := []model.Object{
		{"kind": "Service", "metadata": map[string]any{"name": "web", "namespace": "test"}},
		{"kind": "EndpointSlice", "metadata": map[string]any{"name": "web-1", "namespace": "test", "labels": map[string]any{"kubernetes.io/service-name": "web"}}, "endpoints": []any{map[string]any{"targetRef": map[string]any{"kind": "Pod", "name": "web-1", "uid": "new"}}}},
		{"kind": "Pod", "metadata": map[string]any{"name": "web-1", "namespace": "test", "uid": "old"}},
	}
	snap := model.Snapshot{}
	for _, o := range objects {
		snap.Resources = append(snap.Resources, model.Summarize(o))
	}
	edges := []model.Edge{}
	err := visitDeclaredEdges(context.Background(), snap, resourceIndex(snap.Resources), func(e model.Edge) bool { edges = append(edges, e); return true })
	if err != nil || len(edges) != 1 || edges[0].Relation != "publishes endpoints" {
		t.Fatalf("edges=%+v error=%v", edges, err)
	}
}

type logSelectionProvider struct {
	provider.Provider
	previous bool
	since    string
	tail     int
}

func (p *logSelectionProvider) LogsWithOptions(_ context.Context, _, _, _ string, tail int, previous bool, since string) (string, error) {
	p.tail, p.previous, p.since = tail, previous, since
	return "previous output", nil
}
func TestAdvancedLogsForwardValidatedOptions(t *testing.T) {
	p := &logSelectionProvider{Provider: provider.NewDemo()}
	s := New(p)
	got, err := s.LogsWithOptions(context.Background(), "production", "web", "app", 500, true, "2020-01-01T00:00:00Z")
	if err != nil || got != "previous output" || !p.previous || p.tail != 500 || p.since != "2020-01-01T00:00:00Z" {
		t.Fatalf("selection=%+v err=%v", p, err)
	}
}
