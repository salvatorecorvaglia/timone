package service

import (
	"context"
	"strings"
	"testing"
	"timone/internal/model"
	"timone/internal/provider"
)

type missingIntegration struct {
	provider.Provider
	kind string
}

func (p missingIntegration) List(ctx context.Context, kind, ns string) ([]model.Object, bool, error) {
	if kind == p.kind {
		return nil, false, &provider.Error{Code: 403, Message: "Controller read denied"}
	}
	return p.Provider.List(ctx, kind, ns)
}
func TestFluxPartialAvailabilityPreservesOtherControllerRecords(t *testing.T) {
	for _, kind := range []string{"Kustomization", "HelmRelease"} {
		result, err := New(missingIntegration{provider.NewDemo(), kind}).Integrations(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range result.Items {
			if i.Name == "Flux" && (i.Status != "partial" || len(i.Records) != 1 || len(i.Warnings) != 1 || i.Records[0].Resource.Kind == kind) {
				t.Fatalf("partial Flux data hidden %+v", i)
			}
		}
	}
}
func TestGitOpsDoesNotConfuseReadinessFreshnessAttemptsAndDrift(t *testing.T) {
	makeFlux := func(status map[string]any, suspended bool) model.Resource {
		return model.Summarize(model.Object{"kind": "HelmRelease", "metadata": map[string]any{"name": "app", "namespace": "production", "generation": 2}, "spec": map[string]any{"suspend": suspended}, "status": status})
	}
	cases := []struct {
		name                    string
		r                       model.Resource
		status, revision, drift string
	}{
		{"stale ready", makeFlux(map[string]any{"observedGeneration": 1, "conditions": []any{map[string]any{"type": "Ready", "status": "True", "observedGeneration": 1}}}, false), "Awaiting observation", "", "Not assessed"},
		{"failure beats ready", makeFlux(map[string]any{"observedGeneration": 2, "conditions": []any{map[string]any{"type": "Ready", "status": "True", "observedGeneration": 2}, map[string]any{"type": "Stalled", "status": "True", "observedGeneration": 2}}}, false), "Stalled", "", "Not assessed"},
		{"suspended", makeFlux(map[string]any{"observedGeneration": 2, "conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}, true), "Suspended", "", "Not assessed"},
		{"attempt is not deployed", makeFlux(map[string]any{"observedGeneration": 2, "lastAttemptedRevision": "new", "conditions": []any{map[string]any{"type": "Ready", "status": "False"}}}, false), "Not ready", "", "Not assessed"},
		{"reported drift", makeFlux(map[string]any{"observedGeneration": 2, "history": []any{map[string]any{"chartVersion": "old", "status": "deployed"}}, "conditions": []any{map[string]any{"type": "Ready", "status": "True"}, map[string]any{"type": "Drifted", "status": "True", "reason": "DriftDetected"}}}, false), "Ready", "old", "Controller reports drift"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gitOpsRecord(tc.r)
			if r.Status != tc.status || r.Revision != tc.revision || r.Drift != tc.drift {
				t.Fatalf("incorrect controller interpretation %+v", r)
			}
		})
	}
	argo := model.Summarize(model.Object{"kind": "Application", "status": map[string]any{"sync": map[string]any{"status": "OutOfSync"}, "health": map[string]any{"status": "Healthy"}, "operationState": map[string]any{"phase": "Succeeded"}}})
	got := gitOpsRecord(argo)
	if got.Status != "OutOfSync" || got.Health != "Healthy" || got.Drift != "Not assessed" {
		t.Fatalf("sync/health/drift conflated %+v", got)
	}
}
func TestHelmHistoryComparisonUsesRetainedIdentityOnly(t *testing.T) {
	s := New(provider.NewDemo())
	h, err := s.HelmHistory(context.Background(), "production", "checkout-api")
	if err != nil || len(h.Revisions) != 3 || h.Revisions[0].Revision != 3 {
		t.Fatalf("history %+v %v", h, err)
	}
	d, err := s.HelmCompare(context.Background(), "production", "checkout-api", 1, 3)
	if err != nil || !d.InventoryComparable || len(d.Added) != 1 || d.Added[0].Kind != "Service" || len(d.Removed) != 0 {
		t.Fatalf("comparison %+v %v", d, err)
	}
	if !strings.Contains(d.Limitations, "redacted fields") {
		t.Fatal("comparison coverage omitted")
	}
	if _, err := s.HelmCompare(context.Background(), "production", "checkout-api", 2, 2); provider.Status(err) != 400 {
		t.Fatal("equal revisions accepted")
	}
	if _, err := s.HelmCompare(context.Background(), "production", "checkout-api", 1, 10); provider.Status(err) != 404 {
		t.Fatal("missing revision fabricated")
	}
	scopedService := New(scoped{provider.NewDemo()})
	if _, err := scopedService.HelmHistory(context.Background(), "platform", "prometheus"); provider.Status(err) != 403 {
		t.Fatal("Helm service bypassed configured scope")
	}
}
func TestBindingAuditReportsDirectIndicatorsWithoutClaimingEffectiveAccess(t *testing.T) {
	role := model.Summarize(model.Object{"kind": "ClusterRole", "metadata": map[string]any{"name": "admin"}, "rules": []any{map[string]any{"apiGroups": []any{"*"}, "resources": []any{"*"}, "verbs": []any{"*"}}}})
	binding := model.Summarize(model.Object{"kind": "RoleBinding", "metadata": map[string]any{"name": "bound", "namespace": "production"}, "roleRef": map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "admin"}, "subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "app", "namespace": "production"}}})
	snap := model.Snapshot{Resources: []model.Resource{role, binding}}
	findings, checked := bindingFindings(snap, "production")
	if !checked || len(findings) != 1 || findings[0].Severity != "critical" || !strings.Contains(findings[0].Evidence, "Direct binding indicator only") {
		t.Fatalf("missing conservative binding finding %+v", findings)
	}
	if findings, _ := bindingFindings(snap, "other"); len(findings) != 0 {
		t.Fatal("binding finding outside selected namespace")
	}
	snap.Resources = []model.Resource{binding}
	snap.Warnings = []model.Warning{{Kind: "ClusterRole", Message: "denied"}}
	if findings, _ := bindingFindings(snap, "production"); len(findings) != 0 {
		t.Fatal("unreadable role fabricated")
	}
}

func TestAuditHonorsContainerSecurityOverrideAndIncludesJobs(t *testing.T) {
	container := map[string]any{"name": "batch", "image": "batch:v1", "securityContext": map[string]any{"runAsNonRoot": false, "allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true}}
	job := model.Object{"kind": "Job", "metadata": map[string]any{"name": "batch", "namespace": "production"}, "spec": map[string]any{"template": map[string]any{"spec": map[string]any{"securityContext": map[string]any{"runAsNonRoot": true}, "containers": []any{container}}}}}
	p := observedProvider{Provider: provider.NewDemo(), objects: map[string][]model.Object{"Job": {job}}}
	result, err := New(p).Audit(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	security := false
	for _, f := range result.Items {
		if f.Resource.Kind != "Job" {
			continue
		}
		if f.Rule == "security-context" {
			security = true
		}
		if f.Rule == "probes" {
			t.Fatal("batch Job incorrectly requires long-running probes")
		}
	}
	if !security {
		t.Fatal("explicit container runAsNonRoot=false was hidden by inherited Pod setting")
	}
}

func TestHelmMetadataGroupsRevisionsAndSkipsNonconformingStorage(t *testing.T) {
	result, err := New(provider.NewDemo()).Integrations(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range result.Items {
		if i.Name != "Helm" {
			continue
		}
		if len(i.Records) != 3 {
			t.Fatalf("revisions not grouped %+v", i)
		}
		for _, r := range i.Records {
			if r.Revision != "3" || r.RetainedRevisions != 3 {
				t.Fatalf("incorrect retained revision metadata %+v", r)
			}
		}
	}
	invalid := model.Object{"kind": "Secret", "metadata": map[string]any{"name": "ordinary-secret", "namespace": "production", "labels": map[string]any{"owner": "helm"}}}
	p := observedProvider{Provider: provider.NewDemo(), objects: map[string][]model.Object{"Secret": {invalid}}}
	result, err = New(p).Integrations(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range result.Items {
		if i.Name == "Helm" && (len(i.Records) != 0 || len(i.Warnings) != 1 || i.Status != "partial") {
			t.Fatalf("ordinary Secret treated as release %+v", i)
		}
	}
}
