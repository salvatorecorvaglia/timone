package model

import "time"

type HistoryQuery struct {
	Kind      string `json:"kind" jsonschema:"Pod or Node"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Container string `json:"container,omitempty" jsonschema:"Optional current Pod container name; invalid for Nodes"`
	Range     string `json:"range,omitempty" jsonschema:"1h, 6h or 24h; defaults to 1h"`
}
type HistoryCapability struct {
	State   string `json:"state"`
	Source  string `json:"source"`
	Message string `json:"message"`
}
type HistoryPoint struct {
	Timestamp int64    `json:"timestamp"` // Unix seconds
	Value     *float64 `json:"value"`     // Missing/non-finite samples remain null.
}
type HistorySeries struct {
	Metric    string         `json:"metric"`
	Unit      string         `json:"unit"`
	Container string         `json:"container,omitempty"`
	Points    []HistoryPoint `json:"points"`
}
type HistoryData struct {
	State    string          `json:"state"`
	Message  string          `json:"message"`
	Series   []HistorySeries `json:"series"`
	Warnings []Warning       `json:"warnings"`
}
type HistoryTarget struct {
	HistoryQuery
	Containers []string
	Start, End time.Time
	Step       int64
}
type MetricHistory struct {
	HistoryData
	Source   string    `json:"source"`
	Mode     string    `json:"mode"`
	Resource Resource  `json:"resource"`
	Range    string    `json:"range"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Step     int64     `json:"stepSeconds"`
	Identity string    `json:"identity"`
}
