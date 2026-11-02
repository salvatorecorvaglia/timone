package service

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type UsageDistribution struct {
	Metric   string            `json:"metric"`
	Unit     string            `json:"unit"`
	Samples  int               `json:"samples"`
	Expected int               `json:"expected"`
	Coverage float64           `json:"coverage"`
	Median   *float64          `json:"median"`
	P95      *float64          `json:"p95"`
	Peak     *float64          `json:"peak"`
	Request  *float64          `json:"request"`
	Limit    *float64          `json:"limit"`
	Checks   []AssessmentCheck `json:"checks"`
}
type EfficiencyContainer struct {
	Pod       model.Resource      `json:"pod"`
	Container string              `json:"container"`
	Class     string              `json:"class"`
	State     string              `json:"state"`
	Message   string              `json:"message"`
	Start     time.Time           `json:"start"`
	End       time.Time           `json:"end"`
	Step      int64               `json:"stepSeconds"`
	Identity  string              `json:"identity"`
	Usage     []UsageDistribution `json:"usage"`
}
type EfficiencyAssessment struct {
	Resource     model.Resource        `json:"resource"`
	ObservedAt   time.Time             `json:"observedAt"`
	State        string                `json:"state"`
	Range        string                `json:"range"`
	Source       string                `json:"source"`
	Start        time.Time             `json:"start"`
	End          time.Time             `json:"end"`
	CurrentPods  int                   `json:"currentPods"`
	AnalyzedPods int                   `json:"analyzedPods"`
	Containers   []EfficiencyContainer `json:"containers"`
	Checks       []AssessmentCheck     `json:"checks"`
	Warnings     []model.Warning       `json:"warnings"`
	Limitations  []string              `json:"limitations"`
}

