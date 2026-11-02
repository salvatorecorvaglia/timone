package service

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sort"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type MaintenancePod struct {
	Resource   model.Resource    `json:"resource"`
	Controller *model.Resource   `json:"controller,omitempty"`
	Class      string            `json:"class"`
	Checks     []AssessmentCheck `json:"checks"`
	PDBs       []model.Resource  `json:"pdbs"`
}
type MaintenanceBudget struct {
	Resource           model.Resource `json:"resource"`
	TargetPods         int            `json:"targetPods"`
	DisruptionsAllowed *float64       `json:"disruptionsAllowed"`
	Message            string         `json:"message"`
}
type MaintenanceAssessment struct {
	Node        model.Resource      `json:"node"`
	ObservedAt  time.Time           `json:"observedAt"`
	State       string              `json:"state"`
	Scope       string              `json:"scope"`
	Pods        []MaintenancePod    `json:"pods"`
	Budgets     []MaintenanceBudget `json:"budgets"`
	Checks      []AssessmentCheck   `json:"checks"`
	Warnings    []model.Warning     `json:"warnings"`
	Limitations []string            `json:"limitations"`
}

func matchBudget(pdb, pod model.Object) (bool, bool) {
	if model.Str(pdb, "metadata", "namespace") != model.Str(pod, "metadata", "namespace") {
		return false, true
	}
	raw := model.Map(pdb, "spec", "selector")
	if raw == nil {
		return false, true
	}
	if len(model.Map(raw, "matchLabels")) > 128 || len(model.Arr(raw, "matchExpressions")) > 128 {
		return false, false
	}
	var selector metav1.LabelSelector
	if runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &selector) != nil {
		return false, false
	}
	parsed, e := metav1.LabelSelectorAsSelector(&selector)
	if e != nil {
		return false, false
	}
	values := labels.Set{}
	for k, v := range model.Map(pod, "metadata", "labels") {
		text, ok := v.(string)
		if !ok {
			return false, false
		}
		values[k] = text
	}
	return parsed.Matches(values), true
}
func budgetCheck(pdb, pod model.Object) AssessmentCheck {
	name := model.Str(pdb, "metadata", "name")
	phase := model.Str(pod, "status", "phase")
	if phase == "Pending" || phase == "Succeeded" || phase == "Failed" {
		return check("pdb", "advisory", name+": this Pod phase does not consume the healthy-Pod disruption budget")
	}
	observed, generation := count(pdb, "status", "observedGeneration"), count(pdb, "metadata", "generation")
	if observed == nil || generation == nil || *observed < *generation {
		return check("pdb", "unknown", name+": PDB status is missing or stale")
	}
	ready, _ := podReady(pod)
	policy := model.Str(pdb, "spec", "unhealthyPodEvictionPolicy")
	if phase == "" || phase == "Unknown" {
		return check("pdb", "unknown", name+": Pod phase is unavailable")
	}
	if phase == "Running" && !ready {
		if policy == "AlwaysAllow" {
			return check("pdb", "advisory", name+": AlwaysAllow permits considering this running unhealthy Pod for eviction despite the budget")
		}
		if policy != "" && policy != "IfHealthyBudget" {
			return check("pdb", "unknown", name+": unrecognized unhealthy Pod eviction policy")
		}
		current, desired := count(pdb, "status", "currentHealthy"), count(pdb, "status", "desiredHealthy")
		if current != nil && desired != nil && *current >= *desired {
			return check("pdb", "advisory", name+": running unhealthy Pod and currentHealthy >= desiredHealthy; IfHealthyBudget does not require a positive disruption allowance")
		}
	}
	allowance := count(pdb, "status", "disruptionsAllowed")
	if allowance == nil {
		return check("pdb", "unknown", name+": disruptionsAllowed is unavailable")
	}
	if *allowance <= 0 {
		return check("pdb", "constraint", name+": controller reports no allowed voluntary disruptions")
	}
	return check("pdb", "observed", fmt.Sprintf("%s: %.0f disruptions currently allowed; concurrent evictions share this budget", name, *allowance))
}
func (s *Service) NodeMaintenance(ctx context.Context, name, requestedNS string) (MaintenanceAssessment, error) {
	ctx = provider.WithReadBudget(ctx, provider.InvestigationBytes)
	out := MaintenanceAssessment{ObservedAt: time.Now().UTC(), State: "review-required", Pods: []MaintenancePod{}, Budgets: []MaintenanceBudget{}, Checks: []AssessmentCheck{}, Warnings: []model.Warning{}, Limitations: []string{
		"Read-only preflight evidence, not a drain dry-run or authorization to evict. No cordon, drain, eviction or resource update is performed.",
		"Only readable Pods and PDBs within the configured scope are inspected. Missing or truncated collections cannot establish all node workloads.",
		"Replacement scheduling capacity, admission webhooks, concurrent disruption reservations, CSI health, finalizer progress and controller recovery are not simulated. Positive PDB allowance is not a drain guarantee.",
		"DaemonSet and mirror Pods are not relocated by a normal drain. Local volume accessibility and emptyDir data require an explicit operational decision.",
	}}
	if e := ValidateResource("Node", "", name); e != nil {
		return out, e
	}
	scope, e := s.Scope(requestedNS)
	if e != nil {
		return out, e
	}
	out.Scope = scope
	node, e := s.Provider.Get(ctx, "Node", "", name)
	if e != nil {
		return out, e
	}
	out.Node = assessmentResource(node)
	pods, complete := s.assessmentList(ctx, "Pod", scope, 5000, &out.Warnings)
	pdbs, pdbComplete := s.assessmentList(ctx, "PodDisruptionBudget", scope, 200, &out.Warnings)
	if !complete || !pdbComplete || scope != "" {
		out.Checks = append(out.Checks, check("coverage", "unknown", "This is not a complete, cluster-wide node preflight"))
	}
	if model.Bool(node, "spec", "unschedulable") {
		out.Checks = append(out.Checks, check("cordon", "observed", "Node is already marked unschedulable"))
	}
	cache := map[string]model.Object{}
	failed := map[string]bool{}
	reads := 0
	readLimited := false
	get := func(kind, ns, n string) (model.Object, bool) {
		id := model.ID(kind, ns, n)
		if o, ok := cache[id]; ok {
			return o, true
		}
		if failed[id] {
			return nil, false
		}
		if e := ValidateResource(kind, ns, n); e != nil {
			failed[id] = true
			return nil, false
		}
		if ns != "" {
			if _, e := s.Scope(ns); e != nil {
				failed[id] = true
				return nil, false
			}
		}
		if reads >= 100 {
			if !readLimited {
				out.Warnings = append(out.Warnings, model.Warning{Kind: "Maintenance", Message: "Reference lookups limited to 100"})
				readLimited = true
			}
			return nil, false
		}
		reads++
		o, e := s.Provider.Get(ctx, kind, ns, n)
		if e != nil {
			failed[id] = true
			out.Warnings = append(out.Warnings, model.Warning{Kind: kind, Message: e.Error()})
			return nil, false
		}
		cache[id] = o
		return o, true
	}
	budgetCounts := map[string]int{}
	budgetResources := map[string]model.Object{}
	for _, pod := range pods {
		if model.Str(pod, "spec", "nodeName") != name {
			continue
		}
		if len(out.Pods) >= 200 {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "Pod", Message: "Node maintenance is limited to 200 Pods"})
			break
		}
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		row := MaintenancePod{Resource: assessmentResource(pod), Class: "controller-unverified", Checks: []AssessmentCheck{}, PDBs: []model.Resource{}}
		current := pod
		verifiedController := false
		hasControllerRef := false
		for depth := 0; depth < 4; depth++ {
			found := false
			for _, raw := range model.Arr(current, "metadata", "ownerReferences") {
				ref, _ := raw.(map[string]any)
				if !model.Bool(ref, "controller") {
					continue
				}
				hasControllerRef = true
				parent, ok := get(model.Str(ref, "kind"), row.Resource.Namespace, model.Str(ref, "name"))
				if !ok || !controlledBy(current, parent) {
					row.Checks = append(row.Checks, check("controller", "unknown", "Controller reference could not be verified against a readable current UID"))
					break
				}
				r := assessmentResource(parent)
				row.Controller = &r
				verifiedController = true
				current = parent
				found = true
				if r.Kind == "DaemonSet" {
					row.Class = "daemonset"
					break
				}
			}
			if !found || row.Class == "daemonset" {
				break
			}
		}
		_, mirror := model.Map(pod, "metadata", "annotations")["kubernetes.io/config.mirror"]
		terminal := model.Str(pod, "status", "phase") == "Succeeded" || model.Str(pod, "status", "phase") == "Failed"
		if mirror {
			row.Class = "mirror"
			row.Checks = append(row.Checks, check("mirror", "advisory", "Static/mirror Pod is skipped by normal drain; control its node-local definition separately"))
		} else if terminal {
			row.Class = "terminal"
			row.Checks = append(row.Checks, check("terminal", "observed", "Pod is finished; default unmanaged and emptyDir drain filters do not block terminal Pods"))
		} else if row.Class == "daemonset" {
			row.Checks = append(row.Checks, check("daemonset", "advisory", "DaemonSet Pod is skipped rather than relocated; default kubectl drain requires an explicit ignore-daemonsets decision"))
		} else if !hasControllerRef {
			row.Class = "unmanaged"
			row.Checks = append(row.Checks, check("controller", "constraint", "No controller owner is declared; default drain does not remove unmanaged Pods without an explicit force decision"))
		} else if verifiedController {
			row.Class = "managed"
		}
		if model.Str(pod, "metadata", "deletionTimestamp") != "" {
			row.Checks = append(row.Checks, check("termination", "advisory", "Pod is already terminating; deletion timestamp is not proof that it will finish"))
		}
		if len(model.Arr(pod, "metadata", "finalizers")) > 0 {
			row.Checks = append(row.Checks, check("finalizers", "advisory", "Pod has finalizers; completion depends on their controllers"))
		}
		for i, raw := range model.Arr(pod, "spec", "volumes") {
			if i >= 16 {
				row.Checks = append(row.Checks, check("storage", "unknown", "Only the first 16 Pod volumes are assessed"))
				break
			}
			v, _ := raw.(map[string]any)
			if model.Value(v, "emptyDir") != nil {
				state := "constraint"
				message := "emptyDir data is node-local; default drain requires an explicit data-deletion decision"
				if mirror || row.Class == "daemonset" || terminal {
					state = "advisory"
					message = "emptyDir data is node-local; this Pod is skipped or terminal under default drain filters"
				}
				row.Checks = append(row.Checks, check("emptydir", state, message))
			}
			if model.Value(v, "hostPath") != nil {
				row.Checks = append(row.Checks, check("hostpath", "advisory", "hostPath depends on this node; replacement Pod accessibility is not established"))
			}
			if model.Value(v, "ephemeral") != nil {
				row.Checks = append(row.Checks, check("ephemeral-storage", "unknown", "Generic ephemeral volume lifecycle is not evaluated"))
			}
			if claim := model.Str(v, "persistentVolumeClaim", "claimName"); claim != "" {
				pvc, ok := get("PersistentVolumeClaim", row.Resource.Namespace, claim)
				if !ok {
					row.Checks = append(row.Checks, check("pvc", "unknown", "Referenced PVC is unavailable"))
					continue
				}
				pvName := model.Str(pvc, "spec", "volumeName")
				if pvName == "" {
					row.Checks = append(row.Checks, check("pvc", "unknown", claim+": no bound volume is observed"))
					continue
				}
				pv, ok := get("PersistentVolume", "", pvName)
				if !ok {
					row.Checks = append(row.Checks, check("pv", "unknown", claim+": bound PV is unavailable"))
					continue
				}
				if model.Str(pv, "spec", "claimRef", "uid") == "" || model.Str(pv, "spec", "claimRef", "uid") != model.Str(pvc, "metadata", "uid") || model.Str(pv, "spec", "claimRef", "namespace") != row.Resource.Namespace || model.Str(pv, "spec", "claimRef", "name") != claim {
					row.Checks = append(row.Checks, check("pv", "unknown", claim+": binding identity is mismatched"))
					continue
				}
				if model.Value(pv, "spec", "local") != nil {
					row.Checks = append(row.Checks, check("local-volume", "advisory", claim+": PV uses local storage; availability after node maintenance is not established"))
				}
				if model.Value(pv, "spec", "nodeAffinity") != nil {
					row.Checks = append(row.Checks, check("volume-affinity", "advisory", claim+": PV has node affinity restricting replacement placement"))
				}
			}
		}
		if !mirror && row.Class != "daemonset" && !terminal {
			selectorCoverage := pdbComplete
			matching := []model.Object{}
			matchingTruncated := false
			for _, pdb := range pdbs {
				matched, known := matchBudget(pdb, pod)
				if !known {
					selectorCoverage = false
					row.Checks = append(row.Checks, check("pdb-selector", "unknown", "An unreadable selector prevents complete PDB matching"))
					continue
				}
				if matched {
					if len(matching) >= 20 {
						matchingTruncated = true
						continue
					}
					matching = append(matching, pdb)
					r := assessmentResource(pdb)
					row.PDBs = append(row.PDBs, r)
					budgetCounts[r.ID]++
					budgetResources[r.ID] = pdb
				}
			}
			if matchingTruncated {
				selectorCoverage = false
				row.Checks = append(row.Checks, check("pdb-coverage", "unknown", "Only the first 20 matching PDBs are retained for this Pod"))
			}
			if len(matching) > 1 {
				state := "advisory"
				if model.Str(pod, "status", "phase") == "Running" {
					state = "constraint"
				}
				row.Checks = append(row.Checks, check("pdb-overlap", state, "Multiple PDBs select this Pod; eviction may be rejected because overlapping PDBs are unsupported"))
			}
			for _, pdb := range matching {
				row.Checks = append(row.Checks, budgetCheck(pdb, pod))
			}
			if !selectorCoverage {
				row.Checks = append(row.Checks, check("pdb-coverage", "unknown", "PDB inventory is incomplete"))
			} else if len(matching) == 0 {
				row.Checks = append(row.Checks, check("pdb", "observed", "No matching PDB observed in the complete readable collection; availability is not protected by this check"))
			}
		}
		for _, c := range row.Checks {
			if c.State == "constraint" {
				out.State = "constraints-observed"
			}
		}
		out.Pods = append(out.Pods, row)
	}
	for id, n := range budgetCounts {
		pdb := budgetResources[id]
		budget := count(pdb, "status", "disruptionsAllowed")
		out.Budgets = append(out.Budgets, MaintenanceBudget{Resource: assessmentResource(pdb), TargetPods: n, DisruptionsAllowed: budget, Message: fmt.Sprintf("%d selected node Pods share this one budget. Sequential eviction progress depends on controller recovery; per-Pod allowances must not be added.", n)})
	}
	sort.Slice(out.Budgets, func(i, j int) bool { return out.Budgets[i].Resource.ID < out.Budgets[j].Resource.ID })
	return out, ctx.Err()
}
