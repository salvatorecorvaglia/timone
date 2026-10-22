package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
	"timone/internal/config"
	"timone/internal/model"
	"timone/internal/provider"
	"timone/internal/service"
)

func handler(token string) (http.Handler, *Broker) {
	b := NewBroker()
	return New(service.New(provider.NewDemo()), config.Config{Listen: "127.0.0.1:8080", Mode: "demo", Token: token}, fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Timone</title>")}}, b), b
}
func perform(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestDemoAPIIntegration(t *testing.T) {
	h, _ := handler("")
	paths := []string{"/api/v1/connection", "/healthz", "/readyz", "/api/v1/cluster", "/api/v1/namespaces", "/api/v1/overview", "/api/v1/resources?kind=Pod&namespace=production&limit=2", "/api/v1/search?search=checkout", "/api/v1/resources/Deployment/production/checkout-api", "/api/v1/events?status=warning", "/api/v1/issues", "/api/v1/topology?namespace=production", "/api/v1/metrics", "/api/v1/integrations", "/api/v1/audit", "/api/v1/pods/production/checkout-api-7c8d9f-m4n7q/logs"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			w := perform(h, "GET", path, "", "")
			if w.Code != 200 {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			if !json.Valid(w.Body.Bytes()) {
				t.Fatal("invalid JSON")
			}
			if w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("security headers missing")
			}
		})
	}
}

