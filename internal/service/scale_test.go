package service

import (
	"fmt"
	"testing"
	"time"
	"timone/internal/model"
)

// Measures bounded in-process aggregation only, not API latency or cluster scale.
func BenchmarkPodMetrics1000(b *testing.B) {
	now := time.Now().UTC().Truncate(time.Second)
	pods := make([]model.Object, 1000)
	samples := make([]model.Metric, 1000)
	for i := range pods {
		name := fmt.Sprintf("app-%04d", i)
		pods[i] = model.Object{"kind": "Pod", "metadata": map[string]any{"name": name, "namespace": "bench", "creationTimestamp": now.Add(-time.Hour).Format(time.RFC3339)}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app"}}}, "status": map[string]any{"phase": "Running", "containerStatuses": []any{map[string]any{"name": "app", "ready": true, "state": map[string]any{"running": map[string]any{"startedAt": now.Add(-time.Minute).Format(time.RFC3339)}}}}}}
		samples[i] = model.Metric{Kind: "Pod", Namespace: "bench", Name: name, Container: "app", CPU: .1, Memory: 100, Timestamp: now.Format(time.RFC3339)}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if got := podMetrics(pods, samples, now); len(got) != 1000 || got[0].CPU == nil {
			b.Fatal("incomplete aggregate")
		}
	}
}
func BenchmarkObservations5000(b *testing.B) {
	now := time.Now().UTC()
	resources := make([]model.Resource, 5000)
	for i := range resources {
		resources[i] = model.Resource{ID: fmt.Sprintf("Pod:bench:app-%04d", i), Health: "healthy", Object: model.Object{"metadata": map[string]any{"uid": fmt.Sprint(i)}, "spec": map[string]any{"nodeName": "worker"}}}
	}
	o := newObservations()
	o.observe(model.Snapshot{Resources: resources, ObservedAt: now})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		now = now.Add(time.Millisecond)
		o.observe(model.Snapshot{Resources: resources, ObservedAt: now})
	}
}
