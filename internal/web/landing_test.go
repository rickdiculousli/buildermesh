package web

import (
	"context"
	"fmt"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/usage"
)

var landNow = time.Date(2026, 11, 24, 4, 0, 0, 0, time.UTC)

// lt is 11-24 at hh:mm:ss.
func lt(h, m, s int) time.Time { return time.Date(2026, 11, 24, h, m, s, 0, time.UTC) }

// lstep is one land event of a run: its time, step, text, command, jobs
// and end.
type lstep struct {
	at         time.Time
	step, text string
	cmd        *mesh.LandCmd
	jobs       []mesh.LandJob
	end        string
}

// publishRun publishes path's land events of the run started at run, each
// step's since the at of its first event in a row.
func publishRun(t *testing.T, ctx context.Context, c *mesh.Client, path string, run time.Time, steps []lstep) {
	t.Helper()
	since := ""
	for i, s := range steps {
		if i == 0 || steps[i-1].step != s.step {
			since = s.at.Format(time.RFC3339)
		}
		l := &mesh.Land{Run: run.Format(time.RFC3339), Step: s.step, Since: since, Cmd: s.cmd, Jobs: s.jobs, End: s.end}
		if err := mesh.PublishEvent(ctx, c, mesh.Event{From: path, Event: "land", Text: s.text, At: s.at.Format(time.RFC3339), Land: l}); err != nil {
			t.Fatal(err)
		}
	}
}

func publish(t *testing.T, ctx context.Context, c *mesh.Client, evs ...mesh.Event) {
	t.Helper()
	for _, ev := range evs {
		if err := mesh.PublishEvent(ctx, c, ev); err != nil {
			t.Fatal(err)
		}
	}
}

func landMesh(t *testing.T, nodes []mesh.Node, states map[string]string) (*httptest.Server, context.Context, *mesh.Client) {
	t.Helper()
	ctx, c := connect(t)
	for _, n := range nodes {
		if err := mesh.PutNode(ctx, c, n); err != nil {
			t.Fatal(err)
		}
	}
	for p, st := range states {
		meshtest.ForceState(t, c, p, mesh.State{State: st})
	}
	srv := httptest.NewServer(Handler(c, time.Hour, Options{Host: "k16", Now: func() time.Time { return landNow }}))
	t.Cleanup(srv.Close)
	return srv, ctx, c
}

func labels(r LandRun) []string {
	out := []string{}
	for _, s := range r.Steps {
		out = append(out, s.Label)
	}
	return out
}