func TestGatewayEvidenceUsesProtectedTopologyAndResourceAPIs(t *testing.T) {
	token := strings.Repeat("g", 32)
	h, _ := handler(token)
	for _, path := range []string{"/api/v1/topology", "/api/v1/resources?kind=ReferenceGrant", "/api/v1/resources/GatewayClass/_/demo"} {
		if got := perform(h, "GET", path, "", "").Code; got != 401 {
			t.Fatalf("anonymous read %s: %d", path, got)
		}
		if w := perform(h, "GET", path, "", token); w.Code != 200 {
			t.Fatalf("authorized read %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	w := perform(h, "GET", "/api/v1/topology", "", token)
	var graph model.Graph
	if err := json.Unmarshal(w.Body.Bytes(), &graph); err != nil {
		t.Fatal(err)
	}
	accepted, rejected, grant := false, false, false
	for _, edge := range graph.Edges {
		if edge.Routing == nil {
			continue
		}
		if edge.Confidence != "declared" {
			t.Fatalf("report changed relation provenance: %+v", edge)
		}
		accepted = accepted || edge.Routing.Accepted == "True"
		rejected = rejected || edge.Routing.Accepted == "False"
		for _, e := range edge.Routing.Evidence {
			grant = grant || strings.Contains(e, "Matching ReferenceGrant: production/allow-preview")
		}
	}
	if !accepted || !rejected || !grant {
		t.Fatalf("missing routing evidence accepted=%v rejected=%v grant=%v", accepted, rejected, grant)
	}
}
func TestSharedAuthIsIdenticalForRESTAndMCP(t *testing.T) {
	token := strings.Repeat("a", 32)
	h, _ := handler(token)
	for _, path := range []string{"/api/v1/resources?kind=Secret", "/api/v1/overview", "/api/v1/connection", "/api/v1/stream", "/mcp"} {
		if got := perform(h, "GET", path, "", "").Code; got != 401 {
			t.Fatalf("%s unauthenticated: %d", path, got)
		}
	}
	if perform(h, "GET", "/api/v1/overview", "", token).Code != 200 {
		t.Fatal("bearer rejected")
	}
	w := perform(h, "POST", "/api/v1/session", fmt.Sprintf(`{"token":%q}`, token), "")
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatal("login failed")
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("insecure cookie")
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:8080/api/v1/overview", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("cookie authentication failed")
	}
}
func TestHostAndCrossOriginProtection(t *testing.T) {
	h, _ := handler("")
	for _, tc := range []struct{ host, origin, site string }{{"evil.invalid:8080", "", ""}, {"127.0.0.1:8080", "https://evil.invalid", ""}, {"127.0.0.1:8080", "", "cross-site"}, {"127.0.0.1:8080", "null", ""}} {
		r := httptest.NewRequest("GET", "http://"+tc.host+"/api/v1/resources", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("accepted cross-origin request %+v: %d", tc, w.Code)
		}
	}
}
func TestHTTPValidation(t *testing.T) {
	h, _ := handler("")
	for _, path := range []string{"/api/v1/resources?limit=0", "/api/v1/resources?offset=-1", "/api/v1/resources?limit=201", "/api/v1/resources?kind=Shell", "/api/v1/resources?namespace=../production", "/api/v1/pods/production/api/logs?tail=1001", "/api/v1/resources/Node/production/worker"} {
		if got := perform(h, "GET", path, "", "").Code; got != 400 {
			t.Fatalf("%s returned %d", path, got)
		}
	}
	if perform(h, "DELETE", "/api/v1/resources/Pod/production/storefront", "", "").Code != 405 {
		t.Fatal("write route exposed")
	}
	w := perform(h, "POST", "/api/v1/session", `{"token":"`+strings.Repeat("a", 70<<10)+`"}`, "")
	if w.Code != 400 {
		t.Fatalf("oversized body accepted %d", w.Code)
	}
}
func TestMCPStreamableHTTPInitialize(t *testing.T) {
	token := strings.Repeat("a", 32)
	h, _ := handler(token)
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"serverInfo"`) {
		t.Fatalf("MCP initialization failed %d %s", w.Code, w.Body.String())
	}
}

type streamWriter struct {
	header http.Header
	writes chan string
	once   sync.Once
}

func (w *streamWriter) Header() http.Header         { return w.header }
func (w *streamWriter) WriteHeader(int)             {}
func (w *streamWriter) Write(b []byte) (int, error) { w.writes <- string(b); return len(b), nil }
func (w *streamWriter) Flush()                      {}
func TestSSEInvalidationAndCancellation(t *testing.T) {
	h, b := handler("")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("GET", "http://127.0.0.1:8080/api/v1/stream", nil).WithContext(ctx)
	w := &streamWriter{header: make(http.Header), writes: make(chan string, 8)}
	done := make(chan struct{})
	go func() { h.ServeHTTP(w, r); close(done) }()
	select {
	case msg := <-w.writes:
		if !strings.Contains(msg, "event: invalidate") {
			t.Fatal(msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no initial synchronization")
	}
	b.Notify()
	select {
	case msg := <-w.writes:
		if !strings.Contains(msg, `"revision":1`) {
			t.Fatal(msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no change notification")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("subscriber leaked")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.subs) != 0 {
		t.Fatal("subscription not removed")
	}
}
func TestDeepResourceRoutesServeEmbeddedApp(t *testing.T) {
	h, _ := handler("")
	w := perform(h, "GET", "/resources/Deployment/production/app.v2", "", "")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("deep link failed %d", w.Code)
	}
}

type unauthorizedKubernetes struct{ provider.Provider }

func (p unauthorizedKubernetes) List(context.Context, string, string) ([]model.Object, bool, error) {
	return nil, false, &provider.Error{Code: 401, Message: "Kubernetes authentication failed"}
}
func TestKubernetesAndTimoneAuthenticationFailuresHaveDifferentChallenges(t *testing.T) {
	token := strings.Repeat("a", 32)
	h := New(service.New(unauthorizedKubernetes{provider.NewDemo()}), config.Config{Listen: "127.0.0.1:8080", Token: token}, nil, NewBroker())
	w := perform(h, "GET", "/api/v1/resources?kind=Pod", "", token)
	if w.Code != 401 || w.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("upstream auth failure incorrectly challenged Timone login")
	}
	w = perform(h, "GET", "/api/v1/resources?kind=Pod", "", "")
	if w.Code != 401 || w.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatal("Timone authentication challenge missing")
	}
}

func TestEventTimeValidationAndExactResourceFilters(t *testing.T) {
	h, _ := handler("")
	for _, path := range []string{"/api/v1/events?since=bad", "/api/v1/events?resource=../escape", "/api/v1/events?since=2026-10-15T12:00:00Z&until=2026-10-14T12:00:00Z"} {
		if w := perform(h, "GET", path, "", ""); w.Code != 400 {
			t.Fatalf("invalid filter accepted %s: %d", path, w.Code)
		}
	}
	w := perform(h, "GET", "/api/v1/events?kind=Job&resource=database-backup-29183040", "", "")
	var body struct {
		Items []model.Event
		Total int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || body.Total != 1 || len(body.Items) != 1 || body.Items[0].Kind != "Job" {
		t.Fatalf("filters ignored: %s", w.Body.String())
	}
}

func TestHelmEndpointsUseSharedAuthenticationAndValidateRevisions(t *testing.T) {
	token := strings.Repeat("a", 32)
	h, _ := handler(token)
	paths := []string{"/api/v1/helm/releases/production/checkout-api", "/api/v1/helm/releases/production/checkout-api/compare?from=1&to=3"}
	for _, path := range paths {
		if w := perform(h, "GET", path, "", ""); w.Code != 401 {
			t.Fatalf("anonymous Helm access %d", w.Code)
		}
		w := perform(h, "GET", path, "", token)
		if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("Helm endpoint failed %d %s", w.Code, w.Body.String())
		}
		if w := perform(h, "POST", path, "", token); w.Code < 400 {
			t.Fatal("Helm write route accepted")
		}
	}
	for _, suffix := range []string{"?from=x&to=3", "?from=1&to=1", "?from=0&to=3"} {
		if w := perform(h, "GET", paths[0]+"/compare"+suffix, "", token); w.Code != 400 {
			t.Fatalf("invalid revision accepted %s", suffix)
		}
	}
}

func TestApplicationsAndObservedChangesUseProtectedValidatedReads(t *testing.T) {
	token := strings.Repeat("v", 32)
	h, _ := handler(token)
	for _, path := range []string{"/api/v1/applications?namespace=production&search=checkout", "/api/v1/changes?namespace=production&kind=Pod&limit=2"} {
		if w := perform(h, "GET", path, "", ""); w.Code != 401 {
			t.Fatal("anonymous access", w.Code)
		}
		if w := perform(h, "GET", path, "", token); w.Code != 200 || !json.Valid(w.Body.Bytes()) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/changes?since=invalid", "/api/v1/changes?limit=201", "/api/v1/applications?offset=-1", "/api/v1/applications?kind=Secret"} {
		if w := perform(h, "GET", path, "", token); w.Code != 400 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
}

func TestFocusedReviewAndDriftRoutesRequireAuthenticationAndValidateInputs(t *testing.T) {
	token := strings.Repeat("r", 32)
	h, _ := handler(token)
	for _, path := range []string{"/api/v1/workspace", "/api/v1/authorization?kind=Pod&namespace=production&verb=get", "/api/v1/gitops/Application/platform/checkout-api/drift"} {
		if w := perform(h, "GET", path, "", ""); w.Code != 401 {
			t.Fatal("anonymous read", path, w.Code)
		}
		if w := perform(h, "GET", path, "", token); w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/authorization?kind=Pod&kind=Secret&namespace=production&verb=get", "/api/v1/authorization?kind=Pod&namespace=production&verb=exec", "/api/v1/authorization?kind=Pod&namespace=production&verb=get&url=https://example.invalid", "/api/v1/gitops/Pod/production/app/drift"} {
		if w := perform(h, "GET", path, "", token); w.Code != 400 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
}
