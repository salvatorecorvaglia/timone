package model

type ResourceChange struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
}
type Invalidation struct {
	Revision uint64           `json:"revision"`
	Full     bool             `json:"full,omitempty"`
	Scopes   []ResourceChange `json:"scopes,omitempty"`
}
