// Package daemon holds the bmesh daemon's loops: it keeps each lead's runner
// sample current, wakes idle leads with unread mail, marks quiet nodes
// silent, and runs the routine kinds of the repos in [repos]. Every loop is its own goroutine; a failing step is logged and the
// loop carries on.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

const (
	silenceEvery = time.Minute
	historyAge   = 24 * time.Hour // how far back Activity is rebuilt on start
	followRetry  = time.Minute
	defaultPoll  = 60 * time.Second
)

// Deps is everything the loops need.
type Deps struct {
	Client *mesh.Client
	Cfg    *config.Config
	Runner runner.Runner
	Host   string
	Log    *slog.Logger
	Now    func() time.Time

	Checkouts string // routine instances' checkouts root ([runner] checkouts, expanded)
	ClaudeBin string // the claude command for model routines, split on spaces
}

func (d *Deps) defaults() {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.Now == nil {
		d.Now = time.Now
	}
}

func (d *Deps) from() string { return "daemon." + d.Host }

// WakeText is the message a woken lead receives.
func WakeText(unread int) string {
	return fmt.Sprintf("bmesh: you have %d unread messages. Run `bmesh inbox` now, then continue.", unread)
}

func decodeEvent(data []byte) (mesh.Event, error) {
	var ev mesh.Event
	err := json.Unmarshal(data, &ev)
	return ev, err
}

// Run starts the activity follower, the poll+wake and silence loops and,
// when [repos] names any repo, the routine loop; it returns once ctx ends
// and every loop has stopped.
func Run(ctx context.Context, d Deps) error {
	if d.Client == nil || d.Cfg == nil || d.Runner == nil || d.Host == "" {
		return errors.New("daemon: Client, Cfg, Runner and Host are required")
	}
	d.defaults()
	act := NewActivity()
	pollEvery := d.Cfg.Liveness.PollEvery
	if pollEvery <= 0 {
		pollEvery = defaultPoll
	}

	var wg sync.WaitGroup
	start := func(f func()) {
		wg.Add(1)
		go func() { defer wg.Done(); f() }()
	}
	// The follower rebuilds from history once it is subscribed, so the loops
	// wait for that before their first pass; otherwise a restart would see an
	// empty Activity and mark everyone silent.
	ready := make(chan struct{})
	start(func() { follow(ctx, d, act, ready) })
	select {
	case <-ready:
	case <-ctx.Done():
	}
	start(func() {
		every(ctx, pollEvery, func() { pollOnce(ctx, d, act); wakeOnce(ctx, d, act) })
	})
	start(func() { every(ctx, silenceEvery, func() { silenceOnce(ctx, d, act) }) })
	if d.Cfg.Notify.Ntfy != "" || d.Cfg.Notify.Command != "" {
		digestEvery := d.Cfg.Notify.DigestEvery
		if digestEvery <= 0 {
			digestEvery = time.Hour
		}
		since := d.Now()
		start(func() { every(ctx, digestEvery, func() { digestOnce(ctx, d, &since) }) })
	}
	if len(d.Cfg.Repos) > 0 {
		start(func() { routines(ctx, d) })
	}
	<-ctx.Done()
	wg.Wait()
	return nil
}

// every runs f once now, then on each tick until ctx ends.
func every(ctx context.Context, interval time.Duration, f func()) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		f()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// follow keeps act current: it subscribes to bm.> live, then rebuilds from
// the last 24 hours of history (subscribing first leaves no gap; Observe is
// a max, so overlap is harmless), closes ready, and observes live events
// until ctx ends. A failed subscription or rebuild is retried every minute.
func follow(ctx context.Context, d Deps, act *Activity, ready chan<- struct{}) {
	signalled := false
	signal := func() {
		if !signalled {
			signalled = true
			close(ready)
		}
	}
	defer signal()
	for ctx.Err() == nil {
		sub, cancel := context.WithCancel(ctx)
		ch, err := mesh.Follow(sub, d.Client, "bm.>")
		if err == nil {
			err = act.Rebuild(ctx, d.Client, d.Now().Add(-historyAge))
		}
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return
			}
			d.Log.Warn("activity: follow failed", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(followRetry):
			}
			continue
		}
		signal()
		for s := range ch {
			act.ObserveStored(s)
		}
		cancel()
	}
}
