package model

// GitOps configuration can contain arbitrary inline credentials. Project only
// identity, routing/revision bindings and observed controller evidence. Apply
// this before cache storage, so no transport has to rediscover sensitive fields.
func fields(in map[string]any, names ...string) map[string]any {
	out := map[string]any{}
	for _, name := range names {
		if v, ok := in[name]; ok {
			out[name] = v
		}
	}
	return out
}
func projectGitOps(o Object) Object {
	kind := Str(o, "kind")
	if kind != "Application" && kind != "Kustomization" && kind != "HelmRelease" {
		return o
	}
	spec, status := Map(o, "spec"), Map(o, "status")
	clean := Object{"apiVersion": o["apiVersion"], "kind": kind, "metadata": o["metadata"]}
	if kind == "Application" {
		destination := fields(Map(spec, "destination"), "server", "name", "namespace")
		projectedSpec := map[string]any{"destination": destination, "source": fields(Map(spec, "source"), "targetRevision")}
		if sources := Arr(spec, "sources"); len(sources) > 0 {
			out := []any{}
			for _, raw := range sources {
				m, _ := raw.(map[string]any)
				out = append(out, fields(m, "targetRevision"))
			}
			projectedSpec["sources"] = out
		}
		clean["spec"] = projectedSpec
		projectedStatus := fields(status, "reconciledAt")
		resources := []any{}
		for _, value := range Arr(status, "resources") {
			ref, _ := value.(map[string]any)
			resources = append(resources, fields(ref, "group", "version", "kind", "namespace", "name", "status"))
		}
		projectedStatus["resources"] = resources
		projectedStatus["conditions"] = projectedConditions(status)
		projectedStatus["sync"] = fields(Map(status, "sync"), "status", "revision", "revisions")
		projectedStatus["health"] = fields(Map(status, "health"), "status", "message")
		projectedStatus["operationState"] = fields(Map(status, "operationState"), "phase", "message", "startedAt", "finishedAt")
		clean["status"] = projectedStatus
	} else {
		clean["spec"] = fields(spec, "suspend", "releaseName", "targetNamespace", "storageNamespace")
		if kind == "HelmRelease" {
			Map(clean, "spec")["driftDetection"] = fields(Map(spec, "driftDetection"), "mode")
		}
		projectedStatus := fields(status, "observedGeneration", "lastAppliedRevision", "lastAttemptedRevision", "lastAttemptedConfigDigest")
		projectedStatus["conditions"] = projectedConditions(status)
		entries := []any{}
		for _, value := range Arr(status, "inventory", "entries") {
			ref, _ := value.(map[string]any)
			entries = append(entries, fields(ref, "id", "v"))
		}
		projectedStatus["inventory"] = map[string]any{"entries": entries}
		if history := Arr(status, "history"); len(history) > 0 {
			out := []any{}
			for _, raw := range history {
				m, _ := raw.(map[string]any)
				out = append(out, fields(m, "name", "namespace", "version", "status", "chartVersion", "configDigest"))
			}
			projectedStatus["history"] = out
		}
		clean["status"] = projectedStatus
	}
	return clean
}

// Printer cells may only expose fields inside CustomProjection. Custom specs
// and arbitrary status fields remain private even when their names look benign.
func SafePrinterPath(path string) bool {
	switch path {
	case ".metadata.name", ".metadata.namespace", ".metadata.creationTimestamp", ".metadata.generation", ".status.observedGeneration":
		return true
	}
	switch path {
	case `.status.conditions[?(@.type=="Ready")].status`, `.status.conditions[?(@.type=='Ready')].status`:
		return true
	}
	return false
}

func projectedConditions(status map[string]any) []any {
	out := []any{}
	for _, value := range Arr(status, "conditions") {
		condition, _ := value.(map[string]any)
		out = append(out, fields(condition, "type", "status", "reason", "message", "lastTransitionTime", "observedGeneration"))
	}
	return out
}
