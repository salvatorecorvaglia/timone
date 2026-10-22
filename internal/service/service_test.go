package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"timone/internal/model"
	"timone/internal/provider"
)

func TestDemoIssuesGroupReplicasAndCorrelateEvents(t *testing.T) {
	s := New(provider.NewDemo())
	snap, e := s.Snapshot(context.Background(), "")
	if e != nil {
		t.Fatal(e)
	}
	issues := DetectIssues(snap)
	if len(issues) != 3 {
		t.Fatalf("expected three workload issues, got %d", len(issues))
	}
	var checkout *model.Issue
	for i := range issues {
		if issues[i].Resource.Name == "checkout-api" {
			checkout = &issues[i]
		}
	}
	if checkout == nil || checkout.Resource.Kind != "Deployment" || checkout.Severity != "critical" || len(checkout.Events) < 2 || len(checkout.Evidence) < 3 {
		t.Fatalf("incorrect issue: %+v", checkout)
	}
	for _, c := range checkout.Causes {
		if strings.Contains(c, "connection limit reached") {
			t.Fatal("log data was not observed; root cause must not be asserted")
		}
	}
}
func TestListFiltersSortAndPagination(t *testing.T) {
	s := New(provider.NewDemo())
	page, e := s.List(context.Background(), Query{Kind: "Pod", Namespace: "production", Search: "checkout", Status: "critical", Limit: 1, Sort: "name", Desc: true})
	if e != nil {
		t.Fatal(e)
	}
	if page.Total != 2 || len(page.Items) != 1 || !strings.Contains(page.Items[0].Name, "r8t3v") {
		t.Fatalf("bad page: %+v", page)
	}
	page, e = s.List(context.Background(), Query{Kind: "Pod", Namespace: "production", Phase: "Running", Offset: 5000})
	if e != nil || len(page.Items) != 0 {
		t.Fatalf("out of range pagination: %+v %v", page, e)
	}
}
func TestQueryRejectsUnboundedAndInvalidInputs(t *testing.T) {
	for _, q := range []Query{{Kind: "../secrets"}, {Namespace: "other/ns"}, {Namespace: "a.b"}, {Limit: 201}, {Offset: -1}, {Search: strings.Repeat("a", 201)}, {Sort: "arbitrary"}, {Status: "fake"}} {
		if e := q.Validate(); e == nil {
			t.Fatalf("accepted %+v", q)
		}
	}
	for _, p := range [][3]string{{"Pod", "", "name"}, {"Node", "default", "name"}, {"Pod", "default", "../bad"}, {"Pod", "default", "name?x"}} {
		if e := ValidateResource(p[0], p[1], p[2]); e == nil {
			t.Fatalf("accepted %v", p)
		}
	}
}
func TestTopologyOnlyUsesDeclaredRelationships(t *testing.T) {
	s := New(provider.NewDemo())
	g, e := s.Topology(context.Background(), Query{Namespace: "production"})
	if e != nil {
		t.Fatal(e)
	}
	nodes := map[string]bool{}
	for _, n := range g.Nodes {
		nodes[n.ID] = true
	}
	selected := 0
	for _, edge := range g.Edges {
		if !nodes[edge.Source] || !nodes[edge.Target] || edge.Confidence != "declared" {
			t.Fatalf("invalid edge %+v", edge)
		}
		if edge.Source == model.ID("Service", "production", "checkout-api") && edge.Relation == "selects" {
			selected++
		}
		if edge.Source == model.ID("Service", "production", "payments") && strings.Contains(edge.Target, "checkout") {
			t.Fatal("fabricated service dependency")
		}
	}
	if selected != 3 {
		t.Fatalf("selector matched %d pods", selected)
	}
}

type denied struct{ provider.Provider }

func (d denied) List(ctx context.Context, kind, ns string) ([]model.Object, bool, error) {
	if kind == "Node" {
		return nil, false, &provider.Error{Code: 403, Message: "Kubernetes RBAC denied this read"}
	}
	return d.Provider.List(ctx, kind, ns)
}
func TestPartialPermissionFailureVisible(t *testing.T) {
	s := New(denied{provider.NewDemo()})
	o, e := s.Overview(context.Background(), "")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(o)
	if !strings.Contains(string(b), `"kind":"Node"`) || !strings.Contains(string(b), "RBAC denied") {
		t.Fatalf("missing warning: %s", b)
	}
}

type scoped struct{ provider.Provider }

func (scoped) Namespace() string { return "production" }
func TestNamespaceScopeAlsoAppliesToMCPServices(t *testing.T) {
	s := New(scoped{provider.NewDemo()})
	_, e := s.List(context.Background(), Query{Kind: "Pod", Namespace: "staging"})
	if provider.Status(e) != 403 {
		t.Fatalf("namespace escape: %v", e)
	}
	_, e = s.Logs(context.Background(), "staging", "catalog-preview", "", 10)
	if provider.Status(e) != 403 {
		t.Fatalf("log namespace escape: %v", e)
	}
}
func TestAuditReportsCoverageAndMutableTags(t *testing.T) {
	a, e := New(provider.NewDemo()).Audit(context.Background(), "")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(a)
	if !strings.Contains(string(b), "mutable-image") || !strings.Contains(string(b), "unavailable") {
		t.Fatalf("missing audit evidence: %s", b)
	}
}
func TestSecretDetailAlwaysSanitized(t *testing.T) {
	s := New(provider.NewDemo())
	d, e := s.Detail(context.Background(), "Secret", "production", "checkout-db")
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := d.Manifest["data"]; ok {
		t.Fatal("secret data exposed")
	}
}

func TestNamespaceSnapshotPreservesClusterScopedDataAndAuditScope(t *testing.T) {
	s := New(provider.NewDemo())
	snap, e := s.Snapshot(context.Background(), "production")
	if e != nil {
		t.Fatal(e)
	}
	nodes, namespaces := 0, 0
	for _, r := range snap.Resources {
		if r.Kind == "Node" {
			nodes++
		}
		if r.Kind == "Namespace" {
			namespaces++
			if r.Name != "production" {
				t.Fatal("unselected namespace in scoped snapshot")
			}
		}
	}
	if nodes != 3 || namespaces != 1 {
		t.Fatalf("scoped counts nodes=%d namespaces=%d", nodes, namespaces)
	}
	a, e := s.Audit(context.Background(), "production")
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range a.Items {
		if f.Rule == "network-policy-presence" && f.Resource.Name != "production" {
			t.Fatalf("policy absence asserted outside scope: %+v", f)
		}
	}
}
func TestRBACNamesSupportSystemRolesWithoutPathEscape(t *testing.T) {
	if e := ValidateResource("ClusterRole", "", "system:controller:job-controller"); e != nil {
		t.Fatal(e)
	}
	if e := ValidateResource("Role", "default", "../escape"); e == nil {
		t.Fatal("accepted invalid path")
	}
}
