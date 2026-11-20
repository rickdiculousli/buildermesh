package daemon

import (
	"context"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/prune"
)

// forgetEvery is how often the daemon forgets this box's long-done nodes.
const forgetEvery = time.Hour

// forgetDoneOnce forgets this host's nodes, routines aside (the rescan's
// sweep has those), that have been done for [status] forget_done, so every
// listing of the mesh stops reading them. A node is kept while its worktree
// is on disk, which prune judges by its node, and while another node names
// it as parent, which prune reads to see a child merged; it goes on a later
// pass. It is also kept while its own parent is on the mesh and not done (a
// round settles on its children), and while its T3 thread waits to be
// settled with its unit (settleOnce). Its events stay in the stream.
func forgetDoneOnce(ctx context.Context, d Deps) {
	keep := d.Cfg.Status.ForgetDone
	if keep <= 0 {
		return
	}
	nodes, err := mesh.ListNodes(ctx, d.Client)
	if err != nil {
		d.Log.Warn("forget: list nodes", "err", err)
		return
	}
	states, err := mesh.ReadAllStates(ctx, d.Client)
	if err != nil {
		d.Log.Warn("forget: read states", "err", err)
		return
	}
	parents, present := map[string]bool{}, map[string]bool{}
	for _, n := range nodes {
		parents[n.Parent], present[n.Path] = true, true
	}
	cutoff := d.Now().Add(-keep)
	for _, n := range nodes {
		if n.Host != d.Host || n.Role == "routine" || parents[n.Path] || !doneBefore(states[n.Path], cutoff) {
			continue
		}
		if present[n.Parent] && states[n.Parent].State != "done" {
			continue
		}
		if SettleUnit(n) != "" && n.Runner.Kind == "t3" && n.Runner.Thread != "" && n.Settled == "" {
			continue
		}
		if left, _ := prune.NodeWorktreeLeft(ctx, n); left {
			continue
		}
		if err := mesh.ForgetNode(ctx, d.Client, n.Path); err != nil {
			d.Log.Warn("forget: node", "path", n.Path, "err", err)
			continue
		}
		d.Log.Info("forget: node done longer than forget_done", "path", n.Path, "done_at", states[n.Path].At)
	}
}
