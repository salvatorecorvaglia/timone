package provider

import (
	"context"
	"encoding/json"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"net/http"
	"strings"
	"testing"
	"time"
	"timone/internal/model"
)

type tableRoundTrip func(*http.Request) (*http.Response, error)

func (f tableRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCustomServerTableColumnsSchemaFormattingAndRedaction(t *testing.T) {
	k, dc := customFixture()
	k.server = "https://cluster.test"
	crd, err := dc.Resource(crdGVR).Get(context.Background(), "databases.ops.example", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	version := model.Arr(crd.Object, "spec", "versions")[0].(map[string]any)
	version["additionalPrinterColumns"] = []any{map[string]any{"name": "Ready", "type": "boolean", "jsonPath": `.status.conditions[?(@.type=="Ready")].status`}, map[string]any{"name": "Password", "type": "string", "jsonPath": ".spec.password", "priority": int64(1)}, map[string]any{"name": "Age", "type": "date", "jsonPath": ".metadata.creationTimestamp"}}
	version["schema"] = map[string]any{"openAPIV3Schema": map[string]any{"type": "object", "properties": map[string]any{"spec": map[string]any{"type": "object", "properties": map[string]any{"password": map[string]any{"type": "string", "format": "password", "default": "HIDDEN-SCHEMA"}}}}}}
	if _, err = dc.Resource(crdGVR).Update(context.Background(), crd, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	raw := map[string]any{"apiVersion": "ops.example/v1", "kind": "Database", "metadata": map[string]any{"name": "orders", "namespace": "production", "uid": "one"}, "spec": map[string]any{"password": "HIDDEN-PAYLOAD"}}
	bytes, _ := json.Marshal(raw)
	table := metav1.Table{TypeMeta: metav1.TypeMeta{APIVersion: "meta.k8s.io/v1", Kind: "Table"}, ColumnDefinitions: []metav1.TableColumnDefinition{{Name: "Name", Type: "string"}, {Name: "Ready", Type: "boolean"}, {Name: "Password", Type: "string", Priority: 1}, {Name: "Age", Type: "date"}}, Rows: []metav1.TableRow{{Cells: []any{"orders", true, "HIDDEN-CELL", time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)}, Object: runtime.RawExtension{Raw: bytes}}}}
	k.client = &http.Client{Transport: tableRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/apis/ops.example/v1/namespaces/production/databases" || r.URL.Query().Get("includeObject") != "Object" || !strings.Contains(r.Header.Get("Accept"), "as=Table") {
			t.Fatal(r.URL, r.Header)
		}
		b, _ := json.Marshal(table)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
	})}
	objects, typ, truncated, err := k.CustomTable(context.Background(), "databases.ops.example", "v1", "production")
	if err != nil || truncated || len(objects) != 1 {
		t.Fatal(objects, err)
	}
	r := model.SummarizeCustom(objects[0], typ.Definition)
	if len(r.PrinterCells) != 4 || r.PrinterCells[1] != "true" || r.PrinterCells[2] != "[REDACTED]" || r.PrinterCells[3] != "120m" {
		t.Fatal(r.PrinterCells)
	}
	b, _ := json.Marshal(struct {
		Resource model.Resource
		Type     model.CustomType
	}{r, typ})
	if strings.Contains(string(b), "HIDDEN-") {
		t.Fatal("sensitive content leaked", string(b))
	}
	table.Rows[0].Object.Raw = []byte(`{"apiVersion":"ops.example/v1","kind":"Database","metadata":{"name":"other","namespace":"staging"}}`)
	if _, _, _, err = k.CustomTable(context.Background(), "databases.ops.example", "v1", "production"); Status(err) != 502 {
		t.Fatal("cross namespace table row accepted", err)
	}
}
