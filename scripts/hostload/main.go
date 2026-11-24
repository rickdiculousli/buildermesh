// Command hostload reads this box as its daemon's sampler does (117) and
// prints one live answer as JSON: two reads 5 s apart, the processes
// attributed to this box's nodes that are not done. It reads the box and
// the hub's bm-nodes and bm-state, and writes nothing: run it on a box with
// a real mesh (go run ./scripts/hostload) without a second bmesh serve.
// BMESH_CONFIG picks the config as for bmesh.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/hoststat"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/session"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hostload:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	if cfg.Mesh.NATS == "" {
		return fmt.Errorf("[mesh] nats is empty: this box embeds its hub, and only its bmesh serve opens it")
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
	host := cfg.HostName()
	repos := map[string]string{}
	for _, name := range cfg.RepoNames() {
		if _, p, err := cfg.RepoPath(name); err == nil {
			repos[name] = p
		}
	}
	claude, _ := session.ClaudeDir(os.Getenv)
	s := hoststat.New(hoststat.Env{Host: host, GOOS: runtime.GOOS, Claude: claude, Checkouts: cfg.Expand(cfg.Runner.Checkouts), Repos: repos,
		Nodes: func(ctx context.Context) ([]mesh.Node, error) { return hoststat.LiveNodes(ctx, c, host) }})
	if _, err := s.Minute(ctx); err != nil {
		return err
	}
	time.Sleep(5 * time.Second)
	l, err := s.Live(ctx)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(l)
}
