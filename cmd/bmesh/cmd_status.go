package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/daemon"
	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
	"github.com/rickdiculousli/buildermesh/internal/spec"
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
	Hold   *holdRow           `json:"hold,omitempty"`  // its bm-holds entry
	Ready  *readyRow          `json:"ready,omitempty"` // its bm-ready entry
}

// readyRow is a ready request as mesh-status --json prints it.
type readyRow struct {
	Phase string `json:"phase"`
	Head  string `json:"head"`
	At    string `json:"at"`
}

// readyLine says a ready request as mesh-status prints it, after its
// path: landing 6m: <the lead's landing text>, or the wake still owed.
func readyLine(r mesh.Ready, st mesh.State, now time.Time) string {
	age := "?"
	if at, err := time.Parse(time.RFC3339, r.At); err == nil {
		age = mesh.HoldAge(now.Sub(at))
	}
	switch r.Phase {
	case mesh.PhaseLanded:
		return fmt.Sprintf("landed %s (ready %s ago); the final wake is pending", short8(r.Commit), age)
	case mesh.PhaseRefused:
		return fmt.Sprintf("land refused (ready %s ago): %s; the wake carrying it is pending", age, r.Refusal)
	case mesh.PhaseCancelled:
		return fmt.Sprintf("landing %s: cancelled, the daemon ends it", age)
	}
	s := "landing " + age
	if st.State == "landing" && st.Text != "" {
		s += ": " + st.Text
	} else if st.State == "held" {
		s += ": held, waiting for the release"
	}
	return s
}

// cmdMeshStatus prints the registered nodes with their state, runner and
// unread count: every one with --all or --json, else all but those done for
// longer than [status] keep_done, which a closing line counts; then a line
// per hold, with its age, a line per ready request, with its age, the
// feedback not yet drained (feedbackLines),
// and last a line per landed spec with steps for the operator still open.
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
	holds, err := mesh.ListLeadHolds(ctx, a.client)
	if err != nil {
		return err
	}
	holdOf := map[string]holdRow{}
	for _, r := range holdRows(holds) {
		holdOf[r.Path] = r
	}
	readies, err := mesh.ListReady(ctx, a.client)
	if err != nil {
		return err
	}
	readyOf := map[string]readyRow{}
	for _, r := range readies {
		readyOf[r.Path] = readyRow{Phase: r.Phase, Head: r.Head, At: r.At}
	}
	rows := make([]statusRow, 0, len(nodes))
	for _, n := range nodes {
		r := statusRow{Node: n, State: mesh.State{State: "idle"}}
		if h, ok := holdOf[n.Path]; ok {
			r.Hold = &h
		}
		if rr, ok := readyOf[n.Path]; ok {
			r.Ready = &rr
		}
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
	now := time.Now()
	for _, h := range holds {
		fmt.Fprintf(a.stdout, "%s %s\n", h.Path, h.Line(now))
	}
	for _, r := range readies {
		fmt.Fprintf(a.stdout, "%s %s\n", r.Path, readyLine(r, states[r.Path], now))
	}
	for _, l := range a.boxHoldLines(ctx, now) {
		fmt.Fprintln(a.stdout, l)
	}
	if err := a.feedbackLines(ctx, nodes, states); err != nil {
		return err
	}
	if a.cfg.Runner.Kind == "t3" {
		if tok, err := t3mcp.ReadToken(a.cfg.T3TokenPath()); err == nil && tok.Left(time.Now()) < 7*24*time.Hour {
			host := a.cfg.HostName()
			if n := tok.DaysLeft(time.Now()); n == 0 {
				fmt.Fprintf(a.stdout, "t3 sign-in on %s: expired (bmesh t3 login)\n", host)
			} else if n == 1 {
				fmt.Fprintf(a.stdout, "t3 sign-in on %s: expires in 1 day (bmesh t3 login)\n", host)
			} else {
				fmt.Fprintf(a.stdout, "t3 sign-in on %s: expires in %d days (bmesh t3 login)\n", host, n)
			}
		}
	}
	if line := selfupdate.ReadNotice(config.StateDir()); line != "" {
		fmt.Fprintln(a.stdout, line)
	}
	a.hubLine()
	repos := a.cfg.RepoNames()
	sort.Strings(repos)
	for _, o := range a.openSteps(ctx, repos) {
		fmt.Fprintln(a.stdout, o.Line())
	}
	return nil
}

