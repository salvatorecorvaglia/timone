package config

import (
	"strings"
	"testing"
	"timone/internal/model"
)

func TestDiagnosticConfigurationDefaultsAndRejectsInvalidValues(t *testing.T) {
	t.Setenv("TIMONE_MODE", "demo")
	t.Setenv("TIMONE_LISTEN", "127.0.0.1:8080")
	t.Setenv("TIMONE_TOKEN", "")
	t.Setenv("TIMONE_TOKEN_FILE", "")
	t.Setenv("TIMONE_AUTH_MODE", "token")
	t.Setenv("TIMONE_TRUSTED_PROXIES", "")
	t.Setenv("TIMONE_PUBLIC_URL", "")
	names := []string{"TIMONE_DIAGNOSTICS_STARTUP_GRACE_SECONDS", "TIMONE_DIAGNOSTICS_WARNING_EVENT_WINDOW_SECONDS", "TIMONE_DIAGNOSTICS_RESTART_THRESHOLD"}
	for _, name := range names {
		t.Setenv(name, "")
	}
	c, err := Load()
	if err != nil || c.Diagnostics != model.DefaultDiagnosticPolicy() {
		t.Fatal(c.Diagnostics, err)
	}
	for i, value := range []string{"0", "86400", "10000"} {
		t.Setenv(names[i], value)
	}
	c, err = Load()
	if err != nil || c.Diagnostics != (model.DiagnosticPolicy{StartupGraceSeconds: 0, WarningEventWindowSeconds: 86400, RestartThreshold: 10000}) {
		t.Fatal(c.Diagnostics, err)
	}
	for _, tc := range []struct{ name, value string }{
		{names[0], "3601"}, {names[1], "59"}, {names[1], "86401"}, {names[2], "0"}, {names[2], "10001"},
	} {
		t.Run(tc.name+tc.value, func(t *testing.T) {
			t.Setenv(tc.name, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
	for _, name := range names {
		for _, value := range []string{"-1", "+1", " 60", "60 ", "1.5", "5m", "NaN", "99999999999999999999999999"} {
			t.Run(name+value, func(t *testing.T) {
				t.Setenv(name, value)
				if _, err := Load(); err == nil {
					t.Fatal("invalid integer accepted")
				}
			})
		}
	}
}

func TestSecureDefaults(t *testing.T) {
	for _, k := range []string{"TIMONE_MODE", "TIMONE_LISTEN", "TIMONE_TOKEN", "TIMONE_PUBLIC_URL"} {
		t.Setenv(k, "")
	}
	c, e := Load()
	if e != nil || c.Listen != "127.0.0.1:8080" || c.Mode != "local" {
		t.Fatalf("unsafe defaults %+v %v", c, e)
	}
	t.Setenv("TIMONE_LISTEN", "0.0.0.0:8080")
	if _, e := Load(); e == nil {
		t.Fatal("anonymous shared deployment accepted")
	}
	t.Setenv("TIMONE_TOKEN", strings.Repeat("a", 32))
	if _, e := Load(); e != nil {
		t.Fatal(e)
	}
	t.Setenv("TIMONE_TOKEN", "short")
	if _, e := Load(); e == nil {
		t.Fatal("weak token accepted")
	}
	t.Setenv("TIMONE_TOKEN", "")
	t.Setenv("TIMONE_MODE", "incluster")
	t.Setenv("TIMONE_LISTEN", "127.0.0.1:8080")
	if _, e := Load(); e == nil {
		t.Fatal("anonymous in-cluster accepted")
	}
}

func TestPrometheusConfigurationIsExplicitAndNeverCarriesURLCredentials(t *testing.T) {
	t.Setenv("TIMONE_MODE", "demo")
	t.Setenv("TIMONE_LISTEN", "127.0.0.1:8080")
	t.Setenv("TIMONE_TOKEN", "")
	t.Setenv("TIMONE_TOKEN_FILE", "")
	t.Setenv("TIMONE_AUTH_MODE", "token")
	t.Setenv("TIMONE_TRUSTED_PROXIES", "")
	t.Setenv("TIMONE_PUBLIC_URL", "")
	for _, name := range []string{"URL", "TOKEN_FILE", "CA_FILE", "CLUSTER", "ALLOW_HTTP"} {
		t.Setenv("TIMONE_PROMETHEUS_"+name, "")
	}
	c, err := Load()
	if err != nil || c.Prometheus.URL != "" {
		t.Fatal(c, err)
	}
	t.Setenv("TIMONE_PROMETHEUS_URL", "https://prometheus.example/api-prefix")
	t.Setenv("TIMONE_PROMETHEUS_TOKEN_FILE", "/run/private-token")
	t.Setenv("TIMONE_PROMETHEUS_CLUSTER", "production")
	c, err = Load()
	if err != nil || c.Prometheus.Cluster != "production" || c.Prometheus.TokenFile != "/run/private-token" {
		t.Fatal(c, err)
	}
	for _, endpoint := range []string{"https://user:password@prometheus.example", "https://prometheus.example?query=up", "http://prometheus.example", "file:///tmp/private"} {
		t.Setenv("TIMONE_PROMETHEUS_URL", endpoint)
		if _, err := Load(); err == nil {
			t.Fatal("unsafe monitoring endpoint accepted")
		}
	}
}

func TestCustomResourceAllowlistIsExplicitBoundedAndDisabledByDefault(t *testing.T) {
	t.Setenv("TIMONE_MODE", "demo")
	t.Setenv("TIMONE_LISTEN", "127.0.0.1:8080")
	t.Setenv("TIMONE_TOKEN", "")
	t.Setenv("TIMONE_TOKEN_FILE", "")
	t.Setenv("TIMONE_AUTH_MODE", "token")
	t.Setenv("TIMONE_TRUSTED_PROXIES", "")
	t.Setenv("TIMONE_PUBLIC_URL", "")
	t.Setenv("TIMONE_CUSTOM_RESOURCES", "")
	c, err := Load()
	if err != nil || len(c.CustomResources) != 0 {
		t.Fatal(c, err)
	}
	t.Setenv("TIMONE_CUSTOM_RESOURCES", "databases.ops.example, profiles.ops.example")
	c, err = Load()
	if err != nil || len(c.CustomResources) != 2 {
		t.Fatal(c, err)
	}
	for _, value := range []string{"*", "pods", "databases.ops.example,databases.ops.example", "databases.ops.example,", "pods/exec.ops.example", strings.Repeat("databases.ops.example,", 33)} {
		t.Setenv("TIMONE_CUSTOM_RESOURCES", value)
		if _, err := Load(); err == nil {
			t.Fatal("unsafe custom allowlist accepted", value)
		}
	}
}

func TestExecAuthRequiresExplicitBoolean(t *testing.T) {
	t.Setenv("TIMONE_MODE", "local")
	t.Setenv("TIMONE_LISTEN", "127.0.0.1:8080")
	t.Setenv("TIMONE_TOKEN", "")
	t.Setenv("TIMONE_ALLOW_EXEC_AUTH", "true")
	c, e := Load()
	if e != nil || !c.AllowExecAuth {
		t.Fatalf("opt-in not loaded %v", e)
	}
	t.Setenv("TIMONE_ALLOW_EXEC_AUTH", "maybe")
	if _, e := Load(); e == nil {
		t.Fatal("invalid exec opt-in accepted")
	}
}

func TestHelmDetailsRequireExplicitOptIn(t *testing.T) {
	t.Setenv("TIMONE_MODE", "local")
	t.Setenv("TIMONE_LISTEN", "127.0.0.1:8080")
	t.Setenv("TIMONE_TOKEN", "")
	t.Setenv("TIMONE_HELM_DETAILS", "")
	c, e := Load()
	if e != nil || c.HelmDetails {
		t.Fatal("Helm details enabled by default")
	}
	t.Setenv("TIMONE_HELM_DETAILS", "true")
	c, e = Load()
	if e != nil || !c.HelmDetails {
		t.Fatal("Helm details opt-in ignored")
	}
	t.Setenv("TIMONE_HELM_DETAILS", "yes")
	if _, e := Load(); e == nil {
		t.Fatal("invalid opt-in accepted")
	}
}

func TestPublicOriginsAndProxyTrustFailClosed(t *testing.T) {
	t.Setenv("TIMONE_MODE", "local")
	t.Setenv("TIMONE_LISTEN", "127.0.0.1:8080")
	t.Setenv("TIMONE_TOKEN", strings.Repeat("a", 32))
	t.Setenv("TIMONE_TOKEN_FILE", "")
	t.Setenv("TIMONE_AUTH_MODE", "token")
	t.Setenv("TIMONE_TRUSTED_PROXIES", "")
	for _, origin := range []string{"http://timone.example", "https://user:password@timone.example", "https://timone.example/path", "https://timone.example?q=1", "https://timone.example#fragment", "not-an-origin"} {
		t.Setenv("TIMONE_PUBLIC_URL", origin)
		if _, err := Load(); err == nil {
			t.Fatalf("unsafe origin accepted %q", origin)
		}
	}
	t.Setenv("TIMONE_PUBLIC_URL", "https://timone.example")
	t.Setenv("TIMONE_AUTH_MODE", "proxy")
	if _, err := Load(); err == nil {
		t.Fatal("proxy without trusted peers accepted")
	}
	t.Setenv("TIMONE_TRUSTED_PROXIES", "0.0.0.0/0")
	if _, err := Load(); err == nil {
		t.Fatal("universal proxy trust accepted")
	}
	t.Setenv("TIMONE_TRUSTED_PROXIES", "10.0.0.0/24")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionAndLoginProxyConfiguration(t *testing.T) {
	for _, name := range []string{"TIMONE_TOKEN", "TIMONE_TOKEN_FILE", "TIMONE_TRUSTED_PROXIES", "TIMONE_LOGIN_TRUSTED_PROXIES", "TIMONE_PUBLIC_URL", "TIMONE_RETAINED_MIB"} {
		t.Setenv(name, "")
	}
	t.Setenv("TIMONE_MODE", "demo")
	t.Setenv("TIMONE_LISTEN", "127.0.0.1:8080")
	t.Setenv("TIMONE_AUTH_MODE", "token")
	c, err := Load()
	if err != nil || c.RetainedMiB != 16 {
		t.Fatal(c.RetainedMiB, err)
	}
	for _, value := range []string{"0", "7", "129", "16.5", "bad"} {
		t.Setenv("TIMONE_RETAINED_MIB", value)
		if _, err = Load(); err == nil {
			t.Fatal("invalid retention accepted", value)
		}
	}
	t.Setenv("TIMONE_RETAINED_MIB", "64")
	t.Setenv("TIMONE_LOGIN_TRUSTED_PROXIES", "10.0.0.0/24,2001:db8::/64")
	if c, err = Load(); err != nil || c.RetainedMiB != 64 || len(c.LoginTrustedProxies) != 2 {
		t.Fatal(c, err)
	}
	for _, value := range []string{"0.0.0.0/0", "::/0", "not-a-cidr"} {
		t.Setenv("TIMONE_LOGIN_TRUSTED_PROXIES", value)
		if _, err = Load(); err == nil {
			t.Fatal("invalid proxy trust accepted", value)
		}
	}
}
