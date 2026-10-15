// Command seed fills a scratch mesh (scripts/seed.sh runs it against a
// throwaway bmesh serve) with two leads, their executors, an orphan, a pooled
// routine kind with two instances and a few events and messages. -leads N
// adds leads up to N in all, each with one working executor, to look at a
// crowded map (and long names) on a phone.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

func main() {
	url := flag.String("nats", "nats://127.0.0.1:4222", "the scratch mesh's NATS URL")
	mapURL := flag.String("map", "http://127.0.0.1:3775/", "the map's URL, printed when done")
	leads := flag.Int("leads", 2, "how many leads in all (at least the two seeded)")
	flag.Parse()
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
		{Path: "app-0042", Role: "lead", Repo: "app", Spec: "0042", StartedAt: ago(50)},
		{Path: "app-0042.api", Role: "executor", Repo: "app", Parent: "app-0042", StartedAt: ago(45)},
		{Path: "app-0042.web", Role: "executor", Repo: "app", Parent: "app-0042", StartedAt: ago(40)},
		{Path: "docs-0007", Role: "lead", Repo: "docs", Spec: "0007", StartedAt: ago(30)},
		{Path: "docs-0007.guide", Role: "executor", Repo: "docs", Parent: "docs-0007", StartedAt: ago(25)},
		{Path: "old-0001.fix", Role: "executor", Repo: "app", Parent: "old-0001", StartedAt: ago(90)},
		{Path: "routine.review.opus-5-5-1", Role: "routine", Repo: "app", Model: "claude-opus-5-5"},
		{Path: "routine.review.opus-5-5-2", Role: "routine", Repo: "app", Model: "claude-opus-5-5"},
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
		{"daemon.seed", "silent", "quiet for 25m", "silent"},
		{"docs-0007.guide", "started", "", "working"},
		{"docs-0007.guide", "done", "guide drafted", "done"},
		{"old-0001.fix", "started", "", "working"},
		{"routine.review.opus-5-5-1", "started", "", "working"},
		{"routine.review.opus-5-5-1", "progress", "job seed-j1 ok 4200 ctx 51000", "working"},
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
	for _, m := range []mesh.Message{
		{From: "app-0042", To: "app-0042.api", Text: "keep the handlers small"},
		{From: "app-0042.web", To: "app-0042", Text: "ask from app-0042.web: which chart library?"},
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
	return mesh.SendReply(ctx, c, mesh.Path{"routine", "review", "opus-5-5-1"}, mesh.Path{"app-0042"},
		mesh.Reply{Job: "seed-j1", Kind: "review", OK: true, Output: "looks good", By: "routine.review.opus-5-5-1"})
}
