package service

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"timone/internal/model"
)

func operationalSignals(r model.Resource) []signal {
	out := []signal{}
	add := func(title, evidence, step string) {
		out = append(out, signal{title: title, severity: "warning", evidence: []string{evidence}, causes: []string{}, steps: []string{step}, priority: 35})
	}
	switch r.Kind {
	case "HorizontalPodAutoscaler":
		if model.Value(r.Object, "status", "observedGeneration") != nil && model.Num(r.Object, "status", "observedGeneration") < model.Num(r.Object, "metadata", "generation") {
			return out
		}
		for _, raw := range model.Arr(r.Object, "status", "conditions") {
			c, _ := raw.(map[string]any)
			if (model.Str(c, "type") == "AbleToScale" || model.Str(c, "type") == "ScalingActive") && model.Str(c, "status") == "False" {
				add("Autoscaler cannot scale", model.Str(c, "reason")+": "+model.Str(c, "message"), "Inspect the target, metrics API and autoscaler conditions.")
			}
			if model.Str(c, "type") == "ScalingLimited" && model.Str(c, "status") == "True" {
				add("Autoscaler constrained", model.Str(c, "reason")+": "+model.Str(c, "message"), "Compare desired replicas, configured bounds and current demand.")
			}
		}
	case "ResourceQuota":
		for name, raw := range model.Map(r.Object, "status", "hard") {
			hard, e := resource.ParseQuantity(fmt.Sprint(raw))
			if e != nil || hard.Sign() <= 0 {
				continue
			}
			used, e := resource.ParseQuantity(model.Str(r.Object, "status", "used", name))
			if e == nil && used.Cmp(hard) >= 0 {
				add("Resource quota exhausted", name+": "+used.String()+" / "+hard.String(), "Inspect rejected creation events and namespace demand. Quota exhaustion does not prove an existing workload is failing.")
			}
		}
	}
	return out
}

// OperationalContext returns bounded, authorized related resources and coverage.
// It never treats missing reads as proof that a dependency is absent.
func (s *Service) OperationalContext(ctx context.Context, kind, ns, name string) ([]model.Resource, []model.Warning) {
	kinds := []string{}
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet", "Pod":
		kinds = []string{"HorizontalPodAutoscaler", "PodDisruptionBudget", "ResourceQuota", "LimitRange"}
	case "Service":
		kinds = []string{"EndpointSlice"}
	default:
		return nil, nil
	}
	target, err := s.Provider.Get(ctx, kind, ns, name)
	if err != nil {
		return nil, []model.Warning{{Kind: kind, Message: err.Error()}}
	}
	podLabels := model.Map(target, "spec", "template", "metadata", "labels")
	if kind == "Pod" {
		podLabels = model.Map(target, "metadata", "labels")
	}
	set := labels.Set{}
	for k, v := range podLabels {
		if text, ok := v.(string); ok {
			set[k] = text
		}
	}
	related := []model.Resource{}
	warnings := []model.Warning{}
	endpointCount := 0
	for _, k := range kinds {
		objects, truncated, err := s.Provider.List(ctx, k, ns)
		if err != nil {
			warnings = append(warnings, model.Warning{Kind: k, Message: err.Error()})
			continue
		}
		if truncated {
			warnings = append(warnings, model.Warning{Kind: k, Message: "Related resource inventory truncated"})
		}
		for _, o := range objects {
			match := k == "ResourceQuota" || k == "LimitRange"
			switch k {
			case "HorizontalPodAutoscaler":
				match = model.Str(o, "spec", "scaleTargetRef", "kind") == kind && model.Str(o, "spec", "scaleTargetRef", "name") == name
			case "PodDisruptionBudget":
				var selector v1.LabelSelector
				raw := model.Map(o, "spec", "selector")
				if raw != nil && runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &selector) == nil {
					sel, e := v1.LabelSelectorAsSelector(&selector)
					match = e == nil && sel.Matches(set)
				}
			case "EndpointSlice":
				match = model.Str(o, "metadata", "labels", "kubernetes.io/service-name") == name
				if match {
					for _, raw := range model.Arr(o, "endpoints") {
						ep, _ := raw.(map[string]any)
						if model.Value(ep, "conditions", "ready") != false && !model.Bool(ep, "conditions", "terminating") {
							endpointCount++
						}
					}
				}
			}
			if match {
				clean := model.Sanitize(o)
				related = append(related, model.Summarize(clean))
				if k == "PodDisruptionBudget" && model.Value(o, "status", "disruptionsAllowed") != nil && model.Num(o, "status", "disruptionsAllowed") == 0 {
					warnings = append(warnings, model.Warning{Kind: k, Message: "No voluntary disruptions currently allowed by " + model.Str(o, "metadata", "name") + "; this is not a workload failure."})
				}
				if len(related) >= 200 {
					warnings = append(warnings, model.Warning{Kind: k, Message: "Related resources limited to 200"})
					return related, warnings
				}
			}
		}
		if k == "EndpointSlice" && !truncated && endpointCount == 0 && model.Str(target, "spec", "type") != "ExternalName" {
			warnings = append(warnings, model.Warning{Kind: k, Message: "No ready, non-terminating endpoints observed. Published endpoint state does not establish traffic reachability."})
		}
	}
	return related, warnings
}
