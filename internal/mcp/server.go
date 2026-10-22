package mcp

import (
	"context"
	"encoding/json"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
	"timone/internal/service"
	"timone/web"
)

type Namespace struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"Optional namespace; configured scope is enforced"`
}
type TopologyInput struct {
	service.Query
	Group string `json:"group,omitempty" jsonschema:"Optional workloads, networking or configuration filter, applied before the node limit"`
}
type ResourceInput struct {
	Kind      string `json:"kind" jsonschema:"Exact supported Kubernetes kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}
type HelmInput struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}
type HelmComparisonInput struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	From      int    `json:"from" jsonschema:"Retained revision, positive and different from to"`
	To        int    `json:"to" jsonschema:"Retained revision, positive and different from from"`
}
type IssueInput struct {
	Namespace string `json:"namespace,omitempty"`
	ID        string `json:"id"`
}
type CustomListInput struct {
	Definition string `json:"definition" jsonschema:"Explicit administrator-enabled CRD name, from list_custom_resource_types"`
	Version    string `json:"version,omitempty" jsonschema:"An exact served version; omitted selects the served storage version when available"`
	Namespace  string `json:"namespace,omitempty"`
	Search     string `json:"search,omitempty"`
	Status     string `json:"status,omitempty"`
	Sort       string `json:"sort,omitempty"`
	Desc       bool   `json:"desc,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Offset     int    `json:"offset,omitempty"`
}
type CustomDetailInput struct {
	Definition string `json:"definition"`
	Version    string `json:"version,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}
type LogsInput struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container,omitempty"`
	Tail      int    `json:"tail,omitempty" jsonschema:"Number of lines between 1 and 1000; defaults to 100"`
}

