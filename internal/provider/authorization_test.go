package provider

import (
	"context"
	"fmt"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
	"net/http"
	"testing"
	"timone/internal/model"
)

func TestLiveAuthorizationReviewUsesRealSelfAndServiceAccountSubjects(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status map[string]any
		want   string
	}{{"allowed", map[string]any{"allowed": true}, "allowed"}, {"denied", map[string]any{"allowed": false}, "denied"}, {"error", map[string]any{"allowed": true, "evaluationError": "sensitive-details"}, "unknown"}, {"ambiguous", map[string]any{"allowed": true, "denied": true}, "unknown"}, {"missing", map[string]any{}, "unknown"}} {
		t.Run(tc.name, func(t *testing.T) {
			dc := fake.NewSimpleDynamicClient(runtime.NewScheme())
			k := newCluster("fake", "production", &http.Client{}, dc)
			dc.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
				body := action.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
				if model.Value(body.Object, "spec", "user") != nil || model.Value(body.Object, "spec", "groups") != nil || model.Str(body.Object, "spec", "resourceAttributes", "resource") != "pods" {
					t.Fatal(body)
				}
				return true, &unstructured.Unstructured{Object: map[string]any{"status": tc.status}}, nil
			})
			out, err := k.ReviewAccess(context.Background(), model.AccessQuery{Kind: "Pod", Namespace: "production", Verb: "get"})
			if err != nil || out.State != tc.want {
				t.Fatal(out, err)
			}
		})
	}
	dc := fake.NewSimpleDynamicClient(runtime.NewScheme(), fakeObject("ServiceAccount", "production", "viewer", "one"))
	k := newCluster("fake", "production", &http.Client{}, dc)
	dc.PrependReactor("create", "subjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		body := action.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
		if model.Str(body.Object, "spec", "user") != "system:serviceaccount:production:viewer" || len(model.Arr(body.Object, "spec", "groups")) != 3 {
			t.Fatal(body)
		}
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "authorization.k8s.io", Resource: "subjectaccessreviews"}, "", fmt.Errorf("sensitive-details"))
	})
	out, err := k.ReviewAccess(context.Background(), model.AccessQuery{Kind: "Pod", Namespace: "production", Verb: "get", ServiceAccount: "viewer", ServiceAccountNamespace: "production"})
	if Status(err) != 403 || out.State != "unknown" {
		t.Fatal(out, err)
	}
}
