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

func TestLeaseRunReleasesOnExit(t *testing.T) {
	c, ctx := setup(t)
	repo := gitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, ".bmesh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".bmesh", "config.toml"), []byte("[slots]\ngame = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, errs, code := runCLI(t, nil, "lock-lease", "run", "game", "--ttl", "1m", "--", "sh", "-c", "echo in; exit 7")
	if code != 7 || out != "in\n" || errs != "" {
		t.Fatalf("run: %d %q %q", code, out, errs)
	}
	assertNoLeases(t, c, ctx)

	// Capacity 2 from the repo config: two acquires, then busy.
	k0, _, code := runCLI(t, nil, "lock-lease", "acquire", "game")
	k1, _, code1 := runCLI(t, as("app-001"), "lock-lease", "acquire", "game")
	if code != 0 || code1 != 0 || k0 != "game.0\n" || k1 != "game.1\n" {
		t.Fatalf("acquire: %d %q %d %q", code, k0, code1, k1)
	}
	if _, errs, code := runCLI(t, nil, "lock-lease", "acquire", "game"); code != 3 || errs != "bmesh: slot game is busy\n" {
		t.Fatalf("busy: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "lock-lease", "renew", "game.1"); code != 3 {
		t.Fatalf("renew by non-holder: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, as("app-001"), "lock-lease", "renew", "game.1"); code != 0 {
		t.Fatalf("renew: %d %q", code, errs)
	}
	for _, r := range []struct {
		env map[string]string
		key string
	}{{nil, "game.0"}, {as("app-001"), "game.1"}} {
		if _, errs, code := runCLI(t, r.env, "lock-lease", "release", r.key); code != 0 {
			t.Fatalf("release %s: %d %q", r.key, code, errs)
		}
	}
	if _, errs, code := runCLI(t, nil, "lock-lease", "release", "game.0"); code != 3 {
		t.Fatalf("release twice: %d %q", code, errs)
	}
	assertNoLeases(t, c, ctx)

	// Outside a repo the capacity is 1.
	t.Chdir(t.TempDir())
	if _, _, code := runCLI(t, nil, "lock-lease", "acquire", "solo"); code != 0 {
		t.Fatal(code)
	}
	if _, errs, code := runCLI(t, nil, "lock-lease", "acquire", "solo"); code != 3 || !strings.Contains(errs, "busy") {
		t.Fatalf("solo busy: %d %q", code, errs)
	}
}

func assertNoLeases(t *testing.T, c *mesh.Client, ctx context.Context) {
	t.Helper()
	leases, err := mesh.ListLeases(ctx, c)
	if err != nil || len(leases) != 0 {
		t.Fatalf("leases left: %+v %v", leases, err)
	}
}

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
