package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

func TestApplicationsCorrelateOwnershipSelectorsConfigurationAndDelivery(t *testing.T) {
	out, err := New(provider.NewDemo()).Applications(context.Background(), Query{Search: "checkout", Namespace: "production"})
	if err != nil || out.Total != 1 || len(out.Items) != 1 {
		t.Fatal(out, err)
	}
	a := out.Items[0]
	kinds := map[string]int{}
	for _, r := range a.Resources {
		kinds[r.Kind]++
	}
	if kinds["Deployment"] != 1 || kinds["ReplicaSet"] != 1 || kinds["Pod"] != 3 || kinds["Service"] != 1 || kinds["Secret"] != 1 || len(a.Issues) != 1 || len(a.Integrations) != 2 || a.Health != "critical" {
		t.Fatal(a, kinds)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "DEMO-PRIVATE-HELM-VALUE") {
		t.Fatal("release payload leaked")
	}
}
func TestApplicationsPagingScopeAndMissingCollections(t *testing.T) {
	s := New(scoped{provider.NewDemo()})
	if _, err := s.Applications(context.Background(), Query{Namespace: "staging"}); provider.Status(err) != 403 {
		t.Fatal(err)
	}
	p, err := s.Applications(context.Background(), Query{Limit: 1, Offset: 1})
	if err != nil || len(p.Items) != 1 || p.Total < 2 {
		t.Fatal(p, err)
	}
	if _, err := s.Applications(context.Background(), Query{Limit: 201}); provider.Status(err) != 400 {
		t.Fatal(err)
	}
}
func TestHelmConfigurationDiffRetainsVisibleChangesAndRedactsPayloads(t *testing.T) {
	d, err := New(provider.NewDemo()).HelmCompare(context.Background(), "production", "checkout-api", 2, 3)
	if err != nil || !d.ConfigurationComparable || len(d.Changes) == 0 {
		t.Fatal(d, err)
	}
	found := false
	for _, c := range d.Changes {
		found = found || c.Path == "/spec/replicas"
	}
	if !found {
		t.Fatal(d.Changes)
	}
	b, _ := json.Marshal(d)
	if strings.Contains(string(b), "DEMO-PRIVATE-HELM-VALUE") || strings.Contains(string(b), `"value":"DEMO`) {
		t.Fatal("manifest credential leaked")
	}
}
func TestPodMetricTotalsRequireExactCurrentContainerSets(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	p := model.Object{"kind": "Pod", "metadata": map[string]any{"name": "app", "namespace": "production", "creationTimestamp": now.Add(-time.Hour).Format(time.RFC3339)}, "spec": map[string]any{"containers": []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}}}, "status": map[string]any{"phase": "Running", "containerStatuses": []any{map[string]any{"name": "a", "state": map[string]any{"running": map[string]any{"startedAt": now.Add(-time.Minute).Format(time.RFC3339)}}}, map[string]any{"name": "b", "state": map[string]any{"running": map[string]any{"startedAt": now.Add(-time.Minute).Format(time.RFC3339)}}}}}}
	samples := []model.Metric{{Kind: "Pod", Name: "app", Namespace: "production", Container: "a", CPU: .1, Memory: 100, Timestamp: now.Format(time.RFC3339)}, {Kind: "Pod", Name: "app", Namespace: "production", Container: "b", CPU: .2, Memory: 200, Timestamp: now.Format(time.RFC3339)}}
	got := podMetrics([]model.Object{p}, samples, now)[0]
	if got.State != "available" || got.CPU == nil || *got.CPU < .299 || *got.Memory != 300 {
		t.Fatal(got)
	}
	for _, bad := range [][]model.Metric{samples[:1], {samples[0], samples[0], samples[1]}, {{Kind: "Pod", Name: "app", Namespace: "production", Container: "a", Timestamp: now.Add(-2 * time.Hour).Format(time.RFC3339)}, samples[1]}} {
		got := podMetrics([]model.Object{p}, bad, now)[0]
		if got.CPU != nil || got.Memory != nil || got.State == "available" {
			t.Fatal("partial samples became aggregate", got)
		}
	}
}

func TestFluxDeliveryAssociationsRequireExactInventoryOrOwnership(t *testing.T) {
	member := model.Summarize(model.Object{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "app", "namespace": "production", "labels": map[string]any{"helm.toolkit.fluxcd.io/name": "release", "helm.toolkit.fluxcd.io/namespace": "platform"}}})
	controller := func(kind, name, inventoryID, version string, generation, observed int) model.Resource {
		return model.Summarize(model.Sanitize(model.Object{"apiVersion": "kustomize.toolkit.fluxcd.io/v1", "kind": kind, "metadata": map[string]any{"name": name, "namespace": "platform", "generation": generation}, "status": map[string]any{"observedGeneration": observed, "inventory": map[string]any{"entries": []any{map[string]any{"id": inventoryID, "v": version}}}}}))
	}
	for _, tc := range []struct {
		id, version                string
		generation, observed, want int
	}{{"production_app_apps_Deployment", "v1", 2, 2, 1}, {"staging_app_apps_Deployment", "v1", 2, 2, 0}, {"production_app_other.example_Deployment", "v1", 2, 2, 0}, {"production_app_apps_Deployment", "v2", 2, 2, 0}, {"production_app_apps_Deployment", "v1", 2, 1, 0}} {
		c := controller("Kustomization", "config", tc.id, tc.version, tc.generation, tc.observed)
		got := deliveryAssociations([]model.Resource{member, c}, map[string]model.Resource{member.ID: member})
		if len(got[member.ID]) != tc.want {
			t.Fatal(tc, got)
		}
	}
	c := controller("HelmRelease", "release", "", "", 2, 2)
	got := deliveryAssociations([]model.Resource{member, c}, map[string]model.Resource{member.ID: member})
	if len(got[member.ID]) != 1 {
		t.Fatal("Flux Helm ownership labels not matched", got)
	}
}
