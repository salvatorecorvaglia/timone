package service

import (
	"context"
	"strings"
	"testing"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

func TestDiagnosticPolicyControlsOnlyAgeGatedSignals(t *testing.T) {
	policy := model.DiagnosticPolicy{StartupGraceSeconds: 600, WarningEventWindowSeconds: 120, RestartThreshold: 10}
	for _, tc := range []struct {
		name string
		r    model.Resource
		want int
	}{
		{"pending before boundary", resourceFixture("Pod", "pending", 599*time.Second, nil, map[string]any{"phase": "Pending"}), 0},
		{"pending boundary", resourceFixture("Pod", "pending", 600*time.Second, nil, map[string]any{"phase": "Pending"}), 1},
		{"readiness before boundary", resourceFixture("Pod", "ready", time.Hour, nil, map[string]any{"phase": "Running", "conditions": []any{condition("Ready", "False", "ContainersNotReady", 599*time.Second)}}), 0},
		{"readiness boundary", resourceFixture("Pod", "ready", time.Hour, nil, map[string]any{"phase": "Running", "conditions": []any{condition("Ready", "False", "ContainersNotReady", 600*time.Second)}}), 1},
		{"scheduler grace", resourceFixture("Pod", "schedule", time.Minute, nil, map[string]any{"phase": "Pending", "conditions": []any{condition("PodScheduled", "False", "Unschedulable", time.Minute)}}), 0},
		{"workload grace", resourceFixture("Deployment", "rollout", 599*time.Second, map[string]any{"replicas": 3}, map[string]any{"availableReplicas": 1}), 0},
		{"workload boundary", resourceFixture("Deployment", "rollout", 600*time.Second, map[string]any{"replicas": 3}, map[string]any{"availableReplicas": 1}), 1},
		{"explicit failure immediate", resourceFixture("Deployment", "rollout", time.Second, map[string]any{"replicas": 1}, map[string]any{"conditions": []any{condition("Progressing", "False", "ProgressDeadlineExceeded", time.Second)}}), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issues := detectIssues(model.Snapshot{Resources: []model.Resource{tc.r}, ObservedAt: observed}, policy)
			if len(issues) != tc.want {
				t.Fatalf("want %d issues, got %+v", tc.want, issues)
			}
			for _, i := range issues {
				if i.DiagnosticPolicy != policy {
					t.Fatal("policy missing from issue", i)
				}
			}
		})
	}
	for _, status := range []string{"CrashLoopBackOff", "ImagePullBackOff"} {
		r := resourceFixture("Pod", "broken", time.Second, nil, map[string]any{"phase": "Running"})
		r.Status = status
		if got := resourceSignals(r, observed, policy); len(got) != 1 || got[0].severity != "critical" {
			t.Fatal(got)
		}
	}
	policy.StartupGraceSeconds = 0
	r := resourceFixture("Pod", "new", 0, nil, map[string]any{"phase": "Pending"})
	if len(resourceSignals(r, observed, policy)) != 1 {
		t.Fatal("zero grace did not apply")
	}
	for _, timestamp := range []string{"", "invalid", observed.Add(time.Second).Format(time.RFC3339)} {
		r.CreatedAt = timestamp
		if len(resourceSignals(r, observed, policy)) != 0 {
			t.Fatal("unverified age generated issue", timestamp)
		}
	}
}

func TestDiagnosticPolicyRestartCountAndEventWindow(t *testing.T) {
	policy := model.DiagnosticPolicy{StartupGraceSeconds: 600, WarningEventWindowSeconds: 120, RestartThreshold: 10}
	r := resourceFixture("Pod", "running", time.Hour, nil, map[string]any{"phase": "Running"})
	for _, count := range []int{9, 10, 11} {
		r.Restarts = count
		issues := detectIssues(model.Snapshot{Resources: []model.Resource{r}, ObservedAt: observed}, policy)
		if (len(issues) == 1) != (count >= 10) {
			t.Fatal(count, issues)
		}
		if len(issues) == 1 && !strings.Contains(strings.Join(issues[0].Evidence, " "), "threshold 10 reached; not a restart rate") {
			t.Fatal(issues)
		}
	}
	r.Restarts = 0
	for _, age := range []time.Duration{121 * time.Second, 120 * time.Second, 119 * time.Second, -time.Minute, -61 * time.Second} {
		e := model.Event{ResourceID: r.ID, ResourceUID: "uid-running", Type: "Warning", Reason: "Unhealthy", Timestamp: observed.Add(-age).Format(time.RFC3339)}
		issues := detectIssues(model.Snapshot{Resources: []model.Resource{r}, Events: []model.Event{e}, ObservedAt: observed}, policy)
		want := age <= 120*time.Second && age >= -time.Minute
		if (len(issues) == 1) != want {
			t.Fatal(age, issues)
		}
	}
}

func TestServiceDiagnosticPolicyIsValidatedCopiedAndShared(t *testing.T) {
	if _, err := NewWithDiagnosticPolicy(provider.NewDemo(), nil, model.DiagnosticPolicy{}); err == nil {
		t.Fatal("invalid policy accepted")
	}
	policy := model.DiagnosticPolicy{StartupGraceSeconds: 600, WarningEventWindowSeconds: 120, RestartThreshold: 10}
	pending := resourceFixture("Pod", "pending", 7*time.Minute, nil, map[string]any{"phase": "Pending"})
	model.Map(pending.Object, "metadata")["creationTimestamp"] = time.Now().Add(-7 * time.Minute).UTC().Format(time.RFC3339)
	p := observedProvider{Provider: provider.NewDemo(), objects: map[string][]model.Object{"Pod": {pending.Object}}}
	s, err := NewWithDiagnosticPolicy(p, nil, policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.StartupGraceSeconds = 0 // A caller's later mutation cannot change the service.
	ctx := context.Background()
	for _, svc := range []*Service{New(p), s} {
		list, err := svc.Issues(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		overview, err := svc.Overview(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(list.Items) != len(overview.Issues) || list.DiagnosticPolicy != overview.DiagnosticPolicy {
			t.Fatal("inconsistent issue consumers", list, overview)
		}
		want := 1
		if svc == s {
			want = 0
		}
		if len(list.Items) != want {
			t.Fatal(list)
		}
		issue, err := svc.Issue(ctx, "", "issue-"+pending.ID)
		if want == 0 {
			if provider.Status(err) != 404 {
				t.Fatal(issue, err)
			}
		} else if err != nil || issue.DiagnosticPolicy != model.DefaultDiagnosticPolicy() {
			t.Fatal(issue, err)
		}
	}
}
