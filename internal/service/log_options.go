package service

import (
	"context"
	"time"
	"timone/internal/provider"
)

type advancedLogger interface {
	LogsWithOptions(context.Context, string, string, string, int, bool, string) (string, error)
}

func (s *Service) LogsWithOptions(ctx context.Context, ns, pod, container string, tail int, previous bool, since string) (string, error) {
	if err := ValidateResource("Pod", ns, pod); err != nil {
		return "", err
	}
	if _, err := s.Scope(ns); err != nil {
		return "", err
	}
	if tail < 1 || tail > 1000 || (container != "" && (!nameRE.MatchString(container) || len(container) > 63)) {
		return "", &provider.Error{Code: 400, Message: "Invalid log selection"}
	}
	if since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil || t.After(time.Now()) {
			return "", &provider.Error{Code: 400, Message: "sinceTime must be a past RFC3339 timestamp"}
		}
	}
	if p, ok := s.Provider.(advancedLogger); ok {
		return p.LogsWithOptions(ctx, ns, pod, container, tail, previous, since)
	}
	if s.Provider.Mode() != "demo" && (previous || since != "") {
		return "", &provider.Error{Code: 400, Message: "Advanced logs unavailable"}
	}
	return s.Provider.Logs(ctx, ns, pod, container, tail)
}
