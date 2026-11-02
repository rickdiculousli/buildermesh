// Command seed fills a scratch mesh (scripts/seed.sh runs it against a
// throwaway bmesh serve) with every state the map draws, its events
// back-dated over the last six hours so the timeline's default window shows
// every state and tick: two live leads and their executors (working,
// asking, done and one idle), a held lead that asked first, a landing lead
// (ready three times, refused twice, its box's daemon landing it), a done
// lead, a silent lead, a lead blocked for a day (stale: the band's older
// fold), an orphan, a round and its two engineers (one blocked), a pooled
// routine kind with instances in two repos and three spent fresh runs
// (each instance's jobs a segment), a queued job, a few events and
// messages, two leases, a result and a daemon digest 16 hours old (too old for the strip), and writes the quota fixture
// the seeded config's quota command reads (-quota). -leads N (default 2)
// adds N-2 working leads, each with one working executor, to look at a
// crowded map (and long names) on a phone. -again PATH seeds nothing: it
// publishes a new ask on the seeded mesh's PATH (one asking or silent, such
// as docs-0007), so a row dismissed on the map shows again.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

func main() {
	url := flag.String("nats", "nats://127.0.0.1:4222", "the scratch mesh's NATS URL")
	mapURL := flag.String("map", "http://127.0.0.1:3775/", "the map's URL, printed when done")
	leads := flag.Int("leads", 2, "adds this many working leads less two")
	quota := flag.String("quota", "", "where to write the quota fixture (none when empty)")
	again := flag.String("again", "", "seed nothing: publish a new ask on this node")
	flag.Parse()
	if *again != "" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := ask(ctx, *url, *again); err != nil {
			fmt.Fprintln(os.Stderr, "seed:", err)
			os.Exit(1)
		}
		return
	}
	if *quota != "" {
		if err := writeQuota(*quota, time.Now()); err != nil {
			fmt.Fprintln(os.Stderr, "seed:", err)
			os.Exit(1)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := seed(ctx, *url, *leads); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	fmt.Println("map:", *mapURL)
}

func seed(ctx context.Context, url string, leads int) error {
	c, err := mesh.Connect(ctx, url, "", "hub", "bmesh/seed", false)
	if err != nil {
		return err
	}
	defer c.Close()
	ago := func(m int) string { return time.Now().UTC().Add(-time.Duration(m) * time.Minute).Format(time.RFC3339) }
	for _, n := range []mesh.Node{
		{Path: "app-0042", Role: "lead", Repo: "app", Spec: "0042", StartedAt: ago(330), Branch: "bmesh/app-0042",
			Runner: mesh.NodeRunner{Kind: "t3", Thread: "thread-0042"}, Model: "claude-opus-5-5"},
		{Path: "app-0042.api", Role: "executor", Repo: "app", Parent: "app-0042", StartedAt: ago(305)},
		{Path: "app-0042.web", Role: "executor", Repo: "app", Parent: "app-0042", StartedAt: ago(285)},
		{Path: "app-0042.e2e", Role: "executor", Repo: "app", Parent: "app-0042", StartedAt: ago(5)},
		{Path: "app-0039", Role: "lead", Repo: "app", Spec: "0039", StartedAt: ago(245), Branch: "bmesh/app-0039"},
		{Path: "app-0041", Role: "lead", Repo: "app", Spec: "0041", StartedAt: ago(300), Branch: "bmesh/app-0041"},
		{Path: "app-0040", Role: "lead", Repo: "app", Spec: "0040", StartedAt: ago(275), Branch: "bmesh/app-0040"},
		{Path: "app-0038", Role: "lead", Repo: "app", Spec: "0038", StartedAt: ago(26 * 60), Branch: "bmesh/app-0038"},
		{Path: "docs-0007", Role: "lead", Repo: "docs", Spec: "0007", StartedAt: ago(205)},
		{Path: "docs-0007.guide", Role: "executor", Repo: "docs", Parent: "docs-0007", StartedAt: ago(195)},
		{Path: "old-0001.fix", Role: "executor", Repo: "app", Parent: "old-0001", StartedAt: ago(95)},
		{Path: "app-round-1", Role: "round", Repo: "app", StartedAt: ago(155)},
		{Path: "app-round-1.eng", Role: "engineer", Repo: "app", Parent: "app-round-1", StartedAt: ago(152),
			Branch: "app-round-1-eng", Runner: mesh.NodeRunner{Kind: "t3", Thread: "thread-round-1-eng"}},
		{Path: "app-round-1.ux", Role: "engineer", Repo: "app", Parent: "app-round-1", StartedAt: ago(152),
			Branch: "app-round-1-ux", Runner: mesh.NodeRunner{Kind: "t3", Thread: "thread-round-1-ux"}},
		{Path: "routine.review.opus-5-5-1", Role: "routine", Repo: "app", Model: "claude-opus-5-5"},
		{Path: "routine.review.opus-5-5-2", Role: "routine", Repo: "app", Model: "claude-opus-5-5"},
		{Path: "routine.review.opus-5-5-3", Role: "routine", Repo: "docs", Model: "claude-opus-5-5"},
		{Path: "routine.review.f1a2b3c4d", Role: "routine", Repo: "app", Model: "claude-opus-5-5"},
		{Path: "routine.review.f5e6f7a8b", Role: "routine", Repo: "app", Model: "claude-opus-5-5"},
		{Path: "routine.review.f9c0d1e2f", Role: "routine", Repo: "app", Model: "claude-opus-5-5"},
	} {
		n.Host = "seed"
		if err := mesh.PutNode(ctx, c, n); err != nil {
			return err
		}
	}
	// Each step is an event, back-dated (its at) over the last six hours so
	// every state and tick the timeline draws shows on its default window,
	// and the state it sets; the daemon's silent, landing and land-refused
	// are addressed to (to) the node they move. Steps run oldest first.
	type step struct {
		from, event, text, state, to string
		ago                          int
	}
	for _, s := range []step{
		{"app-0038", "started", "", "working", "", 25 * 60},
		{"app-0038", "blocked", "the branches already exist", "blocked", "", 24 * 60},
		{"app-0042", "started", "planning the endpoints", "working", "", 320},
		{"app-0041", "started", "", "working", "", 290},
		{"app-0042.api", "started", "", "working", "", 300},
		{"app-0042.web", "started", "", "working", "", 280},
		{"app-0040", "started", "", "working", "", 270},
		{"app-0042", "progress", "endpoints planned, executors briefed", "working", "", 250},
		{"app-0039", "started", "", "working", "", 240},
		{"app-0040", "ask", "which hub does the map read?", "asking", "", 230},
		{"app-0041", "progress", "parser and tests in", "working", "", 200},
		{"app-0042.api", "progress", "handlers written, tests next", "working", "", 200},
		{"app-0040", "progress", "answered: the k16 hub", "working", "", 200},
		{"docs-0007", "started", "", "working", "", 200},
		{"docs-0007.guide", "started", "", "working", "", 190},
		{"app-0042", "progress", "api merged", "working", "", 180},
		{"app-0039", "progress", "legend drawn", "working", "", 180},
		{"app-round-1", "started", "", "working", "", 150},
		{"app-round-1.eng", "started", "", "working", "", 148},
		{"app-round-1.ux", "started", "", "working", "", 148},
		{"routine.review.opus-5-5-1", "started", "", "working", "", 160},
		{"routine.review.opus-5-5-1", "progress", "job seed-j0 started", "working", "", 150},
		{"routine.review.opus-5-5-1", "progress", "job seed-j0 ok 300000 ctx 42000", "working", "", 145},
		{"app-0041", "ready", "ready to land", "landing", "", 120},
		{"docs-0007", "progress", "outline agreed", "working", "", 120},
		{"daemon.seed", "landing", "review job 7h3kQ running on seed", "landing", "app-0041", 118},
		{"routine.review.f1a2b3c4d", "started", "", "working", "", 112},
		{"routine.review.f1a2b3c4d", "progress", "job seed-f1 started", "working", "", 111},
		{"routine.review.f1a2b3c4d", "progress", "job seed-f1 ok 420000 ctx 61000", "working", "", 104},
		{"routine.review.f1a2b3c4d", "done", "", "done", "", 104},
		{"daemon.seed", "land-refused", "refused: rebase onto origin/main conflicts in map.js", "working", "app-0041", 100},
		{"app-0039", "ready", "ready to land", "landing", "", 100},
		{"docs-0007.guide", "done", "guide drafted", "done", "", 100},
		{"daemon.seed", "landing", "verify running on seed", "landing", "app-0039", 98},
		{"old-0001.fix", "started", "", "working", "", 90},
		{"app-0039", "done", "landed the map's legend", "done", "", 90},
		{"app-0041", "ready", "conflicts resolved", "landing", "", 80},
		{"daemon.seed", "landing", "review job 8m2pR running on seed", "landing", "app-0041", 78},
		{"routine.review.f5e6f7a8b", "started", "", "working", "", 77},
		{"routine.review.f5e6f7a8b", "progress", "job seed-f5 started", "working", "", 76},
		{"routine.review.f5e6f7a8b", "progress", "job seed-f5 fail 900000", "working", "", 62},
		{"routine.review.f5e6f7a8b", "done", "", "done", "", 62},
		{"daemon.seed", "land-refused", "refused: review failed at t:8a638215", "working", "app-0041", 60},
		{"app-0042", "progress", "lead page done, strip next", "working", "", 60},
		{"app-round-1.eng", "progress", "043 landed, writing 044", "working", "", 60},
		{"app-0040", "held", "before land by operator: hub move first", "held", "", 40},
		{"app-round-1.ux", "blocked", "the app will not run", "blocked", "", 40},
		{"routine.review.opus-5-5-1", "progress", "job seed-j1 started", "working", "", 32},
		{"app-0042.web", "ask", "which chart library?", "asking", "", 30},
		{"routine.review.opus-5-5-1", "progress", "job seed-j1 ok 4200 ctx 51000", "working", "", 28},
		{"daemon.seed", "silent", "quiet for 25m", "silent", "docs-0007", 25},
		{"routine.review.f9c0d1e2f", "started", "", "working", "", 24},
		{"routine.review.f9c0d1e2f", "progress", "job seed-f9 started", "working", "", 23},
		{"app-0041", "ready", "review fixed", "landing", "", 20},
		{"daemon.seed", "landing", "review job 9f2kQ running on seed", "landing", "app-0041", 18},
		{"routine.review.f9c0d1e2f", "progress", "job seed-f9 ok 600000 ctx 58000", "working", "", 13},
		{"routine.review.f9c0d1e2f", "done", "", "done", "", 13},
	} {
		ev, path := mesh.Event{From: s.from, Event: s.event, Text: s.text, To: s.to, At: ago(s.ago)}, s.from
		if s.to != "" {
			path = s.to
		}
		if s.event == "done" {
			ev.Commit = "0123abcd"
		}
		if err := mesh.PublishEvent(ctx, c, ev); err != nil {
			return err
		}
		if _, err := mesh.WriteState(ctx, c, path, mesh.State{State: s.state, Text: s.text, By: "seed", At: ev.At}); err != nil {
			return fmt.Errorf("%s %s: %w", path, s.state, err)
		}
	}
	if err := mesh.PutLeadHold(ctx, c, mesh.LeadHold{Path: "app-0040", Gate: "land", Text: "hub move first", By: "operator", At: ago(3)}); err != nil {
		return err
	}
	// The kind's inbox consumer, so the job below counts as queued.
	if _, err := mesh.KindConsumer(ctx, c, "review", 1, time.Hour); err != nil {
		return err
	}
	for _, m := range []mesh.Message{
		{From: "app-0042", To: "app-0042.api", Text: "keep the handlers small"},
		{From: "app-0042.web", To: "app-0042", Text: "ask from app-0042.web: which chart library?"},
		{From: "rick", To: "app-0042", Text: "ship the lead page before the strip"},
		{From: "app-0042", To: "routine.review", Kind: "job", Text: "review api", At: ago(150),
			Job: &mesh.Job{ID: "seed-j2", Kind: "review", Repo: "app", Commit: "0123abcd"}},
	} {
		if _, err := mesh.Send(ctx, c, m); err != nil {
			return err
		}
	}
	for i := 2; i < leads; i++ {
		lead := fmt.Sprintf("platform-migration-%04d", 100+i)
		exec := lead + ".database-schema-backfill"
		for _, n := range []mesh.Node{
			{Path: lead, Role: "lead", Repo: "platform", Spec: fmt.Sprintf("%04d", 100+i), StartedAt: ago(20 - i)},
			{Path: exec, Role: "executor", Repo: "platform", Parent: lead, StartedAt: ago(15 - i)},
		} {
			n.Host = "seed"
			if err := mesh.PutNode(ctx, c, n); err != nil {
				return err
			}
		}
		for _, p := range []string{lead, exec} {
			if err := mesh.PublishEvent(ctx, c, mesh.Event{From: p, Event: "started"}); err != nil {
				return err
			}
			if _, err := mesh.WriteState(ctx, c, p, mesh.State{State: "working", By: "seed"}); err != nil {
				return fmt.Errorf("%s working: %w", p, err)
			}
		}
	}
	now := time.Now()
	if _, err := mesh.AcquireLease(ctx, c, "app.review", "routine.review.opus-5-5-1", 25*time.Minute, 1, now); err != nil {
		return err
	}
	if _, err := mesh.AcquireLease(ctx, c, "app.deploy", "app-0042", 90*time.Second, 1, now); err != nil {
		return err
	}
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "review", Tree: "seedtree1", Commit: "0123abcd", OK: true,
		Output: "looks good", By: "routine.review.opus-5-5-1"}); err != nil {
		return err
	}
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "daemon.seed", Event: "digest", At: ago(16 * 60),
		Text: "1 asking, 1 done, 1 silent",
		Body: "asking: app-0042.web: which chart library?\ndone: docs-0007.guide @0123abcd\nsilent: docs-0007 (quiet for 25m)"}); err != nil {
		return err
	}
	return mesh.SendReply(ctx, c, mesh.Path{"routine", "review", "opus-5-5-1"}, mesh.Path{"app-0042"},
		mesh.Reply{Job: "seed-j1", Kind: "review", OK: true, Output: "looks good", By: "routine.review.opus-5-5-1"})
}

// ask publishes a new ask on path and moves its state to asking.
func ask(ctx context.Context, url, path string) error {
	c, err := mesh.Connect(ctx, url, "", "hub", "bmesh/seed", false)
	if err != nil {
		return err
	}
	defer c.Close()
	const text = "asking again"
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: path, Event: "ask", Text: text}); err != nil {
		return err
	}
	_, err = mesh.WriteState(ctx, c, path, mesh.State{State: "asking", Text: text, By: "seed"})
	return err
}

// writeQuota writes two providers' readings in the quota command's output
// format, their resets counted from now.
func writeQuota(path string, now time.Time) error {
	at := func(d time.Duration) string { return now.Add(d).UTC().Format(time.RFC3339) }
	rs := []map[string]any{
		{"reading": map[string]any{"provider": "claude", "five_hour": 42.4, "week": 21.2,
			"five_reset": at(70 * time.Minute), "week_reset": at(76 * time.Hour)}},
		{"reading": map[string]any{"provider": "codex", "five_hour": 87.6, "week": 64,
			"five_reset": at(3*time.Hour + 5*time.Minute), "week_reset": at(30 * time.Hour)}},
	}
	b, err := json.Marshal(rs)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
