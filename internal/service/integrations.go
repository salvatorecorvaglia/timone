package service

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"timone/internal/integrations"
	"timone/internal/model"
)

func (s *Service) Integrations(ctx context.Context, ns string) (*IntegrationsResponse, error) {
	q := Query{Namespace: ns}
	if err := q.Validate(); err != nil {
		return nil, err
	}
	ns, err := s.Scope(ns)
	if err != nil {
		return nil, err
	}
	items := []model.Integration{}
	for _, spec := range []struct {
		name    string
		kinds   []string
		message string
	}{
		{"Argo CD", []string{"Application"}, "Reported Application synchronization, health, conditions and last operation. Synced does not establish healthy workloads; OutOfSync is not proof of live configuration drift."},
		{"Flux", []string{"Kustomization", "HelmRelease"}, "Reported reconciliation conditions and revisions. Ready applies to the observed generation; suspension and stale conditions are shown explicitly. Drift is reported only when the controller supplies evidence."},
		{"Helm", []string{"Secret"}, "Helm Secret storage and Secret list permission are required. Labels provide release/revision/status metadata. History requires administrator-enabled decoding; values and raw manifests remain omitted."},
	} {
		integration := model.Integration{Name: spec.name, Status: "available", Message: spec.message, Records: []model.IntegrationRecord{}, Warnings: []model.Warning{}, DetailsEnabled: spec.name == "Helm" && s.Provider.HelmDetailsEnabled()}
		successful := 0
		for _, kind := range spec.kinds {
			objects, trunc, err := s.Provider.List(ctx, kind, ns)
			if err != nil {
				integration.Warnings = append(integration.Warnings, model.Warning{Kind: kind, Message: err.Error()})
				continue
			}
			successful++
			integration.Truncated = integration.Truncated || trunc
			for _, o := range objects {
				r := model.Summarize(o)
				if spec.name == "Helm" {
					if model.Str(o, "metadata", "labels", "owner") != "helm" {
						continue
					}
					_, _, valid := integrations.HelmStorageIdentity(o)
					if !valid {
						integration.Warnings = append(integration.Warnings, model.Warning{Kind: "Helm", Message: "Nonconforming Helm storage metadata was omitted"})
						continue
					}
					integration.Records = append(integration.Records, model.IntegrationRecord{Resource: r, Name: model.Str(o, "metadata", "labels", "name"), Revision: model.Str(o, "metadata", "labels", "version"), Status: model.Str(o, "metadata", "labels", "status"), Conditions: []model.IntegrationCondition{}, Evidence: []string{}, Recommendations: []string{}})
				} else {
					integration.Records = append(integration.Records, gitOpsRecord(r))
				}
			}
		}
		if spec.name == "Helm" {
			groups := map[string]model.IntegrationRecord{}
			for _, r := range integration.Records {
				key := r.Resource.Namespace + ":" + r.Name
				old, exists := groups[key]
				current, _ := strconv.Atoi(r.Revision)
				previous, _ := strconv.Atoi(old.Revision)
				if !exists || current > previous {
					r.RetainedRevisions = old.RetainedRevisions + 1
					groups[key] = r
				} else {
					old.RetainedRevisions++
					groups[key] = old
				}
			}
			integration.Records = []model.IntegrationRecord{}
			for _, r := range groups {
				integration.Records = append(integration.Records, r)
			}
		}
		if successful == 0 {
			integration.Status = "unavailable"
		} else if len(integration.Warnings) > 0 || integration.Truncated {
			integration.Status = "partial"
		} else if len(integration.Records) == 0 {
			integration.Status = "no resources"
		}
		if len(integration.Warnings) > 0 {
			if spec.name == "Helm" {
				integration.Message += " Check Secret list permission and Helm Secret storage in the configured scope."
			} else {
				integration.Message += " Install the controller CRDs if absent and grant read access to " + strings.Join(spec.kinds, ", ") + " in the configured scope."
			}
		}
		sort.Slice(integration.Records, func(i, j int) bool {
			a, b := integration.Records[i], integration.Records[j]
			if a.Name == b.Name && a.Resource.Namespace == b.Resource.Namespace && spec.name == "Helm" {
				ai, _ := strconv.Atoi(a.Revision)
				bi, _ := strconv.Atoi(b.Revision)
				return ai > bi
			}
			return a.Resource.ID < b.Resource.ID
		})
		items = append(items, integration)
	}
	services, trunc, err := s.Provider.List(ctx, "Service", ns)
	detected := false
	warnings := []model.Warning{}
	if err != nil {
		warnings = append(warnings, model.Warning{Kind: "Service", Message: err.Error()})
	} else {
		for _, o := range services {
			if contains(model.Str(o, "metadata", "name"), "prometheus") || model.Str(o, "metadata", "labels", "app.kubernetes.io/name") == "prometheus" {
				detected = true
			}
		}
	}
	history := s.HistoryStatus()
	items = append(items, model.Integration{Name: "Prometheus", Status: history.State, Detected: &detected, Message: history.Message + " Service discovery alone does not establish query integration health. Inspect resource history in Metrics.", Records: []model.IntegrationRecord{}, Warnings: warnings, Truncated: trunc})
	return &IntegrationsResponse{Items: items, Mode: s.Provider.Mode()}, ctx.Err()
}

