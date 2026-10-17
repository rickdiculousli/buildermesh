package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/daemon"
	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/prune"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

const nodeUsage = "usage: bmesh node forget <path> [--worktree [--wait D]] | bmesh node stop <path> [--text \"<why>\"] [--wait D]"

// forgetTTL is how long a forget-worktree (or a stop-thread) waits for a
// box that is asleep.
const forgetTTL = 24 * time.Hour

func (a *app) cmdNode(args []string) error {
	if len(args) == 0 {
		return fail(exitUsage, nodeUsage)
	}
	switch args[0] {
	case "forget":
		return a.cmdNodeForget(args[1:])
	case "stop":
		return a.cmdNodeStop(args[1:])
	}
	return fail(exitUsage, nodeUsage)
}

// cmdNodeStop stops a node: the nodes under it first (a round's children,
// a lead's executors), deepest first, then the node itself, each through
// daemon.StopNode (blocked "stopped by <who>: <why>", then its thread
// interrupted). Threads on other boxes are stopped there by stop-thread
// jobs, sent together after the ones here, with [hosts] on; with it off
// the stop is refused before anything is touched. A round is then settled
// from its children, or, with none left, blocked directly. A done or
// blocked node prints `already <state>`; a stopped one's thread here is
// interrupted again.
func (a *app) cmdNodeStop(args []string) error {
	fs := a.flags("node stop")
	why := fs.String("text", "", "why, as the node's blocked text says it")
	wait := fs.Duration("wait", 30*time.Second, "for a thread on another box: how long to wait for its answer")
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
	if err := a.connect(ctx, "node stop"); err != nil {
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
	finished := func(p string) (string, bool) {
		s := states[p].State
		return s, s == "done" || s == "blocked"
	}
	who, err := a.identityOrHost(ctx, "node stop")
	if err != nil {
		return err
	}
	if s, ok := finished(path); ok {
		// A stopped node's thread here is interrupted again (a stop whose
		// interrupt failed is run again), quietly: it is already stopped.
		if mesh.Stopped(states[path]) && n.Runner.Thread != "" && (n.Host == "" || n.Host == a.cfg.HostName()) && n.Runner.Kind == a.cfg.Runner.Kind {
			r, err := runner.New(a.cfg)
			if err != nil {
				return err
			}
			if _, err := daemon.StopNode(ctx, a.client, r, n, who, *why); err != nil {
				return err
			}
		}
		fmt.Fprintf(a.stdout, "already %s\n", s)
		return nil
	}
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return err
	}
	var under, stop []mesh.Node
	for _, c := range nodes {
		if strings.HasPrefix(c.Path, path+".") {
			under = append(under, c)
		}
	}
	sort.Slice(under, func(i, j int) bool {
		di, dj := strings.Count(under[i].Path, "."), strings.Count(under[j].Path, ".")
		if di != dj {
			return di > dj
		}
		return under[i].Path < under[j].Path
	})
	for _, c := range under {
		if _, ok := finished(c.Path); !ok {
			stop = append(stop, c)
		}
	}
	if n.Role != "round" {
		stop = append(stop, n)
	}

	// Refuse what can't be stopped before stopping anything.
	me := a.cfg.HostName()
	var r runner.Runner
	var box *hostsBox
	for _, c := range stop {
		switch {
		case c.Runner.Thread == "":
		case c.Host != "" && c.Host != me && !a.cfg.Hosts.Enabled:
			return fail(exitRefused, "refused: %s runs on %s; run it on %s", c.Path, c.Host, c.Host)
		case c.Host != "" && c.Host != me:
			if box == nil {
				if box, err = a.hostsBox(); err != nil {
					return err
				}
			}
		case c.Runner.Kind != a.cfg.Runner.Kind:
			return fail(exitRefused, "refused: %s's runner is %q, not this box's %s; run it on %s", c.Path, c.Runner.Kind, a.cfg.Runner.Kind, c.Host)
		case r == nil:
			if r, err = runner.New(a.cfg); err != nil {
				return err
			}
		}
	}
	var remote []mesh.Node
	for _, c := range stop {
		if c.Runner.Thread != "" && c.Host != "" && c.Host != me {
			remote = append(remote, c)
			continue
		}
		var already string
		if c.Runner.Thread == "" {
			already, err = daemon.MarkStopped(ctx, a.client, c.Path, who, *why)
		} else {
			already, err = daemon.StopNode(ctx, a.client, r, c, who, *why)
		}
		if err != nil {
			return err
		}
		if already == "" {
			fmt.Fprintf(a.stdout, "stopped %s\n", c.Path)
		}
	}
	if len(remote) > 0 {
		if err := a.stopRemote(ctx, box, remote, who, *why, *wait); err != nil {
			return err
		}
	}
	if n.Role == "round" {
		state, ok, err := daemon.StopRound(ctx, a.client, n, len(under) > 0, who, *why)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintf(a.stdout, "%s stays %s until its children are stopped\n", path, state)
			return nil
		}
		fmt.Fprintf(a.stdout, "%s %s\n", path, state)
	}
	return nil
}

// stopRemote sends stop-thread for each of nodes to its host, all at once,
// and waits up to wait for the answers, printing `<host>: <answer>` for
// each in order. A node whose box doesn't answer in time is marked stopped
// from here, so its daemon never wakes it, and that box stops the thread
// itself when it takes the job. A refusal exits 3, after the rest.
func (a *app) stopRemote(ctx context.Context, b *hostsBox, nodes []mesh.Node, who, why string, wait time.Duration) error {
	if err := b.sync(ctx, a); err != nil {
		fmt.Fprintf(a.stderr, "bmesh: roster: %v (using this box's copy)\n", err)
	}
	r := b.local.Roster
	var jobs []hosts.Job
	for _, n := range nodes {
		if _, ok := r.Host(n.Host); !ok {
			return fail(exitRefused, "refused: %s runs on %s, which isn't in the roster; run it on %s", n.Path, n.Host, n.Host)
		}
		jobs = append(jobs, hosts.NewJob(b.key, b.host, n.Host, "stop-thread", "", []string{n.Path, who, why}, r.Version, time.Now(), forgetTTL))
	}
	replies, err := a.askBoxes(ctx, b, jobs, wait, nil)
	if err != nil {
		return err
	}
	var refused []string
	for i, n := range nodes {
		rep, ok := replies[jobs[i].ID]
		switch {
		case !ok:
			if _, err := daemon.MarkStopped(ctx, a.client, n.Path, who, why); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "queued; %s is marked stopped and %s stops its thread when it answers\n", n.Path, n.Host)
		case !rep.OK:
			refused = append(refused, n.Host+": "+rep.Text)
		default:
			fmt.Fprintf(a.stdout, "%s: %s\n", n.Host, rep.Text)
		}
	}
	if len(refused) > 0 {
		return fail(exitRefused, "%s", strings.Join(refused, "\n"))
	}
	return nil
}

// cmdNodeForget deletes a done or blocked node, a round whose children are
// all gone, or an idle one that never reported and whose thread isn't
// running: its registration, state, runner
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
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return err
	}
	childless := n.Role == "round"
	for _, c := range nodes {
		if strings.HasPrefix(c.Path, path+".") {
			childless = false
		}
	}
	switch s := stateOf(path); {
	case s == "done" || s == "blocked":
	case childless:
		// A round whose children are all gone has nothing left to settle
		// from: it is forgotten whatever its state.
	case s == "idle":
		if err := a.idleForgettable(ctx, n); err != nil {
			return err
		}
	default:
		return fail(exitRefused, "refused: %s is %s; only a done or blocked node is forgotten", path, s)
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
