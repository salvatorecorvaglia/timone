package service

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

func distributionSeries(values []float64) []model.HistorySeries {
	s := model.HistorySeries{Metric: "cpu", Unit: "cores", Container: "app", Points: []model.HistoryPoint{}}
	for i, v := range values {
		value := v
		s.Points = append(s.Points, model.HistoryPoint{Timestamp: int64(i * 60), Value: &value})
	}
	return []model.HistorySeries{s}
}
func TestEfficiencySampleDistributionAndSignals(t *testing.T) {
	values := []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}
	spec := map[string]any{"resources": map[string]any{"requests": map[string]any{"cpu": "100"}, "limits": map[string]any{"cpu": "20"}}}
	start, end := time.Unix(0, 0), time.Unix(19*60, 0)
	a := sampledUsage("cpu", distributionSeries(values), "app", spec, start, end, 60, true)
	if a.Samples != 20 || a.Coverage != 1 || *a.Median != 9.5 || *a.P95 != 18 || *a.Peak != 19 {
		t.Fatalf("%+v", a)
	}
	codes := map[string]bool{}
	for _, c := range a.Checks {
		codes[c.Code] = true
	}
	if !codes["low-request-utilization"] || !codes["near-limit"] {
		t.Fatal(a.Checks)
	}
	a = sampledUsage("cpu", distributionSeries(values), "app", spec, start, time.Unix(59*60, 0), 60, true)
	if a.Coverage >= .8 {
		t.Fatal(a)
	}
	for _, c := range a.Checks {
		if c.State == "advisory" {
			t.Fatal("partial grid emitted utilization signal", a)
		}
	}
	a = sampledUsage("cpu", distributionSeries(values), "app", spec, start, end, 60, false)
	for _, c := range a.Checks {
		if c.State == "advisory" {
			t.Fatal("unsupported allocation emitted signal", a)
		}
	}
	a = sampledUsage("cpu", distributionSeries([]float64{0, math.NaN(), math.Inf(1), -1}), "app", nil, start, time.Unix(3*60, 0), 60, true)
	if a.Samples != 1 || a.Request != nil || a.Limit != nil || a.Median == nil || *a.Median != 0 {
		t.Fatal(a)
	}
	series := distributionSeries(values)
	series = append(series, series[0])
	a = sampledUsage("cpu", series, "app", nil, start, end, 60, true)
	if a.P95 != nil || a.Samples != 0 {
		t.Fatal("duplicate series accepted", a)
	}
}

type efficiencyHistory struct {
	calls   atomic.Int32
	partial bool
}

