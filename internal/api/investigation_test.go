package api

import (
	"encoding/json"
	"strings"
	"testing"
	"timone/internal/service"
)

func TestInvestigationRoutesAndAuthentication(t *testing.T) {
	token := strings.Repeat("i", 32)
	h, _ := handler(token)
	for _, path := range []string{
		"/api/v1/resources/Deployment/production/checkout-api/incident",
		"/api/v1/resources/Deployment/production/checkout-api/logs",
		"/api/v1/resources/StatefulSet/production/redis/storage",
	} {
		if w := perform(h, "GET", path, "", ""); w.Code != 401 {
			t.Fatalf("unprotected %s: %d", path, w.Code)
		}
		w := perform(h, "GET", path, "", token)
		if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if !strings.Contains(path, "/incident") {
			continue
		}
		var out service.IncidentResponse
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		if out.SchemaVersion != 1 || out.Logs != nil || len(out.Resources) < 2 {
			t.Fatalf("bad incident %+v", out)
		}
	}
}
func TestInvestigationRejectsUnknownDuplicateAndInvalidInputs(t *testing.T) {
	h, _ := handler("")
	for _, path := range []string{
		"/api/v1/resources/Deployment/production/checkout-api/incident?includeLogs=yes",
		"/api/v1/resources/Deployment/production/checkout-api/incident?since=bad",
		"/api/v1/resources/Deployment/production/checkout-api/incident?until=2999-01-01T00:00:00Z",
		"/api/v1/resources/Deployment/production/checkout-api/incident?since=a&since=b",
		"/api/v1/resources/Deployment/production/checkout-api/logs?tail=1001",
		"/api/v1/resources/Deployment/production/checkout-api/logs?previous=1",
		"/api/v1/resources/Deployment/production/checkout-api/logs?container=../private",
		"/api/v1/resources/Deployment/production/checkout-api/logs?unexpected=true",
		"/api/v1/resources/StatefulSet/production/redis/storage?namespace=other",
	} {
		if w := perform(h, "GET", path, "", ""); w.Code != 400 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
