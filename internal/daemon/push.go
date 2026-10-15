package daemon

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/web"
)

// pushBodyMax is where a push's text is cut: ntfy's message limit is 4 KB.
const pushBodyMax = 3 << 10

// defaultPushRetry is the wait before each retry of a failed push.
var defaultPushRetry = []time.Duration{5 * time.Second, 30 * time.Second}

// defaultPushGrace is how long the push in flight may finish at shutdown.
const defaultPushGrace = 10 * time.Second

// Headers are a notification's ntfy headers; empty ones are not sent.
type Headers struct {
	Title, Priority, Tags, Click, Actions string
}

// PushRequest is one push: its headers and body.
type PushRequest struct {
	Headers
	Body string
}

var pushKinds = map[string]struct{ title, priority, tags string }{
	"ask":     {"%s asks", "high", "question"},
	"blocked": {"%s is blocked", "high", "no_entry"},
	"done":    {"%s is done", "default", "white_check_mark"},
}

// Push builds the push for node event ev of node n (ev.From is n's path).
// ok is false unless n is a lead, a round or (for ask) an engineer, and
// ev.Event is listed in cfg.Push. console is the thread link ("" = no
// action button): the lead's or engineer's, or for a round its eng child's. A round's done is titled `<repo> round N
// proposed` and carries the merged pitch; its blocked, `<repo> round N
// blocked`.
func Push(ev mesh.Event, n mesh.Node, cfg config.NotifyConfig, console string) (req PushRequest, ok bool) {
	k, known := pushKinds[ev.Event]
	if (n.Role != "lead" && n.Role != "round" && n.Role != "engineer") || !known || !slices.Contains(cfg.Push, ev.Event) {
		return PushRequest{}, false
	}
	if n.Role == "engineer" && ev.Event != "ask" {
		return PushRequest{}, false // its done and blocked reach the operator through the round
	}
	req.Title = fmt.Sprintf(k.title, ev.From)
	req.Priority, req.Tags = k.priority, k.tags
	if n.Role == "round" {
		if ev.Event != "done" && ev.Event != "blocked" {
			return PushRequest{}, false
		}
		word := "proposed"
		if ev.Event == "blocked" {
			word = "blocked"
		}
		req.Title = fmt.Sprintf("%s round %s %s", n.Repo, roundNumber(n.Path), word)
		if ev.Event == "done" {
			req.Tags = "memo"
		}
	} else if cfg.MapURL != "" {
		req.Click = cfg.MapURL + "#/lead/" + ev.From
	}
	if console != "" {
		req.Actions = "view, Open thread, " + console
	}
	req.Body = cutUTF8(ev.Text, pushBodyMax)
	if ev.Event == "done" && ev.Commit != "" && n.Role != "round" {
		sha := ev.Commit
		if len(sha) > 8 {
			sha = sha[:8]
		}
		if req.Body != "" {
			req.Body += "\n"
		}
		req.Body += "commit " + sha
	}
	return req, true
}

// roundNumber is the N of a round path <repo>-round-<N> ("?" otherwise).
func roundNumber(path string) string {
	i := strings.LastIndex(path, "-round-")
	if i < 0 {
		return "?"
	}
	return path[i+len("-round-"):]
}

// cutUTF8 cuts s to at most n bytes without splitting a rune.
func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func pushEnabled(n config.NotifyConfig) bool {
	return (n.Ntfy != "" || n.Command != "") && len(n.Push) > 0
}

