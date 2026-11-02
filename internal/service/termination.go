package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type DeletionObservation struct {
	State                  string            `json:"state"`
	Timestamp              string            `json:"timestamp"`
	SecondsPastTimestamp   *float64          `json:"secondsPastTimestamp"`
	SecondsUntilTimestamp  *float64          `json:"secondsUntilTimestamp"`
	DeletionGraceSeconds   *float64          `json:"deletionGraceSeconds"`
	ConfiguredGraceSeconds *float64          `json:"configuredGraceSeconds"`
	MetadataFinalizers     []string          `json:"metadataFinalizers"`
	NamespaceFinalizers    []string          `json:"namespaceFinalizers"`
	Checks                 []AssessmentCheck `json:"checks"`
}
type TerminationRelation struct {
	UID       string               `json:"uid,omitempty"`
	Resource  *model.Resource      `json:"resource,omitempty"`
	Kind      string               `json:"kind"`
	Namespace string               `json:"namespace"`
	Name      string               `json:"name"`
	Relation  string               `json:"relation"`
	State     string               `json:"state"`
	Deletion  *DeletionObservation `json:"deletion,omitempty"`
	Checks    []AssessmentCheck    `json:"checks"`
}
type TerminationCollection struct {
	Kind     string `json:"kind"`
	Observed *int   `json:"observed"`
	Complete bool   `json:"complete"`
}
type TerminationAssessment struct {
	UID         string                  `json:"uid"`
	Resource    model.Resource          `json:"resource"`
	ObservedAt  time.Time               `json:"observedAt"`
	State       string                  `json:"state"`
	Deletion    DeletionObservation     `json:"deletion"`
	Relations   []TerminationRelation   `json:"relations"`
	Collections []TerminationCollection `json:"collections"`
	Checks      []AssessmentCheck       `json:"checks"`
	Events      []model.Event           `json:"events"`
	Warnings    []model.Warning         `json:"warnings"`
	Limitations []string                `json:"limitations"`
}

