package daemon

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/notify"
)

// openAfter is how long an ask, a block or a finished family's feedback
// waits before the Stuck report names it, once.
const openAfter = time.Hour

// stuckItem is one thing the Stuck report names: short in the title, line
// in the body.
type stuckItem struct{ short, line string }

// Stuck is the digest: what is stuck, which no push names, or whose push
// has gone unanswered. Each thing is named once, in the pass whose window
// (since, now] holds the moment it became stuck; one after now waits for
// the next pass, so none is lost between passes:
//
//   - a node that went silent (not a routine), at its silent's At;
//   - an ask or block still open openAfter after it was pushed (a lead's,
//     not a fix lead's, a fixer's or a round's; an engineer's ask), not a
//     stop;
//   - a lead hold a whole number of days old;
//   - a family whose every node is done or stopped with feedback pending
//     (feedback, by family, leaves out what bmesh's own fixer takes)
//     openAfter after its last node finished.
//
// The title is `👀 Stuck • <host>: <first item>` with `, +N more`, the body
// a line per item. changed is false when there is nothing.
func Stuck(host string, states map[string]mesh.State, nodes []mesh.Node, holds []mesh.LeadHold, feedback map[string]int, since, now time.Time) (title, body string, changed bool) {
	byPath := map[string]mesh.Node{}
	families := map[string][]string{}
	for _, n := range nodes {
		byPath[n.Path] = n
		if n.Family != "" {
			families[n.Family] = append(families[n.Family], n.Path)
		}
	}
	var items []stuckItem
	for _, p := range slices.Sorted(maps.Keys(states)) {
		st := states[p]
		at, err := time.Parse(time.RFC3339, st.At)
		if err != nil || strings.HasPrefix(p, "routine.") {
			continue
		}
		with := func(line string) string {
			if st.Text != "" {
				line += ": " + st.Text
			}
			return line
		}
		n := byPath[p]
		switch {
		case st.State == "silent" && inWindow(at, since, now):
			items = append(items, stuckItem{p + " silent", with(p + " silent")})
		case st.State == "asking" && reachesOperator(n, "ask") && inWindow(at.Add(openAfter), since, now):
			open := age(now.Sub(at))
			items = append(items, stuckItem{p + " ask open " + open, with(p + " asks, open " + open)})
		case st.State == "blocked" && !mesh.Stopped(st) && reachesOperator(n, "blocked") && inWindow(at.Add(openAfter), since, now):
			open := age(now.Sub(at))
			items = append(items, stuckItem{p + " blocked " + open, with(p + " blocked " + open)})
		}
	}
	for _, h := range holds {
		at, err := time.Parse(time.RFC3339, h.At)
		if err != nil {
			continue
		}
		for t := at.Add(24 * time.Hour); !t.After(now); t = t.Add(24 * time.Hour) {
			if inWindow(t, since, now) {
				items = append(items, stuckItem{fmt.Sprintf("%s held %dd", h.Path, int(now.Sub(at)/(24*time.Hour))), h.Path + ": " + h.Line(now)})
				break
			}
		}
	}
	for _, f := range slices.Sorted(maps.Keys(feedback)) {
		members := families[f]
		var last time.Time
		finished := len(members) > 0 && feedback[f] > 0
		for _, p := range members {
			st := states[p]
			if st.State != "done" && !mesh.Stopped(st) {
				finished = false
			}
			if at, err := time.Parse(time.RFC3339, st.At); err == nil && at.After(last) {
				last = at
			}
		}
		if finished && inWindow(last.Add(openAfter), since, now) {
			items = append(items, stuckItem{f + " feedback waits", fmt.Sprintf("%s: %d feedback pending %s after it finished: bmesh feedback list %s",
				f, feedback[f], age(now.Sub(last)), f)})
		}
	}
	if len(items) == 0 {
		return "", "", false
	}
	subject := host + ": " + items[0].short
	if len(items) > 1 {
		subject += fmt.Sprintf(", +%d more", len(items)-1)
	}
	lines := make([]string, len(items))
	for i, it := range items {
		lines[i] = it.line
	}
	return notify.Title("👀", "Stuck", subject), strings.Join(lines, "\n"), true
}

// reachesOperator reports whether node n's event (ask or blocked) is pushed
// to the operator: a lead's (not a fix lead's), a fixer's, a round's
// blocked or an engineer's ask.
func reachesOperator(n mesh.Node, event string) bool {
	switch n.Role {
	case "lead":
		_, _, fix := mesh.FixFamily(n.Family)
		return !fix
	case "fixer":
		return true
	case "round":
		return event == "blocked"
	case "engineer":
		return event == "ask"
	}
	return false
}

// inWindow reports whether at is in (since, now].
func inWindow(at, since, now time.Time) bool { return at.After(since) && !at.After(now) }

// age renders d as 5m or 2h10m (minutes at least).
func age(d time.Duration) string {
	if d < time.Minute {
		return "0m"
	}
	return shortDuration(d)
}

// digestOnce sends the Stuck report when anything is stuck (Stuck), then
// moves *since to now. A failed delivery leaves *since alone so the next
// pass reports the same things again.
func digestOnce(ctx context.Context, d Deps, since *time.Time) {
	now := d.Now() // before the read, so the window (since, now] has no gap
	states, err := mesh.ReadAllStates(ctx, d.Client)
	if err != nil {
		d.Log.Warn("digest: read states", "err", err)
		return
	}
	nodes, err := mesh.ListNodes(ctx, d.Client)
	if err != nil {
		d.Log.Warn("digest: list nodes", "err", err)
		return
	}
	holds, err := mesh.ListLeadHolds(ctx, d.Client)
	if err != nil {
		d.Log.Warn("digest: list holds", "err", err)
	}
	all, err := mesh.ListAllFeedback(ctx, d.Client)
	if err != nil {
		d.Log.Warn("digest: list feedback", "err", err)
		return // a pass without it would move since past a family's one line
	}
	own := d.Cfg.ModuleRepo(d.Module)
	feedback := map[string]int{}
	for _, f := range all {
		if own == "" || f.Repo != own {
			feedback[f.Family]++
		}
	}
	title, body, changed := Stuck(d.Host, states, nodes, holds, feedback, *since, now)
	if !changed {
		return
	}
	if err := deliver(ctx, d, notify.Headers{Title: title}, body); err != nil {
		d.Log.Warn("digest: deliver", "err", err)
		return
	}
	ev := mesh.Event{From: d.from(), Event: "digest", Text: title, Body: body, At: now.UTC().Format(time.RFC3339)}
	if err := mesh.PublishEvent(ctx, d.Client, ev); err != nil {
		d.Log.Warn("digest: publish", "err", err)
	}
	*since = now
}

// deliver posts to [notify] (notify.Target.Post).
func deliver(ctx context.Context, d Deps, h notify.Headers, body string) error {
	return notify.Target{Ntfy: d.Cfg.Notify.Ntfy, Command: d.Cfg.Notify.Command}.Post(ctx, h, body)
}
