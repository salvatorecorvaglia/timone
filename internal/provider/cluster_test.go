package provider

import (
	"context"
	"encoding/json"
	"io"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"timone/internal/config"
)

type roundTrip func(*http.Request) (*http.Response, error)

func TestMissingKubeconfigContextGuidance(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, selected, message string
	}{
		{"empty", "", "", "no contexts found in kubeconfig"},
		{"stale empty", "current-context: orbstack\n", "", "no contexts found in kubeconfig"},
		{"unselected", "contexts:\n- name: available\n  context: {}\n", "", "no current kubeconfig context selected"},
		{"stale", "current-context: old\ncontexts:\n- name: available\n  context: {}\n", "", "selected kubeconfig context does not exist"},
		{"explicit", "current-context: available\ncontexts:\n- name: available\n  context: {}\n", "missing", "selected kubeconfig context does not exist"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(path, []byte(tc.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			_, _, _, _, err := loadKubernetesConfig(config.Config{Mode: "local", Kubeconfig: path, Context: tc.selected})
			if err == nil || !strings.Contains(err.Error(), tc.message) || !strings.Contains(err.Error(), "timone --") {
				t.Fatalf("expected actionable %q error, got %v", tc.message, err)
			}
		})
	}
}

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func fakeCluster(fn roundTrip) *cluster {
	cfg := &rest.Config{Host: "https://kubernetes.invalid", BearerToken: "test-token", Transport: fn}
	client, e := rest.HTTPClientFor(cfg)
	if e != nil {
		panic(e)
	}
	dc, e := dynamic.NewForConfigAndClient(cfg, client)
	if e != nil {
		panic(e)
	}
	k := newCluster("test", "", client, dc)
	k.server = cfg.Host
	k.auth = "token"
	return k
}
func TestKubernetesPermissionErrorsDoNotExposeBody(t *testing.T) {
	for _, code := range []int{401, 403, 404, 429} {
		k := fakeCluster(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("missing credentials")
			}
			return response(code, `{"message":"sensitive-marker"}`), nil
		})
		_, _, e := k.List(context.Background(), "Pod", "production")
		if Status(e) != code || strings.Contains(e.Error(), "sensitive-marker") {
			t.Fatalf("bad error %v", e)
		}
	}
}
func TestKubernetesPaginationRedactionAndTTLCache(t *testing.T) {
	var calls atomic.Int32
	k := fakeCluster(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Path != "/api/v1/namespaces/production/secrets" {
			t.Errorf("bad path %s", r.URL.Path)
		}
		if r.URL.Query().Get("continue") == "" {
			return response(200, `{"kind":"SecretList","apiVersion":"v1","metadata":{"continue":"next"},"items":[{"metadata":{"name":"one","namespace":"production"},"data":{"password":"sensitive-marker"}}]}`), nil
		}
		return response(200, `{"kind":"SecretList","apiVersion":"v1","items":[{"metadata":{"name":"two","namespace":"production"}}]}`), nil
	})
	o, tr, e := k.List(context.Background(), "Secret", "production")
	if e != nil || tr || len(o) != 2 {
		t.Fatalf("bad list %v", e)
	}
	b, _ := json.Marshal(o)
	if strings.Contains(string(b), "sensitive-marker") {
		t.Fatal("payload leaked")
	}
	_, _, _ = k.List(context.Background(), "Secret", "production")
	if calls.Load() != 2 {
		t.Fatal("list cache not reused")
	}
}
func TestMissingMetricsNeverBecomesZeroUsage(t *testing.T) {
	k := fakeCluster(func(r *http.Request) (*http.Response, error) { return response(404, ""), nil })
	m, e := k.Metrics(context.Background(), "")
	if e != nil || m.Available || len(m.Items) > 0 || m.Message == "" {
		t.Fatalf("bad unavailable metrics %+v %v", m, e)
	}
}
func TestLogsAreBoundedAndScoped(t *testing.T) {
	k := fakeCluster(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("limitBytes") != "131072" || r.URL.Query().Get("tailLines") != "100" {
			t.Error("unbounded logs request")
		}
		return response(200, strings.Repeat("x", 200000)), nil
	})
	logs, e := k.Logs(context.Background(), "production", "api", "main", 100)
	if e != nil || len(logs) != 131072 {
		t.Fatalf("log limit %d %v", len(logs), e)
	}
	k.namespace = "production"
	_, e = k.Get(context.Background(), "Pod", "staging", "api")
	if Status(e) != 403 {
		t.Fatal("scope escaped")
	}
}
func TestWatchCancellation(t *testing.T) {
	k := fakeCluster(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { k.Watch(ctx, func() {}); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch did not cancel")
	}
}
func TestKubeconfigContextAndExecOptIn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	for _, exec := range []bool{false, true} {
		user := "token: example-only"
		if exec {
			user = "exec:\n        apiVersion: client.authentication.k8s.io/v1\n        interactiveMode: Never\n        command: malicious-command"
		}
		content := "current-context: prod\nclusters:\n  - name: harbor\n    cluster:\n      server: https://kubernetes.invalid\ncontexts:\n  - name: prod\n    context:\n      cluster: harbor\n      user: reader\n      namespace: production\nusers:\n  - name: reader\n    user:\n      " + user + "\n"
		if e := os.WriteFile(path, []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
		p, e := NewCluster(config.Config{Mode: "local", Kubeconfig: path})
		if exec {
			if e == nil || !strings.Contains(e.Error(), "exec authentication is disabled") {
				t.Fatalf("exec not rejected: %v", e)
			}
		} else if e != nil || p.Name() != "prod" || p.Namespace() != "production" {
			t.Fatalf("context not loaded %v", e)
		}
	}
}
func TestQuantity(t *testing.T) {
	for s, want := range map[string]float64{"100m": .1, "100000000n": .1, "128Mi": 128 * (1 << 20), "2": 2, "32Gi": 32 * (1 << 30)} {
		if got := Quantity(s); got != want {
			t.Errorf("%s: %f want %f", s, got, want)
		}
	}
}
func TestListHonorsCancellation(t *testing.T) {
	k := fakeCluster(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, e := k.List(ctx, "Pod", "")
	if e == nil {
		t.Fatal("cancellation lost")
	}
}

func TestMalformedMetricsAreOmittedInsteadOfReportedAsZero(t *testing.T) {
	k := fakeCluster(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/metrics.k8s.io/") {
			return response(200, `{"items":[{"metadata":{"name":"broken"},"usage":{"cpu":"not-a-quantity"}}]}`), nil
		}
		return response(403, ""), nil
	})
	m, e := k.Metrics(context.Background(), "")
	if e != nil || len(m.Items) != 0 || !strings.Contains(m.Message, "omitted") {
		t.Fatalf("invalid metrics became usage: %+v %v", m, e)
	}
}

