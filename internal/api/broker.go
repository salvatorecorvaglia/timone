package api

import (
	"sync"
	"sync/atomic"
	"timone/internal/model"
)

type Broker struct {
	mu       sync.Mutex
	subs     map[chan struct{}]struct{}
	revision atomic.Uint64
	changes  []model.Invalidation
}

func NewBroker() *Broker  { return &Broker{subs: map[chan struct{}]struct{}{}} }
func (b *Broker) Notify() { b.NotifyScope(model.ResourceChange{}) }
func (b *Broker) NotifyScope(scope model.ResourceChange) {
	b.mu.Lock()
	defer b.mu.Unlock()
	revision := b.revision.Add(1)
	b.changes = append(b.changes, model.Invalidation{Revision: revision, Full: scope.Kind == "", Scopes: []model.ResourceChange{scope}})
	if len(b.changes) > 256 {
		copy(b.changes, b.changes[len(b.changes)-256:])
		b.changes = b.changes[:256]
	}
	for ch := range b.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// A bounded journal preserves all scopes even when subscriber notifications
// coalesce. Falling behind or encountering a global event requires a full read.
func (b *Broker) Since(revision uint64) model.Invalidation {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := model.Invalidation{Revision: b.revision.Load()}
	if len(b.changes) > 0 && revision+1 < b.changes[0].Revision {
		out.Full = true
		return out
	}
	seen := map[model.ResourceChange]bool{}
	for _, change := range b.changes {
		if change.Revision <= revision {
			continue
		}
		if change.Full {
			out.Full = true
			out.Scopes = nil
			return out
		}
		for _, scope := range change.Scopes {
			if !seen[scope] {
				seen[scope] = true
				out.Scopes = append(out.Scopes, scope)
			}
		}
	}
	return out
}
func (b *Broker) Subscribe() (chan struct{}, func(), bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.subs) >= 128 {
		return nil, nil, false
	}
	ch := make(chan struct{}, 1)
	b.subs[ch] = struct{}{}
	return ch, func() { b.mu.Lock(); delete(b.subs, ch); b.mu.Unlock() }, true
}
