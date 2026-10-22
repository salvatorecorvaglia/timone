package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"timone/internal/config"
	imcp "timone/internal/mcp"
	"timone/internal/model"
	"timone/internal/provider"
	"timone/internal/service"
)

func TestRawProbeCredentialsCannotCrossRESTOrMCPBoundary(t *testing.T) {
	const sentinel = "PRIVATE-HTTP-HEADER-SENTINEL"
	pod := model.Object{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": "headers", "namespace": "production"}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app", "readinessProbe": map[string]any{"httpGet": map[string]any{"httpHeaders": []any{map[string]any{"name": "Authorization", "value": "Bearer " + sentinel}}}}}}}}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var out any
		switch r.URL.Path {
		case "/api":
			out = map[string]any{"kind": "APIVersions", "versions": []string{"v1"}}
		case "/apis":
			out = map[string]any{"kind": "APIGroupList", "groups": []any{}}
		case "/api/v1":
			out = map[string]any{"kind": "APIResourceList", "groupVersion": "v1", "resources": []any{map[string]any{"name": "pods", "kind": "Pod", "namespaced": true, "verbs": []string{"get", "list"}}, map[string]any{"name": "events", "kind": "Event", "namespaced": true, "verbs": []string{"get", "list"}}}}
		case "/api/v1/namespaces/production/pods/headers":
			out = pod
		case "/api/v1/namespaces/production/events":
			out = map[string]any{"apiVersion": "v1", "kind": "EventList", "items": []any{}}
		default:
			w.WriteHeader(404)
			out = map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "reason": "NotFound", "code": 404}
		}
		if err := json.NewEncoder(w).Encode(out); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()
	// The temporary kubeconfig trusts only this test server; no real credentials or cluster are used.
	configuration := map[string]any{"apiVersion": "v1", "kind": "Config", "current-context": "test", "clusters": []any{map[string]any{"name": "test", "cluster": map[string]any{"server": upstream.URL, "certificate-authority-data": base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: upstream.Certificate().Raw}))}}}, "contexts": []any{map[string]any{"name": "test", "context": map[string]any{"cluster": "test", "user": "test"}}}, "users": []any{map[string]any{"name": "test", "user": map[string]any{"token": "test-token"}}}}
	data, _ := json.Marshal(configuration)
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := provider.NewCluster(config.Config{Mode: "local", Kubeconfig: path, Namespace: "production"})
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(p)
	h := New(svc, config.Config{Mode: "local", Listen: "127.0.0.1:8080"}, nil, NewBroker())
	assertSafe := func(text string) {
		t.Helper()
		if strings.Contains(text, sentinel) || !strings.Contains(text, "[REDACTED]") || !strings.Contains(text, "Authorization") {
			t.Fatal("unsafe or incomplete credential projection")
		}
	}
	w := perform(h, "GET", "/api/v1/resources/Pod/production/headers", "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	assertSafe(w.Body.String())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, b := sdk.NewInMemoryTransports()
	server, err := imcp.New(svc).Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := sdk.NewClient(&sdk.Implementation{Name: "projection-test", Version: "1"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := client.CallTool(ctx, &sdk.CallToolParams{Name: "get_resource_details", Arguments: map[string]any{"kind": "Pod", "namespace": "production", "name": "headers"}})
	if err != nil || result.IsError {
		t.Fatal("MCP detail failed", err)
	}
	text := ""
	for _, content := range result.Content {
		if item, ok := content.(*sdk.TextContent); ok {
			text += item.Text
		}
	}
	assertSafe(text)
}
