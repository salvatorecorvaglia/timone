package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery/cached/memory"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	ktesting "k8s.io/client-go/testing"

	"timone/internal/config"
	"timone/internal/credentials"
	"timone/internal/model"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "credential-exec":
			if credentials.Run(context.Background(), os.Args[2:], os.Stdout) != nil {
				os.Exit(1)
			}
			os.Exit(0)
		case "timone-test-plugin":
			var info struct {
				Spec struct {
					Interactive bool `json:"interactive"`
				}
			}
			_ = json.Unmarshal([]byte(os.Getenv("KUBERNETES_EXEC_INFO")), &info)
			if info.Spec.Interactive {
				os.Exit(2)
			}
			_, _ = os.Stderr.WriteString("sensitive-plugin-stderr")
			_, _ = os.Stdout.WriteString(`{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","status":{"token":"fixture-plugin-token","expirationTimestamp":"2099-01-01T00:00:00Z"}}`)
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

var podsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
var secretsGVR = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

func fakeObject(kind, ns, name, rv string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": kind, "metadata": map[string]any{"name": name, "namespace": ns, "uid": name, "resourceVersion": rv}, "spec": map[string]any{}, "status": map[string]any{"phase": "Running"}}}
}
func clientgoCluster(objects ...runtime.Object) (*cluster, *fake.FakeDynamicClient) {
	dc := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{podsGVR: "PodList", secretsGVR: "SecretList"}, objects...)
	k := newCluster("fake-client-go", "production", &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return response(200, `{"gitVersion":"v1.37.1"}`), nil })}, dc)
	k.server = "https://kubernetes.invalid"
	return k, dc
}
func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
func startObservers(t *testing.T, k *cluster, notify func()) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { k.Watch(ctx, notify); close(done) }()
	eventually(t, func() bool { k.mu.Lock(); defer k.mu.Unlock(); return k.watchCtx != nil })
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("informers did not stop")
		}
	})
	return ctx
}
func readyStream(k *cluster, key string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	s := k.streams[key]
	return s != nil && s.informer != nil && s.healthy && s.informer.HasSynced()
}
func TestClientGoInformerUpdatesWithoutRelistingEveryChange(t *testing.T) {
	k, dc := clientgoCluster(fakeObject("Pod", "production", "api", "1"))
	var notified atomic.Int32
	ctx := startObservers(t, k, func() { notified.Add(1) })
	items, _, e := k.List(ctx, "Pod", "production")
	if e != nil || len(items) != 1 {
		t.Fatal(e)
	}
	eventually(t, func() bool { return readyStream(k, "Pod:production") })
	updated := fakeObject("Pod", "production", "api", "2")
	updated.Object["status"] = map[string]any{"phase": "Failed"}
	if _, e := dc.Resource(podsGVR).Namespace("production").Update(ctx, updated, metav1.UpdateOptions{}); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool {
		items, _, e := k.List(ctx, "Pod", "production")
		return e == nil && len(items) == 1 && model.Str(items[0], "status", "phase") == "Failed"
	})
	lists := 0
	for _, a := range dc.Actions() {
		if a.GetVerb() == "list" {
			lists++
		}
	}
	if lists != 1 {
		t.Fatalf("expected one authoritative list; got %d", lists)
	}
	if notified.Load() == 0 {
		t.Fatal("no change invalidation")
	}
	if e := dc.Resource(podsGVR).Namespace("production").Delete(ctx, "api", metav1.DeleteOptions{}); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { o, _, e := k.List(ctx, "Pod", "production"); return e == nil && len(o) == 0 })
}
func TestClientGoSecretInformerStoreIsSanitized(t *testing.T) {
	secret := fakeObject("Secret", "production", "db", "1")
	secret.Object["data"] = map[string]any{"password": "sensitive-marker"}
	secret.SetAnnotations(map[string]string{"last-applied": "sensitive-marker"})
	k, _ := clientgoCluster(secret)
	ctx := startObservers(t, k, func() {})
	if _, _, e := k.List(ctx, "Secret", "production"); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return readyStream(k, "Secret:production") })
	k.mu.Lock()
	s := k.streams["Secret:production"]
	k.mu.Unlock()
	for _, item := range s.informer.GetStore().List() {
		b, _ := json.Marshal(item)
		if strings.Contains(string(b), "sensitive-marker") {
			t.Fatal("secret entered informer store")
		}
	}
}
func TestClientGoRevokedReadPermissionDoesNotServeInformerCache(t *testing.T) {
	k, dc := clientgoCluster(fakeObject("Pod", "production", "api", "1"))
	var denied atomic.Bool
	dc.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		if denied.Load() {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", io.EOF)
		}
		return false, nil, nil
	})
	ctx := startObservers(t, k, func() {})
	if _, _, e := k.List(ctx, "Pod", "production"); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return readyStream(k, "Pod:production") })
	denied.Store(true)
	k.mu.Lock()
	entry := k.cache["Pod:production"]
	entry.at = time.Now().Add(-readLease - time.Second)
	k.cache["Pod:production"] = entry
	k.mu.Unlock()
	for range 2 {
		if _, _, e := k.List(ctx, "Pod", "production"); Status(e) != 403 {
			t.Fatalf("revoked permissions bypassed: %v", e)
		}
	}
	denied.Store(false)
	if _, _, e := k.List(ctx, "Pod", "production"); e != nil {
		t.Fatal(e)
	}
}
func TestClientGoWatchDenialFallsBackWithoutLeakingErrorBody(t *testing.T) {
	k, dc := clientgoCluster(fakeObject("Pod", "production", "api", "1"))
	dc.PrependWatchReactor("pods", func(ktesting.Action) (bool, watch.Interface, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", io.ErrUnexpectedEOF)
	})
	ctx := startObservers(t, k, func() {})
	if _, _, e := k.List(ctx, "Pod", "production"); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool {
		k.mu.Lock()
		defer k.mu.Unlock()
		s := k.streams["Pod:production"]
		return s != nil && s.state == "polling"
	})
	if _, _, e := k.List(ctx, "Pod", "production"); e != nil {
		t.Fatal(e)
	}
	c := k.Connection(ctx)
	if len(c.Streams) != 1 || strings.Contains(c.Streams[0].Message, "unexpected EOF") {
		t.Fatalf("watch state error leaked %+v", c)
	}
}
func TestClientGoSingleFlightAndNamespaceCeiling(t *testing.T) {
	k, dc := clientgoCluster(fakeObject("Pod", "production", "api", "1"))
	var calls atomic.Int32
	dc.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return false, nil, nil
	})
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, e := k.List(context.Background(), "Pod", ""); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("duplicate upstream requests %d", calls.Load())
	}
	before := len(dc.Actions())
	if _, _, e := k.List(context.Background(), "Pod", "staging"); Status(e) != 403 {
		t.Fatal("namespace escaped")
	}
	if len(dc.Actions()) != before {
		t.Fatal("out-of-scope API request was sent")
	}
}
func TestClientGoDiscoveryUsesServedVersionAndRejectsUnavailableKinds(t *testing.T) {
	k, _ := clientgoCluster()
	fd := &fakediscovery.FakeDiscovery{Fake: &ktesting.Fake{Resources: []*metav1.APIResourceList{{GroupVersion: "gateway.networking.k8s.io/v1beta1", APIResources: []metav1.APIResource{{Name: "gateways", Kind: "Gateway", Namespaced: true, Verbs: metav1.Verbs{"get", "list", "watch"}}}}}}}
	k.mapper = restmapper.NewDeferredDiscoveryRESTMapperWithContext(memory.NewMemCacheClientWithContext(fd))
	spec, _ := FindKind("Gateway")
	mapped, e := k.mapping(context.Background(), spec)
	if e != nil || mapped.Version != "v1beta1" {
		t.Fatalf("served version ignored %+v %v", mapped, e)
	}
	spec, _ = FindKind("Application")
	if _, e = k.mapping(context.Background(), spec); Status(e) != 404 {
		t.Fatalf("unavailable CRD not reported %v", e)
	}
}

