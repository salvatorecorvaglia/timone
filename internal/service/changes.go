package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type Change struct {
	ID         string         `json:"id"`
	Resource   model.Resource `json:"resource"`
	UID        string         `json:"uid"`
	Type       string         `json:"type"`
	Evidence   string         `json:"evidence"`
	ObservedAt time.Time      `json:"observedAt"`
}
type observedResource struct {
	uid, health string
	restarts    int
	fingerprint [32]byte
	at          time.Time
}
type observations struct {
	mu      sync.Mutex
	states  map[string]observedResource
	changes []Change
	started time.Time
	wake    chan struct{}
	closed  bool
}

func newObservations() *observations {
	return &observations{states: map[string]observedResource{}, changes: []Change{}, started: time.Now().UTC(), wake: make(chan struct{}, 1)}
}
func (o *observations) observe(snap model.Snapshot) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	o.expireLocked(snap.ObservedAt)
	defer func() {
		select {
		case o.wake <- struct{}{}:
		default:
		}
	}()
	for _, r := range snap.Resources {
		uid := model.Str(r.Object, "metadata", "uid")
		if uid == "" {
			continue
		}
		// Fingerprints stay private in memory. No spec, config value or digest is
		// persisted or returned through a change record.
		data, _ := json.Marshal(model.Value(r.Object, "spec"))
		state := observedResource{uid: uid, health: r.Health, restarts: r.Restarts, fingerprint: sha256.Sum256(data), at: snap.ObservedAt}
		previous, exists := o.states[r.ID]
		typ, evidence := "Observed", "First observed by this Timone process; resource creation is not established."
		if exists {
			// Concurrent collections can complete out of order. Never roll back a newer observation.
			if !state.at.After(previous.at) {
				continue
			}
			switch {
			case previous.uid != uid:
				typ, evidence = "Replaced", "A different Kubernetes UID is now observed at this resource identity."
			case previous.fingerprint != state.fingerprint:
				typ, evidence = "ConfigurationChanged", "The readable specification changed between Timone observations; values are not retained."
			case previous.health != state.health:
				typ, evidence = "HealthChanged", "Reported health changed from "+previous.health+" to "+state.health+"."
			case state.restarts > previous.restarts:
				typ, evidence = "RestartsIncreased", fmt.Sprintf("The cumulative container restart count increased by %d over %.1f seconds between observations (%.3f restarts/minute over this observed interval).", state.restarts-previous.restarts, state.at.Sub(previous.at).Seconds(), float64(state.restarts-previous.restarts)/state.at.Sub(previous.at).Minutes())
			default:
				o.states[r.ID] = state
				continue
			}
		}
		if !exists && len(o.states) >= 20000 {
			continue
		}
		o.states[r.ID] = state
		clean := r
		clean.Object = nil
		clean.Labels = nil
		o.changes = append(o.changes, Change{ID: r.ID + ":" + snap.ObservedAt.Format(time.RFC3339Nano), Resource: clean, UID: uid, Type: typ, Evidence: evidence, ObservedAt: snap.ObservedAt})
	}
	sort.SliceStable(o.changes, func(i, j int) bool { return o.changes[i].ObservedAt.Before(o.changes[j].ObservedAt) })
	if len(o.changes) > 10000 {
		drop := len(o.changes) - 10000
		copy(o.changes, o.changes[drop:])
		clear(o.changes[10000:])
		o.changes = o.changes[:10000]
	}
}

// expireLocked removes expired references as well as visible slice entries.
func (o *observations) expireLocked(now time.Time) {
	for id, state := range o.states {
		if !state.at.Add(time.Hour).After(now) {
			delete(o.states, id)
		}
	}
	retained := o.changes[:0]
	for _, change := range o.changes {
		if change.ObservedAt.Add(time.Hour).After(now) {
			retained = append(retained, change)
		}
	}
	clear(o.changes[len(retained):])
	o.changes = retained
	if len(o.changes) == 0 {
		o.changes = nil
	}
}

