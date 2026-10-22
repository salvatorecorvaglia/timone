// Command fixtures serializes actual in-process demo API responses for UI integration tests.
package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"timone/internal/api"
	"timone/internal/config"
	"timone/internal/provider"
	"timone/internal/service"
)

func main() {
	h := api.New(service.New(provider.NewDemo()), config.Config{Listen: "127.0.0.1:8080", Mode: "demo"}, nil, api.NewBroker())
	paths := []string{"/api/v1/custom-resources/databases.platform.timone.io?version=v1beta1", "/api/v1/custom-resources/databases.platform.timone.io?status=warning&version=v1beta1", "/api/v1/workspace", "/api/v1/authorization?kind=Pod&namespace=production&verb=get", "/api/v1/authorization?kind=Secret&namespace=production&verb=get", "/api/v1/gitops/Application/platform/checkout-api/drift", "/api/v1/gitops/Kustomization/platform/cluster-config/drift", "/api/v1/helm/releases/production/checkout-api", "/api/v1/helm/releases/production/checkout-api/compare?from=2&to=3", "/api/v1/helm/releases/production/checkout-api/compare?from=1&to=3", "/api/v1/connection", "/api/v1/session", "/api/v1/cluster", "/api/v1/overview", "/api/v1/issues", "/api/v1/events", "/api/v1/changes", "/api/v1/changes?namespace=&limit=50", "/api/v1/events?kind=Job", "/api/v1/events?kind=Job&resource=database-backup-29183040", "/api/v1/topology", "/api/v1/audit", "/api/v1/integrations", "/api/v1/metrics", "/api/v1/applications", "/api/v1/applications?search=checkout", "/api/v1/search", "/api/v1/resources?kind=Pod&limit=25", "/api/v1/resources?kind=Pod&search=payments&limit=25", "/api/v1/resources?kind=Pod&status=critical&limit=25", "/api/v1/resources?kind=Deployment&limit=25", "/api/v1/resources/Deployment/production/checkout-api", "/api/v1/resources/Pod/production/checkout-api-7c8d9f-m4n7q", "/api/v1/pods/production/checkout-api-7c8d9f-m4n7q/logs"}
	paths = append(paths, "/api/v1/metrics/history/status", "/api/v1/metrics/history?kind=Node&name=harbor-worker-01&range=1h", "/api/v1/metrics/history?kind=Node&name=harbor-worker-01&range=6h", "/api/v1/metrics/history?kind=Pod&namespace=production&name=checkout-api-7c8d9f-m4n7q&container=checkout-api&range=1h")
	paths = append(paths, "/api/v1/metrics/history?kind=Pod&namespace=production&name=payments-7c8d9f-k9x2p&container=payments&range=1h")
	out := map[string]json.RawMessage{}
	paths = append(paths, "/api/v1/custom-resource-types", "/api/v1/custom-resources/databases.platform.timone.io", "/api/v1/custom-resources/databases.platform.timone.io?search=payments", "/api/v1/custom-resources/databases.platform.timone.io?status=warning", "/api/v1/custom-resources/clusterprofiles.platform.timone.io", "/api/v1/custom-resources/databases.platform.timone.io/production/orders-db", "/api/v1/custom-resources/databases.platform.timone.io/production/payments-db", "/api/v1/custom-resources/clusterprofiles.platform.timone.io/_/production-baseline")
	for _, fields := range []map[string]string{
		{"search": "orders-db"},
		{"search": "orders-db", "definition": "databases.platform.timone.io"},
		{"search": "orders-db", "definition": "databases.platform.timone.io", "status": "warning"},
		{"search": "production"}, {"search": "production", "offset": "20"},
		{"search": "production", "kind": "Pod"},
		{"search": "public", "kind": "Gateway"}, {"search": "missing-resource"},
	} {
		values := url.Values{"limit": {"20"}, "offset": {"0"}, "namespace": {""}}
		for key, value := range fields {
			values.Set(key, value)
		}
		paths = append(paths, "/api/v1/search?"+values.Encode())
	}
	paths = append(paths, "/api/v1/resources/ServiceAccount/production/default", "/api/v1/serviceaccounts/production/default/grants?limit=25&offset=0", "/api/v1/serviceaccounts/production/default/grants?limit=25&offset=2")
	for _, path := range paths {
		r := httptest.NewRequest("GET", "http://127.0.0.1:8080"+path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			fmt.Fprintln(os.Stderr, path, w.Code, w.Body.String())
			os.Exit(1)
		}
		out[path] = w.Body.Bytes()
	}
	if e := json.NewEncoder(os.Stdout).Encode(out); e != nil {
		panic(e)
	}
}
