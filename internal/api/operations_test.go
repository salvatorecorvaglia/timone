package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOperationalAssessmentEndpointsAreProtected(t *testing.T) {
	token := strings.Repeat("o", 32)
	h, _ := handler(token)
	for _, path := range []string{
		"/api/v1/resources/Pod/production/notification-worker-7c8d9f-m4n7q/scheduling",
		"/api/v1/resources/Deployment/production/checkout-api/rollout",
		"/api/v1/nodes/harbor-worker-01/maintenance?namespace=production",
	} {
		if w := perform(h, "GET", path, "", ""); w.Code != 401 {
			t.Fatalf("unprotected %s", path)
		}
		w := perform(h, "GET", path, "", token)
		if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if w := perform(h, "POST", path, "{}", token); w.Code < 400 {
			t.Fatalf("mutation accepted %s", path)
		}
	}
	for _, path := range []string{
		"/api/v1/resources/Pod/production/web/scheduling?force=true",
		"/api/v1/resources/Deployment/production/checkout-api/rollout?raw=true",
		"/api/v1/nodes/harbor-worker-01/maintenance?namespace=a&namespace=b",
		"/api/v1/resources/Pod/production/web/rollout",
	} {
		if w := perform(h, "GET", path, "", token); w.Code != 400 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
