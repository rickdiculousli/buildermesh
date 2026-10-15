// Package shell runs a command line through the platform's shell: sh on
// Unix, cmd on Windows. Nothing else in bmesh names a shell.
package shell

import (
	"context"
	"os/exec"
)

// Command is a command that runs line through the shell with args appended
// as positional arguments (`"$@"` under sh; after the line under cmd /C,
// which gets the whole line raw, so a quote inside it reaches cmd as
// written).
func Command(ctx context.Context, line string, args ...string) *exec.Cmd {
	return command(ctx, line, args)
}