func result(v any) (*sdk.CallToolResult, any, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, nil, e
	}
	if len(b) > 8<<20 {
		return nil, nil, &provider.Error{Code: 413, Message: "Result too large; narrow your query"}
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}, nil, nil
}
func add[I any](s *service.Service, server *sdk.Server, name, desc string, fn func(context.Context, I) (any, error)) {
	sdk.AddTool(server, &sdk.Tool{Name: name, Description: desc, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, _ *sdk.CallToolRequest, in I) (*sdk.CallToolResult, any, error) {
		release, ok := s.TryAdmit()
		if !ok {
			return nil, nil, &provider.Error{Code: 429, Message: "Investigation capacity reached; retry shortly"}
		}
		defer release()
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		v, e := fn(provider.WithReadBudget(ctx, provider.InvestigationBytes), in)
		if e != nil {
			return nil, nil, e
		}
		return result(v)
	})
}
func New(s *service.Service) *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{Name: "timone", Version: web.Version}, nil)
	add(s, server, "list_clusters", "Return the one configured cluster and explicit simulation mode.", func(ctx context.Context, _ struct{}) (any, error) {
		return []map[string]any{{"name": s.Provider.Name(), "mode": s.Provider.Mode(), "namespace": s.Provider.Namespace()}}, nil
	})
	add(s, server, "get_cluster_overview", "Read current overview; inaccessible collections appear as warnings.", func(ctx context.Context, p Namespace) (any, error) {
		q := service.Query{Namespace: p.Namespace}
		if e := q.Validate(); e != nil {
			return nil, e
		}
		return s.Overview(ctx, p.Namespace)
	})
	add(s, server, "list_resources", "List supported resources with namespace, health, search, sorting and bounded pagination.", func(ctx context.Context, p service.Query) (any, error) { return s.List(ctx, p) })
	add(s, server, "search_resources", "Search names, namespaces, kinds and enabled custom definitions using shared scoped reads. Returns bounded ranked identity/health results, pagination and explicit partial collection coverage; no manifests or secret values.", func(ctx context.Context, p service.SearchQuery) (any, error) { return s.Search(ctx, p) })
	add(s, server, "get_resource_details", "Read a sanitized resource manifest and currently retained related events. Secret payloads, nested annotations, inline GitOps configuration and direct environment values are omitted.", func(ctx context.Context, p ResourceInput) (any, error) {
		return s.Detail(ctx, p.Kind, p.Namespace, p.Name)
	})
	add(s, server, "get_pod_logs", "Read bounded container logs. Logs may contain sensitive application output.", func(ctx context.Context, p LogsInput) (any, error) {
		if p.Tail == 0 {
			p.Tail = 100
		}
		logs, e := s.Logs(ctx, p.Namespace, p.Pod, p.Container, p.Tail)
		return map[string]any{"logs": logs, "mode": s.Provider.Mode()}, e
	})
	add(s, server, "list_applications", "Inspect bounded applications correlated by controller ownership, declared selectors, configuration references and delivery metadata; labels are associations, not traffic dependencies.", func(ctx context.Context, p service.Query) (any, error) { return s.Applications(ctx, p) })
	add(s, server, "list_observed_changes", "List currently authorized resource observations retained in this process for at most one hour; sampling may miss changes and correlation does not establish causes.", func(ctx context.Context, p service.Query) (any, error) { return s.Changes(ctx, p) })
	add(s, server, "list_events", "List retained event groups with exact kind/resource filters and inclusive RFC3339 since/until bounds on the latest occurrence.", func(ctx context.Context, p service.Query) (any, error) { return s.Events(ctx, p) })
	add(s, server, "list_issues", "Deterministic diagnostics for built-in and enabled custom resources, with collection coverage and the active administrator policy; evidence and hypotheses are distinguished.", func(ctx context.Context, p Namespace) (any, error) {
		q := service.Query{Namespace: p.Namespace}
		if e := q.Validate(); e != nil {
			return nil, e
		}
		return s.Issues(ctx, p.Namespace)
	})
	add(s, server, "get_issue_details", "Return one observed issue with related evidence and the administrator policy used to detect it. Custom issue IDs use the CRD definition, namespace and name; unknown or omitted issues return not found.", func(ctx context.Context, p IssueInput) (any, error) {
		if len(p.ID) > 600 {
			return nil, &provider.Error{Code: 400, Message: "Invalid issue ID"}
		}
		q := service.Query{Namespace: p.Namespace}
		if e := q.Validate(); e != nil {
			return nil, e
		}
		return s.Issue(ctx, p.Namespace, p.ID)
	})
	add(s, server, "get_resource_topology", "Return at most 200 nodes and 2000 declared relationships, including partial Gateway configuration checks and separate controller reports; no traffic observation.", func(ctx context.Context, p TopologyInput) (any, error) {
		return s.TopologyWithGroup(ctx, p.Query, p.Group)
	})
	add(s, server, "get_resource_metrics", "Return current metrics with nullable container budgets, configured node request and CPU/memory limit budgets with finite/missing/invalid coverage, permission warnings and explicit unavailability; not scheduler allocation or measured pressure.", func(ctx context.Context, p Namespace) (any, error) { return s.Metrics(ctx, p.Namespace) })
	add(s, server, "get_metrics_history_status", "Inspect explicit Prometheus history configuration without probing an endpoint; configured does not mean healthy.", func(ctx context.Context, _ struct{}) (any, error) { return s.HistoryStatus(), nil })
	add(s, server, "get_resource_metrics_history", "Read bounded CPU/memory history for an authorized current Pod/container or Node. Only 1h/6h/24h windows; never arbitrary PromQL. Missing samples remain unavailable.", func(ctx context.Context, p model.HistoryQuery) (any, error) { return s.MetricHistory(ctx, p) })
	add(s, server, "get_audit_findings", "Read configuration indicators and list checks that could not be performed.", func(ctx context.Context, p Namespace) (any, error) {
		q := service.Query{Namespace: p.Namespace}
		if e := q.Validate(); e != nil {
			return nil, e
		}
		return s.Audit(ctx, p.Namespace)
	})
	add(s, server, "get_service_account_grants", "Explain observed RBAC grants for a freshly authorized ServiceAccount, with binding/role provenance, literal rules, namespace scope, bounded pagination and missing coverage. Not an authorization decision; no impersonation or reviews.", func(ctx context.Context, p service.GrantQuery) (any, error) { return s.ServiceAccountGrants(ctx, p) })
	add(s, server, "list_integrations", "Inspect integration coverage, GitOps conditions and revisions; missing controllers remain unavailable.", func(ctx context.Context, p Namespace) (any, error) { return s.Integrations(ctx, p.Namespace) })
	add(s, server, "get_helm_history", "Read retained Helm revision metadata and resource identities. Requires explicit administrator opt-in and Secret RBAC; never returns values or manifests.", func(ctx context.Context, p HelmInput) (any, error) { return s.HelmHistory(ctx, p.Namespace, p.Name) })
	add(s, server, "compare_helm_revisions", "Compare two retained Helm revisions by chart/status, inventory and sanitized configuration; hidden fields are excluded.", func(ctx context.Context, p HelmComparisonInput) (any, error) {
		return s.HelmCompare(ctx, p.Namespace, p.Name, p.From, p.To)
	})
	add(s, server, "list_custom_resource_types", "Inspect explicitly enabled established CRD types, served versions, scopes and discovery warnings.", func(ctx context.Context, _ struct{}) (any, error) { return s.CustomTypes(ctx) })
	add(s, server, "list_custom_resources", "List one enabled CRD type with bounded filters. Only metadata and standard conditions are exposed; no custom specs.", func(ctx context.Context, p CustomListInput) (any, error) {
		return s.CustomList(ctx, p.Definition, p.Version, service.Query{Namespace: p.Namespace, Search: p.Search, Status: p.Status, Sort: p.Sort, Desc: p.Desc, Limit: p.Limit, Offset: p.Offset})
	})
	add(s, server, "get_custom_resource_details", "Read a safe metadata/conditions projection and UID-correlated events. This is not a complete manifest.", func(ctx context.Context, p CustomDetailInput) (any, error) {
		return s.CustomDetail(ctx, p.Definition, p.Version, p.Namespace, p.Name)
	})
	return server
}
func Handler(s *service.Service) http.Handler {
	server := New(s)
	return sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}
