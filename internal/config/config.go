package config

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"timone/internal/auth"
	"timone/internal/integrations"
	"timone/internal/model"
	"timone/internal/prometheus"
)

type Config struct {
	RetainedMiB         int
	GitOps              integrations.DesiredOptions
	Mode                string
	Listen              string
	TokenFile           string
	AuthMode            string
	TrustedProxies      []netip.Prefix
	LoginTrustedProxies []netip.Prefix
	AccessLog           bool
	Token               string
	Kubeconfig          string
	Context             string
	NamespaceExplicit   bool
	Namespace           string
	PublicURL           string
	AllowExecAuth       bool
	HelmDetails         bool
	CustomResources     []string
	Prometheus          prometheus.Options
	Diagnostics         model.DiagnosticPolicy
}

func Load() (Config, error) { return LoadWithOverrides(nil) }

// LoadWithOverrides validates command-line overrides through the same rules as
// environment configuration. Presence in overrides takes precedence, even if empty.
func LoadWithOverrides(overrides map[string]string) (Config, error) {
	getenv := func(key string) string {
		if value, ok := overrides[key]; ok {
			return value
		}
		return os.Getenv(key)
	}
	env := func(key, fallback string) string {
		if value := getenv(key); value != "" {
			return value
		}
		return fallback
	}
	c := Config{Mode: env("TIMONE_MODE", "local"), Listen: env("TIMONE_LISTEN", "127.0.0.1:8080"), Token: getenv("TIMONE_TOKEN"), Kubeconfig: getenv("KUBECONFIG"), Context: getenv("TIMONE_CONTEXT"), Namespace: getenv("TIMONE_NAMESPACE"), PublicURL: getenv("TIMONE_PUBLIC_URL")}
	_, c.NamespaceExplicit = overrides["TIMONE_NAMESPACE"]
	c.RetainedMiB = 16
	if raw := getenv("TIMONE_RETAINED_MIB"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 8 || value > 128 {
			return c, fmt.Errorf("TIMONE_RETAINED_MIB requires an integer from 8 to 128")
		}
		c.RetainedMiB = value
	}
	c.GitOps = integrations.DesiredOptions{ArgoURL: getenv("TIMONE_ARGO_URL"), ArgoTokenFile: getenv("TIMONE_ARGO_TOKEN_FILE"), RenderedURL: getenv("TIMONE_FLUX_RENDERED_URL"), RenderedTokenFile: getenv("TIMONE_FLUX_RENDERED_TOKEN_FILE"), CAFile: getenv("TIMONE_GITOPS_CA_FILE"), Destination: getenv("TIMONE_GITOPS_DESTINATION")}
	if err := c.GitOps.Validate(); err != nil {
		return c, err
	}
	c.Diagnostics = model.DefaultDiagnosticPolicy()
	for _, field := range []struct {
		name string
		dest *int
	}{
		{"TIMONE_DIAGNOSTICS_STARTUP_GRACE_SECONDS", &c.Diagnostics.StartupGraceSeconds},
		{"TIMONE_DIAGNOSTICS_WARNING_EVENT_WINDOW_SECONDS", &c.Diagnostics.WarningEventWindowSeconds},
		{"TIMONE_DIAGNOSTICS_RESTART_THRESHOLD", &c.Diagnostics.RestartThreshold},
	} {
		if raw := getenv(field.name); raw != "" {
			// Decimal digits only: reject units, signs, fractions and whitespace.
			if strings.Trim(raw, "0123456789") != "" {
				return c, fmt.Errorf("%s requires a whole decimal integer", field.name)
			}
			value, err := strconv.Atoi(raw)
			if err != nil {
				return c, fmt.Errorf("%s is outside the supported integer range", field.name)
			}
			*field.dest = value
		}
	}
	if err := c.Diagnostics.Validate(); err != nil {
		return c, err
	}
	c.Prometheus = prometheus.Options{URL: getenv("TIMONE_PROMETHEUS_URL"), TokenFile: getenv("TIMONE_PROMETHEUS_TOKEN_FILE"), CAFile: getenv("TIMONE_PROMETHEUS_CA_FILE"), Cluster: getenv("TIMONE_PROMETHEUS_CLUSTER")}
	switch getenv("TIMONE_PROMETHEUS_ALLOW_HTTP") {
	case "", "false":
	case "true":
		c.Prometheus.AllowHTTP = true
	default:
		return c, fmt.Errorf("TIMONE_PROMETHEUS_ALLOW_HTTP must be true or false")
	}
	if err := prometheus.Validate(c.Prometheus); err != nil {
		return c, err
	}
	if raw := getenv("TIMONE_CUSTOM_RESOURCES"); raw != "" {
		seen := map[string]bool{}
		for _, entry := range strings.Split(raw, ",") {
			name := strings.TrimSpace(entry)
			if !model.ValidCustomDefinition(name) || seen[name] || len(c.CustomResources) >= 32 {
				return c, fmt.Errorf("TIMONE_CUSTOM_RESOURCES requires up to 32 distinct explicit CRD names")
			}
			seen[name] = true
			c.CustomResources = append(c.CustomResources, name)
		}
	}
	switch getenv("TIMONE_ALLOW_EXEC_AUTH") {
	case "", "false":
	case "true":
		c.AllowExecAuth = true
	default:
		return c, fmt.Errorf("TIMONE_ALLOW_EXEC_AUTH must be true or false")
	}
	switch getenv("TIMONE_HELM_DETAILS") {
	case "", "false":
	case "true":
		c.HelmDetails = true
	default:
		return c, fmt.Errorf("TIMONE_HELM_DETAILS must be true or false")
	}
	if c.Mode != "demo" && c.Mode != "local" && c.Mode != "incluster" {
		return c, fmt.Errorf("TIMONE_MODE must be demo, local, or incluster")
	}
	host, _, e := net.SplitHostPort(c.Listen)
	if e != nil {
		return c, fmt.Errorf("invalid listen address")
	}
	ip := net.ParseIP(host)
	c.AuthMode = env("TIMONE_AUTH_MODE", "token")
	c.TokenFile = getenv("TIMONE_TOKEN_FILE")
	if c.AuthMode != "token" && c.AuthMode != "proxy" {
		return c, fmt.Errorf("TIMONE_AUTH_MODE must be token or proxy")
	}
	if c.Token != "" && c.TokenFile != "" {
		return c, fmt.Errorf("configure only one of TIMONE_TOKEN or TIMONE_TOKEN_FILE")
	}
	configuredToken := c.Token
	if c.TokenFile != "" {
		var err error
		configuredToken, err = auth.ReadTokenFile(c.TokenFile)
		if err != nil {
			return c, err
		}
	}
	if configuredToken != "" && !auth.ValidToken(configuredToken) {
		return c, fmt.Errorf("authentication token must be 32–512 printable ASCII characters without spaces")
	}
	if (ip == nil || !ip.IsLoopback() || c.Mode == "incluster" || c.AuthMode == "proxy") && configuredToken == "" {
		return c, fmt.Errorf("shared listeners and proxy/incluster modes require TIMONE_TOKEN or TIMONE_TOKEN_FILE")
	}
	if raw := getenv("TIMONE_TRUSTED_PROXIES"); raw != "" {
		for _, entry := range strings.Split(raw, ",") {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(entry))
			if err != nil || prefix.Bits() == 0 {
				return c, fmt.Errorf("TIMONE_TRUSTED_PROXIES requires explicit non-universal IP CIDRs")
			}
			c.TrustedProxies = append(c.TrustedProxies, prefix.Masked())
		}
	}
	if raw := getenv("TIMONE_LOGIN_TRUSTED_PROXIES"); raw != "" {
		if c.AuthMode != "token" {
			return c, fmt.Errorf("TIMONE_LOGIN_TRUSTED_PROXIES is only used in token mode")
		}
		for _, entry := range strings.Split(raw, ",") {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(entry))
			if err != nil || prefix.Bits() == 0 {
				return c, fmt.Errorf("TIMONE_LOGIN_TRUSTED_PROXIES requires explicit non-universal CIDRs")
			}
			c.LoginTrustedProxies = append(c.LoginTrustedProxies, prefix.Masked())
		}
	}
	if c.AuthMode == "proxy" && len(c.TrustedProxies) == 0 {
		return c, fmt.Errorf("proxy mode requires TIMONE_TRUSTED_PROXIES")
	}
	if c.AuthMode != "proxy" && len(c.TrustedProxies) > 0 {
		return c, fmt.Errorf("TIMONE_TRUSTED_PROXIES is only used in proxy mode")
	}
	switch env("TIMONE_ACCESS_LOG", "true") {
	case "true":
		c.AccessLog = true
	case "false":
	default:
		return c, fmt.Errorf("TIMONE_ACCESS_LOG must be true or false")
	}
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return c, fmt.Errorf("TIMONE_PUBLIC_URL must be an HTTP(S) origin without credentials, path, query or fragment")
		}
		if u.Scheme != "https" {
			host := u.Hostname()
			address := net.ParseIP(host)
			if host != "localhost" && (address == nil || !address.IsLoopback()) {
				return c, fmt.Errorf("public non-loopback origins require HTTPS")
			}
		}
		if port := u.Port(); port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return c, fmt.Errorf("invalid public origin port")
			}
		}
		u.Host = strings.ToLower(u.Host)
		if u.Scheme == "https" {
			u.Host = strings.TrimSuffix(u.Host, ":443")
		} else {
			u.Host = strings.TrimSuffix(u.Host, ":80")
		}
		c.PublicURL = u.Scheme + "://" + u.Host
	}
	if c.AuthMode == "proxy" && !strings.HasPrefix(c.PublicURL, "https://") {
		return c, fmt.Errorf("proxy mode requires an explicit HTTPS TIMONE_PUBLIC_URL")
	}

	return c, nil
}