func observeDeletion(o model.Object, now time.Time) DeletionObservation {
	out := DeletionObservation{State: "not-marked-for-deletion", Timestamp: model.Str(o, "metadata", "deletionTimestamp"), DeletionGraceSeconds: count(o, "metadata", "deletionGracePeriodSeconds"), MetadataFinalizers: []string{}, NamespaceFinalizers: []string{}, Checks: []AssessmentCheck{}}
	if model.Str(o, "kind") == "Pod" {
		out.ConfiguredGraceSeconds = count(o, "spec", "terminationGracePeriodSeconds")
	}
	if out.Timestamp != "" {
		t, err := time.Parse(time.RFC3339Nano, out.Timestamp)
		if err != nil {
			out.State = "invalid-deletion-timestamp"
			out.Checks = append(out.Checks, check("deletion-time", "unknown", "Deletion timestamp cannot be parsed"))
		} else if t.After(now) {
			out.State = "deletion-timestamp-in-future"
			seconds := t.Sub(now).Seconds()
			out.SecondsUntilTimestamp = &seconds
		} else {
			out.State = "deletion-pending"
			seconds := now.Sub(t).Seconds()
			out.SecondsPastTimestamp = &seconds
		}
	}
	for _, field := range []string{"metadata", "spec"} {
		if field == "spec" && model.Str(o, "kind") != "Namespace" {
			continue
		}
		values := []string{}
		for i, raw := range model.Arr(o, field, "finalizers") {
			if i >= 64 {
				out.Checks = append(out.Checks, check("finalizer-coverage", "unknown", field+" finalizers limited to 64"))
				break
			}
			value, ok := raw.(string)
			if ok {
				values = append(values, value)
			}
		}
		if field == "metadata" {
			out.MetadataFinalizers = values
		} else {
			out.NamespaceFinalizers = values
		}
		if len(values) > 0 {
			state := "observed"
			if out.Timestamp != "" {
				state = "constraint"
				if out.State == "invalid-deletion-timestamp" {
					state = "unknown"
				}
			}
			out.Checks = append(out.Checks, check("finalizers", state, field+" finalizers are present; their controllers must complete the required finalization. Names alone do not establish controller health or cleanup progress"))
		}
	}
	if out.Timestamp == "" && model.Str(o, "status", "phase") == "Terminating" {
		out.Checks = append(out.Checks, check("phase", "unknown", "Controller reports Terminating without a readable deletion timestamp"))
	}
	return out
}
func (s *Service) Termination(ctx context.Context, kind, ns, name string) (TerminationAssessment, error) {
	ctx = provider.WithReadBudget(ctx, provider.InvestigationBytes)
	out := TerminationAssessment{ObservedAt: time.Now().UTC(), Relations: []TerminationRelation{}, Collections: []TerminationCollection{}, Checks: []AssessmentCheck{}, Events: []model.Event{}, Warnings: []model.Warning{}, Limitations: []string{
		"Read-only deletion evidence. No delete, force-delete, patch, finalize or finalizer removal is performed. Finalizer names are opaque controller contracts, not proof of a broken controller or a safe cleanup action.",
		"deletionTimestamp can be a future graceful-deletion deadline. Time past that timestamp is not time since the delete request; the configured grace period is not added again. No universal duration establishes that deletion is stuck.",
		"Current readable identities and retained events only; reads are not an atomic snapshot. Owner references require matching kind, namespace, API group and nonempty UID. Missing/replaced owners and denied reads remain unverified.",
		"Pod phase, container state, node readiness and declared PVC references do not prove process exit, actual mounts, successful detach or backend cleanup. Kubernetes object disappearance would not prove that remote processes stopped.",
		"Namespace inventory covers supported built-in/integrated namespaced kinds only, not discovery of every API or custom resource. Counts describe bounded readable collections; even an empty inventory cannot establish that a namespace is empty. Namespace conditions are controller reports, not independent API-discovery tests.",
		"Only direct declared owners, Pod-owned PVC children, PVC consumers, verified PV bindings and their name-referenced VolumeAttachments are followed. Other dependents, generic ephemeral claim resolution and external controllers are not reconstructed.",
	}}
	if kind != "Pod" && kind != "PersistentVolumeClaim" && kind != "Namespace" {
		return out, &provider.Error{Code: 400, Message: "Termination analysis supports Pod, PersistentVolumeClaim and Namespace"}
	}
	if err := ValidateResource(kind, ns, name); err != nil {
		return out, err
	}
	scope := ns
	if kind == "Namespace" {
		scope = name
	}
	if _, err := s.Scope(scope); err != nil {
		return out, err
	}
	root, err := s.Provider.Get(ctx, kind, ns, name)
	if err != nil {
		return out, err
	}
	out.Resource = assessmentResource(root)
	out.UID = model.Str(root, "metadata", "uid")
	out.Deletion = observeDeletion(root, out.ObservedAt)
	out.State = out.Deletion.State
	observed := []model.Resource{out.Resource}
	relationLimited := false
	add := func(o model.Object, relation, state string, checks ...AssessmentCheck) {
		if len(out.Relations) >= 200 {
			if !relationLimited {
				out.Warnings = append(out.Warnings, model.Warning{Kind: "Termination", Message: "Related resource evidence limited to 200 entries"})
				relationLimited = true
			}
			return
		}
		r := assessmentResource(o)
		d := observeDeletion(o, out.ObservedAt)
		out.Relations = append(out.Relations, TerminationRelation{UID: model.Str(o, "metadata", "uid"), Resource: &r, Kind: r.Kind, Namespace: r.Namespace, Name: r.Name, Relation: relation, State: state, Deletion: &d, Checks: checks})
		observed = append(observed, r)
	}
	type read struct {
		o   model.Object
		err error
	}
	cache := map[string]read{}
	readsLimited := false
	get := func(k, nspace, n string) (model.Object, bool) {
		id := model.ID(k, nspace, n)
		if v, ok := cache[id]; ok {
			return v.o, v.err == nil
		}
		if len(cache) >= 64 {
			if !readsLimited {
				out.Warnings = append(out.Warnings, model.Warning{Kind: "Termination", Message: "Reference reads limited to 64 distinct identities"})
				readsLimited = true
			}
			return nil, false
		}
		if err := ValidateResource(k, nspace, n); err != nil {
			out.Warnings = append(out.Warnings, model.Warning{Kind: k, Message: "Unsupported or invalid referenced identity"})
			return nil, false
		}
		checkedScope := nspace
		if k == "Namespace" {
			checkedScope = n
		}
		if checkedScope != "" {
			if _, err := s.Scope(checkedScope); err != nil {
				out.Warnings = append(out.Warnings, model.Warning{Kind: k, Message: err.Error()})
				return nil, false
			}
		}
		o, err := s.Provider.Get(ctx, k, nspace, n)
		cache[id] = read{o, err}
		if err != nil {
			out.Warnings = append(out.Warnings, model.Warning{Kind: k, Message: err.Error()})
		}
		return o, err == nil
	}
	list := func(k, nspace string, limit int) ([]model.Object, bool) {
		items, complete := s.assessmentList(ctx, k, nspace, limit, &out.Warnings)
		n := len(items)
		out.Collections = append(out.Collections, TerminationCollection{Kind: k, Observed: &n, Complete: complete})
		return items, complete
	}
	// Resolve direct owner references without treating a same-name replacement as an owner.
	for i, raw := range model.Arr(root, "metadata", "ownerReferences") {
		if i >= 32 {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "Owner", Message: "Owner references limited to 32"})
			break
		}
		ref, _ := raw.(map[string]any)
		k, n := model.Str(ref, "kind"), model.Str(ref, "name")
		nspace := ns
		def, supported := provider.FindKind(k)
		if supported && !def.Namespaced {
			nspace = ""
		}
		item := TerminationRelation{Kind: k, Namespace: nspace, Name: n, Relation: "declared-owner", State: "unverified", Checks: []AssessmentCheck{}}
		version := model.Str(ref, "apiVersion")
		group := strings.Split(version, "/")[0]
		if !strings.Contains(version, "/") {
			group = ""
		}
		if !supported || def.Group != group || model.Str(ref, "uid") == "" || version == "" || (kind == "Namespace" && def.Namespaced) {
			item.Checks = append(item.Checks, check("owner-identity", "unknown", "Owner reference API group/scope/kind or UID cannot be verified"))
		} else if parent, ok := get(k, nspace, n); ok && model.Str(parent, "metadata", "uid") == model.Str(ref, "uid") {
			item.State = "uid-verified"
			item.UID = model.Str(parent, "metadata", "uid")
			r := assessmentResource(parent)
			item.Resource = &r
			d := observeDeletion(parent, out.ObservedAt)
			item.Deletion = &d
			observed = append(observed, r)
			item.Checks = append(item.Checks, check("owner", "observed", fmt.Sprintf("Declared owner UID matches; controller=%t, blockOwnerDeletion=%t. These flags do not establish the deletion propagation policy", model.Bool(ref, "controller"), model.Bool(ref, "blockOwnerDeletion"))))
		} else {
			item.Checks = append(item.Checks, check("owner-identity", "unknown", "Owner is unreadable, absent or replaced; same-name objects are not accepted as owners"))
		}
		out.Relations = append(out.Relations, item)
	}
	switch kind {
	case "Pod":
		if node := model.Str(root, "spec", "nodeName"); node != "" {
			if o, ok := get("Node", "", node); ok {
				checks := []AssessmentCheck{check("node", "observed", "Pod is assigned to this node; actual kubelet cleanup is not verified")}
				for _, raw := range model.Arr(o, "status", "conditions") {
					c, _ := raw.(map[string]any)
					if model.Str(c, "type") == "Ready" {
						state := "observed"
						if model.Str(c, "status") != "True" {
							state = "advisory"
						}
						checks = append(checks, check("node-ready", state, "Reported Ready="+model.Str(c, "status")+": "+model.Str(c, "reason")))
					}
				}
				add(o, "assigned-node", "declared", checks...)
			}
		}
		for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
			for i, raw := range model.Arr(root, "spec", field) {
				if i >= 64 {
					out.Checks = append(out.Checks, check("containers", "unknown", field+" hook evidence limited to 64"))
					break
				}
				c, _ := raw.(map[string]any)
				if model.Value(c, "lifecycle", "preStop") != nil {
					out.Checks = append(out.Checks, check("pre-stop", "advisory", field+"/"+model.Str(c, "name")+" declares a preStop hook; hook execution and completion are not verified. Command and handler details are omitted"))
				}
			}
		}
		for _, field := range []string{"containerStatuses", "initContainerStatuses", "ephemeralContainerStatuses"} {
			for i, raw := range model.Arr(root, "status", field) {
				if i >= 64 {
					out.Checks = append(out.Checks, check("containers", "unknown", field+" status evidence limited to 64"))
					break
				}
				c, _ := raw.(map[string]any)
				for _, state := range []string{"running", "waiting", "terminated"} {
					if model.Map(c, "state", state) != nil {
						out.Checks = append(out.Checks, check("container-state", "observed", model.Str(c, "name")+": "+state+" "+model.Str(c, "state", state, "reason")+" (controller report, not a live process check)"))
					}
				}
			}
		}
		claims := map[string]bool{}
		for i, raw := range model.Arr(root, "spec", "volumes") {
			if i >= 64 {
				out.Checks = append(out.Checks, check("volumes", "unknown", "Pod volume references limited to 64"))
				break
			}
			v, _ := raw.(map[string]any)
			claim := model.Str(v, "persistentVolumeClaim", "claimName")
			if claim != "" && !claims[claim] {
				claims[claim] = true
				if o, ok := get("PersistentVolumeClaim", ns, claim); ok {
					add(o, "declared-claim-reference", "declared", check("claim", "advisory", "Pod names this claim; the reference has no UID and does not prove a current mount"))
				}
			}
		}
		children, _ := list("PersistentVolumeClaim", ns, 1000)
		for _, child := range children {
			if controlledBy(child, root) {
				add(child, "owned-claim-child", "uid-verified", check("ownership", "observed", "Current claim controller owner reference matches this Pod UID"))
			}
		}
	case "PersistentVolumeClaim":
		pods, complete := list("Pod", ns, 5000)
		consumers := 0
		for _, pod := range pods {
			for _, raw := range model.Arr(pod, "spec", "volumes") {
				v, _ := raw.(map[string]any)
				if model.Str(v, "persistentVolumeClaim", "claimName") == name {
					consumers++
					add(pod, "declared-claim-consumer", "declared", check("claim-reference", "advisory", "Pod references this claim by name. Current mount identity and PVC-protection controller decisions are not established; terminal/deleting Pods are retained as evidence"))
					break
				}
			}
		}
		if consumers == 0 {
			state := "observed"
			message := "No explicit Pod claim-name references were observed; this does not prove a claim is unused or safe to delete"
			if !complete {
				state = "unknown"
				message = "Pod inventory is incomplete; claim consumer absence cannot be established"
			}
			out.Checks = append(out.Checks, check("claim-consumers", state, message))
		}
		if pvName := model.Str(root, "spec", "volumeName"); pvName != "" {
			if pv, ok := get("PersistentVolume", "", pvName); ok {
				ref := model.Map(pv, "spec", "claimRef")
				if model.Str(root, "metadata", "uid") != "" && model.Str(ref, "uid") == model.Str(root, "metadata", "uid") && model.Str(ref, "name") == name && model.Str(ref, "namespace") == ns {
					add(pv, "bound-volume", "uid-verified", check("binding", "observed", "PV claimRef matches the current PVC UID; reclaim policy="+model.Str(pv, "spec", "persistentVolumeReclaimPolicy")+". Backend cleanup is unverified"))
					attachments, _ := list("VolumeAttachment", "", 1000)
					for _, a := range attachments {
						if model.Str(a, "spec", "source", "persistentVolumeName") == pvName {
							checks := []AssessmentCheck{check("attachment", "observed", "Attachment references the PV by name; attached="+fmt.Sprint(model.Value(a, "status", "attached"))+". This does not establish detach progress")}
							if message := model.Str(a, "status", "detachError", "message"); message != "" {
								checks = append(checks, check("detach-error", "advisory", "Controller reports: "+message))
							}
							add(a, "published-volume-reference", "declared", checks...)
						}
					}
				} else {
					out.Checks = append(out.Checks, check("binding", "unknown", "PV claimRef does not match this PVC UID; volume and attachment evidence is omitted"))
				}
			}
		}
	case "Namespace":
		for i, raw := range model.Arr(root, "status", "conditions") {
			if i >= 64 {
				out.Checks = append(out.Checks, check("conditions", "unknown", "Namespace conditions limited to 64"))
				break
			}
			c, _ := raw.(map[string]any)
			state := "observed"
			typ := model.Str(c, "type")
			if model.Str(c, "status") == "True" && (typ == "NamespaceDeletionDiscoveryFailure" || typ == "NamespaceDeletionGroupVersionParsingFailure" || typ == "NamespaceDeletionContentFailure" || typ == "NamespaceContentRemaining" || typ == "NamespaceFinalizersRemaining") {
				state = "advisory"
			}
			if model.Value(c, "observedGeneration") != nil && model.Num(c, "observedGeneration") < model.Num(root, "metadata", "generation") {
				state = "unknown"
			}
			out.Checks = append(out.Checks, check("namespace-condition", state, typ+"="+model.Str(c, "status")+": "+model.Str(c, "reason")+"; "+model.Str(c, "message")))
		}
		out.Checks = append(out.Checks, check("namespace-coverage", "unknown", "Supported-kind inventory is not API discovery; custom resources and unavailable APIs may retain namespace content"))
		scanned := 0
		contents := []model.Object{}
		for _, def := range provider.Kinds {
			if !def.Namespaced {
				continue
			}
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			if scanned >= 5000 {
				out.Collections = append(out.Collections, TerminationCollection{Kind: def.Name})
				continue
			}
			limit := 500
			if 5000-scanned < limit {
				limit = 5000 - scanned
			}
			items, _ := list(def.Name, name, limit)
			scanned += len(items)
			contents = append(contents, items...)
		}
		priority := func(o model.Object) int {
			n := 0
			if model.Str(o, "metadata", "deletionTimestamp") != "" {
				n += 2
			}
			if len(model.Arr(o, "metadata", "finalizers")) > 0 {
				n++
			}
			return n
		}
		sort.SliceStable(contents, func(i, j int) bool {
			a, b := priority(contents[i]), priority(contents[j])
			if a != b {
				return a > b
			}
			return model.ID(model.Str(contents[i], "kind"), name, model.Str(contents[i], "metadata", "name")) < model.ID(model.Str(contents[j], "kind"), name, model.Str(contents[j], "metadata", "name"))
		})
		for _, o := range contents {
			add(o, "namespace-content", "observed", check("content", "observed", "Readable object currently present in this namespace; containment is not an owner reference or proof of a deletion blocker"))
		}

		if scanned >= 5000 {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "Namespace", Message: "Namespace inventory processing limited to 5000 objects; remaining kinds are not scanned"})
		}
	}
	events, eventErr := s.Provider.Events(ctx, scope)
	if eventErr != nil {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Event", Message: eventErr.Error()})
	}
	index := model.IndexEvents(events)
	seen := map[string]bool{}
	for _, r := range observed {
		for _, event := range index.Related(r) {
			if !seen[event.ID] {
				seen[event.ID] = true
				out.Events = append(out.Events, event)
			}
		}
	}
	sort.Slice(out.Events, func(i, j int) bool { return model.NewerEvent(out.Events[i], out.Events[j]) })
	if len(out.Events) > 200 {
		out.Events = out.Events[:200]
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Event", Message: "Termination events limited to 200"})
	}
	out.Checks = append(out.Checks, check("events", "observed", "Retained events provide context, not proof of the current cause. Events without a resource UID use name/time correlation"))
	return out, ctx.Err()
}
