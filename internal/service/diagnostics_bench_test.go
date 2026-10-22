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
