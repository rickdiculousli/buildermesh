package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

func (a *app) cmdWhoami(args []string) error {
	pos, err := parse(a.flags("whoami"), args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, "whoami takes no arguments")
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	p, err := a.identity(ctx, "whoami")
	if err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, p.String())
	return nil
}

// cmdThreadStatus prints a node's state: bmesh thread-status [<path>].
func (a *app) cmdThreadStatus(args []string) error {
	pos, err := parse(a.flags("thread-status"), args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return fail(exitUsage, "usage: bmesh thread-status [<path>]")
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	var p mesh.Path
	if len(pos) == 1 {
		if p, err = mesh.ParsePath(pos[0]); err != nil {
			return fail(exitUsage, "%v", err)
		}
		if err := a.connect(ctx, "thread-status"); err != nil {
			return err
		}
	} else if p, err = a.identity(ctx, "thread-status"); err != nil {
		return err
	}
	s, _, err := mesh.ReadState(ctx, a.client, p.String())
	if err != nil {
		return err
	}
	if a.json {
		return a.printJSON(s)
	}
	line := s.State
	if s.At != "" {
		line += " since " + s.At
	}
	if s.Text != "" {
		line += ": " + s.Text
	}
	fmt.Fprintln(a.stdout, line)
	return nil
}

type statusRow struct {
	Node   mesh.Node          `json:"node"`
	State  mesh.State         `json:"state"`
	Runner *mesh.RunnerSample `json:"runner"`
	Unread int                `json:"unread"`
}

// cmdMeshStatus prints the registered nodes with their state, runner and
// unread count: every one with --all or --json, else all but those done for
// longer than [status] keep_done, which a closing line counts.
func (a *app) cmdMeshStatus(args []string) error {
	fs := a.flags("mesh-status")
	all := fs.Bool("all", false, "show every node, finished ones too")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, "usage: bmesh mesh-status [--all] [--json]")
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "mesh-status"); err != nil {
		return err
	}
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return err
	}
	states, err := mesh.ReadAllStates(ctx, a.client)
	if err != nil {
		return err
	}
	rows := make([]statusRow, 0, len(nodes))
	for _, n := range nodes {
		r := statusRow{Node: n, State: mesh.State{State: "idle"}}
		if s, ok := states[n.Path]; ok {
			r.State = s
		}
		if s, ok, err := mesh.GetRunner(ctx, a.client, n.Path); err != nil {
			return err
		} else if ok {
			r.Runner = &s
		}
		p, err := mesh.ParsePath(n.Path)
		if err != nil {
			return fmt.Errorf("node %s: %w", n.Path, err)
		}
		if r.Unread, err = mesh.Unread(ctx, a.client, p); err != nil {
			return err
		}
		rows = append(rows, r)
	}
	if a.json {
		return a.printJSON(rows)
	}
	keep := a.cfg.Status.KeepDone
	var hide map[string]bool
	if !*all {
		hide = finishedUnits(rows, time.Now().Add(-keep), keep > 0)
	}
	tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PATH\tROLE\tSTATE\tSINCE\tRUNNER\tUNREAD")
	for _, r := range rows {
		if hide[r.Node.Path] {
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\n", r.Node.Path, dash(r.Node.Role), r.State.State,
			dash(r.State.At), runnerWord(r.Runner), r.Unread)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if n := len(hide); n > 0 {
		noun := "nodes"
		if n == 1 {
			noun = "node"
		}
		fmt.Fprintf(a.stdout, "… %d finished %s older than %s (--all shows them)\n", n, noun, config.FormatDuration(keep))
	}
	return nil
}

// finishedUnits returns the paths mesh-status hides: nodes done before
// cutoff (none when !on). A round and its children are one unit, hidden only
// when every one of them is; blocked is never hidden.
func finishedUnits(rows []statusRow, cutoff time.Time, on bool) map[string]bool {
	hide := map[string]bool{}
	if !on {
		return hide
	}
	role := map[string]string{}
	for _, r := range rows {
		role[r.Node.Path] = r.Node.Role
	}
	unit := func(n mesh.Node) string {
		if n.Parent != "" && role[n.Parent] == "round" {
			return n.Parent
		}
		return n.Path
	}
	finished := map[string]bool{} // unit → every member done before cutoff
	for _, r := range rows {
		u := unit(r.Node)
		at, err := time.Parse(time.RFC3339, r.State.At)
		old := r.State.State == "done" && err == nil && at.Before(cutoff)
		if f, seen := finished[u]; !seen || f {
			finished[u] = old
		}
	}
	for _, r := range rows {
		if finished[unit(r.Node)] {
			hide[r.Node.Path] = true
		}
	}
	return hide
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func runnerWord(s *mesh.RunnerSample) string {
	switch {
	case s == nil || !s.Known:
		return "?"
	case s.Running:
		return "running"
	default:
		return "idle"
	}
}

func (a *app) printJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%s\n", b)
	return nil
}

var terminal = map[string]bool{"done": true, "blocked": true, "silent": true}

