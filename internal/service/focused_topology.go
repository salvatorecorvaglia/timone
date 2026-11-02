package service

import (
	"context"
	"sort"
	"timone/internal/model"
	"timone/internal/provider"
)

type FocusedGraph struct {
	model.Graph
	Root      string   `json:"root"`
	Depth     int      `json:"depth"`
	Direction string   `json:"direction"`
	Path      []string `json:"path"`
	PathState string   `json:"pathState"`
}

func (s *Service) FocusedTopology(ctx context.Context, kind, ns, name string, depth int, direction, target string) (FocusedGraph, error) {
	out := FocusedGraph{Graph: model.Graph{Nodes: []model.Resource{}, Edges: []model.Edge{}, Warnings: []model.Warning{}}, Depth: depth, Direction: direction, Path: []string{}, PathState: "not_requested"}
	if depth < 1 || depth > 4 || (direction != "both" && direction != "in" && direction != "out") || len(target) > 800 {
		return out, &provider.Error{Code: 400, Message: "Depth must be 1–4 and direction in, out or both"}
	}
	if e := ValidateResource(kind, ns, name); e != nil {
		return out, e
	}
	if _, e := s.Scope(ns); e != nil {
		return out, e
	}
	root, e := s.Provider.Get(ctx, kind, ns, name)
	if e != nil {
		return out, e
	}
	snap, e := s.Snapshot(ctx, ns, "Secret", "ConfigMap", "Gateway", "HTTPRoute", "GatewayClass", "ReferenceGrant", "PersistentVolume", "StorageClass", "VolumeAttachment")
	if e != nil {
		return out, e
	}
	out.Warnings = snap.Warnings
	out.Root = model.Summarize(root).ID
	all := resourceIndex(snap.Resources)
	all[out.Root] = model.Summarize(root)
	snap.Resources = []model.Resource{}
	for _, r := range all {
		snap.Resources = append(snap.Resources, r)
	}
	sort.Slice(snap.Resources, func(i, j int) bool { return snap.Resources[i].ID < snap.Resources[j].ID })
	seen := map[string]bool{out.Root: true}
	frontier := map[string]bool{out.Root: true}
	edgeIDs := map[string]bool{}
	for step := 0; step < depth; step++ {
		next := map[string]bool{}
		work := 0
		e = visitDeclaredEdges(ctx, snap, all, func(edge model.Edge) bool {
			work++
			if work > 100000 {
				out.Truncated = true
				return false
			}
			if edge.Relation == "contains" && kind != "Namespace" {
				return true
			}
			neighbor := ""
			if frontier[edge.Source] && (direction == "out" || direction == "both") {
				neighbor = edge.Target
			}
			if frontier[edge.Target] && (direction == "in" || direction == "both") {
				neighbor = edge.Source
			}
			if neighbor == "" {
				return true
			}
			if !seen[neighbor] {
				if len(seen) >= 200 {
					out.Truncated = true
					return true
				}
				seen[neighbor] = true
				next[neighbor] = true
			}
			if !edgeIDs[edge.ID] {
				if len(out.Edges) >= 2000 {
					out.Truncated = true
					return false
				}
				edgeIDs[edge.ID] = true
				out.Edges = append(out.Edges, edge)
			}
			return true
		})
		if e != nil {
			return out, e
		}
		frontier = next
		if len(frontier) == 0 {
			break
		}
	}
	for id := range seen {
		out.Nodes = append(out.Nodes, all[id])
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	sort.Slice(out.Edges, func(i, j int) bool { return out.Edges[i].ID < out.Edges[j].ID })
	if out.Truncated {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Topology", Message: "Exploration reached its node, relationship or work limit; narrow scope or re-center on a resource"})
	}
	if target != "" {
		out.PathState = "not_found_in_observed_graph"
		previous := map[string]string{out.Root: ""}
		queue := []string{out.Root}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			if id == target {
				for n := id; n != ""; n = previous[n] {
					out.Path = append([]string{n}, out.Path...)
				}
				out.PathState = "found"
				break
			}
			for _, edge := range out.Edges {
				n := ""
				if edge.Source == id && (direction == "out" || direction == "both") {
					n = edge.Target
				}
				if edge.Target == id && (direction == "in" || direction == "both") {
					n = edge.Source
				}
				if n != "" {
					if _, ok := previous[n]; !ok {
						previous[n] = id
						queue = append(queue, n)
					}
				}
			}
		}
	}
	return out, ctx.Err()
}
