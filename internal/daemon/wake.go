package daemon

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

// wakeMu serializes wake decisions, so the poll and silence loops can't both
// pass the once-per-WakeEvery check for one lead before either records it.
var wakeMu sync.Mutex

// wakeOnce wakes every local lead whose last runner sample is idle, has
// unread mail and was not woken within WakeEvery. A done or blocked lead is
// woken only when that mail holds a message sent to it (bmesh send), so the
// operator can ask a finished lead something; late reports and replies don't
// wake it. A stopped lead (mesh.Stopped) is woken only by a message from
// outside its own tree (resumer), and the wake says who resumed it. A
// thread the user stopped in its runner is never woken here: the poll marks
// it stopped.
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
		st, _, err := mesh.ReadState(ctx, d.Client, n.Path)
		if err != nil {
			d.Log.Warn("wake: read state", "path", n.Path, "err", err)
			continue
		}
		finished := st.State == "done" || st.State == "blocked"
		if !finished {
			s, ok, err := mesh.GetRunner(ctx, d.Client, n.Path)
			if err != nil || !ok || !s.Known || s.Running || s.Stopped {
				continue
			}
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
		resumedBy := ""
		switch {
		case mesh.Stopped(st):
			if resumedBy = resumer(ctx, d, p); resumedBy == "" {
				continue
			}
		case finished && !hasMessage(ctx, d, p):
			continue
		}
		// A finished lead is not polled, so its runner is read here.
		if finished {
			s, err := d.Runner.Status(ctx, n.Runner.Thread)
			if err != nil || !s.Known || s.Running {
				continue
			}
		}
		wake(ctx, d, act, n, unread, resumedBy)
	}
}

// hasMessage reports whether p's unread mail holds a `message` (bmesh send).
func hasMessage(ctx context.Context, d Deps, p mesh.Path) bool {
	pending, err := mesh.Pending(ctx, d.Client, p)
	if err != nil {
		d.Log.Warn("wake: pending", "path", p.String(), "err", err)
		return false
	}
	for _, m := range pending {
		if m.Kind == "message" {
			return true
		}
	}
	return false
}

// resumer is the sender of the oldest unread `message` (bmesh send) in p's
// mail from outside p's own tree, "" when there is none: an executor's ask
// doesn't resume its stopped lead.
func resumer(ctx context.Context, d Deps, p mesh.Path) string {
	pending, err := mesh.Pending(ctx, d.Client, p)
	if err != nil {
		d.Log.Warn("wake: pending", "path", p.String(), "err", err)
		return ""
	}
	for _, m := range pending {
		if m.Kind == "message" && m.From != "" && m.From != p.String() && !strings.HasPrefix(m.From, p.String()+".") {
			return m.From
		}
	}
	return ""
}

// wake sends the wake text (the count and what waits, read from the
// stream; ResumeText when resumedBy names who resumed a stopped node) to
// n's thread unless it was woken within WakeEvery, and on delivery records the wake as a daemon event. A thread
// found running (ErrRunning) is not delivered: nothing is recorded, and the
// next poll tries again. It reports whether the wake was delivered.
func wake(ctx context.Context, d Deps, act *Activity, n mesh.Node, unread int, resumedBy string) bool {
	wakeMu.Lock()
	defer wakeMu.Unlock()
	now := d.Now()
	if last := act.LastWake(n.Path); !last.IsZero() && now.Sub(last) < d.Cfg.Liveness.WakeEvery {
		return false
	}
	// Read only once the wake is due, so the stream is read at most once
	// per WakeEvery; a failed read leaves the count alone.
	var pending []mesh.Message
	if p, err := mesh.ParsePath(n.Path); err == nil {
		if pending, err = mesh.Pending(ctx, d.Client, p); err != nil {
			d.Log.Warn("wake: pending", "path", n.Path, "err", err)
			pending = nil
		}
	}
	text := WakeText(unread, pending)
	if resumedBy != "" {
		text = ResumeText(resumedBy, unread, pending)
	}
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
