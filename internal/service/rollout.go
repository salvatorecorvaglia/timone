package service

import (
	"context"
	"fmt"
	"sort"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type RolloutRevision struct {
	Resource model.Resource `json:"resource"`
	Revision string         `json:"revision"`
	Role     string         `json:"role"`
	Replicas *float64       `json:"replicas"`
	Ready    *float64       `json:"ready"`
	Images   []string       `json:"images"`
}
type RolloutPod struct {
	Resource model.Resource `json:"resource"`
	Revision string         `json:"revision"`
	Images   []string       `json:"images"`
	ImageIDs []string       `json:"imageIDs"`
}
type RolloutAssessment struct {
	Resource           model.Resource    `json:"resource"`
	ObservedAt         time.Time         `json:"observedAt"`
	State              string            `json:"state"`
	Generation         *float64          `json:"generation"`
	ObservedGeneration *float64          `json:"observedGeneration"`
	Desired            *float64          `json:"desired"`
	Updated            *float64          `json:"updated"`
	Ready              *float64          `json:"ready"`
	Available          *float64          `json:"available"`
	CurrentRevision    string            `json:"currentRevision"`
	TargetRevision     string            `json:"targetRevision"`
	Strategy           string            `json:"strategy"`
	Partition          float64           `json:"partition"`
	TargetImages       []string          `json:"targetImages"`
	Revisions          []RolloutRevision `json:"revisions"`
	Pods               []RolloutPod      `json:"pods"`
	Checks             []AssessmentCheck `json:"checks"`
	Issues             []model.Issue     `json:"issues"`
	Events             []model.Event     `json:"events"`
	Warnings           []model.Warning   `json:"warnings"`
	Limitations        []string          `json:"limitations"`
}

func count(o model.Object, path ...string) *float64 {
	if model.Value(o, path...) == nil {
		return nil
	}
	n := model.Num(o, path...)
	return &n
}
func templateImages(spec map[string]any) []string {
	out := []string{}
	for _, field := range []string{"containers", "initContainers"} {
		for _, raw := range model.Arr(spec, field) {
			c, _ := raw.(map[string]any)
			out = append(out, model.Str(c, "name")+"="+model.Str(c, "image"))
		}
	}
	sort.Strings(out)
	return out
}
func (s *Service) Rollout(ctx context.Context, kind, ns, name string) (RolloutAssessment, error) {
	ctx = provider.WithReadBudget(ctx, provider.InvestigationBytes)
	out := RolloutAssessment{ObservedAt: time.Now().UTC(), State: "unknown", TargetImages: []string{}, Revisions: []RolloutRevision{}, Pods: []RolloutPod{}, Checks: []AssessmentCheck{}, Issues: []model.Issue{}, Events: []model.Event{}, Warnings: []model.Warning{}, Limitations: []string{
		"Current readable revisions and Pod identities, not durable release history or proof of application availability.",
		"Image tags and runtime image IDs are reported independently; tag equality does not prove binary equality. Missing counters, stale generations and partial inventories cannot establish rollout completion.",
		"ControllerRevision data is omitted. ReplicaSets and ControllerRevisions are associated only through current controller UID ownership.",
	}}
	if kind != "Deployment" && kind != "StatefulSet" && kind != "DaemonSet" {
		return out, &provider.Error{Code: 400, Message: "Rollout analysis supports Deployment, StatefulSet and DaemonSet"}
	}
	root, pods, warnings, e := s.workloadPods(ctx, kind, ns, name)
	if e != nil {
		return out, e
	}
	out.Warnings = warnings
	out.Resource = assessmentResource(root)
	out.Generation = count(root, "metadata", "generation")
	out.ObservedGeneration = count(root, "status", "observedGeneration")
	out.TargetImages = templateImages(model.Map(root, "spec", "template", "spec"))
	out.CurrentRevision = model.Str(root, "status", "currentRevision")
	out.TargetRevision = model.Str(root, "status", "updateRevision")
	if kind == "Deployment" {
		out.Strategy = model.Str(root, "spec", "strategy", "type")
		if out.Strategy == "" {
			out.Strategy = "RollingUpdate"
		}
		out.TargetRevision = model.Str(root, "metadata", "annotations", "deployment.kubernetes.io/revision")
	} else {
		out.Strategy = model.Str(root, "spec", "updateStrategy", "type")
		if out.Strategy == "" {
			out.Strategy = "RollingUpdate"
		}
	}
	if kind == "DaemonSet" {
		out.Desired = count(root, "status", "desiredNumberScheduled")
		out.Updated = count(root, "status", "updatedNumberScheduled")
		out.Ready = count(root, "status", "numberReady")
		out.Available = count(root, "status", "numberAvailable")
	} else {
		out.Desired = count(root, "spec", "replicas")
		if out.Desired == nil {
			n := 1.0
			out.Desired = &n
		}
		out.Updated = count(root, "status", "updatedReplicas")
		out.Ready = count(root, "status", "readyReplicas")
		out.Available = count(root, "status", "availableReplicas")
	}
	out.Partition = model.Num(root, "spec", "updateStrategy", "rollingUpdate", "partition")
	revisionKind := "ControllerRevision"
	if kind == "Deployment" {
		revisionKind = "ReplicaSet"
	}
	revisions, _ := s.assessmentList(ctx, revisionKind, ns, 5000, &out.Warnings)
	resources := []model.Resource{assessmentResource(root)}
	for _, revision := range revisions {
		if !controlledBy(revision, root) {
			continue
		}
		if len(out.Revisions) >= 100 {
			out.Warnings = append(out.Warnings, model.Warning{Kind: revisionKind, Message: "Owned rollout revisions limited to 100"})
			break
		}
		r := RolloutRevision{Resource: assessmentResource(revision), Role: "retained", Images: []string{}, Replicas: count(revision, "spec", "replicas"), Ready: count(revision, "status", "readyReplicas")}
		if kind == "Deployment" {
			r.Revision = model.Str(revision, "metadata", "annotations", "deployment.kubernetes.io/revision")
			r.Images = templateImages(model.Map(revision, "spec", "template", "spec"))
			if out.TargetRevision != "" && r.Revision == out.TargetRevision {
				r.Role = "target"
			}
		} else {
			r.Revision = model.Str(revision, "revision")
			if r.Resource.Name == out.CurrentRevision {
				r.Role = "current"
			}
			if r.Resource.Name == out.TargetRevision {
				if r.Role == "current" {
					r.Role = "current-and-target"
				} else {
					r.Role = "target"
				}
			}
		}
		out.Revisions = append(out.Revisions, r)
		resources = append(resources, assessmentResource(revision))
	}
	for _, pod := range pods {
		r := RolloutPod{Resource: assessmentResource(pod), Revision: model.Str(pod, "metadata", "labels", "controller-revision-hash"), Images: templateImages(model.Map(pod, "spec")), ImageIDs: []string{}}
		if kind == "Deployment" {
			r.Revision = model.Str(pod, "metadata", "labels", "pod-template-hash")
		}
		for _, field := range []string{"containerStatuses", "initContainerStatuses"} {
			for _, raw := range model.Arr(pod, "status", field) {
				c, _ := raw.(map[string]any)
				if image := model.Str(c, "imageID"); image != "" {
					r.ImageIDs = append(r.ImageIDs, model.Str(c, "name")+"="+image)
				}
			}
		}
		out.Pods = append(out.Pods, r)
		resources = append(resources, assessmentResource(pod))
	}
	fresh := out.Generation != nil && out.ObservedGeneration != nil && *out.ObservedGeneration >= *out.Generation
	if !fresh {
		out.Checks = append(out.Checks, check("generation", "unknown", "Controller has not established observation of the desired generation"))
	}
	deadline := false
	for _, raw := range model.Arr(root, "status", "conditions") {
		c, _ := raw.(map[string]any)
		out.Checks = append(out.Checks, check("controller-condition", "observed", model.Str(c, "type")+"="+model.Str(c, "status")+": "+model.Str(c, "reason")+"; "+model.Str(c, "message")))
		if fresh && model.Str(c, "type") == "Progressing" && model.Str(c, "status") == "False" && model.Str(c, "reason") == "ProgressDeadlineExceeded" {
			deadline = true
		}
	}
	switch {
	case model.Bool(root, "spec", "paused"):
		out.State = "paused"
	case !fresh:
		out.State = "awaiting-observation"
	case deadline:
		out.State = "progress-deadline-exceeded"
	case out.Strategy == "OnDelete":
		out.State = "manual-replacement"
		if out.Desired != nil && out.Updated != nil && out.Ready != nil && *out.Updated >= *out.Desired && *out.Ready >= *out.Desired {
			if kind == "StatefulSet" && out.CurrentRevision != "" && out.CurrentRevision == out.TargetRevision {
				out.State = "controller-reports-complete"
			}
			if kind == "DaemonSet" && out.Available != nil && *out.Available >= *out.Desired {
				out.State = "controller-reports-complete"
			}
		}
		out.Checks = append(out.Checks, check("strategy", "advisory", "OnDelete requires explicit Pod replacement; unchanged Pods are not proof of a stalled rolling update"))
	case out.Desired == nil || out.Updated == nil || out.Ready == nil:
		out.State = "unknown"
	default:
		target := *out.Desired
		if kind == "StatefulSet" {
			target -= out.Partition
			if target < 0 {
				target = 0
			}
		}
		complete := *out.Updated >= target && *out.Ready >= *out.Desired
		if kind == "Deployment" {
			total := count(root, "status", "replicas")
			complete = complete && *out.Updated == *out.Desired && out.Available != nil && *out.Available >= *out.Desired && total != nil && *total == *out.Desired
		}
		if kind == "DaemonSet" {
			complete = complete && out.Available != nil && *out.Available >= *out.Desired
		}
		if kind == "StatefulSet" && out.Partition == 0 {
			complete = complete && out.CurrentRevision != "" && out.CurrentRevision == out.TargetRevision
		}
		out.State = "in-progress"
		if complete {
			out.State = "controller-reports-complete"
		}
		if kind == "StatefulSet" && out.Partition > 0 {
			out.State = "partitioned"
			out.Checks = append(out.Checks, check("partition", "advisory", fmt.Sprintf("Partition %.0f intentionally retains lower ordinals; updated target is %.0f replicas", out.Partition, target)))
		}
	}
	events, err := s.Provider.Events(ctx, ns)
	if err != nil {
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Event", Message: err.Error()})
	}
	index := model.IndexEvents(events)
	seenEvents := map[string]bool{}
	for _, r := range resources {
		for _, event := range index.Related(r) {
			if !seenEvents[event.ID] {
				seenEvents[event.ID] = true
				out.Events = append(out.Events, event)
			}
		}
	}
	sort.Slice(out.Events, func(i, j int) bool { return model.NewerEvent(out.Events[i], out.Events[j]) })
	if len(out.Events) > 200 {
		out.Events = out.Events[:200]
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Event", Message: "Rollout events limited to 200"})
	}
	snap := model.Snapshot{Resources: resources, Events: events, ObservedAt: out.ObservedAt}
	out.Issues = s.detectIssues(snap)
	if len(out.Warnings) > 0 && out.State == "controller-reports-complete" {
		out.Checks = append(out.Checks, check("coverage", "unknown", "Controller reports completion, but related inventory or event coverage is partial"))
	}
	return out, ctx.Err()
}
