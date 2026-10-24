package service

import (
	"fmt"
	"slices"
	"sort"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

// NewWithDiagnosticPolicy validates configuration before the service is exposed.
// A value copy prevents callers from changing policy during concurrent requests.
func NewWithDiagnosticPolicy(p provider.Provider, source HistorySource, policy model.DiagnosticPolicy) (*Service, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	s := NewWithHistory(p, source)
	s.diagnostics = policy
	return s, nil
}

type signal struct {
	title, severity         string
	evidence, causes, steps []string
	priority                int
}

func elapsed(since string, now time.Time, threshold time.Duration) bool {
	t, err := time.Parse(time.RFC3339, since)
	return err == nil && !t.After(now) && now.Sub(t) >= threshold
}
func recentEvent(e model.Event, now time.Time, window time.Duration) bool {
	t, err := time.Parse(time.RFC3339, e.Timestamp)
	return err == nil && !t.After(now.Add(time.Minute)) && now.Sub(t) <= window
}
func resourceSignals(r model.Resource, now time.Time, policy model.DiagnosticPolicy) []signal {
	if r.Definition != "" {
		return customConditionSignals(r, now, policy)
	}
	startupGrace := time.Duration(policy.StartupGraceSeconds) * time.Second
	out := []signal{}
	add := func(title, severity, fact, cause, step string, priority int) {
		out = append(out, signal{title, severity, []string{fact}, []string{cause}, []string{step}, priority})
	}
	// Healthy resources need no formatted evidence. Compute it only for a signal.
	fact := func() string {
		return fmt.Sprintf("%s %s: status=%s, ready=%s, restarts=%d", r.Kind, r.Name, r.Status, r.Ready, r.Restarts)
	}
	switch r.Kind {
	case "PersistentVolumeClaim":
		if r.Status == "Lost" {
			add("Persistent volume claim lost its volume", "critical", fact(), "The claim reports that its previously bound PersistentVolume is lost.", "Inspect the claim and volume events, storage backend and recovery procedures before attempting any changes.", 100)
		}
	case "Pod":
		if model.Str(r.Object, "status", "phase") == "Succeeded" {
			return out
		}
		switch r.Status {
		case "CrashLoopBackOff", "Error", "Failed", "OOMKilled", "CreateContainerConfigError", "RunContainerError", "CreateContainerError", "InvalidImageName":
			add("Container repeatedly failing", "critical", fact(), "Application startup failure, invalid configuration, or resource pressure are possible; inspect logs to distinguish them.", "Read container logs, last termination state, and recent image/configuration changes.", 90)
		case "ImagePullBackOff", "ErrImagePull":
			add("Container image cannot be pulled", "critical", fact(), "The image may not exist, registry credentials may be invalid, or the registry may be unreachable.", "Check the image reference, imagePullSecrets, and the exact registry response in events.", 95)
		case "Pending", "ContainerCreating", "PodInitializing":
			if elapsed(r.CreatedAt, now, startupGrace) {
				add("Pod waiting to start", "warning", fmt.Sprintf("%s; configured startup grace of %d seconds reached", fact(), policy.StartupGraceSeconds), "Scheduling constraints or pending configuration/volume provisioning may prevent startup.", "Inspect scheduling and volume events; compare requests with node allocatable capacity.", 40)
			}
		default:
			if r.Restarts >= policy.RestartThreshold {
				add("Repeated container restarts", "warning", fmt.Sprintf("%s; configured cumulative restart threshold %d reached; not a restart rate", fact(), policy.RestartThreshold), "Container exits or probe failures may explain accumulated restarts.", "Inspect last termination state and timestamps; a cumulative counter alone does not establish an ongoing failure.", 30)
			}
		}
	case "Deployment", "StatefulSet", "DaemonSet":
		if r.Health == "warning" && elapsed(r.CreatedAt, now, startupGrace) {
			add("Workload replicas unavailable", "warning", fact(), "Child Pod failures or an incomplete rollout may explain unavailable replicas.", "Inspect child Pods, workload conditions and rollout events.", 20)
		}
	case "Node":
		if r.Status == "NotReady" {
			add("Node is not ready", "critical", fact(), "Kubelet connectivity or node infrastructure may be impaired.", "Inspect Node Ready conditions and infrastructure connectivity.", 100)
		}
	}
	generation := model.Num(r.Object, "metadata", "generation")
	workloadStale := model.Value(r.Object, "status", "observedGeneration") != nil && model.Num(r.Object, "status", "observedGeneration") < generation
	for _, v := range model.Arr(r.Object, "status", "conditions") {
		c, _ := v.(map[string]any)
		if workloadStale || (model.Value(c, "observedGeneration") != nil && model.Num(c, "observedGeneration") < generation) {
			continue
		}
		typ, status, reason := model.Str(c, "type"), model.Str(c, "status"), model.Str(c, "reason")
		detail := func() string {
			return fmt.Sprintf("%s %s: %s=%s, reason=%s; %s", r.Kind, r.Name, typ, status, reason, model.Str(c, "message"))
		}
		switch {
		case r.Kind == "Pod" && typ == "PodScheduled" && status == "False" && reason == "Unschedulable" && elapsed(model.Str(c, "lastTransitionTime"), now, startupGrace):
			add("Pod cannot be scheduled", "warning", detail(), "Node capacity, affinity, taints or volume constraints may prevent scheduling.", "Use the scheduler condition and events to identify the unsatisfied constraint before changing requests.", 55)
		case r.Kind == "Pod" && (typ == "Ready" || typ == "ContainersReady") && status == "False" && elapsed(model.Str(c, "lastTransitionTime"), now, startupGrace):
			add("Pod readiness condition failing", "warning", detail(), "Readiness probes, startup dependencies or readiness gates may be preventing readiness.", "Inspect individual container readiness, probe events and readiness gates.", 45)
		case r.Kind == "Deployment" && typ == "Progressing" && status == "False" && reason == "ProgressDeadlineExceeded":
			add("Deployment exceeded progress deadline", "critical", detail(), "The rollout has not progressed within its configured deadline; underlying Pod failures need investigation.", "Inspect rollout conditions and child Pod diagnostics before considering a rollback.", 85)
		case r.Kind == "Node" && status == "True" && (typ == "MemoryPressure" || typ == "DiskPressure" || typ == "PIDPressure" || typ == "NetworkUnavailable"):
			add("Node reports "+typ, "warning", detail(), "The node reports an adverse condition; capacity or connectivity may be impaired.", "Inspect node capacity, eviction signals and infrastructure health.", 60)
		case r.Kind == "Job" && status == "True" && (typ == "Failed" || typ == "FailureTarget"):
			add("Job failure reported", "critical", detail(), "The Job controller reports a failure condition; failed Pod attempts alone do not establish Job failure.", "Inspect Job conditions, backoff/deadline settings and failed Pod logs.", 80)
		}
	}
	if r.Kind == "Pod" && len(out) > 0 {
		for _, field := range []string{"containerStatuses", "initContainerStatuses"} {
			for _, raw := range model.Arr(r.Object, "status", field) {
				c, _ := raw.(map[string]any)
				for _, state := range []string{"waiting", "terminated"} {
					if reason := model.Str(c, "state", state, "reason"); reason != "" && reason != "Completed" {
						out[0].evidence = append(out[0].evidence, fmt.Sprintf("Container %s: %s (%s)", model.Str(c, "name"), reason, field))
					}
				}
			}
		}
	}
	return out
}

// DetectIssues preserves the default policy for callers with a standalone snapshot.
func DetectIssues(snap model.Snapshot) []model.Issue {
	return detectIssues(snap, model.DefaultDiagnosticPolicy())
}

func detectIssues(snap model.Snapshot, policy model.DiagnosticPolicy) []model.Issue {
	eventIndex := model.IndexEvents(snap.Events)
	byID := map[string]model.Resource{}
	for _, r := range snap.Resources {
		byID[r.ID] = r
	}
	owner := func(r model.Resource) model.Resource {
		if r.Definition != "" {
			return r
		}
		var visited [8]string
		for depth := 0; depth < len(visited) && !slices.Contains(visited[:depth], r.ID); depth++ {
			visited[depth] = r.ID
			var parent model.Resource
			for _, v := range model.Arr(r.Object, "metadata", "ownerReferences") {
				ref, _ := v.(map[string]any)
				if !model.Bool(ref, "controller") {
					continue
				}
				p, ok := byID[model.ID(model.Str(ref, "kind"), r.Namespace, model.Str(ref, "name"))]
				if ok && (model.Str(ref, "uid") == "" || model.Str(ref, "uid") == model.Str(p.Object, "metadata", "uid")) {
					parent = p
					break
				}
			}
			if parent.ID == "" {
				break
			}
			r = parent
		}
		return r
	}
	groups := map[string]*model.Issue{}
	priorities := map[string]int{}
	// All resources contribute their own signals and events to their verified controller group.
	for _, r := range snap.Resources {
		signals := resourceSignals(r, snap.ObservedAt, policy)
		lastObserved := ""
		if len(signals) > 0 {
			lastObserved = snap.ObservedAt.Format(time.RFC3339)
		}
		for _, ev := range eventIndex.Related(r) {
			if !model.EventMatches(ev, r) || ev.Type != "Warning" || !recentEvent(ev, snap.ObservedAt, time.Duration(policy.WarningEventWindowSeconds)*time.Second) {
				continue
			}
			if lastObserved == "" || model.NewerEvent(ev, model.Event{Timestamp: lastObserved}) {
				lastObserved = ev.Timestamp
			}
			title, priority := "Recent warning: "+ev.Reason, 10
			if r.Definition == "" && ev.Reason == "Unhealthy" {
				title, priority = "Container probe failure reported", 50
			}
			if r.Definition == "" && (ev.Reason == "FailedMount" || ev.Reason == "FailedAttachVolume") {
				title, priority = "Volume attachment or mount failure", 50
			}
			if r.Definition == "" && ev.Reason == "FailedScheduling" {
				title, priority = "Pod cannot be scheduled", 55
			}
			signals = append(signals, signal{title, "warning", []string{ev.Reason + ": " + ev.Message}, []string{"The Kubernetes event is evidence of a recent problem; it does not establish that the problem persists or identify a confirmed root cause."}, []string{"Inspect current conditions and the related event details before taking action."}, priority})
		}
		if len(signals) == 0 {
			continue
		}
		affected := owner(r)
		id := "issue-" + affected.ID
		g := groups[id]
		if g == nil {
			g = &model.Issue{ID: id, Namespace: affected.Namespace, Resource: affected, Evidence: []string{}, Causes: []string{}, Recommendations: []string{}, Events: []model.Event{}, LastObserved: lastObserved, DiagnosticPolicy: policy}
			groups[id] = g
		}
		if model.NewerEvent(model.Event{Timestamp: lastObserved}, model.Event{Timestamp: g.LastObserved}) {
			g.LastObserved = lastObserved
		}
		for _, s := range signals {
			if s.priority > priorities[id] {
				priorities[id] = s.priority
				g.Title = s.title
				g.Severity = s.severity
			}
			g.Evidence = append(g.Evidence, s.evidence...)
			g.Causes = append(g.Causes, s.causes...)
			g.Recommendations = append(g.Recommendations, s.steps...)
		}
	}
	for _, r := range snap.Resources {
		// Controller resolution is unnecessary when there are no events to attach.
		related := eventIndex.Related(r)
		if len(related) == 0 || len(groups) == 0 {
			continue
		}
		g := groups["issue-"+owner(r).ID]
		if g == nil {
			continue
		}
		for _, ev := range related {
			if model.EventMatches(ev, r) {
				if r.Definition != "" {
					ev.CustomDefinition = r.Definition
				}
				g.Events = append(g.Events, ev)
			}
		}
	}
	out := []model.Issue{}
	for _, g := range groups {
		g.Evidence = unique(g.Evidence)
		g.Causes = unique(g.Causes)
		g.Recommendations = unique(g.Recommendations)
		sort.Slice(g.Events, func(i, j int) bool { return model.NewerEvent(g.Events[i], g.Events[j]) })
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity == "critical"
		}
		return out[i].Resource.ID < out[j].Resource.ID
	})
	return out
}
func unique(items []string) []string {
	sort.Strings(items)
	if items == nil {
		return []string{}
	}
	return slices.Compact(items)
}

func (s *Service) detectIssues(snap model.Snapshot) []model.Issue {
	issues := detectIssues(snap, s.diagnostics)
	if len(issues) == 0 {
		return issues
	}
	wanted := make(map[changeIdentity]bool, len(issues))
	for _, issue := range issues {
		wanted[changeIdentity{issue.Resource.ID, model.Str(issue.Resource.Object, "metadata", "uid")}] = true
	}
	recent := s.observations.relatedFor(snap.ObservedAt, wanted)
	for i := range issues {
		resource := issues[i].Resource
		for _, c := range recent[changeIdentity{resource.ID, model.Str(resource.Object, "metadata", "uid")}] {
			issues[i].RecentChanges = append(issues[i].RecentChanges, c.Type+": "+c.Evidence+" Observed "+c.ObservedAt.Format(time.RFC3339)+"; correlation does not establish causation.")
		}
	}
	return issues
}
