// Package addons holds the optional parts compiled into bmesh, one package
// each under addons/. bmesh serve runs one only when config.toml's [addons]
// enable names it; its settings are its [addons.<name>] table.
package addons

import (
	"context"
	"log/slog"

	"github.com/rickdiculousli/buildermesh/addons/t3done"
	"github.com/rickdiculousli/buildermesh/internal/config"
)

// Env is what an add-on runs with.
type Env struct {
	Cfg      *config.Config
	Host     string
	StateDir string // the add-on's own: <state dir>/addons/<name>
	Log      *slog.Logger
}

// Registry is every add-on by its name (config.AddonNames), each run until
// ctx ends.
var Registry = map[string]func(ctx context.Context, e Env){
	"t3-done": func(ctx context.Context, e Env) { t3done.Run(ctx, e.Cfg, e.Host, e.StateDir, e.Log) },
}
