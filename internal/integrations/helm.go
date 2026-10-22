package integrations

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/util/validation"
	yaml "k8s.io/apimachinery/pkg/util/yaml"
	"sort"
	"strings"
	"time"
	"timone/internal/model"
	"unicode"
)

const MaxReleaseBytes = 4 << 20
const MaxEncodedBytes = 2 << 20
const MaxInventory = 1000

// DecodeHelm accepts the Kubernetes JSON data.release representation (outer
// Secret base64, then Helm base64, optionally gzip). Unknown fields are ignored;
// no raw contents or parser errors cross this boundary.
func DecodeHelm(ctx context.Context, encoded, name, ns string, revision int) (model.HelmRevision, int, error) {
	return decodeHelm(ctx, encoded, name, ns, revision)
}

// DecodeStoredHelm validates storage identity at the caller and accepts a distinct
// target namespace, as used by Flux. Raw contents never leave this boundary.
func DecodeStoredHelm(ctx context.Context, encoded, name string, revision int) (model.HelmRevision, int, error) {
	return decodeHelm(ctx, encoded, name, "", revision)
}
func decodeHelm(ctx context.Context, encoded, name, ns string, revision int) (model.HelmRevision, int, error) {
	out := model.HelmRevision{Inventory: []model.HelmResource{}}
	used := 0
	invalid := func() (model.HelmRevision, int, error) {
		return out, used, fmt.Errorf("Helm revision payload is invalid, mismatched, or exceeds safe decoding limits")
	}
	if len(encoded) == 0 || len(encoded) > MaxEncodedBytes {
		return invalid()
	}
	outer, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return invalid()
	}
	data, err := base64.StdEncoding.DecodeString(string(outer))
	if err != nil {
		return invalid()
	}
	used = len(data)
	if bytes.HasPrefix(data, []byte{0x1f, 0x8b, 0x08}) {
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return invalid()
		}
		data, err = io.ReadAll(io.LimitReader(r, MaxReleaseBytes+1))
		used = len(data)
		_ = r.Close()
		if err != nil {
			return invalid()
		}
	}
	if len(data) > MaxReleaseBytes {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return out, 0, err
	}
	var payload struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Version   int    `json:"version"`
		Info      struct {
			Status       string `json:"status"`
			LastDeployed string `json:"last_deployed"`
		} `json:"info"`
		Chart struct {
			Metadata struct {
				Name       string `json:"name"`
				Version    string `json:"version"`
				AppVersion string `json:"appVersion"`
			} `json:"metadata"`
		} `json:"chart"`
		Config   model.Object `json:"config"`
		Manifest string       `json:"manifest"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Name != name || (ns != "" && payload.Namespace != ns) || len(validation.IsDNS1123Label(payload.Namespace)) != 0 || payload.Version != revision {
		return invalid()
	}
	m := payload.Chart.Metadata
	for _, s := range []string{m.Name, m.Version, m.AppVersion} {
		if !safeText(s, 256) {
			return invalid()
		}
	}
	status := "unknown"
	for _, s := range []string{"unknown", "deployed", "uninstalled", "superseded", "failed", "uninstalling", "pending-install", "pending-upgrade", "pending-rollback"} {
		if payload.Info.Status == s {
			status = s
		}
	}
	out.Values, err = SanitizeValues(payload.Config)
	out.ValuesAvailable = err == nil
	out.TargetNamespace = payload.Namespace
	out.Revision = revision
	out.Status = status
	out.Chart = m.Name
	out.ChartVersion = m.Version
	out.AppVersion = m.AppVersion
	if stamp, err := time.Parse(time.RFC3339, payload.Info.LastDeployed); err == nil {
		out.DeployedAt = stamp.UTC().Format(time.RFC3339)
	}
	out.Inventory, err = helmInventory(ctx, payload.Manifest)
	out.InventoryAvailable = err == nil
	if err != nil {
		out.Inventory = []model.HelmResource{}
	}
	if out.InventoryAvailable {
		out.Configuration, err = helmConfiguration(ctx, payload.Manifest)
		out.ConfigurationAvailable = err == nil
	}
	return out, len(data), ctx.Err()
}
func safeText(s string, limit int) bool {
	return len(s) <= limit && !strings.ContainsFunc(s, unicode.IsControl)
}
func InventoryKey(r model.HelmResource) string {
	return r.APIVersion + "|" + r.Kind + "|" + r.Namespace + "|" + r.Name
}
func helmInventory(ctx context.Context, manifest string) ([]model.HelmResource, error) {
	items := []model.HelmResource{}
	seen := map[string]bool{}
	d := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var o map[string]any
		err := d.Decode(&o)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("Inventory unavailable")
		}
		if len(o) == 0 {
			continue
		}
		r := model.HelmResource{APIVersion: model.Str(o, "apiVersion"), Kind: model.Str(o, "kind"), Name: model.Str(o, "metadata", "name"), Namespace: model.Str(o, "metadata", "namespace")}
		if r.APIVersion == "" || r.Kind == "" || r.Name == "" || r.Kind == "List" {
			return nil, fmt.Errorf("Inventory unavailable")
		}
		for _, s := range []string{r.APIVersion, r.Kind, r.Name, r.Namespace} {
			if !safeText(s, 253) {
				return nil, fmt.Errorf("Inventory unavailable")
			}
		}
		key := InventoryKey(r)
		if seen[key] || len(items) >= MaxInventory {
			return nil, fmt.Errorf("Inventory unavailable")
		}
		seen[key] = true
		items = append(items, r)
	}
	sort.Slice(items, func(i, j int) bool { return InventoryKey(items[i]) < InventoryKey(items[j]) })
	return items, nil
}
