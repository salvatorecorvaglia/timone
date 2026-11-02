package service

import (
	"context"
	"fmt"
	"sort"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type JobExecution struct {
	Resource             model.Resource    `json:"resource"`
	State                string            `json:"state"`
	StartTime            string            `json:"startTime"`
	CompletionTime       string            `json:"completionTime"`
	Active               *float64          `json:"active"`
	Succeeded            *float64          `json:"succeeded"`
	Failed               *float64          `json:"failed"`
	Completions          *float64          `json:"completions"`
	Parallelism          *float64          `json:"parallelism"`
	BackoffLimit         *float64          `json:"backoffLimit"`
	BackoffLimitPerIndex *float64          `json:"backoffLimitPerIndex"`
	DeadlineSeconds      *float64          `json:"deadlineSeconds"`
	CompletionMode       string            `json:"completionMode"`
	CompletedIndexes     string            `json:"completedIndexes"`
	FailedIndexes        string            `json:"failedIndexes"`
	Checks               []AssessmentCheck `json:"checks"`
}
type BatchPod struct {
	Resource model.Resource    `json:"resource"`
	Job      model.Resource    `json:"job"`
	Checks   []AssessmentCheck `json:"checks"`
}
type BatchAssessment struct {
	Resource                model.Resource    `json:"resource"`
	ObservedAt              time.Time         `json:"observedAt"`
	State                   string            `json:"state"`
	Schedule                string            `json:"schedule"`
	TimeZone                string            `json:"timeZone"`
	ConcurrencyPolicy       string            `json:"concurrencyPolicy"`
	Suspended               bool              `json:"suspended"`
	LastScheduleTime        string            `json:"lastScheduleTime"`
	LastSuccessfulTime      string            `json:"lastSuccessfulTime"`
	StartingDeadlineSeconds *float64          `json:"startingDeadlineSeconds"`
	SuccessfulHistoryLimit  *float64          `json:"successfulHistoryLimit"`
	FailedHistoryLimit      *float64          `json:"failedHistoryLimit"`
	Jobs                    []JobExecution    `json:"jobs"`
	Pods                    []BatchPod        `json:"pods"`
	Checks                  []AssessmentCheck `json:"checks"`
	Events                  []model.Event     `json:"events"`
	Issues                  []model.Issue     `json:"issues"`
	Warnings                []model.Warning   `json:"warnings"`
	Limitations             []string          `json:"limitations"`
}

func inspectJob(job model.Object, now time.Time) JobExecution {
	out := JobExecution{Resource: assessmentResource(job), State: "unknown", StartTime: model.Str(job, "status", "startTime"), CompletionTime: model.Str(job, "status", "completionTime"), Active: count(job, "status", "active"), Succeeded: count(job, "status", "succeeded"), Failed: count(job, "status", "failed"), Completions: count(job, "spec", "completions"), Parallelism: count(job, "spec", "parallelism"), BackoffLimit: count(job, "spec", "backoffLimit"), BackoffLimitPerIndex: count(job, "spec", "backoffLimitPerIndex"), DeadlineSeconds: count(job, "spec", "activeDeadlineSeconds"), CompletionMode: model.Str(job, "spec", "completionMode"), CompletedIndexes: model.Str(job, "status", "completedIndexes"), FailedIndexes: model.Str(job, "status", "failedIndexes"), Checks: []AssessmentCheck{}}
	if len(out.CompletedIndexes) > 2048 {
		out.CompletedIndexes = out.CompletedIndexes[:2048]
		out.Checks = append(out.Checks, check("indexes", "unknown", "Completed index list was truncated"))
	}
	if len(out.FailedIndexes) > 2048 {
		out.FailedIndexes = out.FailedIndexes[:2048]
		out.Checks = append(out.Checks, check("indexes", "unknown", "Failed index list was truncated"))
	}
	complete, failed, failureTarget, successTarget, suspended := false, false, false, false, false
	for _, raw := range model.Arr(job, "status", "conditions") {
		c, _ := raw.(map[string]any)
		typ, status := model.Str(c, "type"), model.Str(c, "status")
		if model.Value(c, "observedGeneration") != nil && model.Num(c, "observedGeneration") < model.Num(job, "metadata", "generation") {
			out.Checks = append(out.Checks, check("job-condition", "unknown", typ+": condition refers to a previous generation"))
			continue
		}
		out.Checks = append(out.Checks, check("job-condition", "observed", typ+"="+status+": "+model.Str(c, "reason")+"; "+model.Str(c, "message")))
		if status != "True" {
			continue
		}
		switch typ {
		case "Complete":
			complete = true
		case "Failed":
			failed = true
		case "FailureTarget":
			failureTarget = true
		case "SuccessCriteriaMet":
			successTarget = true
		case "Suspended":
			suspended = true
		}
	}
	switch {
	case complete && failed:
		out.State = "conflicting-terminal-conditions"
	case failed:
		out.State = "failed"
	case complete:
		out.State = "complete"
	case failureTarget:
		out.State = "failure-target-pending-termination"
	case successTarget:
		out.State = "success-target-pending-termination"
	case model.Bool(job, "spec", "suspend"):
		out.State = "suspended"
	case suspended:
		out.State = "suspended-reported-awaiting-resume"
	case out.Active != nil && *out.Active > 0:
		out.State = "active"
	case out.Failed != nil && *out.Failed > 0:
		out.State = "attempt-failures-observed"
	case out.StartTime != "":
		out.State = "started-without-terminal-condition"
	}
	if out.Failed != nil && *out.Failed > 0 {
		out.Checks = append(out.Checks, check("attempts", "advisory", fmt.Sprintf("%.0f failed Pods reported; this is not a count of remaining retries or proof of Job failure", *out.Failed)))
	}
	if model.Value(job, "spec", "podFailurePolicy") != nil {
		out.Checks = append(out.Checks, check("pod-failure-policy", "advisory", "Pod failure policy is configured; individual failed Pods may be ignored or counted differently"))
	}
	if model.Value(job, "spec", "successPolicy") != nil {
		out.Checks = append(out.Checks, check("success-policy", "advisory", "Success policy is configured; use controller conditions rather than deriving completion from counts alone"))
	}
	if managedBy := model.Str(job, "spec", "managedBy"); managedBy != "" && managedBy != "kubernetes.io/job-controller" {
		out.Checks = append(out.Checks, check("external-controller", "unknown", "This Job is managed by "+managedBy+"; default Job controller behavior is not assumed"))
	}
	if out.DeadlineSeconds != nil && !model.Bool(job, "spec", "suspend") && !suspended && !complete && !failed {
		start, e := time.Parse(time.RFC3339Nano, out.StartTime)
		if e == nil && !start.After(now) && now.Sub(start).Seconds() >= *out.DeadlineSeconds {
			out.Checks = append(out.Checks, check("active-deadline", "advisory", "Wall time since the reported start reached activeDeadlineSeconds; a terminal controller failure condition is not established by this calculation"))
		}
	}
	return out
}
func (s *Service) Batch(ctx context.Context, kind, ns, name string) (BatchAssessment, error) {
	ctx = provider.WithReadBudget(ctx, provider.InvestigationBytes)
	out := BatchAssessment{ObservedAt: time.Now().UTC(), State: "unknown", Jobs: []JobExecution{}, Pods: []BatchPod{}, Checks: []AssessmentCheck{}, Events: []model.Event{}, Issues: []model.Issue{}, Warnings: []model.Warning{}, Limitations: []string{
		"Retained, currently readable Job/Pod identities only. TTL cleanup and history limits may remove executions; missing history does not establish that a schedule was missed or never ran.",
		"Cron schedules, controller-local time zones, missed-run counts and next execution times are not reconstructed. Controller timestamps and warning events are shown as evidence.",
		"Failed Pod attempts, container restarts and Job terminal failure are different signals. Counters alone do not establish completion, remaining retries or the current health of a CronJob.",
		"Job conditions without observedGeneration are controller reports with unverified generation freshness. FailureTarget/SuccessCriteriaMet are not terminal Failed/Complete conditions.",
	}}
	if kind != "Job" && kind != "CronJob" {
		return out, &provider.Error{Code: 400, Message: "Batch analysis supports Job and CronJob"}
	}
	if e := ValidateResource(kind, ns, name); e != nil {
		return out, e
	}
	if _, e := s.Scope(ns); e != nil {
		return out, e
	}
	root, e := s.Provider.Get(ctx, kind, ns, name)
	if e != nil {
		return out, e
	}
	out.Resource = assessmentResource(root)
	jobs := []model.Object{}
	jobsComplete := true
	if kind == "Job" {
		jobs = append(jobs, root)
	} else {
		out.Schedule = model.Str(root, "spec", "schedule")
		out.TimeZone = model.Str(root, "spec", "timeZone")
		if out.TimeZone == "" {
			out.TimeZone = "Controller-local time zone (not reported)"
		}
		out.ConcurrencyPolicy = defaultString(model.Map(root, "spec"), "concurrencyPolicy", "Allow")
		out.Suspended = model.Bool(root, "spec", "suspend")
		out.LastScheduleTime = model.Str(root, "status", "lastScheduleTime")
		out.LastSuccessfulTime = model.Str(root, "status", "lastSuccessfulTime")
		out.StartingDeadlineSeconds = count(root, "spec", "startingDeadlineSeconds")
		out.SuccessfulHistoryLimit = count(root, "spec", "successfulJobsHistoryLimit")
		out.FailedHistoryLimit = count(root, "spec", "failedJobsHistoryLimit")
		inventory, complete := s.assessmentList(ctx, "Job", ns, 5000, &out.Warnings)
		jobsComplete = complete
		for _, job := range inventory {
			if controlledBy(job, root) {
				jobs = append(jobs, job)
			}
		}
		sort.Slice(jobs, func(i, j int) bool {
			return model.Str(jobs[i], "metadata", "creationTimestamp") > model.Str(jobs[j], "metadata", "creationTimestamp")
		})
		if len(jobs) > 100 {
			jobs = jobs[:100]
			jobsComplete = false
			out.Warnings = append(out.Warnings, model.Warning{Kind: "Job", Message: "CronJob analysis is limited to 100 most recently created owned Jobs"})
		}
		out.State = "idle-reported"
		if !jobsComplete {
			out.State = "unknown"
		}
		if out.Suspended {
			out.State = "suspended"
		}
	}
	resources := []model.Resource{}
	if kind == "CronJob" {
		resources = append(resources, model.Summarize(root))
	}
	allResources := []model.Resource{model.Summarize(root)}
	activeJobs := 0
	identities := map[string]model.Object{}
	terminal := map[string]bool{}
	for _, job := range jobs {
		inspected := inspectJob(job, out.ObservedAt)
		out.Jobs = append(out.Jobs, inspected)
		uid := model.Str(job, "metadata", "uid")
		identities[model.Str(job, "metadata", "name")] = job
		terminal[uid] = inspected.State == "complete" || inspected.State == "failed"
		if inspected.Active != nil && *inspected.Active > 0 && !terminal[uid] {
			activeJobs++
		}
		r := model.Summarize(job)
		allResources = append(allResources, r)
		if kind == "Job" || !terminal[uid] {
			resources = append(resources, r)
		}
		if kind == "Job" {
			out.State = inspected.State
		}
	}
	if kind == "CronJob" {
		if !out.Suspended && activeJobs > 0 {
			out.State = "active"
		}
		for _, raw := range model.Arr(root, "status", "active") {
			ref, _ := raw.(map[string]any)
			job := identities[model.Str(ref, "name")]
			if defaultString(ref, "namespace", ns) != ns || model.Str(ref, "uid") == "" || model.Str(ref, "uid") != model.Str(job, "metadata", "uid") {
				out.Checks = append(out.Checks, check("active-reference", "unknown", "A reported active Job identity is absent, replaced or unreadable"))
				continue
			}
			out.Checks = append(out.Checks, check("active-reference", "observed", "Current controller reports active Job "+model.Str(ref, "name")))
		}
		if !out.Suspended && activeJobs == 0 && len(model.Arr(root, "status", "active")) > 0 {
			out.State = "active-status-unverified"
		}
		if out.Suspended {
			out.Checks = append(out.Checks, check("suspension", "advisory", "New CronJob scheduling is suspended; already started Jobs may continue"))
		}
		if out.ConcurrencyPolicy == "Forbid" && activeJobs > 0 {
			out.Checks = append(out.Checks, check("concurrency", "advisory", "Forbid can skip a trigger while a previous owned Job is active; no missed-run count is inferred"))
		}
		if out.ConcurrencyPolicy == "Replace" && activeJobs > 0 {
			out.Checks = append(out.Checks, check("concurrency", "advisory", "Replace can replace an active owned Job at a subsequent trigger; a replacement is not assumed to have happened"))
		}
		if len(jobs) == 0 {
			out.Checks = append(out.Checks, check("history", "unknown", "No readable owned Jobs are retained; past execution and success are not established"))
		}
	}
	podsLimited := false
	pods, _ := s.assessmentList(ctx, "Pod", ns, 5000, &out.Warnings)
	for _, pod := range pods {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		for _, raw := range model.Arr(pod, "metadata", "ownerReferences") {
			ref, _ := raw.(map[string]any)
			if model.Str(ref, "kind") != "Job" {
				continue
			}
			job := identities[model.Str(ref, "name")]
			if job == nil || !controlledBy(pod, job) {
				continue
			}
			if len(out.Pods) >= 200 {
				if !podsLimited {
					out.Warnings = append(out.Warnings, model.Warning{Kind: "Pod", Message: "Batch Pod evidence is limited to 200 current identities"})
					podsLimited = true
				}
				break
			}
			row := BatchPod{Resource: assessmentResource(pod), Job: assessmentResource(job), Checks: []AssessmentCheck{}}
			if row.Resource.Restarts > 0 {
				row.Checks = append(row.Checks, check("restarts", "observed", fmt.Sprintf("%d cumulative container restarts; separate from Job failed-Pod counts", row.Resource.Restarts)))
			}
			if model.Str(pod, "status", "phase") == "Failed" {
				row.Checks = append(row.Checks, check("attempt", "advisory", "A Pod attempt failed; inspect Job conditions and policies before concluding the Job failed"))
			}
			out.Pods = append(out.Pods, row)
			allResources = append(allResources, model.Summarize(pod))
			if !terminal[model.Str(job, "metadata", "uid")] && model.Str(pod, "status", "phase") != "Failed" && model.Str(pod, "status", "phase") != "Succeeded" {
				resources = append(resources, model.Summarize(pod))
			}
			break
		}
	}
	events, err := s.Provider.Events(ctx, ns)
	if err != nil {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Event", Message: err.Error()})
	}
	index := model.IndexEvents(events)
	seen := map[string]bool{}
	for _, r := range allResources {
		for _, event := range index.Related(r) {
			if !seen[event.ID] {
				seen[event.ID] = true
				out.Events = append(out.Events, event)
			}
		}
	}
	sort.Slice(out.Events, func(i, j int) bool { return model.NewerEvent(out.Events[i], out.Events[j]) })
	if len(out.Events) > 200 {
		out.Events = out.Events[:200]
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Event", Message: "Batch events limited to 200"})
	}
	currentEvents := []model.Event{}
	currentIndex := resourceIndex(resources)
	for _, event := range events {
		r, ok := currentIndex[event.ResourceID]
		if ok && model.EventMatches(event, r) {
			currentEvents = append(currentEvents, event)
		}
	}
	out.Issues = s.detectIssues(model.Snapshot{Resources: resources, Events: currentEvents, ObservedAt: out.ObservedAt})
	return out, ctx.Err()
}
