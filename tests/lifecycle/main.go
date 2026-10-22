// Verify the real image entrypoint as PID 1. No shell replaces the entrypoint,
// host ports are not published, and only uniquely named test containers are removed.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	image := flag.String("image", "timone:ci", "local image to test")
	platform := flag.String("platform", "", "optional linux/amd64 or linux/arm64")
	flag.Parse()
	if err := run(*image, *platform); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func docker(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s failed: %s", args[0], strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
func run(image, platform string) error {
	if platform != "" && platform != "linux/amd64" && platform != "linux/arm64" {
		return fmt.Errorf("invalid test platform")
	}
	dir, err := os.MkdirTemp("", "timone-lifecycle-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return err
	}
	envfile := filepath.Join(dir, "environment")
	if err = os.WriteFile(envfile, []byte("TIMONE_TOKEN="+hex.EncodeToString(secret)+"\nTIMONE_ACCESS_LOG=false\nTIMONE_MODE=demo\nTIMONE_ENV=development\n"), 0600); err != nil {
		return err
	}
	for _, signal := range []string{"SIGTERM", "SIGINT"} {
		name := "timone-lifecycle-" + hex.EncodeToString(secret[:5]) + "-" + strings.ToLower(signal)
		if err = check(image, platform, name, envfile, signal); err != nil {
			return err
		}
	}
	fmt.Println("PID 1 SIGTERM/SIGINT checks passed: active SSE closed, exit 0, non-root/read-only/capability restrictions, no published sockets.")
	return nil
}
func check(image, platform, name, envfile, signal string) (err error) {
	args := []string{"run", "-d", "--name", name, "--network", "none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges:true", "--env-file", envfile}
	if platform != "" {
		args = append(args, "--platform", platform)
	}
	args = append(args, image)
	if _, err = docker(args...); err != nil {
		return err
	}
	defer func() {
		if _, e := docker("rm", "-f", name); e != nil && err == nil {
			err = e
		}
	}()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, e := docker("exec", name, "wget", "-qO-", "http://127.0.0.1:8080/healthz"); e == nil {
			break
		}
		if time.Now().After(deadline) {
			logs, _ := docker("logs", "--tail", "20", name)
			return fmt.Errorf("entrypoint did not become healthy: %s", logs)
		}
		time.Sleep(100 * time.Millisecond)
	}
	probe := `test "$(id -u)" = 10001 && test "$(cat /proc/1/comm)" = timone && grep -q '^NoNewPrivs:.*1' /proc/1/status && grep -q '^CapEff:.*0000000000000000' /proc/1/status && ! touch /usr/local/bin/timone-lifecycle-probe`
	if _, err = docker("exec", name, "sh", "-c", probe); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := exec.CommandContext(ctx, "docker", "exec", name, "sh", "-c", `wget -qO- --header="Authorization: Bearer $TIMONE_TOKEN" http://127.0.0.1:8080/api/v1/stream`)
	pipe, e := stream.StdoutPipe()
	if e != nil {
		return e
	}
	if e = stream.Start(); e != nil {
		return e
	}
	defer func() { cancel(); _ = stream.Wait() }()
	ready := make(chan bool, 1)
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		data := make([]byte, 256)
		n, _ := pipe.Read(data)
		ready <- n > 0
		_, _ = io.Copy(io.Discard, pipe)
	}()
	select {
	case ok := <-ready:
		if !ok {
			return fmt.Errorf("SSE did not establish")
		}
	case <-time.After(5 * time.Second):
		return fmt.Errorf("SSE startup timed out")
	}
	start := time.Now()
	if _, err = docker("kill", "--signal", signal, name); err != nil {
		return err
	}
	exit, err := docker("wait", name)
	if err != nil {
		return err
	}
	if exit != "0" || time.Since(start) > 10*time.Second {
		return fmt.Errorf("%s failed graceful exit: %s", signal, exit)
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		return fmt.Errorf("SSE socket stayed open after process exit")
	}
	out, err := docker("inspect", name)
	if err != nil {
		return err
	}
	var states []struct {
		State struct {
			Running, OOMKilled bool
			ExitCode           int
		}
		HostConfig      struct{ ReadonlyRootfs bool }
		NetworkSettings struct{ Ports map[string]any }
	}
	if err = json.Unmarshal([]byte(out), &states); err != nil {
		return err
	}
	if len(states) != 1 || states[0].State.Running || states[0].State.OOMKilled || !states[0].HostConfig.ReadonlyRootfs || len(states[0].NetworkSettings.Ports) != 0 {
		return fmt.Errorf("unexpected final container state")
	}
	fmt.Println(signal, "clean exit", time.Since(start).Round(time.Millisecond))
	return nil
}
