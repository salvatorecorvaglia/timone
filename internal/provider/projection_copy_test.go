package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"timone/internal/model"
)

func TestHeaderProjectionAndReaderIsolationAcrossCacheAndInformer(t *testing.T) {
	pod := fakeObject("Pod", "production", "headers", "1")
	pod.Object["spec"] = map[string]any{"containers": []any{map[string]any{"name": "app", "readinessProbe": map[string]any{"httpGet": map[string]any{"httpHeaders": []any{map[string]any{"name": "Authorization", "value": "Bearer PRIVATE-PROBE-SENTINEL"}}}}}}}
	k, _ := clientgoCluster(pod)
	startObservers(t, k, func() {})
	items, _, err := k.List(context.Background(), "Pod", "production")
	if err != nil || len(items) != 1 {
		t.Fatal("initial read", err)
	}
	assertSafe := func(o model.Object) {
		t.Helper()
		data, _ := json.Marshal(o)
		if strings.Contains(string(data), "PRIVATE-PROBE-SENTINEL") || !strings.Contains(string(data), "[REDACTED]") {
			t.Fatal("unredacted header")
		}
	}
	assertSafe(items[0])
	model.Map(items[0], "spec")["containers"] = []any{}
	items, _, err = k.List(context.Background(), "Pod", "production")
	if err != nil {
		t.Fatal(err)
	}
	assertSafe(items[0])
	eventually(t, func() bool {
		k.mu.Lock()
		defer k.mu.Unlock()
		s := k.streams["Pod:production"]
		return s != nil && s.healthy && s.informer != nil && s.informer.HasSynced()
	})
	items, _, err = k.List(context.Background(), "Pod", "production")
	if err != nil {
		t.Fatal(err)
	}
	assertSafe(items[0])
	model.Map(items[0], "spec")["containers"] = []any{}
	items, _, err = k.List(context.Background(), "Pod", "production")
	if err != nil {
		t.Fatal(err)
	}
	assertSafe(items[0])
	item, err := k.Get(context.Background(), "Pod", "production", "headers")
	if err != nil {
		t.Fatal(err)
	}
	assertSafe(item)
}
