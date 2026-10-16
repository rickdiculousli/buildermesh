package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/prune"
)

const pruneUsage = "usage: bmesh prune [--repo R | --all] [--apply] [--wait D]"

// pruneTTL is how long a prune waits for a box that is asleep.
const pruneTTL = 24 * time.Hour

// pruneTimeout bounds this box's own prune.
const pruneTimeout = 10 * time.Minute

// cmdPrune prunes this box's checkouts (a dry run unless --apply), then,
// with hosts on, sends prune to every other roster box that accepts it and
// prints each box's rows, prefixed with its name, as they arrive within
// --wait. It exits 1 when a removal here failed.
func (a *app) cmdPrune(args []string) error {
	fs := a.flags("prune")
	repo := fs.String("repo", "", "the registry repo to prune (default: the repo of the current directory)")
	all := fs.Bool("all", false, "every registered repo")
	apply := fs.Bool("apply", false, "remove the remove rows (default: a dry run)")
	wait := fs.Duration("wait", 30*time.Second, "how long to wait for the other boxes' answers")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || (*all && *repo != "") {
		return fail(exitUsage, pruneUsage)
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	var names []string
	switch {
	case *all:
		names = a.cfg.RepoNames()
	case *repo != "":
		if _, ok := a.cfg.Repos[*repo]; !ok {
			return fail(exitRefused, "%s is not in the registry", *repo)
		}
		names = []string{*repo}
	default:
		cur, ok := cwdRepo()
		if !ok {
			return fail(exitUsage, "not in a registered repo's checkout: name one with --repo, or --all")
		}
		n, _, err := a.cfg.RepoPath(cur.Path)
		if _, reg := a.cfg.Repos[n]; err != nil || !reg {
			return fail(exitUsage, "%s is not in the registry: name a repo with --repo, or --all", cur.Path)
		}
		names = []string{n}
	}
	var b *hostsBox
	if a.cfg.Hosts.Enabled {
		if b, err = a.hostsBox(); err != nil {
			fmt.Fprintf(a.stderr, "bmesh: %v; pruning this box only\n", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), pruneTimeout+*wait+time.Minute)
	defer cancel()
	if err := a.connect(ctx, "prune"); err != nil {
		return err
	}
	failed, err := a.pruneHere(ctx, names, *all, *apply)
	if err != nil {
		return err
	}
	if b != nil {
		if err := a.pruneBoxes(ctx, b, names, *all, *apply, *wait); err != nil {
			return err
		}
	}
	if failed {
		return fail(exitError, "a removal on %s failed", a.cfg.HostName())
	}
	return nil
}

// pruneHere prunes this box's checkouts of names in-process, printing its
// rows and summary prefixed with its name; failed: a removal or a repo
// failed.
func (a *app) pruneHere(ctx context.Context, names []string, all, apply bool) (failed bool, err error) {
	host := a.cfg.HostName()
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return false, err
	}
	states, err := mesh.ReadAllStates(ctx, a.client)
	if err != nil {
		return false, err
	}
	var rows []prune.Row
	for _, name := range names {
		path, ok := a.checkoutHere(name)
		if !ok {
			if !all {
				fmt.Fprintf(a.stdout, "%s: no checkout of %s here\n", host, name)
				return false, nil
			}
			continue
		}
		got, err := prune.Repo(ctx, prune.Checkout{Repo: name, Path: path, Self: host, Nodes: nodes, States: states,
			Worktrees: a.cfg.Expand(a.cfg.Runner.Worktrees), Checkouts: a.cfg.Expand(a.cfg.Runner.Checkouts),
			StateDir: config.StateDir(), Apply: apply})
		for _, r := range got {
			fmt.Fprintf(a.stdout, "%s: %s\n", host, r)
			failed = failed || r.Action == prune.Failed
		}
		if err != nil {
			fmt.Fprintf(a.stdout, "%s: %s error %v\n", host, name, err)
			failed = true
		}
		rows = append(rows, got...)
	}
	fmt.Fprintf(a.stdout, "%s: %s\n", host, prune.Summary(rows, apply))
	return failed, nil
}

// checkoutHere is repo's checkout on this box, when its directory exists.
func (a *app) checkoutHere(repo string) (string, bool) {
	_, path, err := a.cfg.RepoPath(repo)
	if err != nil || !dirExists(path) {
		return "", false
	}
	return path, true
}

// pruneBoxes sends prune to every other roster box that accepts it and
// prints their rows as they arrive; a box with no answer prints as
// pending.
func (a *app) pruneBoxes(ctx context.Context, b *hostsBox, names []string, all, apply bool, wait time.Duration) error {
	if err := b.sync(ctx, a); err != nil {
		fmt.Fprintf(a.stderr, "bmesh: roster: %v (using this box's copy)\n", err)
	}
	r := b.local.Roster
	repo := ""
	if !all {
		repo = names[0]
	}
	var args []string
	if apply {
		args = []string{"apply"}
	}
	var jobs []hosts.Job
	for _, to := range accepting(r, "prune") {
		if to == b.host {
			continue
		}
		jobs = append(jobs, hosts.NewJob(b.key, b.host, to, "prune", repo, args, r.Version, time.Now(), pruneTTL))
	}
	if len(jobs) == 0 {
		return nil
	}
	seen := a.presence(ctx, r)
	replies, err := a.askBoxes(ctx, b, jobs, wait, func(j hosts.Job, rep hosts.Reply) {
		for _, line := range strings.Split(rep.Text, "\n") {
			fmt.Fprintf(a.stdout, "%s: %s\n", j.To, line)
		}
	})
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if _, ok := replies[j.ID]; !ok {
			fmt.Fprintf(a.stdout, "%s: %s\n", j.To, pending(j, seen, wait))
		}
	}
	return nil
}
