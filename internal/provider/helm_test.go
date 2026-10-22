package provider

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"timone/internal/model"
)

func helmSecret(revision int) model.Object {
	p := map[string]any{"name": "app", "namespace": "production", "version": revision, "info": map[string]any{"status": "deployed"}, "chart": map[string]any{"metadata": map[string]any{"name": "app", "version": "1.0"}}, "config": map[string]any{"password": "sensitive-marker"}, "manifest": "apiVersion: v1\nkind: Secret\nmetadata:\n  name: app\ndata:\n  password: sensitive-marker\n"}
	raw, _ := json.Marshal(p)
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write(raw)
	_ = w.Close()
	encoded := base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(b.Bytes())))
	return model.Object{"apiVersion": "v1", "kind": "Secret", "type": "helm.sh/release.v1", "metadata": map[string]any{"name": fmt.Sprintf("sh.helm.release.v1.app.v%d", revision), "namespace": "production", "labels": map[string]any{"owner": "helm", "name": "app", "version": fmt.Sprint(revision)}}, "data": map[string]any{"release": encoded}}
}
func TestHelmHistoryIsOptInScopedAndNeverCachesRawSecrets(t *testing.T) {
	calls := 0
	k := fakeCluster(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/api/v1/namespaces/production/secrets" {
			t.Errorf("unexpected resource path %s", r.URL.Path)
		}
		if calls == 1 && (r.URL.Query().Get("labelSelector") != "owner=helm,name=app" || r.URL.Query().Get("limit") != "5") {
			t.Errorf("unbounded/unfiltered history request: %s", r.URL.RawQuery)
		}
		raw, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "SecretList", "items": []any{helmSecret(2), helmSecret(10)}})
		return response(200, string(raw)), nil
	})
	if _, err := k.HelmHistory(context.Background(), "production", "app"); Status(err) != 403 || calls != 0 {
		t.Fatal("disabled Helm decoding accessed Kubernetes")
	}
	k.helmDetails = true
	k.namespace = "production"
	if _, err := k.HelmHistory(context.Background(), "other", "app"); Status(err) != 403 || calls != 0 {
		t.Fatal("namespace boundary bypassed")
	}
	h, err := k.HelmHistory(context.Background(), "production", "app")
	if err != nil || len(h.Revisions) != 2 || h.Revisions[0].Revision != 10 {
		t.Fatalf("history failed %+v %v", h, err)
	}
	if len(k.cache) != 0 || len(k.streams) != 0 {
		t.Fatal("history created an unsanitized cache or observer")
	}
	list, _, err := k.List(context.Background(), "Secret", "production")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range list {
		if model.Value(o, "data") != nil {
			t.Fatal("generic Secret endpoint leaked payload")
		}
	}
	raw, _ := json.Marshal(h)
	if strings.Contains(string(raw), "sensitive-marker") {
		t.Fatal("history exposed release values")
	}
	for _, entry := range k.cache {
		for _, o := range entry.items {
			if model.Value(o, "data") != nil {
				t.Fatal("raw Secret entered generic cache")
			}
		}
	}
}
func TestHelmHistoryReportsInvalidRevisionAndUpstreamPermissionFailures(t *testing.T) {
	k := fakeCluster(func(*http.Request) (*http.Response, error) {
		return response(403, `{"message":"sensitive-marker"}`), nil
	})
	k.helmDetails = true
	if _, err := k.HelmHistory(context.Background(), "production", "app"); Status(err) != 403 || strings.Contains(err.Error(), "sensitive-marker") {
		t.Fatalf("unsafe RBAC failure %v", err)
	}
	k = fakeCluster(func(*http.Request) (*http.Response, error) {
		o := helmSecret(1)
		model.Map(o, "data")["release"] = "sensitive-marker"
		raw, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "SecretList", "items": []any{o}})
		return response(200, string(raw)), nil
	})
	k.helmDetails = true
	h, err := k.HelmHistory(context.Background(), "production", "app")
	if err != nil || len(h.Revisions) != 0 || len(h.Warnings) != 1 {
		t.Fatalf("malformed revision hidden %+v %v", h, err)
	}
}
func TestHelmHistoryBoundsCollectionsAndValidatesReferences(t *testing.T) {
	for _, ref := range [][2]string{{"", "app"}, {"other/ns", "app"}, {"production", "app,name=other"}, {"production", strings.Repeat("a", 54)}} {
		if ValidateHelmReference(ref[0], ref[1]) == nil {
			t.Fatalf("invalid selector input %v", ref)
		}
	}
	if err := ValidateHelmReference("production", "app.internal"); err != nil {
		t.Fatal("valid Helm subdomain rejected")
	}
	k := fakeCluster(func(*http.Request) (*http.Response, error) {
		items := []any{}
		for i := 1; i <= 51; i++ {
			items = append(items, helmSecret(i))
		}
		raw, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "SecretList", "items": items})
		return response(200, string(raw)), nil
	})
	k.helmDetails = true
	if _, err := k.HelmHistory(context.Background(), "production", "app"); Status(err) != 413 {
		t.Fatalf("unbounded revision history %v", err)
	}
}
