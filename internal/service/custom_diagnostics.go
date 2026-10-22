package service

import (
	"container/heap"
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"
	"timone/internal/model"
	"timone/internal/provider"
)

const customIssueLimit = 2000

// Keep the lexically first candidates, independently of concurrent completion.
type customIssueHeap []model.Resource

func (h customIssueHeap) Len() int           { return len(h) }
func (h customIssueHeap) Less(i, j int) bool { return h[i].ID > h[j].ID }
func (h customIssueHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *customIssueHeap) Push(v any)        { *h = append(*h, v.(model.Resource)) }
func (h *customIssueHeap) Pop() any          { old := *h; v := old[len(old)-1]; *h = old[:len(old)-1]; return v }

func customConditionSignals(r model.Resource, now time.Time, policy model.DiagnosticPolicy) []signal {
	c, state := model.CustomReadyCondition(r.Object)
	if state != "current" || model.Str(c, "status") != "False" || !elapsed(model.Str(c, "lastTransitionTime"), now, time.Duration(policy.StartupGraceSeconds)*time.Second) {
		return nil
	}
	return []signal{{
		title: "Custom resource reports not ready", severity: "warning", priority: 40,
		evidence: []string{fmt.Sprintf("%s %s/%s (%s): controller reports Ready=False at generation %.0f; reason=%s; %s; transition age reached configured grace of %d seconds", r.Kind, r.Namespace, r.Name, r.Definition, model.Num(r.Object, "metadata", "generation"), model.Str(c, "reason"), model.Str(c, "message"), policy.StartupGraceSeconds)},
		causes:   []string{"The controller reports a current not-ready condition. Controller-specific reconciliation or dependency failures are possible; the condition alone does not establish a root cause."},
		steps:    []string{"Inspect the custom resource's projected conditions and UID-correlated events. Consult the operator's documentation and authorized controller logs before changing configuration."},
	}}
}

