package service

import (
	"context"
	"testing"
	"timone/internal/model"
	"timone/internal/provider"
)

type auditFixtureProvider struct {
	provider.Provider
	objects     []model.Object
	deniedKinds map[string]bool
}

func (p auditFixtureProvider) List(_ context.Context, kind, ns string) ([]model.Object, bool, error) {
	if p.deniedKinds[kind] {
		return nil, false, &provider.Error{Code: 403, Message: "Controller collection denied"}
	}
	out := []model.Object{}
	for _, o := range p.objects {
		if model.Str(o, "kind") == kind {
			out = append(out, o)
		}
	}
	return out, false, nil
}
func (p auditFixtureProvider) Events(context.Context, string) ([]model.Event, error) {
	return []model.Event{}, nil
}
func TestAuditInspectsManagedAndEphemeralPrivileges(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		owned       bool
	}{{"owned regular container", "containers", true}, {"ephemeral container", "ephemeralContainers", false}} {
		t.Run(tc.name, func(t *testing.T) {
			meta := map[string]any{"name": "audit-pod", "namespace": "production", "uid": "p"}
			if tc.owned {
				meta["ownerReferences"] = []any{map[string]any{"kind": "ReplicaSet", "name": "owner", "controller": true}}
			}
			pod := model.Object{"kind": "Pod", "metadata": meta, "spec": map[string]any{tc.field: []any{map[string]any{"name": "danger", "image": "busybox:1.37", "securityContext": map[string]any{"privileged": true}}}}}
			report, err := New(auditFixtureProvider{Provider: provider.NewDemo(), objects: []model.Object{pod}, deniedKinds: map[string]bool{"Deployment": true, "ReplicaSet": true}}).Audit(context.Background(), "production")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			invalidRequirement := false
			for _, f := range report.Items {
				found = found || f.Rule == "privileged"
				invalidRequirement = invalidRequirement || (tc.field == "ephemeralContainers" && (f.Rule == "resources" || f.Rule == "probes"))
			}
			if invalidRequirement {
				t.Error("ephemeral container was required to declare unsupported budgets/probes")
			}
			if !found {
				t.Errorf("privileged=true present in %s but audit reports no privileged finding; performed=%v", tc.name, report.Performed)
			}
		})
	}
}
