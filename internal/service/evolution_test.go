package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

func TestFocusedTopologyKeepsRootBeyondGlobalNodeCapAndFindsPath(t *testing.T) {
	p := workloadFixture(230)
	s := New(p)
	out, e := s.FocusedTopology(context.Background(), "Deployment", "production", "web", 2, "out", "Pod:production:web-00")
	if e != nil || out.Root != "Deployment:production:web" || len(out.Nodes) > 200 || !out.Truncated || out.PathState != "found" || len(out.Path) != 3 {
		t.Fatalf("nodes=%d truncated=%v path=%v err=%v", len(out.Nodes), out.Truncated, out.Path, e)
	}
	found := false
	for _, r := range out.Nodes {
		found = found || r.ID == out.Root
	}
	if !found {
		t.Fatal("root dropped")
	}
	in, e := s.FocusedTopology(context.Background(), "Pod", "production", "web-00", 2, "in", "Deployment:production:web")
	if e != nil || in.PathState != "found" {
		t.Fatalf("incoming path %v %v", in.Path, e)
	}
	if _, e = s.FocusedTopology(context.Background(), "Pod", "production", "web-00", 5, "both", ""); e == nil {
		t.Fatal("unbounded depth accepted")
	}
}
func TestCaptureExcludesValuesCommandsAndProbeCredentials(t *testing.T) {
	p := workloadFixture(1)
	pod := p.objects[2]
	model.Map(pod, "spec")["containers"] = []any{map[string]any{"name": "app", "image": "web:v2", "args": []any{"PRIVATE-ARG"}, "env": []any{map[string]any{"name": "KEY", "value": "PRIVATE-ENV"}}, "readinessProbe": map[string]any{"exec": map[string]any{"command": []any{"PRIVATE-EXEC"}}, "httpGet": map[string]any{"path": "/PRIVATE-PATH", "port": 8080, "httpHeaders": []any{map[string]any{"name": "Authorization", "value": "PRIVATE-HEADER"}}}}}}
	out, e := New(p).Capture(context.Background(), "Deployment", "production", "web")
	data, _ := json.Marshal(out)
	if e != nil || len(out.Resources) != 3 || strings.Contains(string(data), "PRIVATE-") || !strings.Contains(string(data), "web:v2") {
		t.Fatalf("%s %v", data, e)
	}
}
func archiveOptions(dir string) ArchiveOptions {
	return ArchiveOptions{Directory: dir, Retention: 24 * time.Hour, MaxBytes: 1 << 20, Interval: 10 * time.Second}
}

type archiveCluster struct {
	provider.Provider
	identity string
	scope    string
}

func (p archiveCluster) ClusterIdentity() string { return p.identity }
func (p archiveCluster) Namespace() string       { return p.scope }
func TestArchiveSurvivesRestartFiltersUIDsAndPartitions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "history")
	p := workloadFixture(1)
	s := New(archiveCluster{p, "cluster-a", "production"})
	if e := s.ConfigureArchive(archiveOptions(dir)); e != nil {
		t.Fatal(e)
	}
	pod := model.Summarize(p.objects[2])
	p.events = []model.Event{{ID: "event", ResourceID: pod.ID, ResourceUID: "pod-0", Kind: "Pod", Namespace: "production", ResourceNamespace: "production", Resource: pod.Name, Timestamp: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), Message: "PRIVATE-EVENT-MESSAGE"}}
	s.sampleArchive(context.Background())
	if s.ArchiveStatus().State != "sampling" {
		t.Fatalf("%+v", s.ArchiveStatus())
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatalf("files=%d", len(files))
	}
	data, e := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if e != nil || strings.Contains(string(data), "PRIVATE-EVENT-MESSAGE") || strings.Contains(string(data), "\"spec\"") {
		t.Fatalf("unsafe history %s %v", data, e)
	}
	info, _ := os.Stat(filepath.Join(dir, files[0].Name()))
	if info.Mode().Perm() != 0600 {
		t.Fatal("history file permissions")
	}
	restarted := New(archiveCluster{p, "cluster-a", "production"})
	if e = restarted.ConfigureArchive(archiveOptions(dir)); e != nil {
		t.Fatal(e)
	}
	end := time.Now().UTC().Truncate(time.Second).Add(time.Second)
	window := InvestigationWindow{Start: end.Add(-time.Hour), End: end}
	got, e := restarted.ArchivedHistory(context.Background(), "production", window)
	if e != nil || len(got.Changes) != 3 || len(got.Events) != 1 || len(got.Samples) != 1 {
		t.Fatalf("changes=%d events=%d samples=%d e=%v", len(got.Changes), len(got.Events), len(got.Samples), e)
	}
	incident, err := restarted.Incident(context.Background(), "Deployment", "production", "web", window, false)
	if err != nil || len(incident.ArchiveSamples) != 1 || len(incident.Events) != 1 {
		t.Fatalf("persisted incident evidence unavailable: %v", err)
	}
	other := New(archiveCluster{p, "cluster-b", "production"})
	if e = other.ConfigureArchive(archiveOptions(dir)); e != nil {
		t.Fatal(e)
	}
	isolated, e := other.ArchivedHistory(context.Background(), "production", window)
	if e != nil || len(isolated.Changes) != 0 || len(isolated.Events) != 0 {
		t.Fatal("cluster partition leak")
	}
	model.Map(p.objects[2], "metadata")["uid"] = "replacement"
	filtered, e := restarted.ArchivedHistory(context.Background(), "production", window)
	if e != nil || len(filtered.Events) != 0 {
		t.Fatal("replaced UID events exposed")
	}
	for _, c := range filtered.Changes {
		if c.Resource.Kind == "Pod" {
			t.Fatal("replaced UID changes exposed")
		}
	}
}
func TestArchiveCollectorRunsWithoutUIAndHonorsRetentionBudget(t *testing.T) {
	s := New(provider.NewDemo())
	dir := filepath.Join(t.TempDir(), "history")
	if e := s.ConfigureArchive(archiveOptions(dir)); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.CollectArchive(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for s.ArchiveStatus().LastSample == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("collector did not stop")
	}
	if s.ArchiveStatus().LastSample == nil {
		t.Fatal("no sample without UI requests")
	}
	a := s.archive
	a.mu.Lock()
	defer a.mu.Unlock()
	old := filepath.Join(dir, a.partition+"-old.observation.json")
	if e := os.WriteFile(old, []byte("{}"), 0600); e != nil {
		t.Fatal(e)
	}
	past := time.Now().Add(-48 * time.Hour)
	os.Chtimes(old, past, past)
	for i := 0; i < 3; i++ {
		if e := a.write(archiveRecord{SchemaVersion: 1, Partition: a.partition, ObservedAt: time.Now(), Changes: []Change{{Evidence: strings.Repeat("x", 500000)}}}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := os.Stat(old); !os.IsNotExist(e) {
		t.Fatal("expired record retained")
	}
	files, e := a.files()
	if e != nil {
		t.Fatal(e)
	}
	var size int64
	for _, f := range files {
		size += f.size
	}
	if size > a.options.MaxBytes {
		t.Fatal("disk budget exceeded")
	}
}
