package service

import (
	"context"
	"fmt"
	"sort"
	"timone/internal/model"
	"timone/internal/provider"
)

type StorageItem struct {
	Resource model.Resource    `json:"resource"`
	Facts    map[string]string `json:"facts"`
}
type StorageResponse struct {
	Events      []model.Event   `json:"events"`
	Items       []StorageItem   `json:"items"`
	Edges       []model.Edge    `json:"edges"`
	Warnings    []model.Warning `json:"warnings"`
	Limitations []string        `json:"limitations"`
	Mode        string          `json:"mode"`
}

func (s *Service) Storage(ctx context.Context, kind, ns, name string) (StorageResponse, error) {
	ctx = provider.WithReadBudget(ctx, provider.InvestigationBytes)
	out := StorageResponse{Events: []model.Event{}, Items: []StorageItem{}, Edges: []model.Edge{}, Warnings: []model.Warning{}, Mode: s.Provider.Mode(), Limitations: []string{"Configuration and controller observations only. Filesystem usage, CSI backend health and actual I/O are not measured.", "Only exact readable references are followed. StorageClass parameters, CSI attributes and Secret contents are omitted."}}
	if e := ValidateResource(kind, ns, name); e != nil {
		return out, e
	}
	if _, e := s.Scope(ns); e != nil {
		return out, e
	}
	if kind != "PersistentVolumeClaim" && kind != "PersistentVolume" && !logTarget(kind) {
		return out, &provider.Error{Code: 400, Message: "Select a Pod, workload, PVC or PV for storage investigation"}
	}
	root, e := s.Provider.Get(ctx, kind, ns, name)
	if e != nil {
		return out, e
	}
	seen := map[string]bool{}
	observed := []model.Resource{}
	reads := 0
	limited := false
	limitRead := func() bool {
		if reads >= 100 {
			if !limited {
				out.Warnings = append(out.Warnings, model.Warning{Kind: "Storage", Message: "Storage investigation limited to 100 reference reads"})
				limited = true
			}
			return false
		}
		reads++
		return true
	}
	volumes := map[string]model.Object{}
	add := func(o model.Object) bool {
		r := model.Summarize(o)
		if seen[r.ID] {
			return true
		}
		if len(out.Items) >= 200 {
			return false
		}
		seen[r.ID] = true
		observed = append(observed, r)
		facts := map[string]string{}
		paths := map[string][]string{}
		switch r.Kind {
		case "PersistentVolumeClaim":
			paths = map[string][]string{"phase": {"status", "phase"}, "volume": {"spec", "volumeName"}, "storageClass": {"spec", "storageClassName"}, "requested": {"spec", "resources", "requests", "storage"}, "capacity": {"status", "capacity", "storage"}}
		case "PersistentVolume":
			paths = map[string][]string{"phase": {"status", "phase"}, "capacity": {"spec", "capacity", "storage"}, "storageClass": {"spec", "storageClassName"}, "reclaimPolicy": {"spec", "persistentVolumeReclaimPolicy"}, "driver": {"spec", "csi", "driver"}}
		case "StorageClass":
			paths = map[string][]string{"provisioner": {"provisioner"}, "bindingMode": {"volumeBindingMode"}, "reclaimPolicy": {"reclaimPolicy"}}
		case "VolumeAttachment":
			paths = map[string][]string{"node": {"spec", "nodeName"}, "driver": {"spec", "attacher"}, "attachError": {"status", "attachError", "message"}, "detachError": {"status", "detachError", "message"}}
			if model.Value(o, "status", "attached") != nil {
				facts["attached"] = fmt.Sprint(model.Bool(o, "status", "attached"))
			}
		}
		for label, path := range paths {
			if v := model.Str(o, path...); v != "" {
				facts[label] = v
			}
		}
		if r.Kind == "PersistentVolumeClaim" || r.Kind == "PersistentVolume" {
			if r.Status == "Pending" || r.Status == "Lost" || r.Status == "Failed" {
				out.Warnings = append(out.Warnings, model.Warning{Kind: r.Kind, Message: r.Name + ": " + r.Status + "; inspect provisioning/binding events"})
			}
		}
		if r.Kind == "VolumeAttachment" && (facts["attachError"] != "" || facts["detachError"] != "") {
			out.Warnings = append(out.Warnings, model.Warning{Kind: r.Kind, Message: r.Name + ": controller reports an attachment error"})
		}
		out.Items = append(out.Items, StorageItem{Resource: r, Facts: facts})
		return true
	}
	edge := func(a, b model.Object, rel string) {
		ar, br := model.Summarize(a), model.Summarize(b)
		out.Edges = append(out.Edges, model.Edge{ID: ar.ID + ">" + br.ID, Source: ar.ID, Target: br.ID, Relation: rel, Confidence: "declared"})
	}
	get := func(k, nspace, n string) (model.Object, bool) {
		if e := ValidateResource(k, nspace, n); e != nil {
			out.Warnings = append(out.Warnings, model.Warning{Kind: k, Message: "Invalid referenced identity"})
			return nil, false
		}
		if nspace != "" {
			if _, e := s.Scope(nspace); e != nil {
				out.Warnings = append(out.Warnings, model.Warning{Kind: k, Message: e.Error()})
				return nil, false
			}
		}
		if !limitRead() {
			return nil, false
		}
		o, e := s.Provider.Get(ctx, k, nspace, n)
		if e != nil {
			out.Warnings = append(out.Warnings, model.Warning{Kind: k, Message: e.Error()})
			return nil, false
		}
		if !add(o) {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "Storage", Message: "Storage resource limit of 200 reached"})
			return nil, false
		}
		return o, true
	}
	// Each claim is fetched by exact name; unrelated namespace objects are never returned.
	claims := map[string]model.Object{}
	if kind == "PersistentVolumeClaim" {
		claims[name] = root
		add(root)
	}
	if kind == "PersistentVolume" {
		add(root)
		volumes[name] = root
	}
	if logTarget(kind) {
		_, pods, warnings, e := s.workloadPods(ctx, kind, ns, name)
		if e != nil {
			return out, e
		}
		out.Warnings = append(out.Warnings, warnings...)
		for _, pod := range pods {
			if !add(pod) {
				out.Warnings = append(out.Warnings, model.Warning{Kind: "Storage", Message: "Storage resource limit reached"})
				break
			}
			for _, raw := range model.Arr(pod, "spec", "volumes") {
				v, _ := raw.(map[string]any)
				claim := model.Str(v, "persistentVolumeClaim", "claimName")
				if model.Value(v, "ephemeral") != nil {
					out.Warnings = append(out.Warnings, model.Warning{Kind: "Storage", Message: "Generic ephemeral volume claim resolution is not included"})
				}
				if claim == "" {
					continue
				}
				pvc, ok := claims[claim]
				if !ok {
					pvc, ok = get("PersistentVolumeClaim", ns, claim)
					if ok {
						claims[claim] = pvc
					}
				}
				if ok {
					edge(pod, pvc, "mounts")
				}
			}
		}
	}
	claimNames := []string{}
	for n := range claims {
		claimNames = append(claimNames, n)
	}
	sort.Strings(claimNames)
	for _, n := range claimNames {
		pvc := claims[n]
		if pvName := model.Str(pvc, "spec", "volumeName"); pvName != "" {
			// Check the binding identity before exposing a volume as belonging to a claim.
			if e := ValidateResource("PersistentVolume", "", pvName); e != nil {
				out.Warnings = append(out.Warnings, model.Warning{Kind: "PersistentVolume", Message: "Invalid volume reference"})
				continue
			}
			if !limitRead() {
				break
			}
			pv, e := s.Provider.Get(ctx, "PersistentVolume", "", pvName)
			if e != nil {
				out.Warnings = append(out.Warnings, model.Warning{Kind: "PersistentVolume", Message: e.Error()})
				continue
			}
			ref := model.Map(pv, "spec", "claimRef")
			if model.Str(ref, "name") != n || model.Str(ref, "namespace") != ns || model.Str(ref, "uid") == "" || model.Str(ref, "uid") != model.Str(pvc, "metadata", "uid") {
				out.Warnings = append(out.Warnings, model.Warning{Kind: "PersistentVolume", Message: "Volume claimRef identity does not match the current claim"})
				continue
			}
			if add(pv) {
				volumes[pvName] = pv
				edge(pvc, pv, "bound to")
			}
		}
		if class := model.Str(pvc, "spec", "storageClassName"); class != "" {
			if sc, ok := get("StorageClass", "", class); ok {
				edge(pvc, sc, "uses class")
			}
		}
	}
	volumeNames := []string{}
	for n := range volumes {
		volumeNames = append(volumeNames, n)
	}
	sort.Strings(volumeNames)
	for _, n := range volumeNames {
		pv := volumes[n]
		if class := model.Str(pv, "spec", "storageClassName"); class != "" {
			if sc, ok := get("StorageClass", "", class); ok {
				edge(pv, sc, "uses class")
			}
		}
	}
	if len(volumes) > 0 {
		attachments, trunc, e := s.Provider.List(ctx, "VolumeAttachment", "")
		if e != nil {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "VolumeAttachment", Message: e.Error()})
		} else {
			if trunc {
				out.Warnings = append(out.Warnings, model.Warning{Kind: "VolumeAttachment", Message: "Attachment inventory truncated"})
			}
			for _, o := range attachments {
				if pv, ok := volumes[model.Str(o, "spec", "source", "persistentVolumeName")]; ok && add(o) {
					edge(pv, o, "attachment")
				}
			}
		}
	}
	events, eventErr := s.Provider.Events(ctx, ns)
	if eventErr != nil {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Event", Message: eventErr.Error()})
	} else {
		index := model.IndexEvents(events)
		eventIDs := map[string]bool{}
		for _, r := range observed {
			for _, event := range index.Related(r) {
				if eventIDs[event.ID] {
					continue
				}
				eventIDs[event.ID] = true
				if len(out.Events) >= 1000 {
					break
				}
				out.Events = append(out.Events, event)
				if event.Type == "Warning" {
					out.Warnings = append(out.Warnings, model.Warning{Kind: r.Kind, Message: event.Reason + ": " + event.Message + " (retained event; verify current state)"})
				}
			}
		}
	}
	sort.Slice(out.Events, func(i, j int) bool { return model.NewerEvent(out.Events[i], out.Events[j]) })
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].Resource.ID < out.Items[j].Resource.ID })
	return out, ctx.Err()
}
