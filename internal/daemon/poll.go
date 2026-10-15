package daemon

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// threaded reports whether a node is a session the daemon polls and wakes:
// a lead, or an engineer (a round's thread).
func threaded(n mesh.Node) bool { return n.Role == "lead" || n.Role == "engineer" }

// localLeads returns the leads and engineers this host runs that have a thread.
func localLeads(ctx context.Context, d Deps) ([]mesh.Node, error) {
	nodes, err := mesh.ListNodes(ctx, d.Client)
	if err != nil {
		return nil, err
	}
	var out []mesh.Node
	for _, n := range nodes {
		if threaded(n) && n.Host == d.Host && n.Runner.Thread != "" {
			out = append(out, n)
		}
	}
	return out, nil
}

// syncBranch rewrites a lead's recorded branch to the one its worktree is on
// now: a console may have renamed it since dispatch. A missing worktree or a
// detached HEAD leaves the node as it is.
func syncBranch(ctx context.Context, d Deps, n mesh.Node) {
	if n.Worktree == "" {
		return
	}
	if _, err := os.Stat(n.Worktree); err != nil {
		return
	}
	out, err := exec.CommandContext(ctx, "git", "-C", n.Worktree, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return
	}
	b := strings.TrimSpace(string(out))
	if b == "" || b == "HEAD" || b == n.Branch {
		return
	}
	cur, ok, err := mesh.GetNode(ctx, d.Client, n.Path)
	if err != nil || !ok || cur.Branch == b || cur.Worktree != n.Worktree || cur.Runner.Thread != n.Runner.Thread {
		return
	}
	old := cur.Branch
	cur.Branch = b
	if err := mesh.PutNode(ctx, d.Client, cur); err != nil {
		d.Log.Warn("poll: put node", "path", n.Path, "err", err)
		return
	}
	d.Log.Info("branch: " + n.Path + " " + old + " -> " + b)
}

// pollOnce reads the runner of every local lead that is not done and stores
// the sample in bm-runner; it also keeps the lead's recorded branch level
// with its worktree's.
func pollOnce(ctx context.Context, d Deps, _ *Activity) {
	leads, err := localLeads(ctx, d)
	if err != nil {
		d.Log.Warn("poll: list nodes", "err", err)
		return
	}
	for _, n := range leads {
		if ctx.Err() != nil {
			return
		}
		if st, _, err := mesh.ReadState(ctx, d.Client, n.Path); err == nil && st.State == "done" {
			continue
		}
		syncBranch(ctx, d, n)
		s, err := d.Runner.Status(ctx, n.Runner.Thread)
		if err != nil {
			d.Log.Warn("poll: runner status", "path", n.Path, "thread", n.Runner.Thread, "err", err)
			continue
		}
		sample := mesh.RunnerSample{Running: s.Running, Known: s.Known, LastReply: s.LastReply,
			At: d.Now().UTC().Format(time.RFC3339)}
		if err := mesh.PutRunner(ctx, d.Client, n.Path, sample); err != nil {
			d.Log.Warn("poll: put runner", "path", n.Path, "err", err)
		}
	}
}
