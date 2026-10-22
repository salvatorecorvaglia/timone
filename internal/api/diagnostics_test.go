package api

import (
	"encoding/json"
	"strings"
	"testing"
	"timone/internal/config"
	"timone/internal/model"
	"timone/internal/provider"
	"timone/internal/service"
)

func TestDiagnosticPolicyIsProtectedReadOnlyAndConsistent(t *testing.T) {
	policy := model.DiagnosticPolicy{StartupGraceSeconds: 600, WarningEventWindowSeconds: 120, RestartThreshold: 9}
	s, err := service.NewWithDiagnosticPolicy(provider.NewDemo(), nil, policy)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("p", 32)
	h := New(s, config.Config{Mode: "demo", Listen: "127.0.0.1:8080", Token: token}, nil, NewBroker())
	for _, path := range []string{"/api/v1/overview", "/api/v1/issues", "/api/v1/issues/issue-Deployment:production:checkout-api"} {
		if w := perform(h, "GET", path, "", ""); w.Code != 401 {
			t.Fatal(w.Code)
		}
		w := perform(h, "GET", path, "", token)
		var v struct {
			Policy model.DiagnosticPolicy `json:"diagnosticPolicy"`
			Items  []model.Issue          `json:"items"`
			Issues []model.Issue          `json:"issues"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Policy != policy {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, i := range append(v.Items, v.Issues...) {
			if i.DiagnosticPolicy != policy {
				t.Fatal("different policy on item", i)
			}
		}
		if w := perform(h, "POST", path, `{}`, token); w.Code != 405 {
			t.Fatal("policy unexpectedly writable", w.Code)
		}
	}
}

func TestCustomIssuesUseProtectedCRDQualifiedIdentities(t *testing.T) {
	token := strings.Repeat("c", 32)
	h, _ := handler(token)
	path := "/api/v1/issues/issue-databases.platform.timone.io:production:orders-db"
	if w := perform(h, "GET", path, "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := perform(h, "GET", path, "", token)
	var issue model.Issue
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &issue) != nil || issue.Resource.Definition != "databases.platform.timone.io" || issue.Resource.Version != "v1" || issue.Title != "Custom resource reports not ready" {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "SIMULATED-OMITTED-CUSTOM-SECRET") {
		t.Fatal("custom payload escaped projection")
	}
	if w := perform(h, "GET", path+"?namespace=staging", "", token); w.Code != 404 {
		t.Fatal("namespace filter bypass", w.Code)
	}
	if w := perform(h, "GET", "/api/v1/issues/issue-Database:production:orders-db", "", token); w.Code != 404 {
		t.Fatal("kind-only identity accepted", w.Code)
	}
	w = perform(h, "GET", "/api/v1/issues", "", token)
	var list struct {
		Coverage model.CustomDiagnosticCoverage `json:"customDiagnostics"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || list.Coverage.State != "available" || list.Coverage.ReadinessUnknown != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := perform(h, "POST", path, `{}`, token); w.Code != 405 {
		t.Fatal("write accepted", w.Code)
	}
}
