package api

import (
	"encoding/json"
	"strings"
	"testing"
	"timone/internal/model"
)

func TestGrantHTTPAuthenticationValidationAndProvenance(t *testing.T) {
	token := strings.Repeat("r", 32)
	h, _ := handler(token)
	path := "/api/v1/serviceaccounts/production/default/grants"
	if w := perform(h, "GET", path, "", ""); w.Code != 401 {
		t.Fatal("anonymous grant access", w.Code)
	}
	for _, suffix := range []string{"?limit=0", "?limit=201", "?offset=-1", "?limit=1&limit=2", "?impersonate=admin", "?namespace=staging", "?offset=%ZZ"} {
		if w := perform(h, "GET", path+suffix, "", token); w.Code != 400 {
			t.Fatalf("invalid grant query %s %d", suffix, w.Code)
		}
	}
	if w := perform(h, "POST", path, "{}", token); w.Code != 405 {
		t.Fatal("grant endpoint accepts mutations", w.Code)
	}
	w := perform(h, "GET", path+"?limit=2&offset=1", "", token)
	var report model.GrantReport
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &report) != nil || len(report.Items) != 2 || report.Offset != 1 || report.Total != 5 || report.Mode != "demo" {
		t.Fatalf("grant report %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"rules":null`) || strings.Contains(w.Body.String(), `"annotations"`) {
		t.Fatal("unsafe/ambiguous report", w.Body.String())
	}
	if w := perform(h, "GET", "/api/v1/serviceaccounts/production/not-present/grants", "", token); w.Code != 404 {
		t.Fatal("missing subject accepted", w.Code)
	}
}
