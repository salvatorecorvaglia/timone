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

func TestExplicitLogSourcesCanSelectBeyondDefaultAndRejectForeignUID(t *testing.T) {
	p := workloadFixture(20)
	end := time.Now().UTC().Truncate(time.Second)
	p.text = end.Add(-time.Minute).Format(time.RFC3339) + " selected output\n"
	s := New(p)
	catalog, e := s.LogSources(context.Background(), "Deployment", "production", "web", "")
	if e != nil || len(catalog.Sources) != 20 || p.calls.Load() != 0 {
		t.Fatalf("catalog=%d logReads=%d error=%v", len(catalog.Sources), p.calls.Load(), e)
	}
	last := catalog.Sources[19]
	got, e := s.WorkloadLogs(context.Background(), "Deployment", "production", "web", "", 100, false, InvestigationWindow{Start: end.Add(-time.Hour), End: end}, last)
	if e != nil || len(got.Sources) != 1 || got.Sources[0].Pod != "web-19" || p.calls.Load() != 1 {
		t.Fatalf("sources=%+v error=%v", got.Sources, e)
	}
	p.calls.Store(0)
	invalid := last
	invalid.UID = "stale"
	_, e = s.WorkloadLogs(context.Background(), "Deployment", "production", "web", "", 100, false, got.Window, invalid)
	if provider.Status(e) != 409 || p.calls.Load() != 0 {
		t.Fatal("stale UID selected or log request issued before validation")
	}
	invalid.Pod = "unrelated"
	_, e = s.WorkloadLogs(context.Background(), "Deployment", "production", "web", "", 100, false, got.Window, invalid)
	if provider.Status(e) != 409 || p.calls.Load() != 0 {
		t.Fatal("foreign source was queried")
	}
	_, e = s.WorkloadLogs(context.Background(), "Deployment", "production", "web", "", 100, false, got.Window, last, last)
	if provider.Status(e) != 400 {
		t.Fatal("duplicate source accepted")
	}
	_, e = s.WorkloadLogs(context.Background(), "Deployment", "production", "web", "app", 100, false, got.Window, last)
	if provider.Status(e) != 400 {
		t.Fatal("ambiguous source filter accepted")
	}
}
func TestRunbooksAttachStableRulesToGroupedWorkload(t *testing.T) {
	p := workloadFixture(1)
	model.Map(p.objects[2], "status")["phase"] = "Running"
	model.Map(p.objects[2], "status")["containerStatuses"] = []any{map[string]any{"name": "app", "ready": false, "state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}}}}
	s := New(p)
	s.ConfigureRunbooks([]Runbook{
		{ID: "matching", Title: "Investigate crashes", URL: "https://docs.example/runbook", Rule: "container-failure", Namespace: "production", Kind: "Deployment", Name: "web"},
		{ID: "wrong-namespace", Title: "Wrong namespace", URL: "https://docs.example/other", Rule: "container-failure", Namespace: "other"},
		{ID: "wrong-rule", Title: "Wrong rule", URL: "https://docs.example/other", Rule: "image-pull"},
	})
	issues, e := s.Issues(context.Background(), "production")
	if e != nil || len(issues.Items) != 1 {
		t.Fatalf("%+v %v", issues, e)
	}
	issue := issues.Items[0]
	if len(issue.Runbooks) != 1 || issue.Runbooks[0].ID != "matching" || !strings.Contains(strings.Join(issue.RuleIDs, ","), "container-failure") || issue.Resource.Kind != "Deployment" {
		t.Fatalf("%+v", issue)
	}
}
func TestRunbookFileValidation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "runbooks.json")
	valid := []Runbook{{ID: "crashes", Title: "Crash investigation", URL: "https://docs.example/crashes#logs", Rule: "container-failure"}}
	data, _ := json.Marshal(valid)
	os.WriteFile(file, data, 0600)
	if entries, e := LoadRunbooks(file); e != nil || len(entries) != 1 {
		t.Fatal(e)
	}
	for _, raw := range []string{
		"null",
		`[{"id":"bad","title":"Bad","url":"javascript:alert(1)","rule":"container-failure"}]`,
		`[{"id":"bad","title":"Bad","url":"https://user:pass@docs.example/","rule":"container-failure"}]`,
		`[{"id":"bad","title":"Bad","url":"https://docs.example/","rule":"unknown-rule"}]`,
		`[{"id":"bad","title":"Bad","url":"https://docs.example/"}]`,
		`[{"id":"bad","title":"Bad","url":"https://docs.example/","rule":"container-failure","unknown":true}]`,
	} {
		os.WriteFile(file, []byte(raw), 0600)
		if _, e := LoadRunbooks(file); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
