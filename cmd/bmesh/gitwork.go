package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Git steps shared by propose, spec review and the landing of a bmesh
// worktree on main: the same moves the briefs ask a thread to make, so a
// checkout someone else is working in is never switched, reset or stashed.

// hasOrigin reports whether the repo has an origin remote.
func hasOrigin(ctx context.Context, repoPath string) bool {
	_, err := gitIn(ctx, repoPath, "remote", "get-url", "origin")
	return err == nil
}

// onMain reports whether the main checkout has main checked out.
func onMain(ctx context.Context, repoPath string) bool {
	b, err := gitIn(ctx, repoPath, "symbolic-ref", "--short", "HEAD")
	return err == nil && b == "main"
}

// levelMain brings the repo's main up to date with origin: `pull --ff-only`
// when the main checkout is on main, else `fetch origin main:main`. A
// refusal (local main has commits origin does not, a dirty checkout the
// pull would touch) is an error naming it; without an origin nothing is
// done.
func levelMain(ctx context.Context, repoPath string) error {
	if !hasOrigin(ctx, repoPath) {
		return nil
	}
	if onMain(ctx, repoPath) {
		if _, err := gitIn(ctx, repoPath, "pull", "--quiet", "--ff-only"); err != nil {
			return fmt.Errorf("main in %s is not level with origin: %v", repoPath, err)
		}
		return nil
	}
	if _, err := gitIn(ctx, repoPath, "fetch", "--quiet", "origin", "main:main"); err != nil {
		return fmt.Errorf("main in %s is not level with origin: %v", repoPath, err)
	}
	return nil
}

// addWorktree makes wt on a new branch from base; a branch that already
// exists is refused (exit 3), and a failed add leaves nothing behind.
func addWorktree(ctx context.Context, repoPath, wt, branch, base string) error {
	if _, err := gitIn(ctx, repoPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return fail(exitRefused, "refused: branch %s already exists in %s", branch, repoPath)
	}
	if err := os.MkdirAll(filepath.Dir(wt), 0o755); err != nil {
		return err
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "add", "-b", branch, wt, base).CombinedOutput(); err != nil {
		_, _ = gitIn(ctx, repoPath, "worktree", "remove", "--force", wt)
		_, _ = gitIn(ctx, repoPath, "worktree", "prune")
		_, _ = gitIn(ctx, repoPath, "branch", "-D", branch)
		return fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// landOnMain fast-forwards main to branch: `merge --ff-only` in the main
// checkout when it is on main, else `fetch . <branch>:main`, which moves
// main without touching that checkout.
func landOnMain(ctx context.Context, repoPath, branch string) error {
	if onMain(ctx, repoPath) {
		if _, err := gitIn(ctx, repoPath, "merge", "--quiet", "--ff-only", branch); err != nil {
			return fmt.Errorf("main in %s did not fast-forward: %v", repoPath, err)
		}
		return nil
	}
	if _, err := gitIn(ctx, repoPath, "fetch", "--quiet", ".", branch+":main"); err != nil {
		return fmt.Errorf("main in %s did not fast-forward: %v", repoPath, err)
	}
	return nil
}

// pushMain pushes main to origin when the repo has one.
func pushMain(ctx context.Context, repoPath string) error {
	if !hasOrigin(ctx, repoPath) {
		return nil
	}
	if _, err := gitIn(ctx, repoPath, "push", "--quiet", "origin", "main"); err != nil {
		return fmt.Errorf("push main: %v", err)
	}
	return nil
}

// shortHex is eight random hex characters, for a one-off branch name.
func shortHex() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!*?[](){}<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
