package provider

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
	"timone/internal/integrations"
	"timone/internal/model"
)

func (d *demo) HelmDetailsEnabled() bool { return true }
func (d *demo) HelmHistory(ctx context.Context, ns, name string) (model.HelmHistory, error) {
	out := model.HelmHistory{Name: name, Namespace: ns, Mode: "demo", Revisions: []model.HelmRevision{}, Warnings: []model.Warning{}, Retention: "Simulated retained Helm revisions. Raw values, notes, templates and raw manifests are never returned; comparisons expose only sanitized values differences."}
	if err := ValidateHelmReference(ns, name); err != nil {
		return out, err
	}
	for i := len(d.objects) - 1; i >= 0; i-- {
		o := d.objects[i]
		if model.Str(o, "kind") != "Secret" || model.Str(o, "metadata", "namespace") != ns || model.Str(o, "metadata", "labels", "owner") != "helm" || model.Str(o, "metadata", "labels", "name") != name {
			continue
		}
		rev, _ := strconv.Atoi(model.Str(o, "metadata", "labels", "version"))
		summary, _, err := integrations.DecodeHelm(ctx, model.Str(o, "data", "release"), name, ns, rev)
		if err != nil {
			return out, err
		}
		out.Revisions = append(out.Revisions, summary)
	}
	if len(out.Revisions) == 0 {
		return out, ErrNotFound
	}
	return out, ctx.Err()
}
func (d *demo) seedHelm() {
	for _, name := range []string{"storefront", "checkout-api", "prometheus"} {
		ns := "production"
		if name == "prometheus" {
			ns = "platform"
		}
		for rev := 1; rev <= 3; rev++ {
			version := fmt.Sprintf("2.%d.0", rev)
			status := "superseded"
			if rev == 3 {
				status = "deployed"
			}
			manifest := fmt.Sprintf("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: %s\n  namespace: %s\n", name, ns)
			manifest += fmt.Sprintf("spec:\n  replicas: %d\n  template:\n    spec:\n      containers:\n      - name: app\n        image: example.invalid/%s:v%d\n        env:\n        - name: PASSWORD\n          value: DEMO-PRIVATE-HELM-VALUE\n", rev, name, rev)
			if rev >= 2 {
				manifest += fmt.Sprintf("---\napiVersion: v1\nkind: Service\nmetadata:\n  name: %s\n  namespace: %s\n", name, ns)
			}
			payload := map[string]any{"name": name, "namespace": ns, "version": rev, "info": map[string]any{"status": status, "last_deployed": d.started.Add(-time.Duration(4-rev) * time.Hour).Format(time.RFC3339)}, "chart": map[string]any{"metadata": map[string]any{"name": name, "version": version, "appVersion": fmt.Sprintf("v2.%d.0", rev)}}, "config": map[string]any{"replicaCount": rev, "image": map[string]any{"tag": fmt.Sprintf("v2.%d.0", rev)}, "database": map[string]any{"password": "DEMO-PRIVATE-VALUES-PASSWORD"}, "apiToken": "DEMO-PRIVATE-VALUES-TOKEN"}, "manifest": manifest}
			raw, _ := json.Marshal(payload)
			var compressed bytes.Buffer
			w := gzip.NewWriter(&compressed)
			_, _ = w.Write(raw)
			_ = w.Close()
			data := base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(compressed.Bytes())))
			o := d.add("Secret", ns, fmt.Sprintf("sh.helm.release.v1.%s.v%d", name, rev), nil, nil, map[string]any{"owner": "helm", "name": name, "version": fmt.Sprint(rev), "status": status})
			o["type"] = "helm.sh/release.v1"
			o["data"] = map[string]any{"release": data}
		}
	}
}
