package service

import (
	"context"
	"math"
	"sort"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type HistorySource interface {
	Status() model.HistoryCapability
	Query(context.Context, model.HistoryTarget) (model.HistoryData, error)
}

func NewWithHistory(p provider.Provider, source HistorySource) *Service {
	s := New(p)
	// A demo process never adopts a configured real data source.
	if p.Mode() != "demo" {
		s.history = source
	}
	return s
}
func (s *Service) HistoryStatus() model.HistoryCapability {
	if s.history == nil {
		return model.HistoryCapability{State: "disabled", Source: "prometheus", Message: "Historical metrics are disabled. An administrator must configure an explicit Prometheus endpoint, resource labels and optional credentials. Metrics Server does not retain history."}
	}
	return s.history.Status()
}
func historyRange(q *model.HistoryQuery) (time.Duration, error) {
	if q.Kind != "Pod" && q.Kind != "Node" {
		return 0, &provider.Error{Code: 400, Message: "Historical metrics support Pod or Node"}
	}
	if err := ValidateResource(q.Kind, q.Namespace, q.Name); err != nil {
		return 0, err
	}
	if q.Container != "" && (q.Kind != "Pod" || len(q.Container) > 63 || !validContainer(q.Container)) {
		return 0, &provider.Error{Code: 400, Message: "Invalid historical container selection"}
	}
	if q.Range == "" {
		q.Range = "1h"
	}
	switch q.Range {
	case "1h":
		return time.Hour, nil
	case "6h":
		return 6 * time.Hour, nil
	case "24h":
		return 24 * time.Hour, nil
	}
	return 0, &provider.Error{Code: 400, Message: "History range must be 1h, 6h or 24h"}
}
func validContainer(name string) bool {
	q := Query{Namespace: name}
	return q.Validate() == nil
}
func (s *Service) MetricHistory(ctx context.Context, q model.HistoryQuery) (model.MetricHistory, error) {
	duration, err := historyRange(&q)
	if err != nil {
		return model.MetricHistory{}, err
	}
	if q.Kind == "Pod" {
		if _, err := s.Scope(q.Namespace); err != nil {
			return model.MetricHistory{}, err
		}
	}
	// Fresh resource GET is the authorization boundary even when history is
	// configured. An external monitoring credential cannot bypass Kubernetes RBAC.
	o, err := s.Provider.Get(ctx, q.Kind, q.Namespace, q.Name)
	if err != nil {
		return model.MetricHistory{}, err
	}
	end := time.Now().UTC().Truncate(time.Minute)
	start := end.Add(-duration)
	step := int64(math.Max(1, math.Ceil(duration.Seconds()/240/60))) * 60
	status := s.HistoryStatus()
	out := model.MetricHistory{HistoryData: model.HistoryData{State: status.State, Message: status.Message, Series: []model.HistorySeries{}, Warnings: []model.Warning{}}, Source: status.Source, Mode: s.Provider.Mode(), Resource: model.Summarize(o), Range: q.Range, Start: start, End: end, Step: step}
	containers := []string{}
	if q.Kind == "Pod" {
		seen := map[string]bool{}
		for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
			for _, raw := range model.Arr(o, "spec", field) {
				c, _ := raw.(map[string]any)
				name := model.Str(c, "name")
				if name != "" && validContainer(name) && !seen[name] {
					seen[name] = true
					containers = append(containers, name)
				}
			}
		}
		if q.Container != "" && !seen[q.Container] {
			return out, &provider.Error{Code: 400, Message: "Container is not present in the current Pod specification"}
		}
		if len(containers) == 0 || len(containers) > 64 {
			out.State = "unavailable"
			out.Message = "Current Pod container metadata is unavailable or exceeds the 64-container history limit."
			return out, nil
		}
		sort.Strings(containers)
	}
	if s.history == nil {
		return out, nil
	}
	created, err := time.Parse(time.RFC3339, model.Str(o, "metadata", "creationTimestamp"))
	if err != nil {
		out.State = "unavailable"
		out.Message = "Resource creation time is unavailable; historical identity bounds cannot be established."
		return out, nil
	}
	// Wait one rate/lookback window beyond current object creation. This avoids
	// knowingly including pre-creation rates or normal stale name-reuse samples.
	earliest := created.UTC().Add(5 * time.Minute).Truncate(time.Minute).Add(time.Minute)
	if earliest.After(start) {
		start = earliest
		out.Start = start
	}
	if start.After(end) {
		out.State = "empty"
		out.Message = "History begins after a five-minute rate/lookback window following resource creation. No eligible samples are available yet."
		return out, nil
	}
	target := model.HistoryTarget{HistoryQuery: q, Containers: containers, Start: start, End: end, Step: step}
	data, err := s.history.Query(ctx, target)
	if err != nil {
		return out, err
	}
	out.HistoryData = data
	out.Identity = "History matches configured resource labels rather than Kubernetes UID. The window starts after current object creation plus the five-minute rate/lookback window. Exporter restarts and unusual stale ingestion can still affect identity."
	return out, nil
}

type demoHistory struct{}

func (demoHistory) Status() model.HistoryCapability {
	return model.HistoryCapability{State: "simulated", Source: "simulated", Message: "Simulated historical CPU and memory. No Prometheus endpoint or real cluster is contacted."}
}
func (demoHistory) Query(ctx context.Context, t model.HistoryTarget) (model.HistoryData, error) {
	out := model.HistoryData{State: "simulated", Message: "Simulated historical CPU and memory; gaps represent unavailable samples. No real monitoring endpoint is contacted.", Series: []model.HistorySeries{}, Warnings: []model.Warning{}}
	containers := []string{""}
	if t.Kind == "Pod" {
		containers = t.Containers
		if t.Container != "" {
			containers = []string{t.Container}
		}
	}
	for index, container := range containers {
		for _, metric := range []string{"cpu", "memory"} {
			series := model.HistorySeries{Metric: metric, Unit: "cores", Container: container, Points: []model.HistoryPoint{}}
			if metric == "memory" {
				series.Unit = "bytes"
			}
			for stamp := t.Start.Unix(); stamp <= t.End.Unix(); stamp += t.Step {
				x := float64(stamp)/600 + float64(index)
				value := 0.12 + 0.05*math.Sin(x)
				if t.Kind == "Node" {
					value = 2.2 + 0.4*math.Sin(x)
				}
				if metric == "memory" {
					value = (180 + 20*math.Sin(x/2)) * float64(1<<20)
					if t.Kind == "Node" {
						value = (10 + math.Sin(x/2)) * float64(1<<30)
					}
				}
				point := model.HistoryPoint{Timestamp: stamp, Value: &value}
				if (stamp/t.Step)%19 == 0 {
					point.Value = nil
				}
				series.Points = append(series.Points, point)
			}
			out.Series = append(out.Series, series)
		}
	}
	return out, ctx.Err()
}
