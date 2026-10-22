package provider

import (
	"context"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	kubeversion "k8s.io/apimachinery/pkg/version"
	"timone/internal/model"
)

// CustomProvider is separate from built-in Kind routing and does not accept GVRs
// or API paths from a browser. Every access revalidates its configured CRD.
type CustomProvider interface {
	CustomTypes(context.Context) (model.CustomCatalog, error)
	CustomType(context.Context, string, string) (model.CustomType, string, error)
	CustomList(context.Context, string, string, string) ([]model.Object, model.CustomType, bool, error)
	CustomGet(context.Context, string, string, string, string) (model.Object, model.CustomType, error)
}

func parseCustomType(o model.Object) (model.CustomType, error) {
	t := model.CustomType{Definition: model.Str(o, "metadata", "name"), Kind: model.Str(o, "spec", "names", "kind"), Group: model.Str(o, "spec", "group"), Plural: model.Str(o, "spec", "names", "plural"), Versions: []string{}, Presentations: map[string]model.CustomPresentation{}}
	scope := model.Str(o, "spec", "scope")
	if !model.ValidCustomDefinition(t.Definition) || t.Definition != t.Plural+"."+t.Group || len(validation.IsCIdentifier(t.Kind)) > 0 || len(t.Kind) > 63 || (scope != "Namespaced" && scope != "Cluster") {
		return t, &Error{502, "CustomResourceDefinition has invalid identity or scope"}
	}
	t.Namespaced = scope == "Namespaced"
	established := false
	for _, v := range model.Arr(o, "status", "conditions") {
		c, _ := v.(map[string]any)
		if model.Str(c, "type") == "NamesAccepted" && model.Str(c, "status") == "False" {
			return t, &Error{503, "CustomResourceDefinition names are not accepted"}
		}
		if model.Str(c, "type") == "Established" && model.Str(c, "status") == "True" {
			established = true
		}
	}
	if !established {
		return t, &Error{503, "CustomResourceDefinition is not established"}
	}
	seen := map[string]bool{}
	for _, v := range model.Arr(o, "spec", "versions") {
		version, _ := v.(map[string]any)
		if !model.Bool(version, "served") {
			continue
		}
		name := model.Str(version, "name")
		if len(validation.IsDNS1035Label(name)) > 0 || seen[name] || len(t.Versions) >= 32 {
			return t, &Error{502, "CustomResourceDefinition has invalid served versions"}
		}
		presentation := model.CustomPresentation{Columns: []model.PrinterColumn{}}
		for _, value := range model.Arr(version, "additionalPrinterColumns") {
			column, _ := value.(map[string]any)
			if len(presentation.Columns) >= 64 {
				presentation.Truncated = true
				break
			}
			presentation.Columns = append(presentation.Columns, model.PrinterColumn{Name: model.Str(column, "name"), Type: model.Str(column, "type"), Format: model.Str(column, "format"), Description: model.Str(column, "description"), JSONPath: model.Str(column, "jsonPath"), Priority: int(model.Num(column, "priority"))})
		}
		schema, schemaTruncated := model.SchemaProjection(model.Map(version, "schema", "openAPIV3Schema"))
		presentation.Schema = schema
		presentation.Truncated = presentation.Truncated || schemaTruncated
		t.Presentations[name] = presentation
		seen[name] = true
		t.Versions = append(t.Versions, name)
		if model.Bool(version, "storage") {
			if t.PreferredVersion != "" {
				return t, &Error{502, "CustomResourceDefinition has multiple storage versions"}
			}
			t.PreferredVersion = name
		}
	}
	if len(t.Versions) == 0 {
		return t, &Error{503, "CustomResourceDefinition has no served version"}
	}
	sort.Slice(t.Versions, func(i, j int) bool {
		return kubeversion.CompareKubeAwareVersionStrings(t.Versions[i], t.Versions[j]) > 0
	})
	if t.PreferredVersion == "" {
		t.PreferredVersion = t.Versions[0]
	}
	return t, nil
}
func selectCustomVersion(t model.CustomType, requested string) (string, error) {
	if requested == "" {
		return t.PreferredVersion, nil
	}
	for _, v := range t.Versions {
		if v == requested {
			return v, nil
		}
	}
	return "", &Error{400, "Requested custom resource version is not served"}
}
func customSpec(t model.CustomType, version string) Kind {
	return Kind{Name: t.Kind, Group: t.Group, Version: version, Plural: t.Plural, Namespaced: t.Namespaced}
}
func (k *cluster) CustomType(ctx context.Context, definition, version string) (model.CustomType, string, error) {
	allowed := false
	for _, name := range k.customResources {
		allowed = allowed || name == definition
	}
	if !model.ValidCustomDefinition(definition) || !customVersionValid(version) {
		return model.CustomType{}, "", &Error{400, "Invalid custom resource definition name"}
	}
	if !allowed {
		return model.CustomType{}, "", &Error{403, "Custom resource type is not enabled by the administrator"}
	}
	// Fresh GET, including RBAC, on every access. No discovery fallback or stale
	// catalog can authorize a removed definition or change its scope/version.
	spec, _ := FindKind("CustomResourceDefinition")
	o, err := k.resource(spec, "").Get(quiet(ctx), definition, metav1.GetOptions{})
	if err != nil {
		return model.CustomType{}, "", safeError(err)
	}
	t, err := parseCustomType(model.Object(o.Object))
	if err != nil {
		return t, "", err
	}
	if t.Definition != definition {
		return t, "", &Error{502, "CustomResourceDefinition response identity mismatch"}
	}
	v, err := selectCustomVersion(t, version)
	return t, v, err
}
func (k *cluster) CustomTypes(ctx context.Context) (model.CustomCatalog, error) {
	catalog := model.CustomCatalog{Items: []model.CustomType{}, Warnings: []model.Warning{}, Enabled: len(k.customResources) > 0, Mode: k.Mode(), Message: "Only administrator-enabled CRDs are shown. Instance reads still require Kubernetes RBAC. Custom specs and arbitrary status data are omitted."}
	if !catalog.Enabled {
		catalog.Message = "Custom resource browsing is disabled. Configure TIMONE_CUSTOM_RESOURCES with explicit CRD names and grant get on those CRDs plus get/list on their instances."
		return catalog, nil
	}
	// The administrator allowlist is capped at 32 types. No list of instances is
	// performed for catalog rows and no wildcard CRD list permission is needed.
	for _, name := range k.customResources {
		if err := ctx.Err(); err != nil {
			return catalog, err
		}
		t, _, err := k.CustomType(ctx, name, "")
		if err != nil {
			catalog.Warnings = append(catalog.Warnings, model.Warning{Kind: name, Message: err.Error()})
			continue
		}
		catalog.Items = append(catalog.Items, t)
	}
	sort.Slice(catalog.Items, func(i, j int) bool { return catalog.Items[i].Definition < catalog.Items[j].Definition })
	return catalog, nil
}
func (k *cluster) CustomList(ctx context.Context, definition, version, ns string) ([]model.Object, model.CustomType, bool, error) {
	t, v, err := k.CustomType(ctx, definition, version)
	if err != nil {
		return nil, t, false, err
	}
	spec := customSpec(t, v)
	ns, err = k.scope(spec, ns)
	if err != nil {
		return nil, t, false, err
	}
	// No raw custom specs enter built-in caches or informer stores. The bounded
	// collection is projected before aggregation and refreshed by active polling.
	list, truncated, err := k.directListProjected(ctx, spec, ns, metav1.ListOptions{}, model.CustomProjection)
	if err != nil {
		return nil, t, false, safeError(err)
	}
	return objects(list), t, truncated, nil
}
func (k *cluster) CustomGet(ctx context.Context, definition, version, ns, name string) (model.Object, model.CustomType, error) {
	t, v, err := k.CustomType(ctx, definition, version)
	if err != nil {
		return nil, t, err
	}
	if err := validateCustomObjectScope(t, ns, name); err != nil {
		return nil, t, err
	}
	spec := customSpec(t, v)
	ns, err = k.scope(spec, ns)
	if err != nil {
		return nil, t, err
	}
	o, err := k.resource(spec, ns).Get(quiet(ctx), name, metav1.GetOptions{})
	if err != nil {
		return nil, t, safeError(err)
	}
	if o.GetName() != name || o.GetNamespace() != ns || (o.GetKind() != "" && o.GetKind() != t.Kind) || (o.GetAPIVersion() != "" && o.GetAPIVersion() != t.Group+"/"+v) {
		return nil, t, &Error{502, "Custom resource response identity mismatch"}
	}
	o.SetKind(t.Kind)
	o.SetAPIVersion(t.Group + "/" + v)
	return model.CustomProjection(model.Object(o.Object)), t, nil
}

func customVersionValid(version string) bool {
	return version == "" || (len(validation.IsDNS1035Label(version)) == 0 && !strings.Contains(version, "/"))
}

func validateCustomObjectScope(t model.CustomType, ns, name string) error {
	if len(validation.IsDNS1123Subdomain(name)) > 0 || !validNamespace(ns) || (!t.Namespaced && ns != "") || (t.Namespaced && ns == "") {
		return &Error{400, "Invalid custom resource name or scope"}
	}
	return nil
}
