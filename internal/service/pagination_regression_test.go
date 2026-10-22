package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"timone/internal/model"
	"timone/internal/provider"
)

func TestApplicationEnrichmentMatchesUnpaginatedResults(t *testing.T) {
	objects := []model.Object{{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "shared", "namespace": "production"}}}
	for i := 0; i < 35; i++ {
		name := fmt.Sprintf("app-%02d", i)
		objects = append(objects, model.Object{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": name, "namespace": "production", "uid": name}, "spec": map[string]any{"volumes": []any{map[string]any{"name": "config", "configMap": map[string]any{"name": "shared"}}}}, "status": map[string]any{"phase": "Running"}})
	}
	s := New(auditFixtureProvider{Provider: provider.NewDemo(), objects: objects})
	full, err := s.Applications(context.Background(), Query{Namespace: "production", Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"", "unknown", "healthy", "critical"} {
		expected := []Application{}
		for _, app := range full.Items {
			if status == "" || app.Health == status {
				expected = append(expected, app)
			}
		}
		for _, offset := range []int{0, 20, 40} {
			page, err := s.Applications(context.Background(), Query{Namespace: "production", Limit: 20, Offset: offset, Status: status})
			if err != nil {
				t.Fatal(err)
			}
			start := min(offset, len(expected))
			want, _ := json.Marshal(expected[start:min(start+20, len(expected))])
			got, _ := json.Marshal(page.Items)
			if page.Total != len(expected) || string(want) != string(got) {
				t.Fatalf("page enrichment changed for status=%q offset=%d", status, offset)
			}
		}
	}
}
