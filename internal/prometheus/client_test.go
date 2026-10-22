package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"timone/internal/model"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func fixtureTarget() model.HistoryTarget {
	return model.HistoryTarget{HistoryQuery: model.HistoryQuery{Kind: "Pod", Namespace: "production", Name: "api-123"}, Containers: []string{"api"}, Start: time.Unix(1800000000, 0), End: time.Unix(1800000120, 0), Step: 60}
}
func payload(t model.HistoryTarget, value string) string {
	labels := map[string]string{"namespace": t.Namespace, "pod": t.Name, "container": "api", "cluster": "primary"}
	if t.Kind == "Node" {
		labels = map[string]string{"node": t.Name, "cluster": "primary"}
	}
	b, _ := json.Marshal(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{map[string]any{"metric": labels, "values": []any{[]any{t.Start.Unix(), value}, []any{t.Start.Unix() + 60, "NaN"}, []any{t.End.Unix(), "0"}}}}}})
	return string(b)
}
func TestPrometheusConfigurationRejectsUnsafeEndpoints(t *testing.T) {
	for _, options := range []Options{{URL: "file:///etc/passwd"}, {URL: "https://user:password@metrics.example"}, {URL: "https://metrics.example?query=up"}, {URL: "https://metrics.example?"}, {URL: "https://metrics.example#"}, {URL: "https://metrics.example/path%23segment"}, {URL: "https://metrics.example/#token"}, {URL: "https://metrics.example/%2e%2e/admin"}, {URL: "http://metrics.example"}, {URL: "http://localhost:9090", TokenFile: "token"}, {URL: "https://metrics.example:0"}, {TokenFile: "token"}} {
		if Validate(options) == nil {
			t.Fatalf("unsafe options accepted %+v", options)
		}
	}
	for _, options := range []Options{{}, {URL: "https://metrics.example/prometheus"}, {URL: "http://127.0.0.1:9090"}, {URL: "http://prometheus.monitoring.svc:9090", AllowHTTP: true}} {
		if err := Validate(options); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := New(Options{URL: "https://metrics.example", CAFile: filepath.Join(t.TempDir(), "missing-secret-path")}); err == nil || strings.Contains(err.Error(), "missing-secret-path") {
		t.Fatal("CA file error was not safe", err)
	}
}
func TestPrometheusFixedQueriesPreserveNullsAndCurrentResourceLabels(t *testing.T) {
	target := fixtureTarget()
	c, _ := New(Options{URL: "https://metrics.example/prometheus", Cluster: "primary"})
	c.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		expression := q.Get("query")
		if r.URL.Path != "/prometheus/api/v1/query_range" || !strings.Contains(expression, `namespace="production"`) || !strings.Contains(expression, `pod="api-123"`) || !strings.Contains(expression, `cluster="primary"`) || q.Get("step") != "60" || q.Get("timeout") != "8s" || q.Get("limit") != "65" {
			t.Error("unbounded or incorrectly scoped query", r.URL.Path, q)
		}
		return reply(200, payload(target, "1.25")), nil
	})
	out, err := c.Query(context.Background(), target)
	if err != nil || out.State != "partial" || len(out.Series) != 2 || len(out.Series[0].Points) != 3 || out.Series[0].Points[1].Value != nil || out.Series[0].Points[2].Value == nil || *out.Series[0].Points[2].Value != 0 {
		t.Fatal(out, err)
	}
}
func TestPrometheusRejectsAmbiguousForeignAndMalformedResults(t *testing.T) {
	target := fixtureTarget()
	base := payload(target, "1")
	for _, tc := range []struct {
		name, body string
		code       int
	}{
		{"foreign namespace", strings.ReplaceAll(base, "production", "other"), 200},
		{"foreign cluster", strings.ReplaceAll(base, "primary", "other"), 200},
		{"foreign container", strings.ReplaceAll(base, `"container":"api"`, `"container":"unreadable"`), 200},
		{"out of range", strings.ReplaceAll(base, "1800000000", "1799999940"), 200},
		{"off-grid", strings.ReplaceAll(base, "1800000060", "1800000061"), 200},
		{"upstream error", `{"status":"error","error":"private-marker"}`, 200},
		{"oversized", strings.Repeat("x", MaxBody+1), 200},
		{"denied", "private-marker", 403},
		{"rate limited", "private-marker", 429},
		{"redirect", "private-marker", 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := New(Options{URL: "https://metrics.example", Cluster: "primary"})
			c.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return reply(tc.code, tc.body), nil })
			out, err := c.Query(context.Background(), target)
			if err != nil || out.State != "unavailable" || len(out.Series) != 0 {
				t.Fatal(out, err)
			}
			b, _ := json.Marshal(out)
			if strings.Contains(string(b), "private-marker") {
				t.Fatal("raw upstream payload leaked")
			}
		})
	}
	var envelope map[string]any
	_ = json.Unmarshal([]byte(base), &envelope)
	data := envelope["data"].(map[string]any)
	rows := data["result"].([]any)
	data["result"] = append(rows, rows[0])
	duplicate, _ := json.Marshal(envelope)
	c, _ := New(Options{URL: "https://metrics.example"})
	c.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return reply(200, string(duplicate)), nil })
	out, err := c.Query(context.Background(), target)
	if err != nil || out.State != "unavailable" {
		t.Fatal("duplicate scrapes produced inflated history", out, err)
	}
}
func TestPrometheusPartialEmptyNodeAndContainerCoverage(t *testing.T) {
	for _, node := range []bool{false, true} {
		target := fixtureTarget()
		if node {
			target.Kind = "Node"
			target.Name = "worker-1"
			target.Namespace = ""
			target.Containers = nil
		}
		c, _ := New(Options{URL: "https://metrics.example"})
		c.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
			return reply(200, strings.ReplaceAll(payload(target, "1"), "NaN", "1.0")), nil
		})
		complete, completeErr := c.Query(context.Background(), target)
		if completeErr != nil || complete.State != "available" {
			t.Fatal("valid node/container history unavailable", complete, completeErr)
		}
		c.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			if strings.Contains(r.URL.Query().Get("query"), "cpu") {
				return reply(200, payload(target, "0.5")), nil
			}
			return reply(200, `{"status":"success","data":{"resultType":"matrix","result":[]}}`), nil
		})
		out, err := c.Query(context.Background(), target)
		if err != nil || out.State != "partial" || len(out.Series) != 1 {
			t.Fatal("missing memory became healthy coverage", out, err)
		}
		c.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
			return reply(200, `{"status":"success","data":{"resultType":"matrix","result":[]}}`), nil
		})
		out, err = c.Query(context.Background(), target)
		if err != nil || out.State != "empty" || len(out.Series) != 0 {
			t.Fatal(out, err)
		}
	}
}
func TestPrometheusCredentialRotationFailsClosedAndNeverRedirects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("fixture-first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, _ := New(Options{URL: "https://metrics.example", TokenFile: path})
	target := fixtureTarget()
	expected := "fixture-first"
	var calls atomic.Int32
	c.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+expected {
			t.Error("incorrect Prometheus credential")
		}
		res := reply(302, "private-marker")
		res.Header.Set("Location", "https://untrusted.example/credentials")
		return res, nil
	})
	out, err := c.Query(context.Background(), target)
	if err != nil || out.State != "unavailable" || calls.Load() != 2 {
		t.Fatal("redirect followed", out, err, calls.Load())
	}
	expected = "fixture-second"
	if err := os.WriteFile(path, []byte(expected), 0600); err != nil {
		t.Fatal(err)
	}
	_, _ = c.Query(context.Background(), target)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	out, err = c.Query(context.Background(), target)
	if err != nil || out.State != "unavailable" || calls.Load() != before {
		t.Fatal("missing credentials contacted endpoint", out, err)
	}
}
func TestPrometheusConcurrencyAndCancellationAreBounded(t *testing.T) {
	c, _ := New(Options{URL: "https://metrics.example"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var active, maximum atomic.Int32
	started := make(chan struct{}, 32)
	c.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = c.Query(ctx, fixtureTarget()) }()
	}
	for range 4 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("queries did not start")
		}
	}
	cancel()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled queries leaked")
	}
	if maximum.Load() > 4 || active.Load() != 0 {
		t.Fatal(fmt.Sprintf("unbounded concurrent queries: %d active %d", maximum.Load(), active.Load()))
	}
}

func TestPrometheusSeriesAndSampleLimitsRejectOversizedMatrices(t *testing.T) {
	target := fixtureTarget()
	for _, seriesLimit := range []bool{true, false} {
		var envelope map[string]any
		_ = json.Unmarshal([]byte(payload(target, "1")), &envelope)
		data := envelope["data"].(map[string]any)
		row := data["result"].([]any)[0].(map[string]any)
		if seriesLimit {
			rows := []any{}
			for range MaxSeries + 1 {
				rows = append(rows, row)
			}
			data["result"] = rows
		} else {
			points := []any{}
			for range MaxPoints + 1 {
				points = append(points, []any{target.Start.Unix(), "1"})
			}
			row["values"] = points
		}
		b, _ := json.Marshal(envelope)
		c, _ := New(Options{URL: "https://metrics.example"})
		c.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return reply(200, string(b)), nil })
		out, err := c.Query(context.Background(), target)
		if err != nil || out.State != "unavailable" || len(out.Series) != 0 {
			t.Fatal("oversized matrix escaped limits", out, err)
		}
	}
}
