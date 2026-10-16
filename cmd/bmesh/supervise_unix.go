//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// childAttr leaves the child in the supervisor's process group, so a
// Ctrl-C at the terminal reaches both, as it reaches a plain bmesh serve.
func childAttr(*exec.Cmd) {}

// stopChild asks serve to stop as a service manager does.
func stopChild(p *os.Process) error { return p.Signal(syscall.SIGTERM) }

// detach is Windows only.
func detach(string, []string) (int, error) {
	return 0, errors.New("--detach is Windows only")
}
