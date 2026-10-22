package service

// TryAdmit bounds concurrent API/MCP investigations and fails fast rather than
// accumulating an unbounded queue of goroutines and decoded resource sets.
func (s *Service) TryAdmit() (func(), bool) {
	select {
	case s.admission <- struct{}{}:
		return func() { <-s.admission }, true
	default:
		return nil, false
	}
}
