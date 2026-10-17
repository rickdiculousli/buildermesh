// Command seed fills a scratch mesh (scripts/seed.sh runs it against a
// throwaway bmesh serve) with every state the map draws: two live leads and
// their executors (working, asking, done and one idle), a held lead, a done lead for the
// shelf, a silent lead, an orphan, a round and its two engineers (one
// blocked), a pooled routine kind with instances in two repos and three
// spent fresh runs, a queued job, a few events and messages, two leases, a
// result and a daemon digest, and writes the quota fixture the seeded
// config's quota command reads (-quota). -leads N (default 2) adds N-2
// working leads, each with one working executor, to look at a crowded map
// (and long names) on a phone.
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
	flag.Parse()
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
		{Path: "app-0042", Role: "lead", Repo: "app", Spec: "0042", StartedAt: ago(50), Branch: "bmesh/app-0042",
			Runner: mesh.NodeRunner{Kind: "t3", Thread: "thread-0042"}},
		{Path: "app-0042.api", Role: "executor", Repo: "app", Parent: "app-0042", StartedAt: ago(45)},
		{Path: "app-0042.web", Role: "executor", Repo: "app", Parent: "app-0042", StartedAt: ago(40)},
		{Path: "app-0042.e2e", Role: "executor", Repo: "app", Parent: "app-0042", StartedAt: ago(5)},
		{Path: "app-0039", Role: "lead", Repo: "app", Spec: "0039", StartedAt: ago(80), Branch: "bmesh/app-0039"},
		{Path: "app-0040", Role: "lead", Repo: "app", Spec: "0040", StartedAt: ago(60), Branch: "bmesh/app-0040"},
		{Path: "docs-0007", Role: "lead", Repo: "docs", Spec: "0007", StartedAt: ago(30)},
		{Path: "docs-0007.guide", Role: "executor", Repo: "docs", Parent: "docs-0007", StartedAt: ago(25)},
		{Path: "old-0001.fix", Role: "executor", Repo: "app", Parent: "old-0001", StartedAt: ago(90)},
		{Path: "app-round-1", Role: "round", Repo: "app", StartedAt: ago(35)},
		{Path: "app-round-1.eng", Role: "engineer", Repo: "app", Parent: "app-round-1", StartedAt: ago(35),
			Branch: "app-round-1-eng", Runner: mesh.NodeRunner{Kind: "t3", Thread: "thread-round-1-eng"}},
		{Path: "app-round-1.ux", Role: "engineer", Repo: "app", Parent: "app-round-1", StartedAt: ago(35),
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
	// Each step is an event and the state it sets; silent is the daemon's.
	for _, s := range [][4]string{
		{"app-0042", "started", "planning the endpoints", "working"},
		{"app-0042.api", "started", "", "working"},
		{"app-0042.api", "progress", "handlers written, tests next", "working"},
		{"app-0042.web", "started", "", "working"},
		{"app-0042.web", "ask", "which chart library?", "asking"},
		{"docs-0007", "started", "", "working"},
		{"app-0040", "started", "", "working"},
		{"app-0040", "held", "before land by operator: hub move first", "held"},
		{"app-0039", "started", "", "working"},
		{"app-0039", "done", "landed the map's legend", "done"},
		{"daemon.seed", "silent", "quiet for 25m", "silent"},
		{"docs-0007.guide", "started", "", "working"},
		{"docs-0007.guide", "done", "guide drafted", "done"},
		{"old-0001.fix", "started", "", "working"},
		{"app-round-1", "started", "", "working"},
		{"app-round-1.eng", "started", "", "working"},
		{"app-round-1.eng", "progress", "043 landed, writing 044", "working"},
		{"app-round-1.ux", "started", "", "working"},
		{"app-round-1.ux", "blocked", "the app will not run", "blocked"},
		{"routine.review.opus-5-5-1", "started", "", "working"},
		{"routine.review.opus-5-5-1", "progress", "job seed-j1 ok 4200 ctx 51000", "working"},
		{"routine.review.f1a2b3c4d", "started", "", "working"},
		{"routine.review.f1a2b3c4d", "done", "", "done"},
		{"routine.review.f5e6f7a8b", "started", "", "working"},
		{"routine.review.f5e6f7a8b", "done", "", "done"},
		{"routine.review.f9c0d1e2f", "started", "", "working"},
		{"routine.review.f9c0d1e2f", "done", "", "done"},
	} {
		ev, path := mesh.Event{From: s[0], Event: s[1], Text: s[2]}, s[0]
		if s[1] == "silent" {
			ev.To, path = "docs-0007", "docs-0007"
		}
		if s[1] == "done" {
			ev.Commit = "0123abcd"
		}
		if err := mesh.PublishEvent(ctx, c, ev); err != nil {
			return err
		}
		if _, err := mesh.WriteState(ctx, c, path, mesh.State{State: s[3], Text: s[2], By: "seed"}); err != nil {
			return fmt.Errorf("%s %s: %w", path, s[3], err)
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
		{From: "app-0042", To: "routine.review", Kind: "job", Text: "review api",
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
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "daemon.seed", Event: "digest", At: ago(23),
		Text: "1 asking, 1 done, 1 silent",
		Body: "asking: app-0042.web: which chart library?\ndone: docs-0007.guide @0123abcd\nsilent: docs-0007 (quiet for 25m)"}); err != nil {
		return err
	}
	return mesh.SendReply(ctx, c, mesh.Path{"routine", "review", "opus-5-5-1"}, mesh.Path{"app-0042"},
		mesh.Reply{Job: "seed-j1", Kind: "review", OK: true, Output: "looks good", By: "routine.review.opus-5-5-1"})
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
