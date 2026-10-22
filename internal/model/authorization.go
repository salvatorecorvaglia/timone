package model

type AccessQuery struct {
	Kind                    string `json:"kind"`
	Namespace               string `json:"namespace"`
	Name                    string `json:"name,omitempty"`
	Verb                    string `json:"verb"`
	Subresource             string `json:"subresource,omitempty"`
	ServiceAccount          string `json:"serviceAccount,omitempty"`
	ServiceAccountNamespace string `json:"serviceAccountNamespace,omitempty"`
}
type AccessReview struct {
	Query      AccessQuery `json:"query"`
	State      string      `json:"state"`
	Identity   string      `json:"identity"`
	Mode       string      `json:"mode"`
	Message    string      `json:"message"`
	ObservedAt string      `json:"observedAt"`
}
