package provider

import (
	"context"
	"errors"
	"timone/internal/model"
)

type Kind struct {
	Name       string `json:"name"`
	Group      string `json:"group"`
	Version    string `json:"version"`
	Plural     string `json:"plural"`
	Namespaced bool   `json:"namespaced"`
}

var Kinds = []Kind{
	{"Node", "", "v1", "nodes", false}, {"Namespace", "", "v1", "namespaces", false}, {"Pod", "", "v1", "pods", true},
	{"Deployment", "apps", "v1", "deployments", true}, {"ReplicaSet", "apps", "v1", "replicasets", true}, {"StatefulSet", "apps", "v1", "statefulsets", true}, {"DaemonSet", "apps", "v1", "daemonsets", true},
	{"Job", "batch", "v1", "jobs", true}, {"CronJob", "batch", "v1", "cronjobs", true}, {"Service", "", "v1", "services", true}, {"Ingress", "networking.k8s.io", "v1", "ingresses", true}, {"ConfigMap", "", "v1", "configmaps", true}, {"Secret", "", "v1", "secrets", true},
	{"PersistentVolume", "", "v1", "persistentvolumes", false}, {"PersistentVolumeClaim", "", "v1", "persistentvolumeclaims", true}, {"ServiceAccount", "", "v1", "serviceaccounts", true},
	{"Role", "rbac.authorization.k8s.io", "v1", "roles", true}, {"RoleBinding", "rbac.authorization.k8s.io", "v1", "rolebindings", true}, {"ClusterRole", "rbac.authorization.k8s.io", "v1", "clusterroles", false}, {"ClusterRoleBinding", "rbac.authorization.k8s.io", "v1", "clusterrolebindings", false},
	{"CustomResourceDefinition", "apiextensions.k8s.io", "v1", "customresourcedefinitions", false}, {"NetworkPolicy", "networking.k8s.io", "v1", "networkpolicies", true},
	{"Application", "argoproj.io", "v1alpha1", "applications", true}, {"Kustomization", "kustomize.toolkit.fluxcd.io", "v1", "kustomizations", true}, {"HelmRelease", "helm.toolkit.fluxcd.io", "v2", "helmreleases", true}, {"GatewayClass", "gateway.networking.k8s.io", "v1", "gatewayclasses", false}, {"ReferenceGrant", "gateway.networking.k8s.io", "v1beta1", "referencegrants", true}, {"Gateway", "gateway.networking.k8s.io", "v1", "gateways", true}, {"HTTPRoute", "gateway.networking.k8s.io", "v1", "httproutes", true},
}

func FindKind(name string) (Kind, bool) {
	for _, k := range Kinds {
		if k.Name == name {
			return k, true
		}
	}
	return Kind{}, false
}

type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string { return e.Message }

var ErrNotFound = &Error{404, "Resource not found"}

func Status(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 503
}

type Provider interface {
	Name() string
	Mode() string
	Namespace() string
	Connection(context.Context) model.Connection
	List(context.Context, string, string) ([]model.Object, bool, error)
	Get(context.Context, string, string, string) (model.Object, error)
	Events(context.Context, string) ([]model.Event, error)
	Metrics(context.Context, string) (model.Metrics, error)
	Logs(context.Context, string, string, string, int) (string, error)
	HelmDetailsEnabled() bool
	HelmHistory(context.Context, string, string) (model.HelmHistory, error)
	Watch(context.Context, func())
}

// ScopedWatcher is optional; legacy providers still trigger global invalidation.
type ScopedWatcher interface {
	WatchScoped(context.Context, func(model.ResourceChange))
}
