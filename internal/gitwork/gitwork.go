// Package gitwork is the git steps shared by ideate, build, the ledger
// edits and the daemon's rescaffold: the same moves the briefs ask a
// thread to make, so a checkout someone else is working in is never
// switched, reset or stashed.
package gitwork

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/gitfetch"
	"github.com/rickdiculousli/buildermesh/internal/level"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// Refused is a step that refused with nothing touched: a branch or path
// that already exists, a main that isn't level or won't fast-forward. The
// CLI exits 3 on it.
type Refused struct{ Msg string }

func (e *Refused) Error() string { return e.Msg }

func refused(format string, a ...any) error {
	return &Refused{Msg: fmt.Sprintf(format, a...)}
}

// git runs git in dir and returns its trimmed output; a failure names the
// subcommand and carries git's stderr.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

// HasOrigin reports whether the repo has an origin remote.
func HasOrigin(ctx context.Context, repoPath string) bool {
	_, err := git(ctx, repoPath, "remote", "get-url", "origin")
	return err == nil
}

// OnMain reports whether the main checkout has main checked out.
func OnMain(ctx context.Context, repoPath string) bool {
	b, err := git(ctx, repoPath, "symbolic-ref", "--short", "HEAD")
	return err == nil && b == "main"
}

// LevelMain brings the repo's main up to date with origin: when the main
// checkout is on main, `fetch origin +main:refs/remotes/origin/main` then
// `merge --ff-only refs/remotes/origin/main`, a ref no other fetch rewrites,
// never FETCH_HEAD, which a fetch in the same checkout (the daemon's) can
// rewrite between a pull's fetch and its merge (089); else `fetch origin
// main:main`. A refusal (local main has commits origin does not, a dirty
// checkout the merge would touch) is an error naming it; without an origin
// nothing is done.
func LevelMain(ctx context.Context, repoPath string) error {
	if !HasOrigin(ctx, repoPath) {
		return nil
	}
	if OnMain(ctx, repoPath) {
		if _, err := git(ctx, repoPath, gitfetch.Args("--quiet", "origin", "+refs/heads/main:refs/remotes/origin/main")...); err != nil {
			return fmt.Errorf("main in %s is not level with origin: %v", repoPath, err)
		}
		if _, err := git(ctx, repoPath, "merge", "--quiet", "--ff-only", "refs/remotes/origin/main"); err != nil {
			return fmt.Errorf("main in %s is not level with origin: %v", repoPath, err)
		}
		return nil
	}
	if _, err := git(ctx, repoPath, gitfetch.Args("--quiet", "origin", "main:main")...); err != nil {
		return fmt.Errorf("main in %s is not level with origin: %v", repoPath, err)
	}
	return nil
}

