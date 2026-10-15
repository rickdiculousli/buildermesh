//go:build windows

package shell

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
)

// command hands cmd the line raw through CmdLine: Go's own argument quoting
// follows the C runtime's rules, which cmd does not read, so a quote in the
// line would reach it as \". An argument with a space is wrapped in quotes.
func command(ctx context.Context, line string, args []string) *exec.Cmd {
	for _, a := range args {
		if a == "" || strings.ContainsAny(a, " \t") {
			a = `"` + a + `"`
		}
		line += " " + a
	}
	c := exec.CommandContext(ctx, "cmd")
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /S /C "` + line + `"`}
	return c
}