// A landing of two runs, the first ended by a timeout waiting for the
// review: both runs show, the review job carries the instance that took it,
// its box, model and effort, and the calls it made between its start and
// end; the landing lists while it runs.
func TestLandingTwoRunsAndItsReview(t *testing.T) {
	srv, ctx, c := landMesh(t, []mesh.Node{
		{Path: "app-1", Role: "lead", Repo: "app", Host: "k16"},
		{Path: "routine.review.k16-1", Role: "routine", Repo: "app", Host: "k16", Model: "claude-opus-5-5"},
	}, map[string]string{"app-1": "landing"})
	meshtest.QueueJob(t, c, "review", "app-1", mesh.Job{ID: "J1", Repo: "app", Effort: "high"})
	j1 := []mesh.LandJob{{ID: "J1", Kind: "review", State: "waiting"}}
	publish(t, ctx, c, mesh.Event{From: "app-1", Event: "ready", Text: "ready to land", At: lt(3, 38, 0).Format(time.RFC3339)})
	publishRun(t, ctx, c, "app-1", lt(3, 38, 5), []lstep{
		{at: lt(3, 38, 5), step: "checks", text: "checking"},
		{at: lt(3, 38, 6), step: "kinds", text: "running verify on t:ca2ec50d", cmd: &mesh.LandCmd{Kind: "verify", Command: "make test", Since: lt(3, 38, 6).Format(time.RFC3339)}},
		{at: lt(3, 50, 30), step: "jobs", text: "waiting for job J1 (review, waiting)", jobs: j1},
		{at: lt(3, 50, 34), step: "jobs", text: "waiting for job J1 (review, waiting)", jobs: j1, end: "timed out waiting for job J1 (review)"},
	})
	publishRun(t, ctx, c, "app-1", lt(3, 50, 40), []lstep{
		{at: lt(3, 50, 40), step: "checks", text: "checking"},
		{at: lt(3, 50, 41), step: "kinds", text: "checking verify on t:ca2ec50d"},
		{at: lt(3, 50, 42), step: "jobs", text: "waiting for job J1 (review, running on k16)", jobs: []mesh.LandJob{{ID: "J1", Kind: "review", State: "running", Host: "k16"}}},
		{at: lt(3, 59, 30), step: "jobs", text: "waiting for job J1 (review, running on k16)", jobs: []mesh.LandJob{{ID: "J1", Kind: "review", State: "running", Host: "k16"}}},
	})
	publish(t, ctx, c,
		mesh.Event{From: "routine.review.k16-1", Event: "progress", Text: "job J1 started", To: "app-1", At: lt(3, 50, 31).Format(time.RFC3339)},
		mesh.Event{From: "routine.review.k16-1", Event: "progress", Text: "job J1 ok 330000 ctx 61000", To: "app-1", At: lt(3, 56, 1).Format(time.RFC3339)})
	r := func(at time.Time, ctx int64, cause string) usage.Call {
		return usage.Call{At: at.Format(time.RFC3339Nano), ID: at.Format("150405"), CR: ctx, Ctx: ctx, Cause: cause}
	}
	publishUsage(t, ctx, c, usage.Message{Agent: "routine.review.k16-1", Role: "routine", Host: "k16", Repo: "app", Calls: []usage.Call{
		r(lt(3, 49, 0), 9000, "an earlier job"), r(lt(3, 51, 0), 20000, "message"), r(lt(3, 53, 0), 40000, "Read cmd_land.go"),
		r(lt(3, 55, 0), 61000, "Bash: Find animations in surface files"), r(lt(3, 57, 0), 5000, "the next job")}})

	var v LandingView
	getJSON(t, srv, "/api/landing?lead=app-1", 200, &v)
	if v.ReadyAt != "2026-11-24T03:38:00Z" || !v.Running || v.State != "landing" || len(v.Runs) != 2 {
		t.Fatalf("landing: ready_at %q running %v state %q runs %d", v.ReadyAt, v.Running, v.State, len(v.Runs))
	}
	if r := v.Runs[0]; r.End != "timed out waiting for job J1 (review)" || !reflect.DeepEqual(labels(r), []string{"checks", "verify", "review"}) ||
		r.Steps[2].To != "2026-11-24T03:50:34Z" || r.Steps[1].Text != "running verify on t:ca2ec50d" {
		t.Fatalf("run 1: %+v", r)
	}
	if r := v.Runs[1]; r.End != "" || r.Quiet != 0 || !reflect.DeepEqual(labels(r), []string{"checks", "verify", "review"}) || r.Steps[2].To != "" {
		t.Fatalf("run 2: %+v", r)
	}
	if v.Now == nil || v.Now.Step != "jobs" || v.Text != "waiting for job J1 (review, running on k16)" {
		t.Fatalf("now %+v text %q", v.Now, v.Text)
	}
	want := LandingJob{ID: "J1", Kind: "review", State: "ok", Instance: "routine.review.k16-1", Host: "k16", Model: "claude-opus-5-5",
		Effort: "high", Started: "2026-11-24T03:50:31Z", Ended: "2026-11-24T03:56:01Z", Ms: 330000, Calls: 3, Read: 121000, Ctx: 61000,
		Cause: "Bash: Find animations in surface files", CauseAt: "2026-11-24T03:55:00Z"}
	if len(v.Jobs) != 1 || v.Jobs[0] != want {
		t.Fatalf("jobs %+v\nwant %+v", v.Jobs, want)
	}
	// One review job in the repo: no usual yet.
	if v.ReviewUsual != nil {
		t.Fatalf("review usual %+v from one job", v.ReviewUsual)
	}
	var all []LandingView
	getJSON(t, srv, "/api/landing", 200, &all)
	if len(all) != 1 || all[0].Path != "app-1" {
		t.Fatalf("landings %+v", all)
	}
}

// A job no instance has taken reads waiting.
func TestLandingJobWaiting(t *testing.T) {
	srv, ctx, c := landMesh(t, []mesh.Node{{Path: "app-1", Role: "lead", Repo: "app", Host: "k16"}}, map[string]string{"app-1": "landing"})
	publishRun(t, ctx, c, "app-1", lt(3, 58, 0), []lstep{
		{at: lt(3, 58, 0), step: "checks", text: "checking"},
		{at: lt(3, 59, 0), step: "jobs", text: "waiting for job J2 (review, waiting)", jobs: []mesh.LandJob{{ID: "J2", Kind: "review", State: "waiting"}}},
	})
	var v LandingView
	getJSON(t, srv, "/api/landing?lead=app-1", 200, &v)
	if len(v.Jobs) != 1 || v.Jobs[0].State != "waiting" || v.Jobs[0].Instance != "" || v.ReadyAt != "" {
		t.Fatalf("jobs %+v ready_at %q", v.Jobs, v.ReadyAt)
	}
}

