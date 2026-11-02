package service

import (
	"context"
	"fmt"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type SchedulingNode struct {
	Resource model.Resource    `json:"resource"`
	State    string            `json:"state"`
	Checks   []AssessmentCheck `json:"checks"`
}
type SchedulingAssessment struct {
	Pod         model.Resource      `json:"pod"`
	ObservedAt  time.Time           `json:"observedAt"`
	State       string              `json:"state"`
	Requests    map[string]*float64 `json:"requests"`
	Checks      []AssessmentCheck   `json:"checks"`
	Nodes       []SchedulingNode    `json:"nodes"`
	Events      []model.Event       `json:"events"`
	Warnings    []model.Warning     `json:"warnings"`
	Limitations []string            `json:"limitations"`
}
type schedulingVolume struct {
	name     string
	affinity map[string]any
}

func (s *Service) Scheduling(ctx context.Context, ns, name string) (SchedulingAssessment, error) {
	ctx = provider.WithReadBudget(ctx, provider.InvestigationBytes)
	out := SchedulingAssessment{ObservedAt: time.Now().UTC(), State: "unassigned", Requests: map[string]*float64{}, Checks: []AssessmentCheck{}, Nodes: []SchedulingNode{}, Events: []model.Event{}, Warnings: []model.Warning{}, Limitations: []string{
		"Spec-based constraints, not scheduler eligibility or a scheduler simulation. Admission, custom scheduler profiles, nominated Pods, preemption, topology spread, inter-Pod affinity, extended resources, host ports and CSI attachment limits are not simulated.",
		"CPU and memory use configured requests, including init sidecars/peaks and overhead; in-place resize status and scheduler caches may differ. Namespace-restricted or partial Pod inventories cannot establish free capacity.",
		"For assigned Pods, node checks describe a new placement, not the cause of current startup failure. Pending does not always mean unschedulable. Volume observations, conditions and events may change after collection.",
	}}
	if e := ValidateResource("Pod", ns, name); e != nil {
		return out, e
	}
	if _, e := s.Scope(ns); e != nil {
		return out, e
	}
	pod, e := s.Provider.Get(ctx, "Pod", ns, name)
	if e != nil {
		return out, e
	}
	out.Pod = assessmentResource(pod)
	if model.Str(pod, "spec", "nodeName") != "" {
		out.State = "already-assigned"
		out.Checks = append(out.Checks, check("assignment", "observed", "Pod is already assigned to "+model.Str(pod, "spec", "nodeName")+"; investigate startup, probes or mounts separately"))
	}
	for _, r := range []string{"cpu", "memory"} {
		out.Requests[r] = configuredRequest(pod, r)
	}
	for _, raw := range model.Arr(pod, "status", "conditions") {
		c, _ := raw.(map[string]any)
		if model.Str(c, "type") == "PodScheduled" {
			out.Checks = append(out.Checks, check("scheduler-condition", "observed", model.Str(c, "status")+": "+model.Str(c, "reason")+"; "+model.Str(c, "message")))
		}
	}
	if scheduler := model.Str(pod, "spec", "schedulerName"); scheduler != "" && scheduler != "default-scheduler" {
		out.Checks = append(out.Checks, check("custom-scheduler", "unknown", "Selected scheduler "+scheduler+" may use additional policies"))
	}
	if model.Value(pod, "spec", "affinity", "podAffinity") != nil || model.Value(pod, "spec", "affinity", "podAntiAffinity") != nil {
		out.Checks = append(out.Checks, check("inter-pod-affinity", "unknown", "Inter-Pod affinity and anti-affinity are configured but not evaluated"))
	}
	if len(model.Arr(pod, "spec", "topologySpreadConstraints")) > 0 {
		out.Checks = append(out.Checks, check("topology-spread", "unknown", "Topology spread constraints require cluster-wide domain and Pod coverage"))
	}
	nodes, _ := s.assessmentList(ctx, "Node", "", 200, &out.Warnings)
	pods, podsComplete := s.assessmentList(ctx, "Pod", s.Provider.Namespace(), 5000, &out.Warnings)
	capacityComplete := podsComplete && s.Provider.Namespace() == ""
	used := map[string]map[string]float64{}
	known := map[string]map[string]bool{}
	counts := map[string]int{}
	for _, p := range pods {
		node := model.Str(p, "spec", "nodeName")
		phase := model.Str(p, "status", "phase")
		if node == "" || phase == "Succeeded" || phase == "Failed" || (model.Str(pod, "metadata", "uid") != "" && model.Str(p, "metadata", "uid") == model.Str(pod, "metadata", "uid")) {
			continue
		}
		if used[node] == nil {
			used[node] = map[string]float64{}
			known[node] = map[string]bool{"cpu": true, "memory": true}
		}
		counts[node]++
		for _, res := range []string{"cpu", "memory"} {
			q := configuredRequest(p, res)
			if q == nil {
				known[node][res] = false
			} else {
				used[node][res] += *q
			}
		}
	}
	volumes := []schedulingVolume{}
	for i, raw := range model.Arr(pod, "spec", "volumes") {
		if i >= 32 {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "Storage", Message: "Scheduling volume references limited to 32"})
			break
		}
		v, _ := raw.(map[string]any)
		claim := model.Str(v, "persistentVolumeClaim", "claimName")
		if claim == "" {
			continue
		}
		if err := ValidateResource("PersistentVolumeClaim", ns, claim); err != nil {
			out.Checks = append(out.Checks, check("volume-reference", "unknown", "Invalid claim reference"))
			continue
		}
		pvc, err := s.Provider.Get(ctx, "PersistentVolumeClaim", ns, claim)
		if err != nil {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "PersistentVolumeClaim", Message: err.Error()})
			continue
		}
		if model.Str(pvc, "status", "phase") == "Lost" {
			out.Checks = append(out.Checks, check("volume-claim", "constraint", claim+": PVC reports Lost"))
		}
		pvName := model.Str(pvc, "spec", "volumeName")
		if pvName == "" {
			class := model.Str(pvc, "spec", "storageClassName")
			if class != "" {
				if e := ValidateResource("StorageClass", "", class); e != nil {
					out.Checks = append(out.Checks, check("volume-binding", "unknown", "Invalid storage class reference"))
					continue
				}
				sc, err := s.Provider.Get(ctx, "StorageClass", "", class)
				if err == nil && model.Str(sc, "volumeBindingMode") == "WaitForFirstConsumer" {
					out.Checks = append(out.Checks, check("volume-binding", "observed", claim+": binding is deferred until a consumer is scheduled"))
					continue
				}
				if err == nil {
					out.Checks = append(out.Checks, check("volume-binding", "constraint", claim+": no bound volume is observed for immediate binding"))
					continue
				}
				if err != nil {
					out.Warnings = append(out.Warnings, model.Warning{Kind: "StorageClass", Message: err.Error()})
				}
			}
			out.Checks = append(out.Checks, check("volume-binding", "unknown", claim+": claim is unbound; binding policy is unavailable"))
			continue
		}
		if err := ValidateResource("PersistentVolume", "", pvName); err != nil {
			continue
		}
		pv, err := s.Provider.Get(ctx, "PersistentVolume", "", pvName)
		if err != nil {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "PersistentVolume", Message: err.Error()})
			continue
		}
		if model.Str(pv, "spec", "claimRef", "uid") == "" || model.Str(pv, "spec", "claimRef", "uid") != model.Str(pvc, "metadata", "uid") || model.Str(pv, "spec", "claimRef", "name") != claim || model.Str(pv, "spec", "claimRef", "namespace") != ns {
			out.Checks = append(out.Checks, check("volume-identity", "unknown", claim+": current PV binding identity does not match"))
			continue
		}
		volumes = append(volumes, schedulingVolume{claim, model.Map(pv, "spec", "nodeAffinity", "required")})
	}
	for _, node := range nodes {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		r := assessmentResource(node)
		row := SchedulingNode{Resource: r, State: "no-blocker-in-checked-data", Checks: []AssessmentCheck{}}
		add := func(c AssessmentCheck) {
			row.Checks = append(row.Checks, c)
			if c.State == "constraint" {
				row.State = "constraints-observed"
			} else if c.State == "unknown" && row.State != "constraints-observed" {
				row.State = "incomplete"
			}
		}
		for _, c := range out.Checks {
			if c.State == "constraint" || c.State == "unknown" {
				add(c)
			}
		}
		if assigned := model.Str(pod, "spec", "nodeName"); assigned != "" && assigned != r.Name {
			add(check("assignment", "constraint", "Pod spec is assigned to a different node"))
		}
		if model.Bool(node, "spec", "unschedulable") && !tolerates(pod, map[string]any{"key": "node.kubernetes.io/unschedulable", "effect": "NoSchedule"}) {
			add(check("cordon", "constraint", "Node is marked unschedulable"))
		}
		selectorMatches := true
		for key, expected := range model.Map(pod, "spec", "nodeSelector") {
			if actual, exists := model.Map(node, "metadata", "labels")[key]; !exists || fmt.Sprint(actual) != fmt.Sprint(expected) {
				selectorMatches = false
			}
		}
		if !selectorMatches {
			add(check("node-selector", "constraint", "Node labels do not match spec.nodeSelector"))
		} else {
			add(check("node-selector", "observed", "Declared nodeSelector matches observed labels"))
		}
		if matched, known := requiredNodeSelector(model.Map(pod, "spec", "affinity", "nodeAffinity", "requiredDuringSchedulingIgnoredDuringExecution"), node); !known {
			add(check("node-affinity", "unknown", "Required node affinity contains unsupported requirements"))
		} else if !matched {
			add(check("node-affinity", "constraint", "Required node affinity does not match"))
		}
		for _, raw := range model.Arr(node, "spec", "taints") {
			t, _ := raw.(map[string]any)
			if !tolerates(pod, t) {
				effect := model.Str(t, "effect")
				state := "advisory"
				if effect == "NoSchedule" || effect == "NoExecute" {
					state = "constraint"
				}
				add(check("taint", state, model.Str(t, "key")+": "+effect+" is not tolerated"))
			}
		}
		for _, res := range []string{"cpu", "memory"} {
			request, alloc := out.Requests[res], quantity(node, "status", "allocatable", res)
			if request == nil || alloc == nil {
				add(check("capacity-"+res, "unknown", "Request or allocatable "+res+" quantity is unavailable"))
				continue
			}
			subtotal := used[r.Name][res]
			if *request+subtotal > *alloc {
				add(check("capacity-"+res, "constraint", fmt.Sprintf("Configured %s demand %.3f plus this Pod %.3f exceeds allocatable %.3f; not a resize-aware scheduler allocation", res, subtotal, *request, *alloc)))
			} else if !capacityComplete || (known[r.Name] != nil && !known[r.Name][res]) {
				add(check("capacity-"+res, "unknown", "Observed requests fit allocatable, but full node demand is not established"))
			} else {
				add(check("capacity-"+res, "observed", fmt.Sprintf("Configured %s demand including this Pod %.3f / allocatable %.3f", res, subtotal+*request, *alloc)))
			}
		}
		if max := quantity(node, "status", "allocatable", "pods"); max != nil && float64(counts[r.Name]+1) > *max {
			add(check("pod-count", "constraint", "Observed Pod count plus this Pod exceeds allocatable Pod slots"))
		}
		for _, volume := range volumes {
			if matched, known := requiredNodeSelector(volume.affinity, node); !known {
				add(check("volume-affinity", "unknown", volume.name+": unsupported PV node affinity"))
			} else if !matched {
				add(check("volume-affinity", "constraint", volume.name+": PV node affinity does not match this node"))
			}
		}
		out.Nodes = append(out.Nodes, row)
	}
	events, err := s.Provider.Events(ctx, ns)
	if err != nil {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Event", Message: err.Error()})
	} else {
		for _, event := range events {
			if model.EventMatches(event, out.Pod) {
				out.Events = append(out.Events, event)
				if len(out.Events) >= 200 {
					break
				}
			}
		}
	}
	return out, ctx.Err()
}
