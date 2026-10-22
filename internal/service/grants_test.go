package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"timone/internal/model"
	"timone/internal/provider"
)

type grantReader struct {
	provider.Provider
	objects                 map[string][]model.Object
	failKind, truncatedKind string
	getErr                  error
	wrongIdentity           bool
	mu                      sync.Mutex
	calls                   []string
}

func (p *grantReader) Get(ctx context.Context, kind, ns, name string) (model.Object, error) {
	p.mu.Lock()
	p.calls = append(p.calls, "get:"+kind+":"+ns)
	p.mu.Unlock()
	if p.getErr != nil {
		return nil, p.getErr
	}
	if p.wrongIdentity {
		name = "wrong"
	}
	return model.Object{"kind": kind, "metadata": map[string]any{"name": name, "namespace": ns, "annotations": map[string]any{"private": "private-marker"}}, "secrets": []any{map[string]any{"name": "private-marker"}}}, ctx.Err()
}
func (p *grantReader) List(ctx context.Context, kind, ns string) ([]model.Object, bool, error) {
	p.mu.Lock()
	p.calls = append(p.calls, "list:"+kind+":"+ns)
	p.mu.Unlock()
	if p.failKind == kind {
		return nil, false, &provider.Error{Code: 403, Message: "RBAC read denied"}
	}
	return p.objects[kind], p.truncatedKind == kind, ctx.Err()
}
func (p *grantReader) Events(context.Context, string) ([]model.Event, error) {
	panic("grant report must not read events")
}
func grantRole(kind, ns, name string, rules []any) model.Object {
	return model.Object{"kind": kind, "metadata": map[string]any{"name": name, "namespace": ns}, "rules": rules}
}
func grantBinding(kind, ns, name, roleKind, roleName string, subjects ...any) model.Object {
	return model.Object{"kind": kind, "metadata": map[string]any{"name": name, "namespace": ns}, "roleRef": map[string]any{"apiGroup": rbacGroup, "kind": roleKind, "name": roleName}, "subjects": subjects}
}
func readRule() map[string]any {
	return map[string]any{"apiGroups": []any{""}, "resources": []any{"pods", "pods/log"}, "verbs": []any{"get"}, "resourceNames": []any{"api"}}
}
func grantService(objects map[string][]model.Object) (*Service, *grantReader) {
	p := &grantReader{Provider: provider.NewDemo(), objects: objects}
	return New(p), p
}

