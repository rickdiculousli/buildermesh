//go:build !windows

package shell

import (
	"context"
	"os/exec"
)

func command(ctx context.Context, line string, args []string) *exec.Cmd {
	if len(args) == 0 {
		return exec.CommandContext(ctx, "sh", "-c", line)
	}
	return exec.CommandContext(ctx, "sh", append([]string{"-c", line + ` "$@"`, "sh"}, args...)...)
}
