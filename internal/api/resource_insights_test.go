package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResourceInsightsAPIContracts(t *testing.T) {
	token := strings.Repeat("e", 32)
	h, _ := handler(token)
	for _, path := range []string{
		"/api/v1/resources/Deployment/production/checkout-api/efficiency?range=24h",
		"/api/v1/resources/Pod/production/checkout-api-7c8d9f-k9x2p/efficiency?range=1h",
		"/api/v1/resources/Deployment/production/checkout-api/configuration",
	} {
		if w := perform(h, "GET", path, "", ""); w.Code != 401 {
			t.Fatalf("unprotected %s", path)
		}
		if w := perform(h, "GET", path, "", token); w.Code != 200 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if w := perform(h, "POST", path, "{}", token); w.Code < 400 {
			t.Fatalf("mutation accepted %s", path)
		}
	}
	for _, path := range []string{
		"/api/v1/resources/Deployment/production/checkout-api/efficiency?range=7d",
		"/api/v1/resources/Deployment/production/checkout-api/efficiency?range=1h&range=24h",
		"/api/v1/resources/Deployment/production/checkout-api/efficiency?query=up",
		"/api/v1/resources/Deployment/production/checkout-api/configuration?raw=true",
		"/api/v1/resources/Secret/production/checkout-db/efficiency",
		"/api/v1/resources/ConfigMap/production/checkout-config/configuration",
	} {
		if w := perform(h, "GET", path, "", token); w.Code != 400 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