func TestGrantSubjectIdentityAndStandardGroups(t *testing.T) {
	for _, tc := range []struct {
		name, kind, ns string
		subject        map[string]any
		want           int
	}{
		{"direct", "RoleBinding", "production", map[string]any{"kind": "ServiceAccount", "name": "default", "namespace": "production"}, 1},
		{"default namespace", "RoleBinding", "production", map[string]any{"kind": "ServiceAccount", "name": "default"}, 1},
		{"user identity", "RoleBinding", "production", map[string]any{"kind": "User", "name": "system:serviceaccount:production:default", "apiGroup": rbacGroup}, 1},
		{"global SA group", "RoleBinding", "production", map[string]any{"kind": "Group", "name": "system:serviceaccounts"}, 1},
		{"namespace SA group", "RoleBinding", "production", map[string]any{"kind": "Group", "name": "system:serviceaccounts:production"}, 1},
		{"authenticated group", "ClusterRoleBinding", "", map[string]any{"kind": "Group", "name": "system:authenticated"}, 1},
		{"other namespace", "RoleBinding", "production", map[string]any{"kind": "ServiceAccount", "name": "default", "namespace": "staging"}, 0},
		{"other group", "RoleBinding", "production", map[string]any{"kind": "Group", "name": "system:serviceaccounts:staging"}, 0},
		{"anonymous group", "RoleBinding", "production", map[string]any{"kind": "Group", "name": "system:unauthenticated"}, 0},
		{"foreign API group", "RoleBinding", "production", map[string]any{"kind": "User", "name": "system:serviceaccount:production:default", "apiGroup": "other.example"}, 0},
		{"CRB cannot default namespace", "ClusterRoleBinding", "", map[string]any{"kind": "ServiceAccount", "name": "default"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roleKind, roleNS := "Role", tc.ns
			if tc.kind == "ClusterRoleBinding" {
				roleKind, roleNS = "ClusterRole", ""
			}
			s, _ := grantService(map[string][]model.Object{roleKind: {grantRole(roleKind, roleNS, "reader", []any{readRule()})}, tc.kind: {grantBinding(tc.kind, tc.ns, "binding", roleKind, "reader", tc.subject)}})
			r, e := s.ServiceAccountGrants(context.Background(), GrantQuery{Namespace: "production", Name: "default"})
			if e != nil || len(r.Items) != tc.want || !r.Complete {
				t.Fatalf("%+v %v", r, e)
			}
		})
	}
}
func TestGrantRoleBindingNamespaceAndClusterRoleScope(t *testing.T) {
	subjects := []any{map[string]any{"kind": "ServiceAccount", "name": "default", "namespace": "production"}, map[string]any{"kind": "Group", "name": "system:authenticated"}}
	cluster := grantRole("ClusterRole", "", "reader", []any{readRule(), map[string]any{"nonResourceURLs": []any{"/healthz", "/api/*"}, "verbs": []any{"get"}}})
	cluster["aggregationRule"] = map[string]any{"clusterRoleSelectors": []any{map[string]any{"matchLabels": map[string]any{"role": "reader"}}}}
	s, _ := grantService(map[string][]model.Object{"ClusterRole": {cluster}, "RoleBinding": {grantBinding("RoleBinding", "staging", "preview", "ClusterRole", "reader", subjects...)}, "ClusterRoleBinding": {grantBinding("ClusterRoleBinding", "", "global", "ClusterRole", "reader", subjects...)}})
	r, e := s.ServiceAccountGrants(context.Background(), GrantQuery{Namespace: "production", Name: "default"})
	if e != nil || r.Total != 2 {
		t.Fatalf("%+v %v", r, e)
	}
	for _, g := range r.Items {
		if len(g.Subjects) != 2 || len(g.Rules) != 2 || g.Rules[0].ResourceNames[0] != "api" || g.Rules[0].Resources[1] != "pods/log" {
			t.Fatalf("literal grants/provenance lost %+v", g)
		}
		notes := strings.Join(g.Notes, " ")
		if !strings.Contains(notes, "Aggregated ClusterRole") {
			t.Fatal("aggregation caveat missing")
		}
		if g.Binding.Kind == "RoleBinding" && (g.Namespace != "staging" || g.Scope != "namespace" || !strings.Contains(notes, "Non-resource URLs")) {
			t.Fatalf("scope expanded %+v", g)
		}
		if g.Binding.Kind == "ClusterRoleBinding" && (g.Namespace != "" || g.Scope != "cluster") {
			t.Fatalf("cluster scope lost %+v", g)
		}
	}
}
func TestGrantUnresolvedRolesAndCollectionCoverage(t *testing.T) {
	binding := grantBinding("RoleBinding", "production", "binding", "Role", "missing", map[string]any{"kind": "ServiceAccount", "name": "default"})
	for _, tc := range []struct {
		name, deny, truncated string
		role                  bool
	}{
		{"missing role", "", "", false}, {"denied role list", "Role", "", false}, {"truncated role list with positive evidence", "", "Role", true}, {"denied binding collection", "RoleBinding", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects := map[string][]model.Object{"RoleBinding": {binding}}
			if tc.role {
				objects["Role"] = []model.Object{grantRole("Role", "production", "missing", []any{readRule()})}
			}
			s, p := grantService(objects)
			p.failKind = tc.deny
			p.truncatedKind = tc.truncated
			r, e := s.ServiceAccountGrants(context.Background(), GrantQuery{Namespace: "production", Name: "default"})
			if e != nil || r.Complete || !r.Truncated {
				t.Fatalf("%+v %v", r, e)
			}
			if tc.deny == "RoleBinding" {
				if r.Total != 0 {
					t.Fatal("denied bindings were used")
				}
			} else if !tc.role && (len(r.Items) != 1 || r.Items[0].Role != nil || r.Items[0].Resolution != "unavailable") {
				t.Fatalf("missing role invented %+v", r.Items)
			}
			for _, c := range r.Coverage {
				if c.Kind == tc.deny && (c.State != "unavailable" || c.Observed != nil) {
					t.Fatalf("denied read treated as zero %+v", c)
				}
			}
		})
	}
}
func TestGrantSubjectAuthorizationPrecedesInventoryAndEnforcesScope(t *testing.T) {
	for _, code := range []int{401, 403, 404} {
		s, p := grantService(nil)
		p.getErr = &provider.Error{Code: code, Message: "Subject read failed"}
		if _, err := s.ServiceAccountGrants(context.Background(), GrantQuery{Namespace: "production", Name: "default"}); provider.Status(err) != code {
			t.Fatal(err)
		}
		if len(p.calls) != 1 || !strings.HasPrefix(p.calls[0], "get:") {
			t.Fatalf("inventory read before authorization %v", p.calls)
		}
	}
	s, p := grantService(nil)
	p.wrongIdentity = true
	if _, e := s.ServiceAccountGrants(context.Background(), GrantQuery{Namespace: "production", Name: "default"}); provider.Status(e) != 502 || len(p.calls) != 1 {
		t.Fatalf("mismatched identity accepted %v %v", e, p.calls)
	}
	p.wrongIdentity = false
	p.calls = nil
	p.Provider = scoped{provider.NewDemo()}
	if _, e := s.ServiceAccountGrants(context.Background(), GrantQuery{Namespace: "staging", Name: "default"}); provider.Status(e) != 403 || len(p.calls) != 0 {
		t.Fatal("subject escaped scope", e, p.calls)
	}
	for _, q := range []GrantQuery{{Namespace: "production", Name: "../default"}, {Name: "default"}, {Namespace: "production", Name: "default", Limit: 201}, {Namespace: "production", Name: "default", Offset: -1}} {
		if _, e := s.ServiceAccountGrants(context.Background(), q); provider.Status(e) != 400 {
			t.Fatalf("invalid input accepted %+v %v", q, e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := s.ServiceAccountGrants(ctx, GrantQuery{Namespace: "production", Name: "default"}); e == nil {
		t.Fatal("cancellation lost")
	}
}
func TestGrantScopedInventoryAndFreshIdentity(t *testing.T) {
	s, p := grantService(map[string][]model.Object{"RoleBinding": {grantBinding("RoleBinding", "staging", "outside", "ClusterRole", "reader", map[string]any{"kind": "ServiceAccount", "namespace": "production", "name": "default"})}})
	p.Provider = scoped{provider.NewDemo()}
	for range 2 {
		r, e := s.ServiceAccountGrants(context.Background(), GrantQuery{Namespace: "production", Name: "default"})
		if e != nil || len(r.Items) != 0 || r.Scope != "production" {
			t.Fatalf("outside binding included %+v %v", r, e)
		}
	}
	if len(p.calls) != 10 {
		t.Fatalf("expected fresh GET plus four bounded reads each time %v", p.calls)
	}
	for _, call := range p.calls {
		if strings.HasPrefix(call, "list:") && !strings.HasSuffix(call, ":production") {
			t.Fatalf("unscoped read %s", call)
		}
	}
}
func TestGrantLiteralRulesBoundsAndPayloadExclusion(t *testing.T) {
	binding := grantBinding("RoleBinding", "production", "b", "Role", "reader", map[string]any{"kind": "ServiceAccount", "name": "default"})
	role := grantRole("Role", "production", "reader", []any{readRule()})
	s, _ := grantService(map[string][]model.Object{"RoleBinding": {binding}, "Role": {role}})
	r, e := s.ServiceAccountGrants(context.Background(), GrantQuery{Namespace: "production", Name: "default"})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "private-marker") || strings.Contains(string(b), "annotations") {
		t.Fatal("subject payload leaked", string(b))
	}
	for _, raw := range []any{nil, map[string]any{"verbs": []any{"get"}}, map[string]any{"apiGroups": []any{""}, "resources": []any{"pods"}, "verbs": []any{"get"}, "nonResourceURLs": []any{"/api"}}, map[string]any{"apiGroups": []any{""}, "resources": []any{"pods"}, "verbs": []any{7}}, map[string]any{"apiGroups": []any{""}, "resources": []any{strings.Repeat("x", 2100)}, "verbs": []any{"get"}}} {
		role["rules"] = []any{raw}
		r, e = s.ServiceAccountGrants(context.Background(), GrantQuery{Namespace: "production", Name: "default"})
		if e != nil || r.Complete || len(r.Items[0].Rules) != 0 || !r.Items[0].RulesTruncated {
			t.Fatalf("unsafe rule %+v %v", r, e)
		}
	}
	role["rules"] = []any{}
	r, e = s.ServiceAccountGrants(context.Background(), GrantQuery{Namespace: "production", Name: "default"})
	if e != nil || !r.Complete || r.Items[0].Resolution != "observed" || len(r.Items[0].Rules) != 0 {
		t.Fatalf("empty observed role misreported %+v %v", r, e)
	}
	rules := []any{}
	for range 300 {
		rules = append(rules, readRule())
	}
	role["rules"] = rules
	r, e = s.ServiceAccountGrants(context.Background(), GrantQuery{Namespace: "production", Name: "default"})
	if e != nil || len(r.Items[0].Rules) != 256 || !r.Items[0].RulesTruncated || r.Complete {
		t.Fatalf("rule bound failed %+v %v", r, e)
	}
}
func TestGrantOutputBudgetsDoNotTurnMissingEvidenceIntoEmptyPermissions(t *testing.T) {
	rules := []any{}
	for range 256 {
		rules = append(rules, readRule())
	}
	objects := map[string][]model.Object{"Role": {grantRole("Role", "production", "reader", rules)}}
	for i := 0; i < 9; i++ {
		objects["RoleBinding"] = append(objects["RoleBinding"], grantBinding("RoleBinding", "production", strings.Repeat("b", i+1), "Role", "reader", map[string]any{"kind": "ServiceAccount", "name": "default"}))
	}
	s, _ := grantService(objects)
	r, e := s.ServiceAccountGrants(context.Background(), GrantQuery{Namespace: "production", Name: "default"})
	count := 0
	for _, g := range r.Items {
		count += len(g.Rules)
	}
	if e != nil || count != 2000 || r.Complete || !r.Items[8].RulesTruncated || len(r.Items[8].Rules) != 0 {
		t.Fatalf("global output budget: rules=%d complete=%v error=%v", count, r.Complete, e)
	}
	subjects := []any{}
	for range 256 {
		subjects = append(subjects, map[string]any{"kind": "User", "name": "someone-else"})
	}
	subjects = append(subjects, map[string]any{"kind": "ServiceAccount", "name": "default"})
	objects["RoleBinding"] = []model.Object{grantBinding("RoleBinding", "production", "large-subject-list", "Role", "reader", subjects...)}
	r, e = s.ServiceAccountGrants(context.Background(), GrantQuery{Namespace: "production", Name: "default"})
	if e != nil || r.Complete || r.Total != 0 || len(r.Warnings) == 0 {
		t.Fatalf("subject limit falsely implied absence %+v %v", r, e)
	}
}

func TestGrantStablePaginationAndInvalidReference(t *testing.T) {
	binding := func(name string) model.Object {
		return grantBinding("RoleBinding", "production", name, "Role", "reader", map[string]any{"kind": "ServiceAccount", "name": "default"})
	}
	first := binding("a")
	bad := binding("b")
	model.Map(bad, "roleRef")["apiGroup"] = "wrong.example"
	s, _ := grantService(map[string][]model.Object{"Role": {grantRole("Role", "production", "reader", []any{readRule()})}, "RoleBinding": {binding("c"), bad, first}})
	r, e := s.ServiceAccountGrants(context.Background(), GrantQuery{Namespace: "production", Name: "default", Limit: 1, Offset: 1})
	if e != nil || r.Total != 3 || len(r.Items) != 1 || r.Items[0].Binding.Name != "b" || r.Items[0].Resolution != "invalid reference" || r.Complete {
		t.Fatalf("page %+v %v", r, e)
	}
}
