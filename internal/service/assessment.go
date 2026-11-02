package service

import (
	"context"
	"sort"
	"strconv"
	"timone/internal/model"
)

type AssessmentCheck struct {
	Code    string `json:"code"`
	State   string `json:"state"`
	Message string `json:"message"`
}

func check(code, state, message string) AssessmentCheck { return AssessmentCheck{code, state, message} }
func (s *Service) assessmentList(ctx context.Context, kind, ns string, limit int, warnings *[]model.Warning) ([]model.Object, bool) {
	objects, truncated, e := s.Provider.List(ctx, kind, ns)
	if e != nil {
		*warnings = append(*warnings, model.Warning{Kind: kind, Message: e.Error()})
		return nil, false
	}
	sort.Slice(objects, func(i, j int) bool {
		return model.ID(kind, model.Str(objects[i], "metadata", "namespace"), model.Str(objects[i], "metadata", "name")) < model.ID(kind, model.Str(objects[j], "metadata", "namespace"), model.Str(objects[j], "metadata", "name"))
	})
	if len(objects) > limit {
		objects = objects[:limit]
		truncated = true
	}
	if truncated {
		*warnings = append(*warnings, model.Warning{Kind: kind, Message: "Assessment collection is limited; absence and totals are not established"})
	}
	return objects, !truncated
}
func nodeRequirement(requirement map[string]any, node model.Object, field bool) (bool, bool) {
	key, operator := model.Str(requirement, "key"), model.Str(requirement, "operator")
	var value string
	var present bool
	if field {
		if key != "metadata.name" {
			return false, false
		}
		value = model.Str(node, "metadata", "name")
		present = value != ""
	} else {
		v, ok := model.Map(node, "metadata", "labels")[key]
		value, _ = v.(string)
		present = ok
	}
	if key == "" {
		return false, false
	}
	values := model.Arr(requirement, "values")
	if field && ((operator != "In" && operator != "NotIn") || len(values) != 1) {
		return false, false
	}
	for _, raw := range values {
		if _, ok := raw.(string); !ok {
			return false, false
		}
	}
	has := func() bool {
		for _, v := range values {
			if v == value {
				return true
			}
		}
		return false
	}
	switch operator {
	case "In":
		if len(values) == 0 {
			return false, false
		}
		return present && has(), true
	case "NotIn":
		if len(values) == 0 {
			return false, false
		}
		return !has(), true
	case "Exists":
		if len(values) != 0 {
			return false, false
		}
		return present, true
	case "DoesNotExist":
		if len(values) != 0 {
			return false, false
		}
		return !present, true
	case "Gt", "Lt":
		if len(values) != 1 {
			return false, false
		}
		raw, ok := values[0].(string)
		if !ok {
			return false, false
		}
		threshold, e := strconv.ParseInt(raw, 10, 64)
		if e != nil {
			return false, false
		}
		actual, e := strconv.ParseInt(value, 10, 64)
		if e != nil || !present {
			return false, true
		}
		if operator == "Gt" {
			return actual > threshold, true
		}
		return actual < threshold, true
	}
	return false, false
}

// Required node selector terms are ORed; requirements within each term are ANDed.
// Unknown fields/operators cannot be treated as a successful match.
func requiredNodeSelector(selector map[string]any, node model.Object) (bool, bool) {
	if selector == nil {
		return true, true
	}
	terms := model.Arr(selector, "nodeSelectorTerms")
	if len(terms) > 128 {
		return false, false
	}
	work := 0
	unknown := false
	for _, raw := range terms {
		term, _ := raw.(map[string]any)
		matched, known := true, true
		count := 0
		for _, field := range []string{"matchExpressions", "matchFields"} {
			for _, r := range model.Arr(term, field) {
				work++
				if work > 1024 {
					return false, false
				}
				req, _ := r.(map[string]any)
				ok, understood := nodeRequirement(req, node, field == "matchFields")
				count++
				if understood && !ok {
					matched = false
				}
				known = known && understood
			}
		}
		if count == 0 {
			matched = false
		}
		if matched && known {
			return true, true
		}
		if matched && !known {
			unknown = true
		}
	}
	return false, !unknown
}
func tolerates(pod model.Object, taint map[string]any) bool {
	for _, raw := range model.Arr(pod, "spec", "tolerations") {
		t, _ := raw.(map[string]any)
		if effect := model.Str(t, "effect"); effect != "" && effect != model.Str(taint, "effect") {
			continue
		}
		operator := model.Str(t, "operator")
		if operator == "" {
			operator = "Equal"
		}
		key := model.Str(t, "key")
		if key != "" && key != model.Str(taint, "key") {
			continue
		}
		if operator == "Exists" {
			return true
		}
		if operator == "Equal" && key != "" && model.Str(t, "value") == model.Str(taint, "value") {
			return true
		}
	}
	return false
}
func podReady(pod model.Object) (bool, bool) {
	for _, raw := range model.Arr(pod, "status", "conditions") {
		c, _ := raw.(map[string]any)
		if model.Str(c, "type") == "Ready" {
			return model.Str(c, "status") == "True", true
		}
	}
	return false, false
}

func assessmentResource(o model.Object) model.Resource {
	r := model.Summarize(o)
	r.Labels = nil
	return r
}
