package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvolutionEndpointsRequireAuthenticationAndValidateBounds(t *testing.T) {
	token := strings.Repeat("s", 32)
	h, _ := handler(token)
	for _, path := range []string{"/api/v1/resources/Deployment/production/checkout-api/topology", "/api/v1/resources/Deployment/production/checkout-api/snapshot", "/api/v1/history/status", "/api/v1/history"} {
		if w := perform(h, "GET", path, "", ""); w.Code != 401 {
			t.Fatalf("unprotected %s", path)
		}
		if w := perform(h, "GET", path, "", token); w.Code != 200 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/resources/Pod/production/web/topology?depth=99", "/api/v1/resources/Pod/production/web/topology?depth=1&depth=2", "/api/v1/resources/Deployment/production/checkout-api/snapshot?raw=true", "/api/v1/history?since=bad"} {
		if w := perform(h, "GET", path, "", token); w.Code != 400 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}
