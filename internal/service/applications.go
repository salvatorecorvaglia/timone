package service

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"timone/internal/integrations"
	"timone/internal/model"
	"timone/internal/provider"
)

type Application struct {
	ID            string                    `json:"id"`
	Name          string                    `json:"name"`
	Namespace     string                    `json:"namespace"`
	Health        string                    `json:"health"`
	Basis         string                    `json:"basis"`
	Resources     []model.Resource          `json:"resources"`
	Relationships []model.Edge              `json:"relationships"`
	Issues        []model.Issue             `json:"issues"`
	Integrations  []model.IntegrationRecord `json:"integrations"`
	Truncated     bool                      `json:"truncated"`
}
type ApplicationPage struct {
	Items       []Application   `json:"items"`
	Total       int             `json:"total"`
	Limit       int             `json:"limit"`
	Offset      int             `json:"offset"`
	Warnings    []model.Warning `json:"warnings"`
	ObservedAt  time.Time       `json:"observedAt"`
	Mode        string          `json:"mode"`
	Limitations string          `json:"limitations"`
}

func (s *Service) Applications(ctx context.Context, q Query) (ApplicationPage, error) {
	out := ApplicationPage{Items: []Application{}, Limitations: "Workload ownership and selectors are declared relationships. app.kubernetes.io/part-of is grouping metadata, not a traffic dependency. GitOps links require controller-reported resource inventory or explicit Helm ownership metadata; live drift is not verified. Each application retains at most 500 resources and 1000 relationships. Relationship evaluation stops after 200000 references per investigation and reports partial coverage."}
	if err := q.Validate(); err != nil {
		return out, err
	}
	if q.Kind != "" || q.Resource != "" || q.Phase != "" || q.Since != "" || q.Until != "" || (q.Sort != "" && q.Sort != "name" && q.Sort != "namespace" && q.Sort != "status") {
		return out, &provider.Error{Code: 400, Message: "Applications supports namespace, search, health, name/namespace/status sorting and pagination"}
	}
	snap, err := s.Snapshot(ctx, q.Namespace, "Secret", "ConfigMap", "ServiceAccount")
	if err != nil {
		return out, err
	}
	// Controllers commonly live outside the selected workload namespace. Read
	// their inventory only within the configured workspace namespace ceiling.
	for _, kind := range []string{"Application", "Kustomization", "HelmRelease"} {
		objects, truncated, e := s.Provider.List(ctx, kind, s.Provider.Namespace())
		if e != nil {
			snap.Warnings = append(snap.Warnings, model.Warning{Kind: kind, Message: e.Error()})
			continue
		}
		if truncated {
			snap.Warnings = append(snap.Warnings, model.Warning{Kind: kind, Message: "Controller inventory truncated; delivery associations incomplete."})
		}
		for _, o := range objects {
			snap.Resources = append(snap.Resources, model.Summarize(o))
		}
	}
	out.Mode = s.Provider.Mode()
	out.Limit = q.Limit
	out.Offset = q.Offset
	out.Warnings = snap.Warnings
	out.ObservedAt = snap.ObservedAt
	all := resourceIndex(snap.Resources)
	parent := func(r model.Resource) model.Resource {
		var seen [8]string
		for depth := 0; depth < len(seen) && !slices.Contains(seen[:depth], r.ID); depth++ {
			seen[depth] = r.ID
			next := r
			for _, v := range model.Arr(r.Object, "metadata", "ownerReferences") {
				ref, _ := v.(map[string]any)
				p, ok := all[model.ID(model.Str(ref, "kind"), r.Namespace, model.Str(ref, "name"))]
				if ok && model.Bool(ref, "controller") && (model.Str(ref, "uid") == "" || model.Str(ref, "uid") == model.Str(p.Object, "metadata", "uid")) && (model.Str(ref, "apiVersion") == "" || model.Str(ref, "apiVersion") == model.Str(p.Object, "apiVersion")) {
					next = p
					break
				}
			}
			if next.ID == r.ID {
				break
			}
			r = next
		}
		return r
	}
	groups := map[string]*Application{}
	membership := map[string]string{}
	members := map[string]map[string]bool{}
	addMember := func(id, resourceID string) {
		if id == "" || members[id][resourceID] {
			return
		}
		if len(members[id]) >= 500 {
			groups[id].Truncated = true
			return
		}
		members[id][resourceID] = true
	}
	for _, r := range snap.Resources {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		switch r.Kind {
		case "Deployment", "StatefulSet", "DaemonSet", "CronJob", "Job", "Pod":
		default:
			continue
		}
		root := parent(r)
		name := root.Name
		id := root.ID
		basis := "Controller ownership"
		if label, ok := root.Labels["app.kubernetes.io/part-of"].(string); ok && label != "" {
			name = label
			id = "label:" + root.Namespace + ":" + label
			basis = "Explicit app.kubernetes.io/part-of label"
		}
		if groups[id] == nil {
			groups[id] = &Application{ID: id, Name: name, Namespace: root.Namespace, Health: "unknown", Basis: basis, Resources: []model.Resource{}, Relationships: []model.Edge{}, Issues: []model.Issue{}, Integrations: []model.IntegrationRecord{}}
			members[id] = map[string]bool{}
		}
		membership[r.ID] = id
		membership[root.ID] = id
		addMember(id, r.ID)
		addMember(id, root.ID)
	}
	// Intermediate ReplicaSets and any readable controller-owned descendants.
	for _, r := range snap.Resources {
		if id := membership[parent(r).ID]; id != "" && r.Namespace == groups[id].Namespace {
			addMember(id, r.ID)
			membership[r.ID] = id
		}
	}
	// Include services selecting member Pods, then declared Pod configuration references.
	relationshipsInspected := 0
	const relationshipWorkLimit = 200000
	if err := visitDeclaredEdges(ctx, snap, all, func(e model.Edge) bool {
		relationshipsInspected++
		if relationshipsInspected > relationshipWorkLimit {
			return false
		}
		if e.Relation == "selects" {
			if id := membership[e.Target]; id != "" {
				addMember(id, e.Source)
			}
		}
		if e.Relation == "references" || e.Relation == "mounts" {
			if id := membership[e.Source]; id != "" {
				addMember(id, e.Target)
			}
		}
		return true
	}); err != nil {
		return out, err
	}
	if relationshipsInspected > relationshipWorkLimit {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Application", Message: "Application relationship evaluation limited to 200000 references; narrow namespace scope."})
		for _, group := range groups {
			group.Truncated = true
		}
	}
	for _, r := range snap.Resources {
		if r.Kind == "Pod" {
			if id := membership[r.ID]; id != "" {
				name := model.Str(r.Object, "spec", "serviceAccountName")
				if name == "" {
					name = "default"
				}
				sa := model.ID("ServiceAccount", r.Namespace, name)
				if _, ok := all[sa]; ok {
					addMember(id, sa)
				}
			}
		}
	}
	delivery := deliveryAssociations(snap.Resources, all)
	issues := s.detectIssues(snap)
	// Index retained Helm metadata once, rather than scanning every Secret for each app.
	releasesByNamespace := map[string]map[string]model.Resource{}
	releaseNamesByNamespace := map[string][]string{}
	for _, r := range snap.Resources {
		if r.Kind != "Secret" {
			continue
		}
		name, rev, valid := integrations.HelmStorageIdentity(r.Object)
		if !valid {
			continue
		}
		releases := releasesByNamespace[r.Namespace]
		if releases == nil {
			releases = map[string]model.Resource{}
			releasesByNamespace[r.Namespace] = releases
		}
		old, _ := strconv.Atoi(model.Str(releases[name].Object, "metadata", "labels", "version"))
		if name != "" && rev > old {
			releases[name] = r
		}
	}
	for ns, releases := range releasesByNamespace {
		for name := range releases {
			releaseNamesByNamespace[ns] = append(releaseNamesByNamespace[ns], name)
		}
		sort.Strings(releaseNamesByNamespace[ns])
	}
	for id, g := range groups {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		ids := []string{}
		for rid := range members[id] {
			ids = append(ids, rid)
		}
		sort.Strings(ids)
		if len(ids) > 500 {
			ids = ids[:500]
			g.Truncated = true
		}
		unknownHealth := false
		for _, rid := range ids {
			r, ok := all[rid]
			if !ok {
				continue
			}
			if membership[r.ID] == id {
				unknownHealth = unknownHealth || r.Health == "unknown"
				if r.Health == "critical" {
					g.Health = "critical"
				} else if r.Health == "warning" && g.Health != "critical" {
					g.Health = "warning"
				} else if r.Health == "healthy" && g.Health == "unknown" {
					g.Health = "healthy"
				}
			}
		}
		// Only healthy candidates need relationship coverage to determine status.
		// The selected page builds its complete relationships below.
		if g.Health == "healthy" && !unknownHealth && !g.Truncated && len(out.Warnings) == 0 {
			resources := make([]model.Resource, 0, len(ids))
			visible := map[string]bool{}
			for _, rid := range ids {
				if r, ok := all[rid]; ok {
					resources = append(resources, r)
					visible[rid] = true
				}
			}
			collector := newEdgeCollector(1000, visible)
			collector.countOnly = true
			if err := visitDeclaredEdges(ctx, model.Snapshot{Resources: resources, Warnings: snap.Warnings}, all, collector.add); err != nil {
				return out, err
			}
			g.Truncated = collector.truncated
		}
		if (unknownHealth || g.Truncated || len(out.Warnings) > 0) && g.Health == "healthy" {
			g.Health = "unknown"
		}
		if (q.Search == "" || strings.Contains(strings.ToLower(g.Name+" "+g.Namespace+" "+g.ID), strings.ToLower(q.Search))) && (q.Status == "" || q.Status == g.Health) {
			out.Items = append(out.Items, *g)
		}
	}
	sort.Slice(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		av, bv := a.ID, b.ID
		switch q.Sort {
		case "name":
			av, bv = a.Name, b.Name
		case "namespace":
			av, bv = a.Namespace, b.Namespace
		case "status":
			av, bv = a.Health, b.Health
		}
		if av == bv {
			av, bv = a.ID, b.ID
		}
		if q.Desc {
			return av > bv
		}
		return av < bv
	})
	out.Total = len(out.Items)
	start := min(q.Offset, out.Total)
	out.Items = out.Items[start:min(start+q.Limit, out.Total)]
	// Only enrich the selected page. Membership and relationship coverage above
	// determine health, so filtering and totals retain their original semantics.
	for i := range out.Items {
		g := &out.Items[i]
		id := g.ID
		ids := make([]string, 0, len(members[id]))
		for rid := range members[id] {
			ids = append(ids, rid)
		}
		sort.Strings(ids)
		visible := map[string]bool{}
		for _, rid := range ids {
			if r, ok := all[rid]; ok {
				g.Resources = append(g.Resources, r)
				visible[rid] = true
			}
		}
		collector := newEdgeCollector(1000, visible)
		if err := visitDeclaredEdges(ctx, model.Snapshot{Resources: g.Resources, Warnings: snap.Warnings}, all, collector.add); err != nil {
			return out, err
		}
		g.Relationships = collector.edges
		g.Truncated = g.Truncated || collector.truncated

		for _, issue := range issues {
			if members[id][issue.Resource.ID] {
				g.Issues = append(g.Issues, issue)
			}
		}
		releases := releasesByNamespace[g.Namespace]
		for _, name := range releaseNamesByNamespace[g.Namespace] {

			secret := releases[name]
			for _, member := range g.Resources {
				if member.Labels["app.kubernetes.io/managed-by"] == "Helm" && member.Labels["app.kubernetes.io/instance"] == name {
					g.Integrations = append(g.Integrations, model.IntegrationRecord{Name: name, Status: model.Str(secret.Object, "metadata", "labels", "status"), Revision: model.Str(secret.Object, "metadata", "labels", "version"), Resource: secret, Observation: "label association", Evidence: []string{"Explicit Helm managed-by and instance labels match the observed release Secret metadata."}, Recommendations: []string{"Inspect retained release inventory to verify ownership."}, Conditions: []model.IntegrationCondition{}})
					break
				}
			}
		}
		controllers := map[string]model.Resource{}
		for _, member := range g.Resources {
			for _, controller := range delivery[member.ID] {
				controllers[controller.ID] = controller
			}
		}
		controllerIDs := []string{}
		for id := range controllers {
			controllerIDs = append(controllerIDs, id)
		}
		sort.Strings(controllerIDs)
		for _, id := range controllerIDs {
			record := gitOpsRecord(controllers[id])
			record.Evidence = append(record.Evidence, "Matched an exact controller inventory identity or explicit Flux Helm ownership labels in this application.")
			g.Integrations = append(g.Integrations, record)
		}
	}
	return out, ctx.Err()
}

