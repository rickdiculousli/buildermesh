package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

// settleErrs holds each thread's last logged settle failure, so a failure
// is logged once per change, not every pass.
var settleErrs sync.Map // thread -> string

// SettleUnit is what a node's thread is settled with: an engineer with its
// round, a lead or fixer with its build family (a lead with none alone);
// "" for a node bmesh started no thread for that settles (a round itself,
// an executor, a routine).
func SettleUnit(n mesh.Node) string {
	switch n.Role {
	case "engineer":
		return n.Parent
	case "lead", "fixer":
		return cmp.Or(n.Family, n.Path)
	}
	return ""
}

// SettledText is the settled line's text: "<unit>: <n> threads".
func SettledText(unit string, n int) string {
	if n == 1 {
		return unit + ": 1 thread"
	}
	return fmt.Sprintf("%s: %d threads", unit, n)
}

// unitFinished reports a unit whose threads settle: every member done or
// stopped with no landing request left (its closing-line wake delivered),
// and a round unit's round itself done. Anything else, a member blocked,
// held, asking, silent or still landing, is what the operator still reads.
func unitFinished(unit string, members []mesh.Node, rounds map[string]bool, states map[string]mesh.State, ready map[string]bool) bool {
	if rounds[unit] && states[unit].State != "done" {
		return false
	}
	for _, m := range members {
		if s := states[m.Path]; s.State != "done" && !mesh.Stopped(s) || ready[m.Path] {
			return false
		}
	}
	return true
}

// settleOnce settles, on a T3 runner, the threads this box runs of every
// finished unit (unitFinished): each once its runner reads idle (its
// closing turn ended), marked on its node so no pass settles it again.
// A pass that settled any publishes bm.daemon.<host>.settled, text
// SettledText with every thread of the unit settled on this box. A
// thread T3 can't settle is logged once and tried again next pass.
func settleOnce(ctx context.Context, d Deps) {
	st, ok := d.Runner.(runner.Settler)
	if !ok {
		return
	}
	nodes, err := mesh.ListNodes(ctx, d.Client)
	if err != nil {
		d.Log.Warn("settle: list nodes", "err", err)
		return
	}
	states, err := mesh.ReadAllStates(ctx, d.Client)
	if err != nil {
		d.Log.Warn("settle: read states", "err", err)
		return
	}
	readies, err := mesh.ListReady(ctx, d.Client)
	if err != nil {
		d.Log.Warn("settle: list ready", "err", err)
		return
	}
	ready := map[string]bool{}
	for _, r := range readies {
		ready[r.Path] = true
	}
	rounds := map[string]bool{}
	units := map[string][]mesh.Node{}
	for _, n := range nodes {
		if n.Role == "round" {
			rounds[n.Path] = true
		}
		if u := SettleUnit(n); u != "" {
			units[u] = append(units[u], n)
		}
	}
	for _, u := range slices.Sorted(maps.Keys(units)) {
		if ctx.Err() != nil {
			return
		}
		members := units[u]
		if !unitFinished(u, members, rounds, states, ready) {
			continue
		}
		var pending []mesh.Node
		total := 0
		for _, m := range members {
			if m.Host != d.Host || m.Runner.Kind != "t3" || m.Runner.Thread == "" {
				continue
			}
			if m.Settled != "" {
				total++
				continue
			}
			pending = append(pending, m)
		}
		now := 0
		for _, m := range pending {
			if settleThread(ctx, d, st, m) {
				now++
			}
		}
		if now == 0 {
			continue
		}
		total += now
		ev := mesh.Event{From: d.from(), Event: "settled", To: members[0].Path, Text: SettledText(u, total),
			At: d.Now().UTC().Format(time.RFC3339)}
		if _, err := mesh.PublishEventID(ctx, d.Client, ev, fmt.Sprintf("settled-%s-%s-%d", u, d.Host, total)); err != nil {
			d.Log.Warn("settle: publish", "unit", u, "err", err)
		}
		d.Log.Info("settle: settled " + ev.Text)
	}
}

// settleThread settles m's thread once its runner reads idle and marks
// its node settled; a thread T3 no longer has counts as settled. It
// reports whether the node is marked now.
func settleThread(ctx context.Context, d Deps, st runner.Settler, m mesh.Node) bool {
	thread := m.Runner.Thread
	s, err := d.Runner.Status(ctx, thread)
	if err == nil && s.Running {
		return false
	}
	if err == nil {
		err = st.Settle(ctx, thread)
	}
	if err != nil && !errors.Is(err, runner.ErrNoThread) {
		if prev, _ := settleErrs.Swap(thread, err.Error()); prev != err.Error() {
			d.Log.Warn("settle: thread", "path", m.Path, "thread", thread, "err", err)
		}
		return false
	}
	settleErrs.Delete(thread)
	cur, ok, err := mesh.GetNode(ctx, d.Client, m.Path)
	if err != nil || !ok || cur.Runner.Thread != thread {
		return false
	}
	cur.Settled = d.Now().UTC().Format(time.RFC3339)
	if err := mesh.PutNode(ctx, d.Client, cur); err != nil {
		d.Log.Warn("settle: put node", "path", m.Path, "err", err)
		return false
	}
	return true
}

// unmarkSettled drops n's settled mark once a wake reached its thread (a
// message unsettles a T3 thread), so the settle pass settles it again when
// the turn the wake started ends.
func unmarkSettled(ctx context.Context, d Deps, n mesh.Node) {
	cur, ok, err := mesh.GetNode(ctx, d.Client, n.Path)
	if err != nil || !ok || cur.Settled == "" || cur.Runner.Thread != n.Runner.Thread {
		return
	}
	cur.Settled = ""
	if err := mesh.PutNode(ctx, d.Client, cur); err != nil {
		d.Log.Warn("settle: unmark", "path", n.Path, "err", err)
	}
}
