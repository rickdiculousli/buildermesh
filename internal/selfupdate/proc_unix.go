//go:build !windows

package selfupdate

import (
	"errors"
	"syscall"
)

// pidAlive: signal 0 reaches it, or it exists but isn't ours to signal.
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
