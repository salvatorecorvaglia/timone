package provider

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"regexp"
	"sort"
	"timone/internal/integrations"
	"timone/internal/model"
)

var helmName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

func ValidateHelmReference(ns, name string) error {
	if ns == "" || !validNamespace(ns) || len(name) > 53 || !helmName.MatchString(name) {
		return &Error{Code: 400, Message: "Helm requires a valid namespace and release name (up to 53 DNS subdomain characters)"}
	}
	return nil
}
func (k *cluster) HelmDetailsEnabled() bool { return k.helmDetails }
func (k *cluster) HelmHistory(ctx context.Context, ns, name string) (model.HelmHistory, error) {
	out := model.HelmHistory{Name: name, Namespace: ns, Mode: "cluster", Revisions: []model.HelmRevision{}, Warnings: []model.Warning{}, Retention: "Only currently retained Helm Secret revisions; deleted revisions cannot be recovered. Raw values, notes, templates and raw manifests are never returned; comparisons expose only sanitized values differences."}
	if err := ValidateHelmReference(ns, name); err != nil {
		return out, err
	}
	if !k.helmDetails {
		return out, &Error{Code: 403, Message: "Helm details disabled. Administrator must set TIMONE_HELM_DETAILS=true and grant Secret list permission."}
	}
	spec, _ := FindKind("Secret")
	if _, err := k.scope(spec, ns); err != nil {
		return out, err
	}
	options := metav1.ListOptions{Limit: 5, LabelSelector: "owner=helm,name=" + name}
	seen := map[string]bool{}
	revisions := map[int]bool{}
	count, total := 0, 0
	// Deliberately bypass the generic sanitized list/cache. Raw data exists only
	// inside this bounded operation, and is reduced to safe DTOs before returning.
	for {
		page, err := k.resource(spec, ns).List(quiet(ctx), options)
		if err != nil {
			return out, safeError(err)
		}
		for _, item := range page.Items {
			count++
			if count > 50 {
				return out, &Error{Code: 413, Message: "Helm history exceeds 50 retained revisions; reduce release history retention"}
			}
			o := model.Object(item.Object)
			release, rev, valid := integrations.HelmStorageIdentity(o)
			if !valid || release != name || model.Str(o, "metadata", "namespace") != ns {
				out.Warnings = append(out.Warnings, model.Warning{Kind: "Helm", Message: "A nonconforming release Secret was omitted"})
				continue
			}
			if revisions[rev] {
				return out, &Error{Code: 502, Message: "Duplicate Helm revision in storage"}
			}
			revisions[rev] = true
			summary, size, err := integrations.DecodeStoredHelm(ctx, model.Str(o, "data", "release"), name, rev)
			if budgetErr := consumeReadBudget(ctx, size); budgetErr != nil {
				return out, budgetErr
			}
			total += size
			if total > 16<<20 {
				return out, &Error{Code: 413, Message: "Decoded Helm history exceeds 16 MiB"}
			}

			if err != nil {
				if ctx.Err() != nil {
					return out, ctx.Err()
				}
				out.Warnings = append(out.Warnings, model.Warning{Kind: "Helm", Message: fmt.Sprintf("Revision %d could not be decoded safely", rev)})
				continue
			}
			if !summary.InventoryAvailable {
				out.Warnings = append(out.Warnings, model.Warning{Kind: "Helm", Message: fmt.Sprintf("Revision %d inventory unavailable; malformed, ambiguous or over 1000 resources", rev)})
			}
			out.Revisions = append(out.Revisions, summary)
		}
		next := page.GetContinue()
		if next == "" {
			break
		}
		if seen[next] {
			return out, &Error{Code: 502, Message: "Kubernetes API repeated a Helm continuation token"}
		}
		seen[next] = true
		options.Continue = next
	}
	if count == 0 {
		return out, ErrNotFound
	}
	sort.Slice(out.Revisions, func(i, j int) bool { return out.Revisions[i].Revision > out.Revisions[j].Revision })
	return out, ctx.Err()
}
