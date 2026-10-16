//go:build !windows

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// startLeaseRun runs `lock-lease run solo --ttl 3s` on a shell that starts a
// background sleep and waits for it; it returns the sleep's pid once the
// lease is held, and a channel with the exit code and stderr.
func startLeaseRun(t *testing.T, c *mesh.Client, ctx context.Context) (int, <-chan [2]any) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	pidFile := filepath.Join(dir, "pid")
	res := make(chan [2]any, 1)
	go func() {
		var out, errb bytes.Buffer
		code := run([]string{"lock-lease", "run", "solo", "--ttl", "3s", "--", "sh", "-c", `sleep 20 & echo $! > "$0"; wait`, pidFile},
			strings.NewReader(""), &out, &errb)
		res <- [2]any{code, errb.String()}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, err := os.ReadFile(pidFile)
		if pid, perr := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && perr == nil {
			if leases, err := mesh.ListLeases(ctx, c); err == nil && len(leases) == 1 {
				return pid, res
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("lease run never started its command")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// assertExit waits for the run's result and checks the grandchild is gone.
func assertExit(t *testing.T, res <-chan [2]any, pid int, wantErr string) {
	t.Helper()
	select {
	case r := <-res:
		if r[0] != 1 || r[1] != wantErr {
			t.Fatalf("exit %v %q, want 1 %q", r[0], r[1], wantErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("command not killed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d survived", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLeaseRunKillsOnLostLease(t *testing.T) {
	c, ctx := setup(t)
	pid, res := startLeaseRun(t, c, ctx)
	kv, err := c.JS.KeyValue(ctx, mesh.BucketLeases)
	if err != nil {
		t.Fatal(err)
	}
	if err := kv.Delete(ctx, "solo.0"); err != nil {
		t.Fatal(err)
	}
	assertExit(t, res, pid, "bmesh: lease lost\n")
}

func TestLeaseRunReleasesOnSignal(t *testing.T) {
	c, ctx := setup(t)
	sigs := make(chan os.Signal, 1)
	orig := leaseSignals
	leaseSignals = func() (<-chan os.Signal, func()) { return sigs, func() {} }
	t.Cleanup(func() { leaseSignals = orig })
	pid, res := startLeaseRun(t, c, ctx)
	sigs <- syscall.SIGTERM
	assertExit(t, res, pid, "bmesh: interrupted\n")
	assertNoLeases(t, c, ctx)
	if _, errs, code := runCLI(t, nil, "lock-lease", "run", "solo", "--ttl", "2s", "--", "true"); code != 2 {
		t.Fatalf("short ttl: %d %q", code, errs)
	}
}