func sampledUsage(metric string, series []model.HistorySeries, container string, spec map[string]any, start, end time.Time, step int64, comparable bool) UsageDistribution {
	unit := "cores"
	if metric == "memory" {
		unit = "bytes"
	}
	out := UsageDistribution{Metric: metric, Unit: unit, Request: quantity(spec, "resources", "requests", metric), Limit: quantity(spec, "resources", "limits", metric), Checks: []AssessmentCheck{}}
	if step <= 0 || end.Before(start) {
		out.Checks = append(out.Checks, check("coverage", "unknown", "No eligible observation window"))
		return out
	}
	out.Expected = int(end.Sub(start).Seconds())/int(step) + 1
	values := []float64{}
	seen := map[int64]bool{}
	matches := 0
	for _, s := range series {
		if s.Metric != metric || s.Container != container {
			continue
		}
		matches++
		if s.Unit != unit {
			continue
		}
		for _, p := range s.Points {
			if p.Timestamp < start.Unix() || p.Timestamp > end.Unix() || p.Value == nil || math.IsNaN(*p.Value) || math.IsInf(*p.Value, 0) || *p.Value < 0 || seen[p.Timestamp] {
				continue
			}
			seen[p.Timestamp] = true
			values = append(values, *p.Value)
		}
	}
	if matches > 1 {
		out.Checks = append(out.Checks, check("series", "unknown", "Ambiguous duplicate metric series; statistics omitted"))
		return out
	}
	sort.Float64s(values)
	out.Samples = len(values)
	out.Coverage = float64(out.Samples) / float64(out.Expected)
	if len(values) > 0 {
		median := values[(len(values)-1)/2]
		if len(values)%2 == 0 {
			median = (median + values[len(values)/2]) / 2
		}
		p95 := values[int(math.Ceil(.95*float64(len(values))))-1]
		peak := values[len(values)-1]
		out.Median = &median
		out.P95 = &p95
		out.Peak = &peak
	}
	if !comparable {
		out.Checks = append(out.Checks, check("comparison", "unknown", "Pod-level resources, an allocation mismatch or active resize status are present; container-only configuration is not an effective allocation comparison"))
	}
	if out.Samples < 10 || out.Coverage < .8 {
		out.Checks = append(out.Checks, check("coverage", "unknown", "At least 10 valid samples and 80% of the requested evaluation grid are required for usage signals"))
		return out
	}
	if !comparable {
		return out
	}
	if out.Request != nil && *out.Request > 0 {
		if *out.P95 < .25**out.Request && *out.Peak < .5**out.Request {
			out.Checks = append(out.Checks, check("low-request-utilization", "advisory", "Sampled P95 is below 25% of the current request and sampled maximum below 50%; review a representative longer period before changing configuration"))
		}
		if *out.P95 > *out.Request {
			out.Checks = append(out.Checks, check("above-request", "advisory", "Sampled P95 exceeds the current request; requests do not cap consumption"))
		}
	}
	if out.Limit != nil && *out.Limit > 0 && *out.Peak >= .9**out.Limit {
		out.Checks = append(out.Checks, check("near-limit", "advisory", "Sampled maximum reaches at least 90% of the current limit; this does not establish throttling or predict an OOM"))
	}
	return out
}
func (s *Service) Efficiency(ctx context.Context, kind, ns, name, window string) (EfficiencyAssessment, error) {
	ctx = provider.WithReadBudget(ctx, provider.InvestigationBytes)
	if window == "" {
		window = "24h"
	}
	duration, err := historyRange(&model.HistoryQuery{Kind: "Pod", Namespace: ns, Name: name, Range: window})
	if err != nil {
		return EfficiencyAssessment{}, err
	}
	root, pods, warnings, err := s.workloadPods(ctx, kind, ns, name)
	if err != nil {
		return EfficiencyAssessment{}, err
	}
	end := time.Now().UTC().Truncate(time.Minute)
	start := end.Add(-duration)
	status := s.HistoryStatus()
	out := EfficiencyAssessment{Resource: assessmentResource(root), ObservedAt: time.Now().UTC(), State: status.State, Range: window, Source: status.Source, Start: start, End: end, CurrentPods: len(pods), Containers: []EfficiencyContainer{}, Checks: []AssessmentCheck{}, Warnings: warnings, Limitations: []string{
		"Sampled observations, not automatic sizing recommendations. Median/P95/maximum describe the bounded query evaluation grid, not every scrape or instantaneous peak. CPU samples are five-minute rates; memory is working set, not RSS or an OOM forecast.",
		"Only up to four currently owned Pod identities are queried. Deleted/replaced Pods, past workload membership and historic resource configuration are not reconstructed. No workload-total percentile is calculated by adding per-container percentiles.",
		"Current container requests/limits are compared with past samples, not historical allocations. Unset quantities remain unknown. Init/ephemeral containers are identified separately; Pod-level resources, mismatched observed allocations or active resize status suppress comparison signals.",
		"Coverage is valid grid points divided by the full requested window, including identity-clipped periods and gaps. Signals require at least 10 samples and 80% coverage. A low-usage signal does not establish excess capacity or savings.",
		"History is selected by monitoring labels rather than UID, with the existing post-creation guard. Kubernetes GET authorization is required. No requests, limits, scaling or cluster objects are modified.",
	}}
	out.Checks = append(out.Checks, check("history-source", "observed", status.Message))
	if len(pods) > 4 {
		pods = pods[:4]
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Pod", Message: "Efficiency history is limited to the first four current Pods by membership order; this is not a representative workload sample"})
	}
	type result struct {
		history model.MetricHistory
		err     error
	}
	results := make([]result, len(pods))
	var wg sync.WaitGroup
	investigationWindow := InvestigationWindow{Start: start, End: end}
	for i, pod := range pods {
		wg.Add(1)
		go func(i int, pod model.Object) {
			defer wg.Done()
			results[i].history, results[i].err = s.metricHistoryWindow(ctx, model.HistoryQuery{Kind: "Pod", Namespace: ns, Name: model.Str(pod, "metadata", "name"), Range: window}, &investigationWindow)
		}(i, pod)
	}
	wg.Wait()
podLoop:
	for i, pod := range pods {
		r := results[i]
		out.AnalyzedPods++
		if r.err != nil {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "Pod", Message: model.Str(pod, "metadata", "name") + ": " + r.err.Error()})
			r.history.State = "unavailable"
			r.history.Message = r.err.Error()
		}
		if r.err == nil && (model.Str(pod, "metadata", "uid") == "" || model.Str(r.history.Resource.Object, "metadata", "uid") != model.Str(pod, "metadata", "uid")) {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "Pod", Message: "Pod identity changed during efficiency analysis; its metrics are omitted"})
			continue
		}
		out.Warnings = append(out.Warnings, r.history.Warnings...)
		if r.err == nil {
			pod = r.history.Resource.Object
		}
		comparable := model.Value(pod, "spec", "resources") == nil && model.Str(pod, "status", "resize") == ""
		for _, raw := range model.Arr(pod, "status", "conditions") {
			c, _ := raw.(map[string]any)
			if (model.Str(c, "type") == "PodResizePending" || model.Str(c, "type") == "PodResizeInProgress") && model.Str(c, "status") == "True" {
				comparable = false
			}
		}
		for _, field := range []string{"containerStatuses", "initContainerStatuses"} {
			for _, raw := range model.Arr(pod, "status", field) {
				c, _ := raw.(map[string]any)
				var configured map[string]any
				for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
					for _, raw := range model.Arr(pod, "spec", field) {
						candidate, _ := raw.(map[string]any)
						if model.Str(candidate, "name") == model.Str(c, "name") {
							configured = candidate
						}
					}
				}
				for _, metric := range []string{"cpu", "memory"} {
					desired := quantity(configured, "resources", "requests", metric)
					for _, observed := range []map[string]any{model.Map(c, "allocatedResources"), model.Map(c, "resources", "requests")} {
						if model.Value(observed, metric) != nil {
							actual := quantity(observed, metric)
							if actual == nil || desired == nil || *actual != *desired {
								comparable = false
							}
						}
					}
					if observed := model.Map(c, "resources", "limits"); model.Value(observed, metric) != nil {
						actual := quantity(observed, metric)
						desired := quantity(configured, "resources", "limits", metric)
						if actual == nil || desired == nil || *actual != *desired {
							comparable = false
						}
					}
				}
			}
		}
		for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
			for _, raw := range model.Arr(pod, "spec", field) {
				if len(out.Containers) >= 128 {
					out.Warnings = append(out.Warnings, model.Warning{Kind: "Container", Message: "Efficiency output limited to 128 containers"})
					break podLoop
				}
				c, _ := raw.(map[string]any)
				row := EfficiencyContainer{Pod: assessmentResource(pod), Container: model.Str(c, "name"), Class: field, State: r.history.State, Message: r.history.Message, Start: r.history.Start, End: r.history.End, Step: r.history.Step, Identity: r.history.Identity, Usage: []UsageDistribution{}}
				for _, metric := range []string{"cpu", "memory"} {
					row.Usage = append(row.Usage, sampledUsage(metric, r.history.Series, row.Container, c, start, end, r.history.Step, comparable))
				}
				out.Containers = append(out.Containers, row)
			}
		}
	}
	if status.State != "disabled" {
		out.State = "empty"
		for _, c := range out.Containers {
			for _, usage := range c.Usage {
				if usage.Samples > 0 {
					out.State = "observations"
				}
			}
		}
		if len(out.Warnings) > 0 {
			out.State = "partial"
		}
	}
	return out, ctx.Err()
}
