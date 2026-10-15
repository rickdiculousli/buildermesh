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

func killGroup(cmd *exec.Cmd) error {
	if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
