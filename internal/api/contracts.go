package api

import (
	"timone/internal/auth"
	"timone/internal/provider"
)

// These DTOs keep the REST wire contract explicit without coupling it to handler state.
type clusterResponse struct {
	Name  string          `json:"name"`
	Mode  string          `json:"mode"`
	Scope string          `json:"scope"`
	Kinds []provider.Kind `json:"kinds"`
}
type sessionResponse struct {
	Authenticated bool          `json:"authenticated"`
	AuthRequired  bool          `json:"authRequired"`
	Mode          string        `json:"mode"`
	Identity      auth.Identity `json:"identity"`
}
type authenticationResult struct {
	Authenticated bool `json:"authenticated"`
}

type workspaceResponse struct {
	Enabled   bool     `json:"enabled"`
	Context   string   `json:"context"`
	Namespace string   `json:"namespace"`
	Revision  uint64   `json:"revision"`
	Contexts  []string `json:"contexts"`
	Message   string   `json:"message"`
}

type workspaceSwitchRequest struct {
	Context   string  `json:"context"`
	Namespace *string `json:"namespace"`
	Revision  uint64  `json:"revision"`
	Confirm   bool    `json:"confirm"`
}
