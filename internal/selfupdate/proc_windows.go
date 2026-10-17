//go:build windows

package selfupdate

import "syscall"

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
)

// pidAlive: the process opens and has not exited.
func pidAlive(pid int) bool {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
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
