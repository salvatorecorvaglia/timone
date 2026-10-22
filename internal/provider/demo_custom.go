package provider

import (
	"context"
	"k8s.io/client-go/util/jsonpath"
	"time"
	"timone/internal/model"
)

var demoCustomTypes = []model.CustomType{
	{Definition: "databases.platform.timone.io", Kind: "Database", Group: "platform.timone.io", Plural: "databases", Namespaced: true, Versions: []string{"v1", "v1beta1"}, PreferredVersion: "v1"},
	{Definition: "clusterprofiles.platform.timone.io", Kind: "ClusterProfile", Group: "platform.timone.io", Plural: "clusterprofiles", Versions: []string{"v1alpha1"}, PreferredVersion: "v1alpha1"},
}

func (d *demo) seedCustom() {
	for _, t := range demoCustomTypes {
		scope := "Cluster"
		if t.Namespaced {
			scope = "Namespaced"
		}
		versions := []any{}
		for _, v := range t.Versions {
			versions = append(versions, map[string]any{"name": v, "served": true, "storage": v == t.PreferredVersion, "additionalPrinterColumns": []any{map[string]any{"name": "Ready", "type": "string", "jsonPath": `.status.conditions[?(@.type=="Ready")].status`}, map[string]any{"name": "Age", "type": "date", "jsonPath": ".metadata.creationTimestamp"}, map[string]any{"name": "Generation", "type": "integer", "jsonPath": ".metadata.generation", "priority": 1}}, "schema": map[string]any{"openAPIV3Schema": map[string]any{"type": "object", "properties": map[string]any{"spec": map[string]any{"type": "object", "properties": map[string]any{"replicas": map[string]any{"type": "integer", "minimum": 1}, "password": map[string]any{"type": "string", "format": "password", "default": "SIMULATED-OMITTED-CUSTOM-SECRET"}}}, "status": map[string]any{"type": "object", "x-kubernetes-preserve-unknown-fields": true}}}}})
		}
		d.add("CustomResourceDefinition", "", t.Definition, map[string]any{"group": t.Group, "names": map[string]any{"kind": t.Kind, "plural": t.Plural}, "scope": scope, "versions": versions}, map[string]any{"conditions": []any{map[string]any{"type": "Established", "status": "True"}}}, nil)
	}
	for _, row := range []struct {
		ns, name, ready, reason, message string
		observed                         int
	}{
		{"production", "orders-db", "False", "BackupStorageUnavailable", "Simulated controller report: backup destination could not be reached.", 2},
		{"production", "payments-db", "True", "Reconciled", "Simulated database reports ready.", 2},
		{"staging", "orders-db", "True", "Reconciled", "Previous generation was ready; current generation is not yet observed.", 1},
	} {
		o := d.add("Database", row.ns, row.name, map[string]any{"password": "SIMULATED-OMITTED-CUSTOM-SECRET"}, map[string]any{"credentials": "SIMULATED-OMITTED-CUSTOM-SECRET", "observedGeneration": row.observed, "conditions": []any{map[string]any{"type": "Ready", "status": row.ready, "observedGeneration": row.observed, "reason": row.reason, "message": row.message, "lastTransitionTime": d.started.Add(-8 * time.Minute).Format(time.RFC3339)}}}, map[string]any{"app.kubernetes.io/part-of": "commerce"})
		o["apiVersion"] = "platform.timone.io/v1"
		model.Map(o, "metadata")["generation"] = 2
		if row.ns == "production" && row.name == "orders-db" {
			d.events = append(d.events, model.Event{ID: "custom-orders-backup", CustomDefinition: "databases.platform.timone.io", ResourceID: model.ID("Database", row.ns, row.name), Namespace: row.ns, ResourceNamespace: row.ns, Resource: row.name, ResourceUID: model.Str(o, "metadata", "uid"), Kind: "Database", Type: "Warning", Reason: "BackupStorageUnavailable", Message: row.message, Count: 4, Timestamp: d.started.Add(-2 * time.Minute).Format(time.RFC3339)})
		}
	}
	o := d.add("ClusterProfile", "", "production-baseline", nil, nil, map[string]any{"environment": "production"})
	o["apiVersion"] = "platform.timone.io/v1alpha1"
}
func (d *demo) CustomTypes(ctx context.Context) (model.CustomCatalog, error) {
	items := make([]model.CustomType, 0, len(demoCustomTypes))
	for _, t := range demoCustomTypes {
		t, _, _ = d.CustomType(ctx, t.Definition, "")
		t.Versions = append([]string{}, t.Versions...)
		items = append(items, t)
	}
	return model.CustomCatalog{Items: items, Warnings: []model.Warning{}, Enabled: true, Mode: "demo", Message: "Simulated operator resources. Specs, annotations and arbitrary status fields are omitted."}, ctx.Err()
}
func (d *demo) CustomType(ctx context.Context, definition, version string) (model.CustomType, string, error) {
	if err := ctx.Err(); err != nil {
		return model.CustomType{}, "", err
	}
	for _, t := range demoCustomTypes {
		if t.Definition == definition {
			for _, o := range d.objects {
				if model.Str(o, "kind") == "CustomResourceDefinition" && model.Str(o, "metadata", "name") == definition {
					parsed, e := parseCustomType(o)
					if e != nil {
						return t, "", e
					}
					t = parsed
					break
				}
			}
			v, err := selectCustomVersion(t, version)
			return t, v, err
		}
	}
	return model.CustomType{}, "", ErrNotFound
}
func (d *demo) CustomList(ctx context.Context, definition, version, ns string) ([]model.Object, model.CustomType, bool, error) {
	t, v, err := d.CustomType(ctx, definition, version)
	if err != nil {
		return nil, t, false, err
	}
	items := []model.Object{}
	for _, o := range d.objects {
		if model.Str(o, "kind") == t.Kind && (!t.Namespaced || ns == "" || model.Str(o, "metadata", "namespace") == ns) && model.Str(o, "apiVersion") == t.Group+"/"+t.PreferredVersion {
			clean := model.CustomProjection(o)
			clean["apiVersion"] = t.Group + "/" + v
			items = append(items, clean)
		}
	}
	return items, t, false, nil
}
func (d *demo) CustomGet(ctx context.Context, definition, version, ns, name string) (model.Object, model.CustomType, error) {
	items, t, _, err := d.CustomList(ctx, definition, version, ns)
	if err != nil {
		return nil, t, err
	}
	if err := validateCustomObjectScope(t, ns, name); err != nil {
		return nil, t, err
	}
	for _, o := range items {
		if model.Str(o, "metadata", "name") == name && model.Str(o, "metadata", "namespace") == ns {
			return o, t, nil
		}
	}
	return nil, t, ErrNotFound
}

