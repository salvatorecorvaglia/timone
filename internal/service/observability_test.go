package service

import (
	"context"
	"math"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

var observed = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func resourceFixture(kind, name string, age time.Duration, spec, status map[string]any) model.Resource {
	return model.Summarize(model.Object{"kind": kind, "metadata": map[string]any{"name": name, "namespace": "production", "uid": "uid-" + name, "creationTimestamp": observed.Add(-age).Format(time.RFC3339)}, "spec": spec, "status": status})
}
func condition(typ, status, reason string, age time.Duration) map[string]any {
	return map[string]any{"type": typ, "status": status, "reason": reason, "lastTransitionTime": observed.Add(-age).Format(time.RFC3339)}
}
func TestDiagnosticGraceAndConditionEvidence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		r     model.Resource
		want  int
		title string
	}{
		{"normal startup", resourceFixture("Pod", "new", time.Minute, nil, map[string]any{"phase": "Pending"}), 0, ""},
		{"stuck startup", resourceFixture("Pod", "old", 10*time.Minute, nil, map[string]any{"phase": "Pending"}), 1, "Pod waiting to start"},
		{"condition overrides generic pending", resourceFixture("Pod", "scheduler", 10*time.Minute, nil, map[string]any{"phase": "Pending", "conditions": []any{condition("PodScheduled", "False", "Unschedulable", 8*time.Minute)}}), 1, "Pod cannot be scheduled"},
		{"readiness grace", resourceFixture("Pod", "ready", time.Hour, nil, map[string]any{"phase": "Running", "conditions": []any{condition("Ready", "False", "ContainersNotReady", time.Minute)}}), 0, ""},
		{"sustained readiness", resourceFixture("Pod", "ready", time.Hour, nil, map[string]any{"phase": "Running", "conditions": []any{condition("Ready", "False", "ContainersNotReady", 8*time.Minute)}}), 1, "Pod readiness condition failing"},
		{"deadline", resourceFixture("Deployment", "rollout", time.Minute, map[string]any{"replicas": 3}, map[string]any{"conditions": []any{condition("Progressing", "False", "ProgressDeadlineExceeded", time.Minute)}}), 1, "Deployment exceeded progress deadline"},
		{"node pressure", resourceFixture("Node", "worker", time.Hour, nil, map[string]any{"conditions": []any{condition("Ready", "True", "", time.Hour), condition("DiskPressure", "True", "", time.Minute)}}), 1, "Node reports DiskPressure"},
		{"job retry", resourceFixture("Job", "retry", time.Hour, nil, map[string]any{"failed": 1, "active": 1}), 0, ""},
		{"job failed", resourceFixture("Job", "failed", time.Hour, nil, map[string]any{"conditions": []any{condition("Failed", "True", "BackoffLimitExceeded", time.Minute)}}), 1, "Job failure reported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectIssues(model.Snapshot{Resources: []model.Resource{tc.r}, ObservedAt: observed})
			if len(got) != tc.want || (tc.want > 0 && got[0].Title != tc.title) {
				t.Fatalf("got %+v", got)
			}
		})
	}
}
func TestWarningWindowsAndResourceReplacement(t *testing.T) {
	r := resourceFixture("Pod", "ready", time.Hour, nil, map[string]any{"phase": "Running"})
	for _, tc := range []struct {
		name, uid string
		age       time.Duration
		want      int
	}{
		{"recent probe", "uid-ready", time.Minute, 1}, {"old warning", "uid-ready", time.Hour, 0}, {"replaced resource", "deleted-uid", time.Minute, 0}, {"name-only predates creation", "", 2 * time.Hour, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := model.Event{ID: tc.name, ResourceID: r.ID, ResourceUID: tc.uid, Type: "Warning", Reason: "Unhealthy", Message: "Readiness probe failed", Timestamp: observed.Add(-tc.age).Format(time.RFC3339)}
			issues := DetectIssues(model.Snapshot{Resources: []model.Resource{r}, Events: []model.Event{ev}, ObservedAt: observed})
			if len(issues) != tc.want {
				t.Fatalf("got %+v", issues)
			}
		})
	}
}
func TestDiagnosticOwnerMustBeControllerAndMatchUID(t *testing.T) {
	parent := resourceFixture("Deployment", "owner", time.Hour, map[string]any{"replicas": 0}, nil)
	child := resourceFixture("Pod", "child", time.Hour, nil, map[string]any{"phase": "Failed"})
	for _, tc := range []struct {
		controller bool
		uid        string
		grouped    bool
	}{{false, "uid-owner", false}, {true, "stale-owner", false}, {true, "uid-owner", true}} {
		model.Map(child.Object, "metadata")["ownerReferences"] = []any{map[string]any{"kind": "Deployment", "name": "owner", "uid": tc.uid, "controller": tc.controller}}
		issues := DetectIssues(model.Snapshot{Resources: []model.Resource{parent, child}, ObservedAt: observed})
		if len(issues) != 1 || (issues[0].Resource.ID == parent.ID) != tc.grouped {
			t.Fatalf("incorrect grouping %+v", issues)
		}
	}
}

