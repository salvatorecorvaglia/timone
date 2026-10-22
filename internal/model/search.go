package model

import "time"

// SearchHit deliberately excludes manifests, labels, conditions and log content.
type SearchHit struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Health     string `json:"health"`
	Status     string `json:"status"`
	Definition string `json:"definition,omitempty"`
	Version    string `json:"version,omitempty"`
}

type SearchCoverage struct {
	Kind       string `json:"kind"`
	Definition string `json:"definition,omitempty"`
	State      string `json:"state"`
	Observed   *int   `json:"observed,omitempty"`
	Matched    *int   `json:"matched,omitempty"`
}

type SearchResult struct {
	Items      []SearchHit      `json:"items"`
	Total      int              `json:"total"`
	Limit      int              `json:"limit"`
	Offset     int              `json:"offset"`
	Truncated  bool             `json:"truncated"`
	Complete   bool             `json:"complete"`
	State      string           `json:"state"`
	Namespace  string           `json:"namespace"`
	Coverage   []SearchCoverage `json:"coverage"`
	Warnings   []Warning        `json:"warnings"`
	ObservedAt time.Time        `json:"observedAt"`
}
