package provider

import "timone/internal/model"

func (d *demo) seedGateway() {
	d.add("GatewayClass", "", "demo", map[string]any{"controllerName": "demo.timone.io/gateway"}, nil, nil)
	d.add("Gateway", "production", "public-gateway", map[string]any{"gatewayClassName": "demo", "listeners": []any{map[string]any{"name": "http", "port": 80, "protocol": "HTTP"}}}, nil, nil)
	route := d.add("HTTPRoute", "production", "storefront-route", map[string]any{"parentRefs": []any{map[string]any{"name": "public-gateway"}}, "rules": []any{map[string]any{"backendRefs": []any{map[string]any{"name": "storefront", "port": 80}}}}}, nil, nil)
	model.Map(route, "metadata")["generation"] = int64(2)
	route["status"] = map[string]any{"parents": []any{map[string]any{"parentRef": map[string]any{"name": "public-gateway"}, "controllerName": "demo.timone.io/gateway", "conditions": []any{
		map[string]any{"type": "Accepted", "status": "True", "observedGeneration": int64(2), "reason": "Accepted"},
		map[string]any{"type": "ResolvedRefs", "status": "True", "observedGeneration": int64(2), "reason": "ResolvedRefs"},
	}}}}
	// A rejected parent, an allowed cross-namespace backend and an invalid Service port.
	blocked := d.add("HTTPRoute", "staging", "preview-route", map[string]any{"parentRefs": []any{map[string]any{"name": "public-gateway", "namespace": "production", "sectionName": "http"}}, "rules": []any{map[string]any{"backendRefs": []any{
		map[string]any{"name": "storefront", "namespace": "production", "port": 80},
		map[string]any{"name": "storefront", "namespace": "production", "port": 9999, "weight": 0},
	}}}}, nil, nil)
	model.Map(blocked, "metadata")["generation"] = int64(1)
	blocked["status"] = map[string]any{"parents": []any{map[string]any{"parentRef": map[string]any{"name": "public-gateway", "namespace": "production", "sectionName": "http"}, "controllerName": "demo.timone.io/gateway", "conditions": []any{
		map[string]any{"type": "Accepted", "status": "False", "observedGeneration": int64(1), "reason": "NotAllowedByListeners"},
		map[string]any{"type": "ResolvedRefs", "status": "False", "observedGeneration": int64(1), "reason": "PortNotFound"},
	}}}}
	d.add("ReferenceGrant", "production", "allow-preview", map[string]any{"from": []any{map[string]any{"group": "gateway.networking.k8s.io", "kind": "HTTPRoute", "namespace": "staging"}}, "to": []any{map[string]any{"group": "", "kind": "Service", "name": "storefront"}}}, nil, nil)
}
