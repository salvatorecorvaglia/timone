package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"timone/internal/model"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const gatewayGroup = "gateway.networking.k8s.io"

func defaultString(m map[string]any, key, fallback string) string {
	if model.Value(m, key) == nil {
		return fallback
	}
	return model.Str(m, key)
}

// Reads only the authorized snapshot. Never expands the configured namespace ceiling.
func gatewayEdges(route model.Resource, all map[string]model.Resource, warnings []model.Warning) []model.Edge {
	edges := []model.Edge{}
	_ = visitGatewayEdges(context.Background(), route, all, warnings, func(e model.Edge) bool { edges = append(edges, e); return true })
	return edges
}

func visitGatewayEdges(ctx context.Context, route model.Resource, all map[string]model.Resource, warnings []model.Warning, emit func(model.Edge) bool) error {
	appendEdge := func(source, target, relation, path string, assessment model.RoutingAssessment) bool {
		if _, ok := all[source]; !ok {
			return true
		}
		if _, ok := all[target]; !ok {
			return true
		}
		assessment.Reference = path
		return emit(model.Edge{ID: source + ">" + target + ":" + relation + ":" + path, Source: source, Target: target, Relation: relation, Confidence: "declared", Routing: &assessment})
	}
	for i, v := range model.Arr(route.Object, "spec", "parentRefs") {
		if err := ctx.Err(); err != nil {
			return err
		}
		ref, _ := v.(map[string]any)
		if defaultString(ref, "group", gatewayGroup) != gatewayGroup || defaultString(ref, "kind", "Gateway") != "Gateway" {
			continue
		}
		id := model.ID("Gateway", defaultString(ref, "namespace", route.Namespace), model.Str(ref, "name"))
		if gw, ok := all[id]; ok {
			if !appendEdge(id, route.ID, "parent reference", fmt.Sprintf("spec.parentRefs[%d]", i), assessParent(route, gw, ref, all)) {
				return nil
			}
		}
	}
	for i, v := range model.Arr(route.Object, "spec", "rules") {
		rule, _ := v.(map[string]any)
		for j, v := range model.Arr(rule, "backendRefs") {
			if err := ctx.Err(); err != nil {
				return err
			}
			ref, _ := v.(map[string]any)
			if defaultString(ref, "group", "") != "" || defaultString(ref, "kind", "Service") != "Service" {
				continue
			}
			id := model.ID("Service", defaultString(ref, "namespace", route.Namespace), model.Str(ref, "name"))
			if svc, ok := all[id]; ok {
				if !appendEdge(route.ID, id, "backend reference", fmt.Sprintf("spec.rules[%d].backendRefs[%d]", i, j), assessBackend(route, svc, ref, all, warnings)) {
					return nil
				}
			}
		}
	}
	return ctx.Err()
}

func assessParent(route, gw model.Resource, ref map[string]any, all map[string]model.Resource) model.RoutingAssessment {
	a := model.RoutingAssessment{State: "denied", Evidence: []string{}, Limitations: []string{"Checks cover listener selection, protocol, route kind, namespace policy and hostname intersection only. TLS, filters, listener readiness, Gateway programming and traffic are not verified."}, Accepted: "Unknown", ResolvedRefs: "Unknown"}
	for _, v := range model.Arr(gw.Object, "spec", "listeners") {
		listener, _ := v.(map[string]any)
		if s := model.Str(ref, "sectionName"); s != "" && s != model.Str(listener, "name") {
			continue
		}
		if model.Value(ref, "port") != nil && model.Num(ref, "port") != model.Num(listener, "port") {
			continue
		}
		state, reason := listenerAdmission(route, gw, listener, all)
		hostState, hostReason := hostnameAdmission(route, listener)
		reason += "; " + hostReason
		if state != "denied" && hostState == "denied" {
			state = "denied"
		} else if state == "allowed" && hostState == "unknown" {
			state = "unknown"
		}

		a.Evidence = append(a.Evidence, fmt.Sprintf("Listener %s: %s", model.Str(listener, "name"), reason))
		if state == "allowed" {
			a.State = "allowed"
		} else if state == "unknown" && a.State != "allowed" {
			a.State = "unknown"
		}
	}
	if len(a.Evidence) == 0 {
		a.Evidence = append(a.Evidence, "No configured listener matches the parent sectionName and port.")
	}
	controllerReport(&a, route, gw, ref, all)
	return a
}

