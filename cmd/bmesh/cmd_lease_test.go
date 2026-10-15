package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
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

	out, errs, code := runCLI(t, nil, "lease", "run", "game", "--ttl", "1m", "--", "sh", "-c", "echo in; exit 7")
	if code != 7 || out != "in\n" || errs != "" {
		t.Fatalf("run: %d %q %q", code, out, errs)
	}
	assertNoLeases(t, c, ctx)

	// Capacity 2 from the repo config: two acquires, then busy.
	k0, _, code := runCLI(t, nil, "lease", "acquire", "game")
	k1, _, code1 := runCLI(t, as("app-001"), "lease", "acquire", "game")
	if code != 0 || code1 != 0 || k0 != "game.0\n" || k1 != "game.1\n" {
		t.Fatalf("acquire: %d %q %d %q", code, k0, code1, k1)
	}
	if _, errs, code := runCLI(t, nil, "lease", "acquire", "game"); code != 3 || errs != "bmesh: slot game is busy\n" {
		t.Fatalf("busy: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "lease", "renew", "game.1"); code != 3 {
		t.Fatalf("renew by non-holder: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, as("app-001"), "lease", "renew", "game.1"); code != 0 {
		t.Fatalf("renew: %d %q", code, errs)
	}
	for _, r := range []struct {
		env map[string]string
		key string
	}{{nil, "game.0"}, {as("app-001"), "game.1"}} {
		if _, errs, code := runCLI(t, r.env, "lease", "release", r.key); code != 0 {
			t.Fatalf("release %s: %d %q", r.key, code, errs)
		}
	}
	if _, errs, code := runCLI(t, nil, "lease", "release", "game.0"); code != 3 {
		t.Fatalf("release twice: %d %q", code, errs)
	}
	assertNoLeases(t, c, ctx)

	// Outside a repo the capacity is 1.
	t.Chdir(t.TempDir())
	if _, _, code := runCLI(t, nil, "lease", "acquire", "solo"); code != 0 {
		t.Fatal(code)
	}
	if _, errs, code := runCLI(t, nil, "lease", "acquire", "solo"); code != 3 || !strings.Contains(errs, "busy") {
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

func TestLeaseRunKillsOnLostLease(t *testing.T) {
	c, ctx := setup(t)
	t.Chdir(t.TempDir())
	res := make(chan [2]any, 1)
	go func() {
		var out, errb bytes.Buffer
		code := run([]string{"lease", "run", "solo", "--ttl", "600ms", "--", "sleep", "20"}, strings.NewReader(""), &out, &errb)
		res <- [2]any{code, errb.String()}
	}()
	kv, err := c.JS.KeyValue(ctx, mesh.BucketLeases)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := kv.Get(ctx, "solo.0"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lease never acquired")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := kv.Delete(ctx, "solo.0"); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-res:
		if r[0] != 1 || r[1] != "bmesh: lease lost\n" {
			t.Fatalf("lost: %v %q", r[0], r[1])
		}
	case <-time.After(10 * time.Second):
		t.Fatal("command not killed")
	}
}
