package service

import (
	"context"
	"testing"
	"timone/internal/provider"
)

// Each worker models an independent browser tab: observations and provider
// state are shared, but cumulative read budgets remain request-local.
func BenchmarkOverviewConcurrentTabs(b *testing.B) {
	s := New(provider.NewDemo())
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx := provider.WithReadBudget(context.Background(), provider.InvestigationBytes)
			if _, err := s.Overview(ctx, ""); err != nil {
				b.Error(err)
				return
			}
		}
	})
}