// nodeEvent splits a node event subject bm.<path>.<event>; inbox and daemon
// subjects are not node events.
func nodeEvent(subject string) (path, event string, ok bool) {
	rest, ok := strings.CutPrefix(subject, "bm.")
	if !ok || strings.HasPrefix(rest, "inbox.") || strings.HasPrefix(rest, "daemon.") {
		return "", "", false
	}
	i := strings.LastIndexByte(rest, '.')
	if i <= 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

// pushJob is a claimed push waiting for delivery.
type pushJob struct {
	req  PushRequest
	path string
	seq  uint64
}

// pushLoop pushes a lead's (and a round's) listed events as they happen,
// once each across restarts and daemons. It subscribes to bm.> live, then catches up from
// history (events newer than [notify] catch_up with no push claim yet) and
// skips live events the history already held; a live frame stored over a
// minute ago (a reconnect's backlog) gets the same catch-up rules. Each event is claimed by a
// push daemon event with Nats-Msg-Id push-<seq> before delivery; a
// duplicate reply means someone else has it. Deliveries run in order on
// their own goroutine so a slow or failing ntfy never stalls the follower.
// At shutdown the delivery in hand gets d.PushGrace to finish; every
// claimed push left undelivered is logged and recorded as push-failed with
// body "shutdown" (the claim stands).
func pushLoop(ctx context.Context, d Deps) {
	if !pushEnabled(d.Cfg.Notify) {
		return
	}
	d.defaults()
	grace := d.PushGrace
	if grace == 0 {
		grace = defaultPushGrace
	}
	// dctx is what a delivery runs under: it outlives ctx by grace.
	dctx, dcancel := context.WithCancel(context.WithoutCancel(ctx))
	defer dcancel()
	stopGrace := context.AfterFunc(ctx, func() {
		t := time.AfterFunc(grace, dcancel)
		<-dctx.Done()
		t.Stop()
	})
	defer stopGrace()
	jobs := make(chan pushJob, 256)
	worker := make(chan struct{})
	go func() {
		defer close(worker)
		for {
			select {
			case <-ctx.Done():
				return
			case j := <-jobs:
				if ctx.Err() != nil {
					pushShutdown(d, j)
					continue
				}
				pushDeliver(ctx, dctx, d, j)
			}
		}
	}()
	defer func() {
		<-worker
		for {
			select {
			case j := <-jobs:
				pushShutdown(d, j)
			default:
				return
			}
		}
	}()
	for ctx.Err() == nil {
		sub, cancel := context.WithCancel(ctx)
		ch, err := mesh.Follow(sub, d.Client, "bm.>")
		var last uint64
		if err == nil {
			last, err = pushCatchUp(ctx, d, jobs)
		}
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return
			}
			d.Log.Warn("push: follow failed", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(followRetry):
			}
			continue
		}
		for s := range ch {
			if s.Seq <= last {
				continue // the catch-up had it
			}
			pushLive(ctx, d, s, jobs)
		}
		cancel()
	}
}

// liveStale is how old a live frame's stored time may be before it counts
// as backlog: after a reconnect, Follow delivers what was published during
// the gap as live, and another daemon may have pushed it meanwhile.
const liveStale = time.Minute

// pushLive claims live frame s when it is a listed node event. A frame
// stored over liveStale ago goes through the catch-up rules first: it is
// dropped when older than [notify] catch_up or when a daemon has claimed it.
func pushLive(ctx context.Context, d Deps, s mesh.Stored, jobs chan<- pushJob) {
	path, event, ok := nodeEvent(s.Subject)
	if !ok || !slices.Contains(d.Cfg.Notify.Push, event) {
		return
	}
	if now := d.Now(); now.Sub(s.At) > liveStale {
		if eventTime(s).Before(now.Add(-d.Cfg.Notify.CatchUp)) {
			return
		}
		claimed, err := pushClaims(ctx, d, s.At)
		if err != nil {
			d.Log.Warn("push: read claims", "path", path, "seq", s.Seq, "err", err)
			return
		}
		if claimed[s.Seq] {
			return
		}
	}
	pushClaim(ctx, d, s, path, event, jobs)
}

// eventTime is when node event s happened: its at, else its stored time.
func eventTime(s mesh.Stored) time.Time {
	if ev, err := decodeEvent(s.Data); err == nil {
		if t, err := time.Parse(time.RFC3339, ev.At); err == nil {
			return t
		}
	}
	return s.At
}

// pushClaims returns the seqs named by every daemon's push claims stored
// since since (all of them when zero).
func pushClaims(ctx context.Context, d Deps, since time.Time) (map[uint64]bool, error) {
	hist, err := mesh.History(ctx, d.Client, "bm.daemon.*.push", since)
	if err != nil {
		return nil, err
	}
	claimed := map[uint64]bool{}
	for _, s := range hist {
		if ev, err := decodeEvent(s.Data); err == nil && ev.Seq != 0 {
			claimed[ev.Seq] = true
		}
	}
	return claimed, nil
}

