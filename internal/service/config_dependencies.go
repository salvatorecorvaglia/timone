package service

import (
	"context"
	"fmt"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type ConfigurationReference struct {
	Consumer    model.Resource    `json:"consumer"`
	Origin      string            `json:"origin"`
	Container   string            `json:"container"`
	Path        string            `json:"path"`
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	Key         string            `json:"key"`
	Optional    bool              `json:"optional"`
	Consumption string            `json:"consumption"`
	Update      string            `json:"update"`
	Resource    *model.Resource   `json:"resource,omitempty"`
	State       string            `json:"state"`
	Immutable   *bool             `json:"immutable"`
	Checks      []AssessmentCheck `json:"checks"`
}
type ConfigurationAssessment struct {
	Resource    model.Resource           `json:"resource"`
	ObservedAt  time.Time                `json:"observedAt"`
	State       string                   `json:"state"`
	CurrentPods int                      `json:"currentPods"`
	References  []ConfigurationReference `json:"references"`
	Checks      []AssessmentCheck        `json:"checks"`
	Warnings    []model.Warning          `json:"warnings"`
	Limitations []string                 `json:"limitations"`
}

func (s *Service) Configuration(ctx context.Context, kind, ns, name string) (ConfigurationAssessment, error) {
	ctx = provider.WithReadBudget(ctx, provider.InvestigationBytes)
	root, pods, warnings, err := s.workloadPods(ctx, kind, ns, name)
	if err != nil {
		return ConfigurationAssessment{}, err
	}
	out := ConfigurationAssessment{Resource: assessmentResource(root), ObservedAt: time.Now().UTC(), State: "declared-references", CurrentPods: len(pods), References: []ConfigurationReference{}, Checks: []AssessmentCheck{}, Warnings: warnings, Limitations: []string{
		"Only explicit Pod/template ConfigMap and Secret references are inspected. Application API reads, CSI/external secret providers, service-account-inherited imagePullSecrets and references embedded in commands or custom resources are not discovered.",
		"Desired template and current UID-owned Pods are separate evidence. No configuration version loaded by a running process is inferred from resourceVersion, creation time or current object contents.",
		"Secret payloads and their key inventory are never returned or inspected by this assessment. Secret key availability remains unverified, even when the object is readable. ConfigMap selected key presence is checked without returning values.",
		"Environment values require container recreation to change. Ordinary projected volumes can update eventually, but application reload and propagation are not verified; subPath/subPathExpr mounts do not receive projected updates. Immutable objects cannot be updated in place.",
	}}
	type cached struct {
		o   model.Object
		err error
	}
	cache := map[string]cached{}
	limited := false
	add := func(row ConfigurationReference) {
		if len(out.References) >= 200 {
			if !limited {
				out.Warnings = append(out.Warnings, model.Warning{Kind: "Configuration", Message: "Configuration evidence limited to 200 references"})
				limited = true
			}
			return
		}
		row.Checks = []AssessmentCheck{}
		row.State = "unknown"
		if e := ValidateResource(row.Kind, ns, row.Name); e != nil {
			row.Checks = append(row.Checks, check("reference", "unknown", "Invalid referenced resource name"))
			out.References = append(out.References, row)
			return
		}
		id := model.ID(row.Kind, ns, row.Name)
		entry, ok := cache[id]
		if !ok {
			if len(cache) >= 64 {
				row.Checks = append(row.Checks, check("reference-limit", "unknown", "Reference reads limited to 64 distinct ConfigMaps/Secrets"))
				out.References = append(out.References, row)
				return
			}
			entry.o, entry.err = s.Provider.Get(ctx, row.Kind, ns, row.Name)
			cache[id] = entry
			if entry.err != nil && provider.Status(entry.err) != 404 {
				out.Warnings = append(out.Warnings, model.Warning{Kind: row.Kind, Message: entry.err.Error()})
			}
		}
		if entry.err != nil {
			if provider.Status(entry.err) == 404 {
				row.State = "missing"
				state := "constraint"
				if row.Consumption == "image-pull" {
					state = "advisory"
				}
				if row.Optional {
					row.State = "optional-missing"
					state = "advisory"
				}
				row.Checks = append(row.Checks, check("object", state, "Referenced object was not found in the consumer namespace; optional references do not require object presence"))
			} else {
				row.Checks = append(row.Checks, check("object", "unknown", "Referenced object could not be read; absence is not established"))
			}
		} else {
			r := assessmentResource(entry.o)
			row.Resource = &r
			row.State = "present"
			row.Immutable = optionalBool(entry.o, "immutable")
			if row.Key != "" {
				if row.Kind == "Secret" {
					row.State = "key-unverified"
					row.Checks = append(row.Checks, check("key", "unknown", "Secret key presence is not inspected; readable object presence does not establish key availability"))
				} else {
					_, data := model.Map(entry.o, "data")[row.Key]
					_, binary := model.Map(entry.o, "binaryData")[row.Key]
					if row.Consumption == "environment-key" {
						binary = false
					}
					if !data && !binary {
						row.State = "key-missing"
						state := "constraint"
						if row.Optional {
							row.State = "optional-key-missing"
							state = "advisory"
						}
						row.Checks = append(row.Checks, check("key", state, "Selected ConfigMap key was not found; no payload is returned"))
					} else {
						row.Checks = append(row.Checks, check("key", "observed", "Selected ConfigMap key is present; no payload is returned"))
					}
				}
			}
			if row.Immutable != nil && *row.Immutable {
				row.Checks = append(row.Checks, check("immutable", "observed", "Object is immutable; an in-place payload update is not supported"))
			}
		}
		out.References = append(out.References, row)
	}
	type consumer struct {
		o      model.Object
		spec   map[string]any
		origin string
	}
	consumers := []consumer{}
	if kind != "Pod" {
		consumers = append(consumers, consumer{root, model.Map(root, "spec", "template", "spec"), "desired-template"})
	}
	if len(pods) > 32 {
		pods = pods[:32]
		out.Warnings = append(out.Warnings, model.Warning{Kind: "Pod", Message: "Configuration consumers limited to 32 current owned Pods"})
	}
	for _, pod := range pods {
		consumers = append(consumers, consumer{pod, model.Map(pod, "spec"), "current-pod"})
	}
	for _, consumer := range consumers {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		base := ConfigurationReference{Consumer: assessmentResource(consumer.o), Origin: consumer.origin}
		for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
			for ci, raw := range model.Arr(consumer.spec, field) {
				c, _ := raw.(map[string]any)
				for ei, raw := range model.Arr(c, "env") {
					env, _ := raw.(map[string]any)
					for _, ref := range []struct{ field, kind string }{{"configMapKeyRef", "ConfigMap"}, {"secretKeyRef", "Secret"}} {
						v := model.Map(env, "valueFrom", ref.field)
						if v == nil {
							continue
						}
						row := base
						row.Container = model.Str(c, "name")
						row.Path = fmt.Sprintf("%s[%d].env[%d].valueFrom.%s", field, ci, ei, ref.field)
						row.Kind = ref.kind
						row.Name = model.Str(v, "name")
						row.Key = model.Str(v, "key")
						row.Optional = model.Bool(v, "optional")
						row.Consumption = "environment-key"
						row.Update = "Resolved when the container starts; running environment values are not refreshed automatically"
						add(row)
					}
				}
				for ei, raw := range model.Arr(c, "envFrom") {
					env, _ := raw.(map[string]any)
					for _, ref := range []struct{ field, kind string }{{"configMapRef", "ConfigMap"}, {"secretRef", "Secret"}} {
						v := model.Map(env, ref.field)
						if v == nil {
							continue
						}
						row := base
						row.Container = model.Str(c, "name")
						row.Path = fmt.Sprintf("%s[%d].envFrom[%d].%s", field, ci, ei, ref.field)
						row.Kind = ref.kind
						row.Name = model.Str(v, "name")
						row.Optional = model.Bool(v, "optional")
						row.Consumption = "environment-all"
						row.Update = "Resolved when the container starts; existing environment is not refreshed automatically"
						add(row)
					}
				}
			}
		}
		for vi, raw := range model.Arr(consumer.spec, "volumes") {
			volume, _ := raw.(map[string]any)
			type volumeRef struct {
				v                     map[string]any
				kind, path, nameField string
			}
			refs := []volumeRef{}
			if v := model.Map(volume, "configMap"); v != nil {
				refs = append(refs, volumeRef{v, "ConfigMap", fmt.Sprintf("volumes[%d].configMap", vi), "name"})
			}
			if v := model.Map(volume, "secret"); v != nil {
				refs = append(refs, volumeRef{v, "Secret", fmt.Sprintf("volumes[%d].secret", vi), "secretName"})
			}
			for si, raw := range model.Arr(volume, "projected", "sources") {
				src, _ := raw.(map[string]any)
				for _, ref := range []struct{ field, kind string }{{"configMap", "ConfigMap"}, {"secret", "Secret"}} {
					if v := model.Map(src, ref.field); v != nil {
						refs = append(refs, volumeRef{v, ref.kind, fmt.Sprintf("volumes[%d].projected.sources[%d].%s", vi, si, ref.field), "name"})
					}
				}
			}
			for _, ref := range refs {
				keys := []string{""}
				if items := model.Arr(ref.v, "items"); len(items) > 0 {
					keys = []string{}
					for _, raw := range items {
						item, _ := raw.(map[string]any)
						keys = append(keys, model.Str(item, "key"))
					}
				}
				mounts := []struct{ container, mode, update string }{}
				for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
					for _, raw := range model.Arr(consumer.spec, field) {
						c, _ := raw.(map[string]any)
						for _, raw := range model.Arr(c, "volumeMounts") {
							mount, _ := raw.(map[string]any)
							if model.Str(mount, "name") != model.Str(volume, "name") {
								continue
							}
							mode := "volume"
							update := "Projected files can update eventually; application reload and actual propagation are unverified"
							if model.Str(mount, "subPath") != "" || model.Str(mount, "subPathExpr") != "" {
								mode = "subpath-volume"
								update = "subPath/subPathExpr mounts do not receive projected configuration updates"
							}
							mounts = append(mounts, struct{ container, mode, update string }{model.Str(c, "name"), mode, update})
						}
					}
				}
				if len(mounts) == 0 {
					mounts = append(mounts, struct{ container, mode, update string }{"", "unmounted-volume", "No explicit container mount for this declared volume was observed"})
				}
				for _, mount := range mounts {
					for _, key := range keys {
						row := base
						row.Container = mount.container
						row.Path = ref.path
						row.Kind = ref.kind
						row.Name = model.Str(ref.v, ref.nameField)
						row.Key = key
						row.Optional = model.Bool(ref.v, "optional")
						row.Consumption = mount.mode
						row.Update = mount.update
						add(row)
					}
				}
			}
		}
		for i, raw := range model.Arr(consumer.spec, "imagePullSecrets") {
			ref, _ := raw.(map[string]any)
			row := base
			row.Path = fmt.Sprintf("imagePullSecrets[%d]", i)
			row.Kind = "Secret"
			row.Name = model.Str(ref, "name")
			row.Consumption = "image-pull"
			row.Update = "Registry credential use and cached image availability are not verified"
			add(row)
		}
	}
	if len(out.Warnings) > 0 {
		out.State = "partial"
	}
	return out, ctx.Err()
}
