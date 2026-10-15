// Package checkout makes and moves the clean detached worktrees that routine
// instances run in. They live under the daemon's checkout root, never in a
// requester's worktree.
package checkout

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const gitTimeout = 60 * time.Second

var instanceRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)

// git runs one git command in repo with a timeout and returns trimmed stdout.
func git(repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, s)
	}
	return s, nil
}

// Ensure returns the worktree <root>/<repo name>/<instance>, creating it
// detached at the repo's HEAD when it is missing.
func Ensure(repoPath, root, instance string) (string, error) {
	if !instanceRE.MatchString(instance) {
		return "", fmt.Errorf("checkout instance %q: want lowercase letters, digits, dots and hyphens", instance)
	}
	if root == "" {
		return "", fmt.Errorf("checkout root is empty")
	}
	name := filepath.Base(repoPath)
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return "", fmt.Errorf("checkout repo %q has no usable name", repoPath)
	}
	dir := filepath.Join(root, name, instance)
	if _, err := git(repoPath, "worktree", "prune"); err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil && belongsTo(repoPath, dir) {
		return dir, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if _, err := git(repoPath, "worktree", "prune"); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	if _, err := git(repoPath, "worktree", "add", "--detach", dir, "HEAD"); err != nil {
		return "", err
	}
	return dir, nil
}

// belongsTo reports whether dir is a working worktree of repoPath, so a stale
// one (repo deleted or recreated, or a same-named repo) is replaced.
func belongsTo(repoPath, dir string) bool {
	a, ok := CommonDir(repoPath)
	b, ok2 := CommonDir(dir)
	return ok && ok2 && a == b
}

// CommonDir is the git common dir of the checkout holding dir, absolute with
// symlinks resolved: the same for a repo and each of its linked worktrees.
func CommonDir(dir string) (string, bool) {
	out, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", false
	}
	p, err := filepath.EvalSymlinks(out)
	return p, err == nil
}

// MoveTo leaves dir detached at commit with no tracked changes or untracked
// files from earlier runs.
func MoveTo(dir, commit string) error {
	sha, err := git(dir, "rev-parse", "--verify", "--end-of-options", commit+"^{commit}")
	if err != nil {
		return fmt.Errorf("commit %q: not a commit in %s", commit, dir)
	}
	for _, args := range [][]string{
		{"reset", "--hard", "--quiet"},
		{"checkout", "--detach", "--quiet", sha},
		{"clean", "-fdq"},
	} {
		if _, err := git(dir, args...); err != nil {
			return err
		}
	}
	return nil
}

// Remove deletes the worktree dir and forgets it in repoPath.
func Remove(repoPath, dir string) error {
	if _, err := git(repoPath, "worktree", "remove", "--force", dir); err != nil {
		return err
	}
	_, err := git(repoPath, "worktree", "prune")
	return err
}

// Fetch updates origin's refs when repoPath has an origin remote, else does
// nothing.
func Fetch(repoPath string) error {
	if _, err := git(repoPath, "remote", "get-url", "origin"); err != nil {
		return nil
	}
	_, err := git(repoPath, "fetch", "--quiet", "origin")
	return err
}
