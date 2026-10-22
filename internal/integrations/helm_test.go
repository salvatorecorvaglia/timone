package integrations

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"timone/internal/model"
)

func wire(payload any, compressed bool) string {
	raw, _ := json.Marshal(payload)
	if compressed {
		var b bytes.Buffer
		w := gzip.NewWriter(&b)
		_, _ = w.Write(raw)
		_ = w.Close()
		raw = b.Bytes()
	}
	return base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(raw)))
}
func payload(manifest string) map[string]any {
	return map[string]any{"name": "app", "namespace": "production", "version": 2, "info": map[string]any{"status": "deployed", "last_deployed": "2026-10-15T12:00:00Z", "notes": "sensitive-marker"}, "config": map[string]any{"password": "sensitive-marker"}, "chart": map[string]any{"metadata": map[string]any{"name": "app", "version": "1.2.0", "appVersion": "2.0"}, "templates": []any{map[string]any{"data": "sensitive-marker"}}}, "manifest": manifest}
}
func TestHelmDecoderRestrictsOutputAndSupportsStorageFormats(t *testing.T) {
	manifest := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: app-secret\n  namespace: production\ndata:\n  password: sensitive-marker\n"
	for _, compressed := range []bool{false, true} {
		r, _, err := DecodeHelm(context.Background(), wire(payload(manifest), compressed), "app", "production", 2)
		if err != nil || !r.InventoryAvailable || len(r.Inventory) != 1 || r.ChartVersion != "1.2.0" {
			t.Fatalf("release decode failed %+v %v", r, err)
		}
		raw, _ := json.Marshal(r)
		if strings.Contains(string(raw), "sensitive-marker") || strings.Contains(string(raw), "templates") || strings.Contains(string(raw), `"config":`) {
			t.Fatalf("release content leaked %s", raw)
		}
	}
}
func TestHelmDecoderRejectsMismatchesMalformedAndOversizedInput(t *testing.T) {
	for _, tc := range []struct {
		encoded, name, ns string
		revision          int
	}{
		{"not-base64", "app", "production", 2},
		{wire(payload(""), true), "other", "production", 2},
		{wire(payload(""), true), "app", "other", 2},
		{wire(payload(""), true), "app", "production", 3},
		{strings.Repeat("A", MaxEncodedBytes+1), "app", "production", 2},
		{wire(payload(strings.Repeat("sensitive-marker", MaxReleaseBytes/15+1)), true), "app", "production", 2},
	} {
		_, _, err := DecodeHelm(context.Background(), tc.encoded, tc.name, tc.ns, tc.revision)
		if err == nil || strings.Contains(err.Error(), "sensitive-marker") {
			t.Fatalf("invalid or unsafe decode error %v", err)
		}
	}
}
func TestHelmInventoryFailureDoesNotReturnPartialInventory(t *testing.T) {
	valid := "apiVersion: v1\nkind: Service\nmetadata:\n  name: app\n"
	for _, manifest := range []string{valid + "---\nbroken: [", valid + "---\n" + valid, "apiVersion: v1\nkind: List\nitems: []", strings.Repeat(valid+"---\n", MaxInventory+1)} {
		r, _, err := DecodeHelm(context.Background(), wire(payload(manifest), true), "app", "production", 2)
		if err != nil || r.InventoryAvailable || len(r.Inventory) != 0 {
			t.Fatalf("ambiguous/partial inventory reported as complete %+v %v", r, err)
		}
	}
}
func TestHelmDecodeHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := DecodeHelm(ctx, wire(payload(""), true), "app", "production", 2); err == nil {
		t.Fatal("cancelled decode succeeded")
	}
}

func TestFailedDecodeStillReportsConsumedBytesForHistoryBudget(t *testing.T) {
	encoded := wire(payload(strings.Repeat("x", MaxReleaseBytes+1)), true)
	_, size, err := DecodeHelm(context.Background(), encoded, "app", "production", 2)
	if err == nil || size < MaxReleaseBytes {
		t.Fatalf("failed decoding evaded aggregate byte accounting: %d %v", size, err)
	}
}

func TestStoredHelmNamespaceIsValidatedIndependently(t *testing.T) {
	p := payload("")
	revision, _, err := DecodeStoredHelm(context.Background(), wire(p, true), "app", 2)
	if err != nil || revision.TargetNamespace != "production" {
		t.Fatal(revision, err)
	}
	p["namespace"] = "invalid/namespace"
	if _, _, err := DecodeStoredHelm(context.Background(), wire(p, true), "app", 2); err == nil {
		t.Fatal("invalid target namespace accepted")
	}
}

func TestHelmStorageMetadataRequiresConformingIdentity(t *testing.T) {
	o := model.Object{"type": "helm.sh/release.v1", "metadata": map[string]any{"namespace": "storage", "name": "sh.helm.release.v1.app.v2", "labels": map[string]any{"owner": "helm", "name": "app", "version": "2"}}}
	if name, rev, valid := HelmStorageIdentity(o); !valid || name != "app" || rev != 2 {
		t.Fatal(name, rev, valid)
	}
	model.Map(o, "metadata")["name"] = "unrelated-secret"
	if _, _, valid := HelmStorageIdentity(o); valid {
		t.Fatal("nonconforming storage metadata accepted")
	}
}
