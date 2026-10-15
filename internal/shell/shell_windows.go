//go:build windows

package shell

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// command hands cmd the line raw through CmdLine: Go's own argument quoting
// follows the C runtime's rules, which cmd does not read, so a quote in the
// line would reach it as \". Every argument is wrapped in quotes, inside
// which cmd reads no operator; an argument holding a quote has no safe
// spelling under cmd and is refused when the command starts.
func command(ctx context.Context, line string, args []string) *exec.Cmd {
	c := exec.CommandContext(ctx, "cmd")
	for _, a := range args {
		if strings.ContainsAny(a, "\"\r\n") {
			c.Err = fmt.Errorf("shell: argument %q cannot be passed to cmd", a)
		}
		line += ` "` + a + `"`
	}
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /S /C "` + line + `"`}
	return c
}
