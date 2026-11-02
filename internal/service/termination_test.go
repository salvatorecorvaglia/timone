package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

func TestDeletionTimestampIsNotRequestAge(t *testing.T) {
	now := time.Now().UTC()
	pod := investigationObject("Pod", "production", "app", "pod")
	model.Map(pod, "spec")["terminationGracePeriodSeconds"] = 300
	model.Map(pod, "metadata")["deletionGracePeriodSeconds"] = 30
	model.Map(pod, "metadata")["finalizers"] = []any{"example.com/cleanup"}
	a := observeDeletion(pod, now)
	if a.State != "not-marked-for-deletion" || a.SecondsPastTimestamp != nil || a.Checks[0].State != "observed" {
		t.Fatal(a)
	}
	model.Map(pod, "metadata")["deletionTimestamp"] = now.Add(20 * time.Second).Format(time.RFC3339Nano)
	a = observeDeletion(pod, now)
	if a.State != "deletion-timestamp-in-future" || a.SecondsPastTimestamp != nil || *a.SecondsUntilTimestamp != 20 {
		t.Fatal(a)
	}
	model.Map(pod, "metadata")["deletionTimestamp"] = now.Add(-10 * time.Second).Format(time.RFC3339Nano)
	a = observeDeletion(pod, now)
	if a.State != "deletion-pending" || *a.SecondsPastTimestamp != 10 || *a.DeletionGraceSeconds != 30 || *a.ConfiguredGraceSeconds != 300 {
		t.Fatal("grace was added to timestamp", a)
	}
	model.Map(pod, "metadata")["deletionTimestamp"] = "invalid"
	a = observeDeletion(pod, now)
	if a.State != "invalid-deletion-timestamp" || a.SecondsPastTimestamp != nil {
		t.Fatal(a)
	}
}
func TestNamespaceFinalizersAreSeparateAndBounded(t *testing.T) {
	o := investigationObject("Namespace", "", "old", "ns")
	model.Map(o, "spec")["finalizers"] = []any{"kubernetes"}
	values := []any{}
	for i := 0; i < 70; i++ {
		values = append(values, fmt.Sprintf("example.com/cleanup-%d", i))
	}
	model.Map(o, "metadata")["finalizers"] = values
	a := observeDeletion(o, time.Now())
	if len(a.MetadataFinalizers) != 64 || len(a.NamespaceFinalizers) != 1 || a.NamespaceFinalizers[0] != "kubernetes" {
		t.Fatal(a)
	}
	found := false
	for _, c := range a.Checks {
		found = found || c.Code == "finalizer-coverage"
	}
	if !found {
		t.Fatal(a)
	}
}
func TestTerminationPodOwnersNodeAndSanitizedHookEvidence(t *testing.T) {
	pod := investigationObject("Pod", "production", "app-0", "pod")
	parent := investigationObject("StatefulSet", "production", "app", "parent")
	parent["apiVersion"] = "apps/v1"
	owner(pod, parent)
	model.Arr(pod, "metadata", "ownerReferences")[0].(map[string]any)["apiVersion"] = "apps/v1"
	pod["spec"] = map[string]any{"nodeName": "node", "containers": []any{map[string]any{"name": "app", "lifecycle": map[string]any{"preStop": map[string]any{"exec": map[string]any{"command": []any{"DO_NOT_EXPORT_HOOK"}}}}, "env": []any{map[string]any{"name": "PASSWORD", "value": "DO_NOT_EXPORT_ENV"}}}}}
	node := investigationObject("Node", "", "node", "node")
	node["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "False", "reason": "KubeletNotReady"}}}
	p := &investigationReader{Provider: provider.NewDemo(), objects: []model.Object{pod, parent, node}}
	a, err := New(p).Termination(context.Background(), "Pod", "production", "app-0")
	if err != nil || a.State != "not-marked-for-deletion" {
		t.Fatalf("%+v %v", a, err)
	}
	ownerFound, nodeFound := false, false
	for _, r := range a.Relations {
		ownerFound = ownerFound || r.Relation == "declared-owner" && r.State == "uid-verified"
		nodeFound = nodeFound || r.Relation == "assigned-node"
	}
	if !ownerFound || !nodeFound {
		t.Fatal(a.Relations)
	}
	text, _ := json.Marshal(a)
	if strings.Contains(string(text), "DO_NOT_EXPORT") {
		t.Fatal("hook/env payload leaked", string(text))
	}
	model.Map(parent, "metadata")["uid"] = "replacement"
	a, err = New(p).Termination(context.Background(), "Pod", "production", "app-0")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range a.Relations {
		if r.Relation == "declared-owner" && (r.State != "unverified" || r.Resource != nil) {
			t.Fatal("replacement accepted as owner", r)
		}
	}
	p.denied = "Node"
	a, err = New(p).Termination(context.Background(), "Pod", "production", "app-0")
	if err != nil || len(a.Warnings) == 0 {
		t.Fatalf("%+v %v", a, err)
	}
}
func TestTerminationPVCBindingConsumersAndReplacementEvents(t *testing.T) {
	pvc := investigationObject("PersistentVolumeClaim", "production", "data", "claim")
	model.Map(pvc, "metadata")["deletionTimestamp"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	model.Map(pvc, "metadata")["finalizers"] = []any{"kubernetes.io/pvc-protection"}
	model.Map(pvc, "spec")["volumeName"] = "volume"
	pv := investigationObject("PersistentVolume", "", "volume", "volume")
	pv["spec"] = map[string]any{"claimRef": map[string]any{"uid": "claim", "name": "data", "namespace": "production"}, "persistentVolumeReclaimPolicy": "Retain"}
	pod := investigationObject("Pod", "production", "consumer", "pod")
	pod["spec"] = map[string]any{"volumes": []any{map[string]any{"persistentVolumeClaim": map[string]any{"claimName": "data"}}}}
	pod["status"] = map[string]any{"phase": "Succeeded"}
	attachment := investigationObject("VolumeAttachment", "", "attachment", "attachment")
	attachment["spec"] = map[string]any{"source": map[string]any{"persistentVolumeName": "volume"}}
	attachment["status"] = map[string]any{"attached": true, "detachError": map[string]any{"message": "Controller detach error"}}
	p := &investigationReader{Provider: provider.NewDemo(), objects: []model.Object{pvc, pv, pod, attachment}, events: []model.Event{
		{ID: "old", ResourceID: model.ID("PersistentVolumeClaim", "production", "data"), ResourceUID: "old-claim", Timestamp: time.Now().UTC().Format(time.RFC3339)},
		{ID: "current", ResourceID: model.ID("PersistentVolumeClaim", "production", "data"), ResourceUID: "claim", Timestamp: time.Now().UTC().Format(time.RFC3339)},
	}}
	a, err := New(p).Termination(context.Background(), "PersistentVolumeClaim", "production", "data")
	if err != nil || len(a.Relations) != 3 || len(a.Events) != 1 || a.Events[0].ID != "current" {
		t.Fatalf("%+v %v", a, err)
	}
	model.Map(pv, "spec", "claimRef")["uid"] = "old-claim"
	a, err = New(p).Termination(context.Background(), "PersistentVolumeClaim", "production", "data")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range a.Relations {
		if r.Kind == "PersistentVolume" || r.Kind == "VolumeAttachment" {
			t.Fatal("stale PV binding was followed", r)
		}
	}
	p.denied = "Pod"
	a, err = New(p).Termination(context.Background(), "PersistentVolumeClaim", "production", "data")
	if err != nil || len(a.Warnings) == 0 {
		t.Fatalf("%+v %v", a, err)
	}
	for _, c := range a.Checks {
		if c.Code == "claim-consumers" && c.State != "unknown" {
			t.Fatal("denied inventory became absence", c)
		}
	}
}
func TestTerminationNamespaceScopePartialInventoryAndPayloadOmission(t *testing.T) {
	ns := investigationObject("Namespace", "", "production", "namespace")
	ns["status"] = map[string]any{"phase": "Terminating", "conditions": []any{map[string]any{"type": "NamespaceContentRemaining", "status": "True", "reason": "SomeResourcesRemain"}}}
	secret := investigationObject("Secret", "production", "secret", "secret")
	secret["data"] = map[string]any{"password": "DO_NOT_EXPORT_SECRET"}
	other := investigationObject("Pod", "staging", "outside", "outside")
	p := &investigationReader{Provider: provider.NewDemo(), objects: []model.Object{ns, secret, other}, denied: "Job"}
	a, err := New(scoped{p}).Termination(context.Background(), "Namespace", "", "production")
	if err != nil || len(a.Collections) == 0 || len(a.Warnings) == 0 {
		t.Fatalf("%+v %v", a, err)
	}
	for _, r := range a.Relations {
		if r.Namespace != "production" {
			t.Fatal("namespace escaped", r)
		}
	}
	text, _ := json.Marshal(a)
	if strings.Contains(string(text), "DO_NOT_EXPORT_SECRET") {
		t.Fatal("namespace payload leaked")
	}
	deniedSeen := false
	for _, c := range a.Collections {
		if c.Kind == "Job" {
			deniedSeen = true
			if c.Complete {
				t.Fatal(c)
			}
		}
	}
	if !deniedSeen {
		t.Fatal(a.Collections)
	}
	if _, err = New(scoped{p}).Termination(context.Background(), "Namespace", "", "staging"); provider.Status(err) != 403 {
		t.Fatalf("namespace escaped configured scope: %v", err)
	}
}
func TestTerminationNamespacePrioritizesDeletingContentAndBoundsOutput(t *testing.T) {
	ns := investigationObject("Namespace", "", "production", "namespace")
	objects := []model.Object{ns}
	for i := 0; i < 210; i++ {
		objects = append(objects, investigationObject("ConfigMap", "production", fmt.Sprintf("config-%03d", i), fmt.Sprint(i)))
	}
	pod := investigationObject("Pod", "production", "important", "pod")
	model.Map(pod, "metadata")["deletionTimestamp"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	objects = append(objects, pod)
	p := &investigationReader{Provider: provider.NewDemo(), objects: objects}
	a, err := New(p).Termination(context.Background(), "Namespace", "", "production")
	if err != nil || len(a.Relations) != 200 || len(a.Warnings) == 0 || a.Relations[0].Kind != "Pod" {
		t.Fatalf("%+v %v", a, err)
	}
}
