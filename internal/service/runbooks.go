package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"timone/internal/model"
)

type Runbook struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Rule      string `json:"rule,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name,omitempty"`
}

var diagnosticRules = map[string]string{
	"Persistent volume claim lost its volume": "volume-claim-lost",
	"Container repeatedly failing":            "container-failure",
	"Container image cannot be pulled":        "image-pull",
	"Pod waiting to start":                    "pod-startup",
	"Repeated container restarts":             "container-restarts",
	"Workload replicas unavailable":           "workload-availability",
	"Node is not ready":                       "node-not-ready",
	"Pod cannot be scheduled":                 "pod-scheduling",
	"Pod readiness condition failing":         "pod-readiness",
	"Deployment exceeded progress deadline":   "rollout-deadline",
	"Job failure reported":                    "job-failure",
	"Container probe failure reported":        "container-probe",
	"Volume attachment or mount failure":      "volume-mount",
	"Autoscaler cannot scale":                 "autoscaler-unavailable",
	"Autoscaler constrained":                  "autoscaler-limited",
	"Resource quota exhausted":                "quota-exhausted",
	"Custom resource reports not ready":       "custom-not-ready",
}

func DiagnosticRuleIDs() []string {
	out := []string{"node-condition", "event-warning", "resource-warning"}
	for _, id := range diagnosticRules {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
func diagnosticRuleID(title string) string {
	if id, ok := diagnosticRules[title]; ok {
		return id
	}
	if strings.HasPrefix(title, "Node reports ") {
		return "node-condition"
	}
	if strings.HasPrefix(title, "Recent warning: ") {
		return "event-warning"
	}
	return "resource-warning"
}
func LoadRunbooks(path string) ([]Runbook, error) {
	if path == "" {
		return nil, nil
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, fmt.Errorf("cannot open runbooks file: %w", e)
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if e != nil || len(data) > 256<<10 {
		return nil, fmt.Errorf("runbooks file exceeds 256 KiB or is unreadable")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var entries []Runbook
	if e = decoder.Decode(&entries); e != nil {
		return nil, fmt.Errorf("invalid runbooks JSON: %w", e)
	}
	if decoder.Decode(new(any)) != io.EOF || entries == nil || len(entries) > 100 {
		return nil, fmt.Errorf("runbooks file must contain one array with at most 100 entries")
	}
	validRules := map[string]bool{}
	for _, id := range DiagnosticRuleIDs() {
		validRules[id] = true
	}
	seen := map[string]bool{}
	kindPattern := regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,62}$`)
	for _, r := range entries {
		u, e := url.Parse(r.URL)
		if !nameRE.MatchString(r.ID) || len(r.ID) > 63 || seen[r.ID] || strings.TrimSpace(r.Title) == "" || len(r.Title) > 120 || len(r.URL) > 2048 || e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return nil, fmt.Errorf("invalid runbook identity, title or HTTPS URL")
		}
		if r.Rule != "" && !validRules[r.Rule] {
			return nil, fmt.Errorf("unknown diagnostic rule %q", r.Rule)
		}
		if r.Namespace != "" && (!nameRE.MatchString(r.Namespace) || len(r.Namespace) > 63 || strings.Contains(r.Namespace, ".")) {
			return nil, fmt.Errorf("invalid runbook namespace")
		}
		if r.Kind != "" && !kindPattern.MatchString(r.Kind) {
			return nil, fmt.Errorf("invalid runbook kind")
		}
		if r.Name != "" && (r.Kind == "" || !nameRE.MatchString(r.Name) || len(r.Name) > 253) {
			return nil, fmt.Errorf("runbook resource names require a kind and valid name")
		}
		if r.Rule == "" && r.Namespace == "" && r.Kind == "" {
			return nil, fmt.Errorf("runbook requires a rule, namespace or resource kind")
		}
		seen[r.ID] = true
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

// ConfigureRunbooks installs validated startup configuration before the service is exposed.
func (s *Service) ConfigureRunbooks(entries []Runbook) {
	s.runbooks = append([]Runbook(nil), entries...)
}
func (s *Service) issueRunbooks(issue model.Issue) []model.RunbookLink {
	out := []model.RunbookLink{}
	for _, r := range s.runbooks {
		if r.Namespace != "" && r.Namespace != issue.Resource.Namespace {
			continue
		}
		if r.Kind != "" && r.Kind != issue.Resource.Kind {
			continue
		}
		if r.Name != "" && r.Name != issue.Resource.Name {
			continue
		}
		if r.Rule != "" {
			matched := false
			for _, id := range issue.RuleIDs {
				matched = matched || id == r.Rule
			}
			if !matched {
				continue
			}
		}
		out = append(out, model.RunbookLink{ID: r.ID, Title: r.Title, URL: r.URL})
	}
	return out
}
