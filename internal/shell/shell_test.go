package shell

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestArgvRunsLineWithArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the unix shell")
	}
	a := Argv("echo one")
	if len(a) != 3 || a[0] != "sh" || a[1] != "-c" || a[2] != "echo one" {
		t.Fatalf("no args: %q", a)
	}
	a = Argv("printf '%s|'", "a b", "c")
	out, err := exec.CommandContext(context.Background(), a[0], a[1:]...).Output()
	if err != nil || strings.TrimSpace(string(out)) != "a b|c|" {
		t.Fatalf("%q %v", out, err)
	}
}
