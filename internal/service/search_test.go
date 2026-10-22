package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type searchReader struct {
	provider.Provider
	objects           map[string][]model.Object
	denied, truncated string
	calls             *[]string
	mu                *sync.Mutex
}

func (p searchReader) List(ctx context.Context, kind, ns string) ([]model.Object, bool, error) {
	if p.calls != nil {
		p.mu.Lock()
		*p.calls = append(*p.calls, kind+":"+ns)
		p.mu.Unlock()
	}
	if p.denied == kind {
		return nil, false, &provider.Error{Code: 403, Message: "Collection read denied"}
	}
	return p.objects[kind], p.truncated == kind, ctx.Err()
}
func (p searchReader) Events(context.Context, string) ([]model.Event, error) {
	panic("search must not collect events")
}
func searchObject(kind, ns, name string) model.Object {
	return model.Object{"kind": kind, "metadata": map[string]any{"name": name, "namespace": ns}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app"}}}, "status": map[string]any{"phase": "Running", "containerStatuses": []any{map[string]any{"name": "app", "ready": true}}, "conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}}
}

func TestSearchAllBuiltinsAndEnabledCustomIdentities(t *testing.T) {
	s := New(provider.NewDemo())
	for _, tc := range []struct {
		query      string
		want       int
		definition string
	}{
		{"GatewayClass", 1, ""}, {"ReferenceGrant", 1, ""}, {"orders-db", 2, "databases.platform.timone.io"}, {" production ORDERS ", 1, "databases.platform.timone.io"},
	} {
		got, err := s.Search(context.Background(), SearchQuery{Search: tc.query})
		if err != nil {
			t.Fatal(err)
		}
		if !got.Complete || got.Total != tc.want || len(got.Coverage) != 31 {
			t.Fatalf("%s %+v", tc.query, got)
		}
		for _, r := range got.Items {
			if r.Definition != tc.definition {
				t.Fatalf("wrong resource identity %+v", r)
			}
			if tc.definition != "" && (r.Version != "v1" || !strings.HasPrefix(r.ID, r.Definition+":")) {
				t.Fatalf("missing custom identity %+v", r)
			}
		}
	}
}
func TestSearchRankingPaginationHealthAndMetadataOnly(t *testing.T) {
	objects := map[string][]model.Object{"Pod": {searchObject("Pod", "production", "z-api"), searchObject("Pod", "production", "api-z"), searchObject("Pod", "production", "api"), searchObject("Pod", "staging", "api-a")}}
	objects["Pod"][0]["data"] = map[string]any{"token": "private-marker"}
	model.Map(objects["Pod"][0], "metadata")["annotations"] = map[string]any{"private": "private-marker"}
	model.Map(objects["Pod"][0], "spec")["password"] = "private-marker"
	calls := []string{}
	s := New(searchReader{Provider: provider.NewDemo(), objects: objects, calls: &calls, mu: &sync.Mutex{}})
	first, err := s.Search(context.Background(), SearchQuery{Search: "api", Kind: "Pod", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.Items[0].Name != "api" || first.Total != 4 || !first.Truncated || !first.Complete {
		t.Fatalf("first %+v", first)
	}
	second, err := s.Search(context.Background(), SearchQuery{Search: "api", Kind: "Pod", Limit: 2, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if second.Items[0].Name != "api-z" || second.Items[1].Name != "api-a" {
		t.Fatalf("stable prefix order %+v", second.Items)
	}
	last, err := s.Search(context.Background(), SearchQuery{Search: "api", Kind: "Pod", Status: "healthy", Namespace: "production", Limit: 20, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if last.Total != 3 || len(last.Items) != 1 || last.Items[0].Name != "z-api" || last.Truncated {
		t.Fatalf("last %+v", last)
	}
	b, _ := json.Marshal(last)
	if strings.Contains(string(b), "private-marker") || strings.Contains(string(b), "annotations") || strings.Contains(string(b), "manifest") {
		t.Fatal("search exposed resource payload", string(b))
	}
	private, err := s.Search(context.Background(), SearchQuery{Search: "private-marker", Kind: "Pod"})
	if err != nil || private.Total != 0 {
		t.Fatalf("payload must not be searched %+v %v", private, err)
	}
	if len(calls) != 4 {
		t.Fatalf("filtered search read unnecessary collections %v", calls)
	}
}
func TestSearchPartialCoverageDoesNotClaimAbsence(t *testing.T) {
	s := New(searchReader{Provider: provider.NewDemo(), objects: map[string][]model.Object{"Service": {searchObject("Service", "production", "api")}}, denied: "Secret", truncated: "Service"})
	got, err := s.Search(context.Background(), SearchQuery{Search: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Complete || got.State != "partial" || !got.Truncated || got.Total != 1 || len(got.Warnings) != 2 {
		t.Fatalf("partial %+v", got)
	}
	for _, c := range got.Coverage {
		if c.Kind == "Secret" && (c.State != "unavailable" || c.Observed != nil || c.Matched != nil) {
			t.Fatalf("unavailable must not imply zero %+v", c)
		}
		if c.Kind == "Service" && (c.State != "truncated" || c.Matched == nil || *c.Matched != 1) {
			t.Fatalf("truncated %+v", c)
		}
	}
}

type searchCustomReader struct {
	searchReader
	provider.CustomProvider
	catalog    model.CustomCatalog
	catalogErr error
	customErr  error
	custom     map[string][]model.Object
}

func (p searchCustomReader) CustomTypes(context.Context) (model.CustomCatalog, error) {
	return p.catalog, p.catalogErr
}
func (p searchCustomReader) CustomList(ctx context.Context, definition, version, ns string) ([]model.Object, model.CustomType, bool, error) {
	for _, t := range p.catalog.Items {
		if t.Definition == definition {
			return p.custom[definition], t, false, p.customErr
		}
	}
	return nil, model.CustomType{}, false, &provider.Error{Code: 403, Message: "Custom resource type is not enabled"}
}
func TestSearchCustomKindCollisionsFiltersAndDiscoveryFailures(t *testing.T) {
	p := searchCustomReader{searchReader: searchReader{Provider: provider.NewDemo()}, catalog: model.CustomCatalog{Enabled: true, Items: []model.CustomType{
		{Definition: "pods.ops.example", Kind: "Pod", PreferredVersion: "v1"}, {Definition: "pods.other.example", Kind: "Pod", PreferredVersion: "v1beta1"},
	}}, custom: map[string][]model.Object{}}
	for _, typ := range p.catalog.Items {
		p.custom[typ.Definition] = []model.Object{searchObject("Pod", "production", "api")}
	}
	got, err := New(p).Search(context.Background(), SearchQuery{Search: "api", Status: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 || got.Items[0].ID == got.Items[1].ID || got.Items[0].Health != "unknown" {
		t.Fatalf("custom Pod must not use builtin Pod health %+v", got)
	}
	only, err := New(p).Search(context.Background(), SearchQuery{Search: "api", Definition: "pods.ops.example"})
	if err != nil || only.Total != 1 || len(only.Coverage) != 1 {
		t.Fatalf("custom filter %+v %v", only, err)
	}
	// A custom kind named Namespace must not trigger built-in Namespace filtering.
	p.catalog.Items[0].Kind = "Namespace"
	p.custom["pods.ops.example"] = []model.Object{searchObject("Namespace", "production", "api")}
	only, err = New(p).Search(context.Background(), SearchQuery{Search: "api", Namespace: "production", Definition: "pods.ops.example"})
	if err != nil || only.Total != 1 || only.Items[0].Health != "unknown" {
		t.Fatalf("custom Namespace was confused with core Namespace %+v %v", only, err)
	}
	p.customErr = &provider.Error{Code: 403, Message: "Instance read denied"}
	denied, err := New(p).Search(context.Background(), SearchQuery{Search: "api", Definition: "pods.ops.example"})
	if err != nil || denied.Complete || denied.Total != 0 || len(denied.Warnings) != 1 {
		t.Fatalf("custom denial %+v %v", denied, err)
	}
	p.catalogErr = &provider.Error{Code: 403, Message: "CRD read denied"}
	denied, err = New(p).Search(context.Background(), SearchQuery{Search: "api"})
	if err != nil || denied.Complete || denied.Total != 0 {
		t.Fatalf("catalog denial %+v %v", denied, err)
	}
}
func TestSearchIdleValidationAndScopeBeforeReads(t *testing.T) {
	calls := []string{}
	p := searchReader{Provider: provider.NewDemo(), calls: &calls, mu: &sync.Mutex{}}
	s := New(p)
	idle, err := s.Search(context.Background(), SearchQuery{})
	if err != nil || idle.State != "idle" || len(calls) != 0 {
		t.Fatalf("idle should not read %+v %v %v", idle, err, calls)
	}
	for _, q := range []SearchQuery{{Search: "x"}, {Search: strings.Repeat("a", 201)}, {Search: "api", Limit: 201}, {Search: "api", Offset: -1}, {Search: "api", Definition: "../secret"}, {Search: "api", Kind: "Database"}, {Search: "api", Kind: "Pod", Definition: "pods.ops.example"}, {Search: "api", Namespace: "../other"}, {Search: "api", Status: "fake"}} {
		if _, err := s.Search(context.Background(), q); provider.Status(err) != 400 {
			t.Fatalf("invalid query %+v %v", q, err)
		}
	}
	scopedService := New(searchReader{Provider: scoped{provider.NewDemo()}, calls: &calls, mu: &sync.Mutex{}})
	if _, err := scopedService.Search(context.Background(), SearchQuery{Search: "api", Namespace: "staging"}); provider.Status(err) != 403 {
		t.Fatalf("namespace escaped %v", err)
	}
	if len(calls) != 0 {
		t.Fatal("invalid/out-of-scope request performed reads", calls)
	}
}

type blockingSearchReader struct {
	provider.Provider
	active, max atomic.Int32
	started     chan struct{}
	release     chan struct{}
}

func (p *blockingSearchReader) List(ctx context.Context, _, _ string) ([]model.Object, bool, error) {
	active := p.active.Add(1)
	defer p.active.Add(-1)
	for {
		max := p.max.Load()
		if active <= max || p.max.CompareAndSwap(max, active) {
			break
		}
	}
	p.started <- struct{}{}
	select {
	case <-p.release:
		return nil, false, nil
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}
func TestSearchWorkerBoundAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			p := &blockingSearchReader{Provider: provider.NewDemo(), started: make(chan struct{}, 64), release: make(chan struct{})}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := New(p).Search(ctx, SearchQuery{Search: "api"}); done <- err }()
			for range 4 {
				select {
				case <-p.started:
				case <-time.After(2 * time.Second):
					t.Fatal("workers not started")
				}
			}
			if cancelled {
				cancel()
			} else {
				close(p.release)
			}
			select {
			case err := <-done:
				if cancelled && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost %v", err)
				}
				if !cancelled && err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("search workers leaked")
			}
			if p.max.Load() != 4 || p.active.Load() != 0 {
				t.Fatalf("active=%d peak=%d", p.active.Load(), p.max.Load())
			}
		})
	}
}
func TestSearchBoundedPrefixAcrossLargeCollections(t *testing.T) {
	objects := map[string][]model.Object{}
	for i := 4999; i >= 0; i-- {
		objects["Pod"] = append(objects["Pod"], searchObject("Pod", "production", fmt.Sprintf("api-%04d", i)))
	}
	got, err := New(searchReader{Provider: provider.NewDemo(), objects: objects}).Search(context.Background(), SearchQuery{Kind: "Pod", Search: "api", Offset: 4998, Limit: 1})
	if err != nil || got.Total != 5000 || len(got.Items) != 1 || got.Items[0].Name != "api-4998" || !got.Complete || !got.Truncated {
		t.Fatalf("bounded prefix %+v %v", got, err)
	}
}