func TestEventSeriesUsesLatestOccurrenceAndUID(t *testing.T) {
	k := fakeCluster(func(r *http.Request) (*http.Response, error) {
		return response(200, `{"kind":"EventList","apiVersion":"v1","items":[{"metadata":{"uid":"event","namespace":"production","creationTimestamp":"2026-10-15T10:00:00Z"},"involvedObject":{"uid":"pod-uid","kind":"Pod","name":"api","namespace":"production"},"type":"Warning","reason":"Unhealthy","count":1,"eventTime":"2026-10-15T10:00:00Z","series":{"count":23,"lastObservedTime":"2026-10-15T11:00:00Z"}}]}`), nil
	})
	events, err := k.Events(context.Background(), "production")
	if err != nil || len(events) != 1 || events[0].ResourceUID != "pod-uid" || events[0].Timestamp != "2026-10-15T11:00:00Z" || events[0].Count != 23 || events[0].FirstObserved != "2026-10-15T10:00:00Z" {
		t.Fatalf("series metadata lost %+v %v", events, err)
	}
}

func TestRuntimeNamespaceExplicitCanClearKubeconfigDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contexts.yaml")
	data := `apiVersion: v1
kind: Config
clusters:
- name: cluster
  cluster:
    server: https://cluster.example.test
contexts:
- name: local
  context:
    cluster: cluster
    namespace: production
    user: viewer
users:
- name: viewer
  user:
    token: simulated-only
current-context: local
`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		explicit bool
		want     string
	}{{false, "production"}, {true, ""}} {
		_, _, ns, _, err := loadKubernetesConfig(config.Config{Mode: "local", Kubeconfig: path, Context: "local", NamespaceExplicit: tc.explicit})
		if err != nil || ns != tc.want {
			t.Fatal(ns, err)
		}
	}
}

func TestContextIdentityChangesWhenItsClusterEndpointIsRebound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	write := func(host, ns string) {
		t.Helper()
		data := "apiVersion: v1\nkind: Config\ncurrent-context: active\nclusters:\n- name: target\n  cluster:\n    server: " + host + "\ncontexts:\n- name: active\n  context:\n    cluster: target\n    namespace: " + ns + "\n"
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	load := func() string {
		t.Helper()
		p, err := NewCluster(config.Config{Mode: "local", Kubeconfig: path})
		if err != nil {
			t.Fatal(err)
		}
		return p.(ClusterIdentityProvider).ClusterIdentity()
	}
	write("https://first.example", "production")
	first := load()
	write("https://first.example", "staging")
	if load() != first {
		t.Fatal("namespace ceiling changed cluster binding")
	}
	write("https://second.example", "staging")
	if load() == first {
		t.Fatal("rebound context reused initial cluster identity")
	}
}
