package model

// CloneProjected gives each reader ownership of an already sanitized object.
// Raw Kubernetes objects must pass through Sanitize or CustomProjection first.
// Keeping copying separate avoids repeated JSON encoding on cache hits.
func CloneProjected(o Object) Object {
	if o == nil {
		return nil
	}
	return cloneProjectedValue(map[string]any(o)).(map[string]any)
}

func cloneProjectedValue(v any) any {
	switch value := v.(type) {
	case Object:
		return CloneProjected(value)
	case map[string]any:
		if value == nil {
			return value
		}
		copy := make(map[string]any, len(value))
		for k, v := range value {
			copy[k] = cloneProjectedValue(v)
		}
		return copy
	case []any:
		if value == nil {
			return value
		}
		copy := make([]any, len(value))
		for i, v := range value {
			copy[i] = cloneProjectedValue(v)
		}
		return copy
	case []string:
		if value == nil {
			return value
		}
		return append([]string{}, value...)
	default:
		return value
	}
}
