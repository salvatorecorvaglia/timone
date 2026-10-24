package model

import (
	"encoding/json"
	"fmt"
	"time"
)

type Object map[string]any

func Map(o map[string]any, path ...string) map[string]any {
	var v any = o
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	m, _ := v.(map[string]any)
	return m
}
func Value(o map[string]any, path ...string) any {
	if len(path) == 0 {
		return o
	}
	m := Map(o, path[:len(path)-1]...)
	return m[path[len(path)-1]]
}
func Str(o map[string]any, path ...string) string {
	v := Value(o, path...)
	if v == nil {
		return ""
	}
	if text, ok := v.(string); ok {
		return text
	}
	return fmt.Sprint(v)
}
func Num(o map[string]any, path ...string) float64 {
	v := Value(o, path...)
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}
func Arr(o map[string]any, path ...string) []any { a, _ := Value(o, path...).([]any); return a }
func Bool(o map[string]any, path ...string) bool { v, _ := Value(o, path...).(bool); return v }

type Resource struct {
	PrinterCells []string       `json:"printerCells,omitempty"`
	Definition   string         `json:"definition,omitempty"`
	Version      string         `json:"version,omitempty"`
	ID           string         `json:"id"`
	Kind         string         `json:"kind"`
	Name         string         `json:"name"`
	Namespace    string         `json:"namespace"`
	Status       string         `json:"status"`
	Health       string         `json:"health"`
	Ready        string         `json:"ready"`
	Restarts     int            `json:"restarts"`
	CreatedAt    string         `json:"createdAt"`
	Labels       map[string]any `json:"labels"`
	Object       Object         `json:"-"`
}
type Page struct {
	Items     []Resource `json:"items"`
	Total     int        `json:"total"`
	Limit     int        `json:"limit"`
	Offset    int        `json:"offset"`
	Truncated bool       `json:"truncated"`
}
type Event struct {
	ID                string `json:"id"`
	Namespace         string `json:"namespace"`
	Resource          string `json:"resource"`
	Kind              string `json:"kind"`
	ResourceID        string `json:"resourceId"`
	ResourceNamespace string `json:"resourceNamespace"`
	ResourceUID       string `json:"resourceUid,omitempty"`
	CustomDefinition  string `json:"customDefinition,omitempty"`
	Type              string `json:"type"`
	Reason            string `json:"reason"`
	Message           string `json:"message"`
	Count             int    `json:"count"`
	Timestamp         string `json:"timestamp"`
	FirstObserved     string `json:"firstObserved,omitempty"`
}

// EventMatches avoids attaching retained events to a replacement with the same name.
func EventMatches(e Event, r Resource) bool {
	if r.Definition != "" {
		return CustomEventMatches(e, r)
	}
	if e.ResourceID != r.ID {
		return false
	}
	if e.ResourceUID != "" {
		return e.ResourceUID == Str(r.Object, "metadata", "uid")
	}
	created, ce := time.Parse(time.RFC3339, r.CreatedAt)
	observed, oe := time.Parse(time.RFC3339, e.Timestamp)
	return ce != nil || oe != nil || !observed.Before(created)
}

// NewerEvent orders parsed instants, not lexical RFC3339 strings with different offsets.
func NewerEvent(a, b Event) bool {
	ta, ea := time.Parse(time.RFC3339, a.Timestamp)
	tb, eb := time.Parse(time.RFC3339, b.Timestamp)
	if ea != nil || eb != nil {
		if ea != nil && eb == nil {
			return false
		}
		if ea == nil && eb != nil {
			return true
		}
		return a.ID < b.ID
	}
	if ta.Equal(tb) {
		return a.ID < b.ID
	}
	return ta.After(tb)
}

type Issue struct {
	RecentChanges    []string         `json:"recentChanges"`
	DiagnosticPolicy DiagnosticPolicy `json:"diagnosticPolicy"`
	ID               string           `json:"id"`
	Severity         string           `json:"severity"`
	Title            string           `json:"title"`
	Namespace        string           `json:"namespace"`
	Resource         Resource         `json:"resource"`
	Evidence         []string         `json:"evidence"`
	Causes           []string         `json:"causes"`
	Recommendations  []string         `json:"recommendations"`
	Events           []Event          `json:"events"`
	LastObserved     string           `json:"lastObserved"`
}
type Finding struct {
	ID             string   `json:"id"`
	Rule           string   `json:"rule"`
	Severity       string   `json:"severity"`
	Resource       Resource `json:"resource"`
	Evidence       string   `json:"evidence"`
	Recommendation string   `json:"recommendation"`
}
type Warning struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}
type Snapshot struct {
	Resources  []Resource `json:"resources"`
	Events     []Event    `json:"events"`
	Warnings   []Warning  `json:"warnings"`
	ObservedAt time.Time  `json:"observedAt"`
}
type Edge struct {
	Routing    *RoutingAssessment `json:"routing,omitempty"`
	ID         string             `json:"id"`
	Source     string             `json:"source"`
	Target     string             `json:"target"`
	Relation   string             `json:"relation"`
	Confidence string             `json:"confidence"`
}

