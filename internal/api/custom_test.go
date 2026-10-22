package api

import (
	"strings"
	"testing"
)

func TestCustomResourceHTTPAuthenticationValidationAndProjection(t *testing.T) {
	token := strings.Repeat("c", 32)
	h, _ := handler(token)
	for _, path := range []string{"/api/v1/custom-resource-types", "/api/v1/custom-resources/databases.platform.timone.io", "/api/v1/custom-resources/databases.platform.timone.io/production/orders-db"} {
		if w := perform(h, "GET", path, "", ""); w.Code != 401 {
			t.Fatalf("unauthorized custom route %s: %d", path, w.Code)
		}
		w := perform(h, "GET", path, "", token)
		if w.Code != 200 || strings.Contains(w.Body.String(), "SIMULATED-OMITTED-CUSTOM-SECRET") {
			t.Fatalf("unsafe/custom route failed %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/custom-resources/databases.platform.timone.io?version=v999", "/api/v1/custom-resources/databases.platform.timone.io?limit=0", "/api/v1/custom-resources/databases.platform.timone.io?limit=201", "/api/v1/custom-resources/databases.platform.timone.io?namespace=../other", "/api/v1/custom-resources/databases.platform.timone.io?kind=Pod", "/api/v1/custom-resources/not-a-crd", "/api/v1/custom-resources/clusterprofiles.platform.timone.io/production/production-baseline", "/api/v1/custom-resources/databases.platform.timone.io/_/orders-db"} {
		if w := perform(h, "GET", path, "", token); w.Code != 400 {
			t.Fatalf("invalid custom input %s: %d", path, w.Code)
		}
	}
	if w := perform(h, "DELETE", "/api/v1/custom-resources/databases.platform.timone.io/production/orders-db", "", token); w.Code != 405 {
		t.Fatal("custom write route exposed", w.Code)
	}
}
