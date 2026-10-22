package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRegressionNestedAnnotations(t *testing.T) {
	o := Object{"kind": "Deployment", "metadata": map[string]any{"name": "app"}, "spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]any{"operator.example/bootstrap": "AUDIT-SENSITIVE-SENTINEL"}}}}}
	b, _ := json.Marshal(Sanitize(o))
	if strings.Contains(string(b), "AUDIT-SENSITIVE-SENTINEL") {
		t.Fatal("Nested Pod-template annotation survives generic sanitization")
	}
}
func TestRegressionReadinessGate(t *testing.T) {
	o := Object{"kind": "Pod", "metadata": map[string]any{"name": "app"}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app"}}, "readinessGates": []any{map[string]any{"conditionType": "example.org/ready"}}}, "status": map[string]any{"phase": "Running", "containerStatuses": []any{map[string]any{"name": "app", "ready": true}}, "conditions": []any{map[string]any{"type": "Ready", "status": "False"}, map[string]any{"type": "example.org/ready", "status": "False"}}}}
	r := Summarize(o)
	if r.Health == "healthy" {
		t.Fatalf("Pod Ready=False presented as %s (%s)", r.Health, r.Status)
	}
}
func TestRegressionStaleDeployment(t *testing.T) {
	o := Object{"kind": "Deployment", "metadata": map[string]any{"name": "app", "generation": int64(2)}, "spec": map[string]any{"replicas": int64(3)}, "status": map[string]any{"readyReplicas": int64(3), "availableReplicas": int64(3), "updatedReplicas": int64(0), "observedGeneration": int64(1)}}
	r := Summarize(o)
	if r.Health == "healthy" {
		t.Fatalf("unobserved generation with zero updated replicas presented as %s (%s)", r.Health, r.Status)
	}
}
func TestRegressionContainerFailurePrecedence(t *testing.T) {
	o := Object{"kind": "Pod", "metadata": map[string]any{"name": "app"}, "spec": map[string]any{"containers": []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}}}, "status": map[string]any{"phase": "Running", "containerStatuses": []any{map[string]any{"name": "a", "ready": false, "state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}}}, map[string]any{"name": "b", "ready": false, "state": map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}}}}}}
	r := Summarize(o)
	if r.Health != "critical" {
		t.Fatalf("CrashLoopBackOff masked by later container: %s (%s)", r.Health, r.Status)
	}
}

func TestRegressionGitOpsInlineCredentials(t *testing.T) {
	for _, o := range []Object{
		{"kind": "HelmRelease", "metadata": map[string]any{"name": "app"}, "spec": map[string]any{"values": map[string]any{"database": map[string]any{"password": "AUDIT-GITOPS-SENTINEL"}}}},
		{"kind": "Application", "metadata": map[string]any{"name": "app"}, "spec": map[string]any{"source": map[string]any{"helm": map[string]any{"values": "database:\n  password: AUDIT-GITOPS-SENTINEL"}}}},
	} {
		b, _ := json.Marshal(Sanitize(o))
		if strings.Contains(string(b), "AUDIT-GITOPS-SENTINEL") {
			t.Errorf("%s inline credentials survive the generic detail projection", Str(o, "kind"))
		}
	}
}

func TestHealthPrecedenceIsIndependentOfContainerOrder(t *testing.T) {
	statuses := []any{map[string]any{"name": "app", "state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}}}, map[string]any{"name": "sidecar", "state": map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}}}}
	o := Object{"kind": "Pod", "metadata": map[string]any{"name": "app"}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app"}, map[string]any{"name": "sidecar"}}}, "status": map[string]any{"phase": "Running", "containerStatuses": statuses}}
	for range 2 {
		r := Summarize(o)
		if r.Status != "CrashLoopBackOff" || r.Health != "critical" {
			t.Fatal(r)
		}
		statuses[0], statuses[1] = statuses[1], statuses[0]
	}
}
func TestWorkloadRolloutStrategiesAndMissingReadiness(t *testing.T) {
	o := Object{"kind": "StatefulSet", "metadata": map[string]any{"name": "db", "generation": 1}, "spec": map[string]any{"replicas": 3, "updateStrategy": map[string]any{"rollingUpdate": map[string]any{"partition": 2}}}, "status": map[string]any{"observedGeneration": 1, "readyReplicas": 3, "updatedReplicas": 1, "currentRevision": "old", "updateRevision": "new"}}
	if r := Summarize(o); r.Health != "healthy" {
		t.Fatal("partition rollout rejected", r)
	}
	Map(o, "spec", "updateStrategy", "rollingUpdate")["partition"] = 0
	if r := Summarize(o); r.Health == "healthy" {
		t.Fatal("partial rollout accepted", r)
	}
	Map(o, "spec", "updateStrategy")["type"] = "OnDelete"
	if r := Summarize(o); r.Health != "healthy" {
		t.Fatal("OnDelete rollout rejected", r)
	}
	pod := Object{"kind": "Pod", "spec": map[string]any{"containers": []any{map[string]any{"name": "app"}}}, "status": map[string]any{"phase": "Running", "containerStatuses": []any{map[string]any{"name": "app", "ready": true}}}}
	if r := Summarize(pod); r.Health != "unknown" {
		t.Fatal("missing readiness established success", r)
	}
}
func TestPrinterPathsExcludeInnocuousCustomConfiguration(t *testing.T) {
	for _, path := range []string{".spec.settings", ".status.result", ".spec.endpoint", ".metadata.annotations.config"} {
		if SafePrinterPath(path) {
			t.Fatal(path)
		}
	}
}
