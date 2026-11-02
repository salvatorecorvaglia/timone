package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOperationalMetadataProjectionIsBoundedAndIdempotent(t *testing.T) {
	for _, kind := range []string{"Pod", "ReplicaSet", "Deployment", "ControllerRevision"} {
		o := Object{"kind": kind, "metadata": map[string]any{"annotations": map[string]any{"arbitrary": "PRIVATE-SENTINEL", "kubernetes.io/config.mirror": "PRIVATE-SENTINEL", "deployment.kubernetes.io/revision": "12"}}, "data": map[string]any{"value": "PRIVATE-SENTINEL"}}
		if kind != "ControllerRevision" {
			delete(o, "data")
		}
		clean := Sanitize(o)
		data, _ := json.Marshal(clean)
		if strings.Contains(string(data), "PRIVATE-SENTINEL") {
			t.Fatalf("%s: %s", kind, data)
		}
		twice, _ := json.Marshal(Sanitize(clean))
		if string(data) != string(twice) {
			t.Fatal("projection not idempotent")
		}
	}
}
