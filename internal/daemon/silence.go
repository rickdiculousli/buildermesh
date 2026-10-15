package daemon

import (
	"context"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// silenceOnce marks silent every working node with no event at or under its
// path within Grace, and wakes a silent lead this host runs (under the same
// once-per-WakeEvery rule) when its thread is idle.
func silenceOnce(ctx context.Context, d Deps, act *Activity) {
	states, err := mesh.ReadAllStates(ctx, d.Client)
	if err != nil {
		d.Log.Warn("silence: read states", "err", err)
		return
	}
	now := d.Now()
	for path, st := range states {
		if ctx.Err() != nil {
			return
		}
		if st.State != "working" {
			continue
		}
		last := act.Last(path)
		// The state's own time is a floor, so a node whose events are older
		// than the rebuilt history is judged from its last transition.
		if at, err := time.Parse(time.RFC3339, st.At); err == nil && at.After(last) {
			last = at
		}
		quiet := now.Sub(last)
		if quiet < d.Cfg.Liveness.Grace {
			continue
		}
		text := "quiet for " + shortDuration(quiet)
		if _, err := mesh.WriteState(ctx, d.Client, path, mesh.State{State: "silent", By: "daemon", Text: text,
			At: now.UTC().Format(time.RFC3339)}); err != nil {
			d.Log.Warn("silence: write state", "path", path, "err", err)
			continue
		}
		ev := mesh.Event{From: d.from(), Event: "silent", To: path, Text: text, At: now.UTC().Format(time.RFC3339)}
		if err := mesh.PublishEvent(ctx, d.Client, ev); err != nil {
			d.Log.Warn("silence: publish", "path", path, "err", err)
		}
		wakeSilentLead(ctx, d, act, path)
	}
}

// wakeSilentLead wakes a silent lead (or engineer) this host runs if its
// thread is idle, whatever its unread count.
func wakeSilentLead(ctx context.Context, d Deps, act *Activity, path string) {
	n, ok, err := mesh.GetNode(ctx, d.Client, path)
	if err != nil || !ok || !threaded(n) || n.Host != d.Host || n.Runner.Thread == "" {
		return
	}
	if last := act.LastWake(path); !last.IsZero() && d.Now().Sub(last) < d.Cfg.Liveness.WakeEvery {
		return
	}
	s, err := d.Runner.Status(ctx, n.Runner.Thread)
	if err != nil {
		d.Log.Warn("silence: runner status", "path", path, "err", err)
		return
	}
	if !s.Known || s.Running {
		return
	}
	p, err := mesh.ParsePath(path)
	if err != nil {
		return
	}
	unread, err := mesh.Unread(ctx, d.Client, p)
	if err != nil {
		d.Log.Warn("silence: unread", "path", path, "err", err)
		return
	}
	wake(ctx, d, act, n, unread)
}

// shortDuration prints d to the minute: "25m", "1h5m".
func shortDuration(d time.Duration) string {
	s := d.Round(time.Minute).String()
	return strings.TrimSuffix(s, "0s")
}
