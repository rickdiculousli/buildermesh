//go:build !windows

package proc

import (
	"errors"
	"os/exec"
	"syscall"
)

// group starts cmd in a session of its own, which is also its own process
// group: with no controlling terminal, a descendant that reaches for one (an
// interactive shell) can't stop the run, as a background group of the
// caller's terminal is stopped.
func group(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}

func killGroup(cmd *exec.Cmd) error {
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
