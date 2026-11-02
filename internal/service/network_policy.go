package service

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"strconv"
	"timone/internal/model"
)

type policyResult int

const (
	policyNo policyResult = iota
	policyYes
	policyUnknown
)

func policyAnd(a, b policyResult) policyResult {
	if a == policyNo || b == policyNo {
		return policyNo
	}
	if a == policyUnknown || b == policyUnknown {
		return policyUnknown
	}
	return policyYes
}
func selectorResult(raw map[string]any, values map[string]any) policyResult {
	if raw == nil {
		return policyUnknown
	}
	if len(raw) == 0 {
		return policyYes
	}
	if len(model.Map(raw, "matchLabels")) > 128 || len(model.Arr(raw, "matchExpressions")) > 128 {
		return policyUnknown
	}
	if values == nil {
		return policyUnknown
	}
	var selector metav1.LabelSelector
	if runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &selector) != nil {
		return policyUnknown
	}
	parsed, e := metav1.LabelSelectorAsSelector(&selector)
	if e != nil {
		return policyUnknown
	}
	set := labels.Set{}
	for key, raw := range values {
		value, ok := raw.(string)
		if !ok {
			return policyUnknown
		}
		set[key] = value
	}
	if parsed.Matches(set) {
		return policyYes
	}
	return policyNo
}
func podPort(pod model.Object, name, protocol string) (int, bool) {
	found := 0
	for _, field := range []string{"containers", "initContainers"} {
		for _, raw := range model.Arr(pod, "spec", field) {
			c, _ := raw.(map[string]any)
			if field == "initContainers" && model.Str(c, "restartPolicy") != "Always" {
				continue
			}
			for _, raw := range model.Arr(c, "ports") {
				port, _ := raw.(map[string]any)
				if model.Str(port, "name") != name || defaultString(port, "protocol", "TCP") != protocol {
					continue
				}
				n := model.Num(port, "containerPort")
				if n < 1 || n > 65535 || n != float64(int(n)) {
					return 0, false
				}
				if found != 0 && found != int(n) {
					return 0, false
				}
				found = int(n)
			}
		}
	}
	return found, found != 0
}
func policyPort(raw map[string]any, destination model.Object, port int, protocol string) policyResult {
	if defaultString(raw, "protocol", "TCP") != protocol {
		return policyNo
	}
	value := model.Value(raw, "port")
	if value == nil {
		return policyYes
	}
	switch value.(type) {
	case string:
		resolved, ok := podPort(destination, model.Str(raw, "port"), protocol)
		if !ok {
			return policyUnknown
		}
		if model.Value(raw, "endPort") != nil {
			return policyUnknown
		}
		if port == resolved {
			return policyYes
		}
		return policyNo
	default:
		n := model.Num(raw, "port")
		end := n
		if model.Value(raw, "endPort") != nil {
			end = model.Num(raw, "endPort")
		}
		if n < 1 || end < n || end > 65535 || n != float64(int(n)) || end != float64(int(end)) {
			return policyUnknown
		}
		if port >= int(n) && port <= int(end) {
			return policyYes
		}
		return policyNo
	}
}
func policyPeer(raw map[string]any, peer model.Object, policyNS string, namespaces map[string]model.Object) policyResult {
	if model.Value(raw, "ipBlock") != nil {
		return policyUnknown
	}
	ps, ns := model.Map(raw, "podSelector"), model.Map(raw, "namespaceSelector")
	if ps == nil && ns == nil {
		return policyYes
	}
	if peer == nil {
		return policyUnknown
	}
	peerNS := model.Str(peer, "metadata", "namespace")
	namespaceResult := policyYes
	if ns != nil {
		namespaceResult = selectorResult(ns, objectLabels(namespaces[peerNS]))
	} else if peerNS != policyNS {
		namespaceResult = policyNo
	}
	podResult := policyYes
	if ps != nil {
		podResult = selectorResult(ps, objectLabels(peer))
	}
	return policyAnd(namespaceResult, podResult)
}

type PolicyDirection struct {
	State    string            `json:"state"`
	Policies []model.Resource  `json:"policies"`
	Checks   []AssessmentCheck `json:"checks"`
}

