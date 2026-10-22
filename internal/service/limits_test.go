package service

import (
	"context"
	"math"
	"testing"
	"timone/internal/model"
	"timone/internal/provider"
)

func limited(cpu, memory string, sidecar bool) map[string]any {
	limits := map[string]any{}
	if cpu != "" {
		limits["cpu"] = cpu
	}
	if memory != "" {
		limits["memory"] = memory
	}
	c := map[string]any{"name": "container", "resources": map[string]any{"limits": limits}}
	if sidecar {
		c["restartPolicy"] = "Always"
	}
	return c
}
func boundedPod() model.Object {
	return model.Object{"metadata": map[string]any{"name": "bounded", "namespace": "production"}, "spec": map[string]any{"nodeName": "node-a", "containers": []any{limited("1", "128Mi", false)}}, "status": map[string]any{"phase": "Running"}}
}

func TestConfiguredLimitsAccountForInitOrderSidecarsAndOverhead(t *testing.T) {
	p := model.Object{"spec": map[string]any{"containers": []any{limited("500m", "64Mi", false), limited("250m", "128Mi", false)}, "initContainers": []any{limited("200m", "32Mi", true), limited("2", "2Gi", false), limited("100m", "64Mi", true)}, "overhead": map[string]any{"cpu": "50m", "memory": "16Mi"}}}
	for _, tc := range []struct {
		res  string
		want float64
	}{{"cpu", 2.25}, {"memory", 2096 * (1 << 20)}} {
		got, state := configuredLimit(p, tc.res)
		if state != "finite" || got == nil || math.Abs(*got-tc.want) > 1e-9 {
			t.Fatal(tc.res, got, state)
		}
	}
	model.Map(p, "spec")["resources"] = map[string]any{"limits": map[string]any{"cpu": "3", "memory": "3Gi"}}
	for _, tc := range []struct {
		res  string
		want float64
	}{{"cpu", 3.05}, {"memory", 3088 * (1 << 20)}} {
		got, state := configuredLimit(p, tc.res)
		if state != "finite" || got == nil || math.Abs(*got-tc.want) > 1e-9 {
			t.Fatal(tc.res, got, state)
		}
	}
}

func TestConfiguredLimitsDistinguishMissingZeroInvalidAndPodOverrides(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(model.Object)
		state  string
	}{
		{"no containers", func(p model.Object) { delete(model.Map(p, "spec"), "containers") }, "unknown"},
		{"missing app", func(p model.Object) {
			model.Map(p, "spec")["containers"] = []any{limited("1", "128Mi", false), limited("", "128Mi", false)}
		}, "incomplete"},
		{"zero app", func(p model.Object) {
			model.Map(p, "spec")["containers"] = []any{limited("0", "128Mi", false)}
			model.Map(p, "spec")["overhead"] = map[string]any{"cpu": "1"}
		}, "incomplete"},
		{"missing init", func(p model.Object) { model.Map(p, "spec")["initContainers"] = []any{limited("", "128Mi", false)} }, "incomplete"},
		{"missing sidecar", func(p model.Object) { model.Map(p, "spec")["initContainers"] = []any{limited("", "128Mi", true)} }, "incomplete"},
		{"bad quantity", func(p model.Object) {
			model.Map(p, "spec")["containers"] = []any{limited("secret-marker", "128Mi", false)}
		}, "unknown"},
		{"negative quantity", func(p model.Object) { model.Map(p, "spec")["containers"] = []any{limited("-1", "128Mi", false)} }, "unknown"},
		{"malformed container", func(p model.Object) { model.Map(p, "spec")["containers"] = []any{"bad"} }, "unknown"},
		{"bad overhead", func(p model.Object) { model.Map(p, "spec")["overhead"] = map[string]any{"cpu": "bad"} }, "unknown"},
		{"bad Pod budget", func(p model.Object) {
			model.Map(p, "spec")["resources"] = map[string]any{"limits": map[string]any{"cpu": "bad"}}
		}, "unknown"},
		{"zero Pod budget", func(p model.Object) {
			model.Map(p, "spec")["resources"] = map[string]any{"limits": map[string]any{"cpu": "0"}}
		}, "incomplete"},
		{"finite Pod covers missing containers", func(p model.Object) {
			model.Map(p, "spec")["containers"] = []any{limited("", "128Mi", false)}
			model.Map(p, "spec")["resources"] = map[string]any{"limits": map[string]any{"cpu": "2"}}
		}, "finite"},
		{"bad container remains unknown despite Pod budget", func(p model.Object) {
			model.Map(p, "spec")["containers"] = []any{limited("bad", "128Mi", false)}
			model.Map(p, "spec")["resources"] = map[string]any{"limits": map[string]any{"cpu": "2"}}
		}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := boundedPod()
			tc.mutate(p)
			got, state := configuredLimit(p, "cpu")
			if state != tc.state || (got != nil) != (state == "finite") {
				t.Fatal(got, state)
			}
		})
	}
}

