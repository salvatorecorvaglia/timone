package api

import (
	"encoding/json"
	"strings"
	"testing"
	"timone/internal/model"
)

func TestSearchHTTPValidationAuthAndCustomNavigationIdentity(t *testing.T) {
	token := strings.Repeat("s", 32)
	h, _ := handler(token)
	if w := perform(h, "GET", "/api/v1/search?search=orders-db", "", ""); w.Code != 401 {
		t.Fatal("search auth bypass", w.Code)
	}
	for _, suffix := range []string{"search=x", "search=orders&limit=0", "search=orders&limit=201", "search=orders&offset=-1", "search=orders&search=api", "search=orders&namespace=../other", "search=orders&kind=Pod&definition=databases.platform.timone.io", "search=orders&definition=../secret", "search=orders&query=up", "search=orders&sort=arbitrary", "search=orders&definition=%ZZ"} {
		if w := perform(h, "GET", "/api/v1/search?"+suffix, "", token); w.Code != 400 {
			t.Fatalf("bad search %s %d %s", suffix, w.Code, w.Body.String())
		}
	}
	w := perform(h, "GET", "/api/v1/search?search=orders-db&definition=databases.platform.timone.io&namespace=production&status=warning&limit=1", "", token)
	var result model.SearchResult
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatalf("search failed %d %s", w.Code, w.Body.String())
	}
	if result.Total != 1 || !result.Complete || result.Items[0].Version != "v1" || result.Items[0].Definition != "databases.platform.timone.io" {
		t.Fatalf("wrong identity %+v", result)
	}
	if strings.Contains(w.Body.String(), "SIMULATED-OMITTED-CUSTOM-SECRET") || strings.Contains(w.Body.String(), "manifest") {
		t.Fatal("payload exposed")
	}
	if w := perform(h, "DELETE", "/api/v1/search", "", token); w.Code != 405 {
		t.Fatal("search mutation exposed", w.Code)
	}
}
