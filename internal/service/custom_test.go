package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"timone/internal/model"
	"timone/internal/provider"
)

func TestCustomResourceServiceFiltersVersionsPaginationAndUIDEvents(t *testing.T) {
	s := New(provider.NewDemo())
	ctx := context.Background()
	catalog, err := s.CustomTypes(ctx)
	if err != nil || len(catalog.Items) != 2 || catalog.Mode != "demo" {
		t.Fatal(catalog, err)
	}
	page, err := s.CustomList(ctx, "databases.platform.timone.io", "v1beta1", Query{Namespace: "production", Status: "warning", Search: "orders", Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Total != 1 || page.Version != "v1beta1" || page.Items[0].Health != "warning" {
		t.Fatal(page, err)
	}
	detail, err := s.CustomDetail(ctx, "databases.platform.timone.io", "v1beta1", "production", "orders-db")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Events) != 1 || model.Str(detail.Manifest, "apiVersion") != "platform.timone.io/v1beta1" {
		t.Fatal("missing correlated event or wrong served version", detail)
	}
	b, _ := json.Marshal(detail)
	if strings.Contains(string(b), "SIMULATED-OMITTED-CUSTOM-SECRET") {
		t.Fatal("custom spec/status leaked")
	}
	page, err = s.CustomList(ctx, "databases.platform.timone.io", "", Query{Search: "orders", Sort: "namespace", Desc: true, Limit: 1, Offset: 1})
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].Namespace != "production" {
		t.Fatal(page, err)
	}
	page, err = s.CustomList(ctx, "databases.platform.timone.io", "", Query{Namespace: "staging", Status: "unknown"})
	if err != nil || len(page.Items) != 1 || page.Items[0].Status != "Stale condition" {
		t.Fatal(page, err)
	}
}

type scopedCustom struct {
	provider.Provider
	provider.CustomProvider
}

type customEventSource struct {
	scopedCustom
	events []model.Event
}

func (s customEventSource) Events(context.Context, string) ([]model.Event, error) {
	return s.events, nil
}
func TestCustomEventsRejectRecreatedAndGroupAmbiguousIdentities(t *testing.T) {
	p := provider.NewDemo()
	base := model.Event{ID: "valid", Resource: "orders-db", Kind: "Database", Namespace: "production", ResourceNamespace: "production", ResourceUID: model.ID("Database", "production", "orders-db")}
	events := []model.Event{base}
	for i := 0; i < 3; i++ {
		copy := base
		copy.ID = "invalid"
		switch i {
		case 0:
			copy.ResourceUID = ""
		case 1:
			copy.ResourceUID = "old-object-uid"
		case 2:
			copy.ResourceNamespace = "staging"
		}
		events = append(events, copy)
	}
	s := New(customEventSource{scopedCustom: scopedCustom{Provider: p, CustomProvider: p.(provider.CustomProvider)}, events: events})
	detail, err := s.CustomDetail(context.Background(), "databases.platform.timone.io", "", "production", "orders-db")
	if err != nil || len(detail.Events) != 1 {
		t.Fatal("ambiguous events correlated", detail, err)
	}
}

func (scopedCustom) Namespace() string { return "production" }
func TestCustomResourceServiceRejectsScopeAndUnsafeInputs(t *testing.T) {
	p := provider.NewDemo()
	s := New(scopedCustom{Provider: p, CustomProvider: p.(provider.CustomProvider)})
	ctx := context.Background()
	for _, tc := range []struct {
		definition, version string
		q                   Query
		code                int
	}{{"../secrets", "", Query{}, 400}, {"databases.platform.timone.io", "../v1", Query{}, 400}, {"databases.platform.timone.io", "v999", Query{}, 400}, {"databases.platform.timone.io", "", Query{Limit: 201}, 400}, {"databases.platform.timone.io", "", Query{Namespace: "staging"}, 403}, {"databases.platform.timone.io", "", Query{Kind: "Pod"}, 400}} {
		_, err := s.CustomList(ctx, tc.definition, tc.version, tc.q)
		if err == nil || provider.Status(err) != tc.code {
			t.Fatalf("%+v error %v", tc, err)
		}
	}
	if _, err := s.CustomDetail(ctx, "databases.platform.timone.io", "", "staging", "orders-db"); provider.Status(err) != 403 {
		t.Fatal("namespace bypass", err)
	}
	// Cluster-scope objects remain readable under a namespaced server ceiling.
	if _, err := s.CustomDetail(ctx, "clusterprofiles.platform.timone.io", "", "", "production-baseline"); err != nil {
		t.Fatal(err)
	}
}
