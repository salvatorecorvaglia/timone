package service

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"timone/internal/model"
	"timone/internal/provider"
)

func (s *Service) Topology(ctx context.Context, q Query) (model.Graph, error) {
	return s.TopologyWithGroup(ctx, q, "")
}

// TopologyWithGroup filters before the node cap so large unrelated collections
// cannot hide the resource types selected by the operator.
func (s *Service) TopologyWithGroup(ctx context.Context, q Query, group string) (model.Graph, error) {
	groups := map[string][]string{
		"workloads":     {"Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Pod", "Job", "CronJob"},
		"networking":    {"Service", "Ingress", "Gateway", "GatewayClass", "ReferenceGrant", "HTTPRoute", "Pod"},
		"configuration": {"Secret", "ConfigMap", "PersistentVolumeClaim", "Pod"},
	}
	if group != "" && groups[group] == nil {
		return model.Graph{}, &provider.Error{Code: 400, Message: "Unsupported topology group"}
	}
	if e := q.Validate(); e != nil {
		return model.Graph{}, e
	}
	snap, e := s.Snapshot(ctx, q.Namespace, "Secret", "ConfigMap", "Gateway", "HTTPRoute", "GatewayClass", "ReferenceGrant")
	if e != nil {
		return model.Graph{}, e
	}
	nodes := []model.Resource{}
	visible := map[string]bool{}
	for _, r := range snap.Resources {
		if group != "" && !slices.Contains(groups[group], r.Kind) {
			continue
		}
		if q.Kind != "" && q.Kind != r.Kind {
			continue
		}
		if q.Search != "" && !contains(r.Name, q.Search) {
			continue
		}
		nodes = append(nodes, r)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	trunc := len(nodes) > 200
	nodes = nodes[:min(len(nodes), 200)]
	for _, r := range nodes {
		visible[r.ID] = true
	}
	all := resourceIndex(snap.Resources)
	collector := newEdgeCollector(2000, visible)
	if err := visitDeclaredEdges(ctx, model.Snapshot{Resources: nodes, Warnings: snap.Warnings}, all, collector.add); err != nil {
		return model.Graph{}, err
	}
	filtered := collector.edges
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID < filtered[j].ID })
	if collector.truncated {
		trunc = true
		snap.Warnings = append(snap.Warnings, model.Warning{Kind: "Topology", Message: "Relationships limited to 2000; narrow the namespace or resource filters."})
	}
	return model.Graph{Nodes: nodes, Edges: filtered, Truncated: trunc, Warnings: snap.Warnings}, nil
}

func resourceIndex(resources []model.Resource) map[string]model.Resource {
	all := map[string]model.Resource{}
	for _, r := range resources {
		all[r.ID] = r
	}
	return all
}

// edgeCollector bounds retained relationships, including deduplication state.
type edgeCollector struct {
	edges     []model.Edge
	seen      map[string]bool
	visible   map[string]bool
	limit     int
	truncated bool
	countOnly bool // Coverage preflight does not retain response edges.
}

func newEdgeCollector(limit int, visible map[string]bool) *edgeCollector {
	return &edgeCollector{edges: []model.Edge{}, seen: map[string]bool{}, visible: visible, limit: limit}
}
func (c *edgeCollector) add(e model.Edge) bool {
	if c.visible != nil && (!c.visible[e.Source] || !c.visible[e.Target]) {
		return true
	}
	if c.seen[e.ID] {
		return true
	}
	if c.limit > 0 && len(c.seen) >= c.limit {
		c.truncated = true
		return false
	}
	c.seen[e.ID] = true
	if !c.countOnly {
		c.edges = append(c.edges, e)
	}
	return true
}

