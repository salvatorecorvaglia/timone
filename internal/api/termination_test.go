package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTerminationAPIIsReadOnlyAndValidatesTargets(t *testing.T) {
	token := strings.Repeat("d", 32)
	h, _ := handler(token)
	for _, path := range []string{"/api/v1/resources/Pod/production/redis-0/termination", "/api/v1/resources/PersistentVolumeClaim/production/redis-data/termination", "/api/v1/resources/Namespace/_/staging/termination"} {
		if w := perform(h, "GET", path, "", ""); w.Code != 401 {
			t.Fatalf("unprotected %s", path)
		}
		if w := perform(h, "GET", path, "", token); w.Code != 200 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		for _, verb := range []string{"POST", "PATCH", "DELETE"} {
			if w := perform(h, verb, path, "{}", token); w.Code < 400 {
				t.Fatalf("mutation accepted %s %s", verb, path)
			}
		}
	}
	for _, path := range []string{"/api/v1/resources/Pod/production/redis-0/termination?force=true", "/api/v1/resources/Namespace/production/staging/termination", "/api/v1/resources/Deployment/production/checkout-api/termination"} {
		if w := perform(h, "GET", path, "", token); w.Code != 400 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
