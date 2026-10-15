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
// ok is false unless n is a lead and ev.Event is listed in cfg.Push. console
// is the lead's thread link ("" = no action button).
func Push(ev mesh.Event, n mesh.Node, cfg config.NotifyConfig, console string) (req PushRequest, ok bool) {
	k, known := pushKinds[ev.Event]
	if n.Role != "lead" || !known || !slices.Contains(cfg.Push, ev.Event) {
		return PushRequest{}, false
	}
	req.Title = fmt.Sprintf(k.title, ev.From)
	req.Priority, req.Tags = k.priority, k.tags
	if cfg.MapURL != "" {
		req.Click = cfg.MapURL + "#/lead/" + ev.From
	}
	if console != "" {
		req.Actions = "view, Open thread, " + console
	}
	req.Body = cutUTF8(ev.Text, pushBodyMax)
	if ev.Event == "done" && ev.Commit != "" {
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

// pushLoop pushes a lead's listed events as they happen, once each across
// restarts and daemons. It subscribes to bm.> live, then catches up from
// history (events newer than [notify] catch_up with no push claim yet) and
// skips live events the history already held. Each event is claimed by a
// push daemon event with Nats-Msg-Id push-<seq> before delivery; a
// duplicate reply means someone else has it. Deliveries run in order on
// their own goroutine so a slow or failing ntfy never stalls the follower.
func pushLoop(ctx context.Context, d Deps) {
	if !pushEnabled(d.Cfg.Notify) {
		return
	}
	d.defaults()
	jobs := make(chan pushJob, 256)
	worker := make(chan struct{})
	go func() {
		defer close(worker)
		for {
			select {
			case <-ctx.Done():
				return
			case j := <-jobs:
				pushDeliver(ctx, d, j)
			}
		}
	}()
	defer func() { <-worker }()
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
			if path, event, ok := nodeEvent(s.Subject); ok && slices.Contains(d.Cfg.Notify.Push, event) {
				pushClaim(ctx, d, s, path, event, jobs)
			}
		}
		cancel()
	}
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
	claims, err := mesh.History(ctx, d.Client, "bm.daemon.*.push", time.Time{})
	if err != nil {
		return 0, err
	}
	claimed := map[uint64]bool{}
	for _, s := range claims {
		if ev, err := decodeEvent(s.Data); err == nil && ev.Seq != 0 {
			claimed[ev.Seq] = true
		}
	}
	for _, s := range hist {
		last = max(last, s.Seq)
		path, event, ok := nodeEvent(s.Subject)
		if !ok || claimed[s.Seq] || !slices.Contains(d.Cfg.Notify.Push, event) {
			continue
		}
		at := s.At
		if ev, err := decodeEvent(s.Data); err == nil {
			if t, err := time.Parse(time.RFC3339, ev.At); err == nil {
				at = t
			}
		}
		if at.Before(cutoff) {
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
	req, ok := Push(ev, n, d.Cfg.Notify, web.ConsoleURL(d.Cfg.Console, d.Host, n))
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
	select {
	case jobs <- pushJob{req: req, path: path, seq: s.Seq}:
	case <-ctx.Done():
	}
}

// pushDeliver delivers j, retrying after each of d.PushRetry (default 5s,
// 30s); when every try fails it logs and publishes push-failed. The claim
// stands either way, so a failed push is never repeated.
func pushDeliver(ctx context.Context, d Deps, j pushJob) {
	delays := d.PushRetry
	if delays == nil {
		delays = defaultPushRetry
	}
	var err error
	for i := 0; ; i++ {
		if err = deliver(ctx, d, j.req.Headers, j.req.Body); err == nil {
			return
		}
		if i == len(delays) {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delays[i]):
		}
	}
	if ctx.Err() != nil {
		return
	}
	d.Log.Warn("push: deliver", "path", j.path, "seq", j.seq, "err", err)
	ev := mesh.Event{From: d.from(), Event: "push-failed", To: j.path, Text: j.req.Title, Seq: j.seq, Body: err.Error(),
		At: d.Now().UTC().Format(time.RFC3339)}
	if err := mesh.PublishEvent(ctx, d.Client, ev); err != nil {
		d.Log.Warn("push: publish push-failed", "err", err)
	}
}
