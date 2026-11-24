package daemon

import (
	"context"
	"encoding/json"
	"runtime"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/rickdiculousli/buildermesh/internal/hoststat"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// hostStatEvery is the minute series' period; a var for the tests.
var hostStatEvery = time.Minute

// hostStatRun runs this box's sampler (117) until ctx ends: a read each
// minute published to bmusage.host.<host> (a failed publish is dropped,
// as quota's is), and, from the first read on, live reads answered on
// bmhosts.load.<host> (`null` while none can be made). Windows reads
// nothing. The sampler measures real time, so it reads the wall clock,
// never d.Now.
func hostStatRun(ctx context.Context, d Deps) {
	s := hoststat.New(hoststat.Env{Host: d.Host, GOOS: runtime.GOOS, Claude: d.ClaudeDir, Checkouts: d.Checkouts, Repos: repoPaths(d),
		Nodes: func(ctx context.Context) ([]mesh.Node, error) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			return hoststat.LiveNodes(ctx, d.Client, d.Host)
		}})
	if !s.Supported() {
		return
	}
	minute := func() {
		m, err := s.Minute(ctx)
		if err != nil && ctx.Err() == nil {
			d.Log.Warn("hoststat: read", "err", err)
		}
		if m == nil {
			return
		}
		pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		_ = hoststat.Publish(pctx, d.Client, m)
	}
	minute()
	sub, err := d.Client.NC.Subscribe(hoststat.LoadSubject(d.Host), func(msg *nats.Msg) {
		b := []byte("null")
		if l, err := s.Live(ctx); err == nil && l != nil {
			b, _ = json.Marshal(l)
		} else if err != nil {
			d.Log.Warn("hoststat: live read", "err", err)
		}
		_ = msg.Respond(b)
	})
	if err != nil {
		d.Log.Warn("hoststat: live answers", "err", err)
	} else {
		defer sub.Unsubscribe()
	}
	t := time.NewTicker(hostStatEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			minute()
		}
	}
}

// repoPaths is the registry's rows that resolve: name to checkout path.
func repoPaths(d Deps) map[string]string {
	repos := map[string]string{}
	for _, name := range d.Cfg.RepoNames() {
		if _, p, err := d.Cfg.RepoPath(name); err == nil {
			repos[name] = p
		}
	}
	return repos
}
