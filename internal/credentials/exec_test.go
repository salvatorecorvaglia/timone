package credentials

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "timone-exec-fixture" {
		if len(os.Args) > 2 && os.Args[2] == "environment" {
			_, _ = os.Stdout.WriteString(os.Getenv("TIMONE_TOKEN") + os.Getenv("TIMONE_TOKEN_FILE") + os.Getenv("TIMONE_PROMETHEUS_TOKEN_FILE"))
			os.Exit(0)
		}
		if len(os.Args) > 2 && os.Args[2] == "sleep" {
			time.Sleep(time.Minute)
			os.Exit(0)
		}
		if len(os.Args) > 2 && os.Args[2] == "large" {
			_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), outputLimit+1))
			os.Exit(0)
		}
		_, _ = os.Stderr.WriteString("sensitive-marker\n")
		_, _ = os.Stdout.WriteString(`{"kind":"ExecCredential","status":{"token":"fixture-token"}}`)
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestCredentialHelperSeparatesOutput(t *testing.T) {
	exe, _ := os.Executable()
	var out bytes.Buffer
	if e := Run(context.Background(), []string{exe, "timone-exec-fixture"}, &out); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(out.Bytes(), []byte("sensitive-marker")) || !bytes.Contains(out.Bytes(), []byte("fixture-token")) {
		t.Fatal("invalid plugin output boundary")
	}
}
func TestCredentialHelperTimeoutAndOutputLimit(t *testing.T) {
	exe, _ := os.Executable()
	for _, mode := range []string{"sleep", "large"} {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		var out bytes.Buffer
		start := time.Now()
		e := Run(ctx, []string{exe, "timone-exec-fixture", mode}, &out)
		cancel()
		if e != ErrPlugin || out.Len() != 0 || time.Since(start) > 3*time.Second {
			t.Fatalf("helper limit failed: %s %v", mode, e)
		}
	}
}

func TestCredentialHelperDoesNotPassWorkspaceToken(t *testing.T) {
	t.Setenv("TIMONE_TOKEN", "sensitive-marker")
	t.Setenv("TIMONE_TOKEN_FILE", "sensitive-path")
	t.Setenv("TIMONE_PROMETHEUS_TOKEN_FILE", "sensitive-prometheus-path")
	exe, _ := os.Executable()
	var out bytes.Buffer
	if e := Run(context.Background(), []string{exe, "timone-exec-fixture", "environment"}, &out); e != nil || out.Len() != 0 {
		t.Fatal("workspace token passed to external credential command")
	}
}
