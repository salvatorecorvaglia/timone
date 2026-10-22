package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"timone/internal/model"
	"timone/internal/provider"
)

func TestLiveReviewValidationAndNamespaceCeiling(t *testing.T) {
	s := New(scoped{provider.NewDemo()})
	for _, q := range []model.AccessQuery{{Kind: "Pod", Namespace: "staging", Verb: "get"}, {Kind: "Pod", Namespace: "production", Verb: "get", ServiceAccount: "viewer", ServiceAccountNamespace: "staging"}} {
		if _, err := s.ReviewAccess(context.Background(), q); provider.Status(err) != 403 {
			t.Fatal(err)
		}
	}
	for _, q := range []model.AccessQuery{{Kind: "anything", Verb: "get"}, {Kind: "Pod", Namespace: "production", Verb: "exec"}, {Kind: "Node", Namespace: "production", Verb: "get"}, {Kind: "Pod", Namespace: "production", Verb: "get", Subresource: "exec"}} {
		if _, err := s.ReviewAccess(context.Background(), q); provider.Status(err) != 400 {
			t.Fatal(q, err)
		}
	}
}
func TestGitOpsDriftComputesActualFieldDifferencesForAllControllers(t *testing.T) {
	s := New(provider.NewDemo())
	for _, target := range [][2]string{{"Application", "checkout-api"}, {"Kustomization", "cluster-config"}, {"HelmRelease", "monitoring"}} {
		out, err := s.GitOpsDrift(context.Background(), target[0], "platform", target[1])
		if err != nil || out.State != "drift-detected" || out.Checked != 1 || len(out.Changes) != 1 || out.Changes[0].Path != "/spec/replicas" {
			t.Fatal(out, err)
		}
	}
	if _, err := s.GitOpsDrift(context.Background(), "Pod", "production", "payments"); provider.Status(err) != 400 {
		t.Fatal(err)
	}
}
func TestHelmValuesDiffIsSanitizedAndOptIn(t *testing.T) {
	s := New(provider.NewDemo())
	out, err := s.HelmCompare(context.Background(), "production", "checkout-api", 1, 3)
	if err != nil || !out.ValuesComparable || len(out.ValuesChanges) != 2 {
		t.Fatal(out.ValuesChanges, err)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "DEMO-PRIVATE-VALUES") {
		t.Fatal("values credential leaked")
	}
}
func TestDesiredProjectionIgnoresDefaultsAndNamedListOrder(t *testing.T) {
	desired := map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"name": "a", "image": "app:1"}, map[string]any{"name": "b", "image": "app:2"}}}}
	live := map[string]any{"spec": map[string]any{"defaultField": true, "containers": []any{map[string]any{"name": "b", "image": "app:2", "extra": true}, map[string]any{"name": "a", "image": "app:1"}}}}
	a, _ := json.Marshal(desired)
	b, _ := json.Marshal(desiredLiveProjection(desired, live))
	if string(a) != string(b) {
		t.Fatal(string(a), string(b))
	}
}

type unreadableDriftProvider struct {
	provider.Provider
	provider.DesiredProvider
	code int
}

func (p unreadableDriftProvider) List(ctx context.Context, kind, ns string) ([]model.Object, bool, error) {
	if kind == "Deployment" && p.code == 404 {
		return []model.Object{}, false, nil
	}
	return p.Provider.List(ctx, kind, ns)
}
func (p unreadableDriftProvider) Get(ctx context.Context, kind, ns, name string) (model.Object, error) {
	if kind == "Deployment" {
		return nil, &provider.Error{Code: p.code, Message: "Unavailable"}
	}
	return p.Provider.Get(ctx, kind, ns, name)
}
func TestDriftDistinguishesMissingFromForbiddenLiveObjects(t *testing.T) {
	demo := provider.NewDemo()
	for _, tc := range []struct {
		code             int
		state            string
		checked, changes int
	}{{403, "partial", 0, 0}, {404, "drift-detected", 1, 1}} {
		p := unreadableDriftProvider{Provider: demo, DesiredProvider: demo.(provider.DesiredProvider), code: tc.code}
		out, err := New(p).GitOpsDrift(context.Background(), "Application", "platform", "checkout-api")
		if err != nil || out.State != tc.state || out.Checked != tc.checked || len(out.Changes) != tc.changes {
			t.Fatal(out, err)
		}
	}
}
