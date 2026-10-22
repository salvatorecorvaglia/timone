package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

func TestObservationsOrderIdentityRetentionAndRedaction(t *testing.T) {
	o := newObservations()
	now := time.Now().UTC()
	resource := func(uid string, restarts int, secret string) model.Resource {
		return model.Resource{ID: "Pod:production:app", Kind: "Pod", Name: "app", Namespace: "production", Health: "healthy", Restarts: restarts, Object: model.Object{"metadata": map[string]any{"uid": uid}, "spec": map[string]any{"private": secret}}}
	}
	observe := func(at time.Time, r model.Resource) {
		o.observe(model.Snapshot{ObservedAt: at, Resources: []model.Resource{r}})
	}
	observe(now, resource("one", 1, "PRIVATE-CONTENT"))
	observe(now.Add(time.Minute), resource("one", 3, "PRIVATE-CONTENT"))
	observe(now.Add(30*time.Second), resource("one", 2, "PRIVATE-CONTENT"))
	if len(o.changes) != 2 || o.changes[1].Type != "RestartsIncreased" || !strings.Contains(o.changes[1].Evidence, "2.000 restarts/minute") {
		t.Fatal(o.changes)
	}
	observe(now.Add(2*time.Minute), resource("one", 3, "CHANGED-PRIVATE"))
	if o.changes[2].Type != "ConfigurationChanged" {
		t.Fatal(o.changes)
	}
	b, _ := json.Marshal(o.changes)
	if strings.Contains(string(b), "PRIVATE") {
		t.Fatal("spec leaked", string(b))
	}
	observe(now.Add(3*time.Minute), resource("two", 0, "CHANGED-PRIVATE"))
	if changes := o.related(resource("two", 0, ""), now.Add(4*time.Minute)); len(changes) != 1 || changes[0].Type != "Replaced" {
		t.Fatal(changes)
	}
	observe(now.Add(2*time.Hour), resource("two", 0, "CHANGED-PRIVATE"))
	if len(o.changes) != 1 || o.changes[0].Type != "Observed" {
		t.Fatal(o.changes)
	}
}
func TestObservationsConcurrentSnapshotsNeverRollback(t *testing.T) {
	o := newObservations()
	now := time.Now().UTC()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			o.observe(model.Snapshot{ObservedAt: now.Add(time.Duration(i) * time.Second), Resources: []model.Resource{{ID: "Pod:n:p", Restarts: i, Object: model.Object{"metadata": map[string]any{"uid": "one"}}}}})
		}(i)
	}
	wg.Wait()
	if o.states["Pod:n:p"].restarts != 99 {
		t.Fatal(o.states)
	}
}
func TestChangesFreshScopeAndValidation(t *testing.T) {
	s := New(scoped{provider.NewDemo()})
	if _, err := s.Changes(context.Background(), Query{Namespace: "staging"}); provider.Status(err) != 403 {
		t.Fatal(err)
	}
	if _, err := s.Changes(context.Background(), Query{Since: "invalid"}); provider.Status(err) != 400 {
		t.Fatal(err)
	}
	out, err := s.Changes(context.Background(), Query{Namespace: "production", Kind: "Pod", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range out.Items {
		if c.Resource.Namespace != "production" || c.Resource.Kind != "Pod" || c.Resource.Object != nil {
			t.Fatal(c)
		}
	}
}