// cmdWatch follows the named nodes' trees and runner changes until every
// named node is done, blocked or silent. It needs no daemon.
func (a *app) cmdWatch(args []string) error {
	fs := a.flags("watch")
	nodesArg := fs.String("nodes", "", "comma-separated node paths")
	sinceArg := fs.String("since", "", "replay events from this RFC3339 time (default now)")
	timeout := fs.Duration("timeout", 30*time.Minute, "give up after this long (exit 5)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || *nodesArg == "" {
		return fail(exitUsage, "usage: bmesh watch --nodes a,b [--since RFC3339] [--timeout 30m]")
	}
	var named []string
	leads := map[string]bool{}
	for _, s := range strings.Split(*nodesArg, ",") {
		p, err := mesh.ParsePath(strings.TrimSpace(s))
		if err != nil {
			return fail(exitUsage, "%v", err)
		}
		named = append(named, p.String())
		leads[p.Lead()] = true
	}
	since := time.Now()
	if *sinceArg != "" {
		if since, err = time.Parse(time.RFC3339, *sinceArg); err != nil {
			return fail(exitUsage, "--since: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	timedOut := func(err error) error {
		if ctx.Err() != nil {
			return fail(exitTimeout, "watch timed out")
		}
		return err
	}
	if err := a.connect(ctx, "watch"); err != nil {
		return timedOut(err)
	}
	allTerminal := func() (bool, error) {
		states, err := mesh.ReadAllStates(ctx, a.client)
		if err != nil {
			return false, err
		}
		for _, p := range named {
			if !terminal[states[p].State] {
				return false, nil
			}
		}
		return true, nil
	}
	finished := func() error {
		fmt.Fprintln(a.stdout, "watch-done")
		return nil
	}
	if ok, err := allTerminal(); err != nil {
		return timedOut(err)
	} else if ok {
		return finished()
	}

	// Subscribe before replaying so nothing published in between is missed;
	// the replay prints up to the subscription's first sequence.
	live, err := mesh.Follow(ctx, a.client, "bm.>")
	if err != nil {
		return timedOut(err)
	}
	runners, stopRunners := mesh.WatchRunner(ctx, a.client)
	defer stopRunners()
	running := map[string]bool{}
	for _, p := range named {
		if s, ok, err := mesh.GetRunner(ctx, a.client, p); err != nil {
			return timedOut(err)
		} else if ok && s.Known {
			running[p] = s.Running
		}
	}
	var hist []mesh.Stored
	for lead := range leads {
		h, err := mesh.History(ctx, a.client, mesh.TreeFilter(lead), since)
		if err != nil {
			return timedOut(err)
		}
		hist = append(hist, h...)
	}
	sort.Slice(hist, func(i, j int) bool { return hist[i].Seq < hist[j].Seq })
	var lastSeq uint64
	for _, m := range hist {
		a.printEvent(m, leads)
		lastSeq = m.Seq
	}

	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return fail(exitTimeout, "watch timed out")
		case m, ok := <-live:
			if !ok {
				return fail(exitTimeout, "watch timed out")
			}
			if m.Seq > lastSeq {
				a.printEvent(m, leads)
			}
		case u, ok := <-runners:
			if !ok {
				runners = nil
				continue
			}
			if !contains(named, u.Path) || !u.Sample.Known {
				continue
			}
			if was, seen := running[u.Path]; seen && was == u.Sample.Running {
				continue
			}
			running[u.Path] = u.Sample.Running
			word := "idle"
			if u.Sample.Running {
				word = "running"
			}
			fmt.Fprintf(a.stdout, "%s %s runner %s\n", stamp(u.Sample.At, time.Now()), u.Path, word)
			continue
		case <-tick.C:
			// A state can change without an event in a watched tree
			// (the daemon marks silence), so re-read now and then.
		}
		if ok, err := allTerminal(); err != nil {
			return timedOut(err)
		} else if ok {
			return finished()
		}
	}
}

// printEvent prints a stream message in a watched tree as "<at> <path> <event> <text>".
func (a *app) printEvent(m mesh.Stored, leads map[string]bool) {
	path, event, ok := splitSubject(m.Subject)
	if !ok {
		return
	}
	var ev mesh.Event
	_ = json.Unmarshal(m.Data, &ev)
	if strings.HasPrefix(path, "daemon.") {
		to, err := mesh.ParsePath(ev.To)
		if err != nil || !leads[to.Lead()] {
			return
		}
	} else if p, err := mesh.ParsePath(path); err != nil || !leads[p.Lead()] {
		return
	}
	line := fmt.Sprintf("%s %s %s", stamp(ev.At, m.At), path, event)
	if ev.To != "" {
		line += " " + ev.To
	}
	if ev.Text != "" {
		line += " " + ev.Text
	}
	fmt.Fprintln(a.stdout, line)
}

// splitSubject turns bm.<path>.<event> into its path and event; inbox
// subjects are not events.
func splitSubject(subject string) (path, event string, ok bool) {
	rest, found := strings.CutPrefix(subject, "bm.")
	if !found || strings.HasPrefix(rest, "inbox.") {
		return "", "", false
	}
	i := strings.LastIndex(rest, ".")
	if i <= 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

func stamp(at string, fallback time.Time) string {
	if at != "" {
		return at
	}
	return fallback.UTC().Format(time.RFC3339)
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
