package integrations

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type desiredRoundTrip func(*http.Request) (*http.Response, error)

func (f desiredRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDesiredTransportRejectsRedirectsBoundsResponsesAndCancels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("simulated-rotating-token"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := desiredRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "renderer.example" || r.Header.Get("Authorization") != "Bearer simulated-rotating-token" {
			t.Fatal("unexpected target or credential")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.example"}}, Body: io.NopCloser(strings.NewReader("HIDDEN-UPSTREAM-ERROR"))}, nil
	})
	var out map[string]any
	err := DesiredJSON(context.Background(), "https://renderer.example", "/render", path, "", &out, transport)
	if err == nil || calls != 1 || strings.Contains(err.Error(), "HIDDEN") {
		t.Fatal(err, calls)
	}
	oversized := desiredRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", (4<<20)+1)))}, nil
	})
	if err := DesiredJSON(context.Background(), "https://renderer.example", "/render", "", "", &out, oversized); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatal("oversized source accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = DesiredJSON(ctx, "https://renderer.example", "/render", path, "", &out, transport); err == nil {
		t.Fatal("cancellation ignored")
	}
	for _, options := range []DesiredOptions{{ArgoURL: "http://example"}, {ArgoURL: "https://user:pass@example"}, {RenderedURL: "https://example?token=bad"}, {ArgoTokenFile: "/not/read"}} {
		if options.Validate() == nil {
			t.Fatal("unsafe config accepted")
		}
	}
}
