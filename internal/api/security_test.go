package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
	"timone/internal/config"
	"timone/internal/provider"
	"timone/internal/service"
)

func TestLogoutRevokesSessionAndLoginRejectsTrailingJSON(t *testing.T) {
	token := strings.Repeat("a", 32)
	h, _ := handler(token)
	if w := perform(h, "POST", "/api/v1/session", `{"token":"`+token+`"}{}`, ""); w.Code != 400 {
		t.Fatal("trailing JSON accepted")
	}
	w := perform(h, "POST", "/api/v1/session", `{"token":"`+token+`"}`, "")
	cookies := w.Result().Cookies()
	if w.Code != 200 || len(cookies) != 1 || cookies[0].Value == token {
		t.Fatal("unsafe login cookie")
	}
	r := httptest.NewRequest("DELETE", "http://127.0.0.1:8080/api/v1/session", nil)
	r.AddCookie(cookies[0])
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	r = httptest.NewRequest("GET", "http://127.0.0.1:8080/api/v1/overview", nil)
	r.AddCookie(cookies[0])
	out = httptest.NewRecorder()
	h.ServeHTTP(out, r)
	if out.Code != 401 {
		t.Fatal("logged-out cookie could be replayed")
	}
}
func TestProxyGuardsRESTMCPAndSessionWithoutTokenLoginBypass(t *testing.T) {
	token := strings.Repeat("a", 32)
	c := config.Config{Mode: "demo", Listen: "0.0.0.0:8080", AuthMode: "proxy", Token: token, PublicURL: "https://timone.example", TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}}
	h := New(service.New(provider.NewDemo()), c, nil, NewBroker())
	for _, path := range []string{"/api/v1/overview", "/mcp"} {
		r := httptest.NewRequest("GET", "https://timone.example"+path, nil)
		r.RemoteAddr = "10.0.0.2:4000"
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 || w.Header().Get("X-Timone-Auth-Required") != "proxy" {
			t.Fatal("Bearer token bypassed proxy identity")
		}
	}
	r := httptest.NewRequest("GET", "https://timone.example/api/v1/session", nil)
	r.RemoteAddr = "10.0.0.2:4000"
	r.Header.Set("X-Timone-Proxy-Token", token)
	r.Header.Set("X-Forwarded-User", "alice")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"alice"`) {
		t.Fatal("proxy identity missing from session")
	}
	r = httptest.NewRequest("POST", "https://timone.example/api/v1/session", strings.NewReader(`{"token":"`+token+`"}`))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 405 {
		t.Fatal("proxy mode exposed token login")
	}
}
func TestAccessLogsContainAttributionWithoutSensitiveRequestDetails(t *testing.T) {
	var logs bytes.Buffer
	token := strings.Repeat("a", 32)
	h := NewWithLogger(service.New(provider.NewDemo()), config.Config{Mode: "demo", Listen: "127.0.0.1:8080", Token: token, AccessLog: true}, nil, NewBroker(), slog.New(slog.NewJSONHandler(&logs, nil)))
	w := perform(h, "GET", "/api/v1/resources/Secret/production/checkout-db?private=sensitive-marker", "", token)
	if w.Code != 200 || w.Header().Get("X-Request-ID") == "" {
		t.Fatal("request failed or missing correlation id")
	}
	text := logs.String()
	if strings.Contains(text, token) || strings.Contains(text, "checkout-db") || strings.Contains(text, "sensitive-marker") {
		t.Fatalf("sensitive request details logged: %s", text)
	}
	var record map[string]any
	if json.Unmarshal(logs.Bytes(), &record) != nil || record["actor"] != "administrator" || record["route"] != "GET /api/v1/resources/{kind}/{namespace}/{name}" {
		t.Fatalf("missing safe attribution %s", text)
	}
}
func TestReadinessRemainsAvailableWhenKubernetesCannotBeRead(t *testing.T) {
	h := New(service.New(unauthorizedKubernetes{provider.NewDemo()}), config.Config{Mode: "local", Listen: "127.0.0.1:8080"}, nil, NewBroker())
	if w := perform(h, "GET", "/readyz", "", ""); w.Code != 200 {
		t.Fatal("Kubernetes failure removed diagnostic UI readiness")
	}
	if w := perform(h, "GET", "/api/v1/resources?kind=Pod", "", ""); w.Code != 401 {
		t.Fatal("upstream failure hidden")
	}
}
func TestPublicHostAndOriginMustMatchInSharedDeployments(t *testing.T) {
	h := New(service.New(provider.NewDemo()), config.Config{Mode: "demo", Listen: "0.0.0.0:8080", Token: strings.Repeat("a", 32), PublicURL: "https://timone.example"}, nil, NewBroker())
	r := httptest.NewRequest("GET", "https://attacker.example/api/v1/session", nil)
	r.Header.Set("X-Forwarded-Host", "timone.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("forwarded host bypassed configured public authority")
	}
}

func TestSSERevocationClosesSubscriptionOnNextNotification(t *testing.T) {
	token := strings.Repeat("a", 32)
	h, b := handler(token)
	login := perform(h, "POST", "/api/v1/session", `{"token":"`+token+`"}`, "")
	cookie := login.Result().Cookies()[0]
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("GET", "http://127.0.0.1:8080/api/v1/stream", nil).WithContext(ctx)
	r.AddCookie(cookie)
	w := &streamWriter{header: make(http.Header), writes: make(chan string, 8)}
	done := make(chan struct{})
	go func() { h.ServeHTTP(w, r); close(done) }()
	select {
	case <-w.writes:
	case <-time.After(time.Second):
		t.Fatal("stream not initialized")
	}
	logout := httptest.NewRequest("DELETE", "http://127.0.0.1:8080/api/v1/session", nil)
	logout.AddCookie(cookie)
	h.ServeHTTP(httptest.NewRecorder(), logout)
	b.Notify()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("revoked session kept its stream")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.subs) != 0 {
		t.Fatal("revoked stream subscription leaked")
	}
}
