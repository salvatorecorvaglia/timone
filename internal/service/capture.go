package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type CapturedResource struct {
	Resource      model.Resource   `json:"resource"`
	UID           string           `json:"uid"`
	Configuration map[string]any   `json:"configuration"`
	Conditions    []map[string]any `json:"conditions"`
}
type ResourceCapture struct {
	SchemaVersion int                `json:"schemaVersion"`
	Cluster       string             `json:"cluster"`
	ClusterKey    string             `json:"clusterKey"`
	Mode          string             `json:"mode"`
	CapturedAt    time.Time          `json:"capturedAt"`
	Root          string             `json:"root"`
	Resources     []CapturedResource `json:"resources"`
	Warnings      []model.Warning    `json:"warnings"`
	Limitations   []string           `json:"limitations"`
}

func (s *Service) ClusterKey() string {
	identity := "demo"
	if p, ok := s.Provider.(provider.ClusterIdentityProvider); ok {
		identity = p.ClusterIdentity()
	} else if s.Provider.Mode() != "demo" {
		identity = s.Provider.Mode() + ":" + s.Provider.Name()
	}
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}
func pickConfiguration(in map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, key := range keys {
		if v, ok := in[key]; ok {
			out[key] = v
		}
	}
	return out
}
func captureResource(r model.Resource) CapturedResource {
	o := model.Sanitize(r.Object)
	spec := model.Map(o, "spec")
	config := pickConfiguration(spec, "replicas", "selector", "strategy", "updateStrategy", "minReplicas", "maxReplicas", "scaleTargetRef", "metrics", "minAvailable", "maxUnavailable", "ports", "type", "storageClassName", "accessModes", "volumeName", "resources", "suspend", "schedule", "concurrencyPolicy")
	pod := model.Map(spec, "template", "spec")
	if r.Kind == "Pod" {
		pod = spec
	}
	if r.Kind == "CronJob" {
		pod = model.Map(spec, "jobTemplate", "spec", "template", "spec")
	}
	if pod != nil {
		ps := pickConfiguration(pod, "serviceAccountName", "securityContext", "restartPolicy", "nodeSelector", "tolerations", "topologySpreadConstraints", "affinity")
		for _, field := range []string{"containers", "initContainers"} {
			containers := []any{}
			for _, raw := range model.Arr(pod, field) {
				c, _ := raw.(map[string]any)
				projected := pickConfiguration(c, "name", "image", "resources", "securityContext", "ports", "restartPolicy")
				for _, probe := range []string{"readinessProbe", "livenessProbe", "startupProbe"} {
					if p := model.Map(c, probe); p != nil {
						safe := pickConfiguration(p, "initialDelaySeconds", "periodSeconds", "timeoutSeconds", "successThreshold", "failureThreshold", "terminationGracePeriodSeconds")
						for _, handler := range []string{"httpGet", "tcpSocket", "grpc"} {
							if h := model.Map(p, handler); h != nil {
								safe[handler] = pickConfiguration(h, "port", "scheme")
							}
						}
						if model.Value(p, "exec") != nil {
							safe["exec"] = "command omitted"
						}
						projected[probe] = safe
					}
				}
				containers = append(containers, projected)
			}
			ps[field] = containers
		}
		config["pod"] = ps
	}
	conditions := []map[string]any{}
	for _, raw := range model.Arr(o, "status", "conditions") {
		c, _ := raw.(map[string]any)
		conditions = append(conditions, pickConfiguration(c, "type", "status", "reason", "observedGeneration"))
	}
	clean := r
	clean.Object = nil
	clean.Labels = nil
	return CapturedResource{Resource: clean, UID: model.Str(o, "metadata", "uid"), Configuration: config, Conditions: conditions}
}
func (s *Service) Capture(ctx context.Context, kind, ns, name string) (ResourceCapture, error) {
	out := ResourceCapture{SchemaVersion: 1, Cluster: s.Provider.Name(), ClusterKey: s.ClusterKey(), Mode: s.Provider.Mode(), CapturedAt: time.Now().UTC(), Resources: []CapturedResource{}, Warnings: []model.Warning{}, Limitations: []string{"Comparison covers selected configuration fields and reported conditions, not complete manifests.", "Secret and ConfigMap payloads, annotations, environment values, commands, probe paths/headers and CSI parameters are omitted.", "Descendants are current readable controller identities. Missing resources in a partial capture do not establish deletion."}}
	if e := ValidateResource(kind, ns, name); e != nil {
		return out, e
	}
	if _, e := s.Scope(ns); e != nil {
		return out, e
	}
	root, e := s.Provider.Get(ctx, kind, ns, name)
	if e != nil {
		return out, e
	}
	out.Root = model.Summarize(root).ID
	snap, e := s.Snapshot(ctx, ns)
	if e != nil {
		return out, e
	}
	out.Warnings = snap.Warnings
	objects := []model.Object{}
	for _, r := range snap.Resources {
		objects = append(objects, r.Object)
	}
	members, truncated := descendants(root, objects, 200)
	if truncated {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Snapshot", Message: "Capture limited to 200 resource identities"})
	}
	for _, o := range members {
		out.Resources = append(out.Resources, captureResource(model.Summarize(o)))
	}
	data, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	if len(data) > 2<<20 {
		return out, &provider.Error{Code: 413, Message: "Snapshot exceeds 2 MiB; select a smaller workload"}
	}
	return out, ctx.Err()
}
