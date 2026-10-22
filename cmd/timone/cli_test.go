package main

import (
	"bytes"
	"errors"
	"flag"
	"net"
	"os"
	"strings"
	"testing"
	"timone/internal/config"
)

func TestCLIConfigurationPrecedenceAndSecurity(t *testing.T) {
	for _, key := range []string{"TIMONE_MODE", "TIMONE_LISTEN", "TIMONE_TOKEN", "TIMONE_TOKEN_FILE", "TIMONE_AUTH_MODE", "TIMONE_PUBLIC_URL", "TIMONE_TRUSTED_PROXIES", "TIMONE_LOGIN_TRUSTED_PROXIES", "TIMONE_ALLOW_EXEC_AUTH"} {
		t.Setenv(key, "")
	}
	load := func(args ...string) (config.Config, error) {
		t.Helper()
		o, err := parseCLI(args, &bytes.Buffer{})
		if err != nil {
			return config.Config{}, err
		}
		return config.LoadWithOverrides(o.overrides)
	}
	c, err := load()
	if err != nil || c.Mode != "local" || c.Listen != "127.0.0.1:8080" || c.AllowExecAuth {
		t.Fatalf("unexpected defaults: %+v, %v", c, err)
	}
	t.Setenv("TIMONE_MODE", "demo")
	t.Setenv("TIMONE_CONTEXT", "old-context")
	t.Setenv("TIMONE_NAMESPACE", "old-namespace")
	t.Setenv("TIMONE_ALLOW_EXEC_AUTH", "true")
	c, err = load("--mode=local", "--context=production", "--namespace=", "--allow-exec-auth=false")
	if err != nil || c.Mode != "local" || c.Context != "production" || c.Namespace != "" || !c.NamespaceExplicit || c.AllowExecAuth {
		t.Fatalf("flags did not override environment: %+v, %v", c, err)
	}
	if value := os.Getenv("TIMONE_MODE"); value != "demo" {
		t.Fatal("command-line parsing changed process environment")
	}
	c, err = load()
	if err != nil || c.Mode != "demo" {
		t.Fatal("explicit environment mode lost", err)
	}
	for _, args := range [][]string{{"--listen=0.0.0.0:8080"}, {"--listen=[::]:8080"}, {"--mode=incluster"}, {"--mode=invalid"}, {"--listen=invalid"}} {
		if _, err := load(args...); err == nil {
			t.Fatalf("unsafe/invalid configuration accepted: %v", args)
		}
	}
}

func TestCLIInformationalAndInvalidArguments(t *testing.T) {
	t.Setenv("TIMONE_MODE", "invalid")
	var output bytes.Buffer
	if _, err := parseCLI([]string{"--help"}, &output); !errors.Is(err, flag.ErrHelp) || !strings.Contains(output.String(), "--mode demo") {
		t.Fatal("help unavailable", output.String(), err)
	}
	o, err := parseCLI([]string{"--version"}, &output)
	if err != nil || !o.version {
		t.Fatal("version unavailable", err)
	}
	for _, args := range [][]string{{"--unknown"}, {"--context"}, {"unexpected"}, {"--", "unexpected"}, {"--allow-exec-auth=maybe"}} {
		if _, err := parseCLI(args, &output); err == nil {
			t.Fatal("invalid arguments accepted", args)
		}
	}
}

func TestBrowserOnlyUsesBoundLoopbackAddress(t *testing.T) {
	for _, tc := range []struct{ address, want string }{
		{"127.0.0.1:12345", "http://127.0.0.1:12345"},
		{"[::1]:12345", "http://[::1]:12345"},
		{"0.0.0.0:8080", ""}, {"[::]:8080", ""}, {"192.0.2.1:8080", ""},
	} {
		address, err := net.ResolveTCPAddr("tcp", tc.address)
		if err != nil {
			t.Fatal(err)
		}
		if got := browserURL(address); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.address, got, tc.want)
		}
	}
}

func TestDemoRequiresDevelopmentEnvironment(t *testing.T) {
	for _, environment := range []string{"", "production", "staging", "developmnt"} {
		t.Run(environment, func(t *testing.T) {
			t.Setenv("TIMONE_ENV", environment)
			for _, key := range []string{"TIMONE_TOKEN", "TIMONE_TOKEN_FILE", "TIMONE_AUTH_MODE", "TIMONE_TRUSTED_PROXIES", "TIMONE_LOGIN_TRUSTED_PROXIES", "TIMONE_PUBLIC_URL"} {
				t.Setenv(key, "")
			}
			err := run(nil, cliOptions{overrides: map[string]string{"TIMONE_MODE": "demo", "TIMONE_LISTEN": "127.0.0.1:0"}})
			if err == nil || !strings.Contains(err.Error(), "TIMONE_ENV=development") {
				t.Fatalf("demo must be rejected outside development: %v", err)
			}
		})
	}
}
