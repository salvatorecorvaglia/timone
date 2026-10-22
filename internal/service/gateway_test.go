package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"timone/internal/model"
	"timone/internal/provider"
)

func gatewayResource(kind, ns, name string, spec map[string]any) model.Resource {
	return model.Summarize(model.Object{"kind": kind, "metadata": map[string]any{"name": name, "namespace": ns, "generation": int64(3)}, "spec": spec})
}
func gatewayFixture() (model.Resource, model.Resource, map[string]any, map[string]model.Resource) {
	route := gatewayResource("HTTPRoute", "app", "route", nil)
	l := map[string]any{"name": "web", "port": 80, "protocol": "HTTP"}
	gw := gatewayResource("Gateway", "infra", "edge", map[string]any{"gatewayClassName": "class", "listeners": []any{l}})
	class := gatewayResource("GatewayClass", "", "class", map[string]any{"controllerName": "example.io/controller"})
	ns := gatewayResource("Namespace", "", "app", nil)
	ns.Labels = map[string]any{"team": "web"}
	return route, gw, l, map[string]model.Resource{route.ID: route, gw.ID: gw, class.ID: class, ns.ID: ns}
}

func TestGatewayListenerAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, from, protocol, kind, section string
		selector                            any
		port                                int
		missingNS                           bool
		want                                string
	}{
		{name: "default same excludes cross namespace", want: "denied"},
		{name: "all permits cross namespace without grants", from: "All", want: "allowed"},
		{name: "matching selector", from: "Selector", selector: map[string]any{"matchLabels": map[string]any{"team": "web"}}, want: "allowed"},
		{name: "selector mismatch", from: "Selector", selector: map[string]any{"matchLabels": map[string]any{"team": "other"}}, want: "denied"},
		{name: "match expression", from: "Selector", selector: map[string]any{"matchExpressions": []any{map[string]any{"key": "team", "operator": "In", "values": []any{"web"}}}}, want: "allowed"},
		{name: "missing namespace", from: "Selector", selector: map[string]any{}, missingNS: true, want: "unknown"},
		{name: "missing selector", from: "Selector", want: "unknown"},
		{name: "invalid selector", from: "Selector", selector: map[string]any{"matchExpressions": []any{map[string]any{"key": "team", "operator": "Unsupported"}}}, want: "unknown"},
		{name: "incompatible protocol", from: "All", protocol: "TCP", want: "denied"},
		{name: "incompatible route kind", from: "All", kind: "TCPRoute", want: "denied"},
		{name: "wrong listener name", from: "All", section: "missing", want: "denied"},
		{name: "wrong listener port", from: "All", port: 443, want: "denied"},
		{name: "matching listener port", from: "All", port: 80, section: "web", want: "allowed"},
		{name: "unknown namespace policy", from: "Future", want: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			route, gw, l, all := gatewayFixture()
			policy := map[string]any{}
			if tc.from != "" {
				policy["from"] = tc.from
			}
			if tc.selector != nil {
				policy["selector"] = tc.selector
			}
			allowed := map[string]any{"namespaces": policy}
			if tc.kind != "" {
				allowed["kinds"] = []any{map[string]any{"kind": tc.kind}}
			}
			l["allowedRoutes"] = allowed
			if tc.protocol != "" {
				l["protocol"] = tc.protocol
			}
			if tc.missingNS {
				delete(all, model.ID("Namespace", "", "app"))
			}
			ref := map[string]any{"name": "edge", "namespace": "infra"}
			if tc.section != "" {
				ref["sectionName"] = tc.section
			}
			if tc.port != 0 {
				ref["port"] = tc.port
			}
			got := assessParent(route, gw, ref, all)
			if got.State != tc.want {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestGatewayAnyEligibleListenerAndSameNamespaceDefault(t *testing.T) {
	route, gw, l, all := gatewayFixture()
	route.Namespace = gw.Namespace
	if got := assessParent(route, gw, map[string]any{}, all); got.State != "allowed" {
		t.Fatalf("same namespace %+v", got)
	}
	route.Namespace = "app"
	model.Map(gw.Object, "spec")["listeners"] = []any{l, map[string]any{"name": "public", "port": 8080, "protocol": "HTTP", "allowedRoutes": map[string]any{"namespaces": map[string]any{"from": "All"}}}}
	if got := assessParent(route, gw, map[string]any{}, all); got.State != "allowed" {
		t.Fatalf("any matching listener %+v", got)
	}
	if got := assessParent(route, gw, map[string]any{"sectionName": "public", "port": 80}, all); got.State != "denied" {
		t.Fatalf("must match name AND port %+v", got)
	}
}

func TestGatewayControllerStatusIdentityAndFreshness(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		generation                             int64
		controller                             string
		section                                string
		duplicate, duplicateCondition, noClass bool
		want                                   string
	}{
		{name: "fresh matching controller", generation: 3, controller: "example.io/controller", want: "False"},
		{name: "stale", generation: 2, controller: "example.io/controller", want: "Unknown"},
		{name: "future generation", generation: 4, controller: "example.io/controller", want: "Unknown"},
		{name: "foreign controller", generation: 3, controller: "foreign.io/controller", want: "Unknown"},
		{name: "different listener", generation: 3, controller: "example.io/controller", section: "other", want: "Unknown"},
		{name: "ambiguous status", generation: 3, controller: "example.io/controller", duplicate: true, want: "Unknown"},
		{name: "ambiguous condition", generation: 3, controller: "example.io/controller", duplicateCondition: true, want: "Unknown"},
		{name: "class unavailable", generation: 3, controller: "example.io/controller", noClass: true, want: "Unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			route, gw, l, all := gatewayFixture()
			l["allowedRoutes"] = map[string]any{"namespaces": map[string]any{"from": "All"}}
			ref := map[string]any{"name": "edge", "namespace": "infra"}
			statusRef := map[string]any{"name": "edge", "namespace": "infra", "group": gatewayGroup, "kind": "Gateway"}
			if tc.section != "" {
				statusRef["sectionName"] = tc.section
			}
			condition := map[string]any{"type": "Accepted", "status": "False", "observedGeneration": tc.generation, "reason": "NotAllowedByListeners"}
			conditions := []any{condition}
			if tc.duplicateCondition {
				conditions = append(conditions, condition)
			}
			parent := map[string]any{"parentRef": statusRef, "controllerName": tc.controller, "conditions": conditions}
			parents := []any{parent}
			if tc.duplicate {
				parents = append(parents, parent)
			}
			route.Object["status"] = map[string]any{"parents": parents}
			if tc.noClass {
				delete(all, model.ID("GatewayClass", "", "class"))
			}
			a := assessParent(route, gw, ref, all)
			if a.State != "allowed" || a.Accepted != tc.want || a.ResolvedRefs != "Unknown" {
				t.Fatalf("configuration and report must remain separate %+v", a)
			}
		})
	}
}

func TestGatewayBackendPortsAndReferenceGrants(t *testing.T) {
	for _, tc := range []struct {
		name                                                                  string
		port                                                                  any
		sameNS, grant, badFrom, badGroup, badName, wildcard, incomplete, zero bool
		want                                                                  string
	}{
		{name: "same namespace", port: 80, sameNS: true, want: "allowed"},
		{name: "same namespace unaffected by missing grants", port: 80, sameNS: true, incomplete: true, want: "allowed"},
		{name: "missing port", sameNS: true, want: "denied"},
		{name: "fractional port", port: 80.5, sameNS: true, want: "denied"},
		{name: "targetPort not service port", port: 8080, sameNS: true, want: "denied"},
		{name: "absent grant", port: 80, want: "denied"},
		{name: "unreadable or truncated grant collection", port: 80, incomplete: true, want: "unknown"},
		{name: "named grant", port: 80, grant: true, want: "allowed"},
		{name: "unnamed grant", port: 80, grant: true, wildcard: true, want: "allowed"},
		{name: "positive grant despite partial collection", port: 80, grant: true, incomplete: true, want: "allowed"},
		{name: "wrong source namespace", port: 80, grant: true, badFrom: true, want: "denied"},
		{name: "wrong destination group", port: 80, grant: true, badGroup: true, want: "denied"},
		{name: "wrong destination name", port: 80, grant: true, badName: true, want: "denied"},
		{name: "invalid port still blocked with unknown grant", port: 9999, incomplete: true, want: "denied"},
		{name: "zero weight retains declared relationship", port: 80, grant: true, zero: true, want: "allowed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			route, _, _, all := gatewayFixture()
			ns := "backend"
			if tc.sameNS {
				ns = route.Namespace
			}
			svc := gatewayResource("Service", ns, "api", map[string]any{"ports": []any{map[string]any{"port": 80, "targetPort": 8080}}})
			if tc.grant {
				from := map[string]any{"group": gatewayGroup, "kind": "HTTPRoute", "namespace": route.Namespace}
				to := map[string]any{"group": "", "kind": "Service", "name": svc.Name}
				if tc.badFrom {
					from["namespace"] = "other"
				}
				if tc.badGroup {
					to["group"] = "apps"
				}
				if tc.badName {
					to["name"] = "other"
				}
				if tc.wildcard {
					delete(to, "name")
				}
				g := gatewayResource("ReferenceGrant", ns, "grant", map[string]any{"from": []any{from}, "to": []any{to}})
				all[g.ID] = g
			}
			warnings := []model.Warning{}
			if tc.incomplete {
				warnings = append(warnings, model.Warning{Kind: "ReferenceGrant", Message: "denied or truncated"})
			}
			ref := map[string]any{}
			if tc.port != nil {
				ref["port"] = tc.port
			}
			if tc.zero {
				ref["weight"] = 0
			}
			a := assessBackend(route, svc, ref, all, warnings)
			if a.State != tc.want || a.Accepted != "" || a.ResolvedRefs != "" {
				t.Fatalf("got %+v", a)
			}
			if tc.zero && !strings.Contains(strings.Join(a.Evidence, " "), "weight is zero") {
				t.Fatalf("weight evidence missing %+v", a)
			}
		})
	}
}

