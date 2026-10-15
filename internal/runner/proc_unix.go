//go:build !windows

package runner

import (
	"errors"
	"syscall"
)

// detached starts a child in its own session, so it outlives the caller and
// a signal to the caller's terminal never reaches it.
func detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

// alive reports whether pid names a live process (EPERM: alive, not ours).
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// kill ends pid's process group (its session, since detached made it the
// leader), so the tools claude spawned go with it.
func kill(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}
