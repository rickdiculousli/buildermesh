package daemon

import (
	"context"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/usage"
)

// usageEvery is how often the usage loop reads what grew in this box's
// transcripts (105), whatever [routines] says.
const usageEvery = 30 * time.Second

// usageOnce runs one usage pass; a failure is logged and the next tick
// starts from the cursor.
func usageOnce(ctx context.Context, d Deps) {
	repos := map[string]string{}
	for _, name := range d.Cfg.RepoNames() {
		if _, p, err := d.Cfg.RepoPath(name); err == nil {
			repos[name] = p
		}
	}
	err := usage.Pass(ctx, usage.Env{Client: d.Client, Host: d.Host, Claude: d.ClaudeDir, StateDir: d.StateDir,
		Checkouts: d.Checkouts, Repos: repos, Log: d.Log, Now: d.Now})
	if err != nil && ctx.Err() == nil {
		d.Log.Warn("usage: pass", "err", err)
	}
}
