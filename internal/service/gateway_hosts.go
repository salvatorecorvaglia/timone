package service

import (
	"k8s.io/apimachinery/pkg/util/validation"
	"net"
	"strings"
	"timone/internal/model"
)

// Gateway wildcard hostnames are suffix matches across one or more DNS labels,
// unlike certificate wildcards. This checks configuration, never certificate SANs.
func hostnameAdmission(route model.Resource, listener map[string]any) (string, string) {
	host := model.Str(listener, "hostname")
	valid := func(h string) bool {
		return len(h) <= 253 && net.ParseIP(h) == nil && len(validation.IsDNS1123Subdomain(strings.TrimPrefix(h, "*."))) == 0
	}
	if host != "" && !valid(host) {
		return "unknown", "listener hostname is invalid"
	}
	raw := model.Value(route.Object, "spec", "hostnames")
	if raw == nil {
		return "allowed", "route has no hostname constraint"
	}
	hosts, ok := raw.([]any)
	if !ok || len(hosts) == 0 || len(hosts) > 16 {
		return "unknown", "route hostname list is invalid"
	}
	matched, invalid := false, false
	for _, v := range hosts {
		h, ok := v.(string)
		if !ok || !valid(h) {
			invalid = true
			continue
		}
		if host == "" || hostnameIntersection(h, host) {
			matched = true
		}
	}
	if matched {
		return "allowed", "route and listener hostname constraints intersect"
	}
	if invalid {
		return "unknown", "route hostname evidence is invalid"
	}
	return "denied", "route and listener hostname constraints do not intersect"
}
func hostnameIntersection(a, b string) bool {
	if a == b {
		return true
	}
	aw, bw := strings.HasPrefix(a, "*."), strings.HasPrefix(b, "*.")
	if aw && bw {
		return strings.HasSuffix(a[1:], b[1:]) || strings.HasSuffix(b[1:], a[1:])
	}
	if aw {
		return strings.HasSuffix(b, a[1:])
	}
	if bw {
		return strings.HasSuffix(a, b[1:])
	}
	return false
}
