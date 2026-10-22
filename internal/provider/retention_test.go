package provider

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	"net/http"
	"strings"
	"testing"
	"time"
	"timone/internal/model"
)

func TestRetentionEvictsExpiredAndOldestCacheEntries(t *testing.T) {
	k := newCluster("test", "", &http.Client{}, nil)
	k.retentionBudget = 100
	k.cache["old"] = cachedList{bytes: 60, at: time.Now().Add(-time.Second)}
	k.cache["new"] = cachedList{bytes: 30, at: time.Now()}
	if !k.reserveCacheLocked(40) || k.cache["old"].bytes != 0 || k.cache["new"].bytes != 30 {
		t.Fatal(k.cache)
	}
	k.cache["expired"] = cachedList{bytes: 20, at: time.Now().Add(-time.Minute)}
	k.expireRetentionLocked(time.Now())
	if _, ok := k.cache["expired"]; ok {
		t.Fatal("expired collection retained")
	}
	if k.reserveCacheLocked(101) {
		t.Fatal("oversized cache admitted")
	}
}
func TestInformerGrowthFallsBackWithoutRetainingOversizedObjects(t *testing.T) {
	pod := fakeObject("Pod", "production", "app", "one")
	pod.Object["spec"] = map[string]any{"args": []any{strings.Repeat("x", 500)}}
	dc := fake.NewSimpleDynamicClient(runtime.NewScheme(), pod)
	k := newCluster("test", "production", &http.Client{}, dc)
	k.retentionBudget = 2000
	ctx := startObservers(t, k, func() {})
	if _, _, err := k.List(ctx, "Pod", "production"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return readyStream(k, "Pod:production") })
	updated := pod.DeepCopy()
	updated.SetResourceVersion("2")
	updated.Object["spec"] = map[string]any{"args": []any{strings.Repeat("x", 4000)}}
	if _, err := dc.Resource(podsGVR).Namespace("production").Update(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		k.mu.Lock()
		defer k.mu.Unlock()
		return k.streams["Pod:production"].state == "limited" && k.streams["Pod:production"].retainedBytes == 0
	})
	k.mu.Lock()
	if k.retainedBytesLocked() > k.retentionBudget || len(k.retentionWarnings) == 0 {
		t.Error("retention overflow not reported")
	}
	k.mu.Unlock()
	objects, _, err := k.List(ctx, "Pod", "production")
	if err != nil || len(objects) != 1 || model.Str(objects[0], "metadata", "name") != "app" {
		t.Fatal("bounded polling unavailable", err)
	}
}
func TestIdleInformersReleaseStores(t *testing.T) {
	pod := fakeObject("Pod", "production", "app", "one")
	dc := fake.NewSimpleDynamicClient(runtime.NewScheme(), pod)
	k := newCluster("test", "production", &http.Client{}, dc)
	ctx := startObservers(t, k, func() {})
	_, _, _ = k.List(ctx, "Pod", "production")
	eventually(t, func() bool { return readyStream(k, "Pod:production") })
	k.mu.Lock()
	s := k.streams["Pod:production"]
	informer := s.informer
	s.lastUsed = time.Now().Add(-3 * time.Minute)
	k.expireRetentionLocked(time.Now())
	k.mu.Unlock()
	eventually(t, func() bool { k.mu.Lock(); defer k.mu.Unlock(); return s.retainedBytes == 0 && !s.healthy })
	if len(informer.GetStore().List()) != 0 {
		t.Fatal("retired informer retained objects")
	}
}
