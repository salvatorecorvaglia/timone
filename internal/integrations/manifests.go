package integrations

import (
	"context"
	"fmt"
	"io"
	yaml "k8s.io/apimachinery/pkg/util/yaml"
	"strings"
	"timone/internal/model"
)

// Release manifests follow at least the generic detail redaction policy, with
// stronger omission of configuration payloads, command arguments and headers.
func helmConfiguration(ctx context.Context, manifest string) ([]model.HelmManifest, error) {
	out := []model.HelmManifest{}
	d := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var o model.Object
		err := d.Decode(&o)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("Configuration unavailable")
		}
		if len(o) == 0 {
			continue
		}
		r := model.HelmResource{APIVersion: model.Str(o, "apiVersion"), Kind: model.Str(o, "kind"), Name: model.Str(o, "metadata", "name"), Namespace: model.Str(o, "metadata", "namespace")}
		clean := model.Sanitize(o)
		// Unrecognized custom APIs can store credentials under arbitrary fields.
		known := map[string]string{"Pod": "v1", "Deployment": "apps/v1", "StatefulSet": "apps/v1", "DaemonSet": "apps/v1", "ReplicaSet": "apps/v1", "Job": "batch/v1", "CronJob": "batch/v1", "Service": "v1", "ConfigMap": "v1", "Secret": "v1", "ServiceAccount": "v1", "Ingress": "networking.k8s.io/v1", "PersistentVolumeClaim": "v1", "NetworkPolicy": "networking.k8s.io/v1", "Role": "rbac.authorization.k8s.io/v1", "RoleBinding": "rbac.authorization.k8s.io/v1", "ClusterRole": "rbac.authorization.k8s.io/v1", "ClusterRoleBinding": "rbac.authorization.k8s.io/v1"}
		complete := known[r.Kind] == r.APIVersion
		if !complete {
			clean = model.CustomProjection(o)
		}
		delete(clean, "status")
		var scrub func(any, int) error
		scrub = func(v any, depth int) error {
			if depth > 32 {
				return fmt.Errorf("Configuration exceeds nesting limit")
			}
			switch n := v.(type) {
			case map[string]any:
				for key, value := range n {
					lower := strings.ToLower(key)
					if key == "annotations" || key == "managedFields" || key == "data" || key == "binaryData" || key == "stringData" || key == "command" || key == "args" || key == "httpHeaders" || strings.Contains(lower, "password") || strings.Contains(lower, "token") || strings.Contains(lower, "credential") || strings.Contains(lower, "privatekey") || strings.Contains(lower, "clientsecret") {
						n[key] = "[REDACTED]"
						continue
					}
					if err := scrub(value, depth+1); err != nil {
						return err
					}
				}
			case []any:
				for _, value := range n {
					if err := scrub(value, depth+1); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if err := scrub(map[string]any(clean), 0); err != nil {
			return nil, err
		}
		out = append(out, model.HelmManifest{Resource: r, Object: clean, Supported: complete})
		if len(out) > MaxInventory {
			return nil, fmt.Errorf("Configuration exceeds resource limit")
		}
	}
	return out, nil
}
