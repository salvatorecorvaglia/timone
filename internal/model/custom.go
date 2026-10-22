package model

import (
	"math"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// CustomType is identified by its CRD name, not Kind, which is not globally unique.
type CustomType struct {
	Presentations    map[string]CustomPresentation `json:"presentations,omitempty"`
	Definition       string                        `json:"definition"`
	Kind             string                        `json:"kind"`
	Group            string                        `json:"group"`
	Plural           string                        `json:"plural"`
	Namespaced       bool                          `json:"namespaced"`
	Versions         []string                      `json:"versions"`
	PreferredVersion string                        `json:"preferredVersion"`
}

type CustomCatalog struct {
	Items    []CustomType `json:"items"`
	Warnings []Warning    `json:"warnings"`
	Enabled  bool         `json:"enabled"`
	Mode     string       `json:"mode"`
	Message  string       `json:"message"`
}

func ValidCustomDefinition(name string) bool {
	plural, group, ok := strings.Cut(name, ".")
	return ok && len(validation.IsDNS1035Label(plural)) == 0 && len(validation.IsDNS1123Subdomain(group)) == 0 && len(name) <= 253
}

// CustomProjection deliberately omits spec, arbitrary status fields, annotations
// and unknown top-level data: an operator CR can itself hold credentials.
// Labels and controller condition text remain administrator-visible data.
func CustomProjection(o Object) Object {
	metadata := map[string]any{}
	for _, key := range []string{"name", "namespace", "uid", "creationTimestamp", "resourceVersion"} {
		if v, ok := Value(o, "metadata", key).(string); ok {
			metadata[key] = v
		}
	}
	if generation := Value(o, "metadata", "generation"); generation != nil {
		metadata["generation"] = Num(o, "metadata", "generation")
	}
	labels := map[string]any{}
	for key, value := range Map(o, "metadata", "labels") {
		if v, ok := value.(string); ok {
			labels[key] = v
		}
	}
	metadata["labels"] = labels
	conditions := []any{}
	for _, value := range Arr(o, "status", "conditions") {
		c, ok := value.(map[string]any)
		if !ok {
			continue
		}
		condition := map[string]any{}
		for _, key := range []string{"type", "status", "reason", "message", "lastTransitionTime"} {
			if v, ok := c[key].(string); ok {
				condition[key] = v
			}
		}
		if c["observedGeneration"] != nil {
			condition["observedGeneration"] = Num(c, "observedGeneration")
		}
		conditions = append(conditions, condition)
	}
	status := map[string]any{"conditions": conditions}
	if Value(o, "status", "observedGeneration") != nil {
		status["observedGeneration"] = Num(o, "status", "observedGeneration")
	}
	return Object{"apiVersion": Str(o, "apiVersion"), "kind": Str(o, "kind"), "metadata": metadata, "status": status}
}

func SummarizeCustom(o Object, definition string) Resource {
	r := Resource{Definition: definition, Version: customObjectVersion(o), ID: definition + ":" + Str(o, "metadata", "namespace") + ":" + Str(o, "metadata", "name"), Kind: Str(o, "kind"), Name: Str(o, "metadata", "name"), Namespace: Str(o, "metadata", "namespace"), CreatedAt: Str(o, "metadata", "creationTimestamp"), Labels: Map(o, "metadata", "labels"), Object: o, Health: "unknown", Status: "Observed"}
	// Generic Kind names must never invoke a built-in Pod/Secret/etc health rule.
	c, state := CustomReadyCondition(o)
	switch state {
	case "unverified":
		r.Status = "Unverified condition"
	case "stale":
		r.Status = "Stale condition"
	case "current":
		switch Str(c, "status") {
		case "True":
			r.Status, r.Health = "Ready reported", "healthy"
		case "False":
			r.Status, r.Health = "Not ready reported", "warning"
		}
	}
	if cells, ok := o["__timonePrinterCells"].([]string); ok {
		r.PrinterCells = append([]string{}, cells...)
	}
	return r
}

func customObjectVersion(o Object) string {
	_, version, _ := strings.Cut(Str(o, "apiVersion"), "/")
	return version
}

// CustomReadyCondition accepts one Ready report with an exact positive generation.
// A condition's own generation takes precedence over the top-level fallback.
func CustomReadyCondition(o Object) (map[string]any, string) {
	var ready map[string]any
	for _, value := range Arr(o, "status", "conditions") {
		if c, ok := value.(map[string]any); ok && Str(c, "type") == "Ready" {
			if ready != nil {
				return nil, "ambiguous"
			}
			ready = c
		}
	}
	if ready == nil {
		return nil, "missing"
	}
	generation := Num(o, "metadata", "generation")
	observed := Num(ready, "observedGeneration")
	if Value(ready, "observedGeneration") == nil {
		observed = Num(o, "status", "observedGeneration")
	}
	if generation <= 0 || observed <= 0 || generation > 1<<53-1 || observed > 1<<53-1 || math.Trunc(generation) != generation || math.Trunc(observed) != observed {
		return nil, "unverified"
	}
	if observed != generation {
		return nil, "stale"
	}
	return ready, "current"
}

// CustomEventMatches requires UID identity because Kind is not group-unique.
func CustomEventMatches(e Event, r Resource) bool {
	ns := e.ResourceNamespace
	if ns == "" && r.Namespace != "" {
		ns = e.Namespace
	}
	return e.ResourceUID != "" && e.ResourceUID == Str(r.Object, "metadata", "uid") && e.Kind == r.Kind && e.Resource == r.Name && ns == r.Namespace && (e.CustomDefinition == "" || e.CustomDefinition == r.Definition)
}
