package service

import (
	"fmt"
	"strings"
	"timone/internal/model"
)

func has(values []any, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// These are direct binding indicators, not an effective authorization evaluation.
func bindingFindings(snap model.Snapshot, ns string) ([]model.Finding, bool) {
	roles := map[string]model.Resource{}
	readable := map[string]bool{"Role": true, "ClusterRole": true, "RoleBinding": true, "ClusterRoleBinding": true}
	for _, w := range snap.Warnings {
		readable[w.Kind] = false
	}
	for _, r := range snap.Resources {
		if r.Kind == "Role" || r.Kind == "ClusterRole" {
			roles[r.ID] = r
		}
	}
	performed := (readable["RoleBinding"] && (readable["Role"] || readable["ClusterRole"])) || (readable["ClusterRoleBinding"] && readable["ClusterRole"])
	findings := []model.Finding{}
	for _, binding := range snap.Resources {
		if binding.Kind != "RoleBinding" && binding.Kind != "ClusterRoleBinding" {
			continue
		}
		ref := model.Map(binding.Object, "roleRef")
		kind := model.Str(ref, "kind")
		if model.Str(ref, "apiGroup") != "rbac.authorization.k8s.io" || (kind != "Role" && kind != "ClusterRole") || (binding.Kind == "ClusterRoleBinding" && kind != "ClusterRole") {
			continue
		}
		roleNS := ""
		if kind == "Role" {
			roleNS = binding.Namespace
		}
		role, ok := roles[model.ID(kind, roleNS, model.Str(ref, "name"))]
		if !ok {
			continue
		}
		subjects := []string{}
		for _, v := range model.Arr(binding.Object, "subjects") {
			subject, _ := v.(map[string]any)
			subjectNS := model.Str(subject, "namespace")
			if subjectNS == "" && binding.Kind == "RoleBinding" {
				subjectNS = binding.Namespace
			}
			if model.Str(subject, "kind") == "ServiceAccount" && (ns == "" || subjectNS == ns) {
				subjects = append(subjects, "ServiceAccount "+subjectNS+"/"+model.Str(subject, "name"))
			}
			if model.Str(subject, "kind") == "Group" {
				name := model.Str(subject, "name")
				if name == "system:serviceaccounts" || (strings.HasPrefix(name, "system:serviceaccounts:") && (ns == "" || name == "system:serviceaccounts:"+ns)) {
					subjects = append(subjects, "Group "+name)
				}
			}
		}
		if len(subjects) == 0 {
			continue
		}
		risks := []string{}
		severity := "warning"
		for _, v := range model.Arr(role.Object, "rules") {
			rule, _ := v.(map[string]any)
			verbs, resources, groups := model.Arr(rule, "verbs"), model.Arr(rule, "resources"), model.Arr(rule, "apiGroups")
			if has(verbs, "*") && has(resources, "*") {
				risks = append(risks, "wildcard resource and verb grants")
				severity = "critical"
			}
			if (has(groups, "") || has(groups, "*")) && (has(resources, "secrets") || has(resources, "*")) && (has(verbs, "get") || has(verbs, "list") || has(verbs, "watch") || has(verbs, "*")) {
				risks = append(risks, "Secret read grants (possibly restricted by resourceNames)")
			}
			if (has(groups, "rbac.authorization.k8s.io") || has(groups, "*")) && (has(resources, "roles") || has(resources, "clusterroles") || has(resources, "*")) && (has(verbs, "escalate") || has(verbs, "bind") || has(verbs, "*")) {
				risks = append(risks, "RBAC bind/escalate grants")
				severity = "critical"
			}
		}
		if len(risks) == 0 {
			continue
		}
		risks = unique(risks)
		evidence := fmt.Sprintf("%s binds %s/%s to %s: %s. Direct binding indicator only; effective access and resourceNames restrictions are not resolved.", binding.Kind, role.Kind, role.Name, strings.Join(subjects[:min(len(subjects), 10)], ", "), strings.Join(risks, "; "))
		findings = append(findings, model.Finding{ID: "service-account-binding:" + binding.ID, Rule: "service-account-binding", Severity: severity, Resource: binding, Evidence: evidence, Recommendation: "Review the named binding, restrict resourceNames and verbs where applicable, and validate effective permissions with your Kubernetes authorization workflow."})
	}
	return findings, performed
}