func listenerAdmission(route, gw model.Resource, l map[string]any, all map[string]model.Resource) (string, string) {
	protocol := model.Str(l, "protocol")
	if protocol != "HTTP" && protocol != "HTTPS" {
		return "denied", "protocol does not support HTTPRoute"
	}
	if protocol == "HTTPS" && model.Str(l, "tls", "mode") == "Passthrough" {
		return "denied", "TLS passthrough does not support HTTPRoute"
	}
	kinds := model.Arr(l, "allowedRoutes", "kinds")
	if len(kinds) > 0 {
		found := false
		for _, v := range kinds {
			k, _ := v.(map[string]any)
			if defaultString(k, "group", gatewayGroup) == gatewayGroup && model.Str(k, "kind") == "HTTPRoute" {
				found = true
			}
		}
		if !found {
			return "denied", "allowedRoutes.kinds excludes HTTPRoute"
		}
	}
	policy := model.Map(l, "allowedRoutes", "namespaces")
	switch defaultString(policy, "from", "Same") {
	case "All":
		return "allowed", "namespace policy allows all namespaces"
	case "Same":
		if route.Namespace == gw.Namespace {
			return "allowed", "same namespace as Gateway"
		}
		return "denied", "default/Same policy excludes the route namespace"
	case "Selector":
		ns, ok := all[model.ID("Namespace", "", route.Namespace)]
		if !ok {
			return "unknown", "route Namespace labels are unavailable"
		}
		raw := model.Value(policy, "selector")
		if raw == nil {
			return "unknown", "namespace selector is missing"
		}
		b, err := json.Marshal(raw)
		var selector metav1.LabelSelector
		if err != nil || json.Unmarshal(b, &selector) != nil {
			return "unknown", "namespace selector cannot be parsed"
		}
		parsed, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			return "unknown", "namespace selector is invalid"
		}
		set := labels.Set{}
		for k, v := range ns.Labels {
			s, ok := v.(string)
			if !ok {
				return "unknown", "namespace labels are invalid"
			}
			set[k] = s
		}
		if parsed.Matches(set) {
			return "allowed", "route Namespace matches the listener selector"
		}
		return "denied", "route Namespace does not match the listener selector"
	default:
		return "unknown", "unsupported namespace policy"
	}
}

// Normalize only API defaults, preserving explicit sectionName/port identity.
func parentKey(ref map[string]any, ns string) string {
	port := ""
	if model.Value(ref, "port") != nil {
		port = model.Str(ref, "port")
	}
	return fmt.Sprintf("%s/%s/%s/%s/%s/%s", defaultString(ref, "group", gatewayGroup), defaultString(ref, "kind", "Gateway"), defaultString(ref, "namespace", ns), model.Str(ref, "name"), model.Str(ref, "sectionName"), port)
}

