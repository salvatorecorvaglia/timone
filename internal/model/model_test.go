package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeDoesNotLeakSecretOrAnnotations(t *testing.T) {
	o := Object{"kind": "Secret", "metadata": map[string]any{"name": "test", "annotations": map[string]any{"kubectl.kubernetes.io/last-applied-configuration": "sensitive-marker"}}, "data": map[string]any{"password": "sensitive-marker"}, "stringData": map[string]any{"password": "sensitive-marker"}}
	b, _ := json.Marshal(Sanitize(o))
	if strings.Contains(string(b), "sensitive-marker") {
		t.Fatalf("secret leaked: %s", b)
	}
	if o["data"] == nil {
		t.Fatal("sanitization mutated source")
	}
}
func TestSanitizeNestedEnvironment(t *testing.T) {
	o := Object{"kind": "Deployment", "spec": map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"env": []any{map[string]any{"name": "PASSWORD", "value": "sensitive-marker"}, map[string]any{"name": "TOKEN", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "token", "key": "value"}}}}}}}}}}
	b, _ := json.Marshal(Sanitize(o))
	if strings.Contains(string(b), "sensitive-marker") {
		t.Fatal("direct environment value leaked")
	}
	if !strings.Contains(string(b), "secretKeyRef") {
		t.Fatal("reference removed")
	}
}
func TestResourceHealth(t *testing.T) {
	tests := []struct {
		kind, status, health string
		spec, statusObj      map[string]any
	}{{"Node", "NotReady", "critical", nil, map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "False"}}}}, {"Deployment", "Degraded", "warning", map[string]any{"replicas": 3}, map[string]any{"readyReplicas": 1}}, {"Service", "Observed", "unknown", nil, nil}, {"Pod", "Unknown", "unknown", map[string]any{"containers": []any{map[string]any{"name": "main"}}}, map[string]any{"phase": "Unknown"}}, {"Pod", "Completed", "healthy", nil, map[string]any{"phase": "Succeeded"}}}
	for _, tc := range tests {
		t.Run(tc.kind+tc.status, func(t *testing.T) {
			r := Summarize(Object{"kind": tc.kind, "spec": tc.spec, "status": tc.statusObj})
			if r.Status != tc.status || r.Health != tc.health {
				t.Fatalf("got %s/%s", r.Status, r.Health)
			}
		})
	}
}

func TestJobRetriesDoNotImplyTerminalFailure(t *testing.T) {
	r := Summarize(Object{"kind": "Job", "status": map[string]any{"failed": int64(2), "active": int64(1)}})
	if r.Health == "critical" || r.Status == "Failed" {
		t.Fatalf("retry reported as terminal failure %+v", r)
	}
	r = Summarize(Object{"kind": "Job", "status": map[string]any{"failed": int64(2), "conditions": []any{map[string]any{"type": "Complete", "status": "True"}}}})
	if r.Status != "Completed" || r.Health != "healthy" {
		t.Fatalf("successful retry not completed %+v", r)
	}
}
func TestEventOrderComparesInstantsAcrossOffsets(t *testing.T) {
	a := Event{ID: "a", Timestamp: "2026-10-15T12:00:00+02:00"}
	b := Event{ID: "b", Timestamp: "2026-10-15T10:30:00Z"}
	if NewerEvent(a, b) || !NewerEvent(b, a) {
		t.Fatal("RFC3339 strings sorted lexically instead of chronologically")
	}
	if NewerEvent(Event{ID: "bad", Timestamp: "invalid"}, a) {
		t.Fatal("invalid timestamp precedes valid timestamp")
	}
}

func TestStrPreservesUnstructuredValues(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{nil, ""}, {"label", "label"}, {42, "42"}, {true, "true"}, {json.Number("1.25"), "1.25"},
	} {
		if got := Str(Object{"nested": map[string]any{"value": tc.value}}, "nested", "value"); got != tc.want {
			t.Errorf("Str(%#v) = %q, want %q", tc.value, got, tc.want)
		}
	}
	if got := Str(Object{}, "missing", "value"); got != "" {
		t.Fatalf("missing path = %q", got)
	}
}
