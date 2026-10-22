// Command smoke exercises a built demo binary or a local container image.
// It never reads kubeconfig or contacts a real cluster.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

func check(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("smoke check failed: %v", r)
		}
	}()
	binary := flag.String("binary", "./bin/timone", "built binary path")
	image := flag.String("image", "", "local image to run instead of binary")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	raw := make([]byte, 32)
	_, err = rand.Read(raw)
	if err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	env := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "TIMONE_") {
			env = append(env, e)
		}
	}
	env = append(env, "TIMONE_ENV=development", "TIMONE_MODE=demo", "TIMONE_LISTEN=127.0.0.1:0", "TIMONE_ACCESS_LOG=false", "TIMONE_AUTH_MODE=token", "TIMONE_DIAGNOSTICS_STARTUP_GRACE_SECONDS=420", "TIMONE_DIAGNOSTICS_WARNING_EVENT_WINDOW_SECONDS=600", "TIMONE_DIAGNOSTICS_RESTART_THRESHOLD=8")
	base := ""
	var stop func() error
	tokenFile := ""
	if *image == "" {
		dir, err := os.MkdirTemp("", "timone-smoke-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		tokenFile = filepath.Join(dir, "token")
		if err := os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
			return err
		}
		env = append(env, "TIMONE_TOKEN_FILE="+tokenFile)
		cmd := exec.CommandContext(ctx, *binary)
		cmd.Env = env
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		cmd.Stderr = io.Discard
		if err := cmd.Start(); err != nil {
			return err
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		defer func() { _ = cmd.Process.Kill() }()
		addresses := make(chan string, 1)
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				var entry struct{ Msg, Address string }
				if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry.Msg == "Timone starting" {
					addresses <- entry.Address
					return
				}
			}
			addresses <- ""
		}()
		select {
		case address := <-addresses:
			check(address != "", "binary did not reach startup (loopback listening may be prohibited)")
			base = "http://" + address
		case <-time.After(10 * time.Second):
			return errors.New("binary startup timed out")
		}
		stop = func() error {
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				return err
			}
			select {
			case err := <-done:
				return err
			case <-time.After(10 * time.Second):
				return errors.New("graceful shutdown timed out")
			}
		}
	} else {
		// Docker receives a synthetic test credential through its environment, not a command argument.
		args := []string{"run", "-d", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges:true", "-p", "127.0.0.1::8080", "-e", "TIMONE_ENV=development", "-e", "TIMONE_MODE=demo", "-e", "TIMONE_LISTEN=0.0.0.0:8080", "-e", "TIMONE_ACCESS_LOG=false", "-e", "TIMONE_TOKEN", "-e", "TIMONE_DIAGNOSTICS_STARTUP_GRACE_SECONDS=420", "-e", "TIMONE_DIAGNOSTICS_WARNING_EVENT_WINDOW_SECONDS=600", "-e", "TIMONE_DIAGNOSTICS_RESTART_THRESHOLD=8", *image}
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Env = append(env, "TIMONE_TOKEN="+token)
		out, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("container start failed: %w", err)
		}
		id := strings.TrimSpace(string(out))
		defer func() { _ = exec.Command("docker", "rm", "-f", id).Run() }()
		port, err := exec.CommandContext(ctx, "docker", "port", id, "8080/tcp").Output()
		if err != nil {
			return err
		}
		base = "http://" + strings.TrimSpace(string(port))
		stop = func() error {
			if err := exec.CommandContext(ctx, "docker", "stop", "-t", "10", id).Run(); err != nil {
				return err
			}
			exit, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.ExitCode}}", id).Output()
			if err != nil {
				return err
			}
			if strings.TrimSpace(string(exit)) != "0" {
				return errors.New("container did not exit cleanly")
			}
			return nil
		}
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(method, path, body, credential string, cookie *http.Cookie) (int, []byte, http.Header) {
		r, err := http.NewRequestWithContext(ctx, method, base+path, strings.NewReader(body))
		check(err == nil, "request creation failed")
		if credential != "" {
			r.Header.Set("Authorization", "Bearer "+credential)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if path == "/mcp" {
			r.Header.Set("Accept", "application/json, text/event-stream")
		}
		resp, err := client.Do(r)
		check(err == nil, "HTTP request failed")
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		check(err == nil, "response read failed")
		return resp.StatusCode, b, resp.Header
	}
	ready := false
	for i := 0; i < 30; i++ {
		r, err := client.Get(base + "/healthz")
		if err == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	check(ready, "health endpoint unavailable")
	status, html, _ := call("GET", "/resources/Pod/production/checkout-api-7c8d9f-m4n7q", "", "", nil)
	check(status == 200 && strings.Contains(string(html), "Timone"), "embedded SPA shell unavailable at a resource URL")
	assets := regexp.MustCompile(`(?:src|href)="(/assets/[^"\s]+)"`).FindAllSubmatch(html, -1)
	js, css := false, false
	for _, asset := range assets {
		status, body, headers := call("GET", string(asset[1]), "", "", nil)
		check(status == 200 && len(body) > 0, "embedded frontend asset unavailable")
		if strings.HasSuffix(string(asset[1]), ".js") {
			js = true
			check(strings.Contains(headers.Get("Content-Type"), "javascript"), "JavaScript asset returned an incorrect content type")
		}
		if strings.HasSuffix(string(asset[1]), ".css") {
			css = true
			check(strings.Contains(headers.Get("Content-Type"), "text/css"), "CSS asset returned an incorrect content type")
		}
	}
	check(js && css, "built JavaScript and CSS assets are missing from the embedded shell")
	status, _, _ = call("GET", "/api/v1/overview", "", "", nil)
	check(status == 401, "anonymous overview access allowed")
	status, b, _ := call("GET", "/api/v1/overview", "", token, nil)
	check(status == 200 && strings.Contains(string(b), `"mode":"demo"`), "demo overview unavailable")
	for _, path := range []string{"/api/v1/overview", "/api/v1/issues", "/api/v1/issues/issue-Deployment:production:checkout-api"} {
		status, b, _ = call("GET", path, "", token, nil)
		var result struct {
			Policy struct {
				Startup  int `json:"startupGraceSeconds"`
				Window   int `json:"warningEventWindowSeconds"`
				Restarts int `json:"restartThreshold"`
			} `json:"diagnosticPolicy"`
		}
		check(status == 200 && json.Unmarshal(b, &result) == nil && result.Policy.Startup == 420 && result.Policy.Window == 600 && result.Policy.Restarts == 8, "configured diagnostic policy not shared by runtime endpoints")
	}
	status, b, _ = call("GET", "/api/v1/issues/issue-databases.platform.timone.io:production:orders-db", "", token, nil)
	check(status == 200 && strings.Contains(string(b), `"definition":"databases.platform.timone.io"`) && strings.Contains(string(b), `"version":"v1"`) && strings.Contains(string(b), "Ready=False at generation 2") && !strings.Contains(string(b), "SIMULATED-OMITTED-CUSTOM-SECRET"), "custom diagnostic identity or evidence unavailable, or payload exposed")
	status, b, _ = call("GET", "/api/v1/issues", "", token, nil)
	check(status == 200 && strings.Contains(string(b), `"customDiagnostics":{"state":"available"`) && strings.Contains(string(b), `"readinessUnknown":2`), "custom diagnostic coverage unavailable")
	status, b, _ = call("GET", "/api/v1/resources/Secret/production/checkout-db", "", token, nil)
	check(status == 200 && !strings.Contains(string(b), `"data":`), "Secret payload exposed")
	status, b, _ = call("GET", "/api/v1/helm/releases/production/checkout-api", "", token, nil)
	check(status == 200 && strings.Contains(string(b), `"chartVersion"`), "Helm history unavailable")
	status, b, _ = call("GET", "/api/v1/metrics?namespace=production", "", token, nil)
	var budgets struct {
		Allocations []struct {
			Node  string `json:"node"`
			Scope string `json:"scope"`
			CPU   struct {
				State    string   `json:"state"`
				Total    *float64 `json:"total"`
				Subtotal *float64 `json:"knownSubtotal"`
				Missing  *int     `json:"missingPods"`
			} `json:"cpuLimits"`
		} `json:"allocations"`
	}
	check(status == 200 && json.Unmarshal(b, &budgets) == nil, "node limit budgets unavailable")
	finiteLimits, missingLimits := false, false
	for _, row := range budgets.Allocations {
		check(row.Scope == "namespace production only", "node limit budget namespace scope lost")
		if row.Node == "harbor-worker-01" {
			missingLimits = row.CPU.State == "incomplete" && row.CPU.Total == nil && row.CPU.Subtotal != nil && row.CPU.Missing != nil && *row.CPU.Missing == 2
		}
		if row.Node == "harbor-worker-02" {
			finiteLimits = row.CPU.State == "finite" && row.CPU.Total != nil && *row.CPU.Total > 0
		}
	}
	check(finiteLimits && missingLimits, "finite and incomplete node limit budgets were conflated")
	status, b, _ = call("GET", "/api/v1/metrics/history?kind=Node&name=harbor-worker-01&range=1h", "", token, nil)
	check(status == 200 && strings.Contains(string(b), `"source":"simulated"`) && strings.Contains(string(b), `"points"`), "simulated metrics history unavailable")
	status, b, _ = call("GET", "/api/v1/topology", "", token, nil)
	check(status == 200 && strings.Contains(string(b), `"accepted":"False"`) && strings.Contains(string(b), "Matching ReferenceGrant:"), "Gateway evidence unavailable")
	status, b, _ = call("GET", "/api/v1/search?search=orders-db&namespace=production", "", token, nil)
	check(status == 200 && strings.Contains(string(b), `"definition":"databases.platform.timone.io"`) && strings.Contains(string(b), `"complete":true`), "custom search identity/coverage unavailable")
	status, b, _ = call("GET", "/api/v1/custom-resource-types", "", token, nil)
	check(status == 200 && strings.Contains(string(b), "databases.platform.timone.io"), "custom type discovery unavailable")
	status, b, _ = call("GET", "/api/v1/custom-resources/databases.platform.timone.io/production/orders-db", "", token, nil)
	check(status == 200 && strings.Contains(string(b), "BackupStorageUnavailable") && !strings.Contains(string(b), "SIMULATED-OMITTED-CUSTOM-SECRET"), "custom projection unavailable or sensitive fields exposed")
	status, b, _ = call("GET", "/api/v1/serviceaccounts/production/default/grants", "", token, nil)
	check(status == 200 && strings.Contains(string(b), `"identity":"system:serviceaccount:production:default"`) && strings.Contains(string(b), `"storefront-config"`) && strings.Contains(string(b), `"resolution":"unavailable"`), "ServiceAccount grant evidence unavailable")
	status, _, _ = call("POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"timone-smoke","version":"1"}}}`, token, nil)
	check(status == 200, "authenticated MCP initialization failed")
	status, _, headers := call("POST", "/api/v1/session", `{"token":"`+token+`"}`, "", nil)
	check(status == 200, "login failed")
	response := &http.Response{Header: headers}
	cookies := response.Cookies()
	check(len(cookies) == 1 && cookies[0].Value != token, "unsafe browser cookie")
	cookie := cookies[0]
	status, _, _ = call("GET", "/api/v1/overview", "", "", cookie)
	check(status == 200, "session authentication failed")
	status, _, _ = call("DELETE", "/api/v1/session", "", "", cookie)
	check(status == 200, "logout failed")
	status, _, _ = call("GET", "/api/v1/overview", "", "", cookie)
	check(status == 401, "revoked session replay accepted")
	request, _ := http.NewRequestWithContext(ctx, "GET", base+"/api/v1/stream", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	stream, err := client.Do(request)
	check(err == nil && stream.StatusCode == 200, "SSE connection failed")
	line, err := bufio.NewReader(stream.Body).ReadString('\n')
	stream.Body.Close()
	check(err == nil && strings.HasPrefix(line, "id:"), "SSE initial synchronization missing")
	if tokenFile != "" {
		_, _ = rand.Read(raw)
		newToken := base64.RawURLEncoding.EncodeToString(raw)
		check(os.WriteFile(tokenFile, []byte(newToken), 0600) == nil, "rotation failed")
		status, _, _ = call("GET", "/api/v1/overview", "", token, nil)
		check(status == 401, "old token survived rotation")
		status, _, _ = call("GET", "/api/v1/overview", "", newToken, nil)
		check(status == 200, "rotated token rejected")
		check(os.Remove(tokenFile) == nil, "credential removal failed")
		status, _, _ = call("GET", "/readyz", "", "", nil)
		check(status == 503, "unavailable token source reported ready")
		status, _, _ = call("GET", "/api/v1/overview", "", newToken, nil)
		check(status == 503, "missing token source failed open")
		check(os.WriteFile(tokenFile, []byte(newToken), 0600) == nil, "credential restoration failed")
	}
	check(stop() == nil, "process did not shut down gracefully")
	if tokenFile != "" {
		fmt.Println("File credential rotation, missing-file fail-closed behavior and recovery checks passed.")
	}
	fmt.Println("Runtime smoke checks passed: embedded UI/assets, protected demo API, redaction, Helm, history, topology, global/custom search, RBAC grants, diagnostic policy/custom issues, node limit budgets, MCP, opaque/revocable sessions, SSE and graceful shutdown.")
	return nil
}
