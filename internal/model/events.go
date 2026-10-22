package model

// EventIndex narrows candidate events while EventMatches remains the final
// authority for UID, namespace, API kind and replacement-object correlation.
type EventIndex struct {
	byID  map[string][]Event
	byUID map[string][]Event
}

func IndexEvents(events []Event) EventIndex {
	index := EventIndex{byID: map[string][]Event{}, byUID: map[string][]Event{}}
	for _, e := range events {
		index.byID[e.ResourceID] = append(index.byID[e.ResourceID], e)
		if e.ResourceUID != "" {
			index.byUID[e.ResourceUID] = append(index.byUID[e.ResourceUID], e)
		}
	}
	return index
}
func (i EventIndex) Related(r Resource) []Event {
	candidates := i.byID[r.ID]
	if r.Definition != "" {
		candidates = i.byUID[Str(r.Object, "metadata", "uid")]
	}
	out := []Event{}
	for _, e := range candidates {
		if EventMatches(e, r) {
			out = append(out, e)
		}
	}
	return out
}
