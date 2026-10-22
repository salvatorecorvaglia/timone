package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	"net/http"
	"strings"
	"testing"
	"timone/internal/integrations"
	"timone/internal/model"
)

func TestArgoDesiredManifestAdapterVerifiesApplicationAndDestination(t *testing.T) {
	application := fakeObject("Application", "platform", "app", "uid1")
	application.SetAPIVersion("argoproj.io/v1alpha1")
	application.SetGeneration(2)
	application.Object["spec"] = map[string]any{"destination": map[string]any{"server": "https://cluster.test", "namespace": "production"}, "source": map[string]any{"targetRevision": "main"}}
	dc := fake.NewSimpleDynamicClient(runtime.NewScheme(), application)
	k := newCluster("fake", "", &http.Client{}, dc)
	k.server = "https://cluster.test"
	k.desired = integrations.DesiredOptions{ArgoURL: "https://argo.test"}
	calls := 0
	k.desiredTransport = tableRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "argo.test" || r.URL.Query().Get("appNamespace") != "platform" || r.Header.Get("Authorization") != "" {
			t.Fatal(r.URL, r.Header)
		}
		var response any = application.Object
		if strings.HasSuffix(r.URL.Path, "/manifests") {
			if r.URL.Query().Get("revision") != "main" {
				t.Fatal(r.URL)
			}
			response = map[string]any{"revision": "commit123", "manifests": []string{`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"app"},"spec":{"replicas":3}}`}}
		}
		b, _ := json.Marshal(response)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
	})
	out, err := k.DesiredManifests(context.Background(), "Application", "platform", "app")
	if err != nil || out.Revision != "commit123" || len(out.Manifests) != 1 || calls != 2 {
		t.Fatal(out, calls, err)
	}
	k.desired.Destination = "https://another.test"
	if _, err = k.DesiredManifests(context.Background(), "Application", "platform", "app"); Status(err) != 503 || calls != 2 {
		t.Fatal("unbound destination fetched", err, calls)
	}
}
func TestFluxDesiredRendererRequiresExactUIDGenerationAndRevision(t *testing.T) {
	controller := fakeObject("Kustomization", "platform", "config", "uid2")
	controller.SetAPIVersion("kustomize.toolkit.fluxcd.io/v1")
	controller.SetGeneration(3)
	controller.Object["status"] = map[string]any{"lastAttemptedRevision": "main@sha1:abc", "observedGeneration": int64(3)}
	dc := fake.NewSimpleDynamicClient(runtime.NewScheme(), controller)
	k := newCluster("fake", "", &http.Client{}, dc)
	if _, err := k.DesiredManifests(context.Background(), "Kustomization", "platform", "config"); Status(err) != 503 {
		t.Fatal(err)
	}
	k.desired.RenderedURL = "https://render.test"
	wrong := false
	k.desiredTransport = tableRoundTrip(func(r *http.Request) (*http.Response, error) {
		revision := r.URL.Query().Get("revision")
		if wrong {
			revision = "other"
		}
		out := model.DesiredBundle{ControllerUID: string(controller.GetUID()), Generation: 3, Revision: revision, Namespace: "production", Manifests: []model.Object{{"apiVersion": "v1", "kind": "Service", "metadata": map[string]any{"name": "app"}}}}
		b, _ := json.Marshal(out)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
	})
	if out, err := k.DesiredManifests(context.Background(), "Kustomization", "platform", "config"); err != nil || out.Revision != "main@sha1:abc" {
		t.Fatal(out, err)
	}
	wrong = true
	if _, err := k.DesiredManifests(context.Background(), "Kustomization", "platform", "config"); Status(err) != 409 {
		t.Fatal("unbound renderer accepted", err)
	}
}

func TestFluxStoredReleaseUsesResolvedSnapshotAndTargetNamespace(t *testing.T) {
	for _, target := range []string{"production", "platform"} {
		for _, name := range []string{"platform-app", strings.Repeat("a", 40) + "-97af5d7f41f3"} {
			t.Run(target+"/"+name, func(t *testing.T) {
				controller := fakeObject("HelmRelease", "platform", "app", "1")
				controller.SetAPIVersion("helm.toolkit.fluxcd.io/v2")
				controller.SetGeneration(1)
				controller.Object["spec"] = map[string]any{"targetNamespace": target}
				snapshot := map[string]any{"name": name, "namespace": target, "version": int64(2), "status": "deployed", "chartVersion": "1.0", "configDigest": "sha256:test"}
				controller.Object["status"] = map[string]any{"observedGeneration": int64(1), "lastAttemptedRevision": "1.0", "lastAttemptedConfigDigest": "sha256:test", "history": []any{snapshot}}
				secret := helmSecret(2)
				model.Map(secret, "metadata")["namespace"] = "platform"
				model.Map(secret, "metadata")["name"] = "sh.helm.release.v1." + name + ".v2"
				model.Map(secret, "metadata", "labels")["name"] = name
				payload := map[string]any{"name": name, "namespace": target, "version": 2, "info": map[string]any{"status": "deployed"}, "chart": map[string]any{"metadata": map[string]any{"name": "app", "version": "1.0"}}, "manifest": "apiVersion: v1\nkind: Service\nmetadata:\n  name: app\n"}
				data, _ := json.Marshal(payload)
				model.Map(secret, "data")["release"] = base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(data)))
				k := fakeCluster(func(r *http.Request) (*http.Response, error) {
					if strings.HasSuffix(r.URL.Path, "/secrets") {
						if !strings.Contains(r.URL.Path, "/namespaces/platform/") {
							t.Fatal(r.URL)
						}
						data, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "SecretList", "items": []any{secret}})
						return response(200, string(data)), nil
					}
					data, _ := json.Marshal(controller.Object)
					return response(200, string(data)), nil
				})
				k.helmDetails = true
				out, err := k.DesiredManifests(context.Background(), "HelmRelease", "platform", "app")
				if err != nil || len(out.Manifests) != 1 || out.Namespace != target {
					t.Fatal(out, err)
				}
				snapshot["namespace"] = "other"
				if _, err := k.DesiredManifests(context.Background(), "HelmRelease", "platform", "app"); Status(err) != 409 {
					t.Fatal("mismatched snapshot accepted", err)
				}
			})
		}
	}
}