func visitDeclaredEdges(ctx context.Context, snap model.Snapshot, all map[string]model.Resource, emit func(model.Edge) bool) error {
	// Select the smallest namespace/label bucket, then check the full selector.
	podsByLabel := map[string][]model.Resource{}
	for _, r := range snap.Resources {
		if err := ctx.Err(); err != nil {
			return err
		}
		if r.Kind != "Pod" {
			continue
		}
		for key, value := range r.Labels {
			key = r.Namespace + "\x00" + key + "\x00" + labelText(value)
			podsByLabel[key] = append(podsByLabel[key], r)
		}
	}
	stopped := false
	add := func(a, b, rel string) {
		if stopped {
			return
		}
		if _, ok := all[a]; !ok {
			return
		}
		if _, ok := all[b]; !ok {
			return
		}
		stopped = !emit(model.Edge{ID: a + ">" + b + ":" + rel, Source: a, Target: b, Relation: rel, Confidence: "declared"})
	}
	for _, r := range snap.Resources {
		if err := ctx.Err(); err != nil {
			return err
		}
		if stopped {
			return nil
		}
		if r.Namespace != "" {
			add(model.ID("Namespace", "", r.Namespace), r.ID, "contains")
		}
		for _, v := range model.Arr(r.Object, "metadata", "ownerReferences") {
			ref, _ := v.(map[string]any)
			parent := model.ID(model.Str(ref, "kind"), r.Namespace, model.Str(ref, "name"))
			if model.Str(ref, "uid") == "" || model.Str(ref, "uid") == model.Str(all[parent].Object, "metadata", "uid") {
				add(parent, r.ID, "owns")
			}
		}
		if r.Kind == "Service" {
			sel := model.Map(r.Object, "spec", "selector")
			if len(sel) > 0 {
				var candidates []model.Resource
				first := true
				for key, value := range sel {
					bucket := podsByLabel[r.Namespace+"\x00"+key+"\x00"+labelText(value)]
					if first || len(bucket) < len(candidates) {
						candidates = bucket
						first = false
					}
				}
				for _, pod := range candidates {
					if err := ctx.Err(); err != nil {
						return err
					}
					if stopped {
						return nil
					}
					match := true
					for k, v := range sel {
						if labelText(pod.Labels[k]) != labelText(v) {
							match = false
							break
						}
					}
					if match {
						add(r.ID, pod.ID, "selects")
					}
				}
			}
		}
		if r.Kind == "Ingress" {
			for _, v := range model.Arr(r.Object, "spec", "rules") {
				rule, _ := v.(map[string]any)
				for _, v := range model.Arr(rule, "http", "paths") {
					p, _ := v.(map[string]any)
					add(r.ID, model.ID("Service", r.Namespace, model.Str(p, "backend", "service", "name")), "routes to")
				}
			}
			if n := model.Str(r.Object, "spec", "defaultBackend", "service", "name"); n != "" {
				add(r.ID, model.ID("Service", r.Namespace, n), "routes to")
			}
		}
		if r.Kind == "HTTPRoute" {
			if err := visitGatewayEdges(ctx, r, all, snap.Warnings, func(e model.Edge) bool { stopped = !emit(e); return !stopped }); err != nil {
				return err
			}
		}
		if r.Kind == "Pod" {
			add(r.ID, model.ID("Node", "", model.Str(r.Object, "spec", "nodeName")), "scheduled on")
			for _, v := range model.Arr(r.Object, "spec", "volumes") {
				volume, _ := v.(map[string]any)
				for _, reference := range []struct{ field, kind string }{{"configMap", "ConfigMap"}, {"secret", "Secret"}, {"persistentVolumeClaim", "PersistentVolumeClaim"}} {
					field, kind := reference.field, reference.kind
					key := "name"
					if field == "secret" {
						key = "secretName"
					}
					if field == "persistentVolumeClaim" {
						key = "claimName"
					}
					if n := model.Str(volume, field, key); n != "" {
						add(r.ID, model.ID(kind, r.Namespace, n), "mounts")
					}
				}
			}
			containers := append(append([]any{}, model.Arr(r.Object, "spec", "containers")...), model.Arr(r.Object, "spec", "initContainers")...)
			for _, v := range containers {
				c, _ := v.(map[string]any)
				for _, v := range model.Arr(c, "env") {
					env, _ := v.(map[string]any)
					for _, reference := range []struct{ field, kind string }{{"secretKeyRef", "Secret"}, {"configMapKeyRef", "ConfigMap"}} {
						field, kind := reference.field, reference.kind
						if n := model.Str(env, "valueFrom", field, "name"); n != "" {
							add(r.ID, model.ID(kind, r.Namespace, n), "references")
						}
					}
				}
				for _, v := range model.Arr(c, "envFrom") {
					ref, _ := v.(map[string]any)
					for _, reference := range []struct{ field, kind string }{{"secretRef", "Secret"}, {"configMapRef", "ConfigMap"}} {
						field, kind := reference.field, reference.kind
						if n := model.Str(ref, field, "name"); n != "" {
							add(r.ID, model.ID(kind, r.Namespace, n), "references")
						}
					}
				}
			}
		}
	}
	return ctx.Err()
}

// Kubernetes labels are strings. Keep the formatting fallback for malformed objects.
func labelText(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}
