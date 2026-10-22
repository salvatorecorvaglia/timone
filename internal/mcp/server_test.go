package mcp

import (
	"context"
	"encoding/json"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
	"timone/internal/service"
)

func TestToolsUseSharedServiceAndValidateInputs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server := New(service.New(provider.NewDemo()))
	a, b := sdk.NewInMemoryTransports()
	ss, e := server.Connect(ctx, a, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ss.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "timone-test", Version: "1"}, nil)
	cs, e := client.Connect(ctx, b, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cs.Close()
	tools, e := cs.ListTools(ctx, nil)
	if e != nil || len(tools.Tools) != 23 {
		t.Fatalf("tool discovery %v %+v", e, tools)
	}
	for _, tool := range tools.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("tool not declared read only: %s", tool.Name)
		}
	}
	type toolCase struct {
		name     string
		args     map[string]any
		contains string
	}
	tests := []toolCase{{"list_clusters", map[string]any{}, "demo"}, {"get_cluster_overview", map[string]any{}, "checkout-api"}, {"list_resources", map[string]any{"kind": "Pod", "namespace": "production", "limit": 1}, `"limit":1`}, {"get_resource_details", map[string]any{"kind": "Secret", "namespace": "production", "name": "checkout-db"}, "redaction"}, {"get_pod_logs", map[string]any{"namespace": "production", "pod": "checkout-api-7c8d9f-m4n7q"}, "SIMULATED"}, {"list_events", map[string]any{}, "FailedScheduling"}, {"list_issues", map[string]any{}, "hypotheses"}, {"get_resource_topology", map[string]any{}, "declared"}, {"get_resource_metrics", map[string]any{}, "cpuLimits"}, {"get_audit_findings", map[string]any{}, "mutable-image"}}
	tests = append(tests,
		toolCase{"list_observed_changes", map[string]any{"namespace": "production"}, "Restart clears history"},
		toolCase{"list_applications", map[string]any{"namespace": "production", "search": "checkout"}, "Controller ownership"},
		toolCase{"get_issue_details", map[string]any{"id": "issue-databases.platform.timone.io:production:orders-db", "namespace": "production"}, "Custom resource reports not ready"},
		toolCase{"get_service_account_grants", map[string]any{"namespace": "production", "name": "default"}, "system:authenticated"},
		toolCase{"search_resources", map[string]any{"search": "orders-db"}, "databases.platform.timone.io"},
		toolCase{"get_metrics_history_status", map[string]any{}, "simulated"},
		toolCase{"get_resource_metrics_history", map[string]any{"kind": "Pod", "namespace": "production", "name": "checkout-api-7c8d9f-m4n7q", "range": "1h"}, "points"},
		toolCase{"list_custom_resource_types", map[string]any{}, "databases.platform.timone.io"},
		toolCase{"list_custom_resources", map[string]any{"definition": "databases.platform.timone.io", "namespace": "production", "limit": 1}, `"limit":1`},
		toolCase{"get_custom_resource_details", map[string]any{"definition": "databases.platform.timone.io", "namespace": "production", "name": "orders-db"}, "Safe projection only"},
		toolCase{"list_integrations", map[string]any{}, "DriftDetected"},
		toolCase{"get_helm_history", map[string]any{"namespace": "production", "name": "checkout-api"}, "chartVersion"},
		toolCase{"compare_helm_revisions", map[string]any{"namespace": "production", "name": "checkout-api", "from": 1, "to": 3}, "inventoryComparable"},
	)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, e := cs.CallTool(ctx, &sdk.CallToolParams{Name: tc.name, Arguments: tc.args})
			if e != nil || r.IsError {
				t.Fatalf("tool failed %v %+v", e, r)
			}
			text := ""
			for _, c := range r.Content {
				if v, ok := c.(*sdk.TextContent); ok {
					text += v.Text
				}
			}
			if tc.name == "list_issues" {
				tc.contains = "evidence"
			}
			if !strings.Contains(text, tc.contains) {
				t.Fatalf("missing %q in %s", tc.contains, text)
			}
		})
	}
	for _, args := range []map[string]any{{"kind": "Pod", "limit": 10000}, {"kind": "../secrets"}, {"kind": "Pod", "namespace": "other/namespace"}, {"kind": "Pod", "limit": "all"}} {
		r, e := cs.CallTool(ctx, &sdk.CallToolParams{Name: "list_resources", Arguments: args})
		if e == nil && !r.IsError {
			t.Fatalf("invalid input accepted %+v", args)
		}
	}
	grouped, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "get_resource_topology", Arguments: map[string]any{"group": "workloads", "namespace": "production"}})
	if err != nil || grouped.IsError {
		t.Fatal("MCP topology group rejected", err)
	}
	var graph model.Graph
	if len(grouped.Content) == 0 || json.Unmarshal([]byte(grouped.Content[0].(*sdk.TextContent).Text), &graph) != nil || len(graph.Nodes) == 0 {
		t.Fatal("invalid grouped graph")
	}
	for _, r := range graph.Nodes {
		if r.Kind == "ConfigMap" || r.Kind == "Service" || r.Namespace != "production" {
			t.Fatal("MCP group did not filter", r.ID)
		}
	}
	invalidTopology, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "get_resource_topology", Arguments: map[string]any{"group": "unknown"}})
	if err == nil && !invalidTopology.IsError {
		t.Fatal("MCP accepted invalid group")
	}
	for _, args := range []map[string]any{{"search": "x"}, {"search": "orders", "kind": "Pod", "definition": "databases.platform.timone.io"}, {"search": "orders", "limit": 201}, {"search": "orders", "offset": -1}, {"search": "orders", "namespace": "../other"}} {
		r, e := cs.CallTool(ctx, &sdk.CallToolParams{Name: "search_resources", Arguments: args})
		if e == nil && !r.IsError {
			t.Fatalf("invalid search input accepted %+v", args)
		}
	}
	for _, args := range []map[string]any{{}, {"namespace": "production"}, {"namespace": "../production", "name": "default"}, {"namespace": "production", "name": "default", "limit": 201}, {"namespace": "production", "name": "default", "offset": -1}} {
		r, e := cs.CallTool(ctx, &sdk.CallToolParams{Name: "get_service_account_grants", Arguments: args})
		if e == nil && !r.IsError {
			t.Fatalf("invalid grant input accepted %+v", args)
		}
	}
	badComparison, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "compare_helm_revisions", Arguments: map[string]any{"namespace": "production", "name": "checkout-api"}})
	if err == nil && !badComparison.IsError {
		t.Fatal("missing required comparison revisions accepted")
	}
	for _, args := range []map[string]any{{"kind": "Node", "name": "harbor-worker-01", "range": "7d"}, {"kind": "Secret", "name": "checkout-db", "namespace": "production"}, {"kind": "Pod", "name": "checkout-api-7c8d9f-m4n7q", "namespace": "production", "container": "missing"}, {"kind": "Node"}} {
		r, e := cs.CallTool(ctx, &sdk.CallToolParams{Name: "get_resource_metrics_history", Arguments: args})
		if e == nil && !r.IsError {
			t.Fatalf("invalid metrics history input accepted %+v", args)
		}
	}
	for _, args := range []map[string]any{{}, {"definition": "../secrets"}, {"definition": "databases.platform.timone.io", "version": "v999"}, {"definition": "databases.platform.timone.io", "limit": 201}} {
		r, e := cs.CallTool(ctx, &sdk.CallToolParams{Name: "list_custom_resources", Arguments: args})
		if e == nil && !r.IsError {
			t.Fatalf("custom tool accepted invalid input %+v", args)
		}
	}
	filtered, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "list_events", Arguments: map[string]any{"kind": "Job", "resource": "database-backup-29183040"}})
	if err != nil || filtered.IsError {
		t.Fatalf("event filters failed %v %+v", err, filtered)
	}
	invalid, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "list_events", Arguments: map[string]any{"since": "yesterday"}})
	if err == nil && !invalid.IsError {
		t.Fatal("MCP accepted invalid event time")
	}
	r, e := cs.CallTool(ctx, &sdk.CallToolParams{Name: "get_issue_details", Arguments: map[string]any{"id": "issue-Deployment:production:checkout-api"}})
	if e != nil || r.IsError {
		t.Fatalf("issue details failed %v %+v", e, r)
	}
}

