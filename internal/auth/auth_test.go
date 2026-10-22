package auth

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requestCookie(id string) *http.Request {
	r := httptest.NewRequest("GET", "http://localhost/", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: id})
	return r
}
func TestSessionsAreOpaqueRevocableAndExpireServerSide(t *testing.T) {
	token := strings.Repeat("a", 32)
	m := New(Options{Token: token})
	now := time.Now()
	m.now = func() time.Time { return now }
	id, err := m.Login(token)
	if err != nil || id == token || len(id) != 43 {
		t.Fatalf("unsafe session %v", err)
	}
	r := requestCookie(id)
	identity, err := m.Authenticate(r)
	if err != nil || identity.Method != "session" {
		t.Fatal("session rejected")
	}
	m.Revoke(r)
	identity, _ = m.Authenticate(r)
	if identity.Name != "" {
		t.Fatal("logged-out cookie still accepted")
	}
	id, _ = m.Login(token)
	now = now.Add(Lifetime)
	identity, _ = m.Authenticate(requestCookie(id))
	if identity.Name != "" {
		t.Fatal("server accepted expired session")
	}
}
func TestRotatingTokenFileRevokesOldCredentialsAndNeverFallsBackToAnonymous(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	old, newToken := strings.Repeat("a", 32), strings.Repeat("b", 32)
	if err := os.WriteFile(path, []byte(old+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m := New(Options{TokenFile: path, Anonymous: true})
	id, _ := m.Login(old)
	if err := os.WriteFile(path, []byte(newToken), 0600); err != nil {
		t.Fatal(err)
	}
	identity, err := m.Authenticate(requestCookie(id))
	if err != nil || identity.Name != "" {
		t.Fatal("rotation did not revoke session")
	}
	r := httptest.NewRequest("GET", "http://localhost/", nil)
	r.Header.Set("Authorization", "Bearer "+old)
	identity, _ = m.Authenticate(r)
	if identity.Name != "" {
		t.Fatal("old token survived rotation")
	}
	r.Header.Set("Authorization", "Bearer "+newToken)
	identity, err = m.Authenticate(r)
	if err != nil || identity.Name == "" {
		t.Fatal("new token rejected")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Authenticate(r); err == nil || m.Ready() {
		t.Fatal("missing credential file failed open")
	}
}
func TestBearerCredentialsAreExplicitAndDuplicateCredentialsAreRejected(t *testing.T) {
	token := strings.Repeat("a", 32)
	m := New(Options{Token: token})
	id, _ := m.Login(token)
	for _, header := range []string{token, "Basic " + token, "Bearer " + token + " extra"} {
		r := requestCookie(id)
		r.Header.Set("Authorization", header)
		identity, _ := m.Authenticate(r)
		if identity.Name != "" {
			t.Fatalf("ambiguous credential accepted %q", header[:5])
		}
	}
	r := requestCookie(id)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: id})
	identity, _ := m.Authenticate(r)
	if identity.Name != "" {
		t.Fatal("duplicate sessions accepted")
	}
	r = requestCookie(id)
	r.Header.Add("Authorization", "Bearer "+token)
	r.Header.Add("Authorization", "Bearer "+token)
	identity, _ = m.Authenticate(r)
	if identity.Name != "" {
		t.Fatal("duplicate authorization accepted")
	}
}
func TestProxyIdentityNeedsTrustedPeerSecretAndSingleIdentity(t *testing.T) {
	token := strings.Repeat("a", 32)
	m := New(Options{Mode: "proxy", Token: token, TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}})
	r := httptest.NewRequest("GET", "https://timone.example/", nil)
	r.RemoteAddr = "10.0.0.2:3000"
	r.Header.Set("X-Timone-Proxy-Token", token)
	r.Header.Set("X-Forwarded-User", "alice@example.org")
	id, err := m.Authenticate(r)
	if err != nil || id.Name != "alice@example.org" || id.Method != "proxy" {
		t.Fatal("trusted proxy rejected")
	}
	r.RemoteAddr = "192.0.2.1:3000"
	r.Header.Set("X-Forwarded-For", "10.0.0.2")
	id, _ = m.Authenticate(r)
	if id.Name != "" {
		t.Fatal("spoofed forwarding headers bypassed peer check")
	}
	r.RemoteAddr = "10.0.0.2:3000"
	r.Header.Set("X-Timone-Proxy-Token", "wrong")
	id, _ = m.Authenticate(r)
	if id.Name != "" {
		t.Fatal("proxy secret not required")
	}
	r.Header.Set("X-Timone-Proxy-Token", token)
	r.Header.Add("X-Forwarded-User", "mallory")
	id, _ = m.Authenticate(r)
	if id.Name != "" {
		t.Fatal("duplicate identity accepted")
	}
	if _, err := m.Login(token); err == nil {
		t.Fatal("token login bypassed proxy")
	}
}
func TestSessionStoreAndCredentialFilesAreBounded(t *testing.T) {
	token := strings.Repeat("a", 32)
	m := New(Options{Token: token})
	for i := 0; i < MaxSessions; i++ {
		if _, err := m.Login(token); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Login(token); err == nil {
		t.Fatal("session store is unbounded")
	}
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", MaxTokenBytes+2)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTokenFile(path); err == nil {
		t.Fatal("oversized token file accepted")
	}
}