// A run with no end and no event for longer than two heartbeats reads
// quiet with its age, its command carried from the event it began on (land
// sends it once); the overview lists it while its lead is landing, and
// not once the lead is working.
func TestLandingQuiet(t *testing.T) {
	srv, ctx, c := landMesh(t, []mesh.Node{
		{Path: "app-2", Role: "lead", Repo: "app", Host: "k16"},
		{Path: "app-3", Role: "lead", Repo: "app", Host: "k16"},
	}, map[string]string{"app-2": "landing", "app-3": "working"})
	for _, p := range []string{"app-2", "app-3"} {
		publish(t, ctx, c, mesh.Event{From: p, Event: "ready", At: lt(3, 0, 0).Format(time.RFC3339)})
		publishRun(t, ctx, c, p, lt(3, 1, 0), []lstep{
			{at: lt(3, 1, 0), step: "checks", text: "checking"},
			{at: lt(3, 1, 1), step: "kinds", text: "running verify on t:ab", cmd: &mesh.LandCmd{Kind: "verify", Command: "make test", Since: lt(3, 1, 1).Format(time.RFC3339)}},
			{at: lt(3, 50, 0), step: "kinds", text: "running verify on t:ab", cmd: &mesh.LandCmd{Kind: "verify", Since: lt(3, 1, 1).Format(time.RFC3339), Last: "PASS x", Lines: 3}},
		})
	}
	var v LandingView
	getJSON(t, srv, "/api/landing?lead=app-2", 200, &v)
	if v.Running || len(v.Runs) != 1 || v.Runs[0].Quiet != 600 || v.Now.Cmd.Last != "PASS x" || v.Now.Cmd.Command != "make test" {
		t.Fatalf("quiet: running %v runs %+v", v.Running, v.Runs)
	}
	var all []LandingView
	getJSON(t, srv, "/api/landing", 200, &all)
	if len(all) != 1 || all[0].Path != "app-2" {
		t.Fatalf("landings %+v", all)
	}
}

// A lead landing in its own turn stays working: its run lists, and its
// landing begins with that run, not its old ready, whose run was refused.
// A land before 121 has no land object: its text alone, no runs.
func TestLandingOwnTurnAndBefore121(t *testing.T) {
	srv, ctx, c := landMesh(t, []mesh.Node{
		{Path: "app-4", Role: "lead", Repo: "app", Host: "k16"},
		{Path: "app-5", Role: "lead", Repo: "app", Host: "k16"},
		{Path: "app-6", Role: "lead", Repo: "app", Host: "k16"},
	}, map[string]string{"app-4": "working", "app-5": "landing", "app-6": "working"})
	publish(t, ctx, c, mesh.Event{From: "app-4", Event: "ready", At: lt(2, 0, 0).Format(time.RFC3339)})
	publishRun(t, ctx, c, "app-4", lt(2, 1, 0), []lstep{
		{at: lt(2, 1, 0), step: "checks", text: "checking"},
		{at: lt(2, 10, 0), step: "kinds", text: "running verify on t:ab", end: "verify failed at t:ab"},
	})
	publishRun(t, ctx, c, "app-4", lt(3, 59, 0), []lstep{{at: lt(3, 59, 0), step: "checks", text: "checking"}})
	publish(t, ctx, c, mesh.Event{From: "app-5", Event: "land", Text: "running the inline kinds on t:4208beec", At: lt(3, 59, 0).Format(time.RFC3339)})

	var v LandingView
	getJSON(t, srv, "/api/landing?lead=app-4", 200, &v)
	if v.ReadyAt != "" || !v.Running || len(v.Runs) != 1 || v.Runs[0].Run != "2026-11-24T03:59:00Z" || v.State != "working" {
		t.Fatalf("own turn: %+v", v)
	}
	var old LandingView
	getJSON(t, srv, "/api/landing?lead=app-5", 200, &old)
	if old.Now != nil || len(old.Runs) != 0 || old.Text != "running the inline kinds on t:4208beec" || !old.Running {
		t.Fatalf("before 121: %+v", old)
	}
	var all []LandingView
	getJSON(t, srv, "/api/landing", 200, &all)
	if len(all) != 2 || all[0].Path != "app-4" || all[1].Path != "app-5" {
		t.Fatalf("landings %+v", all)
	}
	getJSON(t, srv, "/api/landing?lead=app-6", 404, nil)
	getJSON(t, srv, "/api/landing?lead=app-7", 404, nil)
}

