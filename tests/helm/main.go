// Command helm validates rendered deployment/security contracts without a cluster.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	yaml "k8s.io/apimachinery/pkg/util/yaml"
	"os"
	"os/exec"
	"strings"
	"timone/internal/model"
)

func require(ok bool, message string) {
	if !ok {
		fmt.Fprintln(os.Stderr, message)
		os.Exit(1)
	}
}
func render(extra ...string) []model.Object {
	args := append([]string{"template", "timone", "deploy/helm/timone", "--set", "auth.existingSecret=test-auth"}, extra...)
	cmd := exec.Command("helm", args...)
	b, err := cmd.CombinedOutput()
	require(err == nil, "Helm render failed: "+string(b))
	d := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(b), 4096)
	out := []model.Object{}
	for {
		var o model.Object
		err := d.Decode(&o)
		if err == io.EOF {
			break
		}
		require(err == nil, "Invalid rendered YAML")
		if len(o) > 0 {
			out = append(out, o)
		}
	}
	return out
}
func find(objects []model.Object, kind string) model.Object {
	for _, o := range objects {
		if model.Str(o, "kind") == kind {
			return o
		}
	}
	return nil
}
func main() {
	// Separate namespaces must never share chart-owned RBAC objects.
	identities := map[string]bool{}
	for _, ns := range []string{"team-a", "team-b"} {
		objects := render("--namespace", ns, "--set", "customResources[0].group=ops.example", "--set", "customResources[0].resource=databases", "--set", "customResources[0].namespaced=true", "--set", "rbac.subjectAccessReviews=true")
		roles := map[string]bool{}
		for _, o := range objects {
			kind, name := model.Str(o, "kind"), model.Str(o, "metadata", "name")
			if kind == "ClusterRole" || kind == "Role" {
				roles[kind+":"+name] = true
			}
			if kind == "ClusterRole" || kind == "ClusterRoleBinding" {
				key := kind + ":" + name
				require(!identities[key], "cross-namespace RBAC name collision: "+key)
				require(len(name) <= 63, "RBAC name exceeds bounded length")
				identities[key] = true
			}
		}
		for _, o := range objects {
			if kind := model.Str(o, "kind"); kind == "ClusterRoleBinding" || kind == "RoleBinding" {
				require(roles[model.Str(o, "roleRef", "kind")+":"+model.Str(o, "roleRef", "name")], "RBAC binding references a missing role")
			}
		}
	}
	legacy := render("--set", "rbac.legacyNames=true")
	require(model.Str(find(legacy, "ClusterRole"), "metadata", "name") == "timone-timone-namespaced", "legacy RBAC upgrade mode changed existing identity")

	defaults := render()
	grantRead, classRead := false, false
	for _, o := range defaults {
		if model.Str(o, "kind") != "ClusterRole" {
			continue
		}
		for _, v := range model.Arr(o, "rules") {
			rule, _ := v.(map[string]any)
			for _, res := range model.Arr(rule, "resources") {
				if res == "referencegrants" {
					grantRead = true
					require(fmt.Sprint(model.Arr(rule, "verbs")) == "[get list]", "ReferenceGrant must use bounded reads")
				}
				if res == "gatewayclasses" {
					classRead = true
					require(fmt.Sprint(model.Arr(rule, "verbs")) == "[get list]", "GatewayClass must use bounded reads")
				}
			}
		}
	}
	require(grantRead && classRead, "Gateway diagnostic reads missing")
	d := find(defaults, "Deployment")
	container, _ := model.Arr(d, "spec", "template", "spec", "containers")[0].(map[string]any)
	require(strings.HasPrefix(model.Str(container, "image"), "ghcr.io/salvatorecorvaglia/timone:"), "default image must use the published registry")
	pinned := render("--set", "image.tag=ignored", "--set", "image.digest=sha256:"+strings.Repeat("a", 64))
	pinnedContainer, _ := model.Arr(find(pinned, "Deployment"), "spec", "template", "spec", "containers")[0].(map[string]any)
	require(model.Str(pinnedContainer, "image") == "ghcr.io/salvatorecorvaglia/timone@sha256:"+strings.Repeat("a", 64), "image digest must override the tag")
	require(exec.Command("helm", "template", "timone", "deploy/helm/timone", "--set", "auth.existingSecret=test-auth", "--set", "image.digest=sha256:invalid").Run() != nil, "invalid image digest accepted")
	require(model.Str(d, "spec", "strategy", "type") == "Recreate", "session deployment must use one Pod during replacement")
	require(model.Bool(container, "securityContext", "readOnlyRootFilesystem"), "read-only root filesystem missing")
	require(model.Bool(d, "spec", "template", "spec", "securityContext", "runAsNonRoot"), "non-root pod security missing")
	require(model.Num(d, "spec", "template", "spec", "securityContext", "runAsUser") == 10001, "unexpected pod UID")
	require(model.Str(d, "spec", "template", "spec", "securityContext", "seccompProfile", "type") == "RuntimeDefault", "default seccomp profile missing")
	require(!model.Bool(container, "securityContext", "allowPrivilegeEscalation"), "privilege escalation enabled")
	require(fmt.Sprint(model.Arr(container, "securityContext", "capabilities", "drop")) == "[ALL]", "container capabilities were not dropped")
	require(model.Num(d, "spec", "template", "spec", "terminationGracePeriodSeconds") >= 15, "insufficient graceful termination budget")
	require(model.Str(container, "startupProbe", "httpGet", "path") == "/healthz", "startup probe missing")
	env := map[string]string{}
	for _, v := range model.Arr(container, "env") {
		e, _ := v.(map[string]any)
		env[model.Str(e, "name")] = model.Str(e, "value")
	}
	require(env["TIMONE_DIAGNOSTICS_STARTUP_GRACE_SECONDS"] == "300" && env["TIMONE_DIAGNOSTICS_WARNING_EVENT_WINDOW_SECONDS"] == "900" && env["TIMONE_DIAGNOSTICS_RESTART_THRESHOLD"] == "5", "default diagnostic policy missing")
	require(env["TIMONE_TOKEN_FILE"] == "/var/run/timone-auth/token" && env["TIMONE_TOKEN"] == "", "workspace credential must use a rotating file, not env")
	volume, _ := model.Arr(d, "spec", "template", "spec", "volumes")[0].(map[string]any)
	require(model.Num(volume, "secret", "defaultMode") == 288, "Secret volume mode must be 0440")
	for _, o := range defaults {
		if model.Str(o, "kind") != "Role" && model.Str(o, "kind") != "ClusterRole" {
			continue
		}
		for _, v := range model.Arr(o, "rules") {
			rule, _ := v.(map[string]any)
			for _, verb := range model.Arr(rule, "verbs") {
				require(verb == "get" || verb == "list" || verb == "watch", "Mutation/wildcard RBAC verb in default chart")
			}
			for _, res := range model.Arr(rule, "resources") {
				require(res != "secrets" && res != "pods/log", "Sensitive read permission enabled by default")
			}
		}
	}
	scoped := render("--set", "scope.namespace=production", "--set", "rbac.clusterResources=false", "--set", "rbac.discovery=false", "--set", "rbac.secrets=true", "--set", "helmDetails.enabled=true")
	require(find(scoped, "Role") != nil && find(scoped, "ClusterRole") == nil, "namespace-scoped chart granted cluster resource permissions")
	proxy := render("--set", "auth.mode=proxy", "--set", "auth.trustedProxies[0]=10.0.0.0/24", "--set", "publicURL=https://timone.example", "--set", "networkPolicy.enabled=true")
	policy := find(proxy, "NetworkPolicy")
	require(policy != nil && len(model.Arr(policy, "spec", "ingress")) == 0, "empty NetworkPolicy peers must deny ingress")
	custom := render("--set", "scope.namespace=production", "--set", "rbac.clusterResources=false", "--set", "rbac.discovery=false", "--set", "customResources[0].group=ops.example", "--set", "customResources[0].resource=databases", "--set", "customResources[0].namespaced=true", "--set", "customResources[1].group=ops.example", "--set", "customResources[1].resource=clusterprofiles", "--set", "customResources[1].namespaced=false")
	definitions, namespaced, global := false, false, false
	for _, o := range custom {
		if model.Str(o, "kind") != "Role" && model.Str(o, "kind") != "ClusterRole" {
			continue
		}
		for _, raw := range model.Arr(o, "rules") {
			rule, _ := raw.(map[string]any)
			for _, res := range model.Arr(rule, "resources") {
				switch res {
				case "customresourcedefinitions":
					definitions = model.Str(o, "kind") == "ClusterRole" && len(model.Arr(rule, "resourceNames")) == 2 && len(model.Arr(rule, "verbs")) == 1 && model.Arr(rule, "verbs")[0] == "get"
				case "databases":
					namespaced = model.Str(o, "kind") == "Role" && model.Str(o, "metadata", "namespace") == "production"
				case "clusterprofiles":
					global = model.Str(o, "kind") == "ClusterRole"
				}
			}
		}
	}
	require(definitions && namespaced && global, "custom API grants must use named CRD GETs and the explicitly configured resource scopes")
	monitoring := render("--set", "prometheus.url=https://metrics.example/prometheus", "--set", "prometheus.cluster=production", "--set", "prometheus.authSecret=metrics-auth", "--set", "prometheus.caConfigMap=metrics-ca")
	md := find(monitoring, "Deployment")
	mc, _ := model.Arr(md, "spec", "template", "spec", "containers")[0].(map[string]any)
	metricsEnv := map[string]string{}
	for _, raw := range model.Arr(mc, "env") {
		entry, _ := raw.(map[string]any)
		metricsEnv[model.Str(entry, "name")] = model.Str(entry, "value")
	}
	require(metricsEnv["TIMONE_PROMETHEUS_URL"] == "https://metrics.example/prometheus" && metricsEnv["TIMONE_PROMETHEUS_TOKEN_FILE"] == "/var/run/timone-prometheus/token" && metricsEnv["TIMONE_PROMETHEUS_CA_FILE"] == "/var/run/timone-prometheus/ca.crt", "Prometheus configuration or file credential mounts missing")
	projected := false
	for _, raw := range model.Arr(md, "spec", "template", "spec", "volumes") {
		volume, _ := raw.(map[string]any)
		if model.Str(volume, "name") == "prometheus-config" {
			projected = model.Num(volume, "projected", "defaultMode") == 288 && len(model.Arr(volume, "projected", "sources")) == 2
		}
	}
	require(projected, "Prometheus credentials and CA must use read-only projected files")
	diagnostics := render("--set", "diagnostics.startupGraceSeconds=0", "--set", "diagnostics.warningEventWindowSeconds=3600", "--set", "diagnostics.restartThreshold=12")
	dd := find(diagnostics, "Deployment")
	dc, _ := model.Arr(dd, "spec", "template", "spec", "containers")[0].(map[string]any)
	diagnosticEnv := map[string]string{}
	for _, raw := range model.Arr(dc, "env") {
		entry, _ := raw.(map[string]any)
		diagnosticEnv[model.Str(entry, "name")] = model.Str(entry, "value")
	}
	require(diagnosticEnv["TIMONE_DIAGNOSTICS_STARTUP_GRACE_SECONDS"] == "0" && diagnosticEnv["TIMONE_DIAGNOSTICS_WARNING_EVENT_WINDOW_SECONDS"] == "3600" && diagnosticEnv["TIMONE_DIAGNOSTICS_RESTART_THRESHOLD"] == "12", "custom diagnostic policy lost or zero defaulted")
	for _, option := range []string{"diagnostics.startupGraceSeconds=-1", "diagnostics.startupGraceSeconds=3601", "diagnostics.warningEventWindowSeconds=59", "diagnostics.warningEventWindowSeconds=86401", "diagnostics.restartThreshold=0", "diagnostics.restartThreshold=10001", "diagnostics.restartThreshold=1.5", "diagnostics.warningEventWindowSeconds=5m", "diagnostics.unknown=5"} {
		require(exec.Command("helm", "template", "timone", "deploy/helm/timone", "--set", "auth.existingSecret=test-auth", "--set", option).Run() != nil, "Invalid diagnostic chart option accepted: "+option)
	}
	for _, extra := range [][]string{{"--set", "prometheus.authSecret=metrics-auth"}, {"--set", "prometheus.url=http://metrics.example", "--set", "prometheus.allowHTTP=true", "--set", "prometheus.authSecret=metrics-auth"}, {"--set", "prometheus.url=http://metrics.example"}, {"--set", "prometheus.url=https://user:password@metrics.example"}} {
		args := append([]string{"template", "timone", "deploy/helm/timone", "--set", "auth.existingSecret=test-auth"}, extra...)
		require(exec.Command("helm", args...).Run() != nil, "Unsafe monitoring chart configuration accepted")
	}
	for _, extra := range [][]string{{"--set", "replicaCount=2"}, {"--set", "auth.mode=unknown"}, {"--set", "auth.mode=proxy"}, {"--set", "publicURL=https://timone.example/path"}, {"--set", "helmDetails.enabled=true"}, {"--set", "ingress.enabled=true", "--set", "publicURL=http://timone.example", "--set", "ingress.host=timone.example"}, {"--set", "customResources[0].group=*", "--set", "customResources[0].resource=databases", "--set", "customResources[0].namespaced=true"}, {"--set", "customResources[0].group=ops.example", "--set", "customResources[0].resource=pods/exec", "--set", "customResources[0].namespaced=true"}} {
		args := append([]string{"template", "timone", "deploy/helm/timone", "--set", "auth.existingSecret=test-auth"}, extra...)
		err := exec.Command("helm", args...).Run()
		require(err != nil, "Invalid chart options were accepted: "+strings.Join(extra, " "))
	}
	reviews := render("--set", "rbac.subjectAccessReviews=true")
	reviewGrant := false
	for _, o := range reviews {
		for _, item := range model.Arr(o, "rules") {
			rule, _ := item.(map[string]any)
			for _, resource := range model.Arr(rule, "resources") {
				if resource == "subjectaccessreviews" {
					reviewGrant = true
					require(fmt.Sprint(model.Arr(rule, "verbs")) == "[create]", "review grant must be create-only")
				}
			}
		}
	}
	require(reviewGrant, "optional SubjectAccessReview grant missing")
	gitops := render("--set", "gitops.argoURL=https://argo.example", "--set", "gitops.fluxRenderedURL=https://renderer.example", "--set", "gitops.authSecret=gitops-auth", "--set", "gitops.caConfigMap=gitops-ca")
	gitopsDeployment := find(gitops, "Deployment")
	gitopsMount := false
	for _, item := range model.Arr(gitopsDeployment, "spec", "template", "spec", "volumes") {
		volume, _ := item.(map[string]any)
		if model.Str(volume, "name") == "gitops-config" {
			gitopsMount = model.Num(volume, "projected", "defaultMode") == 288 && len(model.Arr(volume, "projected", "sources")) == 2
		}
	}
	require(gitopsMount, "GitOps credentials/CA must use projected read-only files")
	for _, option := range []string{"gitops.authSecret=unused", "gitops.argoURL=http://argo.example"} {
		require(exec.Command("helm", "template", "timone", "deploy/helm/timone", "--set", "auth.existingSecret=test-auth", "--set", option).Run() != nil, "invalid GitOps configuration accepted")
	}
	additionalInvalid := [][]string{
		{"--set", "ingress.enabled=true", "--set", "ingress.host=other.example", "--set", "publicURL=https://timone.example"},
		{"--set", "scope.namespace=bad.namespace"},
		{"--set", "retainedMiB=7"}, {"--set", "retainedMiB=129"},
		{"--set", "auth.mode=proxy", "--set", "auth.trustedProxies[0]=10.0.0.0/24", "--set", "publicURL=https://timone.example", "--set", "auth.loginTrustedProxies[0]=10.0.0.0/24"},
		{"--set", "customResources[0].group=ops.example", "--set", "customResources[0].resource=databases", "--set", "customResources[0].namespaced=true", "--set", "customResources[1].group=ops.example", "--set", "customResources[1].resource=databases", "--set", "customResources[1].namespaced=false"},
	}
	for _, extra := range additionalInvalid {
		args := append([]string{"template", "timone", "deploy/helm/timone", "--set", "auth.existingSecret=test-auth"}, extra...)
		require(exec.Command("helm", args...).Run() != nil, "Inconsistent chart configuration accepted")
	}
	encoded, _ := json.Marshal(map[string]any{"checks": "passed", "variants": 8, "invalidConfigurations": 23 + len(additionalInvalid)})
	fmt.Println(string(encoded))
}
