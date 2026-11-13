//go:build !windows

package proc

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/unix"
)

// A grouped command leads a session of its own, so it has no controlling
// terminal for an interactive descendant to take (which stops the run when
// it's started from a terminal).
func TestGroupLeadsItsOwnSession(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	Group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	sid, err := unix.Getsid(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if sid != cmd.Process.Pid {
		t.Fatalf("session id %d, want the child's pid %d", sid, cmd.Process.Pid)
	}
}
