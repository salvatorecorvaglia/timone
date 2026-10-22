package integrations

import (
	"encoding/json"
	"strings"
	"testing"
	"timone/internal/model"
)

func TestValuesSanitizeNestedCredentialsURLsPEMAndNamedEnvironment(t *testing.T) {
	input := model.Object{"replicas": float64(3), "database": map[string]any{"password": "HIDDEN-PASSWORD"}, "env": []any{map[string]any{"name": "API_KEY", "value": "HIDDEN-ENV"}}, "endpoint": "https://user:HIDDEN-URL@example.test", "opaque": "-----BEGIN RSA PRIVATE KEY----- HIDDEN-PEM", "headers": map[string]any{"Authorization": "HIDDEN-HEADER"}, "image": map[string]any{"tag": "v3"}}
	out, err := SanitizeValues(input)
	b, _ := json.Marshal(out)
	if err != nil || strings.Contains(string(b), "HIDDEN-") || !strings.Contains(string(b), `"tag":"v3"`) {
		t.Fatal(string(b), err)
	}
	if model.Str(input, "database", "password") != "HIDDEN-PASSWORD" {
		t.Fatal("source mutated")
	}
}
