// Command hostload reads this box as its daemon's sampler does (117) and
// prints, each period, the minute the sampler reads (120), one JSON line
// each: {"minute": ..., "cover": {agent: run over CPU seconds}}. With --cwd the box's only node is a fake one, "load",
// whose worktree is that dir, so a workload started there reads as an
// agent; without it, it reads the hub's bm-nodes and bm-state for this
// box's nodes that are not done (BMESH_CONFIG picks the config as for
// bmesh). It writes nothing: run it beside a live daemon
// (go run ./scripts/hostload --cwd <dir> --minutes N).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/hoststat"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/session"
)

func main() {
	cwd := flag.String("cwd", "", "the fake node's worktree: a workload run there is agent \"load\"")
	minutes := flag.Int("minutes", 1, "how many periods to print (0: until interrupted)")
	every := flag.Duration("every", time.Minute, "the period between reads")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *cwd, *minutes, *every); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "hostload:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cwd string, minutes int, every time.Duration) error {
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	host := cfg.HostName()
	env := hoststat.Env{Host: host, GOOS: runtime.GOOS}
	if cwd != "" {
		dir, err := filepath.Abs(cwd)
		if err != nil {
			return err
		}
		node := mesh.Node{Path: "load", Host: host, Worktree: dir}
		env.Nodes = func(context.Context) ([]mesh.Node, error) { return []mesh.Node{node}, nil }
	} else {
		if cfg.Mesh.NATS == "" {
			return fmt.Errorf("[mesh] nats is empty: this box embeds its hub, and only its bmesh serve opens it; pass --cwd")
		}
		token, err := cfg.Token()
		if err != nil {
			return err
		}
		c, err := mesh.Connect(ctx, cfg.Mesh.NATS, token, cfg.Mesh.Domain, "bmesh/hostload", false)
		if err != nil {
			return err
		}
		defer c.Close()
		repos := map[string]string{}
		for _, name := range cfg.RepoNames() {
			if _, p, err := cfg.RepoPath(name); err == nil {
				repos[name] = p
			}
		}
		env.Claude, _ = session.ClaudeDir(os.Getenv)
		env.Checkouts, env.Repos = cfg.Expand(cfg.Runner.Checkouts), repos
		env.Nodes = func(ctx context.Context) ([]mesh.Node, error) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			return hoststat.LiveNodes(ctx, c, host)
		}
	}
	s := hoststat.New(env)
	if _, err := s.Minute(ctx); err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	t := time.NewTicker(every)
	defer t.Stop()
	for i := 0; minutes == 0 || i < minutes; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		m, err := s.Minute(ctx)
		if err != nil {
			return err
		}
		if m == nil {
			continue
		}
		if err := enc.Encode(struct {
			Minute *hoststat.Minute   `json:"minute"`
			Cover  map[string]float64 `json:"cover"`
		}{m, m.Cover}); err != nil {
			return err
		}
	}
	return nil
}
