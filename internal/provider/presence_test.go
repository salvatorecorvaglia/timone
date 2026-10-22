package provider

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestDriftPresenceProbeBypassesCachedAbsenceAndRequiresReadableAPI(t *testing.T) {
	status := 404
	calls := 0
	k := fakeCluster(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("fieldSelector") != "metadata.name=app" || r.URL.Query().Get("limit") != "1" {
			t.Fatal("unbounded presence probe", r.URL)
		}
		if status != 200 {
			return response(status, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`), nil
		}
		return response(200, `{"apiVersion":"apps/v1","kind":"DeploymentList","metadata":{},"items":[]}`), nil
	})
	spec, _ := FindKind("Deployment")
	k.cache[listKey(spec, "production")] = cachedList{at: time.Now()}
	if missing, err := k.ConfirmMissing(context.Background(), "Deployment", "production", "app"); missing || Status(err) != 404 || calls != 1 {
		t.Fatal("cached absence used as proof", missing, err, calls)
	}
	status = 200
	if missing, err := k.ConfirmMissing(context.Background(), "Deployment", "production", "app"); err != nil || !missing || calls != 2 {
		t.Fatal(missing, err, calls)
	}
}
