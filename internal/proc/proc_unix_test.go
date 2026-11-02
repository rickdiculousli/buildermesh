//go:build !windows

package proc

import (
	"os/exec"
	"strings"
	"testing"
)

// A grouped command leads a session of its own, so it has no controlling
// terminal for an interactive descendant to take (which stops the run when
// it's started from a terminal).
func TestGroupLeadsItsOwnSession(t *testing.T) {
	cmd := exec.Command("sh", "-c", `echo $$; ps -o sid= -p $$`)
	Group(cmd)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	f := strings.Fields(string(out))
	if len(f) != 2 || f[0] != f[1] {
		t.Fatalf("pid and session id: %q, want one number twice", out)
	}
}
