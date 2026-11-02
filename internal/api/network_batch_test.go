package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNetworkAndBatchAPIContracts(t *testing.T) {
	token := strings.Repeat("b", 32)
	h, _ := handler(token)
	for _, path := range []string{
		"/api/v1/resources/Service/production/checkout-api/network",
		"/api/v1/resources/Service/production/checkout-api/network?sourceNamespace=production&sourcePod=payments-7c8d9f-k9x2p",
		"/api/v1/resources/Ingress/production/public-storefront/network",
		"/api/v1/resources/CronJob/platform/database-backup/batch",
		"/api/v1/resources/Job/platform/database-backup-29183040/batch",
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
		"/api/v1/resources/Service/production/checkout-api/network?sourcePod=a&sourcePod=b",
		"/api/v1/resources/Service/production/checkout-api/network?probe=true",
		"/api/v1/resources/Service/production/checkout-api/network?sourceNamespace=production",
		"/api/v1/resources/Pod/production/web/network",
		"/api/v1/resources/CronJob/platform/database-backup/batch?run=true",
		"/api/v1/resources/Deployment/production/checkout-api/batch",
	} {
		if w := perform(h, "GET", path, "", token); w.Code != 400 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
