package setup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/kind"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo is a repo with a root and a nested lockfile, node_modules ignored,
// and a log file outside it each setup run appends to.
func repo(t *testing.T) (dir, log string) {
	t.Helper()
	dir = t.TempDir()
	log = filepath.Join(t.TempDir(), "setup.log")
	gitT(t, dir, "init", "-q", "-b", "main")
	for rel, text := range map[string]string{"lock.json": "1\n", "web/lock.json": "a\n", ".gitignore": "node_modules/\n", "x.go": "package x\n"} {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-qm", "init")
	return dir, log
}

func runs(t *testing.T, log string) int {
	t.Helper()
	b, err := os.ReadFile(log)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "\n")
}

func TestEnsureRunsWhenInputsChange(t *testing.T) {
	dir, log := repo(t)
	ctx := context.Background()
	s := kind.Setup{Command: "mkdir -p node_modules && echo ran >> " + log, Inputs: []string{":(glob)**/lock.json"}, Timeout: time.Minute}
	if cur, err := Current(ctx, dir, s); err != nil || cur {
		t.Fatalf("current before any run: %v %v", cur, err)
	}
	ran, _, err := Ensure(ctx, dir, s, Options{Clean: true})
	if err != nil || !ran || runs(t, log) != 1 {
		t.Fatalf("first: ran %v err %v runs %d", ran, err, runs(t, log))
	}
	m, err := Marker(ctx, dir)
	if err != nil || !strings.HasPrefix(m, filepath.Join(dir, ".git")) {
		t.Fatalf("marker %s %v: want it in the git dir", m, err)
	}
	if ran, _, err := Ensure(ctx, dir, s, Options{}); err != nil || ran {
		t.Fatalf("unchanged: ran %v err %v", ran, err)
	}
	// An untracked file is not an input; a non-input tracked file is not either.
	os.WriteFile(filepath.Join(dir, "x.go"), []byte("package y\n"), 0o644)
	if cur, _ := Current(ctx, dir, s); !cur {
		t.Fatal("a file outside the inputs made it stale")
	}
	os.WriteFile(filepath.Join(dir, "web/lock.json"), []byte("b\n"), 0o644)
	if cur, _ := Current(ctx, dir, s); cur {
		t.Fatal("a nested input changed, still current")
	}
	if ran, _, err := Ensure(ctx, dir, s, Options{}); err != nil || !ran || runs(t, log) != 2 {
		t.Fatalf("after an input changed: ran %v err %v runs %d", ran, err, runs(t, log))
	}
	// The command is part of the hash.
	s.Command += " # v2"
	if cur, _ := Current(ctx, dir, s); cur {
		t.Fatal("a new command, still current")
	}
}

func TestEnsureNoInputsRunsOnceUnlessAlways(t *testing.T) {
	dir, log := repo(t)
	ctx := context.Background()
	s := kind.Setup{Command: "echo ran >> " + log, Timeout: time.Minute}
	for range 2 {
		if _, _, err := Ensure(ctx, dir, s, Options{}); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "lock.json"), []byte("2\n"), 0o644)
	if _, _, err := Ensure(ctx, dir, s, Options{}); err != nil || runs(t, log) != 1 {
		t.Fatalf("no inputs: runs %d (%v), want once", runs(t, log), err)
	}
	if ran, _, err := Ensure(ctx, dir, s, Options{Always: true}); err != nil || !ran || runs(t, log) != 2 {
		t.Fatalf("always: ran %v runs %d (%v)", ran, runs(t, log), err)
	}
}

func TestEnsureFailureLeavesNoMarker(t *testing.T) {
	dir, _ := repo(t)
	ctx := context.Background()
	if _, _, err := Ensure(ctx, dir, kind.Setup{Command: "true", Timeout: time.Minute}, Options{}); err != nil {
		t.Fatal(err)
	}
	s := kind.Setup{Command: "echo boom; exit 4", Timeout: time.Minute}
	_, out, err := Ensure(ctx, dir, s, Options{Always: true})
	var se *Error
	if !errors.As(err, &se) || !strings.HasPrefix(se.Msg, "setup failed") || !strings.Contains(out, "boom") {
		t.Fatalf("err %v out %q", err, out)
	}
	if m, _ := Marker(ctx, dir); fileExists(m) {
		t.Fatal("a failed run left the marker")
	}
	_, _, err = Ensure(ctx, dir, kind.Setup{Command: "sleep 5", Timeout: 200 * time.Millisecond}, Options{})
	if !errors.As(err, &se) || !strings.Contains(se.Msg, "timed out after 200ms") {
		t.Fatalf("timeout: %v", err)
	}
}

func TestEnsureCleanRefusesADirtyTree(t *testing.T) {
	dir, _ := repo(t)
	ctx := context.Background()
	s := kind.Setup{Command: "mkdir -p node_modules && touch node_modules/ok stray.txt", Timeout: time.Minute}
	_, _, err := Ensure(ctx, dir, s, Options{Clean: true})
	var se *Error
	if !errors.As(err, &se) || se.Msg != "setup dirtied the tree: stray.txt" {
		t.Fatalf("err %v", err)
	}
	if cur, _ := Current(ctx, dir, s); cur {
		t.Fatal("a dirty run was marked current")
	}
	// Without Clean (an agent's own worktree), the same run is fine.
	if _, _, err := Ensure(ctx, dir, s, Options{}); err != nil {
		t.Fatal(err)
	}
}

// A linked worktree keeps its own marker.
func TestMarkerIsPerWorktree(t *testing.T) {
	dir, log := repo(t)
	ctx := context.Background()
	wt := filepath.Join(t.TempDir(), "wt")
	gitT(t, dir, "worktree", "add", "-q", "--detach", wt, "HEAD")
	s := kind.Setup{Command: "echo ran >> " + log, Timeout: time.Minute}
	if _, _, err := Ensure(ctx, dir, s, Options{}); err != nil {
		t.Fatal(err)
	}
	if cur, _ := Current(ctx, wt, s); cur {
		t.Fatal("the linked worktree read the main checkout's marker")
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
