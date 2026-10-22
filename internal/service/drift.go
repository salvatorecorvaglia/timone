package service

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
	"timone/internal/integrations"
	"timone/internal/model"
	"timone/internal/provider"
	"unicode"
)

// Compare only desired-declared fields. Unspecified defaults and extra live fields
// are not evidence of drift. Named object lists are aligned by name, not position.
func desiredLiveProjection(desired, live any) any {
	switch d := desired.(type) {
	case map[string]any:
		l, _ := live.(map[string]any)
		out := map[string]any{}
		for k, v := range d {
			if found, ok := l[k]; ok {
				out[k] = desiredLiveProjection(v, found)
			}
		}
		return out
	case []any:
		l, ok := live.([]any)
		if !ok {
			return live
		}
		keyed := len(d) > 0
		byName := map[string]any{}
		for _, v := range l {
			m, ok := v.(map[string]any)
			name := model.Str(m, "name")
			if !ok || name == "" || byName[name] != nil {
				keyed = false
				break
			}
			byName[name] = v
		}
		seen := map[string]bool{}
		for _, v := range d {
			m, ok := v.(map[string]any)
			name := model.Str(m, "name")
			if !ok || name == "" || seen[name] {
				keyed = false
				break
			}
			seen[name] = true
		}
		out := []any{}
		for i, v := range d {
			var actual any
			if keyed {
				actual = byName[model.Str(v.(map[string]any), "name")]
			} else if i < len(l) {
				actual = l[i]
			}
			out = append(out, desiredLiveProjection(v, actual))
		}
		return out
	default:
		return live
	}
}
func (s *Service) GitOpsDrift(ctx context.Context, kind, ns, name string) (model.DriftReport, error) {
	out := model.DriftReport{State: "unavailable", Mode: s.Provider.Mode(), Changes: []model.DriftChange{}, Warnings: []model.Warning{}, ObservedAt: time.Now().UTC().Format(time.RFC3339), Limitations: "Compares sanitized fields declared in rendered desired manifests against fresh readable live objects. Omitted/defaulted/live-only fields, Secret/ConfigMap data, literal credentials, custom specs, admission transformations and pruning are not assessed. Named lists align by name; other lists are positional. A visible match is not a full synchronization or traffic guarantee. Reads are not an atomic cluster snapshot."}
	if kind != "Application" && kind != "Kustomization" && kind != "HelmRelease" {
		return out, &provider.Error{Code: 400, Message: "Choose an Argo Application or Flux Kustomization/HelmRelease"}
	}
	if err := ValidateResource(kind, ns, name); err != nil {
		return out, err
	}
	if _, err := s.Scope(ns); err != nil {
		return out, err
	}
	controller, err := s.Provider.Get(ctx, kind, ns, name)
	if err != nil {
		return out, err
	}
	out.Controller = model.Summarize(controller)
	source, ok := s.Provider.(provider.DesiredProvider)
	if !ok {
		return out, &provider.Error{Code: 503, Message: "Rendered desired manifests are unavailable"}
	}
	bundle, err := source.DesiredManifests(ctx, kind, ns, name)
	if err != nil {
		return out, err
	}
	if bundle.Generation < 1 || len(bundle.Revision) > 512 || strings.ContainsFunc(bundle.Revision, unicode.IsControl) || bundle.ControllerUID == "" || bundle.ControllerUID != model.Str(controller, "metadata", "uid") || bundle.Generation != int64(model.Num(controller, "metadata", "generation")) || bundle.Revision == "" || len(bundle.Manifests) == 0 || len(bundle.Manifests) > 1000 {
		return out, &provider.Error{Code: 409, Message: "Desired bundle identity/generation/revision is missing, stale or invalid"}
	}
	out.Revision = bundle.Revision
	out.Source = bundle.Source
	out.Total = len(bundle.Manifests)
	candidates := bundle.Manifests[:min(len(bundle.Manifests), 100)]
	if len(bundle.Manifests) > 100 {
		out.Warnings = append(out.Warnings, model.Warning{Kind: kind, Message: "Drift investigation limited to 100 manifests; remaining targets unassessed."})
	}
	seen := map[string]bool{}
	for _, o := range candidates {
		key := model.Str(o, "apiVersion") + "/" + model.Str(o, "kind") + "/" + model.Str(o, "metadata", "namespace") + "/" + model.Str(o, "metadata", "name")
		if seen[key] {
			return out, &provider.Error{Code: 502, Message: "Duplicate desired manifest identity"}
		}
		seen[key] = true
	}
	type missingInventory struct {
		once      sync.Once
		items     []model.Object
		truncated bool
		err       error
	}
	missingReads := map[string]*missingInventory{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, desired := range candidates {
		wg.Add(1)
		go func(desired model.Object) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			warn := func(message string) {
				mu.Lock()
				defer mu.Unlock()
				out.Warnings = append(out.Warnings, model.Warning{Kind: model.Str(desired, "kind"), Message: message})
			}
			spec, ok := provider.FindKind(model.Str(desired, "kind"))
			if !ok || (spec.Group == "" && model.Str(desired, "apiVersion") != "v1") || (spec.Group != "" && model.Str(desired, "apiVersion") != spec.Group+"/"+spec.Version) {
				warn("Desired API group/version/kind is outside the safe comparison allowlist.")
				return
			}
			namespace := model.Str(desired, "metadata", "namespace")
			if spec.Namespaced && namespace == "" {
				namespace = bundle.Namespace
			}
			objectName := model.Str(desired, "metadata", "name")
			if err := ValidateResource(spec.Name, namespace, objectName); err != nil {
				warn("Desired resource identity is invalid or has no resolved namespace.")
				return
			}
			if spec.Namespaced {
				if _, err := s.Scope(namespace); err != nil {
					warn("Desired resource lies outside the workspace namespace ceiling.")
					return
				}
			}
			clean, supported, err := integrations.SafeManifest(desired)
			if err != nil || !supported {
				warn("Desired manifest cannot be safely compared.")
				return
			}
			metadata := model.Map(clean, "metadata")
			for _, key := range []string{"uid", "resourceVersion", "creationTimestamp", "generation", "managedFields", "annotations"} {
				delete(metadata, key)
			}
			if spec.Namespaced {
				metadata["namespace"] = namespace
			}
			live, err := s.Provider.Get(ctx, spec.Name, namespace, objectName)
			r := model.Resource{Kind: spec.Name, Namespace: namespace, Name: objectName, ID: model.ID(spec.Name, namespace, objectName), Health: "unknown"}
			if err != nil {
				confirmedMissing := false
				if provider.Status(err) == 404 {
					if fresh, ok := s.Provider.(provider.FreshPresenceProvider); ok {
						missing, probeError := fresh.ConfirmMissing(ctx, spec.Name, namespace, objectName)
						confirmedMissing = missing && probeError == nil
					} else {

						key := spec.Name + ":" + namespace
						mu.Lock()
						inventory := missingReads[key]
						if inventory == nil {
							inventory = &missingInventory{}
							missingReads[key] = inventory
						}
						mu.Unlock()
						inventory.once.Do(func() {
							inventory.items, inventory.truncated, inventory.err = s.Provider.List(ctx, spec.Name, namespace)
						})
						confirmedMissing = inventory.err == nil && !inventory.truncated
						for _, object := range inventory.items {
							if model.Str(object, "metadata", "name") == objectName {
								confirmedMissing = false
								break
							}
						}
					}
				}
				if confirmedMissing {
					mu.Lock()
					out.Checked++
					if len(out.Changes) < 1000 {
						out.Changes = append(out.Changes, model.DriftChange{Resource: r, Path: "/", Change: "missing", Desired: "Declared resource", Actual: nil})
					} else {
						out.ChangesTruncated = true
					}
					mu.Unlock()
				} else {
					warn("Live resource unreadable; no drift decision inferred.")
				}
				return
			}
			current, _, err := integrations.SafeManifest(live)
			if err != nil {
				warn("Live resource exceeds safe comparison bounds.")
				return
			}
			r = model.Summarize(live)
			comparison := model.HelmComparison{From: model.HelmRevision{ConfigurationAvailable: true, Configuration: []model.HelmManifest{{Resource: model.HelmResource{Kind: spec.Name, Name: objectName, Namespace: namespace}, Object: clean}}}, To: model.HelmRevision{ConfigurationAvailable: true, Configuration: []model.HelmManifest{{Resource: model.HelmResource{Kind: spec.Name, Name: objectName, Namespace: namespace}, Object: model.Object(desiredLiveProjection(map[string]any(clean), map[string]any(current)).(map[string]any))}}}}
			helmDiff(&comparison)
			mu.Lock()
			defer mu.Unlock()
			out.Checked++
			out.ChangesTruncated = out.ChangesTruncated || comparison.ChangesTruncated
			for _, change := range comparison.Changes {
				if len(out.Changes) >= 1000 {
					out.ChangesTruncated = true
					break
				}
				out.Changes = append(out.Changes, model.DriftChange{Resource: r, Path: change.Path, Change: change.Change, Desired: change.Before, Actual: change.After})
			}
		}(desired)
	}
	wg.Wait()
	if out.ChangesTruncated {
		out.Warnings = append(out.Warnings, model.Warning{Kind: kind, Message: "Drift differences truncated at 1000 entries."})
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	latest, err := s.Provider.Get(ctx, kind, ns, name)
	if err != nil {
		return out, err
	}
	if model.Str(latest, "metadata", "uid") != bundle.ControllerUID || int64(model.Num(latest, "metadata", "generation")) != bundle.Generation || model.Str(latest, "status", "lastAttemptedRevision") != model.Str(controller, "status", "lastAttemptedRevision") || model.Str(latest, "status", "lastAttemptedConfigDigest") != model.Str(controller, "status", "lastAttemptedConfigDigest") {
		return out, &provider.Error{Code: 409, Message: "Controller target changed during comparison; retry"}
	}
	sort.Slice(out.Changes, func(i, j int) bool {
		a, b := out.Changes[i], out.Changes[j]
		if a.Resource.ID == b.Resource.ID {
			return a.Path < b.Path
		}
		return a.Resource.ID < b.Resource.ID
	})
	sort.Slice(out.Warnings, func(i, j int) bool {
		a, _ := json.Marshal(out.Warnings[i])
		b, _ := json.Marshal(out.Warnings[j])
		return string(a) < string(b)
	})
	if len(out.Changes) > 0 {
		out.State = "drift-detected"
	} else if out.Checked > 0 && len(out.Warnings) == 0 {
		out.State = "no-visible-drift"
	} else {
		out.State = "partial"
	}
	return out, nil
}
