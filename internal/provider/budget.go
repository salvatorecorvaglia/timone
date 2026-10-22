package provider

import (
	"context"
	"sync/atomic"
)

// InvestigationBytes bounds cumulative projected collection data per request.
// This supplements retention and response limits; it is not an RSS estimate.
const InvestigationBytes = 32 << 20

type readBudgetKey struct{}
type readBudget struct{ remaining atomic.Int64 }

func WithReadBudget(ctx context.Context, bytes int64) context.Context {
	if _, ok := ctx.Value(readBudgetKey{}).(*readBudget); ok {
		return ctx
	}
	budget := &readBudget{}
	budget.remaining.Store(bytes)
	return context.WithValue(ctx, readBudgetKey{}, budget)
}
func consumeReadBudget(ctx context.Context, bytes int) error {
	if budget, ok := ctx.Value(readBudgetKey{}).(*readBudget); ok && budget.remaining.Add(-int64(bytes)) < 0 {
		return &Error{Code: 413, Message: "Investigation collection budget exceeded; narrow namespace or resource filters"}
	}
	return nil
}
