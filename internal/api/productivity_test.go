package api

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"timone/internal/service"
)

func TestSourceCatalogAndExplicitLogSelectionAPI(t *testing.T) {
	token := strings.Repeat("p", 32)
	h, _ := handler(token)
	path := "/api/v1/resources/Deployment/production/checkout-api/log-sources"
	if w := perform(h, "GET", path, "", ""); w.Code != 401 {
		t.Fatal("unprotected source catalog")
	}
	w := perform(h, "GET", path, "", token)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var catalog service.LogSourceCatalog
	if e := json.Unmarshal(w.Body.Bytes(), &catalog); e != nil || len(catalog.Sources) < 2 {
		t.Fatal("invalid catalog")
	}
	sources, _ := json.Marshal(catalog.Sources[1:2])
	selected := url.QueryEscape(string(sources))
	logs := perform(h, "GET", "/api/v1/resources/Deployment/production/checkout-api/logs?sources="+selected, "", token)
	var result service.WorkloadLogResponse
	if logs.Code != 200 || json.Unmarshal(logs.Body.Bytes(), &result) != nil || len(result.Sources) != 1 {
		t.Fatalf("%d %s", logs.Code, logs.Body.String())
	}
	for _, raw := range []string{"[]", "null", `[{"pod":"web","uid":"x","container":"app","unknown":true}]`} {
		if w := perform(h, "GET", "/api/v1/resources/Deployment/production/checkout-api/logs?sources="+url.QueryEscape(raw), "", token); w.Code != 400 {
			t.Fatalf("invalid selection accepted: %s", raw)
		}
	}
	rules := perform(h, "GET", "/api/v1/diagnostic-rules", "", token)
	if rules.Code != 200 || !strings.Contains(rules.Body.String(), "container-failure") {
		t.Fatal("rule catalog unavailable")
	}
}
