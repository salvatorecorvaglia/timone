// Package credentials bounds execution of explicitly trusted kubeconfig plugins.
// It is invoked through an internal CLI command by client-go, never through HTTP/MCP.
package credentials

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const Timeout = 20 * time.Second
const outputLimit = 1 << 20

var ErrPlugin = errors.New("credential plugin failed, timed out, or returned excessive output")

type boundedOutput struct {
	data     []byte
	exceeded bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	space := outputLimit - len(b.data)
	if len(p) > space {
		b.exceeded = true
		p = p[:space]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func Run(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "" {
		return ErrPlugin
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "TIMONE_TOKEN=") && !strings.HasPrefix(entry, "TIMONE_TOKEN_FILE=") && !strings.HasPrefix(entry, "TIMONE_PROMETHEUS_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Stdin = nil
	cmd.Stderr = io.Discard
	out := &boundedOutput{}
	cmd.Stdout = out
	cmd.WaitDelay = time.Second
	if e := cmd.Run(); e != nil || out.exceeded || ctx.Err() != nil {
		return ErrPlugin
	}
	if _, e := stdout.Write(out.data); e != nil {
		return ErrPlugin
	}
	return nil
}
