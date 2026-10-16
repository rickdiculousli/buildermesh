//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	createNoWindow         = 0x08000000 // CREATE_NO_WINDOW
	createBreakawayFromJob = 0x01000000 // CREATE_BREAKAWAY_FROM_JOB
)

// childAttr gives serve a console of its own with no window: the
// supervisor may itself have none (--detach), and a console program started
// from a process without one would open a window.
func childAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
}

// stopChild kills serve: Windows has no stop signal to send a process on
// another console. Jobs it held go back to the inbox when their ack wait
// runs out.
func stopChild(p *os.Process) error { return p.Kill() }

// detach starts exe with args and no console window, out of the task's job
// where the job allows it, and returns its pid without waiting for it.
func detach(exe string, args []string) (int, error) {
	start := func(flags uint32) (int, error) {
		cmd := exec.Command(exe, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
		if err := cmd.Start(); err != nil {
			return 0, err
		}
		pid := cmd.Process.Pid
		return pid, cmd.Process.Release()
	}
	if pid, err := start(createNoWindow | createBreakawayFromJob); err == nil {
		return pid, nil
	}
	return start(createNoWindow) // a job that forbids breakaway refuses the flag
}
