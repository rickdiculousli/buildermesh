package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/kind"
	wsetup "github.com/rickdiculousli/buildermesh/internal/setup"
)

const setupUsage = "usage: bmesh setup [--check] [--force]"

// cmdSetup runs the cwd worktree's [setup] (its .bmesh/config.toml) when
// its installs aren't current, and marks them current (wsetup.Ensure).
// --check runs nothing and exits 1 when they are stale or missing; --force
// runs it anyway. A worktree whose repo names no setup has nothing to do.
func (a *app) cmdSetup(args []string) error {
	fs := a.flags("setup")
	check := fs.Bool("check", false, "say whether the installs are current; exit 1 when stale or missing; run nothing")
	force := fs.Bool("force", false, "run even when the installs are current")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || *check && *force {
		return fail(exitUsage, setupUsage)
	}
	top, err := gitRev("--show-toplevel")
	if err != nil {
		return fail(exitUsage, "bmesh setup runs in a worktree: %v", err)
	}
	rc, err := kind.LoadRepoConfig(top)
	if err != nil {
		return err
	}
	s := rc.Setup
	if s.Command == "" {
		fmt.Fprintf(a.stdout, "setup: none (%s names no [setup])\n", kind.ConfigFile)
		return nil
	}
	ctx := context.Background()
	if *check {
		cur, err := wsetup.Current(ctx, top, s)
		if err != nil {
			return err
		}
		if cur {
			fmt.Fprintf(a.stdout, "setup: current (%s)\n", s.Command)
			return nil
		}
		m, err := wsetup.Marker(ctx, top)
		if err != nil {
			return err
		}
		what := "stale"
		if _, err := os.Stat(m); errors.Is(err, os.ErrNotExist) {
			what = "missing"
		}
		fmt.Fprintf(a.stdout, "setup: %s (%s); run bmesh setup\n", what, s.Command)
		return fail(exitError, "")
	}
	start := time.Now()
	ran, _, err := wsetup.Ensure(ctx, top, s, wsetup.Options{Always: *force, Out: a.stdout})
	if se := (*wsetup.Error)(nil); errors.As(err, &se) {
		return fail(exitError, "%s", se.Msg)
	}
	if err != nil {
		return err
	}
	if !ran {
		fmt.Fprintf(a.stdout, "setup: current (%s)\n", s.Command)
		return nil
	}
	fmt.Fprintf(a.stdout, "setup: ran (%s) in %s\n", s.Command, time.Since(start).Round(100*time.Millisecond))
	return nil
}
