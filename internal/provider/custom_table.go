package provider

import (
	"context"
	"encoding/json"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"net/url"
	"time"
	"timone/internal/model"
)

type CustomTableProvider interface {
	CustomTable(context.Context, string, string, string) ([]model.Object, model.CustomType, bool, error)
}

func (k *cluster) CustomTable(ctx context.Context, definition, version, ns string) ([]model.Object, model.CustomType, bool, error) {
	t, v, err := k.CustomType(ctx, definition, version)
	if err != nil {
		return nil, t, false, err
	}
	spec := customSpec(t, v)
	ns, err = k.scope(spec, ns)
	if err != nil {
		return nil, t, false, err
	}
	path := "/apis/" + url.PathEscape(t.Group) + "/" + url.PathEscape(v)
	if ns != "" {
		path += "/namespaces/" + url.PathEscape(ns)
	}
	path += "/" + url.PathEscape(t.Plural) + "?includeObject=Object&limit=5000"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.server+path, nil)
	if err != nil {
		return nil, t, false, safeError(err)
	}
	req.Header.Set("Accept", "application/json;as=Table;g=meta.k8s.io;v=v1,application/json")
	res, err := k.client.Do(req)
	if err != nil {
		return nil, t, false, safeError(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		if res.StatusCode == 406 {
			return k.CustomList(ctx, definition, version, ns)
		}
		return nil, t, false, &Error{Code: res.StatusCode, Message: "Kubernetes custom table unavailable"}
	}
	var table metav1.Table
	data, err := io.ReadAll(io.LimitReader(res.Body, (16<<20)+1))
	if err != nil || len(data) > 16<<20 {
		return nil, t, false, &Error{413, "Custom table exceeds response limit"}
	}
	if err := consumeReadBudget(ctx, len(data)); err != nil {
		return nil, t, false, err
	}
	if json.Unmarshal(data, &table) != nil {
		return nil, t, false, &Error{502, "Invalid Kubernetes custom table"}
	}
	if table.Kind != "Table" {
		return k.CustomList(ctx, definition, version, ns)
	}
	if len(table.Rows) > 5000 || len(table.ColumnDefinitions) > 65 {
		return nil, t, false, &Error{413, "Custom table exceeds row/column limit"}
	}
	presentation := t.Presentations[v]
	presentation.Columns = []model.PrinterColumn{}
	for i, c := range table.ColumnDefinitions {
		path := "__unverified__"
		if i == 0 && c.Name == "Name" {
			path = ".metadata.name"
		} else if c.Name == "Age" && len(t.Presentations[v].Columns) == 0 {
			path = ".metadata.creationTimestamp"
		} else if i > 0 && i-1 < len(t.Presentations[v].Columns) {
			expected := t.Presentations[v].Columns[i-1]
			if expected.Name == c.Name && expected.Type == c.Type {
				path = expected.JSONPath
			}
		}

		presentation.Columns = append(presentation.Columns, model.PrinterColumn{Name: c.Name, Type: c.Type, Format: c.Format, Description: c.Description, Priority: int(c.Priority), JSONPath: path})
	}
	t.Presentations[v] = presentation
	out := []model.Object{}
	seen := map[string]bool{}
	for _, row := range table.Rows {
		var raw model.Object
		if json.Unmarshal(row.Object.Raw, &raw) != nil || model.Str(raw, "kind") != t.Kind || model.Str(raw, "apiVersion") != t.Group+"/"+v || (!t.Namespaced && model.Str(raw, "metadata", "namespace") != "") || (ns != "" && model.Str(raw, "metadata", "namespace") != ns) || len(row.Cells) != len(presentation.Columns) {
			return nil, t, false, &Error{502, "Invalid custom table object identity or cells"}
		}
		id := model.ID(t.Kind, model.Str(raw, "metadata", "namespace"), model.Str(raw, "metadata", "name"))
		if seen[id] || model.Str(raw, "metadata", "name") == "" {
			return nil, t, false, &Error{502, "Duplicate or missing custom table identity"}
		}
		seen[id] = true
		clean := model.CustomProjection(raw)
		cells := []string{}
		for i, value := range row.Cells {
			column := presentation.Columns[i]
			if !model.SafePrinterPath(column.JSONPath) || model.SensitivePath(column.JSONPath) || model.SensitivePath(column.Name) {
				cells = append(cells, "[REDACTED]")
			} else {
				cells = append(cells, model.PrinterText(value, column.Type, time.Now().UTC()))
			}
		}
		clean["__timonePrinterCells"] = cells
		out = append(out, clean)
	}
	return out, t, table.Continue != "", ctx.Err()
}
