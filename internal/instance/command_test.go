//go:build !windows

package instance

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/kind"
)

func TestCommandInstanceEnvArgsTimeout(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	j := Job{ID: "j1", Kind: "echo", Commit: "c0ffee", Tree: "7ree", From: "routine.echo.f1", Reply: "lead.a",
		Args: []string{"a", "b c"}, Dir: dir}

	r := NewCommand(kind.Kind{Name: "echo", Command: `echo "$BMESH_JOB $BMESH_KIND $BMESH_COMMIT $BMESH_FROM $BMESH_REPLY" >&2; pwd; echo "$BMESH_TREE"`}).Run(ctx, j)
	wd, _ := filepath.EvalSymlinks(dir)
	want := "j1 echo c0ffee routine.echo.f1 lead.a\n" + wd + "\n7ree a b c\n"
	if !r.OK || r.Output != want {
		t.Fatalf("got %+v, want OK output %q", r, want)
	}

	if r := NewCommand(kind.Kind{Command: "exit 3"}).Run(ctx, j); r.OK {
		t.Fatalf("exit 3: got OK")
	}

	j.Args = nil
	start := time.Now()
	r = NewCommand(kind.Kind{Command: "sleep 5", Timeout: time.Second}).Run(ctx, j)
	if r.OK || !strings.Contains(r.Output, "timeout") {
		t.Fatalf("sleep 5 with 1s timeout: got %+v", r)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("timed-out run took %s", d)
	}

	pidfile := filepath.Join(dir, "pid")
	r = NewCommand(kind.Kind{Command: `sleep 20 & echo $! > ` + pidfile + `; wait`, Timeout: time.Second}).Run(ctx, j)
	if r.OK || !strings.Contains(r.Output, "timeout") {
		t.Fatalf("background sleep: got %+v", r)
	}
	waitGone(t, pidfile)

	k, err := kind.Parse("ml", []byte("command = \"\"\"\necho hi\n\"\"\""))
	if err != nil {
		t.Fatal(err)
	}
	if r := NewCommand(k).Run(ctx, Job{Args: []string{"there"}, Dir: dir}); !r.OK || r.Output != "hi there\n" {
		t.Fatalf("multi-line command: got %+v", r)
	}

	cctx, cancel := context.WithCancel(ctx)
	time.AfterFunc(200*time.Millisecond, cancel)
	r = NewCommand(kind.Kind{Command: "sleep 5"}).Run(cctx, j)
	if r.OK || !strings.Contains(r.Output, "cancelled") {
		t.Fatalf("cancelled run: got %+v", r)
	}
}

func TestCommandInstanceKillsLeftoverChildren(t *testing.T) {
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "pid")
	start := time.Now()
	r := NewCommand(kind.Kind{Command: `sleep 20 & echo $! > ` + pidfile + `; echo started`}).Run(context.Background(), Job{Dir: dir})
	if !r.OK || !strings.Contains(r.Output, "started") || strings.Contains(r.Output, "[") {
		t.Fatalf("got %+v, want OK with started and no note", r)
	}
	if d := time.Since(start); d > 2500*time.Millisecond {
		t.Fatalf("run took %s", d)
	}
	waitGone(t, pidfile)
}

// waitGone fails unless the process whose pid is in pidfile exits within 5s.
func waitGone(t *testing.T, pidfile string) {
	t.Helper()
	b, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("child %d survived", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
