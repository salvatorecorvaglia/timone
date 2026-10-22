package cluster_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"timone/internal/model"
	"timone/internal/provider"
	"timone/internal/service"
)

// This is a local export/React-contract test, never real-cluster acceptance.
type restrictedFixtureProvider struct{ provider.Provider }

func (p restrictedFixtureProvider) Mode() string { return "cluster" }
func (p restrictedFixtureProvider) List(ctx context.Context, kind, ns string) ([]model.Object, bool, error) {
	if kind != "Pod" {
		return nil, false, &provider.Error{Code: 403, Message: "Kubernetes denied this collection"}
	}
	return p.Provider.List(ctx, kind, ns)
}
func TestCoverageExportUsesDirectOverviewResponse(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TIMONE_ACCEPTANCE_ARTIFACTS", dir)
	exportCoverage(t, context.Background(), service.New(restrictedFixtureProvider{provider.NewDemo()}))
	data, err := os.ReadFile(filepath.Join(dir, "live-overview.json"))
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Mode     string          `json:"mode"`
		Warnings []model.Warning `json:"warnings"`
		Counts   map[string]int  `json:"counts"`
		Data     any             `json:"data"`
	}
	if err = json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	if response.Mode != "cluster" || len(response.Warnings) == 0 || response.Counts == nil || response.Data != nil {
		t.Fatal("coverage artifact must match the direct API shape consumed by React")
	}
}
