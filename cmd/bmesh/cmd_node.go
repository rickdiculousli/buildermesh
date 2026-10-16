package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/prune"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

const nodeUsage = "usage: bmesh node forget <path> [--worktree [--wait D]]"

// forgetTTL is how long a forget-worktree waits for a box that is asleep.
const forgetTTL = 24 * time.Hour

func (a *app) cmdNode(args []string) error {
	if len(args) == 0 || args[0] != "forget" {
		return fail(exitUsage, nodeUsage)
	}
	return a.cmdNodeForget(args[1:])
}

// cmdNodeForget deletes a done or blocked node, or an idle one that never
// reported and whose thread isn't running: its registration, state, runner
// sample and inbox consumer. With --worktree it first removes the
// node's clean worktree and merged branch: here, or, for another box's
// node with hosts on, by a forget-worktree job to that box, forgetting
// the node only once that box answers it did.
func (a *app) cmdNodeForget(args []string) error {
	fs := a.flags("node forget")
	withWorktree := fs.Bool("worktree", false, "also remove the node's clean worktree and merged branch, on its host")
	wait := fs.Duration("wait", 30*time.Second, "with --worktree on another box: how long to wait for its answer")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail(exitUsage, nodeUsage)
	}
	p, err := mesh.ParsePath(pos[0])
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	path := p.String()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute+*wait)
	defer cancel()
	if err := a.connect(ctx, "node forget"); err != nil {
		return err
	}
	n, ok, err := mesh.GetNode(ctx, a.client, path)
	if err != nil {
		return err
	}
	if !ok {
		return fail(exitRefused, "refused: no node %s", path)
	}
	states, err := mesh.ReadAllStates(ctx, a.client)
	if err != nil {
		return err
	}
	stateOf := func(p string) string {
		if s, ok := states[p]; ok {
			return s.State
		}
		return "idle"
	}
	switch s := stateOf(path); s {
	case "done", "blocked":
	case "idle":
		if err := a.idleForgettable(ctx, n); err != nil {
			return err
		}
	default:
		return fail(exitRefused, "refused: %s is %s; only a done or blocked node is forgotten", path, s)
	}
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return err
	}
	for _, c := range nodes {
		if s := stateOf(c.Path); strings.HasPrefix(c.Path, path+".") && s != "done" && s != "blocked" {
			return fail(exitRefused, "refused: %s under %s is %s", c.Path, path, s)
		}
	}

	var kept string
	if *withWorktree && n.Worktree != "" && n.Host != "" && n.Host != a.cfg.HostName() && a.cfg.Hosts.Enabled {
		if b, err := a.hostsBox(); err == nil {
			ok, err := a.forgetRemote(ctx, b, n, *wait)
			if err != nil || !ok {
				return err
			}
			// The host forgot it too: forgetting is idempotent.
			if err := mesh.ForgetNode(ctx, a.client, path); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "forgot %s\n", path)
			return nil
		}
	}
	if *withWorktree {
		if err := a.removeNodeWorktree(ctx, n, nodes); err != nil {
			return err
		}
	} else if n.Worktree != "" {
		kept = n.Worktree
	}
	if err := mesh.ForgetNode(ctx, a.client, path); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "forgot %s\n", path)
	if kept != "" {
		fmt.Fprintf(a.stdout, "worktree %s kept\n", kept)
	}
	return nil
}

// idleForgettable refuses (exit 3) an idle node that published an event of
// its own, whose thread isn't this box's runner's, or whose runner, asked
// once, can't say the thread is known and not running: a lead between its
// runner start and its first started is idle with no events too.
func (a *app) idleForgettable(ctx context.Context, n mesh.Node) error {
	hist, err := mesh.History(ctx, a.client, "bm."+n.Path+".*", time.Time{})
	if err != nil {
		return err
	}
	for _, m := range hist {
		var ev mesh.Event
		if json.Unmarshal(m.Data, &ev) == nil && ev.From == n.Path {
			return fail(exitRefused, "refused: %s is idle but published %s; only an idle node that never reported is forgotten", n.Path, ev.Event)
		}
	}
	if n.Host == "" {
		return fail(exitRefused, "refused: %s is idle and names no host", n.Path)
	}
	if n.Host != a.cfg.HostName() {
		return fail(exitRefused, "refused: %s is idle and its thread is on %s; run it on %s", n.Path, n.Host, n.Host)
	}
	if n.Runner.Kind != a.cfg.Runner.Kind {
		return fail(exitRefused, "refused: %s is idle and its runner is %q, not this box's %s; run it on %s", n.Path, n.Runner.Kind, a.cfg.Runner.Kind, n.Host)
	}
	mayRun := fail(exitRefused, "refused: %s is idle and its thread may be running; stop it first", n.Path)
	if n.Runner.Thread == "" {
		return mayRun
	}
	r, err := runner.New(a.cfg)
	if err != nil {
		return err
	}
	st, err := r.Status(ctx, n.Runner.Thread)
	switch {
	case err != nil:
		return fail(exitRefused, "refused: %s is idle and its thread may be running (%v); stop it first", n.Path, err)
	case !st.Known:
		return mayRun
	case st.Running:
		return fail(exitRefused, "refused: %s is idle and its thread %s is running; stop it first", n.Path, n.Runner.Thread)
	}
	return nil
}

// removeNodeWorktree removes n's worktree and merged branch on this host
// (prune.RemoveNodeWorktree), printing what it did; a refusal exits 3.
func (a *app) removeNodeWorktree(ctx context.Context, n mesh.Node, nodes []mesh.Node) error {
	lines, err := prune.RemoveNodeWorktree(ctx, n, nodes, a.cfg.HostName())
	for _, l := range lines {
		fmt.Fprintln(a.stdout, l)
	}
	var ref *prune.Refusal
	if errors.As(err, &ref) {
		return fail(exitRefused, "%s", ref.Text)
	}
	return err
}

// forgetRemote sends forget-worktree for n to its host and waits up to
// wait: true when the host removed the worktree and forgot the node.
// A refusal exits 3; no answer leaves the job queued and the node kept.
func (a *app) forgetRemote(ctx context.Context, b *hostsBox, n mesh.Node, wait time.Duration) (bool, error) {
	if err := b.sync(ctx, a); err != nil {
		fmt.Fprintf(a.stderr, "bmesh: roster: %v (using this box's copy)\n", err)
	}
	r := b.local.Roster
	if _, ok := r.Host(n.Host); !ok {
		return false, fail(exitRefused, "refused: worktree %s is on %s, which isn't in the roster; run it there", n.Worktree, n.Host)
	}
	j := hosts.NewJob(b.key, b.host, n.Host, "forget-worktree", "", []string{n.Path}, r.Version, time.Now(), forgetTTL)
	replies, err := a.askBoxes(ctx, b, []hosts.Job{j}, wait, nil)
	if err != nil {
		return false, err
	}
	rep, ok := replies[j.ID]
	switch {
	case !ok:
		fmt.Fprintf(a.stdout, "queued; the node stays until %s answers\n", n.Host)
		return false, nil
	case !rep.OK:
		return false, fail(exitRefused, "%s: %s", n.Host, rep.Text)
	}
	fmt.Fprintf(a.stdout, "%s: %s\n", n.Host, rep.Text)
	return true, nil
}
