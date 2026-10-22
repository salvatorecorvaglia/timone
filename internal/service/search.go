package service

import (
	"container/heap"
	"context"
	"sort"
	"strings"
	"sync"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
	"unicode/utf8"
)

type SearchQuery struct {
	Search     string `json:"search,omitempty" jsonschema:"At least two Unicode characters, at most 200 UTF-8 bytes; case-insensitive metadata terms must all match"`
	Namespace  string `json:"namespace,omitempty"`
	Kind       string `json:"kind,omitempty" jsonschema:"Optional exact built-in kind; mutually exclusive with definition"`
	Definition string `json:"definition,omitempty" jsonschema:"Optional administrator-enabled CRD name; mutually exclusive with kind"`
	Status     string `json:"status,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Offset     int    `json:"offset,omitempty"`
}

func (q *SearchQuery) Validate() error {
	q.Search = strings.TrimSpace(q.Search)
	if !utf8.ValidString(q.Search) || (q.Search != "" && utf8.RuneCountInString(q.Search) < 2) {
		return &provider.Error{Code: 400, Message: "Search requires at least two characters"}
	}
	if q.Definition != "" && (!model.ValidCustomDefinition(q.Definition) || q.Kind != "") {
		return &provider.Error{Code: 400, Message: "Use one supported kind or enabled custom definition"}
	}
	base := Query{Namespace: q.Namespace, Kind: q.Kind, Status: q.Status, Search: q.Search, Limit: q.Limit, Offset: q.Offset}
	if err := base.Validate(); err != nil {
		return err
	}
	q.Limit = base.Limit
	return nil
}

type searchCandidate struct {
	hit  model.SearchHit
	rank int
}

func searchLess(a, b searchCandidate) bool {
	if a.rank != b.rank {
		return a.rank < b.rank
	}
	return a.hit.ID < b.hit.ID
}

// A max heap retains only the requested prefix, independently of collection size.
type searchHeap []searchCandidate

func (h searchHeap) Len() int           { return len(h) }
func (h searchHeap) Less(i, j int) bool { return searchLess(h[j], h[i]) }
func (h searchHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *searchHeap) Push(v any)        { *h = append(*h, v.(searchCandidate)) }
func (h *searchHeap) Pop() any          { old := *h; v := old[len(old)-1]; *h = old[:len(old)-1]; return v }

// Search uses ordinary authorized collection reads, without events, manifests in
// its response, an independent persistent index, or API paths supplied by callers.
func (s *Service) Search(ctx context.Context, q SearchQuery) (model.SearchResult, error) {
	if err := q.Validate(); err != nil {
		return model.SearchResult{}, err
	}
	ns, err := s.Scope(q.Namespace)
	if err != nil {
		return model.SearchResult{}, err
	}
	result := model.SearchResult{Items: []model.SearchHit{}, Limit: q.Limit, Offset: q.Offset, Namespace: ns, State: "idle", Coverage: []model.SearchCoverage{}, Warnings: []model.Warning{}, ObservedAt: time.Now().UTC()}
	if q.Search == "" {
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.State = "complete"
	result.Complete = true
	type collection struct{ kind, definition string }
	tasks := []collection{}
	if q.Definition == "" {
		for _, k := range provider.Kinds {
			if q.Kind == "" || q.Kind == k.Name {
				tasks = append(tasks, collection{kind: k.Name})
			}
		}
	}
	cp, hasCustom := s.Provider.(provider.CustomProvider)
	if q.Definition != "" {
		tasks = append(tasks, collection{definition: q.Definition})
	} else if q.Kind == "" && hasCustom {
		catalog, e := cp.CustomTypes(ctx)
		if e != nil {
			result.Warnings = append(result.Warnings, model.Warning{Kind: "CustomResource", Message: e.Error()})
			result.Coverage = append(result.Coverage, model.SearchCoverage{Kind: "CustomResource", State: "unavailable"})
			result.Complete = false
		} else {
			result.Warnings = append(result.Warnings, catalog.Warnings...)
			for _, w := range catalog.Warnings {
				result.Coverage = append(result.Coverage, model.SearchCoverage{Kind: "CustomResource", Definition: w.Kind, State: "unavailable"})
			}
			if len(catalog.Warnings) > 0 {
				result.Complete = false
			}
			seen := map[string]bool{}
			for _, t := range catalog.Items {
				if seen[t.Definition] {
					continue
				}
				seen[t.Definition] = true
				if !model.ValidCustomDefinition(t.Definition) || len(tasks) >= len(provider.Kinds)+32 {
					result.Complete = false
					result.Warnings = append(result.Warnings, model.Warning{Kind: "CustomResource", Message: "Custom search catalog invalid or exceeds 32 types"})
					break
				}
				tasks = append(tasks, collection{kind: t.Kind, definition: t.Definition})
			}
		}
	}
	var mu sync.Mutex
	candidates := &searchHeap{}
	heap.Init(candidates)
	terms := strings.Fields(strings.ToLower(q.Search))
	consume := func(task collection, objects []model.Object, truncated bool, e error, t model.CustomType) {
		coverage := model.SearchCoverage{Kind: task.kind, Definition: task.definition, State: "available"}
		if coverage.Kind == "" {
			coverage.Kind = "CustomResource"
		}
		if t.Kind != "" {
			coverage.Kind = t.Kind
		}
		mu.Lock()
		defer mu.Unlock()
		if e != nil {
			coverage.State = "unavailable"
			result.Complete = false
			result.Coverage = append(result.Coverage, coverage)
			result.Warnings = append(result.Warnings, model.Warning{Kind: coverageLabel(coverage), Message: e.Error()})
			return
		}
		observed, matched := 0, 0
		for _, o := range objects {
			if ctx.Err() != nil {
				return
			}
			// Defense in depth against a malformed/mis-scoped provider response.
			objectNS := model.Str(o, "metadata", "namespace")
			if ns != "" && objectNS != "" && objectNS != ns {
				continue
			}
			kind := task.kind
			if t.Kind != "" {
				kind = t.Kind
			}
			if model.Str(o, "kind") != kind {
				continue
			}
			if task.definition == "" && kind == "Namespace" && ns != "" && model.Str(o, "metadata", "name") != ns {
				continue
			}
			observed++
			haystack := strings.ToLower(model.Str(o, "metadata", "name") + " " + objectNS + " " + kind + " " + task.definition)
			match := true
			for _, term := range terms {
				if !strings.Contains(haystack, term) {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			r := model.Summarize(o)
			if task.definition != "" {
				r = model.SummarizeCustom(o, task.definition)
			}
			if q.Status != "" && r.Health != q.Status {
				continue
			}
			matched++
			result.Total++
			hit := model.SearchHit{ID: r.ID, Kind: r.Kind, Name: r.Name, Namespace: r.Namespace, Health: r.Health, Status: r.Status, Definition: task.definition}
			if task.definition != "" {
				hit.Version = t.PreferredVersion
			}
			rank := 2
			name := strings.ToLower(r.Name)
			needle := strings.ToLower(q.Search)
			if name == needle {
				rank = 0
			} else if strings.HasPrefix(name, needle) {
				rank = 1
			}
			candidate := searchCandidate{hit: hit, rank: rank}
			if candidates.Len() < q.Offset+q.Limit {
				heap.Push(candidates, candidate)
			} else if searchLess(candidate, (*candidates)[0]) {
				heap.Pop(candidates)
				heap.Push(candidates, candidate)
			}
		}
		coverage.Observed = &observed
		coverage.Matched = &matched
		if truncated {
			coverage.State = "truncated"
			result.Complete = false
			result.Warnings = append(result.Warnings, model.Warning{Kind: coverageLabel(coverage), Message: "Collection truncated; match counts cover observed objects only. Narrow the namespace or type."})
		}
		result.Coverage = append(result.Coverage, coverage)
	}
	jobs := make(chan collection)
	var workers sync.WaitGroup
	for range min(4, len(tasks)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for task := range jobs {
				if ctx.Err() != nil {
					continue
				}
				if task.definition != "" {
					if !hasCustom {
						consume(task, nil, false, &provider.Error{Code: 503, Message: "Custom resource search is unavailable"}, model.CustomType{})
						continue
					}
					objects, t, truncated, e := cp.CustomList(ctx, task.definition, "", ns)
					consume(task, objects, truncated, e, t)
				} else {
					objects, truncated, e := s.Provider.List(ctx, task.kind, ns)
					consume(task, objects, truncated, e, model.CustomType{})
				}
			}
		}()
	}
	for _, task := range tasks {
		select {
		case jobs <- task:
		case <-ctx.Done():
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return model.SearchResult{}, err
	}
	sort.Slice(*candidates, func(i, j int) bool { return searchLess((*candidates)[i], (*candidates)[j]) })
	start := min(q.Offset, candidates.Len())
	for _, candidate := range (*candidates)[start:] {
		result.Items = append(result.Items, candidate.hit)
	}
	result.Truncated = !result.Complete || result.Total > q.Offset+len(result.Items)
	if !result.Complete {
		result.State = "partial"
	}
	sort.Slice(result.Coverage, func(i, j int) bool { return coverageLabel(result.Coverage[i]) < coverageLabel(result.Coverage[j]) })
	sort.Slice(result.Warnings, func(i, j int) bool {
		a, b := result.Warnings[i], result.Warnings[j]
		return a.Kind+"/"+a.Message < b.Kind+"/"+b.Message
	})
	return result, nil
}

func coverageLabel(c model.SearchCoverage) string {
	if c.Definition != "" {
		return c.Definition
	}
	return c.Kind
}
