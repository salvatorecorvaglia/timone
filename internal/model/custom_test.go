package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCustomProjectionOmitsUnknownCredentialFieldsWithoutMutatingSource(t *testing.T) {
	o := Object{"apiVersion": "ops.example/v1", "kind": "Secret", "metadata": map[string]any{"name": "test", "uid": "uid", "annotations": map[string]any{"secret": "sensitive-marker"}, "labels": map[string]any{"team": "platform"}}, "spec": map[string]any{"password": "sensitive-marker"}, "data": map[string]any{"password": "sensitive-marker"}, "status": map[string]any{"credentials": "sensitive-marker", "conditions": []any{map[string]any{"type": "Ready", "status": "True", "password": "sensitive-marker"}}}}
	clean := CustomProjection(o)
	b, _ := json.Marshal(clean)
	if strings.Contains(string(b), "sensitive-marker") || Value(clean, "spec") != nil || Value(clean, "data") != nil {
		t.Fatal("custom payload leaked")
	}
	Map(clean, "metadata", "labels")["team"] = "changed"
	if Str(o, "metadata", "labels", "team") != "platform" || Str(o, "spec", "password") != "sensitive-marker" {
		t.Fatal("projection mutated source")
	}
	if SummarizeCustom(clean, "secrets.ops.example").Health != "unknown" {
		t.Fatal("custom Kind invoked built-in health rules")
	}
}
func TestCustomReadinessRequiresOneCurrentGenerationMatchedCondition(t *testing.T) {
	for _, tc := range []struct {
		generation, observed int
		status               string
		duplicate            bool
		want                 string
	}{{2, 2, "True", false, "healthy"}, {2, 2, "False", false, "warning"}, {2, 1, "True", false, "unknown"}, {2, 0, "True", false, "unknown"}, {0, 0, "True", false, "unknown"}, {2, 2, "Unknown", false, "unknown"}, {2, 2, "True", true, "unknown"}} {
		c := map[string]any{"type": "Ready", "status": tc.status, "observedGeneration": tc.observed}
		conditions := []any{c}
		if tc.duplicate {
			conditions = append(conditions, c)
		}
		o := Object{"kind": "Database", "metadata": map[string]any{"name": "db", "generation": tc.generation}, "status": map[string]any{"conditions": conditions}}
		if got := SummarizeCustom(o, "databases.ops.example").Health; got != tc.want {
			t.Fatalf("%+v got %s", tc, got)
		}
	}
	if !ValidCustomDefinition("databases.ops.example") || ValidCustomDefinition("pods") || ValidCustomDefinition("pods/exec.ops.example") || ValidCustomDefinition("*.ops.example") {
		t.Fatal("invalid custom definition validation")
	}
}

func TestCustomConditionRejectsFractionalOrInexactGenerations(t *testing.T) {
	for _, generation := range []float64{2.5, 1 << 54} {
		o := Object{"metadata": map[string]any{"generation": generation}, "status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "False", "observedGeneration": generation}}}}
		if _, state := CustomReadyCondition(o); state != "unverified" {
			t.Fatal(generation, state)
		}
	}
}
