package model

import (
	"encoding/json"
	"fmt"
	"k8s.io/apimachinery/pkg/util/duration"
	"sort"
	"strings"
	"time"
)

type PrinterColumn struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Format      string `json:"format,omitempty"`
	Description string `json:"description,omitempty"`
	JSONPath    string `json:"jsonPath"`
	Priority    int    `json:"priority"`
}
type CustomPresentation struct {
	Columns   []PrinterColumn `json:"columns"`
	Schema    Object          `json:"schema"`
	Truncated bool            `json:"truncated"`
}

func SensitivePath(path string) bool {
	lower := strings.ToLower(path)
	for _, word := range []string{"secret", "password", "passwd", "token", "credential", "privatekey", "private_key", "apikey", "api_key", "authorization", "connectionstring", "connection_string", ".data", ".binarydata", ".stringdata", ".env", ".annotations"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// Defaults/examples are data, not schema structure, and can contain credentials.
func SchemaProjection(v map[string]any) (Object, bool) {
	count := 0
	truncated := false
	var walk func(any, int) any
	walk = func(value any, depth int) any {
		count++
		if count > 4000 || depth > 20 {
			truncated = true
			return nil
		}
		switch m := value.(type) {
		case map[string]any:
			out := map[string]any{}
			keys := []string{}
			for key := range m {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if count >= 4000 {
					truncated = true
					break
				}
				item := m[key]
				if key == "default" || key == "example" || key == "examples" {
					continue
				}
				out[key] = walk(item, depth+1)
			}
			return out
		case []any:
			out := []any{}
			for _, item := range m {
				if count >= 4000 {
					truncated = true
					break
				}
				out = append(out, walk(item, depth+1))
			}
			return out
		case string:
			if len(m) > 4096 {
				truncated = true
				return m[:4096]
			}
			return m
		default:
			return value
		}
	}
	projected := walk(v, 0)
	result, _ := projected.(map[string]any)
	return Object(result), truncated
}
func PrinterText(value any, typ string, now time.Time) string {
	if value == nil {
		return "<none>"
	}
	if typ == "date" {
		s, ok := value.(string)
		if !ok {
			return "<unknown>"
		}
		stamp, err := time.Parse(time.RFC3339, s)
		if err != nil || stamp.IsZero() {
			return "<unknown>"
		}
		return duration.HumanDuration(now.Sub(stamp))
	}
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case bool, float64, int, int64:
		text = fmt.Sprint(v)
	default:
		b, _ := json.Marshal(v)
		text = string(b)
	}
	if len(text) > 1024 {
		text = text[:1024] + "…"
	}
	return text
}