func TestClientGoGatewayDiagnosticReadsRespectRBACAndNamespace(t *testing.T) {
	grantGVR := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "referencegrants"}
	classGVR := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}
	grant := fakeObject("ReferenceGrant", "production", "permit", "1")
	grant.SetAPIVersion("gateway.networking.k8s.io/v1beta1")
	class := fakeObject("GatewayClass", "", "controller", "1")
	class.SetAPIVersion("gateway.networking.k8s.io/v1")
	dc := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{grantGVR: "ReferenceGrantList", classGVR: "GatewayClassList"}, grant, class)
	k := newCluster("gateway-fake", "production", &http.Client{}, dc)
	items, truncated, err := k.List(context.Background(), "ReferenceGrant", "production")
	if err != nil || truncated || len(items) != 1 {
		t.Fatalf("grant list: %v %v %+v", err, truncated, items)
	}
	if _, err := k.Get(context.Background(), "GatewayClass", "", "controller"); err != nil {
		t.Fatalf("cluster-scoped class read: %v", err)
	}
	before := len(dc.Actions())
	if _, _, err := k.List(context.Background(), "ReferenceGrant", "staging"); Status(err) != 403 {
		t.Fatalf("scope escaped: %v", err)
	}
	if len(dc.Actions()) != before {
		t.Fatal("out-of-scope grant request reached Kubernetes")
	}
	dc.PrependReactor("get", "gatewayclasses", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: classGVR.Group, Resource: classGVR.Resource}, "controller", fmt.Errorf("fixture denied"))
	})
	if _, err := k.Get(context.Background(), "GatewayClass", "", "controller"); Status(err) != 403 {
		t.Fatalf("denied class read: %v", err)
	}
}
func TestClientGoMergedKubeconfigAndExplicitContext(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "contexts"), filepath.Join(dir, "credentials")
	if e := os.WriteFile(a, []byte("current-context: prod\ncontexts:\n- name: prod\n  context:\n    cluster: harbor\n    user: reader\n    namespace: production\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(b, []byte("clusters:\n- name: harbor\n  cluster:\n    server: https://kubernetes.invalid\nusers:\n- name: reader\n  user:\n    token: fixture-only\n"), 0600); e != nil {
		t.Fatal(e)
	}
	cfg, name, ns, auth, e := loadKubernetesConfig(config.Config{Mode: "local", Kubeconfig: a + string(os.PathListSeparator) + b})
	if e != nil || name != "prod" || ns != "production" || auth != "token" || cfg.BearerToken != "fixture-only" {
		t.Fatalf("merge failed %s %s %s %v", name, ns, auth, e)
	}
}
func TestClientGoExecCredentialOptInUsesBoundedHelper(t *testing.T) {
	exe, _ := os.Executable()
	path := filepath.Join(t.TempDir(), "config")
	cfg := map[string]any{"apiVersion": "v1", "kind": "Config", "current-context": "prod", "contexts": []any{map[string]any{"name": "prod", "context": map[string]any{"cluster": "harbor", "user": "exec"}}}, "clusters": []any{map[string]any{"name": "harbor", "cluster": map[string]any{"server": "https://kubernetes.invalid"}}}, "users": []any{map[string]any{"name": "exec", "user": map[string]any{"exec": map[string]any{"command": exe, "args": []any{"timone-test-plugin"}, "apiVersion": "client.authentication.k8s.io/v1", "interactiveMode": "Never"}}}}}
	b, _ := json.Marshal(cfg)
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	if _, _, _, _, e := loadKubernetesConfig(config.Config{Mode: "local", Kubeconfig: path}); e == nil {
		t.Fatal("exec ran without opt-in")
	}
	restCfg, _, _, auth, e := loadKubernetesConfig(config.Config{Mode: "local", Kubeconfig: path, AllowExecAuth: true})
	if e != nil || auth != "exec-plugin" {
		t.Fatal(e)
	}
	if restCfg.ExecProvider.Args[0] != "credential-exec" {
		t.Fatal("timeout/redaction helper missing")
	}
	base := roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer fixture-plugin-token" {
			t.Error("exec credentials not used")
		}
		return response(200, `{}`), nil
	})
	restCfg.WrapTransport = func(http.RoundTripper) http.RoundTripper { return base }
	client, e := rest.HTTPClientFor(restCfg)
	if e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://kubernetes.invalid/version", nil)
	res, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
}

