package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupCommand(t *testing.T) {
	setup(t)
	setups := setupLog(t)
	repo := gitRepo(t)
	if out, errs, code := runCLI(t, nil, "setup", "--check"); code != 0 || out != "setup: none (.bmesh/config.toml names no [setup])\n" {
		t.Fatalf("none: %d %q %q", code, out, errs)
	}
	commitAll(t, repo, map[string]string{".gitignore": "node_modules/\n", "lock.json": "1\n",
		".bmesh/config.toml": "[setup]\ncommand = \"" + installSetup + "\"\ninputs = [\"lock.json\"]\n"})
	cmd := strings.ReplaceAll(installSetup, `\"`, `"`)
	if out, _, code := runCLI(t, nil, "setup", "--check"); code != 1 || out != "setup: missing ("+cmd+"); run bmesh setup\n" || setups() != 0 {
		t.Fatalf("missing: %d %q", code, out)
	}
	if out, errs, code := runCLI(t, nil, "setup"); code != 0 || !strings.HasPrefix(out, "setup: ran ("+cmd+") in ") || setups() != 1 {
		t.Fatalf("run: %d %q %q", code, out, errs)
	}
	if out, _, code := runCLI(t, nil, "setup", "--check"); code != 0 || out != "setup: current ("+cmd+")\n" {
		t.Fatalf("current: %d %q", code, out)
	}
	if out, _, code := runCLI(t, nil, "setup"); code != 0 || out != "setup: current ("+cmd+")\n" || setups() != 1 {
		t.Fatalf("again: %d %q", code, out)
	}
	// An uncommitted input change makes it stale; the agent's own edits
	// don't stop a run.
	if err := os.WriteFile(filepath.Join(repo, "lock.json"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _, code := runCLI(t, nil, "setup", "--check"); code != 1 || !strings.HasPrefix(out, "setup: stale (") || setups() != 1 {
		t.Fatalf("stale: %d %q", code, out)
	}
	if _, errs, code := runCLI(t, nil, "setup"); code != 0 || setups() != 2 {
		t.Fatalf("stale run: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "setup", "--force"); code != 0 || setups() != 3 {
		t.Fatalf("--force: %d %q", code, errs)
	}
	commitAll(t, repo, map[string]string{"lock.json": "2\n", ".bmesh/config.toml": "[setup]\ncommand = \"echo down; exit 2\"\n"})
	if out, errs, code := runCLI(t, nil, "setup"); code != 1 || out != "down\n" || errs != "bmesh: setup failed: exit status 2\n" {
		t.Fatalf("failing: %d %q %q", code, out, errs)
	}
	if _, _, code := runCLI(t, nil, "setup", "--check"); code != 1 {
		t.Fatalf("a failed run left it current: %d", code)
	}
}
