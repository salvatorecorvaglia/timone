package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"
)

type cliOptions struct {
	overrides  map[string]string
	version    bool
	noBrowser  bool
	parentPipe bool
}

func parseCLI(args []string, output io.Writer) (cliOptions, error) {
	o := cliOptions{overrides: map[string]string{}}
	// The executable defaults to a real local cluster. Explicit environment modes
	// keep existing demo, container and Helm invocations working.
	if os.Getenv("TIMONE_MODE") == "" {
		o.overrides["TIMONE_MODE"] = "local"
	}
	flags := flag.NewFlagSet("timone", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: timone [options]\n       kubectl timone [options]")
		fmt.Fprintln(output, "\nRead-only Kubernetes insights and diagnostics.")
		fmt.Fprintln(output, "Uses your current kubeconfig context and opens the local UI in your browser.")
		fmt.Fprintln(output, "Explicit flags override environment variables. Default listen: 127.0.0.1:8080.")
		fmt.Fprintln(output, "\nOptions:")
		flags.PrintDefaults()
		fmt.Fprintln(output, "\nExamples:\n  timone\n  timone --context production --namespace payments\n  TIMONE_ENV=development timone --mode demo --no-browser\n\nThe kubectl form requires kubectl-timone on PATH. See .env.example for advanced settings.")
	}
	keys := map[string]string{"allow-exec-auth": "TIMONE_ALLOW_EXEC_AUTH"}
	for _, entry := range []struct{ name, env, help string }{
		{"mode", "TIMONE_MODE", "Run mode: local (default), incluster, or demo (requires TIMONE_ENV=development)"},
		{"kubeconfig", "KUBECONFIG", "Kubeconfig path or OS-separated path list (default: standard lookup)"},
		{"context", "TIMONE_CONTEXT", "Kubeconfig context (default: current context)"},
		{"namespace", "TIMONE_NAMESPACE", "Namespace scope; an explicit empty value selects all readable namespaces"},
		{"listen", "TIMONE_LISTEN", "HTTP listen address; non-loopback requires authentication"},
	} {
		flags.String(entry.name, "", entry.help)
		keys[entry.name] = entry.env
	}
	flags.Bool("allow-exec-auth", false, "Allow trusted non-interactive kubeconfig credential plugins")
	flags.BoolVar(&o.noBrowser, "no-browser", false, "Do not open the browser automatically")
	flags.BoolVar(&o.parentPipe, "parent-pipe", false, "Stop when the supervising process closes standard input")
	flags.BoolVar(&o.version, "version", false, "Print the application version and exit")
	if err := flags.Parse(args); err != nil {
		return o, err
	}
	if flags.NArg() != 0 {
		return o, fmt.Errorf("unexpected argument %q; use --help for usage", flags.Arg(0))
	}
	flags.Visit(func(f *flag.Flag) {
		if key, ok := keys[f.Name]; ok {
			o.overrides[key] = f.Value.String()
		}
	})
	return o, nil
}

// Never send a wildcard bind address, credentials, or a remote origin to a browser.
func browserURL(address net.Addr) string {
	host, port, err := net.SplitHostPort(address.String())
	if err != nil {
		return ""
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return ""
	}
	return "http://" + net.JoinHostPort(host, port)
}

func openBrowser(parent context.Context, address string) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{address}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", address}
	default:
		command, args = "xdg-open", []string{address}
	}
	return exec.CommandContext(ctx, command, args...).Run()
}
