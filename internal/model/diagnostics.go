package model

import "fmt"

type CustomDiagnosticCoverage struct {
	State             string `json:"state"`
	TypesInspected    int    `json:"typesInspected"`
	ResourcesObserved int    `json:"resourcesObserved"`
	ReadinessUnknown  int    `json:"readinessUnknown"`
	CandidateLimit    int    `json:"candidateLimit"`
	CandidatesOmitted int    `json:"candidatesOmitted"`
	Message           string `json:"message"`
}

// DiagnosticPolicy is administrator configuration, fixed for the process lifetime.
// Durations use explicit seconds on the wire; restart counts are cumulative.
type DiagnosticPolicy struct {
	StartupGraceSeconds       int `json:"startupGraceSeconds"`
	WarningEventWindowSeconds int `json:"warningEventWindowSeconds"`
	RestartThreshold          int `json:"restartThreshold"`
}

func DefaultDiagnosticPolicy() DiagnosticPolicy {
	return DiagnosticPolicy{StartupGraceSeconds: 300, WarningEventWindowSeconds: 900, RestartThreshold: 5}
}

func (p DiagnosticPolicy) Validate() error {
	if p.StartupGraceSeconds < 0 || p.StartupGraceSeconds > 3600 {
		return fmt.Errorf("diagnostics startup grace must be 0–3600 whole seconds")
	}
	if p.WarningEventWindowSeconds < 60 || p.WarningEventWindowSeconds > 86400 {
		return fmt.Errorf("diagnostics warning event window must be 60–86400 whole seconds")
	}
	if p.RestartThreshold < 1 || p.RestartThreshold > 10000 {
		return fmt.Errorf("diagnostics cumulative restart threshold must be 1–10000")
	}
	return nil
}
