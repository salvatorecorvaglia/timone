package provider

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"timone/internal/model"
)

// FreshPresenceProvider disambiguates a missing named object from an unavailable
// API without reusing an informer/permission-lease collection cache.
type FreshPresenceProvider interface {
	ConfirmMissing(context.Context, string, string, string) (bool, error)
}

func (k *cluster) ConfirmMissing(ctx context.Context, kind, ns, name string) (bool, error) {
	spec, ok := FindKind(kind)
	if !ok || name == "" {
		return false, &Error{400, "Invalid presence-check resource"}
	}
	ns, err := k.scope(spec, ns)
	if err != nil {
		return false, err
	}
	spec, err = k.mapping(ctx, spec)
	if err != nil {
		return false, err
	}
	list, err := k.resource(spec, ns).List(quiet(ctx), metav1.ListOptions{Limit: 1, FieldSelector: fields.OneTermEqualSelector("metadata.name", name).String()})
	if err != nil {
		return false, safeError(err)
	}
	if list.GetContinue() != "" || len(list.Items) > 1 {
		return false, &Error{502, "Presence check returned incomplete or excessive results"}
	}
	if len(list.Items) == 0 {
		return true, nil
	}
	if model.Str(list.Items[0].Object, "metadata", "name") != name {
		return false, &Error{502, "Presence check response did not respect the name selector"}
	}
	return false, nil
}
