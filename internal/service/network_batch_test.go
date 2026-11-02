package service

import (
	"context"
	"strings"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

func networkPolicy(name, ns, direction string, rules []any) model.Object {
	p := investigationObject("NetworkPolicy", ns, name, name)
	p["spec"] = map[string]any{"podSelector": map[string]any{}, "policyTypes": []any{strings.ToUpper(direction[:1]) + direction[1:]}, direction: rules}
	return p
}
func TestNativePolicyUnionAndCoverage(t *testing.T) {
	dst := investigationObject("Pod", "production", "web", "web")
	src := investigationObject("Pod", "production", "client", "client")
	deny := networkPolicy("deny", "production", "ingress", nil)
	allow := networkPolicy("allow", "production", "ingress", []any{map[string]any{"ports": []any{map[string]any{"port": 8080}}}})
	cases := []struct {
		name     string
		policies []model.Object
		complete bool
		want     string
	}{
		{"not isolated", nil, true, "not-isolated-by-native-policies"},
		{"deny all", []model.Object{deny}, true, "denied-by-declared-rules"},
		{"additive allowance", []model.Object{deny, allow}, true, "allowed-by-observed-rules"},
		{"partial deny", []model.Object{deny}, false, "unknown"},
		{"partial allowance", []model.Object{allow}, false, "allowed-by-observed-rules"},
		{"partial empty", nil, false, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluatePolicies("ingress", dst, src, dst, 8080, "TCP", tc.policies, tc.complete, nil)
			if got.State != tc.want {
				t.Fatalf("%s != %s", got.State, tc.want)
			}
		})
	}
	if got := evaluatePolicies("egress", src, dst, dst, 8080, "TCP", []model.Object{deny}, true, nil); got.State != "not-isolated-by-native-policies" {
		t.Fatal(got)
	}
	model.Map(src, "spec")["hostNetwork"] = true
	if got := evaluatePolicies("ingress", dst, src, dst, 8080, "TCP", []model.Object{allow}, true, nil); got.State != "unknown" {
		t.Fatal(got)
	}
}
func TestPolicySelectorsAndDestinationPorts(t *testing.T) {
	src := investigationObject("Pod", "clients", "client", "client")
	model.Map(src, "metadata")["labels"] = map[string]any{"role": "client"}
	dst := investigationObject("Pod", "production", "web", "web")
	dst["spec"] = map[string]any{"containers": []any{map[string]any{"ports": []any{map[string]any{"name": "http", "containerPort": 8080}}}}}
	ns := investigationObject("Namespace", "", "clients", "clients")
	model.Map(ns, "metadata")["labels"] = map[string]any{"team": "frontend"}
	peer := map[string]any{"podSelector": map[string]any{"matchLabels": map[string]any{"role": "client"}}, "namespaceSelector": map[string]any{"matchLabels": map[string]any{"team": "frontend"}}}
	if policyPeer(peer, src, "production", nil) != policyUnknown {
		t.Fatal("missing namespace labels must be unknown")
	}
	namespaces := map[string]model.Object{"clients": ns}
	if policyPeer(peer, src, "production", namespaces) != policyYes {
		t.Fatal("selectors must match together")
	}
	model.Map(src, "metadata", "labels")["role"] = "other"
	if policyPeer(peer, src, "production", namespaces) != policyNo {
		t.Fatal("namespace match alone must not allow")
	}
	if policyPeer(map[string]any{"podSelector": map[string]any{}}, src, "production", nil) != policyNo {
		t.Fatal("pod selector alone must stay in policy namespace")
	}
	if policyPeer(map[string]any{"namespaceSelector": map[string]any{}}, src, "production", nil) != policyYes {
		t.Fatal("empty namespace selector matches all namespaces")
	}
	if policyPeer(map[string]any{"ipBlock": map[string]any{"cidr": "10.0.0.0/8"}}, src, "production", nil) != policyUnknown {
		t.Fatal("ipBlock not inferred")
	}
	for _, port := range []map[string]any{{"port": "http"}, {"port": 8000, "endPort": 8090}, {"port": 8080}} {
		if policyPort(port, dst, 8080, "TCP") != policyYes {
			t.Fatal(port)
		}
	}
	if policyPort(map[string]any{"port": "missing"}, dst, 8080, "TCP") != policyUnknown {
		t.Fatal("missing named port")
	}
	if policyPort(map[string]any{"port": 8080, "protocol": "UDP"}, dst, 8080, "TCP") != policyNo {
		t.Fatal("protocol mismatch")
	}
	if got := evaluatePolicies("egress", nil, dst, dst, 8080, "TCP", nil, true, nil); got.State != "source-required" {
		t.Fatal(got)
	}
}
func networkFixture() (*investigationReader, model.Object, model.Object) {
	svc := investigationObject("Service", "production", "web", "service-uid")
	svc["spec"] = map[string]any{"selector": map[string]any{"app": "web"}, "ports": []any{map[string]any{"port": 80, "targetPort": 8080}}}
	pod := investigationObject("Pod", "production", "web-0", "pod-uid")
	slice := investigationObject("EndpointSlice", "production", "web-a", "slice-uid")
	model.Map(slice, "metadata")["labels"] = map[string]any{"kubernetes.io/service-name": "web"}
	slice["ports"] = []any{map[string]any{"port": 8080}}
	slice["endpoints"] = []any{map[string]any{"addresses": []any{"10.0.0.1"}, "targetRef": map[string]any{"kind": "Pod", "name": "web-0", "uid": "pod-uid"}, "conditions": map[string]any{"ready": true}}}
	return &investigationReader{Provider: provider.NewDemo(), objects: []model.Object{svc, pod, slice}}, pod, slice
}
func TestNetworkExactUIDAndSlicePortSets(t *testing.T) {
	p, _, slice := networkFixture()
	other := investigationObject("EndpointSlice", "production", "web-b", "slice-b")
	model.Map(other, "metadata")["labels"] = map[string]any{"kubernetes.io/service-name": "web"}
	other["ports"] = []any{map[string]any{"name": "metrics", "port": 9000}}
	p.objects = append(p.objects, other)
	s := New(p)
	a, err := s.Network(context.Background(), "Service", "production", "web", "production", "web-0")
	if err != nil || len(a.Paths) != 1 || len(a.Paths[0].Endpoints) != 1 {
		t.Fatalf("%+v %v", a, err)
	}
	ep := a.Paths[0].Endpoints[0]
	if ep.Pod == nil || ep.Ingress.State != "same-pod-traffic" || ep.Egress.State != "same-pod-traffic" {
		t.Fatalf("%+v", ep)
	}
	for _, c := range a.Paths[0].Checks {
		if c.State == "constraint" {
			t.Fatal("port-grouped slices are valid", c)
		}
	}
	// A numeric targetPort needs no containerPort declaration.
	for _, c := range ep.Checks {
		if c.Code == "target-port" {
			t.Fatal(c)
		}
	}
	model.Map(model.Arr(slice, "endpoints")[0].(map[string]any), "targetRef")["uid"] = "old-uid"
	a, err = s.Network(context.Background(), "Service", "production", "web", "", "")
	if err != nil || a.Paths[0].Endpoints[0].Pod != nil || a.Paths[0].Endpoints[0].Ingress.State != "unknown" {
		t.Fatalf("stale UID: %+v %v", a, err)
	}
}
func TestNetworkCoverageAndExternalName(t *testing.T) {
	p, _, _ := networkFixture()
	p.denied = "NetworkPolicy"
	a, err := New(p).Network(context.Background(), "Service", "production", "web", "", "")
	if err != nil || len(a.Warnings) == 0 || a.Paths[0].Endpoints[0].Ingress.State != "unknown" {
		t.Fatalf("%+v %v", a, err)
	}
	p.denied = ""
	model.Map(p.objects[0], "spec")["type"] = "ExternalName"
	a, err = New(p).Network(context.Background(), "Service", "production", "web", "", "")
	if err != nil || len(a.Paths[0].Endpoints) != 0 || a.Paths[0].Checks[0].Code != "external-name" {
		t.Fatalf("%+v %v", a, err)
	}
	if _, err = New(p).Network(context.Background(), "Service", "production", "web", "production", ""); err == nil {
		t.Fatal("namespace without source accepted")
	}
}
func TestJobAttemptsAndControllerConditions(t *testing.T) {
	job := investigationObject("Job", "production", "backup", "job")
	job["status"] = map[string]any{"active": 1, "failed": 3, "startTime": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}
	job["spec"] = map[string]any{"backoffLimit": 6, "activeDeadlineSeconds": 60}
	if got := inspectJob(job, time.Now()); got.State != "active" || len(got.Checks) < 2 {
		t.Fatal(got)
	}
	cases := map[string]string{"FailureTarget": "failure-target-pending-termination", "SuccessCriteriaMet": "success-target-pending-termination", "Failed": "failed", "Complete": "complete"}
	for condition, want := range cases {
		model.Map(job, "status")["conditions"] = []any{map[string]any{"type": condition, "status": "True"}}
		if got := inspectJob(job, time.Now()); got.State != want {
			t.Fatal(got)
		}
	}
	delete(model.Map(job, "status"), "conditions")
	model.Map(job, "spec")["suspend"] = true
	if got := inspectJob(job, time.Now()); got.State != "suspended" {
		t.Fatal(got)
	}
	model.Map(job, "status")["completedIndexes"] = strings.Repeat("1", 3000)
	if got := inspectJob(job, time.Now()); len(got.CompletedIndexes) != 2048 {
		t.Fatal("unbounded indexed output")
	}
}
func TestCronJobRetainedFailureAndOwnership(t *testing.T) {
	cron := investigationObject("CronJob", "production", "backup", "cron")
	old := investigationObject("Job", "production", "old", "old-job")
	owner(old, cron)
	old["status"] = map[string]any{"failed": 3, "conditions": []any{map[string]any{"type": "Failed", "status": "True", "reason": "BackoffLimitExceeded"}}}
	newer := investigationObject("Job", "production", "new", "new-job")
	owner(newer, cron)
	newer["status"] = map[string]any{"succeeded": 1, "conditions": []any{map[string]any{"type": "Complete", "status": "True"}}}
	stale := investigationObject("Job", "production", "stale", "stale-job")
	owner(stale, cron)
	model.Map(model.Arr(stale, "metadata", "ownerReferences")[0].(map[string]any))["uid"] = "previous-cron"
	pod := investigationObject("Pod", "production", "old-0", "old-pod")
	owner(pod, old)
	pod["status"] = map[string]any{"phase": "Failed"}
	p := &investigationReader{Provider: provider.NewDemo(), objects: []model.Object{cron, old, newer, stale, pod}}
	a, err := New(p).Batch(context.Background(), "CronJob", "production", "backup")
	if err != nil || len(a.Jobs) != 2 || len(a.Pods) != 1 || a.State != "idle-reported" || len(a.Issues) != 0 {
		t.Fatalf("%+v %v", a, err)
	}
	model.Map(cron, "status")["active"] = []any{map[string]any{"name": "new", "uid": "previous-job"}}
	a, err = New(p).Batch(context.Background(), "CronJob", "production", "backup")
	if err != nil || a.State != "active-status-unverified" {
		t.Fatalf("%+v %v", a, err)
	}
	p.denied = "Job"
	a, err = New(p).Batch(context.Background(), "CronJob", "production", "backup")
	if err != nil || len(a.Warnings) == 0 {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestNetworkAndBatchRespectConfiguredScope(t *testing.T) {
	p, _, _ := networkFixture()
	s := New(scoped{p})
	if _, err := s.Network(context.Background(), "Service", "production", "web", "staging", "client"); provider.Status(err) != 403 {
		t.Fatalf("source escaped scope: %v", err)
	}
	if _, err := s.Network(context.Background(), "Service", "staging", "web", "", ""); provider.Status(err) != 403 {
		t.Fatalf("root escaped scope: %v", err)
	}
	if _, err := s.Batch(context.Background(), "CronJob", "staging", "backup"); provider.Status(err) != 403 {
		t.Fatalf("batch escaped scope: %v", err)
	}
}

func TestBatchIncompleteHistoryDoesNotEstablishIdle(t *testing.T) {
	cron := investigationObject("CronJob", "production", "backup", "cron")
	p := &investigationReader{Provider: provider.NewDemo(), objects: []model.Object{cron}, denied: "Job"}
	a, err := New(p).Batch(context.Background(), "CronJob", "production", "backup")
	if err != nil || a.State != "unknown" || len(a.Warnings) == 0 {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestNetworkIngressPortResolution(t *testing.T) {
	p, _, _ := networkFixture()
	ingress := investigationObject("Ingress", "production", "public", "ingress")
	ingress["spec"] = map[string]any{"defaultBackend": map[string]any{"service": map[string]any{"name": "web", "port": map[string]any{"number": 80}}}}
	p.objects = append(p.objects, ingress)
	a, err := New(p).Network(context.Background(), "Ingress", "production", "public", "", "")
	if err != nil || len(a.Paths) != 1 || a.Paths[0].ServicePort != 80 {
		t.Fatalf("%+v %v", a, err)
	}
	model.Map(ingress, "spec", "defaultBackend", "service", "port")["number"] = 9999
	a, err = New(p).Network(context.Background(), "Ingress", "production", "public", "", "")
	if err != nil || len(a.Paths) != 0 {
		t.Fatalf("%+v %v", a, err)
	}
	found := false
	for _, c := range a.Checks {
		found = found || c.Code == "service-port" && c.State == "constraint"
	}
	if !found {
		t.Fatal(a)
	}
}

func TestNetworkCrossNamespaceRouteGrants(t *testing.T) {
	p, _, _ := networkFixture()
	route := investigationObject("HTTPRoute", "clients", "public", "route")
	route["apiVersion"] = gatewayGroup + "/v1"
	route["spec"] = map[string]any{"rules": []any{map[string]any{"backendRefs": []any{map[string]any{"name": "web", "namespace": "production", "port": 80}}}}}
	p.objects = append(p.objects, route)
	hasGrantState := func(want string) {
		t.Helper()
		a, err := New(p).Network(context.Background(), "HTTPRoute", "clients", "public", "", "")
		if err != nil || len(a.Paths) != 1 {
			t.Fatalf("%+v %v", a, err)
		}
		for _, c := range a.Paths[0].Checks {
			if c.Code == "route-backend" && strings.Contains(c.Message, "ReferenceGrant") && c.State == want {
				return
			}
		}
		t.Fatalf("missing %s grant result: %+v", want, a.Paths[0].Checks)
	}
	hasGrantState("constraint")
	p.denied = "ReferenceGrant"
	hasGrantState("unknown")
	p.denied = ""
	grant := investigationObject("ReferenceGrant", "production", "client-web", "grant")
	grant["spec"] = map[string]any{"from": []any{map[string]any{"group": gatewayGroup, "kind": "HTTPRoute", "namespace": "clients"}}, "to": []any{map[string]any{"group": "", "kind": "Service", "name": "web"}}}
	p.objects = append(p.objects, grant)
	hasGrantState("observed")
}
