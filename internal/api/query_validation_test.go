package api

import (
	"net/http/httptest"
	"testing"
	"timone/internal/provider"
)

func TestQueryValidation(t *testing.T) {
	for _, raw := range []string{"limti=2", "limit=2&limit=3", "desc=yes", "namespace=a&namespace=b", "search=%zz"} {
		r := httptest.NewRequest("GET", "/api/v1/resources?"+raw, nil)
		if _, err := query(r); provider.Status(err) != 400 {
			t.Errorf("%s: %v", raw, err)
		}
	}
	for _, path := range []string{"/api/v1/resources?limit=2&desc=false", "/api/v1/topology?group=workloads", "/api/v1/custom-resources/example.test?version=v1"} {
		if _, err := query(httptest.NewRequest("GET", path, nil)); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}