type observedProvider struct {
	provider.Provider
	objects                   map[string][]model.Object
	events                    []model.Event
	deniedPods, truncatedPods bool
}

func (p observedProvider) List(ctx context.Context, kind, ns string) ([]model.Object, bool, error) {
	if kind == "Pod" && p.deniedPods {
		return nil, false, &provider.Error{Code: 403, Message: "Pod read denied"}
	}
	if objects, ok := p.objects[kind]; ok {
		return objects, kind == "Pod" && p.truncatedPods, nil
	}
	return []model.Object{}, false, nil
}
func (p observedProvider) Events(context.Context, string) ([]model.Event, error) {
	return p.events, nil
}
func TestEventFiltersAreExactBoundedAndChronological(t *testing.T) {
	evs := []model.Event{
		{ID: "b", Kind: "Pod", Namespace: "production", Resource: "api", Type: "Warning", Timestamp: observed.Format(time.RFC3339)},
		{ID: "a", Kind: "Pod", Namespace: "production", Resource: "api", Type: "Warning", Timestamp: observed.Format(time.RFC3339)},
		{ID: "prefix", Kind: "Pod", Resource: "api-other", Type: "Warning", Timestamp: observed.Format(time.RFC3339)},
		{ID: "kind", Kind: "Deployment", Resource: "api", Type: "Warning", Timestamp: observed.Format(time.RFC3339)},
		{ID: "old", Kind: "Pod", Resource: "api", Type: "Warning", Timestamp: observed.Add(-time.Hour).Format(time.RFC3339)},
	}
	s := New(observedProvider{Provider: provider.NewDemo(), events: evs})
	got, err := s.Events(context.Background(), Query{Kind: "Pod", Resource: "api", Status: "warning", Since: observed.Add(-time.Minute).Format(time.RFC3339), Until: observed.Format(time.RFC3339), Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	items := got.Items
	if got.Total != 2 || len(items) != 1 || items[0].ID != "a" {
		t.Fatalf("incorrect filter/page %+v", got)
	}
	for _, q := range []Query{{Resource: "../escape"}, {Since: "yesterday"}, {Since: observed.Format(time.RFC3339), Until: observed.Add(-time.Hour).Format(time.RFC3339)}} {
		if err := q.Validate(); err == nil {
			t.Fatalf("accepted %+v", q)
		}
	}
}
func requested(cpu string, sidecar bool) map[string]any {
	c := map[string]any{"name": "container", "resources": map[string]any{"requests": map[string]any{"cpu": cpu}}}
	if sidecar {
		c["restartPolicy"] = "Always"
	}
	return c
}
func TestConfiguredDemandAccountsForInitSidecarsOverheadAndPodBudget(t *testing.T) {
	p := model.Object{"spec": map[string]any{"containers": []any{requested("500m", false), requested("250m", false)}, "initContainers": []any{requested("200m", true), requested("2", false), requested("100m", true)}, "overhead": map[string]any{"cpu": "50m"}}}
	if got := configuredRequest(p, "cpu"); got == nil || math.Abs(*got-2.25) > 1e-9 {
		t.Fatalf("init peak incorrect %v", got)
	}
	model.Map(p, "spec")["resources"] = map[string]any{"requests": map[string]any{"cpu": "3"}}
	if got := configuredRequest(p, "cpu"); got == nil || math.Abs(*got-3.05) > 1e-9 {
		t.Fatalf("Pod budget incorrect %v", got)
	}
	model.Map(p, "spec", "resources", "requests")["cpu"] = "invalid"
	if configuredRequest(p, "cpu") != nil {
		t.Fatal("invalid budget became zero")
	}
}
func TestMetricBudgetsAndAllocationCoverage(t *testing.T) {
	s := New(provider.NewDemo())
	m, err := s.Metrics(context.Background(), "production")
	if err != nil {
		t.Fatal(err)
	}
	found, unscheduled := false, false
	for _, item := range m.Items {
		if item.Kind == "Pod" && item.Container == "checkout-api" {
			found = true
			if item.CPURequest == nil || *item.CPURequest != .1 || item.MemoryLimit == nil || *item.MemoryLimit != 512*(1<<20) {
				t.Fatalf("missing budgets %+v", item)
			}
		}
	}
	for _, a := range m.Allocations {
		if a.Scope != "namespace production only" {
			t.Fatalf("scope misrepresented %+v", a)
		}
		if a.Node == "" {
			unscheduled = true
			if a.Pods == nil || *a.Pods != 1 || a.CPURequest == nil || *a.CPURequest != .1 {
				t.Fatalf("unbound demand %+v", a)
			}
		}
	}
	if !found || !unscheduled {
		t.Fatal("missing consumption/boundary data")
	}
}
func TestIncompletePodReadDoesNotBecomeZeroRequests(t *testing.T) {
	for _, truncated := range []bool{false, true} {
		p := observedProvider{Provider: provider.NewDemo(), deniedPods: !truncated, truncatedPods: truncated, objects: map[string][]model.Object{"Node": {{"metadata": map[string]any{"name": "node"}}}, "Pod": {}}}
		m, err := New(p).Metrics(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Allocations) != 1 || m.Allocations[0].CPURequest != nil || m.Allocations[0].Pods != nil || len(m.Warnings) == 0 {
			t.Fatalf("incomplete data became zero %+v", m)
		}
	}
}
func TestGatewayEdgesRemainDeclaredReferences(t *testing.T) {
	s := New(provider.NewDemo())
	g, err := s.Topology(context.Background(), Query{Namespace: "production"})
	if err != nil {
		t.Fatal(err)
	}
	parent, backend := false, false
	for _, e := range g.Edges {
		if e.Source == "Gateway:production:public-gateway" && e.Target == "HTTPRoute:production:storefront-route" && e.Relation == "parent reference" {
			parent = true
		}
		if e.Source == "HTTPRoute:production:storefront-route" && e.Target == "Service:production:storefront" && e.Relation == "backend reference" {
			backend = true
		}
	}
	if !parent || !backend {
		t.Fatalf("Gateway relationships missing %+v", g.Edges)
	}
}

func TestStaleConditionsDoNotEstablishCurrentRolloutFailure(t *testing.T) {
	r := resourceFixture("Deployment", "rollout", time.Minute, map[string]any{"replicas": 1}, map[string]any{"readyReplicas": 1, "observedGeneration": 1, "conditions": []any{condition("Progressing", "False", "ProgressDeadlineExceeded", time.Minute)}})
	model.Map(r.Object, "metadata")["generation"] = 2
	issues := DetectIssues(model.Snapshot{Resources: []model.Resource{r}, ObservedAt: observed})
	if len(issues) != 0 {
		t.Fatalf("stale condition reported as current failure %+v", issues)
	}
}
func TestGatewayReferencesRequireReadableEndpointsAndSupportedKinds(t *testing.T) {
	route := resourceFixture("HTTPRoute", "route", time.Hour, map[string]any{"parentRefs": []any{map[string]any{"name": "missing"}}, "rules": []any{map[string]any{"backendRefs": []any{map[string]any{"name": "service"}, map[string]any{"name": "service", "group": "other.example"}, map[string]any{"name": "service", "kind": "Secret"}, map[string]any{"name": "missing"}}}}}, nil)
	target := resourceFixture("Service", "service", time.Hour, nil, nil)
	p := observedProvider{Provider: provider.NewDemo(), objects: map[string][]model.Object{"HTTPRoute": {route.Object}, "Service": {target.Object}}}
	g, err := New(p).Topology(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != 1 || g.Edges[0].Relation != "backend reference" || g.Edges[0].Target != target.ID {
		t.Fatalf("invented reference %+v", g.Edges)
	}
}

func TestEventOnlyIssueKeepsItsObservedTimestamp(t *testing.T) {
	r := resourceFixture("Pod", "healthy", time.Hour, nil, map[string]any{"phase": "Running"})
	ev := model.Event{ID: "warning", ResourceID: r.ID, ResourceUID: "uid-healthy", Type: "Warning", Reason: "Unhealthy", Timestamp: observed.Add(-3 * time.Minute).Format(time.RFC3339)}
	issues := DetectIssues(model.Snapshot{Resources: []model.Resource{r}, Events: []model.Event{ev}, ObservedAt: observed})
	if len(issues) != 1 || issues[0].LastObserved != ev.Timestamp {
		t.Fatalf("event age misrepresented %+v", issues)
	}
}
func TestDetailRejectsEventsForReplacedResource(t *testing.T) {
	const id = "Deployment:production:checkout-api"
	p := observedProvider{Provider: provider.NewDemo(), events: []model.Event{{ID: "old", ResourceID: id, ResourceUID: "deleted-uid"}, {ID: "current", ResourceID: id, ResourceUID: id}}}
	detail, err := New(p).Detail(context.Background(), "Deployment", "production", "checkout-api")
	if err != nil {
		t.Fatal(err)
	}
	ev := detail.Events
	if len(ev) != 1 || ev[0].ID != "current" {
		t.Fatalf("detail attached stale event %+v", ev)
	}
}
