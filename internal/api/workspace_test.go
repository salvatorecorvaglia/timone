package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"timone/internal/config"
	"timone/internal/model"
	"timone/internal/provider"
	"timone/internal/service"
)

type switchingProvider struct {
	provider.Provider
	name, scope string
	stopped     chan struct{}
	entered     chan struct{}
}

func (p switchingProvider) Name() string                             { return p.name }
func (p switchingProvider) Namespace() string                        { return p.scope }
func (p switchingProvider) Watch(ctx context.Context, notify func()) { <-ctx.Done(); close(p.stopped) }
func (p switchingProvider) List(ctx context.Context, kind, ns string) ([]model.Object, bool, error) {
	if p.entered != nil && kind == "Pod" {
		close(p.entered)
		<-ctx.Done()
		return nil, false, ctx.Err()
	}
	return p.Provider.List(ctx, kind, ns)
}
func TestRuntimeSwitchCancelsOldRequestsWatchersAndPreservesSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contexts.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\ncontexts:\n- name: one\n  context: {}\n- name: two\n  context: {}\ncurrent-context: one\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old := switchingProvider{Provider: provider.NewDemo(), name: "one", scope: "production", stopped: make(chan struct{}), entered: make(chan struct{})}
	initial := service.New(old)
	c := config.Config{Mode: "local", Listen: "127.0.0.1:8080", Token: strings.Repeat("a", 32), Kubeconfig: path}
	nextStopped := make(chan struct{})
	rt := NewRuntime(t.Context(), initial, c, nil, NewBroker(), slog.New(slog.NewTextHandler(io.Discard, nil)), func(c config.Config) (*service.Service, error) {
		if c.Context != "two" || c.Namespace != "" || !c.NamespaceExplicit {
			t.Error("wrong switch configuration")
		}
		return service.New(switchingProvider{Provider: provider.NewDemo(), name: c.Context, scope: c.Namespace, stopped: nextStopped}), nil
	})
	defer rt.Close()
	login := perform(rt, "POST", "/api/v1/session", `{"token":"`+c.Token+`"}`, "")
	cookies := login.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal(login.Body.String())
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
		r.AddCookie(cookies[0])
		w := httptest.NewRecorder()
		rt.ServeHTTP(w, r)
		return w
	}
	oldDone := make(chan struct{})
	go func() { defer close(oldDone); request("GET", "/api/v1/resources?kind=Pod&namespace=production", "") }()
	select {
	case <-old.entered:
	case <-time.After(time.Second):
		t.Fatal("request did not enter old provider")
	}
	switched := request("POST", "/api/v1/workspace", `{"context":"two","namespace":"","revision":1,"confirm":true}`)
	if switched.Code != 200 || !strings.Contains(switched.Body.String(), `"revision":2`) {
		t.Fatal(switched.Code, switched.Body.String())
	}
	for _, done := range []chan struct{}{oldDone, old.stopped} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("old workspace leaked")
		}
	}
	if rt.state.service == initial || request("GET", "/api/v1/cluster", "").Code != 200 {
		t.Fatal("service or session not isolated")
	}
	if w := request("POST", "/api/v1/workspace", `{"context":"two","namespace":"","revision":1,"confirm":true}`); w.Code != 409 {
		t.Fatal(w.Code)
	}
}
func TestRuntimeRejectsUnconfirmedUnknownAndSharedSwitches(t *testing.T) {
	for _, c := range []config.Config{{Mode: "demo", Listen: "127.0.0.1:8080"}, {Mode: "local", Listen: "0.0.0.0:8080", Token: strings.Repeat("b", 32)}} {
		rt := NewRuntime(t.Context(), service.New(provider.NewDemo()), c, nil, NewBroker(), slog.Default(), func(config.Config) (*service.Service, error) { return nil, errors.New("must not run") })
		if w := perform(rt, http.MethodPost, "/api/v1/workspace", `{"context":"two","confirm":true,"revision":1}`, c.Token); w.Code != 403 {
			t.Fatal(w.Code, w.Body.String())
		}
		rt.Close()
	}
}

func TestFailedWorkspaceSwitchKeepsCurrentStateAndRejectsOrigin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contexts.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\ncontexts:\n- name: one\n  context: {}\ncurrent-context: one\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	p := switchingProvider{Provider: provider.NewDemo(), name: "one", stopped: stopped}
	c := config.Config{Mode: "local", Listen: "127.0.0.1:8080", Kubeconfig: path}
	rt := NewRuntime(t.Context(), service.New(p), c, nil, NewBroker(), slog.Default(), func(config.Config) (*service.Service, error) { return nil, errors.New("private-configuration") })
	defer rt.Close()
	for _, tc := range []struct {
		body string
		code int
	}{{`{"context":"one","revision":1}`, 400}, {`{"context":"unknown","namespace":"","revision":1,"confirm":true}`, 400}, {`{"context":"one","namespace":"","revision":1,"confirm":true}`, 503}} {
		response := perform(rt, "POST", "/api/v1/workspace", tc.body, "")
		if response.Code != tc.code || strings.Contains(response.Body.String(), "private-configuration") {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if rt.state.revision != 1 || rt.state.ctx.Err() != nil {
		t.Fatal("failed switch changed active workspace")
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/v1/workspace", strings.NewReader(`{"context":"one","namespace":"","revision":1,"confirm":true}`))
	r.Header.Set("Origin", "https://attacker.invalid")
	w := httptest.NewRecorder()
	rt.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestRetiredWorkspaceIsReleasedWithoutAnotherSwitch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contexts.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\ncontexts:\n- name: one\n  context: {}\n- name: two\n  context: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime(t.Context(), service.New(provider.NewDemo()), config.Config{Mode: "local", Listen: "127.0.0.1:8080", Kubeconfig: path}, nil, NewBroker(), slog.New(slog.NewTextHandler(io.Discard, nil)), func(config.Config) (*service.Service, error) { return service.New(provider.NewDemo()), nil })
	defer rt.Close()
	w := perform(rt, "POST", "/api/v1/workspace", `{"context":"two","namespace":"","revision":1,"confirm":true}`, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for {
		rt.switching.Lock()
		empty := len(rt.retired) == 0
		rt.switching.Unlock()
		if empty {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completed retirement remains retained")
		}
		time.Sleep(time.Millisecond)
	}
}