func gitOpsRecord(r model.Resource) model.IntegrationRecord {
	o := r.Object
	out := model.IntegrationRecord{Resource: r, Name: r.Name, Status: "Unknown", Drift: "Not assessed", Conditions: []model.IntegrationCondition{}, Evidence: []string{}, Recommendations: []string{}}
	generation := model.Num(o, "metadata", "generation")
	out.Observation = "unreported"
	out.ObservedAt = model.Str(o, "status", "reconciledAt")
	if observed := model.Value(o, "status", "observedGeneration"); observed != nil {
		out.Stale = model.Num(o, "status", "observedGeneration") < generation
		out.Observation = "current"
		if out.Stale {
			out.Observation = "stale"
		}
	}
	for _, v := range model.Arr(o, "status", "conditions") {
		c, _ := v.(map[string]any)
		stale := out.Stale
		if model.Value(c, "observedGeneration") != nil {
			stale = model.Num(c, "observedGeneration") < generation
			if !stale && model.Value(o, "status", "observedGeneration") == nil && model.Str(c, "type") == "Ready" {
				out.Observation = "current"
			}
		}
		out.Conditions = append(out.Conditions, model.IntegrationCondition{Type: model.Str(c, "type"), Status: model.Str(c, "status"), Reason: model.Str(c, "reason"), Message: model.Str(c, "message"), Timestamp: model.Str(c, "lastTransitionTime"), Stale: stale})
	}
	if r.Kind == "Application" {
		out.Status = model.Str(o, "status", "sync", "status")
		if out.Status == "" {
			out.Status = "Unknown"
		}
		out.Health = model.Str(o, "status", "health", "status")
		out.Revision = model.Str(o, "status", "sync", "revision")
		if revs := model.Arr(o, "status", "sync", "revisions"); len(revs) > 0 {
			values := []string{}
			for _, v := range revs {
				if s, ok := v.(string); ok {
					values = append(values, s)
				}
			}
			out.Revision = strings.Join(values, ", ")
		}
		out.Operation = model.Str(o, "status", "operationState", "phase")
		if out.Operation != "" {
			out.Evidence = append(out.Evidence, "Last operation: "+out.Operation)
		}
		// Argo CD does not consistently expose an observedGeneration. Do not infer freshness.
		if out.Status == "OutOfSync" {
			out.Evidence = append(out.Evidence, "Controller reports OutOfSync; the desired revision may have changed or live state may differ.")
			out.Recommendations = append(out.Recommendations, "Review Argo CD's resource comparison and desired revision before choosing an operational action.")
		}
		if out.Operation == "Failed" || out.Operation == "Error" {
			out.Evidence = append(out.Evidence, "Last operation error: "+model.Str(o, "status", "operationState", "message"))
			out.Recommendations = append(out.Recommendations, "Inspect the failed operation and controller conditions. A past operation failure does not establish current synchronization failure.")
		}
	} else {
		out.Suspended = model.Bool(o, "spec", "suspend")
		out.Revision = model.Str(o, "status", "lastAppliedRevision")
		out.AttemptedRevision = model.Str(o, "status", "lastAttemptedRevision")
		if r.Kind == "HelmRelease" {
			// Never substitute the attempted revision for the successfully deployed one.
			for _, v := range model.Arr(o, "status", "history") {
				h, _ := v.(map[string]any)
				if model.Str(h, "status") == "deployed" {
					out.Revision = model.Str(h, "chartVersion")
					break
				}
			}
		}
		ready, stalled, reconciling, failed := false, false, false, false
		for _, c := range out.Conditions {
			if c.Stale {
				continue
			}
			switch c.Type {
			case "Ready":
				ready = c.Status == "True"
				failed = c.Status == "False"
			case "Stalled":
				stalled = c.Status == "True"
			case "Reconciling":
				reconciling = c.Status == "True"
			case "Drifted":
				if c.Status == "True" {
					out.Drift = "Controller reports drift"
				} else if c.Status == "False" {
					out.Drift = "Controller reports no drift"
				}
			}
		}
		switch {
		case out.Suspended:
			out.Status = "Suspended"
		case stalled:
			out.Status = "Stalled"
		case failed:
			out.Status = "Not ready"
		case reconciling:
			out.Status = "Reconciling"
		case out.Stale && out.Observation != "current":
			out.Status = "Awaiting observation"
		case ready:
			out.Status = "Ready"
		}
		if out.Status == "Ready" && out.Observation == "unreported" {
			out.Status = "Ready (reported)"
		}

		if out.Suspended {
			out.Evidence = append(out.Evidence, "spec.suspend=true; reconciliation is paused by configuration.")
		}
		if out.Stale {
			out.Evidence = append(out.Evidence, "Controller observedGeneration is behind metadata.generation; previous success does not establish current readiness.")
		}
		if failed || stalled {
			out.Recommendations = append(out.Recommendations, "Inspect current failure conditions, source availability and dependency readiness before requesting reconciliation.")
		}
	}
	for _, c := range out.Conditions {
		if c.Stale {
			continue
		}
		if strings.HasSuffix(c.Type, "Error") || (c.Status == "False" && c.Type == "Ready") || (c.Type == "Stalled" && c.Status == "True") {
			out.Evidence = append(out.Evidence, c.Type+" "+c.Reason+": "+c.Message)
		}
	}
	sort.Slice(out.Conditions, func(i, j int) bool { return out.Conditions[i].Type < out.Conditions[j].Type })
	return out
}
