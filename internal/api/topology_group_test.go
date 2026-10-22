package api

import (
	"encoding/json"
	"testing"
	"timone/internal/model"
)

func TestTopologyGroupRESTContract(t *testing.T) {
	h, _ := handler("")
	w := perform(h, "GET", "/api/v1/topology?namespace=production&group=workloads", "", "")
	var graph model.Graph
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &graph) != nil || len(graph.Nodes) == 0 {
		t.Fatal("group request failed", w.Code)
	}
	for _, r := range graph.Nodes {
		if r.Kind == "ConfigMap" || r.Kind == "Service" || r.Namespace != "production" {
			t.Fatal("incorrect group", r.ID)
		}
	}
	for _, query := range []string{"group=unknown", "group=workloads&group=networking"} {
		if w := perform(h, "GET", "/api/v1/topology?"+query, "", ""); w.Code != 400 {
			t.Fatal("invalid group accepted", query, w.Code)
		}
	}
}
