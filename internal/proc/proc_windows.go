//go:build windows

package proc

import (
	"os/exec"
	"strconv"
	"syscall"
)

// group keeps a SysProcAttr the caller set (the shell's CmdLine).
func group(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
}

// killGroup kills the tree under the command's pid; once the command has
// exited the pid may be someone else's, so nothing is done then (orphaned
// descendants are not found on Windows).
func killGroup(cmd *exec.Cmd) error {
	if cmd.ProcessState != nil {
		return nil
	}
	if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
