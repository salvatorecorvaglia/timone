package api

import (
	"encoding/json"
	"strings"
	"testing"
	"timone/internal/model"
)

func TestMetricsExposeProtectedScopedLimitBudgets(t *testing.T) {
	token := strings.Repeat("l", 32)
	h, _ := handler(token)
	path := "/api/v1/metrics?namespace=production"
	if w := perform(h, "GET", path, "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := perform(h, "GET", path, "", token)
	var metrics model.Metrics
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &metrics) != nil || len(metrics.Allocations) == 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, a := range metrics.Allocations {
		if a.Scope != "namespace production only" {
			t.Fatal("scope missing", a)
		}
		for _, b := range []model.LimitBudget{a.CPULimits, a.MemoryLimits} {
			if b.KnownSubtotal == nil || b.FinitePods == nil || b.MissingPods == nil || b.InvalidPods == nil || *b.InvalidPods != 0 || *b.FinitePods+*b.MissingPods != *a.Pods {
				t.Fatal(b)
			}
			if a.Node == "harbor-worker-01" {
				// The existing demo Postgres/Redis Pods omit both limits.
				if b.State != "incomplete" || b.Total != nil || *b.MissingPods != 2 {
					t.Fatal(b)
				}
			} else if b.State != "finite" || b.Total == nil || *b.Total != *b.KnownSubtotal || *b.MissingPods != 0 {
				t.Fatal(b)
			}
		}
	}
	if w := perform(h, "POST", path, `{}`, token); w.Code != 405 {
		t.Fatal("write accepted", w.Code)
	}
	if w := perform(h, "GET", "/api/v1/metrics?namespace=../escape", "", token); w.Code != 400 {
		t.Fatal("invalid namespace accepted", w.Code)
	}
}