func TestClientGoExpiredWatchResynchronizes(t *testing.T) {
	k, dc := clientgoCluster(fakeObject("Pod", "production", "api", "1"))
	var calls atomic.Int32
	dc.PrependWatchReactor("pods", func(ktesting.Action) (bool, watch.Interface, error) {
		if calls.Add(1) == 1 {
			return true, nil, apierrors.NewResourceExpired("sensitive-marker")
		}
		return false, nil, nil
	})
	ctx := startObservers(t, k, func() {})
	if _, _, e := k.List(ctx, "Pod", "production"); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return calls.Load() >= 2 && readyStream(k, "Pod:production") })
	c := k.Connection(ctx)
	if len(c.Streams) != 1 || strings.Contains(c.Streams[0].Message, "sensitive-marker") {
		t.Fatal("expired watch did not recover safely")
	}
}
func TestClientGoConnectionSeparatesReachabilityAndPermissions(t *testing.T) {
	k, _ := clientgoCluster()
	if c := k.Connection(context.Background()); c.State != "connected" || c.ServerVersion != "v1.37.1" {
		t.Fatalf("bad reachability %+v", c)
	}
	k.observe("Pod:production", apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", io.EOF))
	if c := k.Connection(context.Background()); c.State != "degraded" || len(c.CollectionFailures) != 1 {
		t.Fatalf("permission failure hidden %+v", c)
	}
	k.observe("Pod:production", nil)
	if c := k.Connection(context.Background()); c.State != "connected" {
		t.Fatalf("permission recovery missing %+v", c)
	}
}
func TestClientGoWatchScopeNeverExpandsSelectedNamespace(t *testing.T) {
	k, dc := clientgoCluster(fakeObject("Pod", "production", "api", "1"))
	ctx := startObservers(t, k, func() {})
	if _, _, e := k.List(ctx, "Pod", ""); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return readyStream(k, "Pod:production") })
	for _, a := range dc.Actions() {
		if a.GetNamespace() != "production" {
			t.Fatalf("request escaped namespace: %+v", a)
		}
	}
}

