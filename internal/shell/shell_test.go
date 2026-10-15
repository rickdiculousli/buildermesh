package shell

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestCommandRunsLineWithArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the unix shell")
	}
	c := Command(context.Background(), "echo one")
	if len(c.Args) != 3 || c.Args[0] != "sh" || c.Args[1] != "-c" || c.Args[2] != "echo one" {
		t.Fatalf("no args: %q", c.Args)
	}
	out, err := Command(context.Background(), `printf '%s|' "x\"y"`, "a b", "c").Output()
	if err != nil || strings.TrimSpace(string(out)) != `x"y|a b|c|` {
		t.Fatalf("%q %v", out, err)
	}
}
