package service

import (
	"math"
	"sort"
	"time"
	"timone/internal/model"
)

// Never sum partial sets, mismatched sampling times or samples preceding the
// current object's/container's lifetime. This remains timestamp-bound identity,
// not proof of a Metrics Server UID or a runtime measurement transaction.
func podMetrics(pods []model.Object, samples []model.Metric, now time.Time) []model.PodMetric {
	byPod := map[string][]model.Metric{}
	for _, m := range samples {
		if m.Kind == "Pod" {
			id := model.ID("Pod", m.Namespace, m.Name)
			byPod[id] = append(byPod[id], m)
		}
	}
	out := []model.PodMetric{}
	for _, p := range pods {
		r := model.Summarize(p)
		row := model.PodMetric{Resource: r, State: "unavailable", Missing: []string{}, Reason: "Current Running state and complete container identity/samples are required."}
		if model.Str(p, "status", "phase") != "Running" {
			row.Reason = "Pod is not currently Running."
			out = append(out, row)
			continue
		}
		created, err := time.Parse(time.RFC3339, r.CreatedAt)
		if err != nil {
			row.Reason = "Pod creation time unavailable."
			out = append(out, row)
			continue
		}
		expected := map[string]time.Time{}
		statuses := map[string]map[string]any{}
		for _, field := range []string{"containerStatuses", "initContainerStatuses", "ephemeralContainerStatuses"} {
			for _, v := range model.Arr(p, "status", field) {
				c, _ := v.(map[string]any)
				statuses[model.Str(c, "name")] = c
			}
		}
		valid := true
		add := func(name string) {
			c := statuses[name]
			started, e := time.Parse(time.RFC3339, model.Str(c, "state", "running", "startedAt"))
			if e != nil || name == "" || started.After(now) {
				valid = false
			}
			if _, dup := expected[name]; dup {
				valid = false
			}
			expected[name] = started
		}
		for _, v := range model.Arr(p, "spec", "containers") {
			c, _ := v.(map[string]any)
			add(model.Str(c, "name"))
		}
		for _, v := range model.Arr(p, "spec", "initContainers") {
			c, _ := v.(map[string]any)
			if model.Str(c, "restartPolicy") == "Always" {
				add(model.Str(c, "name"))
			}
		}
		for _, v := range model.Arr(p, "spec", "ephemeralContainers") {
			c, _ := v.(map[string]any)
			name := model.Str(c, "name")
			if model.Value(statuses[name], "state", "running") != nil {
				add(name)
			}
		}
		row.Expected = len(expected)
		if !valid || len(expected) == 0 {
			out = append(out, row)
			continue
		}
		seen := map[string]bool{}
		cpu, mem := 0.0, 0.0
		stamp := ""
		for _, m := range byPod[r.ID] {
			started, ok := expected[m.Container]
			ts, e := time.Parse(time.RFC3339, m.Timestamp)
			if !ok || seen[m.Container] || e != nil || ts.Before(created) || ts.Before(started) || ts.After(now.Add(time.Minute)) || now.Sub(ts) > 5*time.Minute || math.IsNaN(m.CPU) || math.IsInf(m.CPU, 0) || math.IsNaN(m.Memory) || math.IsInf(m.Memory, 0) || m.CPU < 0 || m.Memory < 0 {
				valid = false
				continue
			}
			if stamp != "" && stamp != m.Timestamp {
				valid = false
			}
			stamp = m.Timestamp
			seen[m.Container] = true
			cpu += m.CPU
			mem += m.Memory
		}
		row.Observed = len(seen)
		for name := range expected {
			if !seen[name] {
				row.Missing = append(row.Missing, name)
			}
		}
		sort.Strings(row.Missing)
		if valid && len(row.Missing) == 0 && !math.IsInf(cpu, 0) && !math.IsInf(mem, 0) {
			row.State = "available"
			row.CPU = &cpu
			row.Memory = &mem
			row.Timestamp = stamp
			row.Reason = "Complete current regular, native sidecar and running ephemeral container sample set at one timestamp; identity bounded by observed creation/start times."
		} else {
			row.State = "partial"
			row.Reason = "Missing, duplicate, stale, mismatched or unexpected container samples; no aggregate is reported."
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource.ID < out[j].Resource.ID })
	return out
}
