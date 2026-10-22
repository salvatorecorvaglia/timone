package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPVCPhaseHealth(t *testing.T) {
	for phase, health := range map[string]string{"Bound": "healthy", "Pending": "warning", "Lost": "critical", "": "unknown", "Unexpected": "unknown"} {
		r := Summarize(Object{"kind": "PersistentVolumeClaim", "status": map[string]any{"phase": phase}})
		if r.Health != health || r.Status != phase {
			t.Fatalf("phase %q: %+v", phase, r)
		}
	}
}

func TestProjectedHeaderRedactionAndCopyIsolation(t *testing.T) {
	header := map[string]any{"name": "Authorization", "value": "Bearer AUDIT-PRIVATE-HEADER"}
	container := map[string]any{"name": "app", "readinessProbe": map[string]any{"httpGet": map[string]any{"httpHeaders": []any{header}}}, "lifecycle": map[string]any{"postStart": map[string]any{"httpGet": map[string]any{"httpHeaders": []any{header}}}}}
	for _, kind := range []string{"Pod", "Deployment", "CronJob"} {
		for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
			podSpec := map[string]any{field: []any{container}}
			spec := podSpec
			if kind == "Deployment" {
				spec = map[string]any{"template": map[string]any{"spec": podSpec}}
			}
			if kind == "CronJob" {
				spec = map[string]any{"jobTemplate": map[string]any{"spec": map[string]any{"template": map[string]any{"spec": podSpec}}}}
			}
			clean := Sanitize(Object{"kind": kind, "spec": spec})
			b, _ := json.Marshal(clean)
			if strings.Contains(string(b), "AUDIT-PRIVATE-HEADER") || !strings.Contains(string(b), "Authorization") || !strings.Contains(string(b), "[REDACTED]") {
				t.Fatalf("unsafe %s/%s: %s", kind, field, b)
			}
			copy := CloneProjected(clean)
			Map(copy, "spec")["changed"] = true
			if Value(clean, "spec", "changed") != nil {
				t.Fatal("reader mutated retained spec")
			}
		}
	}
	if header["value"] != "Bearer AUDIT-PRIVATE-HEADER" {
		t.Fatal("sanitizer mutated input")
	}
	clean := Object{"nested": map[string]any{"array": []any{map[string]any{"value": "original"}}}, "cells": []string{"original"}}
	copy := CloneProjected(clean)
	Arr(copy, "nested", "array")[0].(map[string]any)["value"] = "mutated"
	copy["cells"].([]string)[0] = "mutated"
	if Str(Arr(clean, "nested", "array")[0].(map[string]any), "value") != "original" || clean["cells"].([]string)[0] != "original" {
		t.Fatal("nested copy alias")
	}
	shapes := Object{"nullMap": map[string]any(nil), "nullArray": []any(nil), "nullCells": []string(nil), "emptyArray": []any{}, "emptyMap": map[string]any{}}
	before, _ := json.Marshal(shapes)
	after, _ := json.Marshal(CloneProjected(shapes))
	if string(before) != string(after) {
		t.Fatal("copy changed null/empty wire shape")
	}
}

func BenchmarkCloneProjected(b *testing.B) {
	o := Sanitize(Object{"kind": "Pod", "metadata": map[string]any{"name": "app", "labels": map[string]any{"app": "api"}}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app", "env": []any{map[string]any{"name": "TOKEN", "value": "private"}}}}}})
	b.Run("json-sanitization", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			Sanitize(o)
		}
	})
	b.Run("projected-copy", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			CloneProjected(o)
		}
	})
}