// Maintain expires idle observations at their deadline and releases all history
// when the owning workspace stops. Standalone service callers retain lazy expiry.
func (s *Service) Maintain(ctx context.Context) {
	o := s.observations
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		o.mu.Lock()
		now := time.Now()
		o.expireLocked(now)
		next := now.Add(time.Hour)
		for _, state := range o.states {
			if at := state.at.Add(time.Hour); at.Before(next) {
				next = at
			}
		}
		for _, c := range o.changes {
			if at := c.ObservedAt.Add(time.Hour); at.Before(next) {
				next = at
			}
		}
		o.mu.Unlock()
		timer.Reset(time.Until(next))
		select {
		case <-ctx.Done():
			o.mu.Lock()
			o.closed = true
			o.states = nil
			clear(o.changes)
			o.changes = nil
			o.mu.Unlock()
			return
		case <-o.wake:
		case <-timer.C:
		}
	}
}

func (o *observations) related(r model.Resource, now time.Time) []Change {
	return o.relatedIndex(now)[changeIdentity{r.ID, model.Str(r.Object, "metadata", "uid")}]
}

type changeIdentity struct{ id, uid string }

// Index the bounded journal once per investigation, rather than once per issue.
func (o *observations) relatedIndex(now time.Time) map[changeIdentity][]Change {
	return o.relatedFor(now, nil)
}
func (o *observations) relatedFor(now time.Time, wanted map[changeIdentity]bool) map[changeIdentity][]Change {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := map[changeIdentity][]Change{}
	for i := len(o.changes) - 1; i >= 0; i-- {
		c := o.changes[i]
		key := changeIdentity{c.Resource.ID, c.UID}
		if wanted != nil && !wanted[key] {
			continue
		}
		if len(out[key]) < 20 && c.Type != "Observed" && !c.ObservedAt.After(now) && now.Sub(c.ObservedAt) <= 15*time.Minute {
			out[key] = append(out[key], c)
		}
	}
	return out
}
func (s *Service) Changes(ctx context.Context, q Query) (*ChangesResponse, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	if q.Phase != "" || q.Sort != "" || q.Desc {
		return nil, &provider.Error{Code: 400, Message: "Observed changes are always newest first; phase/sort/desc are unsupported"}
	}
	snap, err := s.Snapshot(ctx, q.Namespace)
	if err != nil {
		return nil, err
	}
	allowed := map[string]string{}
	for _, r := range snap.Resources {
		allowed[r.ID] = model.Str(r.Object, "metadata", "uid")
	}
	s.observations.mu.Lock()
	defer s.observations.mu.Unlock()
	items := []Change{}
	since, _ := time.Parse(time.RFC3339, q.Since)
	until, _ := time.Parse(time.RFC3339, q.Until)
	for _, c := range s.observations.changes {
		if (q.Status != "" && c.Resource.Health != q.Status) || allowed[c.Resource.ID] != c.UID || c.UID == "" || (q.Kind != "" && q.Kind != c.Resource.Kind) || (q.Resource != "" && q.Resource != c.Resource.Name) || (!since.IsZero() && c.ObservedAt.Before(since)) || (!until.IsZero() && c.ObservedAt.After(until)) || !strings.Contains(strings.ToLower(c.Type+" "+c.Resource.Name+" "+c.Evidence), strings.ToLower(q.Search)) {
			continue
		}
		items = append(items, c)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ObservedAt.Equal(items[j].ObservedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].ObservedAt.After(items[j].ObservedAt)
	})
	total := len(items)
	start := min(q.Offset, total)
	return &ChangesResponse{Items: items[start:min(start+q.Limit, total)], Total: total, Warnings: snap.Warnings, Mode: s.Provider.Mode(), StartedAt: s.observations.started, Retention: "Observed by this process only: at most one hour, 10000 changes and 20000 resource identities. Restart clears history. Sampling may miss intermediate changes; deletion is not inferred from missing reads."}, nil
}
