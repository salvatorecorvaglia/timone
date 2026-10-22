package service

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"timone/internal/model"
	"timone/internal/provider"
)

var nameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
var rbacNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._-]*$`)

type Query struct {
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Search    string `json:"search,omitempty"`
	Status    string `json:"status,omitempty"`
	Resource  string `json:"resource,omitempty"`
	Since     string `json:"since,omitempty"`
	Until     string `json:"until,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Sort      string `json:"sort,omitempty"`
	Desc      bool   `json:"desc,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

func (q *Query) Validate() error {
	if q.Kind != "" {
		if _, ok := provider.FindKind(q.Kind); !ok {
			return &provider.Error{Code: 400, Message: "Unsupported resource kind"}
		}
	}
	if q.Namespace != "" && (len(q.Namespace) > 63 || !nameRE.MatchString(q.Namespace) || strings.Contains(q.Namespace, ".")) {
		return &provider.Error{Code: 400, Message: "Invalid namespace"}
	}
	if len(q.Search) > 200 {
		return &provider.Error{Code: 400, Message: "Search exceeds 200 characters"}
	}
	if len(q.Phase) > 64 {
		return &provider.Error{Code: 400, Message: "Invalid phase"}
	}
	if q.Resource != "" && (len(q.Resource) > 253 || !rbacNameRE.MatchString(q.Resource)) {
		return &provider.Error{Code: 400, Message: "Invalid event resource name"}
	}
	var since, until time.Time
	for _, p := range []struct {
		value string
		dest  *time.Time
	}{{q.Since, &since}, {q.Until, &until}} {
		if p.value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, p.value)
		if err != nil || len(p.value) > 64 {
			return &provider.Error{Code: 400, Message: "Event times must be RFC3339 timestamps"}
		}
		*p.dest = parsed
	}
	if !since.IsZero() && !until.IsZero() && since.After(until) {
		return &provider.Error{Code: 400, Message: "since must not be after until"}
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 200 || q.Offset < 0 || q.Offset > 5000 {
		return &provider.Error{Code: 400, Message: "Use limit 1–200 and offset 0–5000"}
	}
	switch q.Sort {
	case "", "name", "namespace", "status", "restarts", "createdAt":
	default:
		return &provider.Error{Code: 400, Message: "Invalid sort field"}
	}
	switch q.Status {
	case "", "healthy", "warning", "critical", "unknown":
	default:
		return &provider.Error{Code: 400, Message: "Invalid health status"}
	}
	return nil
}
func ValidateResource(kind, ns, name string) error {
	q := Query{Kind: kind, Namespace: ns}
	if e := q.Validate(); e != nil {
		return e
	}
	k, ok := provider.FindKind(kind)
	if !ok {
		return &provider.Error{Code: 400, Message: "Unsupported kind"}
	}
	validName := nameRE.MatchString(name)
	if kind == "Role" || kind == "ClusterRole" || kind == "RoleBinding" || kind == "ClusterRoleBinding" {
		validName = rbacNameRE.MatchString(name)
	}
	if name == "" || len(name) > 253 || !validName {
		return &provider.Error{Code: 400, Message: "Invalid resource name"}
	}
	if k.Namespaced && ns == "" {
		return &provider.Error{Code: 400, Message: "Namespace is required"}
	}
	if !k.Namespaced && ns != "" {
		return &provider.Error{Code: 400, Message: "Cluster-scoped kind cannot have a namespace"}
	}
	return nil
}

type Service struct {
	observations *observations
	admission    chan struct{}
	Provider     provider.Provider
	history      HistorySource
	diagnostics  model.DiagnosticPolicy
}

func New(p provider.Provider) *Service {
	s := &Service{admission: make(chan struct{}, 8), Provider: p, observations: newObservations(), diagnostics: model.DefaultDiagnosticPolicy()}
	if p.Mode() == "demo" {
		s.history = demoHistory{}
	}
	return s
}
func (s *Service) Scope(ns string) (string, error) {
	if scoped := s.Provider.Namespace(); scoped != "" {
		if ns != "" && ns != scoped {
			return "", &provider.Error{Code: 403, Message: "Namespace is outside configured scope"}
		}
		return scoped, nil
	}
	return ns, nil
}
func (s *Service) List(ctx context.Context, q Query) (model.Page, error) {
	if e := q.Validate(); e != nil {
		return model.Page{}, e
	}
	ns, e := s.Scope(q.Namespace)
	if e != nil {
		return model.Page{}, e
	}
	if q.Kind == "" {
		q.Kind = "Pod"
	}
	objects, truncated, e := s.Provider.List(ctx, q.Kind, ns)
	if e != nil {
		return model.Page{}, e
	}
	return resourcePage(objects, truncated, q, model.Summarize), nil
}
func resourcePage(objects []model.Object, truncated bool, q Query, summarize func(model.Object) model.Resource) model.Page {
	items := []model.Resource{}
	for _, o := range objects {
		r := summarize(o)
		if q.Search != "" && !strings.Contains(strings.ToLower(r.Name+" "+r.Namespace+" "+r.Kind), strings.ToLower(q.Search)) {
			continue
		}
		if q.Status != "" && q.Status != r.Health {
			continue
		}
		if q.Phase != "" && q.Phase != r.Status {
			continue
		}
		items = append(items, r)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		cmp := strings.Compare(a.Name, b.Name)
		switch q.Sort {
		case "namespace":
			cmp = strings.Compare(a.Namespace, b.Namespace)
		case "status":
			cmp = strings.Compare(a.Health, b.Health)
		case "createdAt":
			cmp = strings.Compare(a.CreatedAt, b.CreatedAt)
		case "restarts":
			cmp = 0
			if a.Restarts < b.Restarts {
				cmp = -1
			} else if a.Restarts > b.Restarts {
				cmp = 1
			}
		}
		if cmp == 0 {
			cmp = strings.Compare(a.ID, b.ID)
		}
		if q.Desc {
			return cmp > 0
		}
		return cmp < 0
	})
	total := len(items)
	start := min(q.Offset, total)
	end := min(start+q.Limit, total)
	return model.Page{Items: items[start:end], Total: total, Limit: q.Limit, Offset: q.Offset, Truncated: truncated}
}
func (s *Service) Detail(ctx context.Context, kind, ns, name string) (model.Detail, error) {
	if e := ValidateResource(kind, ns, name); e != nil {
		return model.Detail{}, e
	}
	if _, e := s.Scope(ns); e != nil {
		return model.Detail{}, e
	}
	o, e := s.Provider.Get(ctx, kind, ns, name)
	if e != nil {
		return model.Detail{}, e
	}
	events, eventErr := s.Provider.Events(ctx, ns)
	related := []model.Event{}
	for _, v := range events {
		if model.EventMatches(v, model.Summarize(o)) {
			related = append(related, v)
		}
	}
	sort.Slice(related, func(i, j int) bool { return model.NewerEvent(related[i], related[j]) })
	warnings := []model.Warning{}
	if eventErr != nil {
		warnings = append(warnings, model.Warning{Kind: "Event", Message: eventErr.Error()})
	}
	return model.Detail{Resource: model.Summarize(o), Manifest: model.Sanitize(o), Events: related, Warnings: warnings, History: "Current state only. Kubernetes events may expire; no persistent resource history.", Redaction: "Secret payloads, nested annotations, managedFields, inline GitOps configuration, HTTP probe/lifecycle header values and direct environment values are omitted or redacted. Labels, ConfigMaps, command arguments, controller messages and logs may contain sensitive operator-provided information."}, nil
}

var coreKinds = []string{"Node", "Namespace", "Pod", "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "Job", "CronJob", "Service", "Ingress", "PersistentVolumeClaim"}

func (s *Service) Snapshot(ctx context.Context, ns string, extra ...string) (model.Snapshot, error) {
	ctx = provider.WithReadBudget(ctx, provider.InvestigationBytes)
	q := Query{Namespace: ns}
	if e := q.Validate(); e != nil {
		return model.Snapshot{}, e
	}
	ns, e := s.Scope(ns)
	if e != nil {
		return model.Snapshot{}, e
	}
	snap := model.Snapshot{Resources: []model.Resource{}, Events: []model.Event{}, Warnings: []model.Warning{}, ObservedAt: time.Now().UTC()}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	kinds := append(append([]string{}, coreKinds...), extra...)
	for _, kind := range kinds {
		wg.Add(1)
		go func(kind string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			objects, trunc, err := s.Provider.List(ctx, kind, ns)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				snap.Warnings = append(snap.Warnings, model.Warning{Kind: kind, Message: err.Error()})
				return
			}
			if trunc {
				snap.Warnings = append(snap.Warnings, model.Warning{Kind: kind, Message: "Collection truncated at 5000 resources; scope to a namespace"})
			}
			for _, o := range objects {
				r := model.Summarize(o)
				if kind == "Namespace" && ns != "" && r.Name != ns {
					continue
				}
				snap.Resources = append(snap.Resources, r)
			}
		}(kind)
	}
	wg.Wait()
	events, err := s.Provider.Events(ctx, ns)
	if err != nil {
		snap.Warnings = append(snap.Warnings, model.Warning{Kind: "Event", Message: err.Error()})
	} else {
		snap.Events = events
	}
	sort.Slice(snap.Resources, func(i, j int) bool { return snap.Resources[i].ID < snap.Resources[j].ID })
	sort.Slice(snap.Warnings, func(i, j int) bool { return snap.Warnings[i].Kind < snap.Warnings[j].Kind })
	if ctx.Err() == nil {
		s.observations.observe(snap)
	}
	return snap, ctx.Err()
}
func (s *Service) Overview(ctx context.Context, ns string) (*model.Overview, error) {
	snap, e := s.Snapshot(ctx, ns)
	if e != nil {
		return nil, e
	}
	counts := map[string]int{}
	for _, kind := range coreKinds {
		available := true
		for _, w := range snap.Warnings {
			if w.Kind == kind {
				available = false
			}
		}
		if available {
			counts[kind] = 0
		}
	}
	pods := map[string]int{}
	ready, desired := 0, 0
	namespaces := []string{}
	nodes := []model.Resource{}
	workloads := []model.Resource{}
	for _, r := range snap.Resources {
		counts[r.Kind]++
		switch r.Kind {
		case "Namespace":
			namespaces = append(namespaces, r.Name)
		case "Node":
			nodes = append(nodes, r)
		case "Pod":
			pods[r.Status]++
		case "Deployment":
			ready += int(model.Num(r.Object, "status", "availableReplicas"))
			n := model.Num(r.Object, "spec", "replicas")
			if model.Value(r.Object, "spec", "replicas") == nil {
				n = 1
			}
			desired += int(n)
			workloads = append(workloads, r)
		case "StatefulSet", "DaemonSet":
			workloads = append(workloads, r)
		}
	}
	customCoverage, err := s.customDiagnostics(ctx, &snap, ns)
	if err != nil {
		return nil, err
	}
	issues := s.detectIssues(snap)
	health := "healthy"
	healthRank := map[string]int{"healthy": 0, "unknown": 1, "warning": 2, "critical": 3}
	for _, r := range snap.Resources {
		switch r.Kind {
		case "Node", "Pod", "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "Job", "PersistentVolumeClaim":
			if healthRank[r.Health] > healthRank[health] {
				health = r.Health
			}
		}
	}
	for _, i := range issues {
		if i.Severity == "critical" {
			health = "critical"
			break
		}
		if health != "critical" {
			health = "warning"
		}
	}
	if (len(snap.Warnings) > 0 || customCoverage.State == "partial" || customCoverage.State == "unavailable" || customCoverage.ReadinessUnknown > 0) && health == "healthy" {
		health = "unknown"
	}
	sort.Strings(namespaces)
	events := snap.Events
	sort.Slice(events, func(i, j int) bool { return model.NewerEvent(events[i], events[j]) })
	return &model.Overview{
		Cluster: s.Provider.Name(), Mode: s.Provider.Mode(), Scope: s.Provider.Namespace(),
		Health: health, Counts: counts, PodStates: pods,
		AvailableReplicas: ready, DesiredReplicas: desired,
		Namespaces: namespaces, Nodes: nodes, Workloads: workloads, Issues: issues,
		CustomDiagnostics: customCoverage, DiagnosticPolicy: s.diagnostics,
		Events: events[:min(len(events), 6)], Warnings: snap.Warnings,
		ObservedAt: snap.ObservedAt, History: "Current Kubernetes events only; no persistent retention.",
	}, nil
}
func (s *Service) Events(ctx context.Context, q Query) (*EventsResponse, error) {
	if e := q.Validate(); e != nil {
		return nil, e
	}
	ns, e := s.Scope(q.Namespace)
	if e != nil {
		return nil, e
	}
	events, e := s.Provider.Events(ctx, ns)
	if e != nil {
		return nil, e
	}
	since, _ := time.Parse(time.RFC3339, q.Since)
	until, _ := time.Parse(time.RFC3339, q.Until)
	items := []model.Event{}
	for _, ev := range events {
		if q.Resource != "" && ev.Resource != q.Resource {
			continue
		}
		stamp, err := time.Parse(time.RFC3339, ev.Timestamp)
		if (q.Since != "" || q.Until != "") && (err != nil || (!since.IsZero() && stamp.Before(since)) || (!until.IsZero() && stamp.After(until))) {
			continue
		}

		if q.Kind != "" && ev.Kind != q.Kind {
			continue
		}
		if q.Status == "warning" && ev.Type != "Warning" {
			continue
		}
		if q.Status == "healthy" && ev.Type != "Normal" {
			continue
		}
		if !strings.Contains(strings.ToLower(ev.Message+" "+ev.Resource+" "+ev.Reason), strings.ToLower(q.Search)) {
			continue
		}
		items = append(items, ev)
	}
	sort.Slice(items, func(i, j int) bool { return model.NewerEvent(items[i], items[j]) })
	total := len(items)
	oldest, newest := "", ""
	if total > 0 {
		oldest = items[total-1].Timestamp
		for _, ev := range items {
			if first, err := time.Parse(time.RFC3339, ev.FirstObserved); err == nil {
				old, oe := time.Parse(time.RFC3339, oldest)
				if oe != nil || first.Before(old) {
					oldest = ev.FirstObserved
				}
			}
		}
		newest = items[0].Timestamp
	}
	start := min(q.Offset, total)
	return &EventsResponse{Items: items[start:min(start+q.Limit, total)], Total: total, Oldest: oldest, Newest: newest, Retention: "Only events currently retained by Kubernetes; repetitive events use Kubernetes count."}, nil
}
func (s *Service) Issues(ctx context.Context, ns string) (*IssuesResponse, error) {
	snap, e := s.Snapshot(ctx, ns)
	if e != nil {
		return nil, e
	}
	customCoverage, err := s.customDiagnostics(ctx, &snap, ns)
	if err != nil {
		return nil, err
	}
	return &IssuesResponse{Items: s.detectIssues(snap), CustomDiagnostics: customCoverage, DiagnosticPolicy: s.diagnostics, Warnings: snap.Warnings, ObservedAt: snap.ObservedAt}, nil
}
func (s *Service) Logs(ctx context.Context, ns, pod, container string, tail int) (string, error) {
	if e := ValidateResource("Pod", ns, pod); e != nil {
		return "", e
	}
	if container != "" && (!nameRE.MatchString(container) || len(container) > 63) {
		return "", &provider.Error{Code: 400, Message: "Invalid container name"}
	}
	if _, e := s.Scope(ns); e != nil {
		return "", e
	}
	if tail < 1 || tail > 1000 {
		return "", &provider.Error{Code: 400, Message: "tail must be 1–1000"}
	}
	return s.Provider.Logs(ctx, ns, pod, container, tail)
}
func (s *Service) Metrics(ctx context.Context, ns string) (model.Metrics, error) {
	q := Query{Namespace: ns}
	if e := q.Validate(); e != nil {
		return model.Metrics{}, e
	}
	scope, e := s.Scope(ns)
	if e != nil {
		return model.Metrics{}, e
	}
	return s.metrics(ctx, scope)
}
func (s *Service) Issue(ctx context.Context, ns, id string) (model.Issue, error) {
	snap, e := s.Snapshot(ctx, ns)
	if e != nil {
		return model.Issue{}, e
	}
	if _, err := s.customDiagnostics(ctx, &snap, ns); err != nil {
		return model.Issue{}, err
	}
	for _, i := range s.detectIssues(snap) {
		if i.ID == id {
			return i, nil
		}
	}
	return model.Issue{}, provider.ErrNotFound
}
