package service

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/api/resource"
	"math"
	"sort"
	"time"
	"timone/internal/model"
)

func quantity(o map[string]any, path ...string) *float64 {
	s := model.Str(o, path...)
	if s == "" {
		return nil
	}
	q, err := resource.ParseQuantity(s)
	if err != nil || q.Sign() < 0 {
		return nil
	}
	f := q.AsApproximateFloat64()
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return &f
}
func value(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// configuredRequest uses current Pod spec only: running sidecar init containers,
// sequential init peaks, Pod-level overrides and Pod overhead are accounted for.
// It is not a scheduler simulation (resize status and admission policies can differ).
func configuredRequest(p model.Object, res string) *float64 {
	sum, sidecars, peak := 0.0, 0.0, 0.0
	request := func(c map[string]any) (float64, bool) {
		if model.Value(c, "resources", "requests", res) == nil {
			return 0, true
		}
		q := quantity(c, "resources", "requests", res)
		return value(q), q != nil
	}
	for _, v := range model.Arr(p, "spec", "containers") {
		c, _ := v.(map[string]any)
		q, ok := request(c)
		if !ok {
			return nil
		}
		sum += q
	}
	for _, v := range model.Arr(p, "spec", "initContainers") {
		c, _ := v.(map[string]any)
		q, ok := request(c)
		if !ok {
			return nil
		}
		if model.Str(c, "restartPolicy") == "Always" {
			sidecars += q
			peak = math.Max(peak, sidecars)
		} else {
			peak = math.Max(peak, sidecars+q)
		}
	}
	result := math.Max(sum+sidecars, peak)
	if model.Value(p, "spec", "resources", "requests", res) != nil {
		q := quantity(p, "spec", "resources", "requests", res)
		if q == nil {
			return nil
		}
		result = *q
	}
	if model.Value(p, "spec", "overhead", res) != nil {
		q := quantity(p, "spec", "overhead", res)
		if q == nil {
			return nil
		}
		result += *q
	}
	if math.IsInf(result, 0) || math.IsNaN(result) {
		return nil
	}
	return &result
}
func (s *Service) metrics(ctx context.Context, ns string) (model.Metrics, error) {
	out, err := s.Provider.Metrics(ctx, ns)
	if err != nil {
		return out, err
	}
	out.Warnings = []model.Warning{}
	out.Allocations = []model.Allocation{}
	pods, truncated, podErr := s.Provider.List(ctx, "Pod", ns)
	nodes, nodeTruncated, nodeErr := s.Provider.List(ctx, "Node", "")
	if podErr != nil {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Pod", Message: podErr.Error()})
	}
	if truncated {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Pod", Message: "Pod collection truncated; request and limit aggregates unavailable. Select a namespace."})
	}
	if nodeErr != nil {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Node", Message: nodeErr.Error()})
	}
	if nodeTruncated {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Node", Message: "Node collection truncated; only returned nodes have allocatable capacity."})
	}
	byID := map[string]model.Object{}
	allocations := map[string]*model.Allocation{}
	scope := "all readable namespaces"
	if ns != "" {
		scope = "namespace " + ns + " only"
	}
	get := func(name string) *model.Allocation {
		a := allocations[name]
		if a == nil {
			cpu, mem := 0.0, 0.0
			pods := 0
			a = &model.Allocation{Node: name, Scope: scope, Pods: &pods, CPURequest: &cpu, MemoryRequest: &mem, CPULimits: newLimitBudget(), MemoryLimits: newLimitBudget()}
			allocations[name] = a
		}
		return a
	}
	for _, n := range nodes {
		a := get(model.Str(n, "metadata", "name"))
		a.CPUAllocatable = quantity(n, "status", "allocatable", "cpu")
		a.MemoryAllocatable = quantity(n, "status", "allocatable", "memory")
	}
	for _, p := range pods {
		byID[model.ID("Pod", model.Str(p, "metadata", "namespace"), model.Str(p, "metadata", "name"))] = p
		phase := model.Str(p, "status", "phase")
		if phase == "Succeeded" || phase == "Failed" {
			continue
		}
		a := get(model.Str(p, "spec", "nodeName"))
		*a.Pods++
		addLimitBudget(&a.CPULimits, p, "cpu")
		addLimitBudget(&a.MemoryLimits, p, "memory")
		cpu, mem := configuredRequest(p, "cpu"), configuredRequest(p, "memory")
		if cpu == nil || mem == nil {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "Pod", Message: fmt.Sprintf("Invalid request quantity on %s/%s; affected node aggregate unavailable", model.Str(p, "metadata", "namespace"), model.Str(p, "metadata", "name"))})
		}
		if cpu == nil || a.CPURequest == nil {
			a.CPURequest = nil
		} else {
			*a.CPURequest += *cpu
		}
		if mem == nil || a.MemoryRequest == nil {
			a.MemoryRequest = nil
		} else {
			*a.MemoryRequest += *mem
		}
	}
	for _, a := range allocations {
		finalizeLimitBudget(&a.CPULimits, podErr != nil || truncated)
		finalizeLimitBudget(&a.MemoryLimits, podErr != nil || truncated)
		if podErr != nil || truncated {
			a.CPURequest = nil
			a.MemoryRequest = nil
			a.Pods = nil
		}
		out.Allocations = append(out.Allocations, *a)
	}
	sort.Slice(out.Allocations, func(i, j int) bool { return out.Allocations[i].Node < out.Allocations[j].Node })
	for i := range out.Items {
		m := &out.Items[i]
		if m.Kind != "Pod" {
			continue
		}
		p := byID[model.ID("Pod", m.Namespace, m.Name)]
		if p == nil {
			continue
		}
		containers := append(append([]any{}, model.Arr(p, "spec", "containers")...), model.Arr(p, "spec", "initContainers")...)
		for _, v := range containers {
			c, _ := v.(map[string]any)
			if m.Container != model.Str(c, "name") {
				continue
			}
			m.CPURequest = quantity(c, "resources", "requests", "cpu")
			m.MemoryRequest = quantity(c, "resources", "requests", "memory")
			m.CPULimit = quantity(c, "resources", "limits", "cpu")
			m.MemoryLimit = quantity(c, "resources", "limits", "memory")
		}
	}
	out.Message += " Request aggregates use non-terminal Pod specs, init sidecar/peak requests, Pod-level overrides and overhead. These are configured demand, not measured usage or resize-aware scheduler allocations. Namespace filters show only that namespace's demand. Limit totals require finite positive budgets for every observed non-terminal Pod; missing/zero limits and invalid quantities are separate coverage states. Known limit subtotals include fully bounded Pods only and do not bound the remaining Pods. Configured limits may exceed allocatable capacity without proving usage pressure. Missing container budgets remain unavailable."
	if podErr == nil && !truncated {
		out.PodTotals = podMetrics(pods, out.Items, time.Now().UTC())
	} else {
		out.PodTotals = []model.PodMetric{}
	}
	return out, ctx.Err()
}
