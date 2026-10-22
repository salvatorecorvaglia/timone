package model

// Detail is the shared built-in/custom REST and MCP resource-detail contract.
// Manifest contains only the applicable safe projection, never a raw object.
type Detail struct {
	Resource  Resource    `json:"resource"`
	Manifest  Object      `json:"manifest"`
	Type      *CustomType `json:"type,omitempty"`
	Events    []Event     `json:"events"`
	Warnings  []Warning   `json:"warnings"`
	History   string      `json:"history"`
	Redaction string      `json:"redaction"`
}
