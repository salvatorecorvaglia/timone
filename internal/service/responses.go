package service

import (
	"time"
	"timone/internal/model"
)

// Named response contracts preserve the public JSON schema.
type EventsResponse struct {
	Items     []model.Event `json:"items"`
	Total     int           `json:"total"`
	Oldest    string        `json:"oldest"`
	Newest    string        `json:"newest"`
	Retention string        `json:"retention"`
}
type IssuesResponse struct {
	Items             []model.Issue                  `json:"items"`
	CustomDiagnostics model.CustomDiagnosticCoverage `json:"customDiagnostics"`
	DiagnosticPolicy  model.DiagnosticPolicy         `json:"diagnosticPolicy"`
	Warnings          []model.Warning                `json:"warnings"`
	ObservedAt        time.Time                      `json:"observedAt"`
}
type ChangesResponse struct {
	Items     []Change        `json:"items"`
	Total     int             `json:"total"`
	Warnings  []model.Warning `json:"warnings"`
	Mode      string          `json:"mode"`
	StartedAt time.Time       `json:"startedAt"`
	Retention string          `json:"retention"`
}
type AuditResponse struct {
	Items       []model.Finding `json:"items"`
	Warnings    []model.Warning `json:"warnings"`
	Performed   []string        `json:"performed"`
	Unavailable []string        `json:"unavailable"`
	Disclaimer  string          `json:"disclaimer"`
}
type IntegrationsResponse struct {
	Items []model.Integration `json:"items"`
	Mode  string              `json:"mode"`
}