// Build associations once instead of rescanning every controller for each app.
func deliveryAssociations(resources []model.Resource, all map[string]model.Resource) map[string][]model.Resource {
	out := map[string][]model.Resource{}
	inventory := map[string]model.Resource{}
	helmControllers := map[string]model.Resource{}
	for _, r := range resources {
		group, _, has := strings.Cut(model.Str(r.Object, "apiVersion"), "/")
		if !has {
			group = ""
		}
		inventory[r.Namespace+"_"+r.Name+"_"+group+"_"+r.Kind] = r
		if r.Kind == "HelmRelease" {
			helmControllers[r.Namespace+"/"+r.Name] = r
		}
	}
	for _, controller := range resources {
		if controller.Kind == "Application" {
			for _, value := range model.Arr(controller.Object, "status", "resources") {
				ref, _ := value.(map[string]any)
				r, ok := all[model.ID(model.Str(ref, "kind"), model.Str(ref, "namespace"), model.Str(ref, "name"))]
				if !ok {
					continue
				}
				group, _, has := strings.Cut(model.Str(r.Object, "apiVersion"), "/")
				if !has {
					group = ""
				}
				if group == model.Str(ref, "group") {
					out[r.ID] = append(out[r.ID], controller)
				}
			}
		}
		if controller.Kind != "Kustomization" && controller.Kind != "HelmRelease" {
			continue
		}
		generation := model.Num(controller.Object, "metadata", "generation")
		if generation <= 0 || model.Num(controller.Object, "status", "observedGeneration") != generation {
			continue
		}
		for _, value := range model.Arr(controller.Object, "status", "inventory", "entries") {
			ref, _ := value.(map[string]any)
			r, ok := inventory[model.Str(ref, "id")]
			if !ok {
				continue
			}
			_, v, has := strings.Cut(model.Str(r.Object, "apiVersion"), "/")
			if !has {
				v = model.Str(r.Object, "apiVersion")
			}
			if model.Str(ref, "v") == v {
				out[r.ID] = append(out[r.ID], controller)
			}
		}
	}
	for _, r := range resources {
		name, _ := r.Labels["helm.toolkit.fluxcd.io/name"].(string)
		ns, _ := r.Labels["helm.toolkit.fluxcd.io/namespace"].(string)
		if name != "" && ns != "" {
			if controller, ok := helmControllers[ns+"/"+name]; ok {
				out[r.ID] = append(out[r.ID], controller)
			}
		}
	}
	return out
}
