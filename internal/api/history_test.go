package api

import (
	"strings"
	"testing"
)

func TestHistoryHTTPAuthValidationAndSimulation(t *testing.T) {
	token := strings.Repeat("m", 32)
	h, _ := handler(token)
	for _, path := range []string{"/api/v1/metrics/history/status", "/api/v1/metrics/history?kind=Node&name=harbor-worker-01&range=6h", "/api/v1/metrics/history?kind=Pod&namespace=production&name=checkout-api-7c8d9f-m4n7q"} {
		if w := perform(h, "GET", path, "", ""); w.Code != 401 {
			t.Fatal("history authentication bypass", path, w.Code)
		}
		if w := perform(h, "GET", path, "", token); w.Code != 200 || !strings.Contains(w.Body.String(), "simulated") {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	for _, suffix := range []string{"kind=Secret&name=checkout-db&namespace=production", "kind=Pod&name=checkout-api", "kind=Node&name=harbor-worker-01&range=7d", "kind=Node&name=harbor-worker-01&query=up", "kind=Node&name=harbor-worker-01&kind=Pod", "kind=Node&name=harbor-worker-01&container=api"} {
		if w := perform(h, "GET", "/api/v1/metrics/history?"+suffix, "", token); w.Code != 400 {
			t.Fatal("invalid history accepted", suffix, w.Code)
		}
	}
}