// boxHoldLines are the holds every box's presence carries (093), one line
// each as waiting on the operator: `<host>: <repo> waits for your approval
// (+<N> commits): in a terminal on <host>, bmesh hosts approve <id>
// (expires <day hh:mm>)`. None with [hosts] off or before this box joined;
// a presence read that fails is said on stderr.
func (a *app) boxHoldLines(ctx context.Context, now time.Time) []string {
	if !a.cfg.Hosts.Enabled {
		return nil
	}
	local, ok, err := hosts.LoadLocal(config.StateDir())
	if err != nil || !ok {
		return nil
	}
	kv, err := hosts.Bucket(ctx, a.client.JS, hosts.BucketHosts)
	if err == nil {
		var seen []hosts.Seen
		if seen, err = hosts.ReadPresence(ctx, kv, local.Roster, now); err == nil {
			var out []string
			for _, s := range seen {
				for _, x := range s.Presence.Held {
					out = append(out, fmt.Sprintf("%s: in a terminal on %s, bmesh hosts approve %s (expires %s)",
						x.Waits(s.Host.Name), s.Host.Name, x.ID, x.ExpiresAt()))
				}
			}
			return out
		}
	}
	fmt.Fprintf(a.stderr, "bmesh: holds: %v\n", err)
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

// drainNext is what watch-done adds for the build families of the watched
// leads that have feedback pending (100), a fixer's family left out: "; next: bmesh feedback list
// <family>", one per family, "" for none. A node or a queue it can't read
// adds nothing.
func (a *app) drainNext(ctx context.Context, paths []string) string {
	var next []string
	for _, p := range paths {
		n, ok, err := mesh.GetNode(ctx, a.client, p)
		if err != nil || !ok || n.Role != "lead" || n.Family == "" || a.fixerFamily(ctx, n.Family) {
			continue
		}
		cmd := "bmesh feedback list " + n.Family
		if slices.Contains(next, cmd) {
			continue
		}
		if fb, err := mesh.ListFeedback(ctx, a.client, n.Family); err == nil && len(fb) > 0 {
			next = append(next, cmd)
		}
	}
	if len(next) == 0 {
		return ""
	}
	return "; next: " + strings.Join(next, ", then ")
}

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
		fmt.Fprintln(a.stdout, "watch-done"+a.drainNext(ctx, named))
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

// splitSubject turns bm.<path>.<event> into its path and event. Daemon
// events split as daemon.<host> and <event>; every other mesh-internal
// subject (mesh.IsMeshInternal), host traffic included, is not an event.
func splitSubject(subject string) (path, event string, ok bool) {
	rest, found := strings.CutPrefix(subject, "bm.")
	if !found || mesh.IsMeshInternal(subject) && !strings.HasPrefix(rest, "daemon.") {
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

// feedbackLines prints the feedback not yet drained: the open issues naming
// bmesh's own repo (spec.OpenIssues) as one line, `fixes: N pending for
// <repo> (<fixer> <state>)` with the fixer that has them, `(starts within
// a minute)` when the daemon starts one on its next pass, `(<fixer>
// blocked)` while the newest fixer's block waits on the operator, or
// `(<fixer> done; they wait on you)` when it had them and left them; then
// every other issue as `feedback: N pending in <family>` per family.
func (a *app) feedbackLines(ctx context.Context, nodes []mesh.Node, states map[string]mesh.State) error {
	all, err := mesh.ListAllFeedback(ctx, a.client)
	if err != nil {
		return err
	}
	own := a.ownRepo()
	var families []string
	counts := map[string]int{}
	var mine []mesh.Feedback
	for _, f := range all {
		if own != "" && f.Repo == own {
			mine = append(mine, f)
			continue
		}
		if counts[f.Family] == 0 {
			families = append(families, f.Family)
		}
		counts[f.Family]++
	}
	if len(mine) > 0 {
		_, repoPath, err := a.cfg.RepoPath(own)
		if err != nil {
			return err
		}
		specs, err := spec.ScanRef(repoPath, a.cfg.Spec.Glob, "main")
		if err != nil {
			return ledgerFail(err)
		}
		fixed, err := spec.FixedSeqs(repoPath, "main")
		if err != nil {
			return err
		}
		if open := spec.OpenIssues(mine, own, specs, fixed); len(open) > 0 {
			fx := mesh.ReadFixers(nodes, states, nil, own)
			who := "starts within a minute"
			switch {
			case fx.Live.Path != "":
				who = fx.Live.Path + " " + cmp.Or(fx.LiveState.State, "idle")
			case fx.LastState.State == "blocked":
				who = fx.Last.Path + " blocked"
			case !daemon.FixerDue(fx, open):
				who = fx.Last.Path + " done; they wait on you"
			}
			fmt.Fprintf(a.stdout, "fixes: %d pending for %s (%s)\n", len(open), own, who)
		}
	}
	sort.Strings(families)
	for _, f := range families {
		fmt.Fprintf(a.stdout, "feedback: %d pending in %s\n", counts[f], f)
	}
	return nil
}
