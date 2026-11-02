package service

import (
	"context"
	"sort"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type IncidentResponse struct {
	ArchiveSamples []time.Time           `json:"archiveSamples,omitempty"`
	SchemaVersion  int                   `json:"schemaVersion"`
	Cluster        string                `json:"cluster"`
	Mode           string                `json:"mode"`
	CollectedAt    time.Time             `json:"collectedAt"`
	Window         InvestigationWindow   `json:"window"`
	Detail         model.Detail          `json:"detail"`
	Resources      []model.Resource      `json:"resources"`
	Issues         []model.Issue         `json:"issues"`
	Events         []model.Event         `json:"events"`
	Changes        []Change              `json:"changes"`
	Metrics        []model.Metric        `json:"metrics"`
	History        []model.MetricHistory `json:"history"`
	Logs           *WorkloadLogResponse  `json:"logs,omitempty"`
	Warnings       []model.Warning       `json:"warnings"`
	Limitations    []string              `json:"limitations"`
}

func (s *Service) Incident(ctx context.Context, kind, ns, name string, window InvestigationWindow, includeLogs bool) (IncidentResponse, error) {
	ctx = provider.WithReadBudget(ctx, provider.InvestigationBytes)
	out := IncidentResponse{SchemaVersion: 1, Cluster: s.Provider.Name(), Mode: s.Provider.Mode(), CollectedAt: time.Now().UTC(), Window: window, Resources: []model.Resource{}, Issues: []model.Issue{}, Events: []model.Event{}, Changes: []Change{}, Metrics: []model.Metric{}, History: []model.MetricHistory{}, Warnings: []model.Warning{}, Limitations: []string{
		"Conditions and issues describe current readable state, not reconstructed past state. Temporal correlation does not establish causation.",
		"Changes are sampled in this process, retained for at most one hour and lost on restart. Kubernetes events may have expired.",
		"Only currently readable resource identities are included. Deleted Pods and logs are unavailable without external storage.",
		"Historical metrics cover at most four current Pod/Node identities. Current metric samples outside the selected interval are omitted.",
	}}
	if _, err := Window(window.Start.Format(time.RFC3339Nano), window.End.Format(time.RFC3339Nano), time.Now()); err != nil {
		return out, err
	}
	detail, err := s.Detail(ctx, kind, ns, name)
	if err != nil {
		return out, err
	}
	out.Detail = detail
	snap, err := s.Snapshot(ctx, ns)
	if err != nil {
		return out, err
	}
	out.Warnings = append(out.Warnings, detail.Warnings...)
	out.Warnings = append(out.Warnings, snap.Warnings...)
	objects := make([]model.Object, 0, len(snap.Resources))
	for _, r := range snap.Resources {
		objects = append(objects, r.Object)
	}
	members, trunc := descendants(detail.Manifest, objects, 201)
	if trunc {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Incident", Message: "Resource membership limited to 200 descendants"})
	}
	wanted := map[changeIdentity]bool{}
	byID := map[string]model.Resource{}
	for _, o := range members {
		r := model.Summarize(o)
		out.Resources = append(out.Resources, r)
		byID[r.ID] = r
		wanted[changeIdentity{r.ID, model.Str(o, "metadata", "uid")}] = true
	}
	for _, issue := range s.detectIssues(snap) {
		if _, ok := byID[issue.Resource.ID]; ok && wanted[changeIdentity{issue.Resource.ID, model.Str(issue.Resource.Object, "metadata", "uid")}] {
			out.Issues = append(out.Issues, issue)
		}
	}
	for _, e := range snap.Events {
		r, ok := byID[e.ResourceID]
		if !ok || !model.EventMatches(e, r) {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, e.Timestamp)
		if err == nil && window.contains(t) {
			out.Events = append(out.Events, e)
		}
	}
	if len(out.Events) > 1000 {
		out.Events = out.Events[:1000]
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Event", Message: "Incident events limited to 1000"})
	}
	s.observations.mu.Lock()
	for _, c := range s.observations.changes {
		if wanted[changeIdentity{c.Resource.ID, c.UID}] && window.contains(c.ObservedAt) {
			out.Changes = append(out.Changes, c)
		}
	}
	s.observations.mu.Unlock()
	sort.Slice(out.Changes, func(i, j int) bool { return out.Changes[i].ObservedAt.Before(out.Changes[j].ObservedAt) })
	if len(out.Changes) > 1000 {
		out.Changes = out.Changes[len(out.Changes)-1000:]
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Changes", Message: "Incident changes limited to 1000"})
	}
	if s.archive != nil {
		archived, e := s.readArchive(ctx, window, wanted, ArchivedHistory{Status: s.ArchiveStatus(), Window: window, Samples: []time.Time{}, Changes: []Change{}, Events: []model.Event{}, Warnings: []model.Warning{}})
		if e != nil {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "History", Message: "Persisted history unavailable"})
		} else {
			out.ArchiveSamples = archived.Samples
			out.Warnings = append(out.Warnings, archived.Warnings...)
			out.Limitations[1] = "Current in-memory observations are supplemented with retained samples for matching readable UIDs. Collection gaps and restarts can miss intermediate changes. Persisted event messages are omitted."
			changeIDs := map[string]bool{}
			for _, c := range out.Changes {
				changeIDs[c.ID] = true
			}
			for _, c := range archived.Changes {
				if !changeIDs[c.ID] {
					out.Changes = append(out.Changes, c)
				}
			}
			eventIDs := map[string]bool{}
			for _, event := range out.Events {
				eventIDs[event.ID] = true
			}
			for _, event := range archived.Events {
				if !eventIDs[event.ID] {
					out.Events = append(out.Events, event)
				}
			}
			sort.Slice(out.Changes, func(i, j int) bool { return out.Changes[i].ObservedAt.Before(out.Changes[j].ObservedAt) })
			sort.Slice(out.Events, func(i, j int) bool { return model.NewerEvent(out.Events[i], out.Events[j]) })
			if len(out.Changes) > 1000 {
				out.Changes = out.Changes[len(out.Changes)-1000:]
				out.Warnings = append(out.Warnings, model.Warning{Kind: "Changes", Message: "Combined changes limited to 1000"})
			}
			if len(out.Events) > 1000 {
				out.Events = out.Events[:1000]
				out.Warnings = append(out.Warnings, model.Warning{Kind: "Event", Message: "Combined events limited to 1000"})
			}
		}
	}
	metrics, e := s.Provider.Metrics(ctx, ns)
	if e != nil {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Metrics", Message: e.Error()})
	} else {
		if !metrics.Available {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "Metrics", Message: metrics.Message})
		}
		for _, m := range metrics.Items {
			r, ok := byID[model.ID(m.Kind, m.Namespace, m.Name)]
			ts, err := time.Parse(time.RFC3339Nano, m.Timestamp)
			created, ce := time.Parse(time.RFC3339Nano, r.CreatedAt)
			if ok && err == nil && ce == nil && !ts.Before(created) && window.contains(ts) {
				out.Metrics = append(out.Metrics, m)
			}
		}
	}
	historyTargets := 0
	for _, r := range out.Resources {
		if r.Kind != "Pod" && r.Kind != "Node" {
			continue
		}
		historyTargets++
		if historyTargets > 4 {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "History", Message: "Historical metrics limited to four current resources"})
			break
		}
		historyCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		h, e := s.metricHistoryWindow(historyCtx, model.HistoryQuery{Kind: r.Kind, Namespace: r.Namespace, Name: r.Name}, &window)
		cancel()
		if e != nil {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "History", Message: e.Error()})
			continue
		}
		if !wanted[changeIdentity{h.Resource.ID, model.Str(h.Resource.Object, "metadata", "uid")}] {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "History", Message: "Resource identity changed during metric collection"})
			continue
		}
		out.History = append(out.History, h)
	}
	if includeLogs && logTarget(kind) {
		logs, e := s.WorkloadLogs(ctx, kind, ns, name, "", 100, false, window)
		if e != nil {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "Logs", Message: e.Error()})
		} else {
			allowedSources := map[string]bool{}
			for i := range logs.Sources {
				source := &logs.Sources[i]
				if wanted[changeIdentity{model.ID("Pod", ns, source.Pod), source.UID}] {
					allowedSources[source.Pod+"\x00"+source.Container] = true
				} else {
					source.State = "unavailable"
					source.Message = "Identity changed during incident collection"
				}
			}
			retained := logs.Lines[:0]
			for _, line := range logs.Lines {
				if allowedSources[line.Pod+"\x00"+line.Container] {
					retained = append(retained, line)
				}
			}
			logs.Lines = retained
			out.Logs = &logs
		}
	}
	current, e := s.Provider.Get(ctx, kind, ns, name)
	if e != nil {
		return out, e
	}
	if model.Str(current, "metadata", "uid") != model.Str(detail.Manifest, "metadata", "uid") {
		return out, &provider.Error{Code: 409, Message: "Resource identity changed during investigation; reload"}
	}
	return out, ctx.Err()
}
