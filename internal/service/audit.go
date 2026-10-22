package service

import (
	"context"
	"fmt"
	"strings"
	"timone/internal/model"
)

func contains(a, b string) bool { return strings.Contains(strings.ToLower(a), strings.ToLower(b)) }
func (s *Service) Audit(ctx context.Context, ns string) (*AuditResponse, error) {
	snap, e := s.Snapshot(ctx, ns, "NetworkPolicy", "Role", "ClusterRole", "RoleBinding", "ClusterRoleBinding")
	if e != nil {
		return nil, e
	}
	findings := []model.Finding{}
	performed := []string{}
	unavailable := []string{"Live ServiceAccount authorization decisions / impersonation analysis", "Full NetworkPolicy coverage and CNI enforcement", "Image vulnerability scanning", "Admission policy evaluation"}
	readable := map[string]bool{}
	for _, k := range append(append([]string{}, coreKinds...), "NetworkPolicy", "Role", "ClusterRole") {
		readable[k] = true
	}
	for _, w := range snap.Warnings {
		readable[w.Kind] = false
	}
	if readable["Pod"] || readable["Deployment"] || readable["StatefulSet"] || readable["DaemonSet"] || readable["Job"] || readable["CronJob"] {
		performed = append(performed, "privileged", "host-access", "resources", "mutable-image", "security-context", "probes")
	} else {
		unavailable = append(unavailable, "Workload configuration checks: no complete readable workload collection")
	}
	if readable["Role"] || readable["ClusterRole"] {
		performed = append(performed, "wildcard-rbac")
	} else {
		unavailable = append(unavailable, "RBAC wildcard checks: Role and ClusterRole collections unavailable")
	}
	if readable["NetworkPolicy"] && readable["Namespace"] {
		performed = append(performed, "network-policy-presence")
	} else {
		unavailable = append(unavailable, "NetworkPolicy presence: namespace or policy inventory unavailable")
	}

	add := func(r model.Resource, rule, severity, evidence, recommendation string) {
		findings = append(findings, model.Finding{ID: rule + ":" + r.ID + ":" + fmt.Sprint(len(findings)), Rule: rule, Severity: severity, Resource: r, Evidence: evidence, Recommendation: recommendation})
	}
	policies := map[string]bool{}
	policyVisible := readable["NetworkPolicy"] && readable["Namespace"]
	for _, w := range snap.Warnings {
		if w.Kind == "NetworkPolicy" {
			policyVisible = false
		}
	}
	for _, r := range snap.Resources {
		if r.Kind == "NetworkPolicy" {
			policies[r.Namespace] = true
		}
		if r.Kind == "Role" || r.Kind == "ClusterRole" {
			for _, v := range model.Arr(r.Object, "rules") {
				rule, _ := v.(map[string]any)
				for _, field := range []string{"verbs", "resources", "apiGroups"} {
					for _, v := range model.Arr(rule, field) {
						if v == "*" {
							add(r, "wildcard-rbac", "warning", "Wildcard in RBAC "+field, "Replace wildcard rules with the specific API groups, resources and verbs required.")
						}
					}
				}
			}
		}
	}
	for _, r := range snap.Resources {
		var ps map[string]any
		switch r.Kind {
		case "Deployment", "StatefulSet", "DaemonSet", "Job":
			ps = model.Map(r.Object, "spec", "template", "spec")
		case "CronJob":
			ps = model.Map(r.Object, "spec", "jobTemplate", "spec", "template", "spec")
		case "Pod":
			// Admission and debug containers can differ from the controller template.
			ps = model.Map(r.Object, "spec")
		case "Namespace":
			if policyVisible && !policies[r.Name] {
				add(r, "network-policy-presence", "info", "No NetworkPolicy objects observed in namespace; traffic isolation is not assessed.", "Evaluate namespace isolation and CNI NetworkPolicy support.")
			}
		}
		if ps == nil {
			continue
		}
		for _, field := range []string{"hostNetwork", "hostPID", "hostIPC"} {
			if model.Bool(ps, field) {
				add(r, "host-access", "critical", field+" is enabled", "Disable host namespace access unless essential.")
			}
		}
		for _, v := range model.Arr(ps, "volumes") {
			m, _ := v.(map[string]any)
			if model.Map(m, "hostPath") != nil {
				add(r, "host-access", "warning", "hostPath volume "+model.Str(m, "name"), "Use a scoped persistent volume instead of host filesystem access.")
			}
		}
		for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
			for _, v := range model.Arr(ps, field) {
				c, _ := v.(map[string]any)
				name := model.Str(c, "name")
				if model.Bool(c, "securityContext", "privileged") {
					add(r, "privileged", "critical", name+" runs privileged", "Remove privileged mode and grant only required capabilities.")
				}
				image := model.Str(c, "image")
				last := image[strings.LastIndex(image, "/")+1:]
				if strings.HasSuffix(image, ":latest") || (!strings.Contains(last, ":") && !strings.Contains(last, "@")) {
					add(r, "mutable-image", "warning", name+" uses "+image, "Pin images to immutable digests or a controlled version tag.")
				}
				if field != "ephemeralContainers" && (model.Str(c, "resources", "requests", "cpu") == "" || model.Str(c, "resources", "requests", "memory") == "" || model.Str(c, "resources", "limits", "cpu") == "" || model.Str(c, "resources", "limits", "memory") == "") {
					add(r, "resources", "warning", name+" is missing requests or limits", "Set CPU and memory requests and limits based on measured use.")
				}
				nonroot := model.Bool(ps, "securityContext", "runAsNonRoot")
				if model.Value(c, "securityContext", "runAsNonRoot") != nil {
					nonroot = model.Bool(c, "securityContext", "runAsNonRoot")
				}
				if !nonroot || model.Value(c, "securityContext", "allowPrivilegeEscalation") != false || model.Value(c, "securityContext", "readOnlyRootFilesystem") != true {
					add(r, "security-context", "warning", name+" has incomplete container security settings", "Set runAsNonRoot, disable privilege escalation and use a read-only root filesystem where feasible.")
				}
				if r.Kind != "Job" && r.Kind != "CronJob" && field == "containers" && (model.Map(c, "readinessProbe") == nil || model.Map(c, "livenessProbe") == nil) {
					add(r, "probes", "info", name+" lacks readiness or liveness probes", "Add application-appropriate probes; avoid probes that cause restart loops.")
				}
			}
		}
	}
	effectiveNS, scopeErr := s.Scope(ns)
	if scopeErr != nil {
		return nil, scopeErr
	}
	bindings, checked := bindingFindings(snap, effectiveNS)
	findings = append(findings, bindings...)
	if checked {
		performed = append(performed, "service-account-binding")
	} else {
		unavailable = append(unavailable, "ServiceAccount binding indicators: role/binding collections unavailable")
	}
	return &AuditResponse{Items: findings, Warnings: snap.Warnings, Performed: performed, Unavailable: unavailable, Disclaimer: "Configuration indicators based on readable resources, not a security certification."}, nil
}
