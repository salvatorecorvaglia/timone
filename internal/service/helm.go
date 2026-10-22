package service

import (
	"context"
	"sort"
	"timone/internal/integrations"
	"timone/internal/model"
	"timone/internal/provider"
)

func (s *Service) HelmHistory(ctx context.Context, ns, name string) (model.HelmHistory, error) {
	if err := provider.ValidateHelmReference(ns, name); err != nil {
		return model.HelmHistory{}, err
	}
	if _, err := s.Scope(ns); err != nil {
		return model.HelmHistory{}, err
	}
	return s.Provider.HelmHistory(ctx, ns, name)
}
func (s *Service) HelmCompare(ctx context.Context, ns, name string, from, to int) (model.HelmComparison, error) {
	out := model.HelmComparison{Added: []model.HelmResource{}, Removed: []model.HelmResource{}, Limitations: "Compares retained chart metadata, identities and sanitized desired manifest fields. Secret/ConfigMap contents, literal environment values, command arguments, headers, credential fields, annotations, unredacted Helm values/hooks/templates/notes and live drift are excluded. Unknown custom APIs expose metadata only. No visible differences do not establish equality of redacted fields. Empty inventory namespaces mean unspecified in the stored manifest."}
	if from < 1 || to < 1 || from == to || from > 2147483647 || to > 2147483647 {
		return out, &provider.Error{Code: 400, Message: "Select two distinct positive Helm revision numbers"}
	}
	history, err := s.HelmHistory(ctx, ns, name)
	if err != nil {
		return out, err
	}
	foundFrom, foundTo := false, false
	for _, r := range history.Revisions {
		if r.Revision == from {
			out.From = r
			foundFrom = true
		}
		if r.Revision == to {
			out.To = r
			foundTo = true
		}
	}
	if !foundFrom || !foundTo {
		return out, &provider.Error{Code: 404, Message: "Selected revision is not retained or could not be decoded"}
	}
	out.InventoryComparable = out.From.InventoryAvailable && out.To.InventoryAvailable
	if out.InventoryComparable {
		a, b := map[string]model.HelmResource{}, map[string]model.HelmResource{}
		for _, r := range out.From.Inventory {
			a[integrations.InventoryKey(r)] = r
		}
		for _, r := range out.To.Inventory {
			b[integrations.InventoryKey(r)] = r
		}
		for k, r := range a {
			if _, ok := b[k]; !ok {
				out.Removed = append(out.Removed, r)
			}
		}
		for k, r := range b {
			if _, ok := a[k]; !ok {
				out.Added = append(out.Added, r)
			}
		}
		sort.Slice(out.Added, func(i, j int) bool {
			return integrations.InventoryKey(out.Added[i]) < integrations.InventoryKey(out.Added[j])
		})
		sort.Slice(out.Removed, func(i, j int) bool {
			return integrations.InventoryKey(out.Removed[i]) < integrations.InventoryKey(out.Removed[j])
		})
	}
	helmDiff(&out)
	out.ValuesChanges = []model.FieldChange{}
	out.ValuesComparable = out.From.ValuesAvailable && out.To.ValuesAvailable
	if out.ValuesComparable {
		diff := model.HelmComparison{From: model.HelmRevision{ConfigurationAvailable: true, Configuration: []model.HelmManifest{{Resource: model.HelmResource{Kind: "Values"}, Object: out.From.Values}}}, To: model.HelmRevision{ConfigurationAvailable: true, Configuration: []model.HelmManifest{{Resource: model.HelmResource{Kind: "Values"}, Object: out.To.Values}}}}
		helmDiff(&diff)
		out.ValuesTruncated = diff.ChangesTruncated
		for _, c := range diff.Changes {
			out.ValuesChanges = append(out.ValuesChanges, model.FieldChange{Path: c.Path, Change: c.Change, Before: c.Before, After: c.After})
		}
	}

	return out, nil
}