func (*efficiencyHistory) Status() model.HistoryCapability {
	return model.HistoryCapability{State: "configured", Source: "prometheus"}
}
func (h *efficiencyHistory) Query(ctx context.Context, target model.HistoryTarget) (model.HistoryData, error) {
	h.calls.Add(1)
	out := model.HistoryData{State: "available", Series: []model.HistorySeries{}, Warnings: []model.Warning{}}
	for _, container := range target.Containers {
		for _, metric := range []string{"cpu", "memory"} {
			s := model.HistorySeries{Metric: metric, Unit: "cores", Container: container, Points: []model.HistoryPoint{}}
			if metric == "memory" {
				s.Unit = "bytes"
			}
			for stamp := target.Start.Unix(); stamp <= target.End.Unix(); stamp += target.Step {
				if h.partial && len(s.Points) >= 3 {
					break
				}
				v := .01
				if metric == "memory" {
					v = 1024
				}
				s.Points = append(s.Points, model.HistoryPoint{Timestamp: stamp, Value: &v})
			}
			out.Series = append(out.Series, s)
		}
	}
	return out, ctx.Err()
}
func TestEfficiencyUsesBoundedOwnedPodsAndAuthorization(t *testing.T) {
	p := workloadFixture(6)
	h := &efficiencyHistory{}
	s := New(p)
	s.history = h
	a, err := s.Efficiency(context.Background(), "Deployment", "production", "web", "1h")
	if err != nil || h.calls.Load() != 4 || a.AnalyzedPods != 4 || a.CurrentPods != 6 || len(a.Warnings) == 0 {
		t.Fatalf("%+v %v calls %d", a, err, h.calls.Load())
	}
	h.calls.Store(0)
	p.denied = "Pod"
	a, err = s.Efficiency(context.Background(), "Deployment", "production", "web", "1h")
	if err != nil || h.calls.Load() != 0 || len(a.Warnings) == 0 {
		t.Fatalf("unauthorized queries: %+v %v", a, err)
	}
	if _, err = s.Efficiency(context.Background(), "Deployment", "production", "web", "7d"); err == nil {
		t.Fatal("unbounded range accepted")
	}
	if _, err = New(scoped{p}).Efficiency(context.Background(), "Pod", "staging", "web", "1h"); provider.Status(err) != 403 {
		t.Fatal(err)
	}
}
func TestEfficiencyDisabledAndPartialHistoryRemainUnknown(t *testing.T) {
	p := workloadFixture(1)
	s := New(p)
	s.history = nil
	a, err := s.Efficiency(context.Background(), "Deployment", "production", "web", "24h")
	if err != nil || a.State != "disabled" || len(a.Containers) == 0 {
		t.Fatalf("%+v %v", a, err)
	}
	for _, row := range a.Containers {
		for _, usage := range row.Usage {
			if usage.Median != nil {
				t.Fatal("missing history became zero", usage)
			}
		}
	}
	s.history = &efficiencyHistory{partial: true}
	a, err = s.Efficiency(context.Background(), "Deployment", "production", "web", "24h")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range a.Containers {
		for _, usage := range row.Usage {
			for _, c := range usage.Checks {
				if c.State == "advisory" {
					t.Fatal("sparse history emitted sizing signal", usage)
				}
			}
		}
	}
}
func configurationFixture() (*investigationReader, model.Object) {
	pod := investigationObject("Pod", "production", "app", "pod")
	pod["spec"] = map[string]any{"containers": []any{map[string]any{"name": "app", "env": []any{
		map[string]any{"name": "INLINE", "value": "DO_NOT_EXPORT_LITERAL"},
		map[string]any{"name": "CONFIG", "valueFrom": map[string]any{"configMapKeyRef": map[string]any{"name": "config", "key": "present"}}},
		map[string]any{"name": "OPTIONAL", "valueFrom": map[string]any{"configMapKeyRef": map[string]any{"name": "absent", "key": "key", "optional": true}}},
		map[string]any{"name": "PASSWORD", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "credentials", "key": "password"}}},
	}, "envFrom": []any{map[string]any{"configMapRef": map[string]any{"name": "config"}}}, "volumeMounts": []any{map[string]any{"name": "settings", "subPath": "one-file"}}}}, "volumes": []any{map[string]any{"name": "settings", "projected": map[string]any{"sources": []any{map[string]any{"configMap": map[string]any{"name": "config", "items": []any{map[string]any{"key": "missing", "path": "one-file"}}}}}}}}}
	cm := investigationObject("ConfigMap", "production", "config", "config")
	cm["data"] = map[string]any{"present": "DO_NOT_EXPORT_CONFIG"}
	cm["immutable"] = true
	secret := investigationObject("Secret", "production", "credentials", "credentials")
	secret["data"] = map[string]any{"password": "DO_NOT_EXPORT_SECRET"}
	return &investigationReader{Provider: provider.NewDemo(), objects: []model.Object{pod, cm, secret}}, pod
}
func TestConfigurationReferenceAvailabilityUpdatesAndRedaction(t *testing.T) {
	p, _ := configurationFixture()
	a, err := New(p).Configuration(context.Background(), "Pod", "production", "app")
	if err != nil || len(a.References) != 5 {
		t.Fatalf("%+v %v", a, err)
	}
	states := map[string]bool{}
	subpath := false
	for _, ref := range a.References {
		states[ref.State] = true
		if ref.Consumption == "subpath-volume" {
			subpath = true
			if !strings.Contains(ref.Update, "do not receive") {
				t.Fatal(ref)
			}
		}
	}
	for _, state := range []string{"present", "optional-missing", "key-unverified", "key-missing"} {
		if !states[state] {
			t.Fatalf("missing %s %+v", state, a)
		}
	}
	if !subpath {
		t.Fatal("projected subpath was not detected")
	}
	b, _ := json.Marshal(a)
	for _, secret := range []string{"DO_NOT_EXPORT_LITERAL", "DO_NOT_EXPORT_CONFIG", "DO_NOT_EXPORT_SECRET"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("payload leak %s", secret)
		}
	}
	p.denied = "Secret"
	a, err = New(p).Configuration(context.Background(), "Pod", "production", "app")
	if err != nil || len(a.Warnings) == 0 {
		t.Fatalf("%+v %v", a, err)
	}
	for _, ref := range a.References {
		if ref.Kind == "Secret" && ref.State != "unknown" {
			t.Fatal("denied read inferred absence", ref)
		}
	}
}
func TestConfigurationTemplatesAndScope(t *testing.T) {
	p, pod := configurationFixture()
	root := investigationObject("Deployment", "production", "app", "deployment")
	root["spec"] = map[string]any{"template": map[string]any{"spec": model.Map(pod, "spec")}}
	rs := investigationObject("ReplicaSet", "production", "app-rs", "rs")
	owner(rs, root)
	owner(pod, rs)
	p.objects = append(p.objects, root, rs)
	a, err := New(p).Configuration(context.Background(), "Deployment", "production", "app")
	if err != nil || a.CurrentPods != 1 || len(a.References) != 10 {
		t.Fatalf("%+v %v", a, err)
	}
	origins := map[string]bool{}
	for _, ref := range a.References {
		origins[ref.Origin] = true
	}
	if !origins["desired-template"] || !origins["current-pod"] {
		t.Fatal(a)
	}
	if _, err = New(scoped{p}).Configuration(context.Background(), "Deployment", "staging", "app"); provider.Status(err) != 403 {
		t.Fatal(err)
	}
}

