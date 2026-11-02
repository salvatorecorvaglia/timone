package service

import (
	"context"
	"sort"
	"timone/internal/model"
	"timone/internal/provider"
)

func logTarget(kind string) bool {
	switch kind {
	case "Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job":
		return true
	}
	return false
}
func controlledBy(child, parent model.Object) bool {
	uid := model.Str(parent, "metadata", "uid")
	if uid == "" || model.Str(child, "metadata", "namespace") != model.Str(parent, "metadata", "namespace") {
		return false
	}
	for _, raw := range model.Arr(child, "metadata", "ownerReferences") {
		ref, _ := raw.(map[string]any)
		if model.Bool(ref, "controller") && model.Str(ref, "uid") == uid && model.Str(ref, "kind") == model.Str(parent, "kind") && model.Str(ref, "name") == model.Str(parent, "metadata", "name") && (model.Str(ref, "apiVersion") == "" || model.Str(ref, "apiVersion") == model.Str(parent, "apiVersion")) {
			return true
		}
	}
	return false
}
func descendants(root model.Object, objects []model.Object, limit int) ([]model.Object, bool) {
	selected := []model.Object{root}
	seen := map[string]bool{model.ID(model.Str(root, "kind"), model.Str(root, "metadata", "namespace"), model.Str(root, "metadata", "name")): true}
	sort.Slice(objects, func(i, j int) bool {
		return model.Str(objects[i], "metadata", "name") < model.Str(objects[j], "metadata", "name")
	})
	for depth := 0; depth < 8; depth++ {
		added := false
		for _, o := range objects {
			id := model.ID(model.Str(o, "kind"), model.Str(o, "metadata", "namespace"), model.Str(o, "metadata", "name"))
			if seen[id] {
				continue
			}
			for _, parent := range selected {
				if controlledBy(o, parent) {
					if len(selected) >= limit {
						return selected, true
					}
					seen[id] = true
					selected = append(selected, o)
					added = true
					break
				}
			}
		}
		if !added {
			break
		}
	}
	return selected, false
}
func (s *Service) workloadPods(ctx context.Context, kind, ns, name string) (model.Object, []model.Object, []model.Warning, error) {
	if !logTarget(kind) {
		return nil, nil, nil, &provider.Error{Code: 400, Message: "Select a Pod, Deployment, StatefulSet, DaemonSet, ReplicaSet or Job"}
	}
	if e := ValidateResource(kind, ns, name); e != nil {
		return nil, nil, nil, e
	}
	if _, e := s.Scope(ns); e != nil {
		return nil, nil, nil, e
	}
	root, e := s.Provider.Get(ctx, kind, ns, name)
	if e != nil {
		return nil, nil, nil, e
	}
	if kind == "Pod" {
		return root, []model.Object{root}, []model.Warning{}, nil
	}
	warnings := []model.Warning{}
	objects := []model.Object{}
	kinds := []string{"Pod"}
	if kind == "Deployment" {
		kinds = append(kinds, "ReplicaSet")
	}
	for _, k := range kinds {
		list, trunc, e := s.Provider.List(ctx, k, ns)
		if e != nil {
			warnings = append(warnings, model.Warning{Kind: k, Message: e.Error()})
			continue
		}
		if trunc {
			warnings = append(warnings, model.Warning{Kind: k, Message: "Workload membership inventory truncated"})
		}
		objects = append(objects, list...)
	}
	members, trunc := descendants(root, objects, 201)
	if trunc {
		warnings = append(warnings, model.Warning{Kind: "Pod", Message: "Workload membership limited to 200 descendants"})
	}
	pods := []model.Object{}
	for _, o := range members {
		if model.Str(o, "kind") == "Pod" {
			pods = append(pods, o)
		}
	}
	if len(pods) == 0 {
		warnings = append(warnings, model.Warning{Kind: "Pod", Message: "No readable current Pods with verified controller UIDs were found"})
	}
	return root, pods, warnings, ctx.Err()
}