func TestDiagnosticToolsExposeTheSameConfiguredPolicy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	policy := model.DiagnosticPolicy{StartupGraceSeconds: 600, WarningEventWindowSeconds: 60, RestartThreshold: 12}
	s, err := service.NewWithDiagnosticPolicy(provider.NewDemo(), nil, policy)
	if err != nil {
		t.Fatal(err)
	}
	a, b := sdk.NewInMemoryTransports()
	server, err := New(s).Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := sdk.NewClient(&sdk.Implementation{Name: "policy-test", Version: "1"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, name := range []string{"get_cluster_overview", "list_issues", "get_issue_details"} {
		args := map[string]any{"namespace": "production"}
		if name == "get_issue_details" {
			args["id"] = "issue-Deployment:production:checkout-api"
		}
		result, err := client.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError {
			t.Fatal(name, result, err)
		}
		var body struct {
			Policy model.DiagnosticPolicy `json:"diagnosticPolicy"`
		}
		found := false
		for _, c := range result.Content {
			if text, ok := c.(*sdk.TextContent); ok {
				if err := json.Unmarshal([]byte(text.Text), &body); err != nil {
					t.Fatal(err)
				}
				found = body.Policy == policy
			}
		}
		if !found {
			t.Fatal("configured diagnostic policy missing from tool", name, body)
		}
	}
}

type unprojectedProvider struct{ provider.Provider }

func (p unprojectedProvider) Get(_ context.Context, kind, ns, name string) (model.Object, error) {
	return model.Object{"apiVersion": "argoproj.io/v1alpha1", "kind": kind, "metadata": map[string]any{"name": name, "namespace": ns, "annotations": map[string]any{"token": "private-marker"}}, "spec": map[string]any{"source": map[string]any{"targetRevision": "main", "helm": map[string]any{"values": "private-marker"}}}}, nil
}
func TestMCPDetailsProjectCredentialsAndShareAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	s := service.New(unprojectedProvider{provider.NewDemo()})
	a, b := sdk.NewInMemoryTransports()
	server, err := New(s).Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := sdk.NewClient(&sdk.Implementation{Name: "projection-test", Version: "1"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	params := &sdk.CallToolParams{Name: "get_resource_details", Arguments: map[string]any{"kind": "Application", "namespace": "production", "name": "app"}}
	result, err := client.CallTool(ctx, params)
	if err != nil || result.IsError {
		t.Fatal(result, err)
	}
	data, err := json.Marshal(result)
	if err != nil || strings.Contains(string(data), "private-marker") || !strings.Contains(string(data), "main") {
		t.Fatal(string(data), err)
	}
	var releases []func()
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	for range 8 {
		release, ok := s.TryAdmit()
		if !ok {
			t.Fatal("early rejection")
		}
		releases = append(releases, release)
	}
	result, err = client.CallTool(ctx, params)
	if err != nil || !result.IsError {
		t.Fatal("MCP bypassed shared admission", result, err)
	}
}