func TestConfigurationBinaryKeysAreVolumeOnly(t *testing.T) {
	p, _ := configurationFixture()
	cm := p.objects[1]
	delete(cm, "data")
	cm["binaryData"] = map[string]any{"present": "c2VjcmV0", "missing": "c2VjcmV0"}
	a, err := New(p).Configuration(context.Background(), "Pod", "production", "app")
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range a.References {
		if ref.Name == "config" && ref.Consumption == "environment-key" && ref.State != "key-missing" {
			t.Fatal("binaryData cannot supply env key", ref)
		}
		if ref.Consumption == "subpath-volume" && ref.State != "present" {
			t.Fatal("binaryData can supply projected files", ref)
		}
	}
}
func TestEfficiencyMatchingAllocatedResourcesAllowComparison(t *testing.T) {
	p := workloadFixture(1)
	for _, pod := range p.objects {
		if model.Str(pod, "kind") != "Pod" {
			continue
		}
		c := model.Arr(pod, "spec", "containers")[0].(map[string]any)
		c["resources"] = map[string]any{"requests": map[string]any{"cpu": "1", "memory": "1Gi"}}
		pod["status"] = map[string]any{"containerStatuses": []any{map[string]any{"name": model.Str(c, "name"), "allocatedResources": map[string]any{"cpu": "1", "memory": "1Gi"}}}}
	}
	s := New(p)
	s.history = &efficiencyHistory{}
	a, err := s.Efficiency(context.Background(), "Deployment", "production", "web", "1h")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range a.Containers {
		for _, u := range row.Usage {
			for _, c := range u.Checks {
				found = found || c.Code == "low-request-utilization"
			}
		}
	}
	if !found {
		t.Fatal("matching reported allocation suppressed valid comparison", a)
	}
	for _, pod := range p.objects {
		if model.Str(pod, "kind") != "Pod" {
			continue
		}
		c := model.Arr(pod, "status", "containerStatuses")[0].(map[string]any)
		model.Map(c, "allocatedResources")["cpu"] = "2"
	}
	a, err = s.Efficiency(context.Background(), "Deployment", "production", "web", "1h")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range a.Containers {
		for _, u := range row.Usage {
			for _, c := range u.Checks {
				if c.State == "advisory" {
					t.Fatal("allocation mismatch emitted comparison", u)
				}
			}
		}
	}
}
