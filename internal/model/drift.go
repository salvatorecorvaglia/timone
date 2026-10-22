package model

type DesiredBundle struct {
	ControllerUID string   `json:"controllerUID"`
	Generation    int64    `json:"generation"`
	Revision      string   `json:"revision"`
	Source        string   `json:"source"`
	Namespace     string   `json:"namespace"`
	Manifests     []Object `json:"manifests"`
}
type DriftChange struct {
	Resource Resource `json:"resource"`
	Path     string   `json:"path"`
	Desired  any      `json:"desired"`
	Actual   any      `json:"actual"`
	Change   string   `json:"change"`
}
type DriftReport struct {
	ChangesTruncated bool          `json:"changesTruncated"`
	Controller       Resource      `json:"controller"`
	Revision         string        `json:"revision"`
	Source           string        `json:"source"`
	State            string        `json:"state"`
	Mode             string        `json:"mode"`
	Checked          int           `json:"checked"`
	Total            int           `json:"total"`
	Changes          []DriftChange `json:"changes"`
	Warnings         []Warning     `json:"warnings"`
	ObservedAt       string        `json:"observedAt"`
	Limitations      string        `json:"limitations"`
}
