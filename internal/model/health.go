package model

import "fmt"

// Stable precedence prevents a later container's startup state hiding a failure.
func podReasonPriority(reason string) int {
	switch reason {
	case "ImagePullBackOff", "ErrImagePull", "InvalidImageName":
		return 100
	case "CrashLoopBackOff", "OOMKilled", "Error", "Failed", "CreateContainerConfigError", "RunContainerError", "CreateContainerError":
		return 90
	case "ContainerCreating", "PodInitializing", "Pending":
		return 30
	case "", "Completed":
		return 0
	default:
		return 40
	}
}
func summarizePod(o Object, r *Resource) {
	phase := Str(o, "status", "phase")
	r.Status = phase
	ready, total := 0, len(Arr(o, "spec", "containers"))
	priority := 0
	for _, field := range []string{"containerStatuses", "initContainerStatuses"} {
		for _, x := range Arr(o, "status", field) {
			c, _ := x.(map[string]any)
			r.Restarts += int(Num(c, "restartCount"))
			if field == "containerStatuses" && Bool(c, "ready") {
				ready++
			}
			for _, state := range []string{"waiting", "terminated"} {
				reason := Str(c, "state", state, "reason")
				rank := podReasonPriority(reason)
				if rank > priority || (rank == priority && rank > 0 && reason < r.Status) {
					r.Status = reason
					priority = rank
				}
			}
		}
	}
	r.Ready = fmt.Sprintf("%d/%d", ready, total)
	if phase == "Succeeded" {
		r.Health, r.Status = "healthy", "Completed"
		return
	}
	if phase == "Failed" || priority >= 90 {
		r.Health = "critical"
		if phase == "Failed" && priority == 0 {
			r.Status = "Failed"
		}
		return
	}
	if phase == "Pending" || priority > 0 || (phase == "Running" && ready < total) {
		r.Health = "warning"
		return
	}
	if phase != "Running" || total == 0 {
		return
	}
	// Readiness gates and native sidecars participate in the Pod Ready condition.
	condition := ""
	for _, x := range Arr(o, "status", "conditions") {
		c, _ := x.(map[string]any)
		if Str(c, "type") == "Ready" {
			if condition != "" {
				r.Status = "Unknown readiness"
				return
			}
			condition = Str(c, "status")
		}
	}
	if condition == "False" {
		r.Health, r.Status = "warning", "NotReady"
		return
	}
	if condition != "True" {
		r.Status = "Unknown readiness"
		return
	}
	r.Health = "healthy"
}
func summarizeWorkload(o Object, r *Resource) {
	desired := Num(o, "spec", "replicas")
	if Value(o, "spec", "replicas") == nil {
		desired = 1
	}
	ready := Num(o, "status", "readyReplicas")
	if r.Kind == "DaemonSet" {
		desired = Num(o, "status", "desiredNumberScheduled")
		ready = Num(o, "status", "numberReady")
	}
	r.Ready = fmt.Sprintf("%.0f/%.0f", ready, desired)
	if ready < desired {
		r.Health, r.Status = "warning", "Degraded"
		return
	}
	generation := Num(o, "metadata", "generation")
	if generation <= 0 || Value(o, "status", "observedGeneration") == nil || Num(o, "status", "observedGeneration") != generation {
		r.Status = "Awaiting observation"
		return
	}
	if r.Kind == "DaemonSet" && Value(o, "status", "desiredNumberScheduled") == nil {
		r.Status = "Unknown availability"
		return
	}
	if r.Kind == "Deployment" && (Value(o, "status", "updatedReplicas") == nil && desired > 0 || Num(o, "status", "updatedReplicas") < desired || Num(o, "status", "availableReplicas") < desired) {
		r.Health, r.Status = "warning", "Progressing"
		return
	}
	if r.Kind == "StatefulSet" && Str(o, "spec", "updateStrategy", "type") != "OnDelete" {
		partition := Num(o, "spec", "updateStrategy", "rollingUpdate", "partition")
		mismatch := partition == 0 && Str(o, "status", "currentRevision") != "" && Str(o, "status", "updateRevision") != "" && Str(o, "status", "currentRevision") != Str(o, "status", "updateRevision")
		if Num(o, "status", "updatedReplicas") < max(0, desired-partition) || mismatch {
			r.Health, r.Status = "warning", "Progressing"
			return
		}
	}
	if r.Kind == "DaemonSet" && (Num(o, "status", "numberUnavailable") > 0 || (Str(o, "spec", "updateStrategy", "type") != "OnDelete" && Num(o, "status", "updatedNumberScheduled") < desired)) {
		r.Health, r.Status = "warning", "Progressing"
		return
	}
	r.Health, r.Status = "healthy", "Available"
}