// The usual time of a step and label is the median over the repo's runs
// that went past it, a step taken twice in a run summed; a step with two
// samples (main, waited in two runs, only fetched in the third) has none;
// a kinds row that ran nothing (answered from the record) is no sample;
// the review's is the median of the repo's review jobs. While a run waits
// for the lease, the holder and its newest land text show.
func TestLandingUsualAndLease(t *testing.T) {
	srv, ctx, c := landMesh(t, []mesh.Node{
		{Path: "lib-1", Role: "lead", Repo: "lib"}, {Path: "lib-2", Role: "lead", Repo: "lib"}, {Path: "lib-3", Role: "lead", Repo: "lib"},
		{Path: "lib-4", Role: "lead", Repo: "lib"},
		{Path: "lib-8", Role: "lead", Repo: "lib"}, {Path: "lib-9", Role: "lead", Repo: "lib"},
		{Path: "routine.review.r1", Role: "routine", Repo: "lib"}, {Path: "other-1", Role: "lead", Repo: "other"},
	}, map[string]string{"lib-8": "landing", "lib-9": "landing"})
	cmd := func(at time.Time) *mesh.LandCmd { return &mesh.LandCmd{Kind: "verify", Since: at.Format(time.RFC3339)} }
	// lib-1 runs verify twice (100s and 200s), its main only fetched.
	b := lt(1, 0, 0)
	s := func(sec int) time.Time { return b.Add(time.Duration(sec) * time.Second) }
	publishRun(t, ctx, c, "lib-1", b, []lstep{
		{at: s(0), step: "checks", text: "checking"},
		{at: s(10), step: "kinds", text: "running verify", cmd: cmd(s(10))},
		{at: s(110), step: "jobs", text: "review: none (size S)"},
		{at: s(115), step: "checks", text: "checking"},
		{at: s(120), step: "kinds", text: "running verify", cmd: cmd(s(120))},
		{at: s(320), step: "main", text: "fetching origin/main"},
		{at: s(321), step: "lease", text: "taking land.lib"},
		{at: s(322), step: "push", text: "pushing abcd1234"},
		{at: s(330), step: "push", text: "pushing abcd1234", end: "landed abcd1234"},
	})
	for i, verify := range []int{700, 800} {
		p, b := []string{"lib-2", "lib-3"}[i], lt(2, i*10, 0)
		s := func(sec int) time.Time { return b.Add(time.Duration(sec) * time.Second) }
		publishRun(t, ctx, c, p, b, []lstep{
			{at: s(0), step: "checks", text: "checking"},
			{at: s(10), step: "kinds", text: "running verify", cmd: cmd(s(10))},
			{at: s(10 + verify), step: "main", text: "waiting for main in /m to be pushed"},
			{at: s(20 + verify), step: "lease", text: "taking land.lib"},
			{at: s(21 + verify), step: "push", text: "pushing abcd1234"},
			{at: s(30 + verify), step: "push", text: "pushing abcd1234", end: "landed abcd1234"},
		})
	}
	// lib-4's rerun answers verify from the record in a second: no sample.
	publishRun(t, ctx, c, "lib-4", lt(2, 50, 0), []lstep{
		{at: lt(2, 50, 0), step: "checks", text: "checking"},
		{at: lt(2, 50, 1), step: "kinds", text: "checking verify on t:ab"},
		{at: lt(2, 50, 2), step: "jobs", text: "waiting for job J9 (review, waiting)", jobs: []mesh.LandJob{{ID: "J9", Kind: "review", State: "waiting"}}},
		{at: lt(2, 51, 0), step: "jobs", text: "waiting for job J9 (review, waiting)", end: "timed out waiting for job J9 (review)"},
	})
	// Another repo's run is not lib's.
	publishRun(t, ctx, c, "other-1", lt(2, 30, 0), []lstep{
		{at: lt(2, 30, 0), step: "kinds", text: "running verify", cmd: cmd(lt(2, 30, 0))},
		{at: lt(2, 31, 0), step: "push", text: "pushing", end: "landed abcd1234"},
	})
	for i, ms := range []int{300000, 400000, 500000} {
		publish(t, ctx, c, mesh.Event{From: "routine.review.r1", Event: "progress", Text: fmt.Sprintf("job R%d ok %d", i, ms), At: lt(2, 40+i, 0).Format(time.RFC3339)})
	}
	publishRun(t, ctx, c, "lib-8", lt(3, 58, 0), []lstep{{at: lt(3, 59, 0), step: "rebase", text: "rebasing onto 1234abcd"}})
	publishRun(t, ctx, c, "lib-9", lt(3, 59, 0), []lstep{
		{at: lt(3, 59, 0), step: "checks", text: "checking"},
		{at: lt(3, 59, 10), step: "lease", text: "waiting for land.lib (held by lib-8)"},
	})

	var v LandingView
	getJSON(t, srv, "/api/landing?lead=lib-9", 200, &v)
	want := []Usual{
		{Step: "checks", Label: "checks", Secs: 10, N: 5}, // lib-1's 10 + 5, lib-2, lib-3 and lib-9's 10, lib-4's 1
		{Step: "kinds", Label: "verify", Secs: 700, N: 3},
		{Step: "lease", Label: "lease", Secs: 1, N: 3},
		{Step: "push", Label: "push", Secs: 9, N: 3},
	}
	if !reflect.DeepEqual(v.Usual, want) {
		t.Fatalf("usual %+v\nwant %+v", v.Usual, want)
	}
	if v.ReviewUsual == nil || *v.ReviewUsual != (Usual{Secs: 400, N: 3}) {
		t.Fatalf("review usual %+v", v.ReviewUsual)
	}
	if v.Lease == nil || *v.Lease != (LandingLease{Holder: "lib-8", Text: "rebasing onto 1234abcd"}) {
		t.Fatalf("lease %+v", v.Lease)
	}
}

