package provider

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"time"
	"timone/internal/model"
)

type AuthorizationProvider interface {
	ReviewAccess(context.Context, model.AccessQuery) (model.AccessReview, error)
}

func (k *cluster) ReviewAccess(ctx context.Context, q model.AccessQuery) (model.AccessReview, error) {
	out := model.AccessReview{Query: q, State: "unknown", Identity: "Active Kubernetes credential", Mode: k.Mode(), ObservedAt: time.Now().UTC().Format(time.RFC3339), Message: "A point-in-time authorizer decision, not admission or an authorization grant."}
	spec, _ := FindKind(q.Kind)
	attrs := map[string]any{"namespace": q.Namespace, "verb": q.Verb, "group": spec.Group, "version": spec.Version, "resource": spec.Plural, "subresource": q.Subresource, "name": q.Name}
	kind, plural := "SelfSubjectAccessReview", "selfsubjectaccessreviews"
	body := map[string]any{"resourceAttributes": attrs}
	if q.ServiceAccount != "" {
		account, err := k.Get(ctx, "ServiceAccount", q.ServiceAccountNamespace, q.ServiceAccount)
		if err != nil {
			return out, err
		}
		kind, plural = "SubjectAccessReview", "subjectaccessreviews"
		body["uid"] = model.Str(account, "metadata", "uid")
		out.Message += " ServiceAccount reviews use its current UID and standard groups; credential-specific bound-token extras are not reconstructed."
		body["user"] = "system:serviceaccount:" + q.ServiceAccountNamespace + ":" + q.ServiceAccount
		body["groups"] = []any{"system:serviceaccounts", "system:serviceaccounts:" + q.ServiceAccountNamespace, "system:authenticated"}
		out.Identity = body["user"].(string)
	}
	review := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "authorization.k8s.io/v1", "kind": kind, "spec": body}}
	result, err := k.dynamic.Resource(schema.GroupVersionResource{Group: "authorization.k8s.io", Version: "v1", Resource: plural}).Create(quiet(ctx), review, metav1.CreateOptions{})
	if err != nil {
		return out, safeError(err)
	}
	status := model.Map(result.Object, "status")
	allowed, aok := status["allowed"].(bool)
	denied, dok := status["denied"].(bool)
	if model.Str(status, "evaluationError") != "" {
		out.Message = "Kubernetes authorizer reported an evaluation error; no effective decision established."
		return out, nil
	}
	if aok && allowed && !denied {
		out.State = "allowed"
	} else if (dok && denied && !allowed) || (aok && !allowed && !denied) {
		out.State = "denied"
	}
	return out, nil
}
func (d *demo) ReviewAccess(ctx context.Context, q model.AccessQuery) (model.AccessReview, error) {
	state := "allowed"
	if q.Verb != "get" && q.Verb != "list" && q.Verb != "watch" || q.Kind == "Secret" {
		state = "denied"
	}
	identity := "Simulated active Kubernetes identity"
	if q.ServiceAccount != "" {
		identity = "Simulated ServiceAccount " + q.ServiceAccountNamespace + "/" + q.ServiceAccount
	}
	return model.AccessReview{Query: q, State: state, Identity: identity, Mode: "demo", ObservedAt: time.Now().UTC().Format(time.RFC3339), Message: "SIMULATED authorization decision; no Kubernetes review was sent."}, ctx.Err()
}
