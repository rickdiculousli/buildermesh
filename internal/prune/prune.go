// Package prune removes the worktrees and branches a finished node leaves
// behind on this box: one node's (`node forget --worktree`, the
// forget-worktree op) or a whole checkout's (the prune op).
package prune

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// Refusal is a removal refused with nothing touched; its text starts
// "refused: ".
type Refusal struct{ Text string }

func (r *Refusal) Error() string { return r.Text }

func refuse(format string, a ...any) error {
	return &Refusal{Text: "refused: " + fmt.Sprintf(format, a...)}
}

// git runs git in dir and returns its trimmed output; a failure names the
// subcommand and carries git's stderr.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
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

func dirExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func hasBranch(ctx context.Context, repo, branch string) bool {
	_, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// ancestor: a is an ancestor of (or is) b.
func ancestor(ctx context.Context, repo, a, b string) bool {
	_, err := git(ctx, repo, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// landedIn reports whether rev's work is in one of into: it is an
// ancestor, or each of its commits has an equivalent there (git cherry),
// as after a rebase replayed them under new SHAs.
func landedIn(ctx context.Context, repo, rev string, into []string) (bool, error) {
	for _, target := range into {
		if ancestor(ctx, repo, rev, target) {
			return true, nil
		}
		cherry, err := git(ctx, repo, "cherry", target, rev)
		if err != nil {
			return false, err
		}
		if !strings.Contains("\n"+cherry, "\n+") {
			return true, nil
		}
	}
	return false, nil
}

// RemoveNodeWorktree removes n's worktree and the branch checked out there
// (else the node's) on this box, self, and returns what it did, a line
// each. It is refused (a *Refusal, nothing touched) when they are on
// another host, the worktree is dirty or the branch has not landed in
// main (an executor's: or in its parent's branch). A worktree already gone
// NodeWorktreeLeft reports whether n's worktree is still on disk here and
// its branch (the one checked out there, else the node's) still in its
// repo, as RemoveNodeWorktree finds them.
func NodeWorktreeLeft(ctx context.Context, n mesh.Node) (worktree, branch bool) {
	if n.Worktree == "" {
		return false, false
	}
	repo, b := n.RepoPath, n.Branch
	worktree = dirExists(n.Worktree)
	if worktree {
		if common, err := git(ctx, n.Worktree, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil && repo == "" {
			repo = filepath.Dir(common)
		}
		if cur, err := git(ctx, n.Worktree, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
			b = cur
		}
	}
	return worktree, repo != "" && b != "" && hasBranch(ctx, repo, b)
}

// from disk is pruned.
func RemoveNodeWorktree(ctx context.Context, n mesh.Node, nodes []mesh.Node, self string) ([]string, error) {
	if n.Worktree == "" {
		return []string{n.Path + " has no worktree"}, nil
	}
	if n.Host == "" {
		return nil, refuse("%s is idle and names no host", n.Path)
	}
	if n.Host != self {
		return nil, refuse("worktree %s is on %s; run it there", n.Worktree, n.Host)
	}
	repo := n.RepoPath
	present := dirExists(n.Worktree)
	if repo == "" && present {
		common, err := git(ctx, n.Worktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil {
			return nil, err
		}
		repo = filepath.Dir(common)
	}
	if repo == "" {
		return nil, refuse("%s names no repo and its worktree %s is gone", n.Path, n.Worktree)
	}
	if present {
		dirty, err := git(ctx, n.Worktree, "status", "--porcelain")
		if err != nil {
			return nil, err
		}
		if dirty != "" {
			return nil, refuse("worktree %s is dirty", n.Worktree)
		}
	}
	branch := n.Branch
	if present { // the branch checked out there: a runner may have renamed it
		if b, err := git(ctx, n.Worktree, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
			branch = b
		}
	}
	if branch != "" && !hasBranch(ctx, repo, branch) {
		branch = "" // already deleted
	}
	if branch != "" {
		into := []string{"main"}
		for _, p := range nodes {
			if p.Path == n.Parent && p.Branch != "" && n.Role == "executor" && hasBranch(ctx, repo, p.Branch) {
				into = append([]string{p.Branch}, into...)
			}
		}
		ok, err := landedIn(ctx, repo, "refs/heads/"+branch, into)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, refuse("branch %s is not merged into %s", branch, strings.Join(into, " or "))
		}
	}
	if present {
		if _, err := git(ctx, repo, "worktree", "remove", n.Worktree); err != nil {
			return nil, err
		}
	} else if _, err := git(ctx, repo, "worktree", "prune"); err != nil {
		return nil, err
	}
	lines := []string{"removed worktree " + n.Worktree}
	if branch != "" {
		// -D: merged into main is checked above; -d would also want the
		// branch's upstream, which a rebase before landing leaves behind.
		if _, err := git(ctx, repo, "branch", "-D", branch); err != nil {
			return lines, err
		}
		lines = append(lines, "deleted branch "+branch)
	}
	return lines, nil
}