// RoutingAssessment separates partial configuration checks from controller reports.
// Neither is evidence of observed traffic or complete Gateway API conformance.
type RoutingAssessment struct {
	State        string   `json:"state"`
	Reference    string   `json:"reference"`
	Evidence     []string `json:"evidence"`
	Limitations  []string `json:"limitations"`
	Accepted     string   `json:"accepted,omitempty"`
	ResolvedRefs string   `json:"resolvedRefs,omitempty"`
}
type Graph struct {
	Nodes     []Resource `json:"nodes"`
	Edges     []Edge     `json:"edges"`
	Truncated bool       `json:"truncated"`
	Warnings  []Warning  `json:"warnings"`
}
type Metric struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	Kind           string   `json:"kind"`
	Container      string   `json:"container,omitempty"`
	CPU            float64  `json:"cpu"`
	Memory         float64  `json:"memory"`
	CPUCapacity    float64  `json:"cpuCapacity,omitempty"`
	MemoryCapacity float64  `json:"memoryCapacity,omitempty"`
	Timestamp      string   `json:"timestamp"`
	CPURequest     *float64 `json:"cpuRequest"`
	MemoryRequest  *float64 `json:"memoryRequest"`
	CPULimit       *float64 `json:"cpuLimit"`
	MemoryLimit    *float64 `json:"memoryLimit"`
}
type Metrics struct {
	PodTotals   []PodMetric  `json:"podTotals"`
	Available   bool         `json:"available"`
	Source      string       `json:"source"`
	Message     string       `json:"message"`
	Items       []Metric     `json:"items"`
	Allocations []Allocation `json:"allocations"`
	Warnings    []Warning    `json:"warnings"`
}
type PodMetric struct {
	Resource  Resource `json:"resource"`
	State     string   `json:"state"`
	CPU       *float64 `json:"cpu"`
	Memory    *float64 `json:"memory"`
	Timestamp string   `json:"timestamp"`
	Expected  int      `json:"expected"`
	Observed  int      `json:"observed"`
	Missing   []string `json:"missing"`
	Reason    string   `json:"reason"`
}

// Allocation is configured scheduling demand, independent of measured consumption.
// Nil numbers mean unavailable, never zero usage or an unlimited aggregate limit.
type Allocation struct {
	Node              string      `json:"node"`
	Pods              *int        `json:"pods"`
	CPURequest        *float64    `json:"cpuRequest"`
	MemoryRequest     *float64    `json:"memoryRequest"`
	CPUAllocatable    *float64    `json:"cpuAllocatable"`
	MemoryAllocatable *float64    `json:"memoryAllocatable"`
	Scope             string      `json:"scope"`
	CPULimits         LimitBudget `json:"cpuLimits"`
	MemoryLimits      LimitBudget `json:"memoryLimits"`
}

// LimitBudget describes declared spec budgets, never measured usage or a node
// enforcement boundary. Subtotals include only Pods with fully finite budgets.
type LimitBudget struct {
	State         string   `json:"state"`
	Total         *float64 `json:"total"`
	KnownSubtotal *float64 `json:"knownSubtotal"`
	FinitePods    *int     `json:"finitePods"`
	MissingPods   *int     `json:"missingPods"`
	InvalidPods   *int     `json:"invalidPods"`
}

