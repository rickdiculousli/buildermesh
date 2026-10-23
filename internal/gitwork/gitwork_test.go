package gitwork

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// repos is an origin with a v1 tag, a checkout of it on main, and peer, a
// clone that lands commits on origin.
type repos struct{ origin, box, peer string }

func newRepos(t *testing.T) repos {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	root := t.TempDir()
	r := repos{origin: filepath.Join(root, "origin.git"), box: filepath.Join(root, "box"), peer: filepath.Join(root, "peer")}
	meshtest.Git(t, root, "init", "-q", "--bare", "-b", "main", r.origin)
	meshtest.Git(t, root, "init", "-q", "-b", "main", r.peer)
	meshtest.CommitFiles(t, r.peer, map[string]string{"a.txt": "one\n"})
	meshtest.Git(t, r.peer, "remote", "add", "origin", r.origin)
	meshtest.Git(t, r.peer, "tag", "v1")
	meshtest.Git(t, r.peer, "push", "-q", "origin", "main", "v1")
	meshtest.Git(t, root, "clone", "-q", r.origin, r.box)
	return r
}

// land commits on peer and pushes it: origin's main moves ahead of box.
func (r repos) land(t *testing.T) string {
	t.Helper()
	sha := meshtest.CommitFiles(t, r.peer, map[string]string{"b.txt": "two\n"})
	meshtest.Git(t, r.peer, "push", "-q", "origin", "HEAD:main")
	return sha
}

// racingGit puts a git on PATH that runs the self-update's fetch (main and
// the release tags, each written to FETCH_HEAD as for-merge) in the same
// checkout right after a pull's own fetch and right before a merge: a real
// pull runs it as its git, through GIT_EXEC_PATH, so the race lands inside
// the pull.
func racingGit(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the racing git is a shell script")
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	execPath, err := exec.Command(real, "--exec-path").Output()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := `#!/bin/sh
real='` + real + `'
bin='` + bin + `'
GIT_EXEC_PATH='` + strings.TrimSpace(string(execPath)) + `'
export GIT_EXEC_PATH
dir=.
if [ "$1" = -C ]; then dir=$2; fi
sub=$1
if [ "$1" = -C ]; then sub=$3; fi
race() { "$real" -C "$dir" fetch --quiet origin '+refs/heads/main:refs/remotes/origin/main' '+refs/tags/v*:refs/tags/v*' || exit 1; }
case "$sub" in
pull) GIT_EXEC_PATH=$bin exec "$real" "$@" ;;
merge) race; exec "$real" "$@" ;;
fetch)
	case " $* " in
	*" --update-head-ok "*) "$real" "$@" || exit; race ;;
	*) exec "$real" "$@" ;;
	esac ;;
*) exec "$real" "$@" ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A fetch of main and the tags landing in the checkout between levelMain's
// fetch and its merge leaves a FETCH_HEAD a pull can't fast-forward to
// ("Cannot fast-forward to multiple branches"); the level still reaches
// origin's main.
func TestLevelMainIgnoresAnotherFetchsFetchHead(t *testing.T) {
	r := newRepos(t)
	top := r.land(t)
	racingGit(t)
	if err := LevelMain(context.Background(), r.box); err != nil {
		t.Fatalf("level: %v", err)
	}
	if got := meshtest.Git(t, r.box, "rev-parse", "HEAD"); got != top {
		t.Fatalf("main at %s, want origin's %s", got, top)
	}
	// The race happened: what it left is what a pull would have merged.
	fh, err := os.ReadFile(filepath.Join(r.box, ".git", "FETCH_HEAD"))
	if err != nil || strings.Count(string(fh), "\t\t") < 2 {
		t.Fatalf("FETCH_HEAD lists %q (%v), want main and v1 for merge", fh, err)
	}
}

// LevelMain's refusals are what they were: local commits origin lacks.
func TestLevelMainRefusesLocalCommits(t *testing.T) {
	r := newRepos(t)
	r.land(t)
	meshtest.CommitFiles(t, r.box, map[string]string{"c.txt": "mine\n"})
	err := LevelMain(context.Background(), r.box)
	if err == nil || !strings.Contains(err.Error(), "main in "+r.box+" is not level with origin: git merge:") {
		t.Fatalf("level: %v, want a refusal from the merge", err)
	}
}

// Levelling main, on main or off it, leaves no FETCH_HEAD behind for a
// person's own pull to read.
func TestLevelMainWritesNoFetchHead(t *testing.T) {
	r := newRepos(t)
	top := r.land(t)
	if err := LevelMain(context.Background(), r.box); err != nil {
		t.Fatalf("level on main: %v", err)
	}
	r.land(t)
	meshtest.Git(t, r.box, "switch", "-q", "-c", "side")
	if err := LevelMain(context.Background(), r.box); err != nil {
		t.Fatalf("level off main: %v", err)
	}
	if got := meshtest.Git(t, r.box, "rev-parse", "main"); got == top {
		t.Fatal("main did not move off main")
	}
	if _, err := os.Stat(filepath.Join(r.box, ".git", "FETCH_HEAD")); !os.IsNotExist(err) {
		t.Fatalf("FETCH_HEAD after levelling: %v, want none", err)
	}
}
