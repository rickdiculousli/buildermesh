// Package daemon holds the bmesh daemon's loops: it keeps each lead's runner
// sample current, wakes idle leads with unread mail, marks quiet nodes
// silent, and runs the routine kinds of the repos in [repos]. Every loop is
// its own goroutine; a failing step is logged and the loop carries on.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
	"github.com/rickdiculousli/buildermesh/internal/web"
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
	GOOS   string // the OS a routine kind's os is matched against; "" = runtime.GOOS
	Log    *slog.Logger
	Now    func() time.Time

	Checkouts string // routine instances' checkouts root ([runner] checkouts, expanded)
	ClaudeBin string // the claude command for model routines, split on spaces

	PushRetry []time.Duration // waits before each push retry; nil = 5s, 30s
	PushGrace time.Duration   // how long a push in flight may finish at shutdown; 0 = 10s

	T3Token  string // the T3 sign-in file `bmesh t3 login` writes; "" = no expiry warnings
	StateDir string // what survives a restart: the push record, the roster and salt records

	ConfigDir  string        // the config file's directory: host.key and the file store's keys
	HostsEvery time.Duration // the hosts layer's tick; 0 = a minute

	Quota *web.Quota // the quota gate the map shares; nil = one of the reminder's own from [quota] command

	Revision string // this binary's commit (vcs.revision); "" = unknown
	Release  string // the release this binary is or follows (v0.2.0, v0.2.0+3), as stamped; "" = none
	Tag      string // with no Revision, the release tag a tagged module install was built at, resolved on main by an update; "" = none
	RevTime  string // that commit's time (RFC 3339); "" = unknown
	Module   string // the module this binary was built from; "" = unknown, no upstream to read
	Exe      string // the binary's path, which [update] replaces and the fixer start runs
	Restart  func() // ends serve so the binary re-execs; called after an update drained
	// Supervised says serve runs under `bmesh serve --restart`
	// (BMESH_SUPERVISED=1), which starts the new binary after an update on
	// Windows, where there is no exec; a Windows daemon without it refuses
	// to update.
	Supervised bool

	// StartFixer starts bmesh's own fixer and returns what it printed;
	// nil runs `<Exe> fix`.
	StartFixer func(ctx context.Context) (string, error)
}

func (d *Deps) defaults() {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.GOOS == "" {
		d.GOOS = runtime.GOOS
	}
}

func (d *Deps) from() string { return "daemon." + d.Host }

// wakeItems is how many pending messages the wake text names.
const wakeItems = 5

// WakeText is the message a woken lead receives: the unread count, then what
// is waiting, read from pending (up to five, then how many more). With no
// pending messages (the read failed) it is the count alone.
func WakeText(unread int, pending []mesh.Message) string {
	var items []string
	for i, m := range pending {
		if i == wakeItems {
			items = append(items, fmt.Sprintf("and %d more", len(pending)-wakeItems))
			break
		}
		items = append(items, WakeItem(m))
	}
	head := fmt.Sprintf("bmesh: %d unread", unread)
	if len(items) > 0 {
		head += ": " + strings.Join(items, "; ")
	}
	return head + ". Run `bmesh inbox` now, then continue."
}

// ResumeText is the wake of a stopped node a message resumed: the wake
// text, headed "resumed by <who>".
func ResumeText(by string, unread int, pending []mesh.Message) string {
	return "bmesh: resumed by " + by + "; " + strings.TrimPrefix(WakeText(unread, pending), "bmesh: ")
}

// WakeItem names one pending message: a report by its event and, for done,
// branch and commit; a reply by its job and verdict; anything else by its
// kind and sender.
func WakeItem(m mesh.Message) string {
	switch {
	case m.Kind == "report" && m.Report != nil && m.Report.Event == "done":
		c := m.Report.Commit
		if len(c) > 8 {
			c = c[:8]
		}
		return fmt.Sprintf("done from %s (branch %s at %s)", m.From, m.Report.Branch, c)
	case m.Kind == "report" && m.Report != nil:
		return m.Report.Event + " from " + m.From
	case m.Kind == "reply" && m.Reply != nil:
		verdict := "ok"
		if !m.Reply.OK {
			verdict = "fail"
		}
		return fmt.Sprintf("reply to job %s (%s %s)", m.Reply.Job, m.Reply.Kind, verdict)
	case m.From == "":
		return m.Kind + " message"
	}
	return m.Kind + " from " + m.From
}

func decodeEvent(data []byte) (mesh.Event, error) {
	var ev mesh.Event
	err := json.Unmarshal(data, &ev)
	return ev, err
}

// Run publishes started (after pushing an update that never started), then
// starts the activity follower, the poll+wake+rounds and silence loops, the
// digest and push loops when [notify] has somewhere to send, the quota
// reset reminders when [quota] remind_topic is set, and, when
// [repos] names any repo, the routine loop and (with this binary's module
// known) the fixer loop, and with [hosts] enabled the hosts loop; it
// returns once ctx ends and every loop has stopped.
func Run(ctx context.Context, d Deps) error {
	if d.Client == nil || d.Cfg == nil || d.Runner == nil || d.Host == "" {
		return errors.New("daemon: Client, Cfg, Runner and Host are required")
	}
	d.defaults()
	announce(ctx, d)
	pending(ctx, d)
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
		every(ctx, pollEvery, func() { pollOnce(ctx, d, act); wakeOnce(ctx, d, act); roundsOnce(ctx, d) })
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
	if d.T3Token != "" && (d.Cfg.Notify.Ntfy != "" || d.Cfg.Notify.Command != "") {
		start(func() { every(ctx, time.Hour, func() { t3LoginOnce(ctx, d) }) })
	}
	if d.Cfg.Quota.RemindTopic != "" {
		q := d.Quota
		if q == nil {
			q = &web.Quota{Command: d.Cfg.Quota.Command}
		}
		start(func() { every(ctx, remindEvery, func() { remindOnce(ctx, d, q) }) })
	}
	if pushEnabled(d.Cfg) {
		start(func() { pushLoop(ctx, d) })
	}
	if len(d.Cfg.Repos) > 0 {
		start(func() { routines(ctx, d) })
	}
	if len(d.Cfg.Repos) > 0 && d.Module != "" {
		start(func() { fixerLoop(ctx, d, pollEvery) })
	}
	if d.Cfg.Hosts.Enabled {
		start(func() { hostsRun(ctx, d) })
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
