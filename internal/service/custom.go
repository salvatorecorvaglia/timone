package service

import (
	"context"
	"sort"

	"k8s.io/apimachinery/pkg/util/validation"
	"timone/internal/model"
	"timone/internal/provider"
)

func (s *Service) customProvider() (provider.CustomProvider, error) {
	p, ok := s.Provider.(provider.CustomProvider)
	if !ok {
		return nil, &provider.Error{Code: 503, Message: "Custom resource browsing is unavailable for this provider"}
	}
	return p, nil
}
func validateCustom(definition, version string, q *Query) error {
	if !model.ValidCustomDefinition(definition) || (version != "" && len(validation.IsDNS1035Label(version)) > 0) {
		return &provider.Error{Code: 400, Message: "Invalid custom resource definition or version"}
	}
	if q.Kind != "" {
		return &provider.Error{Code: 400, Message: "Use the definition name to identify a custom type, not kind"}
	}
	return q.Validate()
}
func (s *Service) CustomTypes(ctx context.Context) (model.CustomCatalog, error) {
	p, err := s.customProvider()
	if err != nil {
		return model.CustomCatalog{}, err
	}
	return p.CustomTypes(ctx)
}

type CustomPage struct {
	model.Page
	Type    model.CustomType `json:"type"`
	Version string           `json:"version"`
}

func (s *Service) CustomList(ctx context.Context, definition, version string, q Query) (CustomPage, error) {
	if err := validateCustom(definition, version, &q); err != nil {
		return CustomPage{}, err
	}
	ns, err := s.Scope(q.Namespace)
	if err != nil {
		return CustomPage{}, err
	}
	p, err := s.customProvider()
	if err != nil {
		return CustomPage{}, err
	}
	var objects []model.Object
	var t model.CustomType
	var truncated bool
	if tables, ok := s.Provider.(provider.CustomTableProvider); ok {
		objects, t, truncated, err = tables.CustomTable(ctx, definition, version, ns)
	} else {
		objects, t, truncated, err = p.CustomList(ctx, definition, version, ns)
	}
	if err != nil {
		return CustomPage{}, err
	}
	if version == "" {
		version = t.PreferredVersion
	}
	return CustomPage{Page: resourcePage(objects, truncated, q, func(o model.Object) model.Resource { return model.SummarizeCustom(o, definition) }), Type: t, Version: version}, nil
}
func (s *Service) CustomDetail(ctx context.Context, definition, version, ns, name string) (model.Detail, error) {
	q := Query{Namespace: ns}
	if err := validateCustom(definition, version, &q); err != nil {
		return model.Detail{}, err
	}
	if len(validation.IsDNS1123Subdomain(name)) > 0 {
		return model.Detail{}, &provider.Error{Code: 400, Message: "Invalid custom resource name"}
	}
	if ns != "" {
		if _, err := s.Scope(ns); err != nil {
			return model.Detail{}, err
		}
	}
	p, err := s.customProvider()
	if err != nil {
		return model.Detail{}, err
	}
	o, t, err := p.CustomGet(ctx, definition, version, ns, name)
	if err != nil {
		return model.Detail{}, err
	}
	r := model.SummarizeCustom(o, definition)
	eventNS := ns
	if !t.Namespaced {
		eventNS = s.Provider.Namespace()
	}
	events, eventErr := s.Provider.Events(ctx, eventNS)
	related := []model.Event{}
	for _, ev := range events {
		if model.CustomEventMatches(ev, r) {
			ev.CustomDefinition = definition
			related = append(related, ev)
		}
	}
	sort.Slice(related, func(i, j int) bool { return model.NewerEvent(related[i], related[j]) })
	warnings := []model.Warning{}
	if eventErr != nil {
		warnings = append(warnings, model.Warning{Kind: "Event", Message: eventErr.Error()})
	}
	return model.Detail{Resource: r, Manifest: model.CustomProjection(o), Type: &t, Events: related, Warnings: warnings, History: "Current observed state only; events correlate by UID and may expire. Custom resources refresh by active polling without retained revisions.", Redaction: "Safe projection only: spec, annotations, owner references and arbitrary status fields are omitted. Printer cells expose approved identity/readiness paths only. Labels and controller condition messages remain visible and may contain sensitive operator-provided information. This YAML is not a complete manifest and must not be applied."}, nil
}
