package service

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type InvestigationWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func Window(since, until string, now time.Time) (InvestigationWindow, error) {
	end := now.UTC()
	var err error
	if until != "" {
		end, err = time.Parse(time.RFC3339, until)
		if err != nil {
			return InvestigationWindow{}, &provider.Error{Code: 400, Message: "until must be RFC3339"}
		}
	}
	start := end.Add(-time.Hour)
	if since != "" {
		start, err = time.Parse(time.RFC3339, since)
		if err != nil {
			return InvestigationWindow{}, &provider.Error{Code: 400, Message: "since must be RFC3339"}
		}
	}
	start = start.UTC().Truncate(time.Second)
	end = end.UTC().Truncate(time.Second)
	if !start.Before(end) || end.After(now.Add(time.Second)) || end.Sub(start) > 24*time.Hour {
		return InvestigationWindow{}, &provider.Error{Code: 400, Message: "Select a past interval greater than zero and no longer than 24 hours"}
	}
	return InvestigationWindow{Start: start.UTC().Truncate(time.Second), End: end.UTC().Truncate(time.Second)}, nil
}
func (w InvestigationWindow) contains(t time.Time) bool { return !t.Before(w.Start) && !t.After(w.End) }

type LogLine struct {
	Timestamp *time.Time `json:"timestamp"`
	Pod       string     `json:"pod"`
	Container string     `json:"container"`
	Text      string     `json:"text"`
}
type LogSource struct {
	Pod       string `json:"pod"`
	UID       string `json:"uid"`
	Container string `json:"container"`
	State     string `json:"state"`
	Message   string `json:"message"`
	Truncated bool   `json:"truncated"`
}
type WorkloadLogResponse struct {
	Window    InvestigationWindow `json:"window"`
	Lines     []LogLine           `json:"lines"`
	Sources   []LogSource         `json:"sources"`
	Warnings  []model.Warning     `json:"warnings"`
	Truncated bool                `json:"truncated"`
	Mode      string              `json:"mode"`
}

