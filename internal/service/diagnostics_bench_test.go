package service

import (
	"fmt"
	"testing"
	"time"
	"timone/internal/model"
)

func BenchmarkDiagnostics5000x5000(b *testing.B) {
	snap := model.Snapshot{ObservedAt: time.Now().UTC()}
	for i := range 5000 {
		name := fmt.Sprintf("app-%04d", i)
		o := model.Object{"kind": "Pod", "metadata": map[string]any{"name": name, "namespace": "bench", "uid": name}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app"}}}, "status": map[string]any{"phase": "Running", "containerStatuses": []any{map[string]any{"ready": true}}}}
		snap.Resources = append(snap.Resources, model.Summarize(o))
		snap.Events = append(snap.Events, model.Event{ResourceID: "Pod:bench:unrelated", Type: "Normal", Timestamp: snap.ObservedAt.Format(time.RFC3339)})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		DetectIssues(snap)
	}
}

// Exercise the failure/grouping path with actually related events, complementing
// the unrelated-event index benchmark above and the retained-history benchmark.
func BenchmarkDiagnosticsRelatedFailures5000(b *testing.B) {
	now := time.Now().UTC()
	snap := model.Snapshot{ObservedAt: now}
	for i := range 5000 {
		name := fmt.Sprintf("failure-%04d", i)
		object := model.Object{"kind": "Pod", "metadata": map[string]any{"name": name, "namespace": "bench", "uid": name}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app"}}}, "status": map[string]any{"phase": "Failed"}}
		resource := model.Summarize(object)
		snap.Resources = append(snap.Resources, resource)
		snap.Events = append(snap.Events, model.Event{ID: name, ResourceID: resource.ID, ResourceUID: name, Kind: "Pod", Resource: name, ResourceNamespace: "bench", Type: "Warning", Reason: "BackOff", Timestamp: now.Format(time.RFC3339)})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if issues := DetectIssues(snap); len(issues) != 5000 || len(issues[0].Events) != 1 {
			b.Fatal("failure/event coverage missing")
		}
	}
}
