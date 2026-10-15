package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

const nodeUsage = "usage: bmesh node forget <path> [--worktree]"

func (a *app) cmdNode(args []string) error {
	if len(args) == 0 || args[0] != "forget" {
		return fail(exitUsage, nodeUsage)
	}
	return a.cmdNodeForget(args[1:])
}

// cmdNodeForget deletes a done or blocked node: its registration, state,
// runner sample and inbox consumer. With --worktree it first removes the
// node's clean worktree and merged branch on this host.
func (a *app) cmdNodeForget(args []string) error {
	fs := a.flags("node forget")
	withWorktree := fs.Bool("worktree", false, "also remove the node's clean worktree and merged branch on this host")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail(exitUsage, nodeUsage)
	}
	p, err := mesh.ParsePath(pos[0])
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	path := p.String()
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "node forget"); err != nil {
		return err
	}
	n, ok, err := mesh.GetNode(ctx, a.client, path)
	if err != nil {
		return err
	}
	if !ok {
		return fail(exitRefused, "refused: no node %s", path)
	}
	states, err := mesh.ReadAllStates(ctx, a.client)
	if err != nil {
		return err
	}
	stateOf := func(p string) string {
		if s, ok := states[p]; ok {
			return s.State
		}
		return "idle"
	}
	if s := stateOf(path); s != "done" && s != "blocked" {
		return fail(exitRefused, "refused: %s is %s; only a done or blocked node is forgotten", path, s)
	}
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return err
	}
	for _, c := range nodes {
		if s := stateOf(c.Path); strings.HasPrefix(c.Path, path+".") && s != "done" && s != "blocked" {
			return fail(exitRefused, "refused: %s under %s is %s", c.Path, path, s)
		}
	}

	var kept string
	if *withWorktree {
		if err := a.removeNodeWorktree(ctx, n, nodes); err != nil {
			return err
		}
	} else if n.Worktree != "" {
		kept = n.Worktree
	}
	if err := mesh.ForgetNode(ctx, a.client, path); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "forgot %s\n", path)
	if kept != "" {
		fmt.Fprintf(a.stdout, "worktree %s kept\n", kept)
	}
	return nil
}

// removeNodeWorktree removes n's worktree and the branch checked out there
// (else the node's): refused (exit 3, nothing touched) when they are on
// another host, the worktree is dirty or the branch has not landed in main
// (an executor's: or in its parent's branch). A worktree already gone from
// disk is pruned.
func (a *app) removeNodeWorktree(ctx context.Context, n mesh.Node, nodes []mesh.Node) error {
	if n.Worktree == "" {
		fmt.Fprintf(a.stdout, "%s has no worktree\n", n.Path)
		return nil
	}
	if n.Host != a.cfg.HostName() {
		return fail(exitRefused, "refused: worktree %s is on %s; run it there", n.Worktree, n.Host)
	}
	repo := n.RepoPath
	present := dirExists(n.Worktree)
	if repo == "" && present {
		common, err := gitIn(ctx, n.Worktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil {
			return err
		}
		repo = filepath.Dir(common)
	}
	if repo == "" {
		return fail(exitRefused, "refused: %s names no repo and its worktree %s is gone", n.Path, n.Worktree)
	}
	if present {
		dirty, err := gitIn(ctx, n.Worktree, "status", "--porcelain")
		if err != nil {
			return err
		}
		if dirty != "" {
			return fail(exitRefused, "refused: worktree %s is dirty", n.Worktree)
		}
	}
	branch := n.Branch
	if present { // the branch checked out there: a runner may have renamed it
		if b, err := gitIn(ctx, n.Worktree, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
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
		ok, err := landedIn(ctx, repo, branch, into)
		if err != nil {
			return err
		}
		if !ok {
			return fail(exitRefused, "refused: branch %s is not merged into %s", branch, strings.Join(into, " or "))
		}
	}
	if present {
		if _, err := gitIn(ctx, repo, "worktree", "remove", n.Worktree); err != nil {
			return err
		}
	} else if _, err := gitIn(ctx, repo, "worktree", "prune"); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "removed worktree %s\n", n.Worktree)
	if branch != "" {
		// -D: merged into main is checked above; -d would also want the
		// branch's upstream, which a rebase before landing leaves behind.
		if _, err := gitIn(ctx, repo, "branch", "-D", branch); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "deleted branch %s\n", branch)
	}
	return nil
}

func hasBranch(ctx context.Context, repo, branch string) bool {
	_, err := gitIn(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// landedIn reports whether branch's work is in one of into: its tip is an
// ancestor, or each of its commits has an equivalent there (git cherry), as
// after a rebase replayed them under new SHAs.
func landedIn(ctx context.Context, repo, branch string, into []string) (bool, error) {
	for _, target := range into {
		merged, err := gitIn(ctx, repo, "branch", "--list", "--merged", target, branch)
		if err != nil {
			return false, err
		}
		if merged != "" {
			return true, nil
		}
		cherry, err := gitIn(ctx, repo, "cherry", target, branch)
		if err != nil {
			return false, err
		}
		if !strings.Contains("\n"+cherry, "\n+") {
			return true, nil
		}
	}
	return false, nil
}
