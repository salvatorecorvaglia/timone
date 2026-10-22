package service

import (
	"context"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

func TestMaintenanceExpiresIdleHistoryAndStopsRecording(t *testing.T) {
	s := New(provider.NewDemo())
	now := time.Now()
	old := now.Add(-time.Hour + 40*time.Millisecond)
	s.observations.states["old"] = observedResource{at: old}
	s.observations.changes = []Change{{ID: "old", ObservedAt: old}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Maintain(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(time.Second)
	for {
		s.observations.mu.Lock()
		empty := len(s.observations.states) == 0 && len(s.observations.changes) == 0
		s.observations.mu.Unlock()
		if empty {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle observations did not expire")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	s.observations.observe(model.Snapshot{ObservedAt: time.Now(), Resources: []model.Resource{{ID: "late", Object: model.Object{"metadata": map[string]any{"uid": "late"}}}}})
	if len(s.observations.states) != 0 {
		t.Fatal("retired workspace recorded new history")
	}
}

func TestExpiryAndJournalCapClearDiscardedReferences(t *testing.T) {
	o := newObservations()
	now := time.Now()
	o.changes = []Change{{ID: "expired", ObservedAt: now.Add(-time.Hour)}, {ID: "current", ObservedAt: now}}
	backing := o.changes
	o.expireLocked(now)
	if len(o.changes) != 1 || o.changes[0].ID != "current" || backing[1].ID != "" {
		t.Fatal("expired backing references retained", backing)
	}
	o.changes = make([]Change, 10002)
	for i := range o.changes {
		o.changes[i] = Change{ID: "retained", ObservedAt: now}
	}
	backing = o.changes
	o.observe(model.Snapshot{ObservedAt: now})
	if len(o.changes) != 10000 || backing[10000].ID != "" || backing[10001].ID != "" {
		t.Fatal("capped journal retained discarded entries")
	}
}
