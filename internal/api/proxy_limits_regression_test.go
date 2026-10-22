package api

import (
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
	"timone/internal/config"
	"timone/internal/provider"
	"timone/internal/service"
)

func TestLoopbackProxyAcceptsPublicOrigin(t *testing.T) {
	token := strings.Repeat("x", 32)
	c := config.Config{Listen: "127.0.0.1:8080", Mode: "local", AuthMode: "proxy", Token: token, PublicURL: "https://timone.example.org", TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}
	handler := New(service.New(provider.NewDemo()), c, nil, NewBroker())
	for _, tc := range []struct {
		host, peer, origin, token string
		want                      int
	}{
		{"timone.example.org", "127.0.0.1:1234", "https://timone.example.org", token, 200},
		{"attacker.example", "127.0.0.1:1234", "", token, 403},
		{"127.0.0.1:8080", "127.0.0.1:1234", "", token, 403},
		{"timone.example.org", "192.0.2.1:1234", "", token, 401},
		{"timone.example.org", "127.0.0.1:1234", "https://attacker.example", token, 403},
		{"timone.example.org", "127.0.0.1:1234", "", "invalid", 401},
	} {
		host := tc.host
		req := httptest.NewRequest("GET", "http://"+host+"/api/v1/cluster", nil)
		req.RemoteAddr = tc.peer
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("X-Forwarded-Host", "timone.example.org")
		req.Header.Set("X-Timone-Proxy-Token", tc.token)
		req.Header.Set("X-Forwarded-User", "operator")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != tc.want {
			t.Errorf("proxy host policy host=%s returns %d: %s", host, res.Code, res.Body.String())
		}
	}
}

func TestThrottledPeerCannotExhaustGlobalQuota(t *testing.T) {
	var limiter loginLimiter
	now := time.Now()
	bad := httptest.NewRequest("POST", "http://127.0.0.1/api/v1/session", nil)
	bad.RemoteAddr = "192.0.2.1:1234"
	accepted := 0
	for i := 0; i < 2000; i++ {
		if limiter.allow(bad, nil, now) {
			accepted++
		}
	}
	if accepted != 20 {
		t.Fatalf("per-peer budget changed: accepted %d attempts", accepted)
	}
	good := httptest.NewRequest("POST", "http://127.0.0.1/api/v1/session", nil)
	good.RemoteAddr = "192.0.2.2:4321"
	if !limiter.allow(good, nil, now) {
		t.Errorf("a peer with only %d admitted attempts exhausted the global quota using 1980 already-throttled requests; unrelated peer denied", accepted)
	}
}