type gatewayReadProvider struct {
	provider.Provider
	deny bool
}

func (p gatewayReadProvider) List(ctx context.Context, kind, ns string) ([]model.Object, bool, error) {
	if kind == "ReferenceGrant" && p.deny {
		return nil, false, &provider.Error{Code: 403, Message: "ReferenceGrant read denied"}
	}
	return p.Provider.List(ctx, kind, ns)
}
func TestGatewayTopologyUsesAuthorizedCollectionsAndDistinctReferences(t *testing.T) {
	s := New(gatewayReadProvider{Provider: provider.NewDemo(), deny: true})
	g, err := s.Topology(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	ids := map[string]bool{}
	for _, e := range g.Edges {
		if ids[e.ID] {
			t.Fatal("duplicate edge ID")
		}
		ids[e.ID] = true
		if e.Routing != nil && e.Relation == "backend reference" && strings.Contains(e.Source, "preview-route") {
			states[e.Routing.Reference] = e.Routing.State
		}
	}
	if states["spec.rules[0].backendRefs[0]"] != "unknown" || states["spec.rules[0].backendRefs[1]"] != "denied" {
		t.Fatalf("got %v", states)
	}
	// Filtering does not read endpoints outside the authorized namespace snapshot.
	scoped, err := s.Topology(context.Background(), Query{Namespace: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range scoped.Edges {
		if e.Routing != nil {
			t.Fatalf("unreadable endpoints must not be synthesized %+v", e)
		}
	}
}

func TestTopologyRelationshipLimit(t *testing.T) {
	objects := map[string][]model.Object{}
	for i := 0; i < 50; i++ {
		objects["Pod"] = append(objects["Pod"], model.Object{"kind": "Pod", "metadata": map[string]any{"name": fmt.Sprintf("pod-%02d", i), "namespace": "production", "labels": map[string]any{"app": "api"}}})
		objects["Service"] = append(objects["Service"], model.Object{"kind": "Service", "metadata": map[string]any{"name": fmt.Sprintf("svc-%02d", i), "namespace": "production"}, "spec": map[string]any{"selector": map[string]any{"app": "api"}}})
	}
	g, err := New(observedProvider{Provider: provider.NewDemo(), objects: objects}).Topology(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 100 || len(g.Edges) != 2000 || !g.Truncated {
		t.Fatalf("bounds nodes=%d edges=%d truncated=%v", len(g.Nodes), len(g.Edges), g.Truncated)
	}
	if len(g.Warnings) == 0 {
		t.Fatal("edge truncation must be explicit")
	}
}

func TestGatewayHostnameIntersection(t *testing.T) {
	for _, tc := range []struct{ listener, route, want string }{
		{"api.example.com", "api.example.com", "allowed"},
		{"*.example.com", "api.eu.example.com", "allowed"},
		{"*.example.com", "example.com", "denied"},
		{"api.example.com", "other.example.com", "denied"},
		{"*.eu.example.com", "*.example.com", "allowed"},
		{"*.a.example.com", "*.b.example.com", "denied"},
		{"api.example.com", "*.example.com", "allowed"},
		{"", "api.example.com", "allowed"},
		{"api.example.com", "bad.*.example.com", "unknown"},
		{"127.0.0.1", "api.example.com", "unknown"},
	} {
		t.Run(tc.listener+"/"+tc.route, func(t *testing.T) {
			r := gatewayResource("HTTPRoute", "app", "r", map[string]any{"hostnames": []any{tc.route}})
			state, _ := hostnameAdmission(r, map[string]any{"hostname": tc.listener})
			if state != tc.want {
				t.Fatal(state, tc.want)
			}
		})
	}
	route, gw, l, all := gatewayFixture()
	l["allowedRoutes"] = map[string]any{"namespaces": map[string]any{"from": "All"}}
	l["hostname"] = "app.example.com"
	route.Object["spec"] = map[string]any{"hostnames": []any{"other.example.com"}}
	if got := assessParent(route, gw, map[string]any{}, all); got.State != "denied" {
		t.Fatal(got)
	}
}
