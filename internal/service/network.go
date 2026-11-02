package service

import (
	"context"
	"fmt"
	"strconv"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type NetworkEndpoint struct {
	Slice       model.Resource    `json:"slice"`
	Pod         *model.Resource   `json:"pod,omitempty"`
	Addresses   []string          `json:"addresses"`
	Ready       *bool             `json:"ready"`
	Serving     *bool             `json:"serving"`
	Terminating bool              `json:"terminating"`
	Port        *float64          `json:"port"`
	Checks      []AssessmentCheck `json:"checks"`
	Ingress     PolicyDirection   `json:"ingress"`
	Egress      PolicyDirection   `json:"egress"`
}
type NetworkPath struct {
	Reference   string            `json:"reference"`
	Service     model.Resource    `json:"service"`
	ServicePort float64           `json:"servicePort"`
	PortName    string            `json:"portName"`
	Protocol    string            `json:"protocol"`
	TargetPort  string            `json:"targetPort"`
	Checks      []AssessmentCheck `json:"checks"`
	Endpoints   []NetworkEndpoint `json:"endpoints"`
}
type NetworkAssessment struct {
	Resource    model.Resource    `json:"resource"`
	Source      *model.Resource   `json:"source,omitempty"`
	ObservedAt  time.Time         `json:"observedAt"`
	State       string            `json:"state"`
	Checks      []AssessmentCheck `json:"checks"`
	Paths       []NetworkPath     `json:"paths"`
	Warnings    []model.Warning   `json:"warnings"`
	Limitations []string          `json:"limitations"`
}
type backendReference struct {
	namespace, name, portName, reference string
	port                                 float64
	route                                model.Object
	ref                                  map[string]any
}
type networkObjects struct {
	items    []model.Object
	complete bool
}

func optionalBool(o map[string]any, path ...string) *bool {
	if model.Value(o, path...) == nil {
		return nil
	}
	b := model.Bool(o, path...)
	return &b
}
func objectLabels(o model.Object) map[string]any {
	if o == nil {
		return nil
	}
	if v := model.Map(o, "metadata", "labels"); v != nil {
		return v
	}
	return map[string]any{}
}
func (s *Service) Network(ctx context.Context, kind, ns, name, sourceNS, sourceName string) (NetworkAssessment, error) {
	ctx = provider.WithReadBudget(ctx, provider.InvestigationBytes)
	out := NetworkAssessment{ObservedAt: time.Now().UTC(), State: "configuration-only", Checks: []AssessmentCheck{}, Paths: []NetworkPath{}, Warnings: []model.Warning{}, Limitations: []string{
		"Declared Route/Ingress, Service, EndpointSlice and native NetworkPolicy observations only. No DNS lookup, network probe or traffic test is performed. TLS, route filters, authentication and service-mesh behavior are not evaluated.",
		"Choose a source Pod to evaluate its egress and backend ingress. Gateway/Ingress controller source identity, SNAT/DNAT, hostNetwork, ipBlock peers, CNI enforcement and cluster-wide policy extensions are not inferred.",
		"Native policy results combine additive rules, peer selectors, namespace selectors and supported ports. A declared allowance is not proof of connectivity; an incomplete inventory cannot establish denial or absence.",
		"Only UID-verified Pod endpoints receive Pod policy analysis. Numeric ports need not be declared in container specs. publishNotReadyAddresses may publish endpoints independently of application readiness.",
	}}
	if kind != "Service" && kind != "Ingress" && kind != "HTTPRoute" && kind != "Gateway" {
		return out, &provider.Error{Code: 400, Message: "Network analysis supports Service, Ingress, HTTPRoute and Gateway"}
	}
	if e := ValidateResource(kind, ns, name); e != nil {
		return out, e
	}
	if _, e := s.Scope(ns); e != nil {
		return out, e
	}
	root, e := s.Provider.Get(ctx, kind, ns, name)
	if e != nil {
		return out, e
	}
	out.Resource = assessmentResource(root)
	objects := map[string]model.Object{out.Resource.ID: root}
	failed := map[string]bool{}
	reads := 0
	get := func(k, nspace, n string) (model.Object, bool) {
		id := model.ID(k, nspace, n)
		if o, ok := objects[id]; ok {
			return o, true
		}
		if failed[id] {
			return nil, false
		}
		if e := ValidateResource(k, nspace, n); e != nil {
			failed[id] = true
			out.Warnings = append(out.Warnings, model.Warning{Kind: k, Message: "Invalid referenced resource identity"})
			return nil, false
		}
		if nspace != "" {
			if _, e := s.Scope(nspace); e != nil {
				failed[id] = true
				out.Warnings = append(out.Warnings, model.Warning{Kind: k, Message: e.Error()})
				return nil, false
			}
		}
		if reads >= 100 {
			failed[id] = true
			out.Warnings = append(out.Warnings, model.Warning{Kind: k, Message: "Network reference read limit of 100 reached"})
			return nil, false
		}
		reads++
		o, e := s.Provider.Get(ctx, k, nspace, n)
		if e != nil {
			failed[id] = true
			out.Warnings = append(out.Warnings, model.Warning{Kind: k, Message: e.Error()})
			return nil, false
		}
		objects[id] = o
		return o, true
	}
	catalogs := map[string]networkObjects{}
	list := func(k, nspace string) networkObjects {
		key := k + ":" + nspace
		if v, ok := catalogs[key]; ok {
			return v
		}
		if nspace != "" {
			if _, e := s.Scope(nspace); e != nil {
				out.Warnings = append(out.Warnings, model.Warning{Kind: k, Message: e.Error()})
				v := networkObjects{}
				catalogs[key] = v
				return v
			}
		}
		limit := 5000
		if k == "NetworkPolicy" || k == "ReferenceGrant" {
			limit = 200
		}
		items, complete := s.assessmentList(ctx, k, nspace, limit, &out.Warnings)
		v := networkObjects{items, complete}
		catalogs[key] = v
		return v
	}
	var source model.Object
	if sourceName != "" {
		if sourceNS == "" {
			sourceNS = ns
		}
		if e := ValidateResource("Pod", sourceNS, sourceName); e != nil {
			return out, e
		}
		if _, e := s.Scope(sourceNS); e != nil {
			return out, e
		}
		source, e = s.Provider.Get(ctx, "Pod", sourceNS, sourceName)
		if e != nil {
			return out, e
		}
		r := assessmentResource(source)
		out.Source = &r
	} else if sourceNS != "" {
		return out, &provider.Error{Code: 400, Message: "A source namespace requires a source Pod"}
	}
	labelNeeds := map[string]bool{}
	needsLabels := func(nspace, direction string, policies []model.Object) bool {
		key := nspace + ":" + direction
		if value, ok := labelNeeds[key]; ok {
			return value
		}
		value := namespacePolicyLabelsNeeded(policies, direction)
		labelNeeds[key] = value
		return value
	}
	namespaces := map[string]model.Object{}
	loadNamespace := func(nspace string) {
		if nspace == "" {
			return
		}
		if _, ok := namespaces[nspace]; ok {
			return
		}
		o, ok := get("Namespace", "", nspace)
		if ok {
			namespaces[nspace] = o
		}
	}

	references := []backendReference{}
	referencesLimited := false
	endpointTotal := 0
	appendReference := func(r backendReference) {
		if len(references) >= 64 {
			if !referencesLimited {
				out.Checks = append(out.Checks, check("backends", "unknown", "Only the first 64 backend references are inspected"))
				referencesLimited = true
			}
			return
		}
		references = append(references, r)
	}
	appendRoute := func(route model.Object) {
		routeNS := model.Str(route, "metadata", "namespace")
		loadNamespace(routeNS)
		all := map[string]model.Resource{}
		for key, o := range objects {
			all[key] = model.Summarize(o)
		}
		for n, o := range namespaces {
			all[model.ID("Namespace", "", n)] = model.Summarize(o)
		}
		for _, raw := range model.Arr(route, "spec", "parentRefs") {
			ref, _ := raw.(map[string]any)
			if defaultString(ref, "group", gatewayGroup) != gatewayGroup || defaultString(ref, "kind", "Gateway") != "Gateway" {
				continue
			}
			parentNS := defaultString(ref, "namespace", routeNS)
			if kind == "Gateway" && (parentNS != ns || model.Str(ref, "name") != name) {
				continue
			}
			gw, ok := get("Gateway", parentNS, model.Str(ref, "name"))
			if !ok {
				continue
			}
			assessment := assessParent(model.Summarize(route), model.Summarize(gw), ref, all)
			for _, fact := range assessment.Evidence {
				state := "observed"
				if assessment.State == "denied" {
					state = "constraint"
				} else if assessment.State == "unknown" {
					state = "unknown"
				}
				out.Checks = append(out.Checks, check("gateway-parent", state, fact))
			}
		}
		for i, raw := range model.Arr(route, "spec", "rules") {
			rule, _ := raw.(map[string]any)
			for j, raw := range model.Arr(rule, "backendRefs") {
				ref, _ := raw.(map[string]any)
				if defaultString(ref, "group", "") != "" || defaultString(ref, "kind", "Service") != "Service" {
					out.Checks = append(out.Checks, check("backend-kind", "unknown", "Non-Service route backends are not evaluated"))
					continue
				}
				if model.Value(ref, "weight") != nil && model.Num(ref, "weight") == 0 {
					out.Checks = append(out.Checks, check("backend-weight", "advisory", "A backend has zero declared traffic weight"))
				}
				appendReference(backendReference{namespace: defaultString(ref, "namespace", routeNS), name: model.Str(ref, "name"), port: model.Num(ref, "port"), reference: fmt.Sprintf("%s/%s rules[%d].backendRefs[%d]", routeNS, model.Str(route, "metadata", "name"), i, j), route: route, ref: ref})
			}
		}
	}
	switch kind {
	case "Service":
		appendReference(backendReference{namespace: ns, name: name, reference: "Service.spec.ports"})
	case "Ingress":
		add := func(backend map[string]any, path string) {
			if service := model.Map(backend, "service"); service != nil {
				appendReference(backendReference{namespace: ns, name: model.Str(service, "name"), portName: model.Str(service, "port", "name"), port: model.Num(service, "port", "number"), reference: path})
			} else {
				out.Checks = append(out.Checks, check("ingress-backend", "unknown", "Resource backends are not evaluated"))
			}
		}
		if v := model.Map(root, "spec", "defaultBackend"); v != nil {
			add(v, "spec.defaultBackend")
		}
		for i, raw := range model.Arr(root, "spec", "rules") {
			rule, _ := raw.(map[string]any)
			for j, raw := range model.Arr(rule, "http", "paths") {
				path, _ := raw.(map[string]any)
				add(model.Map(path, "backend"), fmt.Sprintf("spec.rules[%d].http.paths[%d]", i, j))
			}
		}
	case "HTTPRoute":
		appendRoute(root)
	case "Gateway":
		routes := list("HTTPRoute", s.Provider.Namespace())
		matched := 0
		for _, route := range routes.items {
			for _, raw := range model.Arr(route, "spec", "parentRefs") {
				ref, _ := raw.(map[string]any)
				if defaultString(ref, "group", gatewayGroup) == gatewayGroup && defaultString(ref, "kind", "Gateway") == "Gateway" && defaultString(ref, "namespace", model.Str(route, "metadata", "namespace")) == ns && model.Str(ref, "name") == name {
					appendRoute(route)
					matched++
					break
				}
			}
			if matched >= 32 {
				out.Checks = append(out.Checks, check("routes", "unknown", "Gateway analysis is limited to 32 referencing HTTPRoutes"))
				break
			}
		}
	}
	for _, reference := range references {
		if kind != "Service" && reference.port <= 0 && reference.portName == "" {
			out.Checks = append(out.Checks, check("backend-port", "constraint", "Backend Service port is missing or invalid"))
			continue
		}
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		svc, ok := get("Service", reference.namespace, reference.name)
		if !ok {
			continue
		}
		svcNS := model.Str(svc, "metadata", "namespace")
		ports := model.Arr(svc, "spec", "ports")
		found := false
		for portIndex, raw := range ports {
			if portIndex >= 32 {
				out.Checks = append(out.Checks, check("service-ports", "unknown", "Service ports limited to 32"))
				break
			}
			port, _ := raw.(map[string]any)
			number := model.Num(port, "port")
			portName := model.Str(port, "name")
			protocol := defaultString(port, "protocol", "TCP")
			if reference.portName != "" && portName != reference.portName {
				continue
			}
			if reference.port > 0 && number != reference.port {
				continue
			}
			found = true
			path := NetworkPath{Reference: reference.reference, Service: assessmentResource(svc), ServicePort: number, PortName: portName, Protocol: protocol, TargetPort: model.Str(port, "targetPort"), Checks: []AssessmentCheck{}, Endpoints: []NetworkEndpoint{}}
			if path.TargetPort == "" {
				path.TargetPort = strconv.Itoa(int(number))
			}
			if reference.route != nil {
				all := map[string]model.Resource{}
				for key, o := range objects {
					all[key] = model.Summarize(o)
				}
				grants := networkObjects{complete: true}
				if model.Str(reference.route, "metadata", "namespace") != svcNS {
					grants = list("ReferenceGrant", svcNS)
				}
				for _, o := range grants.items {
					all[model.Summarize(o).ID] = model.Summarize(o)
				}
				coverage := []model.Warning{}
				if !grants.complete {
					coverage = append(coverage, model.Warning{Kind: "ReferenceGrant", Message: "Incomplete grant inventory"})
				}
				assessment := assessBackend(model.Summarize(reference.route), model.Summarize(svc), reference.ref, all, coverage)
				for _, fact := range assessment.Evidence {
					state := "observed"
					if assessment.State == "denied" {
						state = "constraint"
					} else if assessment.State == "unknown" {
						state = "unknown"
					}
					path.Checks = append(path.Checks, check("route-backend", state, fact))
				}
			}
			if model.Str(svc, "spec", "type") == "ExternalName" {
				path.Checks = append(path.Checks, check("external-name", "unknown", "ExternalName uses DNS; EndpointSlices and Pod policy evaluation do not establish its target"))
				out.Paths = append(out.Paths, path)
				continue
			}
			slices := list("EndpointSlice", svcNS)
			readyCount := 0
			serviceSlices := 0
			for _, slice := range slices.items {
				if model.Str(slice, "metadata", "labels", "kubernetes.io/service-name") != reference.name {
					continue
				}
				serviceSlices++
				if serviceSlices > 200 {
					path.Checks = append(path.Checks, check("slices", "unknown", "Service slices limited to 200"))
					slices.complete = false
					break
				}
				slicePorts := []map[string]any{}
				for _, raw := range model.Arr(slice, "ports") {
					sp, _ := raw.(map[string]any)
					if model.Str(sp, "name") == portName && defaultString(sp, "protocol", "TCP") == protocol {
						slicePorts = append(slicePorts, sp)
					}
				}
				if len(slicePorts) == 0 {
					// Slices may group backends with different port sets.
					continue
				}
				for _, sp := range slicePorts {
					for _, raw := range model.Arr(slice, "endpoints") {
						if endpointTotal >= 200 {
							path.Checks = append(path.Checks, check("endpoints", "unknown", "Global endpoint output is limited to 200"))
							out.Paths = append(out.Paths, path)
							return out, ctx.Err()
						}
						if len(path.Endpoints) >= 200 {
							slices.complete = false
							break
						}
						endpoint, _ := raw.(map[string]any)
						ep := NetworkEndpoint{Slice: assessmentResource(slice), Addresses: []string{}, Ready: optionalBool(endpoint, "conditions", "ready"), Serving: optionalBool(endpoint, "conditions", "serving"), Terminating: model.Bool(endpoint, "conditions", "terminating"), Port: count(model.Object(sp), "port"), Checks: []AssessmentCheck{}, Ingress: PolicyDirection{State: "unknown", Policies: []model.Resource{}, Checks: []AssessmentCheck{}}, Egress: PolicyDirection{State: "source-required", Policies: []model.Resource{}, Checks: []AssessmentCheck{}}}
						for _, address := range model.Arr(endpoint, "addresses") {
							if value, ok := address.(string); ok && len(ep.Addresses) < 8 {
								ep.Addresses = append(ep.Addresses, value)
							}
						}
						if (ep.Ready == nil || *ep.Ready) && !ep.Terminating {
							readyCount++
						}
						if model.Bool(svc, "spec", "publishNotReadyAddresses") {
							ep.Checks = append(ep.Checks, check("published-readiness", "advisory", "Service publishes not-ready addresses; endpoint readiness is not proof of Pod/application readiness"))
						}
						targetRef := model.Map(endpoint, "targetRef")
						targetNS := defaultString(targetRef, "namespace", svcNS)
						var destination model.Object
						if model.Str(targetRef, "kind") == "Pod" && targetNS == svcNS && model.Str(targetRef, "uid") != "" {
							candidate, ok := get("Pod", targetNS, model.Str(targetRef, "name"))
							if ok && model.Str(candidate, "metadata", "uid") == model.Str(targetRef, "uid") {
								destination = candidate
								r := assessmentResource(candidate)
								ep.Pod = &r
							} else {
								ep.Checks = append(ep.Checks, check("endpoint-identity", "unknown", "Endpoint Pod UID is missing, stale or unreadable"))
							}
						} else {
							ep.Checks = append(ep.Checks, check("endpoint-identity", "unknown", "Endpoint has no exact same-namespace Pod UID reference; it may be an external or manually managed backend"))
						}
						if ep.Port == nil || *ep.Port < 1 || *ep.Port > 65535 || *ep.Port != float64(int(*ep.Port)) {
							ep.Checks = append(ep.Checks, check("endpoint-port", "unknown", "Endpoint port is unavailable"))
						} else {
							expected, err := strconv.Atoi(path.TargetPort)
							if err != nil {
								if destination != nil {
									if resolved, ok := podPort(destination, path.TargetPort, protocol); ok {
										expected = resolved
									} else {
										state := "constraint"
										if len(model.Map(svc, "spec", "selector")) == 0 {
											state = "advisory"
										}
										ep.Checks = append(ep.Checks, check("target-port", state, "Named targetPort does not resolve unambiguously in the current Pod spec; selectorless services may be managed manually"))
									}
								}
							} else if expected < 1 || expected > 65535 {
								expected = 0
							}
							if expected > 0 && float64(expected) != *ep.Port {
								state := "constraint"
								if len(model.Map(svc, "spec", "selector")) == 0 {
									state = "advisory"
								}
								ep.Checks = append(ep.Checks, check("target-port", state, "Published EndpointSlice port differs from the declared/resolved Service targetPort"))
							}
							if destination != nil {
								policies := list("NetworkPolicy", svcNS)
								if source != nil && needsLabels(svcNS, "ingress", policies.items) {
									loadNamespace(sourceNS)
								}
								ep.Ingress = evaluatePolicies("ingress", destination, source, destination, int(*ep.Port), protocol, policies.items, policies.complete, namespaces)
								if source != nil {
									egress := policies
									if sourceNS != svcNS {
										egress = list("NetworkPolicy", sourceNS)
									}
									if needsLabels(sourceNS, "egress", egress.items) {
										loadNamespace(svcNS)
									}
									ep.Egress = evaluatePolicies("egress", source, destination, destination, int(*ep.Port), protocol, egress.items, egress.complete, namespaces)
								}
							}
						}
						endpointTotal++
						path.Endpoints = append(path.Endpoints, ep)
					}
				}
			}
			if !slices.complete {
				path.Checks = append(path.Checks, check("endpoint-coverage", "unknown", "Endpoint inventory is incomplete or limited"))
			} else if readyCount == 0 {
				path.Checks = append(path.Checks, check("ready-endpoints", "constraint", "No ready non-terminating endpoints observed; terminating-backend fallback behavior is not evaluated"))
			}
			out.Paths = append(out.Paths, path)
			if len(out.Paths) >= 64 {
				out.Checks = append(out.Checks, check("paths", "unknown", "Network path output limited to 64 Service ports"))
				return out, ctx.Err()
			}
		}
		if !found {
			out.Checks = append(out.Checks, check("service-port", "constraint", "Backend reference does not match a declared Service port"))
		}
	}
	if len(out.Paths) == 0 {
		out.Checks = append(out.Checks, check("paths", "unknown", "No readable supported Service paths were found"))
	}
	return out, ctx.Err()
}
