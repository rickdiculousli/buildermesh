package main

import (
	"fmt"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

// configRule is a check config check makes past config.Load's own; tests
// set one to stand for a binary that rejects what an older one took.
var configRule = func(*config.Config) error { return nil }

// checkConfig loads the config at path (default config.Path()) as every
// command does, and applies configRule: nil when this binary takes it.
func checkConfig(path string) (*config.Config, error) {
	if path == "" {
		path = config.Path()
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if err := configRule(cfg); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// cmdConfig is `config check`: load this box's config (and registry) with
// this binary and say whether it takes it, naming the key when it doesn't
// (exit 3), so an update checks a config with the new binary before it
// swaps it in.
func (a *app) cmdConfig(args []string) error {
	fs := a.flags("config")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || pos[0] != "check" {
		return fail(exitUsage, "usage: bmesh config check")
	}
	cfg, err := checkConfig(a.configPath)
	if err != nil {
		return fail(exitRefused, "config check failed: %v", err)
	}
	for _, l := range cfg.Ignored() {
		fmt.Fprintln(a.stderr, l)
	}
	fmt.Fprintf(a.stdout, "config ok: %s\n", cfg.Path())
	return nil
}