// pushCatchUp claims the listed node events in history no older than
// [notify] catch_up (by their at, else the stored time) that no daemon has
// claimed, in stream order, and returns the last history sequence.
func pushCatchUp(ctx context.Context, d Deps, jobs chan<- pushJob) (last uint64, err error) {
	cutoff := d.Now().Add(-d.Cfg.Notify.CatchUp)
	hist, err := mesh.History(ctx, d.Client, "bm.>", cutoff)
	if err != nil {
		return 0, err
	}
	claimed, err := pushClaims(ctx, d, time.Time{})
	if err != nil {
		return 0, err
	}
	for _, s := range hist {
		last = max(last, s.Seq)
		path, event, ok := nodeEvent(s.Subject)
		if !ok || claimed[s.Seq] || !slices.Contains(d.Cfg.Notify.Push, event) {
			continue
		}
		if eventTime(s).Before(cutoff) {
			continue
		}
		pushClaim(ctx, d, s, path, event, jobs)
	}
	return last, nil
}

// pushClaim builds the push for stored node event s, claims it and queues
// it for delivery; non-leads, a lost claim race and failures are skipped
// (failures logged).
func pushClaim(ctx context.Context, d Deps, s mesh.Stored, path, event string, jobs chan<- pushJob) {
	ev, err := decodeEvent(s.Data)
	if err != nil {
		d.Log.Warn("push: decode", "subject", s.Subject, "err", err)
		return
	}
	ev.From, ev.Event = path, event
	n, ok, err := mesh.GetNode(ctx, d.Client, path)
	if err != nil {
		d.Log.Warn("push: node", "path", path, "err", err)
		return
	}
	if !ok {
		return
	}
	linked := n
	if n.Role == "round" {
		// The action opens the engineer's thread: the operator replies there.
		if eng, ok, err := mesh.GetNode(ctx, d.Client, path+".eng"); err == nil && ok {
			linked = eng
		}
	}
	req, ok := Push(ev, n, d.Cfg.Notify, web.ConsoleURL(d.Cfg.Console, d.Host, linked))
	if !ok {
		return
	}
	claim := mesh.Event{From: d.from(), Event: "push", To: path, Text: req.Title, Seq: s.Seq,
		At: d.Now().UTC().Format(time.RFC3339)}
	dup, err := mesh.PublishEventID(ctx, d.Client, claim, fmt.Sprintf("push-%d", s.Seq))
	if err != nil {
		d.Log.Warn("push: claim", "path", path, "seq", s.Seq, "err", err)
		return
	}
	if dup {
		return // another daemon, or this one before a restart, has it
	}
	j := pushJob{req: req, path: path, seq: s.Seq}
	select {
	case jobs <- j:
	case <-ctx.Done():
		pushShutdown(d, j)
	}
}

// pushDeliver delivers j under dctx, retrying after each of d.PushRetry
// (default 5s, 30s); when every try fails it logs and publishes
// push-failed. A try that fails once ctx has ended, or a retry wait that
// ctx ends, is recorded by pushShutdown. The claim stands either way, so a
// failed push is never repeated.
func pushDeliver(ctx, dctx context.Context, d Deps, j pushJob) {
	delays := d.PushRetry
	if delays == nil {
		delays = defaultPushRetry
	}
	var err error
	for i := 0; ; i++ {
		if err = deliver(dctx, d, j.req.Headers, j.req.Body); err == nil {
			return
		}
		if ctx.Err() != nil {
			pushShutdown(d, j)
			return
		}
		if i == len(delays) {
			break
		}
		select {
		case <-ctx.Done():
			pushShutdown(d, j)
			return
		case <-time.After(delays[i]):
		}
	}
	d.Log.Warn("push: deliver", "path", j.path, "seq", j.seq, "err", err)
	ev := mesh.Event{From: d.from(), Event: "push-failed", To: j.path, Text: j.req.Title, Seq: j.seq, Body: err.Error(),
		At: d.Now().UTC().Format(time.RFC3339)}
	if err := mesh.PublishEvent(ctx, d.Client, ev); err != nil {
		d.Log.Warn("push: publish push-failed", "err", err)
	}
}

// pushShutdown records claimed push j as undelivered at shutdown: a log
// line and a push-failed event with body "shutdown", published under a
// short context of its own since the daemon's has ended.
func pushShutdown(d Deps, j pushJob) {
	d.Log.Warn("push: not delivered at shutdown", "path", j.path, "seq", j.seq)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ev := mesh.Event{From: d.from(), Event: "push-failed", To: j.path, Text: j.req.Title, Seq: j.seq, Body: "shutdown",
		At: d.Now().UTC().Format(time.RFC3339)}
	if err := mesh.PublishEvent(ctx, d.Client, ev); err != nil {
		d.Log.Warn("push: publish push-failed", "err", err)
	}
}
