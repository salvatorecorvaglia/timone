package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
	"timone/internal/model"
)

var customGVR = schema.GroupVersionResource{Group: "ops.example", Version: "v1", Resource: "databases"}
var crdGVR = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}

func customFixture() (*cluster, *fake.FakeDynamicClient) {
	crd := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition", "metadata": map[string]any{"name": "databases.ops.example"}, "spec": map[string]any{"group": "ops.example", "scope": "Namespaced", "names": map[string]any{"plural": "databases", "kind": "Database"}, "versions": []any{map[string]any{"name": "v1", "served": true, "storage": true}, map[string]any{"name": "v1alpha1", "served": false}}}, "status": map[string]any{"conditions": []any{map[string]any{"type": "Established", "status": "True"}}}}}
	o := fakeObject("Database", "production", "orders", "1")
	o.SetAPIVersion("ops.example/v1")
	o.Object["spec"] = map[string]any{"password": "private-marker"}
	o.Object["status"] = map[string]any{"credentials": "private-marker"}
	dc := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{crdGVR: "CustomResourceDefinitionList", customGVR: "DatabaseList"}, crd, o)
	k := newCluster("custom-fake", "production", &http.Client{}, dc)
	k.customResources = []string{"databases.ops.example"}
	return k, dc
}
func TestCustomReadsRevalidateCRDAndDoNotCacheRawPayloads(t *testing.T) {
	k, dc := customFixture()
	ctx := context.Background()
	items, typ, truncated, err := k.CustomList(ctx, "databases.ops.example", "", "production")
	if err != nil || len(items) != 1 || truncated || typ.Kind != "Database" {
		t.Fatal(items, typ, err)
	}
	b, _ := json.Marshal(items)
	if strings.Contains(string(b), "private-marker") || len(k.cache) != 0 || len(k.streams) != 0 {
		t.Fatal("custom data entered a cache or response")
	}
	if _, _, err := k.CustomGet(ctx, "databases.ops.example", "v1", "production", "orders"); err != nil {
		t.Fatal(err)
	}
	if err := dc.Resource(crdGVR).Delete(ctx, "databases.ops.example", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := k.CustomList(ctx, "databases.ops.example", "", "production"); Status(err) != 404 {
		t.Fatal("stale CRD authorized instance read", err)
	}
}
func TestCustomProviderRejectsDisabledUnservedAndCrossNamespaceReads(t *testing.T) {
	k, dc := customFixture()
	ctx := context.Background()
	if _, _, _, err := k.CustomList(ctx, "pods.ops.example", "", ""); Status(err) != 403 || len(dc.Actions()) != 0 {
		t.Fatal("disabled type reached Kubernetes", err)
	}
	if _, _, _, err := k.CustomList(ctx, "databases.ops.example", "v1alpha1", ""); Status(err) != 400 {
		t.Fatal("unserved version accepted", err)
	}
	if _, _, _, err := k.CustomList(ctx, "databases.ops.example", "", "staging"); Status(err) != 403 {
		t.Fatal("namespace ceiling bypass", err)
	}
	if _, _, err := k.CustomGet(ctx, "databases.ops.example", "", "", "orders"); Status(err) != 400 {
		t.Fatal("namespaced detail missing namespace", err)
	}
	for _, a := range dc.Actions() {
		if a.GetResource() == customGVR {
			t.Fatal("invalid read reached custom collection", a)
		}
	}
}
func TestCustomCatalogPreservesPartialAvailabilityAndPermissionFailures(t *testing.T) {
	k, dc := customFixture()
	k.customResources = append(k.customResources, "missing.ops.example")
	catalog, err := k.CustomTypes(context.Background())
	if err != nil || len(catalog.Items) != 1 || len(catalog.Warnings) != 1 {
		t.Fatal(catalog, err)
	}
	dc.PrependReactor("get", "customresourcedefinitions", func(a ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(crdGVR.GroupResource(), "test", fmt.Errorf("private-marker"))
	})
	catalog, err = k.CustomTypes(context.Background())
	if err != nil || len(catalog.Items) != 0 || len(catalog.Warnings) != 2 {
		t.Fatal(catalog, err)
	}
	b, _ := json.Marshal(catalog)
	if strings.Contains(string(b), "private-marker") {
		t.Fatal("upstream error payload leaked")
	}
	if _, _, err := k.CustomGet(context.Background(), "databases.ops.example", "", "production", "orders"); Status(err) != 403 {
		t.Fatal("revoked CRD permission bypassed", err)
	}
	k.customResources = nil
	before := len(dc.Actions())
	catalog, err = k.CustomTypes(context.Background())
	if err != nil || catalog.Enabled || len(dc.Actions()) != before {
		t.Fatal("disabled catalog queried Kubernetes")
	}
}
func TestCustomTypeRejectsMalformedScopeIdentityAndConditions(t *testing.T) {
	d := NewDemo().(*demo)
	for _, o := range d.objects {
		if model.Str(o, "kind") != "CustomResourceDefinition" {
			continue
		}
		for _, change := range []func(model.Object){func(o model.Object) { model.Map(o, "spec")["group"] = "../core" }, func(o model.Object) { model.Map(o, "spec")["scope"] = "Unknown" }, func(o model.Object) { o["status"] = map[string]any{} }, func(o model.Object) {
			model.Map(o, "spec")["versions"] = []any{map[string]any{"name": "v1/../secrets", "served": true}}
		}} {
			copy := model.Sanitize(o)
			change(copy)
			if _, err := parseCustomType(copy); err == nil {
				t.Fatal("unsafe definition accepted", copy)
			}
		}
	}
}
func TestCustomCollectionBoundAndInstanceErrors(t *testing.T) {
	k, dc := customFixture()
	ctx := context.Background()
	dc.PrependReactor("list", "databases", func(a ktesting.Action) (bool, runtime.Object, error) {
		list := &unstructured.UnstructuredList{}
		for i := 0; i < collectionLimit+1; i++ {
			o := fakeObject("Database", "production", fmt.Sprintf("db-%04d", i), "1")
			o.SetAPIVersion("ops.example/v1")
			list.Items = append(list.Items, *o)
		}
		return true, list, nil
	})
	items, _, truncated, err := k.CustomList(ctx, "databases.ops.example", "", "")
	if err != nil || !truncated || len(items) != collectionLimit {
		t.Fatal(len(items), truncated, err)
	}
	dc.PrependReactor("list", "databases", func(a ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(customGVR.GroupResource(), "", fmt.Errorf("private-marker"))
	})
	if _, _, _, err := k.CustomList(ctx, "databases.ops.example", "", ""); Status(err) != 403 || strings.Contains(err.Error(), "private-marker") {
		t.Fatal(err)
	}
}