func (s *Service) WorkloadLogs(ctx context.Context, kind, ns, name, container string, tail int, previous bool, window InvestigationWindow, selected ...LogSourceRef) (WorkloadLogResponse, error) {
	ctx = provider.WithReadBudget(ctx, provider.InvestigationBytes)
	out := WorkloadLogResponse{Window: window, Lines: []LogLine{}, Sources: []LogSource{}, Warnings: []model.Warning{}, Mode: s.Provider.Mode()}
	if tail < 1 || tail > 1000 || (container != "" && (!nameRE.MatchString(container) || len(container) > 63)) {
		return out, &provider.Error{Code: 400, Message: "Invalid log selection"}
	}
	if _, err := Window(window.Start.Format(time.RFC3339Nano), window.End.Format(time.RFC3339Nano), time.Now()); err != nil {
		return out, err
	}
	if len(selected) > 12 || (len(selected) > 0 && container != "") {
		return out, &provider.Error{Code: 400, Message: "Select at most 12 explicit sources, without a container filter"}
	}
	catalog, err := s.LogSources(ctx, kind, ns, name, container)
	if err != nil {
		return out, err
	}
	out.Warnings = catalog.Warnings
	out.Truncated = catalog.Truncated
	candidates := map[LogSourceRef]bool{}
	for _, source := range catalog.Sources {
		candidates[source] = true
	}
	if len(selected) > 0 {
		seen := map[LogSourceRef]bool{}
		for _, source := range selected {
			if source.Pod == "" || source.Container == "" || source.UID == "" || len(source.UID) > 256 || !nameRE.MatchString(source.Pod) || !nameRE.MatchString(source.Container) || len(source.Pod) > 253 || len(source.Container) > 63 || seen[source] {
				return out, &provider.Error{Code: 400, Message: "Invalid or duplicate log source"}
			}
			if !candidates[source] {
				return out, &provider.Error{Code: 409, Message: "A selected source changed or no longer belongs to this workload; refresh the source list"}
			}
			seen[source] = true
			out.Sources = append(out.Sources, LogSource{Pod: source.Pod, UID: source.UID, Container: source.Container, State: "pending"})
		}
	} else {
		for _, source := range catalog.Sources {
			out.Sources = append(out.Sources, LogSource{Pod: source.Pod, UID: source.UID, Container: source.Container, State: "pending"})
		}
	}
	sort.Slice(out.Sources, func(i, j int) bool {
		return out.Sources[i].Pod+"\x00"+out.Sources[i].Container < out.Sources[j].Pod+"\x00"+out.Sources[j].Container
	})
	if len(out.Sources) > 12 {
		out.Sources = out.Sources[:12]
		out.Truncated = true
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Logs", Message: "Only the first 12 Pod/container sources are read; choose explicit sources to change this selection"})
	}
	if len(out.Sources) == 0 {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Logs", Message: "No matching readable container sources"})
	}
	chunks := make([][]LogLine, len(out.Sources))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for i := range out.Sources {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			source := &out.Sources[i]
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				source.State = "unavailable"
				source.Message = "Request cancelled"
				return
			}
			defer func() { <-slots }()
			sourceCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			current, e := s.Provider.Get(sourceCtx, "Pod", ns, source.Pod)
			if e != nil {
				source.State = "unavailable"
				source.Message = e.Error()
				return
			}
			if source.UID == "" || source.UID != model.Str(current, "metadata", "uid") {
				source.State = "unavailable"
				source.Message = "Pod identity changed; reload workload membership"
				return
			}
			text, e := s.LogsWithOptions(sourceCtx, ns, source.Pod, source.Container, tail, previous, window.Start.Format(time.RFC3339Nano))
			if e != nil {
				source.State = "unavailable"
				source.Message = e.Error()
				return
			}
			source.State = "available"
			source.Truncated = len(text) >= 131072
			if len(text) > 65536 {
				text = text[:65536]
				if n := strings.LastIndexByte(text, '\n'); n >= 0 {
					text = text[:n]
				}
				source.Truncated = true
			}
			unknown := 0
			for _, line := range strings.Split(text, "\n") {
				if line == "" {
					continue
				}
				stamp, body, ok := strings.Cut(line, " ")
				t, e := time.Parse(time.RFC3339Nano, stamp)
				if !ok || e != nil {
					unknown++
					continue
				}
				if window.contains(t) {
					ts := t
					chunks[i] = append(chunks[i], LogLine{Timestamp: &ts, Pod: source.Pod, Container: source.Container, Text: body})
				}
			}
			if unknown > 0 {
				source.Message = "Lines without valid timestamps omitted from the selected interval"
			}
		}(i)
	}
	wg.Wait()
	for i, chunk := range chunks {
		out.Lines = append(out.Lines, chunk...)
		out.Truncated = out.Truncated || out.Sources[i].Truncated
	}
	sort.SliceStable(out.Lines, func(i, j int) bool {
		a, b := out.Lines[i], out.Lines[j]
		if !a.Timestamp.Equal(*b.Timestamp) {
			return a.Timestamp.Before(*b.Timestamp)
		}
		return a.Pod+"\x00"+a.Container < b.Pod+"\x00"+b.Container
	})
	if len(out.Lines) > 5000 {
		out.Lines = out.Lines[len(out.Lines)-5000:]
		out.Truncated = true
	}
	return out, ctx.Err()
}

type LogSourceRef struct {
	Pod       string `json:"pod"`
	UID       string `json:"uid"`
	Container string `json:"container"`
}
type LogSourceCatalog struct {
	Sources   []LogSourceRef  `json:"sources"`
	Warnings  []model.Warning `json:"warnings"`
	Truncated bool            `json:"truncated"`
}

func (s *Service) LogSources(ctx context.Context, kind, ns, name, container string) (LogSourceCatalog, error) {
	ctx = provider.WithReadBudget(ctx, provider.InvestigationBytes)
	out := LogSourceCatalog{Sources: []LogSourceRef{}, Warnings: []model.Warning{}}
	if container != "" && (!nameRE.MatchString(container) || len(container) > 63) {
		return out, &provider.Error{Code: 400, Message: "Invalid container filter"}
	}
	_, pods, warnings, err := s.workloadPods(ctx, kind, ns, name)
	if err != nil {
		return out, err
	}
	out.Warnings = warnings
	for _, pod := range pods {
		seen := map[string]bool{}
		for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
			for _, raw := range model.Arr(pod, "spec", field) {
				c, _ := raw.(map[string]any)
				n := model.Str(c, "name")
				if n == "" || seen[n] || (container != "" && container != n) {
					continue
				}
				seen[n] = true
				if len(out.Sources) >= 1000 {
					out.Truncated = true
					continue
				}
				out.Sources = append(out.Sources, LogSourceRef{Pod: model.Str(pod, "metadata", "name"), UID: model.Str(pod, "metadata", "uid"), Container: n})
			}
		}
	}
	sort.Slice(out.Sources, func(i, j int) bool {
		a, b := out.Sources[i], out.Sources[j]
		return a.Pod+"\x00"+a.Container < b.Pod+"\x00"+b.Container
	})
	if out.Truncated {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Logs", Message: "Source catalog limited to 1000 containers; narrow the container filter"})
	}
	return out, ctx.Err()
}
