package model

import "time"

type RBACRule struct {
	APIGroups       []string `json:"apiGroups"`
	Resources       []string `json:"resources"`
	Verbs           []string `json:"verbs"`
	ResourceNames   []string `json:"resourceNames"`
	NonResourceURLs []string `json:"nonResourceURLs"`
}
type RBACReference struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}
type RBACGrant struct {
	Binding        Resource      `json:"binding"`
	Role           *Resource     `json:"role,omitempty"`
	Reference      RBACReference `json:"reference"`
	Subjects       []string      `json:"subjects"`
	Scope          string        `json:"scope"`
	Namespace      string        `json:"namespace"`
	Resolution     string        `json:"resolution"`
	Rules          []RBACRule    `json:"rules"`
	RulesTruncated bool          `json:"rulesTruncated"`
	Notes          []string      `json:"notes"`
}
type GrantCoverage struct {
	Kind     string `json:"kind"`
	State    string `json:"state"`
	Observed *int   `json:"observed,omitempty"`
}
type GrantReport struct {
	ServiceAccount Resource        `json:"serviceAccount"`
	Identity       string          `json:"identity"`
	Groups         []string        `json:"groups"`
	Mode           string          `json:"mode"`
	Scope          string          `json:"scope"`
	Items          []RBACGrant     `json:"items"`
	Total          int             `json:"total"`
	Limit          int             `json:"limit"`
	Offset         int             `json:"offset"`
	Complete       bool            `json:"complete"`
	Truncated      bool            `json:"truncated"`
	Coverage       []GrantCoverage `json:"coverage"`
	Warnings       []Warning       `json:"warnings"`
	Limitations    []string        `json:"limitations"`
	ObservedAt     time.Time       `json:"observedAt"`
}