func evaluatePolicies(direction string, subject, peer, destination model.Object, port int, protocol string, policies []model.Object, complete bool, namespaces map[string]model.Object) PolicyDirection {
	out := PolicyDirection{State: "unknown", Policies: []model.Resource{}, Checks: []AssessmentCheck{}}
	if subject == nil {
		out.State = "source-required"
		out.Checks = append(out.Checks, check("policy-source", "unknown", "Choose a readable source Pod to evaluate this direction"))
		return out
	}
	if model.Bool(subject, "spec", "hostNetwork") || model.Bool(peer, "spec", "hostNetwork") {
		out.Checks = append(out.Checks, check("host-network", "unknown", "hostNetwork policy behavior depends on the network implementation"))
		return out
	}
	if peer != nil && model.Str(subject, "metadata", "uid") != "" && model.Str(subject, "metadata", "uid") == model.Str(peer, "metadata", "uid") && model.Str(subject, "metadata", "namespace") == model.Str(peer, "metadata", "namespace") && model.Str(subject, "metadata", "name") == model.Str(peer, "metadata", "name") {
		out.State = "same-pod-traffic"
		out.Checks = append(out.Checks, check("same-pod", "observed", "Native NetworkPolicy does not isolate a Pod from itself; this does not establish connectivity"))
		return out
	}
	selected, unknown, allowed := false, !complete, false
	work := 0
policyLoop:
	for _, policy := range policies {
		work++
		if work > 2048 {
			unknown = true
			break
		}
		if model.Str(policy, "metadata", "namespace") != model.Str(subject, "metadata", "namespace") {
			continue
		}
		applies := false
		types := model.Arr(policy, "spec", "policyTypes")
		if len(types) == 0 {
			applies = direction == "ingress" || (direction == "egress" && len(model.Arr(policy, "spec", "egress")) > 0)
		} else {
			for _, t := range types {
				applies = applies || (direction == "ingress" && t == "Ingress") || (direction == "egress" && t == "Egress")
			}
		}
		if !applies {
			continue
		}
		match := selectorResult(model.Map(policy, "spec", "podSelector"), objectLabels(subject))
		if match == policyUnknown {
			unknown = true
			continue
		}
		if match == policyNo {
			continue
		}
		selected = true
		if len(out.Policies) < 20 {
			out.Policies = append(out.Policies, assessmentResource(policy))
		}
		rules := model.Arr(policy, "spec", direction)
		if len(rules) > 128 {
			unknown = true
			continue
		}
		for _, raw := range rules {
			work++
			if work > 2048 {
				unknown = true
				break policyLoop
			}
			rule, _ := raw.(map[string]any)
			ports := model.Arr(rule, "ports")
			portMatch := policyYes
			if len(ports) > 0 {
				portMatch = policyNo
				if len(ports) > 128 {
					portMatch = policyUnknown
				} else {
					for _, raw := range ports {
						work++
						if work > 2048 {
							unknown = true
							break policyLoop
						}
						p, _ := raw.(map[string]any)
						match := policyPort(p, destination, port, protocol)
						if match == policyYes {
							portMatch = policyYes
							break
						}
						if match == policyUnknown {
							portMatch = policyUnknown
						}
					}
				}
			}
			key := "from"
			if direction == "egress" {
				key = "to"
			}
			peers := model.Arr(rule, key)
			peerMatch := policyYes
			if len(peers) > 0 {
				peerMatch = policyNo
				if len(peers) > 128 {
					peerMatch = policyUnknown
				} else {
					for _, raw := range peers {
						work++
						if work > 2048 {
							unknown = true
							break policyLoop
						}
						p, _ := raw.(map[string]any)
						match := policyPeer(p, peer, model.Str(policy, "metadata", "namespace"), namespaces)
						if match == policyYes {
							peerMatch = policyYes
							break
						}
						if match == policyUnknown {
							peerMatch = policyUnknown
						}
					}
				}
			}
			match := policyAnd(portMatch, peerMatch)
			if match == policyYes {
				allowed = true
			}
			if match == policyUnknown {
				unknown = true
			}
		}
	}
	if work > 2048 {
		out.Checks = append(out.Checks, check("policy-limit", "unknown", "Policy evaluation limited to 2048 rule/peer/port operations; policy references are a bounded subset"))
	}
	switch {
	case allowed:
		out.State = "allowed-by-observed-rules"
	case unknown:
		out.State = "unknown"
	case !selected:
		out.State = "not-isolated-by-native-policies"
	default:
		out.State = "denied-by-declared-rules"
	}
	out.Checks = append(out.Checks, check("native-"+direction, "observed", "Native policy result for destination "+strconv.Itoa(port)+"/"+protocol+": "+out.State+". This is not observed traffic or CNI enforcement."))
	return out
}

func namespacePolicyLabelsNeeded(policies []model.Object, direction string) bool {
	key := "from"
	if direction == "egress" {
		key = "to"
	}
	for _, policy := range policies {
		for _, raw := range model.Arr(policy, "spec", direction) {
			rule, _ := raw.(map[string]any)
			for _, raw := range model.Arr(rule, key) {
				peer, _ := raw.(map[string]any)
				if len(model.Map(peer, "namespaceSelector")) > 0 {
					return true
				}
			}
		}
	}
	return false
}
