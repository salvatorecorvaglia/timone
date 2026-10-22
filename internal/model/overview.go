package model

import "time"

// Overview is shared by REST, MCP and generated frontend fixtures.
type Overview struct {
	Cluster           string                   `json:"cluster"`
	Mode              string                   `json:"mode"`
	Scope             string                   `json:"scope"`
	Health            string                   `json:"health"`
	Counts            map[string]int           `json:"counts"`
	PodStates         map[string]int           `json:"podStates"`
	AvailableReplicas int                      `json:"availableReplicas"`
	DesiredReplicas   int                      `json:"desiredReplicas"`
	Namespaces        []string                 `json:"namespaces"`
	Nodes             []Resource               `json:"nodes"`
	Workloads         []Resource               `json:"workloads"`
	Issues            []Issue                  `json:"issues"`
	CustomDiagnostics CustomDiagnosticCoverage `json:"customDiagnostics"`
	DiagnosticPolicy  DiagnosticPolicy         `json:"diagnosticPolicy"`
	Events            []Event                  `json:"events"`
	Warnings          []Warning                `json:"warnings"`
	ObservedAt        time.Time                `json:"observedAt"`
	History           string                   `json:"history"`
}