func TestClientGoTruncatedCollectionDoesNotStartInformer(t *testing.T) {
	k, dc := clientgoCluster()
	list := &unstructured.UnstructuredList{}
	for i := range collectionLimit + 1 {
		o := fakeObject("Pod", "production", fmt.Sprintf("pod-%d", i), "1")
		list.Items = append(list.Items, *o)
	}
	dc.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) { return true, list.DeepCopy(), nil })
	ctx := startObservers(t, k, func() {})
	items, truncated, e := k.List(ctx, "Pod", "production")
	if e != nil || !truncated || len(items) != collectionLimit {
		t.Fatalf("collection limit failed %d %t %v", len(items), truncated, e)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.streams) != 0 {
		t.Fatal("truncated snapshot started an incomplete informer")
	}
}

func TestGitOpsRedactionPrecedesCacheAndInformerStorage(t *testing.T) {
	application := fakeObject("Application", "production", "app", "1")
	application.SetAPIVersion("argoproj.io/v1alpha1")
	application.Object["spec"] = map[string]any{"source": map[string]any{"targetRevision": "main", "helm": map[string]any{"values": "private-marker"}}, "template": map[string]any{"metadata": map[string]any{"annotations": map[string]any{"private": "private-marker"}}}}
	application.SetAnnotations(map[string]string{"private": "private-marker"})
	dc := fake.NewSimpleDynamicClient(runtime.NewScheme(), application)
	k := newCluster("test", "production", &http.Client{}, dc)
	ctx := startObservers(t, k, func() {})
	objects, _, err := k.List(ctx, "Application", "production")
	if err != nil {
		t.Fatal(err)
	}
	assertClean := func(v any) {
		t.Helper()
		data, _ := json.Marshal(v)
		if strings.Contains(string(data), "private-marker") {
			t.Fatal("raw GitOps configuration retained")
		}
	}
	assertClean(objects)
	eventually(t, func() bool { return readyStream(k, "Application:production") })
	k.mu.Lock()
	s := k.streams["Application:production"]
	informer := s.informer
	var cached []model.Object
	for _, entry := range k.cache {
		cached = append(cached, entry.items...)
	}
	k.mu.Unlock()
	assertClean(cached)
	assertClean(informer.GetStore().List())
	got, err := k.Get(ctx, "Application", "production", "app")
	if err != nil {
		t.Fatal(err)
	}
	assertClean(got)
}