func controllerReport(a *model.RoutingAssessment, route, gw model.Resource, ref map[string]any, all map[string]model.Resource) {
	class := all[model.ID("GatewayClass", "", model.Str(gw.Object, "spec", "gatewayClassName"))]
	controller := model.Str(class.Object, "spec", "controllerName")
	if controller == "" {
		a.Evidence = append(a.Evidence, "Controller report unknown: GatewayClass/controller identity is unavailable.")
		return
	}
	matches := []map[string]any{}
	for _, v := range model.Arr(route.Object, "status", "parents") {
		parent, _ := v.(map[string]any)
		if model.Str(parent, "controllerName") == controller && parentKey(model.Map(parent, "parentRef"), route.Namespace) == parentKey(ref, route.Namespace) {
			matches = append(matches, parent)
		}
	}
	if len(matches) != 1 {
		a.Evidence = append(a.Evidence, "Controller report unknown: expected exactly one matching parent status from "+controller+".")
		return
	}
	generation := model.Num(route.Object, "metadata", "generation")
	for _, typ := range []string{"Accepted", "ResolvedRefs"} {
		conditions := []map[string]any{}
		for _, v := range model.Arr(matches[0], "conditions") {
			c, _ := v.(map[string]any)
			if model.Str(c, "type") == typ {
				conditions = append(conditions, c)
			}
		}
		if len(conditions) != 1 || generation <= 0 || model.Num(conditions[0], "observedGeneration") != generation {
			a.Evidence = append(a.Evidence, "Controller "+typ+" unknown: missing, duplicate or stale condition.")
			continue
		}
		c := conditions[0]
		status := model.Str(c, "status")
		if status != "True" && status != "False" && status != "Unknown" {
			status = "Unknown"
		}
		if typ == "Accepted" {
			a.Accepted = status
		} else {
			a.ResolvedRefs = status
		}
		// Bound untrusted controller text and do not treat route-wide resolution as a per-backend result.
		a.Evidence = append(a.Evidence, fmt.Sprintf("Controller %s reports %s=%s for route generation %.0f (%.200s).", controller, typ, status, generation, model.Str(c, "reason")))
	}
	a.Limitations = append(a.Limitations, "Controller conditions describe this route/parent, not individual backends or observed traffic; Gateway changes may not yet be reconciled.")
}

func assessBackend(route, svc model.Resource, ref map[string]any, all map[string]model.Resource, warnings []model.Warning) model.RoutingAssessment {
	a := model.RoutingAssessment{State: "allowed", Evidence: []string{}, Limitations: []string{"Checks cover Service port and cross-namespace ReferenceGrant only. Endpoint readiness, appProtocol, backend TLS, filters and traffic are not verified."}}
	port := model.Num(ref, "port")
	found := false
	for _, v := range model.Arr(svc.Object, "spec", "ports") {
		p, _ := v.(map[string]any)
		if port == model.Num(p, "port") {
			found = true
		}
	}
	if port < 1 || port > 65535 || math.Trunc(port) != port || !found {
		a.State = "denied"
		a.Evidence = append(a.Evidence, "Backend port is missing, invalid or absent from Service.spec.ports[].port.")
	} else {
		a.Evidence = append(a.Evidence, fmt.Sprintf("Service exposes backend port %.0f (Service port, not targetPort).", port))
	}
	if route.Namespace == svc.Namespace {
		a.Evidence = append(a.Evidence, "Same-namespace backend does not require a ReferenceGrant.")
	} else {
		grants := []string{}
		for _, g := range all {
			if g.Kind != "ReferenceGrant" || g.Namespace != svc.Namespace {
				continue
			}
			from := false
			to := false
			for _, v := range model.Arr(g.Object, "spec", "from") {
				f, _ := v.(map[string]any)
				if model.Str(f, "group") == gatewayGroup && model.Str(f, "kind") == "HTTPRoute" && model.Str(f, "namespace") == route.Namespace {
					from = true
				}
			}
			for _, v := range model.Arr(g.Object, "spec", "to") {
				t, _ := v.(map[string]any)
				if model.Value(t, "group") != nil && model.Str(t, "group") == "" && model.Str(t, "kind") == "Service" && (model.Value(t, "name") == nil || model.Str(t, "name") == svc.Name) {
					to = true
				}
			}
			if from && to {
				grants = append(grants, g.Namespace+"/"+g.Name)
			}
		}
		sort.Strings(grants)
		if len(grants) > 0 {
			a.Evidence = append(a.Evidence, "Matching ReferenceGrant: "+grants[0])
		} else {
			complete := true
			for _, w := range warnings {
				if w.Kind == "ReferenceGrant" {
					complete = false
				}
			}
			if complete {
				a.State = "denied"
				a.Evidence = append(a.Evidence, "No matching ReferenceGrant in the backend namespace.")
			} else {
				if a.State != "denied" {
					a.State = "unknown"
				}
				a.Evidence = append(a.Evidence, "ReferenceGrant collection unavailable or incomplete; permission cannot be determined.")
			}
		}
	}
	if model.Value(ref, "weight") != nil && model.Num(ref, "weight") == 0 {
		a.Evidence = append(a.Evidence, "Backend weight is zero; this reference receives no configured forwarding weight.")
	}
	return a
}
