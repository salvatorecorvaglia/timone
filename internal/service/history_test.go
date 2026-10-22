package service

import (
	"context"
	"strings"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type historyProvider struct {
	provider.Provider
	denied  bool
	created string
}

func (historyProvider) Mode() string      { return "cluster" }
func (historyProvider) Namespace() string { return "production" }
func (p historyProvider) Get(ctx context.Context, kind, ns, name string) (model.Object, error) {
	if p.denied {
		return nil, &provider.Error{Code: 403, Message: "Kubernetes RBAC denied this read"}
	}
	o, err := p.Provider.Get(ctx, kind, ns, name)
	if p.created != "" && o != nil {
		model.Map(o, "metadata")["creationTimestamp"] = p.created
	}
	return o, err
}

type recordingHistory struct {
	calls  int
	target model.HistoryTarget
}

func (*recordingHistory) Status() model.HistoryCapability {
	return model.HistoryCapability{State: "configured", Source: "prometheus"}
}
func (h *recordingHistory) Query(ctx context.Context, target model.HistoryTarget) (model.HistoryData, error) {
	h.calls++
	h.target = target
	return model.HistoryData{State: "empty", Series: []model.HistorySeries{}, Warnings: []model.Warning{}}, ctx.Err()
}
func TestHistoryAuthorizationScopeAndInputPrecedeMonitoringQueries(t *testing.T) {
	history := &recordingHistory{}
	p := historyProvider{Provider: provider.NewDemo()}
	s := NewWithHistory(p, history)
	ctx := context.Background()
	base := model.HistoryQuery{Kind: "Pod", Namespace: "production", Name: "checkout-api-7c8d9f-m4n7q", Range: "1h"}
	for _, change := range []func(*model.HistoryQuery){func(q *model.HistoryQuery) { q.Namespace = "staging" }, func(q *model.HistoryQuery) { q.Kind = "Secret" }, func(q *model.HistoryQuery) { q.Range = "720h" }, func(q *model.HistoryQuery) { q.Name = `pod"} or up` }, func(q *model.HistoryQuery) { q.Container = "missing" }} {
		q := base
		change(&q)
		if _, err := s.MetricHistory(ctx, q); err == nil || history.calls != 0 {
			t.Fatal("invalid history queried external data", q, err)
		}
	}
	p.denied = true
	s = NewWithHistory(p, history)
	if _, err := s.MetricHistory(ctx, base); provider.Status(err) != 403 || history.calls != 0 {
		t.Fatal("external credential bypassed Kubernetes RBAC", err)
	}
	p.denied = false
	s = NewWithHistory(p, history)
	if _, err := s.MetricHistory(ctx, base); err != nil || history.calls != 1 || len(history.target.Containers) != 1 || history.target.Namespace != "production" {
		t.Fatal(history, err)
	}
}
func TestHistoryWindowsContainersAndResourceAge(t *testing.T) {
	for _, window := range []string{"1h", "6h", "24h"} {
		history := &recordingHistory{}
		s := NewWithHistory(historyProvider{Provider: provider.NewDemo()}, history)
		q := model.HistoryQuery{Kind: "Pod", Namespace: "production", Name: "checkout-api-7c8d9f-m4n7q", Container: "checkout-api", Range: window}
		out, err := s.MetricHistory(context.Background(), q)
		if err != nil || history.calls != 1 || out.End.Sub(out.Start) > 24*time.Hour || int64(out.End.Sub(out.Start).Seconds())/out.Step+1 > 241 || history.target.Container != "checkout-api" {
			t.Fatal(out, err)
		}
	}
	history := &recordingHistory{}
	s := NewWithHistory(historyProvider{Provider: provider.NewDemo(), created: time.Now().UTC().Format(time.RFC3339)}, history)
	out, err := s.MetricHistory(context.Background(), model.HistoryQuery{Kind: "Node", Name: "harbor-worker-01"})
	if err != nil || out.State != "empty" || history.calls != 0 {
		t.Fatal("queried pre-creation rate history", out, err)
	}
	s = NewWithHistory(historyProvider{Provider: provider.NewDemo(), created: "malformed"}, history)
	out, err = s.MetricHistory(context.Background(), model.HistoryQuery{Kind: "Node", Name: "harbor-worker-01"})
	if err != nil || out.State != "unavailable" || history.calls != 0 {
		t.Fatal("invalid creation timestamp allowed history", out, err)
	}
}
func TestDemoHistoryCannotAdoptExternalSourceAndDisabledIsExplicit(t *testing.T) {
	history := &recordingHistory{}
	s := NewWithHistory(provider.NewDemo(), history)
	out, err := s.MetricHistory(context.Background(), model.HistoryQuery{Kind: "Node", Name: "harbor-worker-01", Range: "6h"})
	if err != nil || out.State != "simulated" || out.Source != "simulated" || len(out.Series) != 2 || history.calls != 0 {
		t.Fatal(out, err)
	}
	gaps := 0
	for _, series := range out.Series {
		for _, point := range series.Points {
			if point.Value == nil {
				gaps++
			}
		}
	}
	if gaps == 0 {
		t.Fatal("demo history lacks unavailable sample gaps")
	}
	s = New(historyProvider{Provider: provider.NewDemo()})
	out, err = s.MetricHistory(context.Background(), model.HistoryQuery{Kind: "Node", Name: "harbor-worker-01"})
	if err != nil || out.State != "disabled" || !strings.Contains(out.Message, "configure") || len(out.Series) != 0 {
		t.Fatal(out, err)
	}
}
