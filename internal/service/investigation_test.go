package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type investigationReader struct {
	provider.Provider
	objects       []model.Object
	denied        string
	truncated     string
	events        []model.Event
	concurrent    atomic.Int32
	peak          atomic.Int32
	calls         atomic.Int32
	failContainer string
	text          string
}

func (p *investigationReader) List(ctx context.Context, k, ns string) ([]model.Object, bool, error) {
	if p.denied == k {
		return nil, false, &provider.Error{Code: 403, Message: "Read denied"}
	}
	list := []model.Object{}
	for _, o := range p.objects {
		if model.Str(o, "kind") == k && (ns == "" || model.Str(o, "metadata", "namespace") == ns) {
			list = append(list, o)
		}
	}
	return list, p.truncated == k, ctx.Err()
}
func (p *investigationReader) Get(ctx context.Context, k, ns, n string) (model.Object, error) {
	if p.denied == k {
		return nil, &provider.Error{Code: 403, Message: "Read denied"}
	}
	for _, o := range p.objects {
		if model.Str(o, "kind") == k && model.Str(o, "metadata", "namespace") == ns && model.Str(o, "metadata", "name") == n {
			return o, ctx.Err()
		}
	}
	return nil, provider.ErrNotFound
}
func (p *investigationReader) Events(ctx context.Context, _ string) ([]model.Event, error) {
	return p.events, ctx.Err()
}
func (p *investigationReader) Metrics(context.Context, string) (model.Metrics, error) {
	return model.Metrics{Items: []model.Metric{}, Available: true}, nil
}
func (p *investigationReader) LogsWithOptions(ctx context.Context, _, _, container string, _ int, _ bool, _ string) (string, error) {
	n := p.concurrent.Add(1)
	defer p.concurrent.Add(-1)
	p.calls.Add(1)
	for {
		old := p.peak.Load()
		if n <= old || p.peak.CompareAndSwap(old, n) {
			break
		}
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(time.Millisecond):
	}
	if container == p.failContainer {
		return "", &provider.Error{Code: 403, Message: "Logs denied"}
	}
	return p.text, nil
}
func investigationObject(kind, ns, name, uid string) model.Object {
	return model.Object{"apiVersion": "v1", "kind": kind, "metadata": map[string]any{"name": name, "namespace": ns, "uid": uid, "creationTimestamp": time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)}, "spec": map[string]any{}, "status": map[string]any{}}
}
func owner(o, parent model.Object) {
	model.Map(o, "metadata")["ownerReferences"] = []any{map[string]any{"kind": model.Str(parent, "kind"), "name": model.Str(parent, "metadata", "name"), "uid": model.Str(parent, "metadata", "uid"), "controller": true}}
}
func workloadFixture(n int) *investigationReader {
	root := investigationObject("Deployment", "production", "web", "deployment")
	rs := investigationObject("ReplicaSet", "production", "web-rs", "rs")
	owner(rs, root)
	p := &investigationReader{Provider: provider.NewDemo(), objects: []model.Object{root, rs}, failContainer: "never"}
	for i := 0; i < n; i++ {
		pod := investigationObject("Pod", "production", fmt.Sprintf("web-%02d", i), fmt.Sprintf("pod-%d", i))
		owner(pod, rs)
		model.Map(pod, "spec")["containers"] = []any{map[string]any{"name": "app"}}
		p.objects = append(p.objects, pod)
	}
	return p
}
func TestWorkloadLogsIdentityWindowConcurrencyAndBounds(t *testing.T) {
	p := workloadFixture(15)
	stranger := investigationObject("Pod", "production", "intruder", "intruder")
	model.Map(stranger, "spec")["containers"] = []any{map[string]any{"name": "app"}}
	// Same controller name, different UID must not join the workload.
	owner(stranger, p.objects[1])
	model.Arr(stranger, "metadata", "ownerReferences")[0].(map[string]any)["uid"] = "old-rs"
	p.objects = append(p.objects, stranger)
	end := time.Now().UTC().Truncate(time.Second)
	window := InvestigationWindow{Start: end.Add(-time.Hour), End: end}
	p.text = window.Start.Add(time.Minute).Format(time.RFC3339Nano) + " earlier\n" + end.Add(time.Minute).Format(time.RFC3339Nano) + " future\nmalformed line\n" + end.Add(-time.Minute).Format(time.RFC3339Nano) + " later\n"
	result, err := New(p).WorkloadLogs(context.Background(), "Deployment", "production", "web", "", 100, false, window)
	if err != nil || len(result.Sources) != 12 || !result.Truncated || len(result.Lines) != 24 || p.peak.Load() > 4 {
		t.Fatalf("sources=%d lines=%d peak=%d err=%v", len(result.Sources), len(result.Lines), p.peak.Load(), err)
	}
	for _, line := range result.Lines {
		if line.Pod == "intruder" || !window.contains(*line.Timestamp) {
			t.Fatal("incorrect log membership/window")
		}
	}
	if result.Lines[0].Text != "earlier" {
		t.Fatal("logs not ordered by timestamp")
	}
}
func TestWorkloadLogsPartialErrorsCancellationAndScope(t *testing.T) {
	p := workloadFixture(1)
	pod := p.objects[2]
	model.Map(pod, "spec")["containers"] = []any{map[string]any{"name": "good"}, map[string]any{"name": "bad"}}
	p.failContainer = "bad"
	end := time.Now().UTC().Truncate(time.Second)
	p.text = end.Add(-time.Minute).Format(time.RFC3339) + " usable\n"
	s := New(p)
	window := InvestigationWindow{Start: end.Add(-time.Hour), End: end}
	got, e := s.WorkloadLogs(context.Background(), "Deployment", "production", "web", "", 100, false, window)
	if e != nil || len(got.Lines) != 1 || got.Sources[0].State != "unavailable" {
		t.Fatalf("%+v %v", got, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = s.WorkloadLogs(ctx, "Deployment", "production", "web", "", 100, false, window); e == nil {
		t.Fatal("cancelled read succeeded")
	}
	if _, e = s.WorkloadLogs(context.Background(), "Deployment", "other", "web", "", 100, false, window); e == nil {
		t.Fatal("unreadable root accepted")
	}
}
func TestIncidentWindowAndExportExcludeLogsByDefault(t *testing.T) {
	p := workloadFixture(1)
	s := New(p)
	end := time.Now().UTC().Truncate(time.Second)
	window := InvestigationWindow{Start: end.Add(-time.Hour), End: end}
	pod := model.Summarize(p.objects[2])
	p.events = []model.Event{{ID: "event", ResourceID: pod.ID, ResourceUID: model.Str(pod.Object, "metadata", "uid"), Kind: "Pod", ResourceNamespace: "production", Namespace: "production", Resource: pod.Name, Timestamp: end.Add(-time.Minute).Format(time.RFC3339), Type: "Warning", Reason: "FailedMount"}}
	// Only retained evidence in the selected interval belongs in the timeline.
	got, e := s.Incident(context.Background(), "Deployment", "production", "web", window, false)
	if e != nil || got.Logs != nil || p.calls.Load() != 0 || len(got.Resources) != 3 || len(got.Events) != 1 {
		t.Fatalf("%+v %v", got, e)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "\"logs\"") {
		t.Fatal("logs included without opt-in")
	}
	for _, q := range [][2]string{{"invalid", ""}, {"2020-01-01T00:00:00Z", "2020-01-03T00:00:00Z"}, {"", "2999-01-01T00:00:00Z"}} {
		if _, e := Window(q[0], q[1], end); e == nil {
			t.Fatalf("invalid window accepted %v", q)
		}
	}
}
func TestStorageExactBindingRedactionAndAttachmentErrors(t *testing.T) {
	pvc := investigationObject("PersistentVolumeClaim", "production", "data", "claim")
	model.Map(pvc, "spec")["volumeName"] = "pv"
	model.Map(pvc, "spec")["storageClassName"] = "csi"
	pv := investigationObject("PersistentVolume", "", "pv", "volume")
	model.Map(pv, "spec")["claimRef"] = map[string]any{"name": "data", "namespace": "production", "uid": "claim"}
	model.Map(pv, "spec")["storageClassName"] = "csi"
	sc := investigationObject("StorageClass", "", "csi", "class")
	sc["parameters"] = map[string]any{"credential": "PRIVATE-PARAMETER"}
	sc["provisioner"] = "demo.csi"
	attachment := investigationObject("VolumeAttachment", "", "attach", "attachment")
	model.Map(attachment, "spec")["source"] = map[string]any{"persistentVolumeName": "pv"}
	model.Map(attachment, "status")["attachError"] = map[string]any{"message": "CSI attachment failed"}
	p := &investigationReader{Provider: provider.NewDemo(), objects: []model.Object{pvc, pv, sc, attachment}}
	result, e := New(p).Storage(context.Background(), "PersistentVolumeClaim", "production", "data")
	encoded, _ := json.Marshal(result)
	if e != nil || len(result.Items) != 4 || len(result.Edges) != 4 || strings.Contains(string(encoded), "PRIVATE-PARAMETER") || len(result.Warnings) == 0 {
		t.Fatalf("%s %v", encoded, e)
	}
	// A rebound volume must not be associated with the previous claim.
	model.Map(pv, "spec", "claimRef")["uid"] = "reused"
	result, e = New(p).Storage(context.Background(), "PersistentVolumeClaim", "production", "data")
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range result.Items {
		if item.Resource.Kind == "PersistentVolume" || item.Resource.Kind == "VolumeAttachment" {
			t.Fatal("mismatched binding exposed")
		}
	}
	p.denied = "StorageClass"
	result, e = New(p).Storage(context.Background(), "PersistentVolumeClaim", "production", "data")
	if e != nil || len(result.Warnings) == 0 {
		t.Fatal("partial read denial should remain visible")
	}
}

type windowHistory struct{ target model.HistoryTarget }

func (h *windowHistory) Status() model.HistoryCapability {
	return model.HistoryCapability{State: "available", Source: "test"}
}
func (h *windowHistory) Query(_ context.Context, target model.HistoryTarget) (model.HistoryData, error) {
	h.target = target
	return model.HistoryData{State: "available", Series: []model.HistorySeries{}, Warnings: []model.Warning{}}, nil
}
func TestIncidentUsesAbsoluteWindowForHistoryAndExplicitLogs(t *testing.T) {
	p := workloadFixture(1)
	s := New(p)
	end := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Minute)
	window := InvestigationWindow{Start: end.Add(-time.Hour), End: end}
	p.text = end.Add(-time.Minute).Format(time.RFC3339) + " incident line\n"
	history := &windowHistory{}
	s.history = history
	got, e := s.Incident(context.Background(), "Deployment", "production", "web", window, true)
	if e != nil {
		t.Fatal(e)
	}
	if !history.target.Start.Equal(window.Start) || !history.target.End.Equal(window.End) {
		t.Fatal("history did not use investigation interval")
	}
	if got.Logs == nil || len(got.Logs.Lines) != 1 || p.calls.Load() != 1 {
		t.Fatal("explicit log evidence missing")
	}
}
func TestInvestigationServicesRejectOutOfScopeNamespace(t *testing.T) {
	s := New(scoped{provider.NewDemo()})
	end := time.Now().UTC().Truncate(time.Second)
	window := InvestigationWindow{Start: end.Add(-time.Hour), End: end}
	_, e := s.WorkloadLogs(context.Background(), "Deployment", "other", "web", "", 100, false, window)
	if provider.Status(e) != 403 {
		t.Fatalf("logs scope: %v", e)
	}
	_, e = s.Storage(context.Background(), "PersistentVolumeClaim", "other", "data")
	if provider.Status(e) != 403 {
		t.Fatalf("storage scope: %v", e)
	}
	_, e = s.Incident(context.Background(), "Deployment", "other", "web", window, false)
	if provider.Status(e) != 403 {
		t.Fatalf("incident scope: %v", e)
	}
}
