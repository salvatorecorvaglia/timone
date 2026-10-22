package provider

func (d *demo) seedGrants() {
	// These are illustrative declarations, never permissions granted to a real account.
	role := d.add("ClusterRole", "", "workload-observer", nil, nil, nil)
	role["rules"] = []any{map[string]any{"apiGroups": []any{"apps"}, "resources": []any{"deployments"}, "verbs": []any{"get", "list", "watch"}}}
	binding := d.add("RoleBinding", "staging", "production-preview-observer", nil, nil, nil)
	binding["roleRef"] = map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "workload-observer"}
	binding["subjects"] = []any{map[string]any{"kind": "ServiceAccount", "name": "default", "namespace": "production"}}
	discovery := d.add("ClusterRole", "", "demo-discovery", nil, nil, nil)
	discovery["rules"] = []any{map[string]any{"nonResourceURLs": []any{"/api", "/apis", "/version"}, "verbs": []any{"get"}}}
	global := d.add("ClusterRoleBinding", "", "demo-authenticated-discovery", nil, nil, nil)
	global["roleRef"] = map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "demo-discovery"}
	global["subjects"] = []any{map[string]any{"kind": "Group", "name": "system:authenticated", "apiGroup": "rbac.authorization.k8s.io"}}
	restricted := d.add("Role", "production", "storefront-config-reader", nil, nil, nil)
	restricted["rules"] = []any{map[string]any{"apiGroups": []any{""}, "resources": []any{"configmaps"}, "resourceNames": []any{"storefront-config"}, "verbs": []any{"get"}}}
	named := d.add("RoleBinding", "production", "commerce-config-reader", nil, nil, nil)
	named["roleRef"] = map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": "storefront-config-reader"}
	named["subjects"] = []any{map[string]any{"kind": "Group", "name": "system:serviceaccounts:production", "apiGroup": "rbac.authorization.k8s.io"}}
	unresolved := d.add("RoleBinding", "production", "legacy-reader", nil, nil, nil)
	unresolved["roleRef"] = map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": "retired-role"}
	unresolved["subjects"] = []any{map[string]any{"kind": "User", "name": "system:serviceaccount:production:default", "apiGroup": "rbac.authorization.k8s.io"}}
}
