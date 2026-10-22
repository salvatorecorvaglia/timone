package provider

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestInvestigationBudgetIsSharedAcrossConcurrentCollections(t *testing.T) {
	ctx := WithReadBudget(context.Background(), 100)
	ctx = WithReadBudget(ctx, 1000) // A nested call must never reset the allowance.
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if consumeReadBudget(ctx, 10) == nil {
				accepted.Add(1)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 10 {
		t.Fatal("concurrent reads exceeded shared budget", accepted.Load())
	}
	if Status(consumeReadBudget(ctx, 1)) != 413 {
		t.Fatal("budget overflow did not fail closed")
	}
}
func TestCachedCollectionCannotBypassInvestigationBudget(t *testing.T) {
	k, _ := clientgoCluster(fakeObject("Pod", "production", "api", "1"))
	if _, _, err := k.List(context.Background(), "Pod", "production"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.List(WithReadBudget(context.Background(), 1), "Pod", "production"); Status(err) != 413 {
		t.Fatal("cache bypassed collection budget", err)
	}
}
