package service

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

const rbacGroup = "rbac.authorization.k8s.io"

type GrantQuery struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

func grantIdentity(o model.Object) model.Resource {
	r := model.Summarize(o)
	r.Labels = nil
	r.Object = nil
	return r
}

// ServiceAccountGrants explains observed RBAC bindings. It does not impersonate
// the subject, create authorization reviews, or claim an allow/deny decision.
func (s *Service) ServiceAccountGrants(ctx context.Context, q GrantQuery) (model.GrantReport, error) {
	if err := ValidateResource("ServiceAccount", q.Namespace, q.Name); err != nil {
		return model.GrantReport{}, err
	}
	page := Query{Limit: q.Limit, Offset: q.Offset}
	if err := page.Validate(); err != nil {
		return model.GrantReport{}, err
	}
	if _, err := s.Scope(q.Namespace); err != nil {
		return model.GrantReport{}, err
	}
	workspace, err := s.Scope("")
	if err != nil {
		return model.GrantReport{}, err
	}
	// Always authorize the current subject with a GET before any grant inventory.
	sa, err := s.Provider.Get(ctx, "ServiceAccount", q.Namespace, q.Name)
	if err != nil {
		return model.GrantReport{}, err
	}
	if model.Str(sa, "kind") != "ServiceAccount" || model.Str(sa, "metadata", "name") != q.Name || model.Str(sa, "metadata", "namespace") != q.Namespace {
		return model.GrantReport{}, &provider.Error{Code: 502, Message: "ServiceAccount response identity mismatch"}
	}
	report := model.GrantReport{ServiceAccount: grantIdentity(sa), Identity: "system:serviceaccount:" + q.Namespace + ":" + q.Name, Groups: []string{"system:serviceaccounts", "system:serviceaccounts:" + q.Namespace, "system:authenticated"}, Mode: s.Provider.Mode(), Scope: workspace, Items: []model.RBACGrant{}, Limit: page.Limit, Offset: page.Offset, Complete: true, Coverage: []model.GrantCoverage{}, Warnings: []model.Warning{}, ObservedAt: time.Now().UTC(), Limitations: []string{
		"Observed RBAC grant inventory only; no live authorization decision, token validation, impersonation or authorization-review request is performed.",
		"Only the configured workspace and readable collections are covered. Missing grants do not prove denial; other authorizers, extra identity groups, admission and indirect escalation paths are not evaluated.",
		"Rules are shown literally with binding scope. Resource-name restrictions, API groups, subresources and non-resource URL patterns are preserved, not expanded into permission claims.",
		"Reads are not an atomic snapshot. For aggregated ClusterRoles only current rules are shown; aggregation-controller reconciliation is not verified.",
	}}
	if workspace != "" {
		report.Limitations = append(report.Limitations, "RoleBindings outside namespace "+workspace+" are not read, even when they could reference this ServiceAccount.")
	}
	type collection struct {
		objects   []model.Object
		truncated bool
		err       error
	}
	collections := map[string]collection{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, kind := range []string{"Role", "ClusterRole", "RoleBinding", "ClusterRoleBinding"} {
		wg.Add(1)
		go func(kind string) {
			defer wg.Done()
			objects, truncated, e := s.Provider.List(ctx, kind, workspace)
			mu.Lock()
			collections[kind] = collection{objects, truncated, e}
			mu.Unlock()
		}(kind)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return model.GrantReport{}, err
	}
	roles := map[string]model.Object{}
	bindings := []model.Object{}
	for _, kind := range []string{"Role", "ClusterRole", "RoleBinding", "ClusterRoleBinding"} {
		c := collections[kind]
		coverage := model.GrantCoverage{Kind: kind, State: "available"}
		if c.err != nil {
			coverage.State = "unavailable"
			report.Complete = false
			report.Warnings = append(report.Warnings, model.Warning{Kind: kind, Message: c.err.Error()})
		} else {
			count := len(c.objects)
			coverage.Observed = &count
			invalidIdentity := false
			if c.truncated {
				coverage.State = "truncated"
				report.Complete = false
				report.Warnings = append(report.Warnings, model.Warning{Kind: kind, Message: "Collection truncated; unobserved bindings or referenced roles may be missing."})
			}
			for _, o := range c.objects {
				if model.Str(o, "kind") != kind {
					report.Complete = false
					invalidIdentity = true
					continue
				}
				ns := model.Str(o, "metadata", "namespace")
				namespaced := kind == "Role" || kind == "RoleBinding"
				if (namespaced && ns == "") || (!namespaced && ns != "") || (namespaced && workspace != "" && ns != workspace) {
					report.Complete = false
					invalidIdentity = true
					continue
				}
				if kind == "Role" || kind == "ClusterRole" {
					roles[model.ID(kind, ns, model.Str(o, "metadata", "name"))] = o
				} else {
					bindings = append(bindings, o)
				}
			}
			if invalidIdentity {
				coverage.State = "partial"
				report.Warnings = append(report.Warnings, model.Warning{Kind: kind, Message: "Invalid or out-of-scope collection identities were omitted."})
			}
		}
		report.Coverage = append(report.Coverage, coverage)
	}
	sort.Slice(bindings, func(i, j int) bool { return bindingID(bindings[i]) < bindingID(bindings[j]) })
	retainedRules := 0
	unresolved, clipped := false, false
	for _, binding := range bindings {
		if err := ctx.Err(); err != nil {
			return model.GrantReport{}, err
		}
		matches, subjectsClipped := grantSubjects(binding, q.Namespace, q.Name, report.Identity, report.Groups)
		if subjectsClipped {
			report.Complete = false
			clipped = true
		}
		if len(matches) == 0 {
			continue
		}
		report.Total++
		// Still count matching bindings beyond this page, without copying their rules.
		if report.Total <= page.Offset || len(report.Items) >= page.Limit {
			continue
		}
		ns := model.Str(binding, "metadata", "namespace")
		scope := "namespace"
		if model.Str(binding, "kind") == "ClusterRoleBinding" {
			scope = "cluster"
		}
		ref := model.Map(binding, "roleRef")
		roleKind, roleName := model.Str(ref, "kind"), model.Str(ref, "name")
		roleNS := ""
		if roleKind == "Role" {
			roleNS = ns
		}
		grant := model.RBACGrant{Binding: grantIdentity(binding), Reference: model.RBACReference{Kind: roleKind, Name: roleName, Namespace: roleNS}, Subjects: matches, Scope: scope, Namespace: ns, Resolution: "unavailable", Rules: []model.RBACRule{}, Notes: []string{}}
		valid := model.Str(ref, "apiGroup") == rbacGroup && (roleKind == "Role" || roleKind == "ClusterRole") && !(scope == "cluster" && roleKind != "ClusterRole") && len(roleName) > 0 && len(roleName) <= 253 && rbacNameRE.MatchString(roleName)
		role, ok := roles[model.ID(roleKind, roleNS, roleName)]
		if !valid {
			grant.Resolution = "invalid reference"
			grant.Notes = append(grant.Notes, "Binding roleRef is invalid or unsupported; no rule grant is asserted.")
			unresolved = true
		} else if !ok {
			grant.Notes = append(grant.Notes, "Referenced role is absent from the readable inventory. It may be missing, denied or outside collection limits; no empty permission set is inferred.")
			unresolved = true
		} else {
			r := grantIdentity(role)
			grant.Role = &r
			grant.Resolution = "observed"
			raw := model.Arr(role, "rules")
			if model.Value(role, "rules") != nil && raw == nil {
				grant.Resolution = "invalid rules"
				grant.RulesTruncated = true
				unresolved = true
			}
			for index, value := range raw {
				if err := ctx.Err(); err != nil {
					return model.GrantReport{}, err
				}
				if index >= 256 || retainedRules >= 2000 {
					grant.RulesTruncated = true
					clipped = true
					break
				}
				rule, e := boundedGrantRule(value)
				if e != nil {
					grant.Resolution = "partial rules"
					grant.RulesTruncated = true
					clipped = true
					continue
				}
				grant.Rules = append(grant.Rules, rule)
				retainedRules++
			}
			if model.Value(role, "aggregationRule") != nil {
				grant.Notes = append(grant.Notes, "Aggregated ClusterRole: these are its currently observed rules; aggregation freshness is not verified.")
			}
			if scope == "namespace" {
				grant.Notes = append(grant.Notes, "This RoleBinding applies only to namespaced resources in "+ns+". Non-resource URLs and cluster-scoped resources in its referenced role do not gain cluster-wide scope.")
			}
			if grant.RulesTruncated {
				grant.Notes = append(grant.Notes, "Rule output is incomplete because a rule was invalid/oversized or an output limit was reached.")
			}
		}
		report.Items = append(report.Items, grant)
	}
	if unresolved {
		report.Complete = false
		report.Warnings = append(report.Warnings, model.Warning{Kind: "RBAC", Message: "Some page bindings have unresolved roles or invalid references/rules. No permission denial is inferred."})
	}
	if clipped {
		report.Complete = false
		report.Warnings = append(report.Warnings, model.Warning{Kind: "RBAC", Message: "Subject/rule evidence invalid or limited: 256 subjects per binding, 256 inspected rules per role, 2000 retained rules per page and 2 KiB per rule."})
	}
	report.Truncated = !report.Complete || report.Total > page.Offset+len(report.Items)
	return report, nil
}

func bindingID(o model.Object) string {
	return model.ID(model.Str(o, "kind"), model.Str(o, "metadata", "namespace"), model.Str(o, "metadata", "name"))
}
func grantSubjects(binding model.Object, ns, name, identity string, groups []string) ([]string, bool) {
	subjects := model.Arr(binding, "subjects")
	incomplete := model.Value(binding, "subjects") != nil && subjects == nil
	matched := map[string]bool{}
	for _, v := range subjects[:min(len(subjects), 256)] {
		s, ok := v.(map[string]any)
		if !ok {
			incomplete = true
			continue
		}
		kind, n, group := model.Str(s, "kind"), model.Str(s, "name"), model.Str(s, "apiGroup")
		switch kind {
		case "ServiceAccount":
			subjectNS := model.Str(s, "namespace")
			if subjectNS == "" && model.Str(binding, "kind") == "RoleBinding" {
				subjectNS = model.Str(binding, "metadata", "namespace")
			}
			if group == "" && n == name && subjectNS == ns {
				matched["ServiceAccount "+ns+"/"+name] = true
			}
		case "User":
			if (group == "" || group == rbacGroup) && model.Str(s, "namespace") == "" && n == identity {
				matched["User "+identity] = true
			}
		case "Group":
			if (group == "" || group == rbacGroup) && model.Str(s, "namespace") == "" {
				for _, g := range groups {
					if n == g {
						matched["Group "+g] = true
					}
				}
			}
		}
	}
	out := []string{}
	for subject := range matched {
		out = append(out, subject)
	}
	sort.Strings(out)
	return out, incomplete || len(subjects) > 256
}

func boundedGrantRule(value any) (model.RBACRule, error) {
	b, err := json.Marshal(value)
	var rule model.RBACRule
	if err != nil || len(b) > 2048 || json.Unmarshal(b, &rule) != nil || len(rule.Verbs) == 0 || (len(rule.Resources) == 0 && len(rule.NonResourceURLs) == 0) || (len(rule.Resources) > 0 && (len(rule.APIGroups) == 0 || len(rule.NonResourceURLs) > 0)) || (len(rule.NonResourceURLs) > 0 && (len(rule.ResourceNames) > 0 || len(rule.APIGroups) > 0)) {
		return rule, &provider.Error{Code: 502, Message: "Invalid or oversized RBAC rule"}
	}
	for field, values := range [][]string{rule.APIGroups, rule.Resources, rule.Verbs, rule.ResourceNames, rule.NonResourceURLs} {
		if len(values) > 128 {
			return rule, &provider.Error{Code: 502, Message: "Oversized RBAC rule"}
		}
		if field > 0 {
			for _, value := range values {
				if value == "" {
					return rule, &provider.Error{Code: 502, Message: "Invalid empty RBAC rule value"}
				}
			}
		}
	}
	// Use non-nil arrays for an unambiguous, consistent wire shape.
	if rule.APIGroups == nil {
		rule.APIGroups = []string{}
	}
	if rule.Resources == nil {
		rule.Resources = []string{}
	}
	if rule.ResourceNames == nil {
		rule.ResourceNames = []string{}
	}
	if rule.NonResourceURLs == nil {
		rule.NonResourceURLs = []string{}
	}
	return rule, nil
}
