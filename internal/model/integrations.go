package model

// Raw payloads stay private. Only sanitized, bounded differences are serialized.
type HelmResource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
}
type HelmRevision struct {
	TargetNamespace        string         `json:"targetNamespace,omitempty"`
	Values                 Object         `json:"-"`
	ValuesAvailable        bool           `json:"valuesAvailable"`
	Configuration          []HelmManifest `json:"-"`
	ConfigurationAvailable bool           `json:"configurationAvailable"`
	Revision               int            `json:"revision"`
	Status                 string         `json:"status"`
	Chart                  string         `json:"chart"`
	ChartVersion           string         `json:"chartVersion"`
	AppVersion             string         `json:"appVersion"`
	DeployedAt             string         `json:"deployedAt"`
	Inventory              []HelmResource `json:"inventory"`
	InventoryAvailable     bool           `json:"inventoryAvailable"`
}
type HelmHistory struct {
	Name      string         `json:"name"`
	Namespace string         `json:"namespace"`
	Mode      string         `json:"mode"`
	Revisions []HelmRevision `json:"revisions"`
	Warnings  []Warning      `json:"warnings"`
	Retention string         `json:"retention"`
}
type FieldChange struct {
	Path   string `json:"path"`
	Change string `json:"change"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}
type HelmComparison struct {
	ValuesChanges           []FieldChange  `json:"valuesChanges"`
	ValuesComparable        bool           `json:"valuesComparable"`
	ValuesTruncated         bool           `json:"valuesTruncated"`
	Changes                 []HelmChange   `json:"changes"`
	ConfigurationComparable bool           `json:"configurationComparable"`
	ChangesTruncated        bool           `json:"changesTruncated"`
	From                    HelmRevision   `json:"from"`
	To                      HelmRevision   `json:"to"`
	Added                   []HelmResource `json:"added"`
	Removed                 []HelmResource `json:"removed"`
	InventoryComparable     bool           `json:"inventoryComparable"`
	Limitations             string         `json:"limitations"`
}
type HelmManifest struct {
	Resource  HelmResource
	Object    Object
	Supported bool
}
type HelmChange struct {
	Resource HelmResource `json:"resource"`
	Path     string       `json:"path"`
	Change   string       `json:"change"`
	Before   any          `json:"before"`
	After    any          `json:"after"`
}
type IntegrationCondition struct {
	Type      string `json:"type"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Stale     bool   `json:"stale"`
}
type IntegrationRecord struct {
	Resource          Resource               `json:"resource"`
	Name              string                 `json:"name"`
	Status            string                 `json:"status"`
	Health            string                 `json:"health,omitempty"`
	Revision          string                 `json:"revision,omitempty"`
	AttemptedRevision string                 `json:"attemptedRevision,omitempty"`
	RetainedRevisions int                    `json:"retainedRevisions,omitempty"`
	Chart             string                 `json:"chart,omitempty"`
	Drift             string                 `json:"drift,omitempty"`
	Operation         string                 `json:"operation,omitempty"`
	Suspended         bool                   `json:"suspended"`
	Stale             bool                   `json:"stale"`
	Observation       string                 `json:"observation"`
	ObservedAt        string                 `json:"observedAt,omitempty"`
	Conditions        []IntegrationCondition `json:"conditions"`
	Evidence          []string               `json:"evidence"`
	Recommendations   []string               `json:"recommendations"`
}
type Integration struct {
	Name           string              `json:"name"`
	Status         string              `json:"status"`
	Message        string              `json:"message"`
	Records        []IntegrationRecord `json:"records"`
	Warnings       []Warning           `json:"warnings"`
	Truncated      bool                `json:"truncated"`
	DetailsEnabled bool                `json:"detailsEnabled"`
	Detected       *bool               `json:"detected,omitempty"`
}
