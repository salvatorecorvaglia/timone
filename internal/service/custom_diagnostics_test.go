package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

func diagnosticCustomObject(kind, ns, name string) model.Object {
	return model.Object{"apiVersion": "ops.example/v1", "kind": kind, "metadata": map[string]any{"name": name, "namespace": ns, "uid": "uid-" + name, "generation": 2, "creationTimestamp": observed.Add(-time.Hour).Format(time.RFC3339)}, "spec": map[string]any{"password": "OMIT-THIS"}, "status": map[string]any{"password": "OMIT-THIS", "conditions": []any{map[string]any{"type": "Ready", "status": "False", "observedGeneration": 2, "lastTransitionTime": observed.Add(-10 * time.Minute).Format(time.RFC3339), "reason": "ReconcileFailed", "message": "Controller reports unavailable dependency"}}}}
}

func TestCustomDiagnosticConditionsRequireFreshUniqueTimedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(model.Object)
		want   int
	}{
		{"current", func(model.Object) {}, 1},
		{"stale", func(o model.Object) {
			model.Arr(o, "status", "conditions")[0].(map[string]any)["observedGeneration"] = 1
		}, 0},
		{"future generation", func(o model.Object) {
			model.Arr(o, "status", "conditions")[0].(map[string]any)["observedGeneration"] = 3
		}, 0},
		{"missing generation", func(o model.Object) { delete(model.Map(o, "metadata"), "generation") }, 0},
		{"unknown", func(o model.Object) { model.Arr(o, "status", "conditions")[0].(map[string]any)["status"] = "Unknown" }, 0},
		{"ready", func(o model.Object) { model.Arr(o, "status", "conditions")[0].(map[string]any)["status"] = "True" }, 0},
		{"duplicate", func(o model.Object) {
			model.Map(o, "status")["conditions"] = append(model.Arr(o, "status", "conditions"), model.Arr(o, "status", "conditions")[0])
		}, 0},
		{"recent", func(o model.Object) {
			model.Arr(o, "status", "conditions")[0].(map[string]any)["lastTransitionTime"] = observed.Add(-299 * time.Second).Format(time.RFC3339)
		}, 0},
		{"boundary", func(o model.Object) {
			model.Arr(o, "status", "conditions")[0].(map[string]any)["lastTransitionTime"] = observed.Add(-300 * time.Second).Format(time.RFC3339)
		}, 1},
		{"future transition", func(o model.Object) {
			model.Arr(o, "status", "conditions")[0].(map[string]any)["lastTransitionTime"] = observed.Add(time.Second).Format(time.RFC3339)
		}, 0},
		{"missing transition", func(o model.Object) {
			delete(model.Arr(o, "status", "conditions")[0].(map[string]any), "lastTransitionTime")
		}, 0},
		{"fallback generation", func(o model.Object) {
			delete(model.Arr(o, "status", "conditions")[0].(map[string]any), "observedGeneration")
			model.Map(o, "status")["observedGeneration"] = 2
		}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := diagnosticCustomObject("Pod", "production", "db")
			tc.mutate(o)
			r := model.SummarizeCustom(model.CustomProjection(o), "pods.ops.example")
			// A custom Kind named Pod cannot invoke crash/restart built-in rules.
			r.Status, r.Restarts = "CrashLoopBackOff", 999
			issues := detectIssues(model.Snapshot{Resources: []model.Resource{r}, ObservedAt: observed}, model.DefaultDiagnosticPolicy())
			if len(issues) != tc.want {
				t.Fatal(issues)
			}
			if len(issues) > 0 && (issues[0].Title != "Custom resource reports not ready" || issues[0].Severity != "warning") {
				t.Fatal(issues)
			}
		})
	}
}