// The readies a landing begins from: one not started yet reads waiting
// (not a land before 121); one the daemon withdrew starts no later landing,
// so a run after it is the lead's own turn. A command that waited for its
// slot keeps its words once its since moves.
func TestLandingReadiesAndSlotWait(t *testing.T) {
	srv, ctx, c := landMesh(t, []mesh.Node{
		{Path: "app-1", Role: "lead", Repo: "app"}, {Path: "app-2", Role: "lead", Repo: "app"}, {Path: "app-3", Role: "lead", Repo: "app"},
	}, map[string]string{"app-1": "held", "app-2": "working", "app-3": "landing"})
	// app-1: an old land before 121, then a ready the operator holds.
	publish(t, ctx, c,
		mesh.Event{From: "app-1", Event: "land", Text: "old text", At: lt(1, 0, 0).Format(time.RFC3339)},
		mesh.Event{From: "app-1", Event: "ready", At: lt(3, 50, 0).Format(time.RFC3339)},
		mesh.Event{From: "app-2", Event: "ready", At: lt(2, 0, 0).Format(time.RFC3339)},
		mesh.Event{From: "daemon.k16", Event: "land-withdrawn", To: "app-2", Text: "ready withdrawn: a message came", At: lt(2, 1, 0).Format(time.RFC3339)})
	publishRun(t, ctx, c, "app-2", lt(3, 59, 0), []lstep{{at: lt(3, 59, 0), step: "checks", text: "checking"}})
	t0, t1 := lt(3, 58, 0).Format(time.RFC3339), lt(3, 58, 40).Format(time.RFC3339)
	publishRun(t, ctx, c, "app-3", lt(3, 57, 0), []lstep{
		{at: lt(3, 57, 0), step: "checks", text: "checking"},
		{at: lt(3, 58, 0), step: "kinds", text: "waiting for the emulator slot for verify",
			cmd: &mesh.LandCmd{Kind: "verify", Command: "make test", Since: t0, Wait: &mesh.LandWait{Slot: "emulator", Holder: "app-9", Place: 1}}},
		{at: lt(3, 58, 40), step: "kinds", text: "running verify on t:ab", cmd: &mesh.LandCmd{Kind: "verify", Since: t1}},
		{at: lt(3, 59, 40), step: "kinds", text: "running verify on t:ab", cmd: &mesh.LandCmd{Kind: "verify", Since: t1, Last: "ok", Lines: 1}},
	})

	var held LandingView
	getJSON(t, srv, "/api/landing?lead=app-1", 200, &held)
	if !held.Waiting || held.Now != nil || held.Text != "" || held.ReadyAt != "2026-11-24T03:50:00Z" || len(held.Runs) != 0 {
		t.Fatalf("waiting: %+v", held)
	}
	var own LandingView
	getJSON(t, srv, "/api/landing?lead=app-2", 200, &own)
	if own.ReadyAt != "" || own.Waiting || len(own.Runs) != 1 || own.Runs[0].Run != "2026-11-24T03:59:00Z" {
		t.Fatalf("own turn after a withdrawn ready: %+v", own)
	}
	var slot LandingView
	getJSON(t, srv, "/api/landing?lead=app-3", 200, &slot)
	if slot.Now == nil || slot.Now.Cmd == nil || slot.Now.Cmd.Command != "make test" || slot.Now.Cmd.Since != t1 {
		t.Fatalf("command after the slot wait: %+v", slot.Now)
	}
}