// AddWorktree makes wt on a new branch from base; a branch or a path that
// already exists is Refused (nothing touched), and a failed add leaves
// nothing behind.
func AddWorktree(ctx context.Context, repoPath, wt, branch, base string) error {
	if _, err := git(ctx, repoPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return refused("refused: branch %s already exists in %s", branch, repoPath)
	}
	if _, err := os.Lstat(wt); err == nil {
		return refused("refused: %s already exists; remove it first", wt)
	}
	if err := os.MkdirAll(filepath.Dir(wt), 0o755); err != nil {
		return err
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "add", "-b", branch, wt, base).CombinedOutput(); err != nil {
		_, _ = git(ctx, repoPath, "worktree", "remove", "--force", wt)
		_, _ = git(ctx, repoPath, "worktree", "prune")
		_, _ = git(ctx, repoPath, "branch", "-D", branch)
		return fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoveWorktree drops a bmesh worktree, whatever its state, and its
// branch unless keepBranch (the branch is the caller's to judge: a landed
// one is merged into main, an abandoned one holds nothing).
func RemoveWorktree(ctx context.Context, repoPath, wt, branch string, keepBranch bool) error {
	if _, err := os.Stat(wt); err == nil {
		if _, err := git(ctx, repoPath, "worktree", "remove", "--force", wt); err != nil {
			return err
		}
	}
	_, _ = git(ctx, repoPath, "worktree", "prune")
	if keepBranch {
		return nil
	}
	_, err := git(ctx, repoPath, "branch", "-D", branch)
	return err
}

// Lock takes a repo's lock and returns its release; nil takes none.
type Lock func() (unlock func(), err error)

func (l Lock) take() (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	return l()
}

// RepoLock is the lock for the registered repo at repoPath, <stateDir>/
// locks/<repo>.lock (020), when cfg has [hosts] enabled; otherwise, or for
// a path the registry doesn't name, nil.
func RepoLock(cfg *config.Config, stateDir, repoPath string) Lock {
	if cfg == nil || !cfg.Hosts.Enabled {
		return nil
	}
	name, _, err := cfg.RepoPath(repoPath)
	if _, ok := cfg.Repos[name]; err != nil || !ok {
		return nil
	}
	return func() (func(), error) { return level.Lock(stateDir, name) }
}

// LandOnMain fast-forwards main to branch under lock, so a level of the
// same checkout never runs at once.
func LandOnMain(ctx context.Context, repoPath, branch string, lock Lock) error {
	unlock, err := lock.take()
	if err != nil {
		return err
	}
	defer unlock()
	return fastForward(ctx, repoPath, branch)
}

// fastForward moves main to branch: `merge --ff-only` in the main checkout
// when it is on main, else `fetch . <branch>:main`, which moves main
// without touching that checkout.
func fastForward(ctx context.Context, repoPath, branch string) error {
	if OnMain(ctx, repoPath) {
		if _, err := git(ctx, repoPath, "merge", "--quiet", "--ff-only", branch); err != nil {
			return fmt.Errorf("main in %s did not fast-forward: %v", repoPath, err)
		}
		return nil
	}
	if _, err := git(ctx, repoPath, gitfetch.Args("--quiet", ".", branch+":main")...); err != nil {
		return fmt.Errorf("main in %s did not fast-forward: %v", repoPath, err)
	}
	return nil
}

// ShortHex is eight random hex characters, for a one-off branch name.
func ShortHex() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Edit is one edit landed on a repo's main by EditOnMain.
type Edit struct {
	Repo      string // the registry name, whose token starts the branch
	RepoPath  string // the main checkout
	What      string // the branch's middle word and the refusal's noun: review, steps, scaffold
	Worktrees string // the worktree goes in <Worktrees>/<Repo>/<branch>
	Lock      Lock   // held from the level to the landing; nil takes none
	// Check runs on the committed worktree before it lands; nil checks
	// nothing.
	Check func(wt string) error
	// Discard drops a commit that did not land with its branch (one the
	// caller makes again on a retry); otherwise that branch is kept and
	// named in the refusal.
	Discard bool
	// Warn hears what didn't stop the landing: a worktree that would not
	// go, a main checkout that didn't follow a push. nil drops it.
	Warn func(error)
}

// EditOnMain levels the repo's main under e.Lock, makes a bmesh worktree
// from it on a one-off branch, `<repo token>-<what>-<hex>`, and runs edit
// there. When edit returns a commit message, it stages the paths edit
// returns, commits as bmesh, pushes the commit to origin's main and
// fast-forwards main, returning the commit; "" when edit had nothing to
// land. A main that isn't level, or a push origin refuses, is Refused with
// nothing landed, so a retry starts clean. Without an origin it only
// fast-forwards main. The worktree always goes; the branch goes with it
// unless it holds a commit that did not land, which is kept and named
// (unless e.Discard).
func EditOnMain(ctx context.Context, e Edit, edit func(wt string) (msg string, paths []string, err error)) (string, error) {
	warn := e.Warn
	if warn == nil {
		warn = func(error) {}
	}
	unlock, err := e.Lock.take()
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := LevelMain(ctx, e.RepoPath); err != nil {
		return "", refused("refused: %v", err)
	}
	tag, err := ShortHex()
	if err != nil {
		return "", err
	}
	branch := mesh.RepoToken(e.Repo) + "-" + e.What + "-" + tag
	wt := filepath.Join(e.Worktrees, e.Repo, branch)
	if err := AddWorktree(ctx, e.RepoPath, wt, branch, "main"); err != nil {
		return "", err
	}
	committed, landed := false, false
	defer func() {
		keep := committed && !landed && !e.Discard
		if err := RemoveWorktree(context.WithoutCancel(ctx), e.RepoPath, wt, branch, keep); err != nil {
			warn(err)
		}
	}()
	msg, paths, err := edit(wt)
	if err != nil || msg == "" {
		return "", err
	}
	if _, err := git(ctx, wt, append([]string{"add", "-A", "--"}, paths...)...); err != nil {
		return "", err
	}
	if _, err := git(ctx, wt, "-c", "user.name=bmesh", "-c", "user.email=bmesh@localhost", "commit", "-q", "-m", msg); err != nil {
		return "", err
	}
	committed = true
	sha, err := git(ctx, wt, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if e.Check != nil {
		if err := e.Check(wt); err != nil {
			return "", err
		}
	}
	kept := "; the " + e.What + " is committed on branch " + branch
	if e.Discard {
		kept = ""
	}
	if !HasOrigin(ctx, e.RepoPath) {
		if err := fastForward(ctx, e.RepoPath, branch); err != nil {
			return "", refused("refused: %v%s", err, kept)
		}
		landed = true
		return sha, nil
	}
	// origin first, as land does: a push refused (origin moved) leaves
	// local main where it was, so the next try levels cleanly.
	if _, err := git(context.WithoutCancel(ctx), wt, "push", "--quiet", "origin", "HEAD:refs/heads/main"); err != nil {
		return "", refused("refused: push main: %v%s", err, kept)
	}
	landed = true
	if err := fastForward(context.WithoutCancel(ctx), e.RepoPath, branch); err != nil {
		warn(fmt.Errorf("%v; origin/main has %s", err, sha[:8]))
	}
	return sha, nil
}
