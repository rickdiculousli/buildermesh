// Command seed fills a scratch mesh (scripts/seed.sh runs it against a
// throwaway bmesh serve) with every state the map draws, its events
// back-dated over the last six hours so the timeline's default window shows
// every state and tick: two live leads and their executors (working,
// asking, done and one idle), a held lead that asked first, a landing lead
// (ready three times, refused twice, its box's daemon landing it, and the
// same waiting text in two land events and the daemon's landing), a done
// lead, a silent lead, a lead blocked for a day (stale: the band's older
// fold), an orphan, a round and its two engineers (one blocked), a pooled
// routine kind with instances in two repos and three spent fresh runs
// (each instance's jobs a segment), a queued job, a few events and
// messages, two leases, a result and a daemon digest 16 hours old (too old for the strip), and writes the quota fixture
// the seeded config's quota command reads (-quota). It also publishes token
// use to the bm-usage stream, back-dated inside each node's working span: the
// main calls of app-0042 (about 120, context 20k to 320k, one compaction
// down to 40k, one +19k step from "Read cases.go") with a subagent file,
// app-0041, app-0039, app-0040, app-0042.api and routine.review.opus-5-5-1,
// two coordinator sessions of runs split by design and exec (one with a
// subagent, one peaking at 420k), and a day of claude and codex quota
// readings every five minutes (claude's plan losing its five-hour window
// halfway, so the strip reads none), and a day of two other boxes' minutes
// on bmusage.host (box-a on Linux with pressure, mac-b on macOS with its
// pressure level), their CPU and memory split among the seeded agents
// while each works; the seeded serve's own daemon answers box seed's live
// reads from this box's real processes. For the flags (108) it adds app-batch-1, four
// S leads done three days ago that with app-0039 make the S baseline;
// app-0041 (S) reads past 2.5x that median and is refused three times
// (overworking), and app-0045's four subagents read 140M in the last 40
// minutes (runaway); app-0039, app-0041, app-0042 and app-0045 are the live
// family app-batch-2. -leads N (default 2)
// adds N-2 working leads, each with one working executor, to look at a
// crowded map (and long names) on a phone. -worktree DIR makes DIR
// app-0042's worktree: seed.sh passes the seeded serve's working directory,
// so the serve's own process reads as that lead's on the hosts tab. -again PATH seeds nothing: it
// publishes a new ask on the seeded mesh's PATH (one asking or silent, such
// as docs-0007), so a row dismissed on the map shows again.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/hoststat"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/usage"
)

func main() {
	url := flag.String("nats", "nats://127.0.0.1:4222", "the scratch mesh's NATS URL")
	mapURL := flag.String("map", "http://127.0.0.1:3775/", "the map's URL, printed when done")
	leads := flag.Int("leads", 2, "adds this many working leads less two")
	quota := flag.String("quota", "", "where to write the quota fixture (none when empty)")
	again := flag.String("again", "", "seed nothing: publish a new ask on this node")
	worktree := flag.String("worktree", "", "app-0042's worktree: the seeded serve's working directory, so its process is that lead's")
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := seed(ctx, *url, *leads, *worktree); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	fmt.Println("map:", *mapURL)
}

// batch1 is app-batch-1's first start in minutes ago: three days and five
// hours.
const batch1 = 3*24*60 + 300