func TestLimitAggregationSeparatesFiniteMissingInvalidDimensions(t *testing.T) {
	pods := []model.Object{boundedPod(), boundedPod(), boundedPod()}
	model.Map(pods[1], "metadata")["name"] = "missing-cpu"
	model.Map(pods[1], "spec")["containers"] = []any{limited("", "256Mi", false)}
	model.Map(pods[2], "metadata")["name"] = "invalid-memory"
	model.Map(pods[2], "spec")["containers"] = []any{limited("2", "bad", false)}
	p := observedProvider{Provider: provider.NewDemo(), objects: map[string][]model.Object{"Pod": pods, "Node": {{"metadata": map[string]any{"name": "node-a"}, "status": map[string]any{"allocatable": map[string]any{"cpu": "2", "memory": "1Gi"}}}}}}
	m, err := New(p).Metrics(context.Background(), "")
	if err != nil || len(m.Allocations) != 1 {
		t.Fatal(m, err)
	}
	a := m.Allocations[0]
	if a.CPULimits.State != "incomplete" || a.CPULimits.Total != nil || *a.CPULimits.KnownSubtotal != 3 || *a.CPULimits.FinitePods != 2 || *a.CPULimits.MissingPods != 1 || *a.CPULimits.InvalidPods != 0 {
		t.Fatal(a.CPULimits)
	}
	if a.MemoryLimits.State != "unknown" || a.MemoryLimits.Total != nil || *a.MemoryLimits.KnownSubtotal != 384*(1<<20) || *a.MemoryLimits.FinitePods != 2 || *a.MemoryLimits.InvalidPods != 1 {
		t.Fatal(a.MemoryLimits)
	}
	if a.CPURequest == nil || a.MemoryRequest == nil || *a.Pods != 3 {
		t.Fatal("limit uncertainty corrupted independent requests/counts", a)
	}
}

func TestLimitAggregationExcludesTerminalPodsAndSeparatesUnscheduled(t *testing.T) {
	pods := []model.Object{boundedPod(), boundedPod(), boundedPod(), boundedPod()}
	for i, phase := range []string{"Running", "Pending", "Succeeded", "Failed"} {
		model.Map(pods[i], "status")["phase"] = phase
		model.Map(pods[i], "metadata")["name"] = phase
	}
	delete(model.Map(pods[1], "spec"), "nodeName")
	p := observedProvider{Provider: provider.NewDemo(), objects: map[string][]model.Object{"Pod": pods, "Node": {{"metadata": map[string]any{"name": "idle"}}}}}
	m, err := New(p).Metrics(context.Background(), "")
	if err != nil || len(m.Allocations) != 3 {
		t.Fatal(m, err)
	}
	for _, a := range m.Allocations {
		want := 1.0
		if a.Node == "idle" {
			want = 0
		}
		if a.CPULimits.State != "finite" || a.CPULimits.Total == nil || *a.CPULimits.Total != want || *a.Pods != int(want) {
			t.Fatal(a)
		}
		if a.CPUAllocatable != nil || a.MemoryAllocatable != nil {
			t.Fatal("missing capacity became available")
		}
	}
}

func TestIncompletePodInventoryHidesLimitTotalsAndCoverageCounts(t *testing.T) {
	for _, truncated := range []bool{false, true} {
		p := observedProvider{Provider: provider.NewDemo(), deniedPods: !truncated, truncatedPods: truncated, objects: map[string][]model.Object{"Pod": {boundedPod()}, "Node": {{"metadata": map[string]any{"name": "node-a"}}}}}
		m, err := New(p).Metrics(context.Background(), "")
		if err != nil || len(m.Allocations) != 1 {
			t.Fatal(m, err)
		}
		for _, b := range []model.LimitBudget{m.Allocations[0].CPULimits, m.Allocations[0].MemoryLimits} {
			if b.State != "unavailable" || b.Total != nil || b.KnownSubtotal != nil || b.FinitePods != nil || b.MissingPods != nil || b.InvalidPods != nil {
				t.Fatal("incomplete reads became known limits", b)
			}
		}
	}
}

type limitMetricsProvider struct {
	provider.Provider
	podReads, nodeReads int
}

func (p *limitMetricsProvider) List(ctx context.Context, kind, ns string) ([]model.Object, bool, error) {
	if kind == "Pod" {
		p.podReads++
		if ns != "production" {
			return nil, false, &provider.Error{Code: 403, Message: "wrong scope"}
		}
		return []model.Object{boundedPod()}, false, nil
	}
	if kind == "Node" {
		p.nodeReads++
		return nil, false, &provider.Error{Code: 403, Message: "Node reads denied"}
	}
	return nil, false, nil
}
func (p *limitMetricsProvider) Namespace() string { return "production" }
func (p *limitMetricsProvider) Metrics(context.Context, string) (model.Metrics, error) {
	return model.Metrics{Available: false, Source: "metrics-server", Items: []model.Metric{}, Message: "Metrics API unavailable."}, nil
}

func TestLimitBudgetsNeedNoMetricsServerAndRespectNamespaceCeiling(t *testing.T) {
	p := &limitMetricsProvider{Provider: provider.NewDemo()}
	s := New(p)
	m, err := s.Metrics(context.Background(), "")
	if err != nil || m.Available || len(m.Allocations) != 1 {
		t.Fatal(m, err)
	}
	a := m.Allocations[0]
	if a.Scope != "namespace production only" || a.CPULimits.Total == nil || *a.CPULimits.Total != 1 || a.CPUAllocatable != nil || p.podReads != 1 || p.nodeReads != 1 {
		t.Fatal(a, p)
	}
	if _, err := s.Metrics(context.Background(), "staging"); provider.Status(err) != 403 || p.podReads != 1 {
		t.Fatal("namespace bypass", err)
	}
}
