package service

import (
	"context"
	"timone/internal/model"
	"timone/internal/provider"
)

func (s *Service) ReviewAccess(ctx context.Context, q model.AccessQuery) (model.AccessReview, error) {
	bad := func() (model.AccessReview, error) {
		return model.AccessReview{}, &provider.Error{Code: 400, Message: "Select a supported kind, verb, subresource and valid namespace/name"}
	}
	spec, ok := provider.FindKind(q.Kind)
	if !ok {
		return bad()
	}
	switch q.Verb {
	case "get", "list", "watch", "create", "update", "patch", "delete":
	default:
		return bad()
	}
	switch q.Subresource {
	case "":
	case "log":
		if q.Kind != "Pod" || q.Verb != "get" {
			return bad()
		}
	case "scale":
		if q.Kind != "Deployment" && q.Kind != "StatefulSet" && q.Kind != "ReplicaSet" {
			return bad()
		}
	case "status":
	default:
		return bad()
	}
	if q.Name != "" && (len(q.Name) > 253 || !rbacNameRE.MatchString(q.Name)) {
		return bad()
	}
	if err := (&Query{Namespace: q.Namespace}).Validate(); err != nil {
		return model.AccessReview{}, err
	}
	if spec.Namespaced {
		ns, err := s.Scope(q.Namespace)
		if err != nil {
			return model.AccessReview{}, err
		}
		if ns == "" {
			return bad()
		}
		q.Namespace = ns
	} else if q.Namespace != "" {
		return bad()
	}
	if q.ServiceAccount != "" {
		if err := ValidateResource("ServiceAccount", q.ServiceAccountNamespace, q.ServiceAccount); err != nil {
			return model.AccessReview{}, err
		}
		if _, err := s.Scope(q.ServiceAccountNamespace); err != nil {
			return model.AccessReview{}, err
		}
	} else if q.ServiceAccountNamespace != "" {
		return bad()
	}
	p, ok := s.Provider.(provider.AuthorizationProvider)
	if !ok {
		return model.AccessReview{}, &provider.Error{Code: 503, Message: "Live authorization reviews unavailable"}
	}
	return p.ReviewAccess(ctx, q)
}