func (d *demo) CustomTable(ctx context.Context, definition, version, ns string) ([]model.Object, model.CustomType, bool, error) {
	objects, t, truncated, err := d.CustomList(ctx, definition, version, ns)
	if err != nil {
		return nil, t, truncated, err
	}
	if version == "" {
		version = t.PreferredVersion
	}
	presentation := t.Presentations[version]
	columns := append([]model.PrinterColumn{{Name: "Name", Type: "string", JSONPath: ".metadata.name"}}, presentation.Columns...)
	for _, clean := range objects {
		var raw model.Object
		for _, candidate := range d.objects {
			if model.Str(candidate, "kind") == t.Kind && model.Str(candidate, "metadata", "uid") == model.Str(clean, "metadata", "uid") {
				raw = candidate
				break
			}
		}
		cells := []string{}
		for _, column := range columns {
			cell := "<none>"
			parser := jsonpath.New("demo").AllowMissingKeys(true)
			if parser.Parse("{"+column.JSONPath+"}") == nil {
				values, e := parser.FindResults(map[string]any(raw))
				if e == nil && len(values) > 0 && len(values[0]) > 0 {
					cell = model.PrinterText(values[0][0].Interface(), column.Type, time.Now().UTC())
				}
			}
			if model.SensitivePath(column.JSONPath) || model.SensitivePath(column.Name) {
				cell = "[REDACTED]"
			}
			cells = append(cells, cell)
		}
		clean["__timonePrinterCells"] = cells
	}
	presentation.Columns = columns
	t.Presentations[version] = presentation
	return objects, t, truncated, nil
}
