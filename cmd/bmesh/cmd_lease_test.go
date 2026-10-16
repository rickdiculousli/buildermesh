package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
