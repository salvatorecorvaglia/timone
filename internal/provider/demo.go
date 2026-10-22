package provider

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
	"timone/internal/model"
)

type demo struct {
	objects []model.Object
	events  []model.Event
	started time.Time
}

func NewDemo() Provider           { d := &demo{started: time.Now().UTC()}; d.seed(); return d }
func (d *demo) Name() string      { return "harbor-demo" }
func (d *demo) Mode() string      { return "demo" }
func (d *demo) Namespace() string { return "" }
func (d *demo) add(kind, ns, name string, spec, status map[string]any, labels map[string]any) model.Object {
	o := model.Object{"apiVersion": "v1", "kind": kind, "metadata": map[string]any{"name": name, "namespace": ns, "uid": model.ID(kind, ns, name), "creationTimestamp": d.started.Add(-72 * time.Hour).Format(time.RFC3339), "labels": labels}, "spec": spec, "status": status}
	if k, ok := FindKind(kind); ok && k.Group != "" {
		o["apiVersion"] = k.Group + "/" + k.Version
	}
	switch kind {
	case "Deployment", "StatefulSet", "ReplicaSet", "DaemonSet":
		model.Map(o, "metadata")["generation"] = 1
		if status != nil {
			status["observedGeneration"] = 1
			if kind == "StatefulSet" {
				status["updatedReplicas"] = status["readyReplicas"]
			}
			if kind == "DaemonSet" {
				status["updatedNumberScheduled"] = status["desiredNumberScheduled"]
			}
		}
	case "Pod":
		if status != nil {
			ready := "True"
			for _, raw := range model.Arr(status, "containerStatuses") {
				c, _ := raw.(map[string]any)
				if !model.Bool(c, "ready") {
					ready = "False"
				}
			}
			status["conditions"] = []any{map[string]any{"type": "Ready", "status": ready, "lastTransitionTime": d.started.Add(-time.Hour).Format(time.RFC3339)}}
		}
	}
	d.objects = append(d.objects, o)
	return o
}
func (d *demo) seed() {
	for _, ns := range []string{"production", "platform", "staging", "kube-system"} {
		d.add("Namespace", "", ns, nil, map[string]any{"phase": "Active"}, nil)
	}
	for i := 1; i <= 3; i++ {
		d.add("Node", "", fmt.Sprintf("harbor-worker-%02d", i), map[string]any{}, map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}, "capacity": map[string]any{"cpu": "8", "memory": "32Gi"}, "allocatable": map[string]any{"cpu": "7800m", "memory": "30Gi"}, "nodeInfo": map[string]any{"kubeletVersion": "v1.35.2", "osImage": "Ubuntu 24.04 LTS"}}, map[string]any{"topology.kubernetes.io/zone": fmt.Sprintf("eu-west-1%c", 'a'+i-1)})
	}
	apps := []struct {
		name, ns, image string
		replicas, ready int
		state           string
	}{{"checkout-api", "production", "ghcr.io/timone-demo/checkout:v2.8.1", 3, 1, "CrashLoopBackOff"}, {"storefront", "production", "ghcr.io/timone-demo/storefront:v4.12.0", 3, 3, "Running"}, {"payments", "production", "ghcr.io/timone-demo/payments:v1.9.3", 2, 2, "Running"}, {"inventory", "production", "ghcr.io/timone-demo/inventory:v3.2.0", 2, 2, "Running"}, {"notification-worker", "production", "ghcr.io/timone-demo/notifications:v1.4.0", 2, 1, "Pending"}, {"prometheus", "platform", "quay.io/prometheus/prometheus:v3.2.0", 1, 1, "Running"}, {"catalog-preview", "staging", "ghcr.io/timone-demo/catalog:latest", 1, 0, "ImagePullBackOff"}}
	for ai, a := range apps {
		labels := map[string]any{"app.kubernetes.io/name": a.name, "app.kubernetes.io/instance": a.name, "app": a.name, "app.kubernetes.io/managed-by": "Helm"}
		containers := []any{map[string]any{"name": a.name, "image": a.image, "ports": []any{map[string]any{"containerPort": 8080}}, "resources": map[string]any{"requests": map[string]any{"cpu": "100m", "memory": "128Mi"}, "limits": map[string]any{"cpu": "500m", "memory": "512Mi"}}, "readinessProbe": map[string]any{"httpGet": map[string]any{"path": "/ready", "port": 8080}}, "livenessProbe": map[string]any{"httpGet": map[string]any{"path": "/health", "port": 8080}}, "securityContext": map[string]any{"runAsNonRoot": true, "allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "capabilities": map[string]any{"drop": []any{"ALL"}}}}}
		if a.name == "checkout-api" {
			containers[0].(map[string]any)["env"] = []any{map[string]any{"name": "DATABASE_URL", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "checkout-db", "key": "url"}}}}
		}
		ps := map[string]any{"containers": containers, "serviceAccountName": "default", "securityContext": map[string]any{"runAsNonRoot": true}}
		d.add("Deployment", a.ns, a.name, map[string]any{"replicas": a.replicas, "selector": map[string]any{"matchLabels": map[string]any{"app": a.name}}, "template": map[string]any{"metadata": map[string]any{"labels": labels}, "spec": ps}}, map[string]any{"readyReplicas": a.ready, "availableReplicas": a.ready, "updatedReplicas": a.replicas}, labels)
		rsname := a.name + "-7c8d9f"
		rs := d.add("ReplicaSet", a.ns, rsname, map[string]any{"replicas": a.replicas}, map[string]any{"readyReplicas": a.ready}, labels)
		model.Map(rs, "metadata")["ownerReferences"] = []any{map[string]any{"kind": "Deployment", "name": a.name, "controller": true, "uid": model.ID("Deployment", a.ns, a.name)}}
		d.add("Service", a.ns, a.name, map[string]any{"selector": map[string]any{"app": a.name}, "type": "ClusterIP", "clusterIP": fmt.Sprintf("10.43.0.%d", ai+10), "ports": []any{map[string]any{"port": 80, "targetPort": 8080}}}, nil, labels)
		for i := 0; i < a.replicas; i++ {
			name := fmt.Sprintf("%s-%s", rsname, []string{"k9x2p", "m4n7q", "r8t3v"}[i])
			state := a.state
			if i < a.ready {
				state = "Running"
			}
			ready := state == "Running"
			phase := "Running"
			cs := map[string]any{"running": map[string]any{"startedAt": d.started.Add(-4 * time.Hour).Format(time.RFC3339)}}
			restarts := 0
			if state == "Pending" {
				phase = "Pending"
				cs = map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}}
			}
			if strings.Contains(state, "BackOff") {
				restarts = 8 + i
				cs = map[string]any{"waiting": map[string]any{"reason": state, "message": "Simulated failure for troubleshooting"}}
			}
			if state == "Pending" {
				cs = map[string]any{"waiting": map[string]any{"reason": "Pending"}}
			}
			podspec := map[string]any{}
			for k, v := range ps {
				podspec[k] = v
			}
			if state != "Pending" {
				podspec["nodeName"] = fmt.Sprintf("harbor-worker-%02d", (ai+i)%3+1)
			}
			pod := d.add("Pod", a.ns, name, podspec, map[string]any{"phase": phase, "podIP": fmt.Sprintf("10.42.%d.%d", ai+1, i+10), "containerStatuses": []any{map[string]any{"name": a.name, "ready": ready, "restartCount": restarts, "state": cs}}}, labels)
			model.Map(pod, "metadata")["ownerReferences"] = []any{map[string]any{"kind": "ReplicaSet", "name": rsname, "controller": true, "uid": model.ID("ReplicaSet", a.ns, rsname)}}
			if !ready {
				reason, msg := "BackOff", "Back-off restarting failed container. Last exit code: 1."
				if state == "Pending" {
					reason = "FailedScheduling"
					msg = "0/3 nodes are available: 3 Insufficient memory."
				}
				if state == "ImagePullBackOff" {
					reason = "Failed"
					msg = "Failed to pull image: manifest unknown. Check the image tag and registry permissions."
				}
				d.events = append(d.events, model.Event{ID: "event-" + name, Namespace: a.ns, Resource: name, Kind: "Pod", ResourceID: model.ID("Pod", a.ns, name), Type: "Warning", Reason: reason, Message: msg, Count: 12 + i, Timestamp: d.started.Add(time.Duration(-2-i) * time.Minute).Format(time.RFC3339)})
			}
		}
	}
	d.add("Secret", "production", "checkout-db", nil, nil, map[string]any{"app": "checkout-api"})
	d.add("ConfigMap", "production", "checkout-config", nil, nil, map[string]any{"app": "checkout-api"})["data"] = map[string]any{"LOG_LEVEL": "info", "HTTP_PORT": "8080"}
	for _, a := range []string{"postgres", "redis"} {
		labels := map[string]any{"app": a}
		d.add("StatefulSet", "production", a, map[string]any{"replicas": 1}, map[string]any{"readyReplicas": 1}, labels)
		pod := d.add("Pod", "production", a+"-0", map[string]any{"containers": []any{map[string]any{"name": a, "image": map[string]string{"redis": "redis:7.4.2", "postgres": "postgres:17.4"}[a]}}, "nodeName": "harbor-worker-01", "volumes": []any{map[string]any{"name": "data", "persistentVolumeClaim": map[string]any{"claimName": a + "-data"}}}}, map[string]any{"phase": "Running", "containerStatuses": []any{map[string]any{"name": a, "ready": true, "restartCount": 0}}}, labels)
		model.Map(pod, "metadata")["ownerReferences"] = []any{map[string]any{"kind": "StatefulSet", "name": a, "controller": true, "uid": model.ID("StatefulSet", "production", a)}}
		d.add("Service", "production", a, map[string]any{"selector": labels, "ports": []any{map[string]any{"port": 5432}}}, nil, labels)
		d.add("PersistentVolumeClaim", "production", a+"-data", map[string]any{"resources": map[string]any{"requests": map[string]any{"storage": "20Gi"}}, "accessModes": []any{"ReadWriteOnce"}}, map[string]any{"phase": "Bound"}, labels)
	}
	d.add("Ingress", "production", "public-storefront", map[string]any{"rules": []any{map[string]any{"host": "shop.demo.invalid", "http": map[string]any{"paths": []any{map[string]any{"path": "/", "backend": map[string]any{"service": map[string]any{"name": "storefront", "port": map[string]any{"number": 80}}}}}}}}}, nil, nil)
	d.add("CronJob", "platform", "database-backup", map[string]any{"schedule": "0 2 * * *"}, nil, nil)
	d.add("Job", "platform", "database-backup-29183040", nil, map[string]any{"succeeded": 1, "conditions": []any{map[string]any{"type": "Complete", "status": "True"}}}, nil)
	d.add("DaemonSet", "kube-system", "node-agent", nil, map[string]any{"numberReady": 3, "desiredNumberScheduled": 3, "numberUnavailable": 0}, nil)
	for _, ns := range []string{"production", "staging", "platform"} {
		d.add("ServiceAccount", ns, "default", nil, nil, nil)
	}
	for _, name := range []string{"storefront", "checkout-api", "payments"} {
		health, sync := "Healthy", "Synced"
		conditions := []any{}
		if name == "checkout-api" {
			health = "Degraded"
			conditions = append(conditions, map[string]any{"type": "SyncError", "message": "Simulated controller report: deployment health check failed", "lastTransitionTime": d.started.Add(-5 * time.Minute).Format(time.RFC3339)})
		}
		if name == "payments" {
			sync = "OutOfSync"
		}
		application := d.add("Application", "platform", name, map[string]any{"source": map[string]any{"repoURL": "https://example.invalid/platform/apps", "path": "apps/" + name}}, map[string]any{"resources": []any{map[string]any{"group": "apps", "kind": "Deployment", "namespace": "production", "name": name}}, "sync": map[string]any{"status": sync, "revision": "8f31a2c"}, "health": map[string]any{"status": health}, "reconciledAt": d.started.Add(-time.Minute).Format(time.RFC3339), "conditions": conditions, "operationState": map[string]any{"phase": "Succeeded"}}, nil)
		model.Map(application, "metadata")["generation"] = 1
	}
	k := d.add("Kustomization", "platform", "cluster-config", map[string]any{"suspend": false}, map[string]any{"observedGeneration": 1, "lastAppliedRevision": "main@sha1:8f31a2c", "lastAttemptedRevision": "main@sha1:8f31a2c", "conditions": []any{map[string]any{"type": "Ready", "status": "True", "observedGeneration": 1, "reason": "ReconciliationSucceeded", "message": "Applied revision main@sha1:8f31a2c"}}}, nil)
	model.Map(k, "metadata")["generation"] = 1
	h := d.add("HelmRelease", "platform", "monitoring", map[string]any{"driftDetection": map[string]any{"mode": "warn"}}, map[string]any{"observedGeneration": 2, "lastAttemptedRevision": "2.3.0", "history": []any{map[string]any{"status": "deployed", "chartVersion": "2.3.0"}}, "conditions": []any{map[string]any{"type": "Ready", "status": "True", "observedGeneration": 2, "reason": "UpgradeSucceeded", "message": "Simulated successful Helm upgrade"}, map[string]any{"type": "Drifted", "status": "True", "observedGeneration": 2, "reason": "DriftDetected", "message": "Simulated controller report: managed Deployment differs from release"}}}, nil)
	model.Map(h, "metadata")["generation"] = 2

	role := d.add("Role", "production", "release-secret-reader", nil, nil, nil)
	role["rules"] = []any{map[string]any{"apiGroups": []any{""}, "resources": []any{"secrets"}, "verbs": []any{"get", "list"}}}
	binding := d.add("RoleBinding", "production", "default-secret-reader", nil, nil, nil)
	binding["roleRef"] = map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": "release-secret-reader"}
	binding["subjects"] = []any{map[string]any{"kind": "ServiceAccount", "name": "default", "namespace": "production"}}
	d.seedHelm()

	normal := []struct{ kind, name, reason, msg, ns string }{{"Deployment", "storefront", "ScalingReplicaSet", "Scaled up replica set storefront-7c8d9f to 3", "production"}, {"Job", "database-backup-29183040", "Completed", "Job completed successfully", "platform"}, {"Node", "harbor-worker-03", "NodeReady", "Node status is now: NodeReady", ""}, {"Deployment", "checkout-api", "ScalingReplicaSet", "Scaled up replica set checkout-api-7c8d9f to 3", "production"}}
	for i, e := range normal {
		d.events = append(d.events, model.Event{ID: fmt.Sprintf("normal-%d", i), Namespace: e.ns, Resource: e.name, Kind: e.kind, ResourceID: model.ID(e.kind, e.ns, e.name), Type: "Normal", Reason: e.reason, Message: e.msg, Count: 1, Timestamp: d.started.Add(-time.Duration(5+i*4) * time.Minute).Format(time.RFC3339)})
	}
	// Populate identity and occurrence metadata without changing the simulated event range.
	for i := range d.events {
		d.events[i].ResourceUID = d.events[i].ResourceID
		d.events[i].ResourceNamespace = d.events[i].Namespace
		d.events[i].FirstObserved = d.events[i].Timestamp
	}
	d.seedGateway()
	d.seedGrants()
	d.seedCustom()

}
func (d *demo) List(ctx context.Context, kind, ns string) ([]model.Object, bool, error) {
	spec, ok := FindKind(kind)
	if !ok {
		return nil, false, &Error{400, "Unsupported resource kind"}
	}
	out := []model.Object{}
	for _, o := range d.objects {
		if model.Str(o, "kind") == kind && (!spec.Namespaced || ns == "" || model.Str(o, "metadata", "namespace") == ns) {
			out = append(out, model.Sanitize(o))
		}
	}
	return out, false, ctx.Err()
}
func (d *demo) Get(ctx context.Context, kind, ns, name string) (model.Object, error) {
	for _, o := range d.objects {
		if model.Str(o, "kind") == kind && model.Str(o, "metadata", "name") == name && model.Str(o, "metadata", "namespace") == ns {
			return model.Sanitize(o), ctx.Err()
		}
	}
	return nil, ErrNotFound
}
func (d *demo) Events(ctx context.Context, ns string) ([]model.Event, error) {
	out := []model.Event{}
	for _, e := range d.events {
		if ns == "" || e.Namespace == ns {
			out = append(out, e)
		}
	}
	return out, ctx.Err()
}
func (d *demo) Metrics(ctx context.Context, ns string) (model.Metrics, error) {
	m := model.Metrics{Available: true, Source: "simulated", Message: "Simulated current samples. Historical charts use a separate isolated demo source.", Items: []model.Metric{}}
	pulse := math.Sin(time.Since(d.started).Seconds()/60) * 0.08
	for i, o := range d.objects {
		kind := model.Str(o, "kind")
		if kind == "Pod" && model.Summarize(o).Status != "Running" {
			continue
		}
		if kind != "Node" && kind != "Pod" {
			continue
		}
		n := model.Str(o, "metadata", "namespace")
		if kind == "Pod" && ns != "" && n != ns {
			continue
		}
		cpu, mem := 0.05+float64(i%7)*0.03, float64(80+i%8*20)*(1<<20)
		if kind == "Node" {
			cpu = 2.1 + float64(i%3)*0.7 + pulse
			mem = float64(10+i%3*3) * (1 << 30)
		}
		item := model.Metric{Name: model.Str(o, "metadata", "name"), Namespace: n, Kind: kind, CPU: cpu, Memory: mem, Timestamp: time.Now().UTC().Format(time.RFC3339)}
		if kind == "Pod" {
			c, _ := model.Arr(o, "spec", "containers")[0].(map[string]any)
			item.Container = model.Str(c, "name")
		}
		if kind == "Node" {
			item.CPUCapacity = 8
			item.MemoryCapacity = 32 * (1 << 30)
		}
		m.Items = append(m.Items, item)
	}
	return m, ctx.Err()
}
func (d *demo) Logs(ctx context.Context, ns, pod, container string, tail int) (string, error) {
	o, e := d.Get(ctx, "Pod", ns, pod)
	if e != nil {
		return "", e
	}
	if container != "" {
		found := false
		for _, c := range model.Arr(o, "spec", "containers") {
			m, _ := c.(map[string]any)
			if model.Str(m, "name") == container {
				found = true
			}
		}
		if !found {
			return "", &Error{400, "Container not found"}
		}
	}
	ts := d.started.Format(time.RFC3339)
	if strings.HasPrefix(pod, "checkout") {
		return ts + " [SIMULATED] INFO starting checkout-api v2.8.1\n" + ts + " [SIMULATED] ERROR database connection rejected: connection limit reached\n" + ts + " [SIMULATED] FATAL startup failed, exit code 1\n", nil
	}
	return ts + " [SIMULATED] INFO application started\n" + ts + " [SIMULATED] INFO readiness probe succeeded\n", nil
}
func (d *demo) Watch(ctx context.Context, notify func()) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			notify()
		}
	}
}
