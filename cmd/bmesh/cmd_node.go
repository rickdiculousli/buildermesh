package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

const nodeUsage = "usage: bmesh node forget <path> [--worktree]"

func (a *app) cmdNode(args []string) error {
	if len(args) == 0 || args[0] != "forget" {
		return fail(exitUsage, nodeUsage)
	}
	return a.cmdNodeForget(args[1:])
}

// cmdNodeForget deletes a done or blocked node, or an idle one that never
// reported and whose thread isn't running: its registration, state, runner
// sample and inbox consumer. With --worktree it first removes the
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
	switch s := stateOf(path); s {
	case "done", "blocked":
	case "idle":
		if err := a.idleForgettable(ctx, n); err != nil {
			return err
		}
	default:
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

// idleForgettable refuses (exit 3) an idle node that published an event of
// its own, whose thread isn't this box's runner's, or whose runner, asked
// once, can't say the thread is known and not running: a lead between its
// runner start and its first started is idle with no events too.
func (a *app) idleForgettable(ctx context.Context, n mesh.Node) error {
	hist, err := mesh.History(ctx, a.client, "bm."+n.Path+".*", time.Time{})
	if err != nil {
		return err
	}
	for _, m := range hist {
		var ev mesh.Event
		if json.Unmarshal(m.Data, &ev) == nil && ev.From == n.Path {
			return fail(exitRefused, "refused: %s is idle but published %s; only an idle node that never reported is forgotten", n.Path, ev.Event)
		}
	}
	if n.Host == "" {
		return fail(exitRefused, "refused: %s is idle and names no host", n.Path)
	}
	if n.Host != a.cfg.HostName() {
		return fail(exitRefused, "refused: %s is idle and its thread is on %s; run it on %s", n.Path, n.Host, n.Host)
	}
	if n.Runner.Kind != a.cfg.Runner.Kind {
		return fail(exitRefused, "refused: %s is idle and its runner is %q, not this box's %s; run it on %s", n.Path, n.Runner.Kind, a.cfg.Runner.Kind, n.Host)
	}
	mayRun := fail(exitRefused, "refused: %s is idle and its thread may be running; stop it first", n.Path)
	if n.Runner.Thread == "" {
		return mayRun
	}
	r, err := runner.New(a.cfg)
	if err != nil {
		return err
	}
	st, err := r.Status(ctx, n.Runner.Thread)
	switch {
	case err != nil:
		return fail(exitRefused, "refused: %s is idle and its thread may be running (%v); stop it first", n.Path, err)
	case !st.Known:
		return mayRun
	case st.Running:
		return fail(exitRefused, "refused: %s is idle and its thread %s is running; stop it first", n.Path, n.Runner.Thread)
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
	if n.Host == "" {
		return fail(exitRefused, "refused: %s is idle and names no host", n.Path)
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
