package service

import (
	"context"
	"testing"
	"timone/internal/model"
	"timone/internal/provider"
)

func nodeFixture(name, zone string) model.Object {
	node := investigationObject("Node", "", name, name)
	model.Map(node, "metadata")["labels"] = map[string]any{"zone": zone, "size": "5"}
	model.Map(node, "status")["allocatable"] = map[string]any{"cpu": "2", "memory": "8Gi", "pods": "10"}
	return node
}
func requestedPod(name string) model.Object {
	pod := investigationObject("Pod", "production", name, name)
	model.Map(pod, "spec")["containers"] = []any{map[string]any{"name": "app", "resources": map[string]any{"requests": map[string]any{"cpu": "1", "memory": "128Mi"}}}}
	model.Map(pod, "status")["phase"] = "Pending"
	return pod
}
func hasCheck(checks []AssessmentCheck, code, state string) bool {
	for _, c := range checks {
		if c.Code == code && c.State == state {
			return true
		}
	}
	return false
}
func TestSchedulingReportsSelectorTaintAndCapacityConstraints(t *testing.T) {
	pod := requestedPod("target")
	model.Map(pod, "spec")["nodeSelector"] = map[string]any{"zone": "a"}
	a, b, c := nodeFixture("tainted", "a"), nodeFixture("full", "a"), nodeFixture("wrong-zone", "b")
	model.Map(a, "spec")["taints"] = []any{map[string]any{"key": "dedicated", "value": "gpu", "effect": "NoSchedule"}}
	resident := requestedPod("resident")
	model.Map(resident, "spec")["nodeName"] = "full"
	model.Map(resident, "status")["phase"] = "Running"
	containers := model.Arr(resident, "spec", "containers")
	containers[0].(map[string]any)["resources"] = map[string]any{"requests": map[string]any{"cpu": "2", "memory": "128Mi"}}
	p := &investigationReader{Provider: provider.NewDemo(), objects: []model.Object{pod, a, b, c, resident}}
	out, e := New(p).Scheduling(context.Background(), "production", "target")
	if e != nil || len(out.Nodes) != 3 {
		t.Fatalf("%+v %v", out, e)
	}
	found := map[string][]AssessmentCheck{}
	for _, node := range out.Nodes {
		found[node.Resource.Name] = node.Checks
	}
	if !hasCheck(found["tainted"], "taint", "constraint") || !hasCheck(found["full"], "capacity-cpu", "constraint") || !hasCheck(found["wrong-zone"], "node-selector", "constraint") {
		t.Fatalf("%+v", out.Nodes)
	}
	model.Map(pod, "spec")["tolerations"] = []any{map[string]any{"key": "dedicated", "operator": "Exists", "effect": "NoSchedule"}}
	out, e = New(scoped{p}).Scheduling(context.Background(), "production", "target")
	if e != nil {
		t.Fatal(e)
	}
	for _, node := range out.Nodes {
		if node.Resource.Name == "tainted" && (!hasCheck(node.Checks, "capacity-cpu", "unknown") || hasCheck(node.Checks, "taint", "constraint")) {
			t.Fatal("partial namespace scope established free capacity or ignored a toleration")
		}
	}
}
func TestNodeAffinityTermsORAndUnknownFields(t *testing.T) {
	node := nodeFixture("n", "a")
	selector := map[string]any{"nodeSelectorTerms": []any{
		map[string]any{"matchExpressions": []any{map[string]any{"key": "zone", "operator": "In", "values": []any{"b"}}}},
		map[string]any{"matchFields": []any{map[string]any{"key": "metadata.name", "operator": "In", "values": []any{"n"}}}},
	}}
	if matched, known := requiredNodeSelector(selector, node); !matched || !known {
		t.Fatal("ORed node selector terms not respected")
	}
	selector["nodeSelectorTerms"] = []any{map[string]any{}}
	if matched, known := requiredNodeSelector(selector, node); matched || !known {
		t.Fatal("empty node selector term must not match")
	}
	selector["nodeSelectorTerms"] = []any{map[string]any{"matchFields": []any{map[string]any{"key": "unsupported", "operator": "Exists"}}}}
	if _, known := requiredNodeSelector(selector, node); known {
		t.Fatal("unknown field accepted")
	}
	for _, tc := range []struct {
		operator string
		values   []any
		want     bool
	}{
		{"Gt", []any{"4"}, true}, {"Lt", []any{"4"}, false}, {"Exists", nil, true}, {"NotIn", []any{"4"}, true},
	} {
		if got, known := nodeRequirement(map[string]any{"key": "size", "operator": tc.operator, "values": tc.values}, node, false); got != tc.want || !known {
			t.Fatalf("operator %s", tc.operator)
		}
	}
}
func TestRolloutDistinguishesStalePausedPartitionAndOnDelete(t *testing.T) {
	p := workloadFixture(1)
	root := p.objects[0]
	model.Map(root, "metadata")["generation"] = 2
	model.Map(root, "metadata")["annotations"] = map[string]any{"deployment.kubernetes.io/revision": "3"}
	model.Map(root, "spec")["replicas"] = 1
	root["status"] = map[string]any{"observedGeneration": 1, "replicas": 1, "updatedReplicas": 1, "readyReplicas": 1, "availableReplicas": 1}
	s := New(p)
	out, e := s.Rollout(context.Background(), "Deployment", "production", "web")
	if e != nil || out.State != "awaiting-observation" {
		t.Fatalf("%s %v", out.State, e)
	}
	model.Map(root, "status")["observedGeneration"] = 2
	model.Map(root, "spec")["paused"] = true
	out, e = s.Rollout(context.Background(), "Deployment", "production", "web")
	if e != nil || out.State != "paused" {
		t.Fatal(out.State, e)
	}
	model.Map(root, "spec")["paused"] = false
	out, e = s.Rollout(context.Background(), "Deployment", "production", "web")
	if e != nil || out.State != "controller-reports-complete" {
		t.Fatal(out.State, e)
	}
	stateful := investigationObject("StatefulSet", "production", "db", "db")
	model.Map(stateful, "metadata")["generation"] = 1
	stateful["spec"] = map[string]any{"replicas": 3, "updateStrategy": map[string]any{"type": "RollingUpdate", "rollingUpdate": map[string]any{"partition": 2}}}
	stateful["status"] = map[string]any{"observedGeneration": 1, "updatedReplicas": 1, "readyReplicas": 3, "currentRevision": "old", "updateRevision": "new"}
	p.objects = append(p.objects, stateful)
	out, e = s.Rollout(context.Background(), "StatefulSet", "production", "db")
	if e != nil || out.State != "partitioned" {
		t.Fatal(out.State, e)
	}
	model.Map(stateful, "spec")["updateStrategy"] = map[string]any{"type": "OnDelete"}
	out, e = s.Rollout(context.Background(), "StatefulSet", "production", "db")
	if e != nil || out.State != "manual-replacement" {
		t.Fatal(out.State, e)
	}
}
func TestPDBHealthPoliciesAndMaintenanceSharedBudgets(t *testing.T) {
	p := workloadFixture(2)
	node := nodeFixture("worker", "a")
	p.objects = append(p.objects, node)
	pdb := investigationObject("PodDisruptionBudget", "production", "web-budget", "budget")
	model.Map(pdb, "metadata")["generation"] = 1
	pdb["spec"] = map[string]any{"selector": map[string]any{}}
	pdb["status"] = map[string]any{"observedGeneration": 1, "disruptionsAllowed": 0, "currentHealthy": 0, "desiredHealthy": 1}
	for _, pod := range p.objects[2:4] {
		model.Map(pod, "spec")["nodeName"] = "worker"
		model.Map(pod, "status")["phase"] = "Running"
		model.Map(pod, "status")["conditions"] = []any{map[string]any{"type": "Ready", "status": "True"}}
	}
	p.objects = append(p.objects, pdb)
	out, e := New(p).NodeMaintenance(context.Background(), "worker", "production")
	if e != nil || len(out.Pods) != 2 || len(out.Budgets) != 1 || out.Budgets[0].TargetPods != 2 || out.State != "constraints-observed" {
		t.Fatalf("%+v %v", out, e)
	}
	unhealthy := p.objects[2]
	model.Map(unhealthy, "status")["conditions"] = []any{}
	model.Map(pdb, "spec")["unhealthyPodEvictionPolicy"] = "AlwaysAllow"
	if c := budgetCheck(pdb, unhealthy); c.State != "advisory" {
		t.Fatalf("AlwaysAllow %v", c)
	}
	model.Map(pdb, "spec")["unhealthyPodEvictionPolicy"] = "IfHealthyBudget"
	model.Map(pdb, "status")["currentHealthy"] = 1
	if c := budgetCheck(pdb, unhealthy); c.State != "advisory" {
		t.Fatalf("IfHealthyBudget %v", c)
	}
	model.Map(pdb, "status")["observedGeneration"] = 0
	if c := budgetCheck(pdb, unhealthy); c.State != "unknown" {
		t.Fatalf("stale PDB %v", c)
	}
}
func TestMaintenanceSkipsMirrorAndDoesNotBlockTerminalEmptyDir(t *testing.T) {
	mirror := requestedPod("mirror")
	model.Map(mirror, "spec")["nodeName"] = "worker"
	model.Map(mirror, "metadata")["annotations"] = map[string]any{"kubernetes.io/config.mirror": "private-marker"}
	model.Map(mirror, "spec")["volumes"] = []any{map[string]any{"emptyDir": map[string]any{}}}
	terminal := requestedPod("done")
	model.Map(terminal, "spec")["nodeName"] = "worker"
	model.Map(terminal, "status")["phase"] = "Succeeded"
	model.Map(terminal, "spec")["volumes"] = []any{map[string]any{"emptyDir": map[string]any{}}}
	p := &investigationReader{Provider: provider.NewDemo(), objects: []model.Object{mirror, terminal, nodeFixture("worker", "a")}}
	out, e := New(p).NodeMaintenance(context.Background(), "worker", "")
	if e != nil || len(out.Pods) != 2 {
		t.Fatal(e)
	}
	for _, pod := range out.Pods {
		for _, c := range pod.Checks {
			if c.State == "constraint" {
				t.Fatalf("%s: %+v", pod.Class, c)
			}
		}
	}
}

