package daemon

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

// wakeMu serializes wake decisions, so the poll and silence loops can't both
// pass the once-per-WakeEvery check for one lead before either records it.
var wakeMu sync.Mutex

// wakeOnce wakes every local lead whose last runner sample is idle, which is
// not done or blocked, has unread mail and was not woken within WakeEvery.
func wakeOnce(ctx context.Context, d Deps, act *Activity) {
	leads, err := localLeads(ctx, d)
	if err != nil {
		d.Log.Warn("wake: list nodes", "err", err)
		return
	}
	for _, n := range leads {
		if ctx.Err() != nil {
			return
		}
		s, ok, err := mesh.GetRunner(ctx, d.Client, n.Path)
		if err != nil || !ok || !s.Known || s.Running {
			continue
		}
		st, _, err := mesh.ReadState(ctx, d.Client, n.Path)
		if err != nil {
			d.Log.Warn("wake: read state", "path", n.Path, "err", err)
			continue
		}
		if st.State == "done" || st.State == "blocked" {
			continue
		}
		p, err := mesh.ParsePath(n.Path)
		if err != nil {
			continue
		}
		unread, err := mesh.Unread(ctx, d.Client, p)
		if err != nil {
			d.Log.Warn("wake: unread", "path", n.Path, "err", err)
			continue
		}
		if unread == 0 {
			continue
		}
		wake(ctx, d, act, n, unread)
	}
}

// wake sends the wake text to n's thread unless it was woken within
// WakeEvery, and on delivery records the wake as a daemon event. A thread
// found running (ErrRunning) is not delivered: nothing is recorded, and the
// next poll tries again. It reports whether the wake was delivered.
func wake(ctx context.Context, d Deps, act *Activity, n mesh.Node, unread int) bool {
	wakeMu.Lock()
	defer wakeMu.Unlock()
	now := d.Now()
	if last := act.LastWake(n.Path); !last.IsZero() && now.Sub(last) < d.Cfg.Liveness.WakeEvery {
		return false
	}
	text := WakeText(unread)
	// Runner.Send reads Status itself and refuses a running thread.
	err := d.Runner.Send(ctx, n.Runner.Thread, text)
	if errors.Is(err, runner.ErrRunning) {
		d.Log.Info("wake: not delivered, thread is running", "path", n.Path, "thread", n.Runner.Thread)
		return false
	}
	if err != nil {
		d.Log.Warn("wake: send", "path", n.Path, "thread", n.Runner.Thread, "err", err)
		return false
	}
	act.ObserveWake(n.Path, now)
	ev := mesh.Event{From: d.from(), Event: "wake", To: n.Path, Text: text, At: now.UTC().Format(time.RFC3339)}
	if err := mesh.PublishEvent(ctx, d.Client, ev); err != nil {
		d.Log.Warn("wake: publish", "path", n.Path, "err", err)
	}
	return true
}