func ID(kind, ns, name string) string { return kind + ":" + ns + ":" + name }
func Summarize(o Object) Resource {
	kind, ns, name := Str(o, "kind"), Str(o, "metadata", "namespace"), Str(o, "metadata", "name")
	r := Resource{ID: ID(kind, ns, name), Kind: kind, Name: name, Namespace: ns, Health: "unknown", Status: "Observed", CreatedAt: Str(o, "metadata", "creationTimestamp"), Labels: Map(o, "metadata", "labels"), Object: o}
	switch kind {
	case "Pod":
		summarizePod(o, &r)
	case "Deployment", "StatefulSet", "ReplicaSet", "DaemonSet":
		summarizeWorkload(o, &r)
	case "Node":
		r.Health = "unknown"
		r.Status = "Unknown"
		for _, x := range Arr(o, "status", "conditions") {
			c, _ := x.(map[string]any)
			if Str(c, "type") == "Ready" {
				r.Health = "healthy"
				r.Status = "Ready"
				if Str(c, "status") != "True" {
					r.Health = "critical"
					r.Status = "NotReady"
				}
			}
		}
		if r.Health == "healthy" {
			for _, x := range Arr(o, "status", "conditions") {
				c, _ := x.(map[string]any)
				if Str(c, "status") == "True" && (Str(c, "type") == "MemoryPressure" || Str(c, "type") == "DiskPressure" || Str(c, "type") == "PIDPressure" || Str(c, "type") == "NetworkUnavailable") {
					r.Health, r.Status = "warning", Str(c, "type")
				}
			}
		}
	case "Job":
		r.Health = "healthy"
		r.Status = "Active"
		for _, x := range Arr(o, "status", "conditions") {
			c, _ := x.(map[string]any)
			if Str(c, "status") != "True" {
				continue
			}
			switch Str(c, "type") {
			case "Complete":
				r.Status = "Completed"
			case "Failed", "FailureTarget":
				r.Health, r.Status = "critical", "Failed"
			}
		}
	case "PersistentVolumeClaim":
		r.Status = Str(o, "status", "phase")
		switch r.Status {
		case "Bound":
			r.Health = "healthy"
		case "Pending":
			r.Health = "warning"
		case "Lost":
			r.Health = "critical"
		}
	case "PersistentVolume", "Namespace":
		r.Status = Str(o, "status", "phase")
		r.Health = "healthy"
		if r.Status == "Pending" || r.Status == "Failed" {
			r.Health = "warning"
		}
	case "Service", "Ingress", "CronJob", "ConfigMap", "ServiceAccount", "Secret", "NetworkPolicy":
		r.Status = "Observed"
		r.Health = "unknown"
	}
	return r
}

// Sanitize removes Secret payloads and annotations (including last-applied manifests),
// and direct environment variable values in every API, UI, and MCP response.
func Sanitize(o Object) Object {
	b, err := json.Marshal(o)
	if err != nil {
		return nil
	}
	var copy Object
	if json.Unmarshal(b, &copy) != nil {
		return nil
	}
	copy = projectGitOps(copy)
	delete(Map(copy, "metadata"), "managedFields")
	delete(Map(copy, "metadata"), "annotations")
	if Str(copy, "kind") == "Secret" {
		delete(copy, "data")
		delete(copy, "stringData")
		delete(copy, "binaryData")
	}
	var walk func(any)
	walk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			delete(Map(n, "metadata"), "annotations")
			delete(Map(n, "metadata"), "managedFields")
			for _, header := range Arr(n, "httpHeaders") {
				if h, ok := header.(map[string]any); ok {
					if _, exists := h["value"]; exists {
						h["value"] = "[REDACTED]"
					}
				}
			}
			if env, ok := n["env"].([]any); ok {
				for _, e := range env {
					if m, ok := e.(map[string]any); ok {
						if _, yes := m["value"]; yes {
							m["value"] = "[REDACTED]"
						}
					}
				}
			}
			for _, x := range n {
				walk(x)
			}
		case []any:
			for _, x := range n {
				walk(x)
			}
		}
	}
	walk(map[string]any(copy))
	return copy
}

// Connection describes external API reachability separately from the UI stream.
type Connection struct {
	RetentionWarnings    []Warning      `json:"retentionWarnings"`
	RetainedBytes        int            `json:"retainedBytes"`
	RetentionBudgetBytes int            `json:"retentionBudgetBytes"`
	State                string         `json:"state"`
	Mode                 string         `json:"mode"`
	Context              string         `json:"context"`
	Namespace            string         `json:"namespace"`
	Auth                 string         `json:"auth"`
	ServerVersion        string         `json:"serverVersion,omitempty"`
	Discovery            string         `json:"discovery"`
	Message              string         `json:"message"`
	LastSuccessfulRead   *time.Time     `json:"lastSuccessfulRead,omitempty"`
	CheckedAt            time.Time      `json:"checkedAt"`
	CollectionFailures   []Warning      `json:"collectionFailures"`
	Streams              []StreamStatus `json:"streams"`
}
type StreamStatus struct {
	Kind      string     `json:"kind"`
	Namespace string     `json:"namespace"`
	State     string     `json:"state"`
	Message   string     `json:"message,omitempty"`
	LastSync  *time.Time `json:"lastSync,omitempty"`
}