// customDiagnostics enriches only diagnostic consumers, never built-in counts or
// topology. Every collection goes through the existing fresh CRD/RBAC boundary.
func (s *Service) customDiagnostics(ctx context.Context, snap *model.Snapshot, requestedNS string) (model.CustomDiagnosticCoverage, error) {
	out := model.CustomDiagnosticCoverage{State: "unavailable", CandidateLimit: customIssueLimit, Message: "Only enabled custom types, unique generation-matched Ready=False conditions after the configured grace, and recent UID-matched warning events are assessed. No generic spec or root-cause analysis."}
	p, ok := s.Provider.(provider.CustomProvider)
	if !ok {
		return out, nil
	}
	ns, err := s.Scope(requestedNS)
	if err != nil {
		return out, err
	}
	catalog, err := p.CustomTypes(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		snap.Warnings = append(snap.Warnings, model.Warning{Kind: "CustomResource", Message: "Custom diagnostic discovery unavailable: " + err.Error()})
		return out, nil
	}
	if !catalog.Enabled {
		out.State = "disabled"
		return out, ctx.Err()
	}
	out.State = "available"
	for _, w := range snap.Warnings {
		if w.Kind == "Event" {
			out.State = "partial"
		}
	}
	snap.Warnings = append(snap.Warnings, catalog.Warnings...)
	if len(catalog.Warnings) > 0 {
		out.State = "partial"
	}
	types := []model.CustomType{}
	seen := map[string]bool{}
	if len(catalog.Items) > 32 {
		out.State = "partial"
		snap.Warnings = append(snap.Warnings, model.Warning{Kind: "CustomResource", Message: "Custom diagnostic catalog exceeds 32 entries; remaining types omitted."})
	}
	for _, t := range catalog.Items[:min(len(catalog.Items), 32)] {
		if !model.ValidCustomDefinition(t.Definition) || seen[t.Definition] {
			out.State = "partial"
			snap.Warnings = append(snap.Warnings, model.Warning{Kind: "CustomResource", Message: "Invalid, duplicate or over-limit custom diagnostic catalog entry omitted (maximum 32 types)."})
			continue
		}
		seen[t.Definition] = true
		types = append(types, t)
	}
	// Index events once; generic custom correlation must never fall back to name.
	warningsByUID := map[string][]model.Event{}
	for _, e := range snap.Events {
		if e.ResourceUID != "" && e.Type == "Warning" && recentEvent(e, snap.ObservedAt, time.Duration(s.diagnostics.WarningEventWindowSeconds)*time.Second) {
			warningsByUID[e.ResourceUID] = append(warningsByUID[e.ResourceUID], e)
		}
	}
	candidates := &customIssueHeap{}
	heap.Init(candidates)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, typ := range types {
		wg.Add(1)
		go func(definition string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			objects, actual, truncated, err := p.CustomList(ctx, definition, "", ns)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.State = "partial"
				snap.Warnings = append(snap.Warnings, model.Warning{Kind: definition, Message: "Custom diagnostic read unavailable: " + err.Error()})
				return
			}
			if actual.Definition != definition || actual.Definition != actual.Plural+"."+actual.Group || actual.Kind == "" || actual.PreferredVersion == "" {
				out.State = "partial"
				snap.Warnings = append(snap.Warnings, model.Warning{Kind: definition, Message: "Custom diagnostic collection identity invalid; omitted."})
				return
			}
			out.TypesInspected++
			if truncated || len(objects) > 5000 {
				out.State = "partial"
				snap.Warnings = append(snap.Warnings, model.Warning{Kind: definition, Message: "Custom diagnostic collection truncated; narrow namespace scope."})
			}
			invalid := false
			identities := map[string]bool{}
			for _, o := range objects[:min(len(objects), 5000)] {
				if ctx.Err() != nil {
					return
				}
				objectNS, name := model.Str(o, "metadata", "namespace"), model.Str(o, "metadata", "name")
				if len(validation.IsDNS1123Subdomain(name)) > 0 || (actual.Namespaced && (objectNS == "" || len(validation.IsDNS1123Label(objectNS)) > 0 || (ns != "" && objectNS != ns))) || (!actual.Namespaced && objectNS != "") || model.Str(o, "kind") != actual.Kind || model.Str(o, "apiVersion") != actual.Group+"/"+actual.PreferredVersion {
					invalid = true
					continue
				}
				// Project again as defense in depth for alternate provider implementations.
				r := model.SummarizeCustom(model.CustomProjection(o), definition)
				if identities[r.ID] {
					invalid = true
					continue
				}
				identities[r.ID] = true
				out.ResourcesObserved++
				if r.Health == "unknown" {
					out.ReadinessUnknown++
				}
				candidate := len(customConditionSignals(r, snap.ObservedAt, s.diagnostics)) > 0
				for _, e := range warningsByUID[model.Str(r.Object, "metadata", "uid")] {
					candidate = candidate || model.CustomEventMatches(e, r)
				}
				if !candidate {
					continue
				}
				if candidates.Len() < customIssueLimit {
					heap.Push(candidates, r)
				} else {
					out.CandidatesOmitted++
					if r.ID < (*candidates)[0].ID {
						(*candidates)[0] = r
						heap.Fix(candidates, 0)
					}
				}
			}
			if invalid {
				out.State = "partial"
				snap.Warnings = append(snap.Warnings, model.Warning{Kind: definition, Message: "Invalid, duplicate or out-of-scope custom diagnostic objects omitted."})
			}
		}(typ.Definition)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if out.CandidatesOmitted > 0 {
		out.State = "partial"
		snap.Warnings = append(snap.Warnings, model.Warning{Kind: "CustomResource", Message: "Custom diagnostic candidate limit of 2000 reached; some issues are omitted. Narrow namespace scope."})
	}
	sort.Slice(*candidates, func(i, j int) bool { return (*candidates)[i].ID < (*candidates)[j].ID })
	snap.Resources = append(snap.Resources, (*candidates)...)
	sort.Slice(snap.Warnings, func(i, j int) bool {
		if snap.Warnings[i].Kind != snap.Warnings[j].Kind {
			return snap.Warnings[i].Kind < snap.Warnings[j].Kind
		}
		return snap.Warnings[i].Message < snap.Warnings[j].Message
	})
	return out, nil
}
