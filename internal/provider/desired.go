package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"timone/internal/integrations"
	"timone/internal/model"
)

type DesiredProvider interface {
	DesiredManifests(context.Context, string, string, string) (model.DesiredBundle, error)
}

func (k *cluster) DesiredManifests(ctx context.Context, kind, ns, name string) (model.DesiredBundle, error) {
	out := model.DesiredBundle{Manifests: []model.Object{}}
	o, err := k.Get(ctx, kind, ns, name)
	if err != nil {
		return out, err
	}
	out.ControllerUID = model.Str(o, "metadata", "uid")
	out.Generation = int64(model.Num(o, "metadata", "generation"))
	switch kind {
	case "Application":
		if len(model.Arr(o, "spec", "sources")) > 0 {
			return out, &Error{503, "Multi-source Argo rendering is not supported by this adapter"}
		}
		destination := model.Str(o, "spec", "destination", "server")
		destinationName := model.Str(o, "spec", "destination", "name")
		expected := k.desired.Destination
		if expected == "" {
			expected = k.server
		}
		if destination != expected && destinationName != expected {
			return out, &Error{503, "Argo destination is not explicitly bound to the active workspace"}
		}
		out.Namespace = model.Str(o, "spec", "destination", "namespace")
		path := "/api/v1/applications/" + url.PathEscape(name)
		params := url.Values{"appNamespace": {ns}}
		var reported model.Object
		if err := integrations.DesiredJSON(ctx, k.desired.ArgoURL, path+"?"+params.Encode(), k.desired.ArgoTokenFile, k.desired.CAFile, &reported, k.desiredTransport); err != nil {
			return out, &Error{503, err.Error()}
		}
		if model.Str(reported, "metadata", "uid") != out.ControllerUID || int64(model.Num(reported, "metadata", "generation")) != out.Generation {
			return out, &Error{409, "Argo API application identity/generation differs from the active cluster"}
		}
		revision := model.Str(o, "spec", "source", "targetRevision")
		if revision == "" {
			revision = "HEAD"
		}
		params.Set("revision", revision)
		var response struct {
			Manifests []string `json:"manifests"`
			Revision  string   `json:"revision"`
		}
		if err := integrations.DesiredJSON(ctx, k.desired.ArgoURL, path+"/manifests?"+params.Encode(), k.desired.ArgoTokenFile, k.desired.CAFile, &response, k.desiredTransport); err != nil {
			return out, &Error{503, err.Error()}
		}
		if response.Revision == "" || len(response.Manifests) > 1000 {
			return out, &Error{502, "Argo returned an unresolved revision or excessive manifest inventory"}
		}
		for _, text := range response.Manifests {
			var object model.Object
			if json.Unmarshal([]byte(text), &object) != nil {
				return out, &Error{502, "Argo manifest is not structured JSON"}
			}
			out.Manifests = append(out.Manifests, object)
		}
		out.Revision = response.Revision
		out.Source = "Argo CD rendered target manifests"
	case "Kustomization", "HelmRelease":
		if out.Generation < 1 || model.Num(o, "status", "observedGeneration") != float64(out.Generation) {
			return out, &Error{503, "Flux target generation has not been observed; desired revision cannot be bound safely"}
		}
		revision := model.Str(o, "status", "lastAttemptedRevision")
		if revision == "" {
			return out, &Error{503, "Flux has no observed attempted target revision"}
		}
		if k.desired.RenderedURL != "" {
			params := url.Values{"revision": {revision}, "uid": {out.ControllerUID}, "generation": {fmt.Sprint(out.Generation)}}
			var rendered model.DesiredBundle
			if err := integrations.DesiredJSON(ctx, k.desired.RenderedURL, "/api/v1/rendered/"+kind+"/"+url.PathEscape(ns)+"/"+url.PathEscape(name)+"?"+params.Encode(), k.desired.RenderedTokenFile, k.desired.CAFile, &rendered, k.desiredTransport); err != nil {
				return out, &Error{503, err.Error()}
			}
			if rendered.ControllerUID != out.ControllerUID || rendered.Generation != out.Generation || rendered.Revision != revision || len(rendered.Manifests) > 1000 {
				return out, &Error{409, "Rendered Flux bundle does not match current controller identity, generation and target revision"}
			}
			rendered.Source = "Configured revision-bound Flux renderer"
			return rendered, nil
		}
		if kind == "Kustomization" {
			return out, &Error{503, "Flux Kustomization requires a configured rendered-manifest source; status/inventory cannot reconstruct desired fields"}
		}
		// Stored Helm state is a target source only when Flux ties the current
		// observed generation and attempted config digest to a deployed snapshot.
		generation := model.Num(o, "metadata", "generation")
		configDigest := model.Str(o, "status", "lastAttemptedConfigDigest")
		snapshotVersion := 0
		snapshotName, snapshotNamespace := "", ""
		if generation > 0 && model.Num(o, "status", "observedGeneration") == generation && configDigest != "" {
			for _, item := range model.Arr(o, "status", "history") {
				snapshot, _ := item.(map[string]any)
				if model.Str(snapshot, "status") == "deployed" && model.Str(snapshot, "chartVersion") == revision && model.Str(snapshot, "configDigest") == configDigest {
					snapshotVersion = int(model.Num(snapshot, "version"))
					snapshotName, snapshotNamespace = model.Str(snapshot, "name"), model.Str(snapshot, "namespace")
					break
				}
			}
		}
		if snapshotVersion < 1 {
			return out, &Error{503, "Flux target is not tied to a current deployed Helm snapshot/config digest; configure a revision-bound renderer"}
		}
		targetNS := model.Str(o, "spec", "targetNamespace")
		if targetNS == "" {
			targetNS = ns
		}
		// Use the controller's resolved identity, including Flux's default-name hash.
		releaseName := snapshotName
		if explicit := model.Str(o, "spec", "releaseName"); explicit != "" && explicit != releaseName {
			return out, &Error{409, "Flux deployed snapshot release name differs from its declared target"}
		}
		if snapshotNamespace != targetNS || ValidateHelmReference(targetNS, releaseName) != nil {
			return out, &Error{409, "Flux deployed snapshot has no valid matching target identity"}
		}
		storageNS := model.Str(o, "spec", "storageNamespace")
		if storageNS == "" {
			storageNS = ns
		}
		history, err := k.HelmHistory(ctx, storageNS, releaseName)
		if err != nil {
			return out, err
		}
		for _, r := range history.Revisions {
			if r.TargetNamespace == targetNS && r.Revision == snapshotVersion && r.Status == "deployed" && r.ChartVersion == revision && r.ConfigurationAvailable {
				for _, manifest := range r.Configuration {
					object := manifest.Object
					if spec, ok := FindKind(model.Str(object, "kind")); ok && spec.Namespaced && model.Str(object, "metadata", "namespace") == "" {
						model.Map(object, "metadata")["namespace"] = targetNS
					}
					out.Manifests = append(out.Manifests, object)
				}
				out.Revision = revision
				out.Namespace = targetNS
				out.Source = "Retained deployed Helm release matching Flux attempted chart revision"
				return out, nil
			}
		}
		return out, &Error{503, "No retained deployed Helm manifest matches the Flux target revision; configure a revision-bound renderer"}
	default:
		return out, &Error{400, "Unsupported GitOps controller"}
	}
	return out, ctx.Err()
}
func (d *demo) DesiredManifests(ctx context.Context, kind, ns, name string) (model.DesiredBundle, error) {
	controller, err := d.Get(ctx, kind, ns, name)
	if err != nil {
		return model.DesiredBundle{}, err
	}
	target := "checkout-api"
	if kind == "HelmRelease" {
		target = "prometheus"
	}
	if kind == "Kustomization" {
		target = "storefront"
	}
	targetNS := "production"
	if target == "prometheus" {
		targetNS = "platform"
	}
	return model.DesiredBundle{ControllerUID: model.Str(controller, "metadata", "uid"), Generation: int64(model.Num(controller, "metadata", "generation")), Revision: "demo-target-8f31a2c", Source: "Simulated rendered desired revision", Namespace: targetNS, Manifests: []model.Object{{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": target, "namespace": targetNS}, "spec": map[string]any{"replicas": float64(4)}}}}, ctx.Err()
}
