//go:build windows

package runner

import (
	"os"
	"syscall"
)

const (
	detachedProcess         = 0x8    // DETACHED_PROCESS
	processQueryLimitedInfo = 0x1000 // PROCESS_QUERY_LIMITED_INFORMATION
	stillActive             = 259    // STILL_ACTIVE
)

// detached starts a child with no console in its own process group, so it
// outlives the caller and the caller's Ctrl-C never reaches it.
func detached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess}
}

// alive reports whether pid names a process that has not exited.
func alive(pid int) bool {
	h, err := syscall.OpenProcess(processQueryLimitedInfo, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// kill terminates pid.
func kill(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