func TestCustomDiagnosticsDemoConsumersAndSafeIdentity(t *testing.T) {
	s := New(provider.NewDemo())
	ctx := context.Background()
	list, err := s.Issues(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	overview, err := s.Overview(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 4 || len(overview.Issues) != 4 {
		t.Fatal(list, overview)
	}
	coverage := list.CustomDiagnostics
	if coverage.State != "available" || coverage.TypesInspected != 2 || coverage.ResourcesObserved != 4 || coverage.ReadinessUnknown != 2 {
		t.Fatal(coverage)
	}
	if _, ok := overview.Counts["Database"]; ok {
		t.Fatal("custom resource polluted built-in counts")
	}
	issue, err := s.Issue(ctx, "production", "issue-databases.platform.timone.io:production:orders-db")
	if err != nil || issue.Resource.Definition != "databases.platform.timone.io" || issue.Resource.Version != "v1" || len(issue.Events) != 1 || issue.Events[0].CustomDefinition != issue.Resource.Definition {
		t.Fatal(issue, err)
	}
	b, _ := json.Marshal(list)
	if strings.Contains(string(b), "SIMULATED-OMITTED-CUSTOM-SECRET") {
		t.Fatal("custom payload escaped projection")
	}
	if !strings.Contains(strings.Join(issue.Evidence, " "), "Ready=False at generation 2") {
		t.Fatal(issue)
	}
}

type diagnosticCustomProvider struct {
	provider.Provider
	catalog    model.CustomCatalog
	catalogErr error
	list       func(context.Context, string, string) ([]model.Object, model.CustomType, bool, error)
}

func (p diagnosticCustomProvider) CustomTypes(ctx context.Context) (model.CustomCatalog, error) {
	if ctx.Err() != nil {
		return p.catalog, ctx.Err()
	}
	return p.catalog, p.catalogErr
}
func (p diagnosticCustomProvider) CustomList(ctx context.Context, d, v, ns string) ([]model.Object, model.CustomType, bool, error) {
	return p.list(ctx, d, ns)
}
func (p diagnosticCustomProvider) CustomType(context.Context, string, string) (model.CustomType, string, error) {
	panic("unexpected per-object metadata read")
}
func (p diagnosticCustomProvider) CustomGet(context.Context, string, string, string, string) (model.Object, model.CustomType, error) {
	panic("unexpected per-object read")
}
func diagnosticType() model.CustomType {
	return model.CustomType{Definition: "databases.ops.example", Kind: "Database", Group: "ops.example", Plural: "databases", Namespaced: true, Versions: []string{"v1"}, PreferredVersion: "v1"}
}
func diagnosticSnapshot() model.Snapshot {
	return model.Snapshot{Resources: []model.Resource{}, Events: []model.Event{}, Warnings: []model.Warning{}, ObservedAt: observed}
}

func TestCustomDiagnosticCoverageDeniedTruncatedDisabledAndScope(t *testing.T) {
	typ := diagnosticType()
	for _, mode := range []string{"available", "denied", "truncated", "catalog denied", "disabled", "invalid objects", "event denied", "duplicate objects", "collection identity", "kind mismatch", "version mismatch"} {
		t.Run(mode, func(t *testing.T) {
			var reads atomic.Int32
			p := diagnosticCustomProvider{Provider: scoped{provider.NewDemo()}, catalog: model.CustomCatalog{Enabled: mode != "disabled", Items: []model.CustomType{typ}}}
			if mode == "catalog denied" {
				p.catalogErr = &provider.Error{Code: 403, Message: "denied"}
			}
			p.list = func(ctx context.Context, d, ns string) ([]model.Object, model.CustomType, bool, error) {
				reads.Add(1)
				if d != typ.Definition || ns != "production" {
					t.Errorf("scope lost %s %s", d, ns)
				}
				if mode == "denied" {
					return nil, typ, false, &provider.Error{Code: 403, Message: "denied"}
				}
				o := diagnosticCustomObject("Database", "production", "db")
				if mode == "invalid objects" {
					model.Map(o, "metadata")["namespace"] = "staging"
				}
				if mode == "duplicate objects" {
					return []model.Object{o, o}, typ, false, nil
				}
				if mode == "collection identity" {
					actual := typ
					actual.Definition = "other.ops.example"
					return []model.Object{o}, actual, false, nil
				}
				if mode == "kind mismatch" {
					o["kind"] = "Secret"
				}
				if mode == "version mismatch" {
					o["apiVersion"] = "ops.example/v2"
				}
				return []model.Object{o}, typ, mode == "truncated", nil
			}
			snap := diagnosticSnapshot()
			if mode == "event denied" {
				snap.Warnings = append(snap.Warnings, model.Warning{Kind: "Event", Message: "denied"})
			}
			coverage, err := New(p).customDiagnostics(context.Background(), &snap, "")
			want := "partial"
			if mode == "available" {
				want = "available"
			}
			if mode == "disabled" {
				want = "disabled"
			}
			if mode == "catalog denied" {
				want = "unavailable"
			}
			if err != nil || coverage.State != want {
				t.Fatal(coverage, err)
			}
			if (mode == "disabled" || mode == "catalog denied") && reads.Load() != 0 {
				t.Fatal("read after disabled/denied discovery")
			}
			if mode == "invalid objects" && len(snap.Resources) > 0 {
				t.Fatal("cross-namespace disclosure")
			}
			if mode == "denied" && coverage.ResourcesObserved != 0 {
				t.Fatal("fabricated counts")
			}
			if mode == "duplicate objects" && (len(snap.Resources) != 1 || coverage.ResourcesObserved != 1) {
				t.Fatal("duplicate candidate", coverage)
			}
		})
	}
}

func TestCustomDiagnosticCandidateBudgetIsStable(t *testing.T) {
	typ := diagnosticType()
	objects := make([]model.Object, 2100)
	for i := range objects {
		objects[i] = diagnosticCustomObject("Database", "production", fmt.Sprintf("db-%04d", i))
	}
	p := diagnosticCustomProvider{Provider: provider.NewDemo(), catalog: model.CustomCatalog{Enabled: true, Items: []model.CustomType{typ}}}
	p.list = func(context.Context, string, string) ([]model.Object, model.CustomType, bool, error) {
		return objects, typ, false, nil
	}
	for range 2 {
		snap := diagnosticSnapshot()
		coverage, err := New(p).customDiagnostics(context.Background(), &snap, "")
		if err != nil || coverage.State != "partial" || coverage.ResourcesObserved != 2100 || coverage.CandidatesOmitted != 100 || len(snap.Resources) != 2000 {
			t.Fatal(coverage, err)
		}
		if snap.Resources[0].Name != "db-0000" || snap.Resources[1999].Name != "db-1999" {
			t.Fatal("candidate selection depends on collection order")
		}
		for i, j := 0, len(objects)-1; i < j; i, j = i+1, j-1 {
			objects[i], objects[j] = objects[j], objects[i]
		}
	}
}

func TestCustomDiagnosticsWorkersCancelAndCatalogIsBounded(t *testing.T) {
	var active, peak, reads atomic.Int32
	p := diagnosticCustomProvider{Provider: provider.NewDemo(), catalog: model.CustomCatalog{Enabled: true}}
	for i := range 35 {
		typ := diagnosticType()
		typ.Plural = fmt.Sprintf("databases%d", i)
		typ.Definition = typ.Plural + "." + typ.Group
		p.catalog.Items = append(p.catalog.Items, typ)
	}
	p.list = func(ctx context.Context, d, ns string) ([]model.Object, model.CustomType, bool, error) {
		reads.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		<-ctx.Done()
		return nil, model.CustomType{}, false, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { snap := diagnosticSnapshot(); _, err := New(p).customDiagnostics(ctx, &snap, ""); done <- err }()
	deadline := time.After(2 * time.Second)
	for peak.Load() < 4 {
		select {
		case <-deadline:
			t.Fatal("workers did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("workers leaked")
	}
	if peak.Load() > 4 || active.Load() != 0 || reads.Load() > 32 {
		t.Fatal(peak.Load(), active.Load(), reads.Load())
	}
	// Without cancellation, malformed and excessive catalog entries are partial.
	p.list = func(ctx context.Context, d, ns string) ([]model.Object, model.CustomType, bool, error) {
		typ := diagnosticType()
		typ.Definition = d
		typ.Plural = strings.TrimSuffix(d, ".ops.example")
		return nil, typ, false, nil
	}
	snap := diagnosticSnapshot()
	coverage, err := New(p).customDiagnostics(context.Background(), &snap, "")
	if err != nil || coverage.TypesInspected != 32 || coverage.State != "partial" {
		t.Fatal(coverage, err)
	}
}

func TestCustomDiagnosticEventsRequireUIDAndDoNotUseBuiltinReasonSemantics(t *testing.T) {
	o := diagnosticCustomObject("Pod", "production", "db")
	model.Map(o, "status")["conditions"] = []any{}
	r := model.SummarizeCustom(model.CustomProjection(o), "pods.ops.example")
	base := model.Event{ResourceUID: "uid-db", Resource: "db", Kind: "Pod", Namespace: "production", Type: "Warning", Reason: "FailedScheduling", Timestamp: observed.Add(-time.Minute).Format(time.RFC3339)}
	for _, mutate := range []func(*model.Event){func(e *model.Event) { e.ResourceUID = "" }, func(e *model.Event) { e.ResourceUID = "old" }, func(e *model.Event) { e.CustomDefinition = "pods.foreign.example" }, func(e *model.Event) { e.ResourceNamespace = "staging" }} {
		e := base
		mutate(&e)
		if len(DetectIssues(model.Snapshot{Resources: []model.Resource{r}, Events: []model.Event{e}, ObservedAt: observed})) > 0 {
			t.Fatal("uncertain identity matched", e)
		}
	}
	issues := DetectIssues(model.Snapshot{Resources: []model.Resource{r}, Events: []model.Event{base}, ObservedAt: observed})
	if len(issues) != 1 || issues[0].Title != "Recent warning: FailedScheduling" || issues[0].Events[0].CustomDefinition != "pods.ops.example" {
		t.Fatal(issues)
	}
}

func TestCustomDiagnosticKindCollisionAndUnknownHealth(t *testing.T) {
	typ := diagnosticType()
	typ.Kind = "Pod"
	typ.Plural = "pods"
	typ.Definition = "pods.ops.example"
	p := diagnosticCustomProvider{Provider: observedProvider{Provider: provider.NewDemo()}, catalog: model.CustomCatalog{Enabled: true, Items: []model.CustomType{typ}}}
	readyUnknown := false
	p.list = func(context.Context, string, string) ([]model.Object, model.CustomType, bool, error) {
		o := diagnosticCustomObject("Pod", "production", "db")
		if readyUnknown {
			model.Arr(o, "status", "conditions")[0].(map[string]any)["status"] = "Unknown"
		}
		return []model.Object{o}, typ, false, nil
	}
	for _, unknown := range []bool{false, true} {
		readyUnknown = unknown
		overview, err := New(p).Overview(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		if overview.Counts["Pod"] != 0 {
			t.Fatal("custom kind polluted built-in Pod count")
		}
		if unknown {
			if overview.Health != "unknown" || len(overview.Issues) != 0 {
				t.Fatal("unverified custom readiness reported as healthy", overview)
			}
		} else if overview.Health != "warning" || len(overview.Issues) != 1 {
			t.Fatal(overview)
		}
	}
}

func TestCustomDiagnosticClusterScopeAndSameKindsStayDistinct(t *testing.T) {
	namespaced := diagnosticType()
	cluster := namespaced
	cluster.Definition = "profiles.ops.example"
	cluster.Plural = "profiles"
	cluster.Namespaced = false
	p := diagnosticCustomProvider{Provider: scoped{provider.NewDemo()}, catalog: model.CustomCatalog{Enabled: true, Items: []model.CustomType{namespaced, cluster}}}
	p.list = func(ctx context.Context, d, ns string) ([]model.Object, model.CustomType, bool, error) {
		if ns != "production" {
			t.Errorf("namespace ceiling lost %s", ns)
		}
		typ := namespaced
		objectNS := "production"
		if d == cluster.Definition {
			typ = cluster
			objectNS = ""
		}
		o := diagnosticCustomObject("Database", objectNS, "same-name")
		model.Map(o, "metadata")["ownerReferences"] = []any{map[string]any{"controller": true, "kind": "Pod", "name": "foreign-parent"}}
		return []model.Object{o}, typ, false, nil
	}
	snap := diagnosticSnapshot()
	coverage, err := New(p).customDiagnostics(context.Background(), &snap, "")
	issues := DetectIssues(snap)
	if err != nil || coverage.State != "available" || len(issues) != 2 || issues[0].ID == issues[1].ID || issues[0].Resource.Definition == issues[1].Resource.Definition {
		t.Fatal(coverage, issues, err)
	}
	for _, r := range snap.Resources {
		if model.Value(r.Object, "metadata", "ownerReferences") != nil || model.Value(r.Object, "spec") != nil {
			t.Fatal("alternate provider bypassed projection")
		}
	}
}
