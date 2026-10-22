package service

import (
	"math"
	"timone/internal/model"
)

// configuredLimit is a conservative spec-only budget: every execution stage
// needs positive limits, unless a positive Pod-level limit supplies the budget.
// Missing or zero values are incomplete, invalid quantities are unknown.
func configuredLimit(p model.Object, res string) (*float64, string) {
	containers := model.Arr(p, "spec", "containers")
	if len(containers) == 0 {
		return nil, "unknown"
	}
	sum, sidecars, peak := 0.0, 0.0, 0.0
	missing, invalid := false, false
	read := func(v any) float64 {
		c, ok := v.(map[string]any)
		if !ok {
			invalid = true
			return 0
		}
		if model.Value(c, "resources", "limits", res) == nil {
			missing = true
			return 0
		}
		q := quantity(c, "resources", "limits", res)
		if q == nil {
			invalid = true
			return 0
		}
		if *q == 0 {
			missing = true
		}
		return *q
	}
	for _, c := range containers {
		sum += read(c)
	}
	for _, v := range model.Arr(p, "spec", "initContainers") {
		c, _ := v.(map[string]any)
		q := read(v)
		if model.Str(c, "restartPolicy") == "Always" {
			sidecars += q
			peak = math.Max(peak, sidecars)
		} else {
			peak = math.Max(peak, sidecars+q)
		}
	}
	result := math.Max(sum+sidecars, peak)
	if model.Value(p, "spec", "resources", "limits", res) != nil {
		q := quantity(p, "spec", "resources", "limits", res)
		if q == nil {
			invalid = true
		} else if *q == 0 {
			missing = true
			result = 0
		} else {
			result = *q
			missing = false
		}
	}
	if model.Value(p, "spec", "overhead", res) != nil {
		q := quantity(p, "spec", "overhead", res)
		if q == nil {
			invalid = true
		} else if result > 0 {
			result += *q
		}
	}
	if invalid || math.IsNaN(result) || math.IsInf(result, 0) {
		return nil, "unknown"
	}
	if missing {
		return nil, "incomplete"
	}
	return &result, "finite"
}

func newLimitBudget() model.LimitBudget {
	sum, finite, missing, invalid := 0.0, 0, 0, 0
	return model.LimitBudget{KnownSubtotal: &sum, FinitePods: &finite, MissingPods: &missing, InvalidPods: &invalid}
}
func addLimitBudget(b *model.LimitBudget, p model.Object, res string) {
	q, state := configuredLimit(p, res)
	switch state {
	case "finite":
		*b.FinitePods++
		if b.KnownSubtotal != nil {
			*b.KnownSubtotal += *q
			if math.IsInf(*b.KnownSubtotal, 0) || math.IsNaN(*b.KnownSubtotal) {
				b.KnownSubtotal = nil
			}
		}
	case "incomplete":
		*b.MissingPods++
	default:
		*b.InvalidPods++
	}
}
func finalizeLimitBudget(b *model.LimitBudget, unavailable bool) {
	if unavailable {
		*b = model.LimitBudget{State: "unavailable"}
		return
	}
	b.Total = nil
	switch {
	case *b.InvalidPods > 0 || b.KnownSubtotal == nil:
		b.State = "unknown"
	case *b.MissingPods > 0:
		b.State = "incomplete"
	default:
		b.State = "finite"
		total := *b.KnownSubtotal
		b.Total = &total
	}
}
