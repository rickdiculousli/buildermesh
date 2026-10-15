package daemon

import (
	"context"
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

// pollOnce reads the runner of every local lead that is not done and stores
// the sample in bm-runner.
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