func TestSchedulingWaitForConsumerAndPVNodeAffinity(t *testing.T) {
	pod := requestedPod("storage")
	model.Map(pod, "spec")["volumes"] = []any{map[string]any{"persistentVolumeClaim": map[string]any{"claimName": "data"}}}
	pvc := investigationObject("PersistentVolumeClaim", "production", "data", "claim")
	model.Map(pvc, "spec")["storageClassName"] = "delayed"
	sc := investigationObject("StorageClass", "", "delayed", "class")
	sc["volumeBindingMode"] = "WaitForFirstConsumer"
	node := nodeFixture("worker", "a")
	p := &investigationReader{Provider: provider.NewDemo(), objects: []model.Object{pod, pvc, sc, node}}
	s := New(p)
	out, e := s.Scheduling(context.Background(), "production", "storage")
	if e != nil || !hasCheck(out.Checks, "volume-binding", "observed") || hasCheck(out.Checks, "volume-binding", "constraint") {
		t.Fatalf("%+v %v", out.Checks, e)
	}
	pv := investigationObject("PersistentVolume", "", "volume", "volume")
	model.Map(pvc, "spec")["volumeName"] = "volume"
	model.Map(pv, "spec")["claimRef"] = map[string]any{"uid": "claim", "namespace": "production", "name": "data"}
	model.Map(pv, "spec")["nodeAffinity"] = map[string]any{"required": map[string]any{"nodeSelectorTerms": []any{map[string]any{"matchExpressions": []any{map[string]any{"key": "zone", "operator": "In", "values": []any{"other"}}}}}}}
	p.objects = append(p.objects, pv)
	out, e = s.Scheduling(context.Background(), "production", "storage")
	if e != nil || !hasCheck(out.Nodes[0].Checks, "volume-affinity", "constraint") {
		t.Fatalf("%+v %v", out.Nodes, e)
	}
}
func TestMaintenanceDaemonSetEmptyDirIsSkippedAndOverlappingPDBsAreNotAdded(t *testing.T) {
	ds := investigationObject("DaemonSet", "production", "agent", "agent")
	pod := requestedPod("agent-pod")
	owner(pod, ds)
	model.Map(pod, "spec")["nodeName"] = "worker"
	model.Map(pod, "status")["phase"] = "Running"
	model.Map(pod, "spec")["volumes"] = []any{map[string]any{"emptyDir": map[string]any{}}}
	p := &investigationReader{Provider: provider.NewDemo(), objects: []model.Object{nodeFixture("worker", "a"), ds, pod}}
	out, e := New(p).NodeMaintenance(context.Background(), "worker", "")
	if e != nil || out.Pods[0].Class != "daemonset" || hasCheck(out.Pods[0].Checks, "emptydir", "constraint") {
		t.Fatalf("%+v %v", out, e)
	}
	p.objects[1] = investigationObject("Deployment", "production", "app", "app")
	owner(pod, p.objects[1])
	model.Map(pod, "spec")["volumes"] = []any{}
	model.Map(pod, "status")["conditions"] = []any{map[string]any{"type": "Ready", "status": "True"}}
	for _, name := range []string{"one", "two"} {
		pdb := investigationObject("PodDisruptionBudget", "production", name, name)
		model.Map(pdb, "metadata")["generation"] = 1
		pdb["spec"] = map[string]any{"selector": map[string]any{}}
		pdb["status"] = map[string]any{"observedGeneration": 1, "disruptionsAllowed": 2}
		p.objects = append(p.objects, pdb)
	}
	out, e = New(p).NodeMaintenance(context.Background(), "worker", "")
	if e != nil || !hasCheck(out.Pods[0].Checks, "pdb-overlap", "constraint") {
		t.Fatalf("%+v %v", out, e)
	}
}