func seed(ctx context.Context, url string, leads int, worktree string) error {
	c, err := mesh.Connect(ctx, url, "", "hub", "bmesh/seed", false)
	if err != nil {
		return err
	}
	defer c.Close()
	ago := func(m int) string { return time.Now().UTC().Add(-time.Duration(m) * time.Minute).Format(time.RFC3339) }
	for _, n := range []mesh.Node{
		{Path: "app-0042", Role: "lead", Repo: "app", Spec: "0042", StartedAt: ago(330), Branch: "bmesh/app-0042", Worktree: worktree,
			Runner: mesh.NodeRunner{Kind: "t3", Thread: "thread-0042"}, Model: "claude-opus-5-5", Family: "app-batch-2"},
		{Path: "app-0042.api", Role: "executor", Repo: "app", Parent: "app-0042", StartedAt: ago(305)},
		{Path: "app-0042.web", Role: "executor", Repo: "app", Parent: "app-0042", StartedAt: ago(285)},
		{Path: "app-0042.e2e", Role: "executor", Repo: "app", Parent: "app-0042", StartedAt: ago(5)},
		{Path: "app-0039", Role: "lead", Repo: "app", Spec: "0039", StartedAt: ago(245), Branch: "bmesh/app-0039", Family: "app-batch-2"},
		{Path: "app-0041", Role: "lead", Repo: "app", Spec: "0041", StartedAt: ago(300), Branch: "bmesh/app-0041", Family: "app-batch-2"},
		{Path: "app-0045", Role: "lead", Repo: "app", Spec: "0045", StartedAt: ago(70), Branch: "bmesh/app-0045", Family: "app-batch-2"},
		{Path: "app-0043", Role: "lead", Repo: "app", Spec: "0043", StartedAt: ago(150), Branch: "bmesh/app-0043", Model: "claude-opus-5-5"},
		{Path: "app-0030", Role: "lead", Repo: "app", Spec: "0030", StartedAt: ago(batch1), Family: "app-batch-1"},
		{Path: "app-0031", Role: "lead", Repo: "app", Spec: "0031", StartedAt: ago(batch1 - 10), Family: "app-batch-1"},
		{Path: "app-0032", Role: "lead", Repo: "app", Spec: "0032", StartedAt: ago(batch1 - 20), Family: "app-batch-1"},
		{Path: "app-0033", Role: "lead", Repo: "app", Spec: "0033", StartedAt: ago(batch1 - 30), Family: "app-batch-1"},
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
		// app-batch-1: four S leads, finished three days ago (the size rule's baseline).
		{"app-0030", "started", "", "working", "", batch1},
		{"app-0031", "started", "", "working", "", batch1 - 10},
		{"app-0032", "started", "", "working", "", batch1 - 20},
		{"app-0033", "started", "", "working", "", batch1 - 30},
		{"app-0030", "done", "landed spec-30", "done", "", batch1 - 95},
		{"app-0031", "done", "landed spec-31", "done", "", batch1 - 130},
		{"daemon.seed", "land-refused", "refused: review failed at t:3a1b2c3d", "working", "app-0032", batch1 - 150},
		{"app-0032", "done", "landed spec-32", "done", "", batch1 - 190},
		{"app-0033", "done", "landed spec-33", "done", "", batch1 - 240},
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
		{"app-0043", "started", "", "working", "", 149},
		{"app-round-1.eng", "started", "", "working", "", 148},
		{"app-round-1.ux", "started", "", "working", "", 148},
		{"routine.review.opus-5-5-1", "started", "", "working", "", 160},
		{"routine.review.opus-5-5-1", "progress", "job seed-j0 started", "working", "", 150},
		{"routine.review.opus-5-5-1", "progress", "job seed-j0 ok 300000 ctx 42000", "working", "", 145},
		{"app-0041", "ready", "ready to land", "landing", "", 135},
		{"daemon.seed", "land-refused", "refused: verify failed at t:1c2d3e4f", "working", "app-0041", 130},
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
		{"app-0045", "started", "fanning out four readers", "working", "", 68},
		{"app-0042", "progress", "lead page done, strip next", "working", "", 60},
		{"app-round-1.eng", "progress", "043 landed, writing 044", "working", "", 60},
		{"app-0040", "held", "before land by operator: hub move first", "held", "", 40},
		{"app-round-1.ux", "blocked", "the app will not run", "blocked", "", 40},
		{"routine.review.opus-5-5-1", "progress", "job seed-j1 started", "working", "", 32},
		{"app-0042.web", "ask", "which chart library?", "asking", "", 30},
		{"routine.review.opus-5-5-1", "progress", "job seed-j1 ok 4200 ctx 51000", "working", "", 28},
		{"app-0045", "progress", "four subagents reading the corpus", "working", "", 35},
		{"daemon.seed", "silent", "quiet for 25m", "silent", "docs-0007", 25},
		{"routine.review.f9c0d1e2f", "started", "", "working", "", 24},
		{"routine.review.f9c0d1e2f", "progress", "job seed-f9 started", "working", "", 23},
		{"app-0041", "ready", "review fixed", "landing", "", 20},
		{"routine.review.f9c0d1e2f", "progress", "job seed-f9 ok 600000 ctx 58000", "working", "", 13},
		{"routine.review.f9c0d1e2f", "done", "", "done", "", 13},
		{"routine.review.opus-5-5-2", "started", "", "working", "", 9},
		{"daemon.seed", "landing", "waiting for job 9f2kQ (review, running on seed)", "landing", "app-0041", 7},
		{"app-0043", "ready", "ready to land", "landing", "", 6},
		{"daemon.seed", "landing", "running verify on t:4208beec", "landing", "app-0043", 5},
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
		{From: "app-0041", To: "routine.review", Kind: "job", Text: "base: 0123abcd", At: ago(8),
			Job: &mesh.Job{ID: "9f2kQ", Kind: "review", Repo: "app", Commit: "0123abcd", Effort: "high"}},
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
	if err := mesh.SendReply(ctx, c, mesh.Path{"routine", "review", "opus-5-5-1"}, mesh.Path{"app-0042"},
		mesh.Reply{Job: "seed-j1", Kind: "review", OK: true, Output: "looks good", By: "routine.review.opus-5-5-1"}); err != nil {
		return err
	}
	if err := seedUsage(ctx, c, now); err != nil {
		return err
	}
	if err := seedLanding(ctx, c, now); err != nil {
		return err
	}
	if err := seedHosts(ctx, c, now); err != nil {
		return err
	}
	return seedQuota(ctx, c, now)
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
		{"reading": map[string]any{"provider": "claude", "five_hour": nil, "week": 21.2,
			"five_reset": nil, "week_reset": at(76 * time.Hour)}},
		{"reading": map[string]any{"provider": "codex", "five_hour": 87.6, "week": 64,
			"five_reset": at(3*time.Hour + 5*time.Minute), "week_reset": at(30 * time.Hour)}},
	}
	b, err := json.Marshal(rs)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// thread is one transcript file's worth of usage: a message head and its
// calls, published to subj.
type thread struct {
	subj, agent, role, repo, session, file, sub, desc string
	calls                                             []usage.Call
	marks                                             []usage.Mark
}

// publish sends t in messages of at most 500 calls (the marks with the
// first), like the daemon's pass.
func (t thread) publish(ctx context.Context, c *mesh.Client) error {
	for i, chunk := 0, 0; i < len(t.calls) || chunk == 0; i, chunk = i+500, chunk+1 {
		m := usage.Message{Agent: t.agent, Role: t.role, Host: "seed", Repo: t.repo, Session: t.session,
			File: t.file, Sub: t.sub, Desc: t.desc, Calls: t.calls[i:min(i+500, len(t.calls))]}
		if chunk == 0 {
			m.Marks = t.marks
		}
		data, err := json.Marshal(m)
		if err != nil {
			return err
		}
		id := fmt.Sprintf("seed/%s/%d", t.file, chunk)
		if _, err := c.JS.Publish(ctx, t.subj, data, jetstream.WithMsgID(id), jetstream.WithExpectStream(mesh.StreamUsage)); err != nil {
			return fmt.Errorf("usage %s: %w", t.subj, err)
		}
	}
	return nil
}

// ctxPath is n context sizes climbing in uneven steps from first to last;
// when big is in 1..n-1 that step is exactly bigStep and the others share
// the rest, so it is the largest.
func ctxPath(r *rand.Rand, n int, first, last, big, bigStep int64) []int64 {
	w := make([]float64, n)
	total := 0.0
	for i := 1; i < n; i++ {
		if int64(i) != big {
			x := r.Float64()
			w[i] = 0.1 + 1.8*x*x
			total += w[i]
		}
	}
	rest := float64(last - first)
	if big > 0 {
		rest -= float64(bigStep)
	}
	out := make([]int64, n)
	out[0] = first
	for i := 1; i < n; i++ {
		d := int64(w[i] / total * rest)
		if int64(i) == big {
			d = bigStep
		}
		out[i] = out[i-1] + d
	}
	return out
}

// spread is n increasing times across [from, to), jittered.
func spread(r *rand.Rand, n int, from, to time.Time) []time.Time {
	out := make([]time.Time, n)
	for i := range out {
		out[i] = from.Add(time.Duration(float64(to.Sub(from)) * (float64(i) + 0.8*r.Float64()) / float64(n)))
	}
	return out
}

var causes = []string{"Read cases.go", "Read map.js", "Bash: run the tests", "Edit map.js", "Edit api.go",
	"Grep handlers", "Agent review the diff", "message", "Write notes.md", "wake"}

// calls builds one call per time with the context sizes ctx: cache reads
// about 95% of it, the causes drawn at random, the first one a message.
// phase, when given, is each call's.
func calls(r *rand.Rand, id, sub string, ts []time.Time, ctx []int64, phase []string) []usage.Call {
	out := make([]usage.Call, len(ts))
	for i, t := range ts {
		in := int64(1 + r.IntN(8))
		cw := int64(float64(ctx[i]) * (0.03 + 0.02*r.Float64()))
		c := usage.Call{At: t.UTC().Format(time.RFC3339Nano), ID: fmt.Sprintf("msg_%s_%03d", id, i), Model: "claude-opus-5-5",
			In: in, CW: cw, CR: ctx[i] - in - cw, Out: int64(120 + r.IntN(1500)), Ctx: ctx[i], Sub: sub,
			Cause: causes[r.IntN(len(causes))]}
		if i == 0 {
			c.Cause = "message"
		}
		if phase != nil {
			c.Phase = phase[i]
		}
		out[i] = c
	}
	return out
}

// seedUsage publishes the seeded agents' token use; every call is inside
// the last six hours and its agent's working span.
func seedUsage(ctx context.Context, c *mesh.Client, now time.Time) error {
	r := rand.New(rand.NewPCG(107, 1))
	ago := func(m float64) time.Time { return now.Add(-time.Duration(m * float64(time.Minute))) }
	node := func(path, role string) thread {
		return thread{subj: "bmusage.node." + path, agent: path, role: role, repo: "app",
			session: fmt.Sprintf("5eed0000-0000-4000-8000-%012x", r.Uint64()>>16), file: "-home-seed-app/" + path + ".jsonl"}
	}
	var all []thread

	// app-0042: ctx 20k to 320k over 80 calls, a compaction to 40k, 40 more
	// calls; the +19k step at call 45 reads cases.go.
	t := node("app-0042", "lead")
	ts := spread(r, 120, ago(318), ago(2))
	ctxs := append(ctxPath(r, 80, 20000, 320000, 45, 19000), ctxPath(r, 40, 40000, 150000, 0, 0)...)
	t.calls = calls(r, "0042", "", ts, ctxs, nil)
	t.calls[45].Cause = "Read cases.go"
	t.calls[80].Cause = "compact"
	t.marks = []usage.Mark{{At: ts[79].Add(ts[80].Sub(ts[79]) / 2).UTC().Format(time.RFC3339Nano), Kind: "compact"}}
	all = append(all, t)
	// Its subagent file.
	s := t
	s.file, s.sub, s.desc, s.marks = "-home-seed-app/"+t.session+"/subagents/agent-a1b2c3.jsonl", "agent-a1b2c3", "Explore the handlers", nil
	s.calls = calls(r, "0042sub", "agent-a1b2c3", spread(r, 15, ago(210), ago(195)), ctxPath(r, 15, 8000, 60000, 0, 0), nil)
	all = append(all, s)

	for _, n := range []struct {
		path, role  string
		n           int
		from, to    float64
		first, peak int64
	}{
		{"app-0041", "lead", 110, 288, 20, 18000, 250000}, // past 2.5x the S median: overworking
		{"app-0039", "lead", 40, 238, 90, 15000, 150000},
		{"app-0040", "lead", 30, 268, 40, 12000, 110000},
		{"app-0042.api", "executor", 30, 298, 200, 10000, 90000},
		// app-batch-1, done three days ago: with app-0039, five S runs, the S baseline.
		{"app-0030", "lead", 35, batch1 - 2, batch1 - 90, 15000, 190000},
		{"app-0031", "lead", 40, batch1 - 12, batch1 - 125, 15000, 210000},
		{"app-0032", "lead", 45, batch1 - 22, batch1 - 185, 15000, 210000},
		{"app-0033", "lead", 50, batch1 - 32, batch1 - 235, 15000, 230000},
		// app-0045's own thread; its subagents below read the most.
		{"app-0045", "lead", 40, 66, 3, 30000, 130000},
	} {
		t := node(n.path, n.role)
		t.calls = calls(r, n.path, "", spread(r, n.n, ago(n.from), ago(n.to)), ctxPath(r, n.n, n.first, n.peak, 0, 0), nil)
		all = append(all, t)
	}
	// app-0045's four subagents, 100 calls each at 300k to 400k in the
	// last 40 minutes: 140M, past burn's 100M in an hour (runaway) for the
	// next 20 minutes.
	for k := range 4 {
		id := fmt.Sprintf("agent-f%d0045", k)
		t := node("app-0045", "lead")
		t.file, t.sub, t.desc = "-home-seed-app/app-0045/subagents/"+id+".jsonl", id, fmt.Sprintf("Read corpus part %d", k+1)
		t.calls = calls(r, "0045"+id, id, spread(r, 100, ago(40), ago(2)), ctxPath(r, 100, 300000, 400000, 0, 0), nil)
		all = append(all, t)
	}
	// The routine instance: five calls around each of its two jobs.
	t = node("routine.review.opus-5-5-1", "routine")
	ts = append(spread(r, 5, ago(150), ago(145)), spread(r, 5, ago(32), ago(28))...)
	t.calls = calls(r, "review1", "", ts, ctxPath(r, 10, 8000, 51000, 0, 0), nil)
	all = append(all, t)

	// Two coordinator sessions: runs of calls 30 s to 3 min apart with gaps
	// over 5 min between, each run split by phase.
	type seg struct {
		n     int
		phase string
	}
	for _, ss := range []struct {
		id    string
		start float64
		peak  int64
		runs  [][]seg
	}{
		{"ec03a02c-5eed-4a1b-9c0d-000000000001", 335, 420000, [][]seg{{{14, "design"}}, {{8, "design"}, {12, "exec"}}, {{22, "exec"}},
			{{10, "exec"}, {9, "design"}}, {{6, "design"}, {14, "exec"}}, {{18, "exec"}}}},
		{"b7d4e91f-5eed-4a1b-9c0d-000000000002", 280, 210000, [][]seg{{{12, "design"}, {6, "exec"}}, {{10, "exec"}, {8, "design"}},
			{{9, "design"}, {11, "exec"}}, {{12, "exec"}}}},
	} {
		at := ago(ss.start)
		var ts []time.Time
		var phase []string
		for _, run := range ss.runs {
			at = at.Add(time.Duration(6+r.IntN(13)) * time.Minute)
			for _, sg := range run {
				for i := 0; i < sg.n; i++ {
					at = at.Add(30*time.Second + time.Duration(r.IntN(150))*time.Second)
					ts, phase = append(ts, at), append(phase, sg.phase)
				}
			}
		}
		agent := usage.SessionAgent("seed", ss.id)
		t := thread{subj: "bmusage.session.seed." + ss.id, agent: agent, role: usage.Coordinator, repo: "app", session: ss.id,
			file: "-home-seed-app/" + ss.id + ".jsonl"}
		t.calls = calls(r, ss.id[:8], "", ts, ctxPath(r, len(ts), 30000, ss.peak, 0, 0), phase)
		all = append(all, t)
		if ss.peak < 300000 {
			// Its subagent: eight calls in a few seconds, in the phase of the main call they follow.
			k := 20
			sub := t
			sub.file, sub.sub, sub.desc = "-home-seed-app/"+ss.id+"/subagents/agent-d4e5f6.jsonl", "agent-d4e5f6", "Review the spec"
			var sts []time.Time
			var sph []string
			for j := 0; j < 8; j++ {
				sts, sph = append(sts, ts[k].Add(time.Duration(3*(j+1))*time.Second)), append(sph, phase[k])
			}
			sub.calls = calls(r, ss.id[:8]+"sub", "agent-d4e5f6", sts, ctxPath(r, 8, 10000, 70000, 0, 0), sph)
			all = append(all, sub)
		}
	}
	for _, t := range all {
		if err := t.publish(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

// seedQuota publishes a day of readings, one per provider every five
// minutes: the five-hour percent climbs and drops at a reset every five
// hours, the week's climbs slowly.
func seedQuota(ctx context.Context, c *mesh.Client, now time.Time) error {
	r := rand.New(rand.NewPCG(107, 2))
	const five = 5 * time.Hour
	for _, p := range []struct {
		name        string
		reset       time.Duration // the next five-hour reset from now
		peak, week  float64       // the five-hour percent at a reset; the week's percent now
		perDay      float64       // the week's gain per day
		weekResetIn time.Duration
	}{
		{"claude", 70 * time.Minute, 78, 21.2, 9, 76 * time.Hour},
		{"codex", 3*time.Hour + 5*time.Minute, 94, 64, 12, 30 * time.Hour},
	} {
		for k := 0; k < 288; k++ {
			t := now.Add(-time.Duration(k) * 5 * time.Minute).UTC().Truncate(time.Second)
			// The first reset after t, on the grid that has one at now+reset.
			next := now.Add(p.reset)
			for next.Add(-five).After(t) {
				next = next.Add(-five)
			}
			phase := 1 - next.Sub(t).Seconds()/five.Seconds()
			fh := math.Min(100, p.peak*math.Pow(phase, 0.85)+r.Float64()*2)
			week := p.week - p.perDay*now.Sub(t).Hours()/24 + r.Float64()*0.2
			readAt := t.Format(time.RFC3339)
			var fiveHour, fiveReset any = math.Round(fh*10) / 10, next.UTC().Format(time.RFC3339)
			if p.name == "claude" && k < 144 {
				fiveHour, fiveReset = nil, nil
			}
			data, err := json.Marshal(map[string]any{"provider": p.name, "five_hour": fiveHour, "week": math.Round(week*10) / 10,
				"five_reset": fiveReset, "week_reset": now.Add(p.weekResetIn).UTC().Format(time.RFC3339), "read_at": readAt})
			if err != nil {
				return err
			}
			if _, err := c.JS.Publish(ctx, "bmusage.quota.seed", data, jetstream.WithMsgID("seed/quota/"+p.name+"/"+readAt),
				jetstream.WithExpectStream(mesh.StreamUsage)); err != nil {
				return fmt.Errorf("quota: %w", err)
			}
		}
	}
	return nil
}

// seedHosts publishes a day of minutes of two boxes the seeded serve isn't:
// box-a (Linux, 16 cores, pressure rising with its load) and mac-b (macOS,
// 10 cores, its pressure level warn for the busiest hour). Each agent uses
// CPU and memory only inside its working span (minutes ago).
func seedHosts(ctx context.Context, c *mesh.Client, now time.Time) error {
	r := rand.New(rand.NewPCG(117, 1))
	type agent struct {
		name     string
		from, to float64 // minutes ago
		cpu      float64 // cores at its busiest
		rss      int64
	}
	boxes := []struct {
		host, os string
		cores    int
		total    int64
		agents   []agent
	}{
		{"box-a", "linux", 16, 32 << 30, []agent{
			{"app-0042", 330, 0, 6, 5 << 30}, {"app-0042.api", 305, 200, 3, 2 << 30}, {"app-0041", 300, 0, 4, 3 << 30},
			{"app-0045", 70, 0, 9, 4 << 30}, {"routine.review.opus-5-5-1", 150, 28, 1.5, 1 << 30}}},
		{"mac-b", "darwin", 10, 16 << 30, []agent{
			{"app-0039", 245, 90, 3, 2 << 30}, {"session.seed.ec03a02c-5eed-4a1b-9c0d-000000000001", 335, 0, 1, 1 << 30}}},
	}
	start := now.Truncate(time.Minute)
	for _, b := range boxes {
		for m := 24 * 60; m >= 1; m-- {
			at := start.Add(-time.Duration(m) * time.Minute)
			wave := 0.6 + 0.4*math.Sin(float64(m)/37)
			mm := hoststat.Minute{Host: b.host, OS: b.os, At: at.UTC().Format(time.RFC3339), Cores: b.cores,
				MemTotal: b.total, Agents: map[string]hoststat.Use{}}
			var cpu float64
			var mem int64
			for _, a := range b.agents {
				if float64(m) > a.from || float64(m) < a.to {
					continue
				}
				u := hoststat.Use{CPU: math.Round(a.cpu*wave*(0.7+0.6*r.Float64())*100) / 100, RSS: a.rss + int64(r.IntN(1<<28))}
				mm.Agents[a.name] = u
				cpu, mem = cpu+u.CPU, mem+u.RSS
			}
			if limit := float64(b.cores) * 0.88; cpu > limit { // a box can't run more than its cores
				for a, u := range mm.Agents {
					u.CPU = math.Round(u.CPU*limit/cpu*100) / 100
					mm.Agents[a] = u
				}
				cpu = limit
			}
			other := hoststat.Use{CPU: math.Round((0.3+r.Float64()*0.8)*100) / 100, RSS: 3<<30 + int64(r.IntN(1<<29))}
			mm.Agents[hoststat.Other] = other
			mm.CPU, mm.MemUsed = math.Round((cpu+other.CPU)*100)/100, mem+other.RSS
			mm.Load1 = math.Round(mm.CPU*(1+0.3*r.Float64())*100) / 100
			if b.os == "linux" {
				over := math.Max(0, mm.CPU-float64(b.cores)*0.5) / float64(b.cores) * 100
				cp, mp, ip := math.Round(math.Min(over*2, 60)*100)/100, 0.0, math.Round(r.Float64()*3*100)/100
				// Memory stalls every task for a short stretch, and less for a longer one.
				full := 0.0
				switch {
				case m >= 400 && m < 410:
					full = 6
				case m >= 200 && m < 215:
					full = 1.5
				}
				mp += full * 2
				mm.Pressure = &hoststat.Pressure{CPU: &cp, Mem: &mp, IO: &ip, MemFull: &full}
				mm.SwapUsed = 256 << 20
				// The queued share rises as the box's CPU passes half its cores,
				// and the newest minutes queue hard, so the head line has a share
				// now; run is its CPU over the minute.
				share := math.Min(0.5, over/100*1.4)
				if m <= 3 {
					share = math.Max(share, 0.3)
				}
				run := math.Round(mm.CPU*60*100) / 100
				wait := math.Round(run*share/(1-share)*100) / 100
				mm.Run, mm.Wait = &run, &wait
				for _, ag := range b.agents { // the slice, not the map: the seed's draws stay in order
					a := ag.name
					u, ok := mm.Agents[a]
					if !ok || strings.HasPrefix(a, "routine.") { // a routine's processes exit between reads: under half coverage
						continue
					}
					s := math.Min(0.6, share*(0.6+0.8*r.Float64()))
					ar := math.Round(u.CPU*60*0.9*100) / 100
					aw := math.Round(ar*s/(1-s)*100) / 100
					u.Run, u.Wait = &ar, &aw
					mm.Agents[a] = u
				}
			} else {
				lvl := 1
				if m >= 120 && m < 180 {
					lvl = 2
				}
				mm.MacPressure = &lvl
				mm.SwapUsed = 1 << 30
			}
			if err := hoststat.Publish(ctx, c, &mm); err != nil {
				return err
			}
		}
	}
	return nil
}

// landRun is one land run to publish: its lead, start and events, each an
// offset from the start in seconds, its step and text, and its command,
// jobs and end.
type landRun struct {
	path string
	run  time.Time
	evs  []landStep
}

type landStep struct {
	sec        float64
	step, text string
	cmd        *mesh.LandCmd
	jobs       []mesh.LandJob
	end        string
}

// publish sends the run's land events, each step's since the time of its
// first event in a row, the command's words on its first event only.
func (lr landRun) publish(ctx context.Context, c *mesh.Client) error {
	at := func(sec float64) string {
		return lr.run.Add(time.Duration(sec * float64(time.Second))).UTC().Format(time.RFC3339)
	}
	since, said := "", map[string]bool{}
	for i, e := range lr.evs {
		if i == 0 || lr.evs[i-1].step != e.step {
			since = at(e.sec)
		}
		l := &mesh.Land{Run: at(0), Step: e.step, Since: since, Jobs: e.jobs, End: e.end}
		if e.cmd != nil {
			cmd := *e.cmd
			if said[cmd.Since] {
				cmd.Command = ""
			}
			said[cmd.Since] = true
			l.Cmd = &cmd
		}
		if err := mesh.PublishEvent(ctx, c, mesh.Event{From: lr.path, Event: "land", Text: e.text, At: at(e.sec), Land: l}); err != nil {
			return err
		}
	}
	return nil
}

// seedLanding publishes the land runs the map's landing views draw:
// app-0041's two (verify, then a timeout waiting for review job 9f2kQ;
// then the wait again while routine.review.opus-5-5-2 runs it, with its
// model calls), app-0043 mid-verify with output lines, and three landed
// runs of app-batch-1 three days ago, so each step has its usual time.
func seedLanding(ctx context.Context, c *mesh.Client, now time.Time) error {
	ago := func(sec float64) time.Time { return now.Add(-time.Duration(sec * float64(time.Second))) }
	rfc := func(t time.Time) string { return t.UTC().Format(time.RFC3339) }
	var runs []landRun
	// app-batch-1's landings, each ending at its done.
	for i, l := range []struct {
		path          string
		done, verify  float64
		lease, rebase float64
	}{{"app-0030", batch1 - 95, 660, 4, 3}, {"app-0031", batch1 - 130, 720, 95, 6}, {"app-0033", batch1 - 240, 610, 2, 4}} {
		t := l.verify + 30 + l.lease + l.rebase + 90 + 20
		start := ago(float64(l.done)*60 + t)
		vs := rfc(start.Add(time.Second))
		v := l.verify
		runs = append(runs, landRun{path: l.path, run: start, evs: []landStep{
			{sec: 0, step: "checks", text: "checking"},
			{sec: 1, step: "kinds", text: "running verify on t:1a2b3c4" + fmt.Sprint(i), cmd: &mesh.LandCmd{Kind: "verify", Command: "make test", Since: vs}},
			{sec: 1 + v, step: "jobs", text: "review: on the branch already (ok at t:1a2b3c4" + fmt.Sprint(i) + ")"},
			{sec: 30 + v, step: "lease", text: "taking land.app"},
			{sec: 30 + v + l.lease, step: "rebase", text: "rebasing onto 5e6f7a8b"},
			{sec: 30 + v + l.lease + l.rebase, step: "results", text: "running verify on the rebased tree t:9c8d7e6f", cmd: &mesh.LandCmd{Kind: "verify", Command: "make test", Since: rfc(start.Add(time.Duration(30+v+l.lease+l.rebase) * time.Second))}},
			{sec: 120 + v + l.lease + l.rebase, step: "push", text: "pushing 0123abcd"},
			{sec: t, step: "push", text: "pushing 0123abcd", end: "landed 0123abcd"},
		}})
	}
	// app-0041: ready 20 minutes ago. Run 1 verifies for 12m and times out
	// waiting for the review it queued; the daemon runs land again, which
	// waits for that job, running on opus-5-5-2.
	r1 := ago(19*60 + 55)
	gradle := &mesh.LandCmd{Kind: "verify", Command: "./gradlew --no-daemon -q assembleDebug testDebugUnitTest && tools/hid-rig/run.sh", Since: rfc(r1.Add(time.Second))}
	line := func(cmd *mesh.LandCmd, last string, lines int, at time.Time) *mesh.LandCmd {
		c := *cmd
		c.Last, c.LastAt, c.Lines = last, rfc(at), lines
		return &c
	}
	waiting := []mesh.LandJob{{ID: "9f2kQ", Kind: "review", State: "waiting"}}
	running := []mesh.LandJob{{ID: "9f2kQ", Kind: "review", State: "running", Host: "seed"}}
	run1 := []landStep{{sec: 0, step: "checks", text: "checking"}, {sec: 1, step: "kinds", text: "running verify on t:ca2ec50d", cmd: gradle}}
	for m := 1; m <= 12; m++ {
		run1 = append(run1, landStep{sec: float64(1 + 60*m), step: "kinds", text: "running verify on t:ca2ec50d",
			cmd: line(gradle, fmt.Sprintf("PASS case %d", m), 2*m, r1.Add(time.Duration(60*m-5)*time.Second))})
	}
	run1 = append(run1,
		landStep{sec: 749, step: "jobs", text: "waiting for job 9f2kQ (review, waiting)", jobs: waiting},
		landStep{sec: 750, step: "jobs", text: "waiting for job 9f2kQ (review, waiting)", jobs: waiting, end: "timed out waiting for job 9f2kQ (review)"})
	runs = append(runs, landRun{path: "app-0041", run: r1, evs: run1})
	r2 := ago(7*60 + 20)
	run2 := []landStep{{sec: 0, step: "checks", text: "checking"}, {sec: 1, step: "kinds", text: "checking verify on t:ca2ec50d"},
		{sec: 2, step: "jobs", text: "waiting for job 9f2kQ (review, waiting)", jobs: waiting},
		{sec: 25, step: "jobs", text: "waiting for job 9f2kQ (review, running on seed)", jobs: running}}
	for s := 85.0; s < 7*60; s += 60 {
		run2 = append(run2, landStep{sec: s, step: "jobs", text: "waiting for job 9f2kQ (review, running on seed)", jobs: running})
	}
	runs = append(runs, landRun{path: "app-0041", run: r2, evs: run2})
	// app-0043: mid-verify, its newest line 12 seconds old.
	r3 := ago(5*60 + 55)
	vcmd := &mesh.LandCmd{Kind: "verify", Command: "./gradlew --no-daemon -q assembleDebug testDebugUnitTest && tools/hid-rig/run.sh", Since: rfc(r3.Add(2 * time.Second))}
	run3 := []landStep{{sec: 0, step: "checks", text: "checking"}, {sec: 2, step: "kinds", text: "running verify on t:4208beec", cmd: vcmd}}
	for m, last := range []string{"> Task :app:assembleDebug", "PASS switch to android", "PASS switch to keyboard", "PASS switch to gamepad", "PASS switch to web_buttons"} {
		sec := float64(62 + 60*m)
		if m == 4 {
			sec = 5*60 + 45
		}
		run3 = append(run3, landStep{sec: sec, step: "kinds", text: "running verify on t:4208beec",
			cmd: line(vcmd, last, 4+3*m+1, r3.Add(time.Duration(sec-12)*time.Second))})
	}
	runs = append(runs, landRun{path: "app-0043", run: r3, evs: run3})
	for _, lr := range runs {
		if err := lr.publish(ctx, c); err != nil {
			return err
		}
	}
	// The review instance takes 9f2kQ and calls its model until 10 seconds ago.
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "routine.review.opus-5-5-2", Event: "progress", Text: "job 9f2kQ started",
		To: "app-0041", At: rfc(ago(7*60 - 5))}); err != nil {
		return err
	}
	if _, err := mesh.WriteState(ctx, c, "routine.review.opus-5-5-2", mesh.State{State: "working", By: "seed", At: rfc(ago(7*60 - 5))}); err != nil {
		return err
	}
	r := rand.New(rand.NewPCG(122, 1))
	ts := spread(r, 29, ago(7*60-10), ago(10))
	ts[28] = ago(10)
	t := thread{subj: "bmusage.node.routine.review.opus-5-5-2", agent: "routine.review.opus-5-5-2", role: "routine", repo: "app",
		session: "5eed0122-0000-4000-8000-000000000001", file: "-home-seed-app/routine.review.opus-5-5-2.jsonl"}
	t.calls = calls(r, "r0122", "", ts, ctxPath(r, 29, 30000, 139000, 0, 0), nil)
	t.calls[28].Cause = "Bash: Find animations in surface files"
	return t.publish(ctx, c)
}
