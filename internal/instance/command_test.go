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
			t.Fatalf("grandchild %d survived", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}

	cctx, cancel := context.WithCancel(ctx)
	time.AfterFunc(200*time.Millisecond, cancel)
	r = NewCommand(kind.Kind{Command: "sleep 5"}).Run(cctx, j)
	if r.OK || !strings.Contains(r.Output, "cancelled") {
		t.Fatalf("cancelled run: got %+v", r)
	}
}
