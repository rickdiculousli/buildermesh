package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/prune"
)

// pruneJob runs a received prune (023): j.Repo is a registry repo, or ""
// for every one with a checkout here; j.Args holds "apply" to remove. It
// answers this box's rows, one a line, and a summary line.
func (h *hostsNode) pruneJob(ctx context.Context, j *hosts.Job) (bool, string) {
	apply := false
	for _, a := range j.Args {
		if a != "apply" {
			return false, "refused: unknown argument " + cut(a, 32)
		}
		apply = true
	}
	repos := h.d.Cfg.RepoNames()
	if j.Repo != "" {
		if _, ok := h.checkoutOf(j.Repo); !ok {
			return true, fmt.Sprintf("no checkout of %s here", cut(j.Repo, 64))
		}
		repos = []string{j.Repo}
	}
	nodes, err := mesh.ListNodes(ctx, h.d.Client)
	if err != nil {
		return false, "nodes: " + err.Error()
	}
	states, err := mesh.ReadAllStates(ctx, h.d.Client)
	if err != nil {
		return false, "states: " + err.Error()
	}
	var lines []string
	var all []prune.Row
	for _, repo := range repos {
		path, ok := h.checkoutOf(repo)
		if !ok {
			continue
		}
		rows, err := prune.Repo(ctx, prune.Checkout{Repo: repo, Path: path, Self: h.d.Host, Nodes: nodes, States: states,
			Worktrees: h.d.Cfg.Expand(h.d.Cfg.Runner.Worktrees), Checkouts: h.d.Checkouts, StateDir: h.d.StateDir, Apply: apply})
		for _, r := range rows {
			lines = append(lines, r.String())
		}
		if err != nil {
			lines = append(lines, repo+" error "+err.Error())
		}
		all = append(all, rows...)
	}
	lines = append(lines, prune.Summary(all, apply))
	h.d.Log.Info("hosts: prune", "repo", j.Repo, "by", j.By, "apply", apply, "result", lines[len(lines)-1])
	return true, strings.Join(lines, "\n")
}

// forgetWorktreeJob runs a received forget-worktree (023): j.Args is the
// node's path. A done or blocked node of this box's has its worktree and
// merged branch removed (prune.RemoveNodeWorktree), then is forgotten.
func (h *hostsNode) forgetWorktreeJob(ctx context.Context, j *hosts.Job) (bool, string) {
	if len(j.Args) != 1 {
		return false, "refused: forget-worktree takes a node path"
	}
	p, err := mesh.ParsePath(j.Args[0])
	if err != nil {
		return false, "refused: " + cut(err.Error(), 128)
	}
	path := p.String()
	n, ok, err := mesh.GetNode(ctx, h.d.Client, path)
	if err != nil {
		return false, err.Error()
	}
	if !ok {
		return false, "refused: no node " + path
	}
	if n.Host != h.d.Host {
		return false, fmt.Sprintf("refused: %s is on %s, not %s", path, n.Host, h.d.Host)
	}
	st, _, err := mesh.ReadState(ctx, h.d.Client, path)
	if err != nil {
		return false, err.Error()
	}
	state := st.State
	if state == "" {
		state = "idle"
	}
	if state != "done" && state != "blocked" {
		return false, fmt.Sprintf("refused: %s is %s; only a done or blocked node is forgotten", path, state)
	}
	nodes, err := mesh.ListNodes(ctx, h.d.Client)
	if err != nil {
		return false, err.Error()
	}
	lines, err := prune.RemoveNodeWorktree(ctx, n, nodes, h.d.Host)
	if err != nil {
		var ref *prune.Refusal
		if errors.As(err, &ref) {
			return false, ref.Text
		}
		return false, strings.Join(append(lines, err.Error()), "; ")
	}
	if err := mesh.ForgetNode(ctx, h.d.Client, path); err != nil {
		return false, strings.Join(append(lines, err.Error()), "; ")
	}
	h.d.Log.Info("hosts: forget-worktree", "node", path, "by", j.By)
	return true, strings.Join(append(lines, "forgot "+path), "; ")
}
