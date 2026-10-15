package daemon

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// Activity is the newest event time per node path and the newest wake sent
// per lead, rebuilt from stream history on start and kept current from the
// live stream.
type Activity struct {
	mu   sync.Mutex
	last map[string]time.Time
	wake map[string]time.Time
}

func NewActivity() *Activity {
	return &Activity{last: map[string]time.Time{}, wake: map[string]time.Time{}}
}

// Rebuild observes every stored bm.> message newer than since.
func (a *Activity) Rebuild(ctx context.Context, c *mesh.Client, since time.Time) error {
	hist, err := mesh.History(ctx, c, "bm.>", since)
	if err != nil {
		return err
	}
	for _, s := range hist {
		a.ObserveStored(s)
	}
	return nil
}

// ObserveStored observes a stream message: a daemon wake event records a wake
// for its "to"; anything else goes through Observe.
func (a *Activity) ObserveStored(s mesh.Stored) {
	if strings.HasPrefix(s.Subject, "bm.daemon.") && strings.HasSuffix(s.Subject, ".wake") {
		if ev, err := decodeEvent(s.Data); err == nil && ev.To != "" {
			a.ObserveWake(ev.To, s.At)
		}
		return
	}
	a.Observe(s.Subject, s.At)
}

// Observe records a node event bm.<path>.<event> at at. Inbox and daemon
// subjects are not node activity and are ignored.
func (a *Activity) Observe(subject string, at time.Time) {
	rest, ok := strings.CutPrefix(subject, "bm.")
	if !ok || strings.HasPrefix(rest, "inbox.") || strings.HasPrefix(rest, "daemon.") {
		return
	}
	i := strings.LastIndexByte(rest, '.')
	if i <= 0 {
		return
	}
	path := rest[:i]
	a.mu.Lock()
	defer a.mu.Unlock()
	if at.After(a.last[path]) {
		a.last[path] = at
	}
}

// ObserveWake records a wake sent to path at at.
func (a *Activity) ObserveWake(path string, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if at.After(a.wake[path]) {
		a.wake[path] = at
	}
}

// Last is the newest event at or under path (path itself or any path that
// starts with path + "."), zero when there is none.
func (a *Activity) Last(path string) time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	var newest time.Time
	for q, at := range a.last {
		if (q == path || strings.HasPrefix(q, path+".")) && at.After(newest) {
			newest = at
		}
	}
	return newest
}

// LastWake is the newest wake sent to path, zero when there is none.
func (a *Activity) LastWake(path string) time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.wake[path]
}
