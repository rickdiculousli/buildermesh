package main

import (
	"fmt"

	"github.com/rickdiculousli/buildermesh"
	"github.com/rickdiculousli/buildermesh/internal/align"
)

const alignUsage = "usage: bmesh align <repo> [--check]"

// cmdAlign gives a ship repo its AGENTS.md sections, its proposals
// directory and the routine kinds its row names: bmesh align <repo>
// [--check]. --check prints the unified diff of what would change and
// exits 1 when there is any; a row that is not ship, or a repo without a
// row, exits 2.
func (a *app) cmdAlign(args []string) error {
	fs := a.flags("align")
	check := fs.Bool("check", false, "print the diff and exit 1 when the repo would change; write nothing")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail(exitUsage, alignUsage)
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	name, repoPath, err := a.cfg.RepoPath(pos[0])
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	row, ok := a.cfg.Repos[name]
	if !ok {
		return fail(exitUsage, "%s is not in repos.toml; align needs its row (verify, remote, routines)", name)
	}
	if row.Mode != "ship" {
		return fail(exitUsage, "%s is %s; only ship repos are aligned", name, row.Mode)
	}
	targets, skipped, err := align.Targets(repoPath, row, buildermesh.AgentsSnippet)
	if err != nil {
		return err
	}
	if *check {
		diff := align.Plan(targets)
		fmt.Fprint(a.stdout, diff)
		if diff != "" {
			return fail(exitError, "")
		}
		return nil
	}
	written, err := align.Write(repoPath, targets)
	for _, rel := range written {
		fmt.Fprintf(a.stdout, "wrote %s\n", rel)
	}
	if err != nil {
		return err
	}
	for _, k := range skipped {
		fmt.Fprintf(a.stderr, "bmesh: the row lists no verify commands, so the %s kind was not written\n", k)
	}
	if len(written) == 0 {
		fmt.Fprintf(a.stdout, "%s is aligned\n", name)
	}
	return nil
}
