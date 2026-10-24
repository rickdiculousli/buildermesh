package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

// landFix is a lead app-080 on the test host, landing: a bare origin, the
// main checkout and the lead's worktree with one commit of its own, spec
// 080 accepted on main and done on the lead's branch, the request in
// bm-ready, and the steps through RunStep.
type landFix struct {
	*fixture
	origin, repo, wt, head string

	act *Activity // shared across passes, as one daemon's is

	mu    sync.Mutex
	calls []string // each step's command: land, done <text>, held
	// land is what the fake land does; nil pushes the change as land would.
	land func(ctx context.Context) (string, int)
}

func newLandFix(t *testing.T) *landFix {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
	} {
		t.Setenv(k, v)
	}
	poll := stepPoll
	stepPoll = 20 * time.Millisecond
	t.Cleanup(func() { stepPoll = poll })
	f := &landFix{fixture: setup(t), act: NewActivity()}
	root := t.TempDir()
	f.origin, f.repo, f.wt = filepath.Join(root, "origin.git"), filepath.Join(root, "repo"), filepath.Join(root, "wt")
	meshtest.Git(t, root, "init", "--bare", f.origin)
	meshtest.Git(t, root, "init", "-b", "main", f.repo)
	meshtest.Git(t, f.repo, "remote", "add", "origin", f.origin)
	f.write(t, filepath.Join(f.repo, "docs", "proposals", "080-thing.md"), "---\nid: 80\nstatus: accepted\n---\nbody\n")
	meshtest.Git(t, f.repo, "add", "-A")
	meshtest.Git(t, f.repo, "commit", "-m", "base")
	meshtest.Git(t, f.repo, "push", "-q", "origin", "main")
	meshtest.Git(t, f.repo, "worktree", "add", "-q", "-b", "lead", f.wt, "main")
	f.write(t, filepath.Join(f.wt, "b.txt"), "b\n")
	f.write(t, filepath.Join(f.wt, "docs", "proposals", "080-thing.md"), "---\nid: 80\nstatus: done\n---\nbody\n")
	meshtest.Git(t, f.wt, "add", "-A")
	meshtest.Git(t, f.wt, "commit", "-m", "the work")
	f.head = f.git(t, "rev-parse", "HEAD")

	if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: "app-080", Role: "lead", Host: testHost, Spec: "080",
		RepoPath: f.repo, Worktree: f.wt, Runner: mesh.NodeRunner{Kind: "t3", Thread: "th-1"}}); err != nil {
		t.Fatal(err)
	}
	f.setState(t, "app-080", "working")
	f.setState(t, "app-080", "landing")
	if err := mesh.CreateReady(f.ctx, f.c, mesh.Ready{Path: "app-080", Spec: "080", Head: f.head, Text: "built it",
		At: f.now.UTC().Format(time.RFC3339), Phase: mesh.PhaseReady}); err != nil {
		t.Fatal(err)
	}
	f.d.RunStep = f.step
	return f
}

func (f *landFix) write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *landFix) git(t *testing.T, args ...string) string {
	t.Helper()
	out, err := gitOut(context.Background(), f.wt, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

// step is the RunStep hook: it records the command and plays it.
func (f *landFix) step(ctx context.Context, dir string, args ...string) (string, int, error) {
	if dir != f.wt || len(args) < 3 || args[0] != "--node" || args[1] != "app-080" {
		return "bad step " + dir + " " + strings.Join(args, " "), 2, nil
	}
	f.mu.Lock()
	land := f.land
	switch args[2] {
	case "done":
		f.calls = append(f.calls, "done "+args[len(args)-1])
	default:
		f.calls = append(f.calls, args[2])
	}
	f.mu.Unlock()
	switch args[2] {
	case "land":
		if land != nil {
			out, code := land(ctx)
			return out, code, nil
		}
		return f.landed(), 0, nil
	case "held":
		if _, err := mesh.WriteState(ctx, f.c, "app-080", mesh.State{State: "held", Text: "before land by operator", By: "node"}); err != nil {
			return err.Error(), 1, nil
		}
		return "held app-080", 0, nil
	case "done":
		ev := mesh.Event{From: "app-080", Event: "done", Text: args[len(args)-1], Commit: args[4]}
		if err := mesh.PublishEvent(ctx, f.c, ev); err != nil {
			return err.Error(), 1, nil
		}
		if _, err := mesh.WriteState(ctx, f.c, "app-080", mesh.State{State: "done", Text: ev.Text, By: "node"}); err != nil {
			return err.Error(), 1, nil
		}
		return "done app-080 " + short8(args[4]), 0, nil
	}
	return "unknown step", 2, nil
}

// landed does what land does on success: HEAD reworded to carry the Spec
// and Verified trailers, pushed to origin's main.
func (f *landFix) landed() string {
	ctx := context.Background()
	tree, _ := gitOut(ctx, f.wt, "rev-parse", "HEAD^{tree}")
	msg := "the work\n\nSpec: 080\nVerified: verify ok t:" + short8(tree)
	for _, args := range [][]string{{"commit", "--amend", "-q", "-m", msg}, {"push", "-q", "origin", "HEAD:main"}} {
		if out, err := gitOut(ctx, f.wt, args...); err != nil {
			return "git " + strings.Join(args, " ") + ": " + out
		}
	}
	rec, _ := gitOut(ctx, f.wt, "rev-parse", "HEAD")
	return "landed " + short8(rec)
}

func (f *landFix) steps() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.calls...)
}

func (f *landFix) pass() { LandPass(f.ctx, f.d, f.act) }

func (f *landFix) ready(t *testing.T) (mesh.Ready, bool) {
	t.Helper()
	r, _, ok, err := mesh.GetReady(f.ctx, f.c, "app-080")
	if err != nil {
		t.Fatal(err)
	}
	return r, ok
}

func (f *landFix) state(t *testing.T) mesh.State {
	t.Helper()
	s, _, err := mesh.ReadState(f.ctx, f.c, "app-080")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A ready lead is landed: land, then done with the lead's line and what
// origin/main reads after a fetch, then one final wake; the request ends,
// and a later pass does nothing.
func TestLandLandsAndWakesOnce(t *testing.T) {
	f := newLandFix(t)
	f.pass()
	rec := f.git(t, "rev-parse", "HEAD")
	main, _ := gitOut(f.ctx, f.origin, "rev-parse", "main")
	if main != rec {
		t.Fatalf("origin main %s, want %s", main, rec)
	}
	want := []string{"land", "done built it; landed " + rec[:8] + ", origin/main at " + rec[:8] + " contains it"}
	if got := f.steps(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("steps %q, want %q", got, want)
	}
	wake := "send th-1 " + LandedText(rec, "080", rec)
	if sends := f.sends(t); len(sends) != 1 || sends[0] != wake {
		t.Fatalf("sends %q", sends)
	}
	if !strings.Contains(wake, "bmesh: landed "+rec[:8]+", spec 080 done; origin/main at "+rec[:8]+" contains it. Write your closing line; nothing else to run.") {
		t.Fatalf("final wake %q", wake)
	}
	if _, ok := f.ready(t); ok {
		t.Fatal("the request outlived the final wake")
	}
	if st := f.state(t); st.State != "done" {
		t.Fatalf("state %+v", st)
	}
	f.pass()
	if len(f.steps()) != 2 || len(f.sends(t)) != 1 {
		t.Fatalf("a second pass acted: %q %q", f.steps(), f.sends(t))
	}
	assertNoClaims(t, f)
}

// A daemon that died after the push finds the change on main: it runs done
// once and sends the final wake once, and runs no land.
func TestLandAfterThePushRunsDoneOnce(t *testing.T) {
	f := newLandFix(t)
	f.landed()
	f.pass()
	f.pass()
	rec := f.git(t, "rev-parse", "HEAD")
	if got := f.steps(); len(got) != 1 || !strings.HasPrefix(got[0], "done built it; landed "+rec[:8]) {
		t.Fatalf("steps %q", got)
	}
	if sends := f.sends(t); len(sends) != 1 || !strings.Contains(sends[0], "landed "+rec[:8]) {
		t.Fatalf("sends %q", sends)
	}
}

// A land of the release before left its `NNN: done at` record commit
// pushed: it is found on main all the same, from the spec on origin/main.
func TestLandAfterAnOldPushRunsDoneOnce(t *testing.T) {
	f := newLandFix(t)
	ctx := context.Background()
	merged := f.git(t, "rev-parse", "HEAD")
	f.write(t, filepath.Join(f.wt, "docs", "proposals", "080-thing.md"), "---\nid: 80\nstatus: done\nmerged: "+merged+"\n---\nbody\n")
	for _, args := range [][]string{{"commit", "-qam", "080: done at " + short8(merged)}, {"push", "-q", "origin", "HEAD:main"}} {
		if out, err := gitOut(ctx, f.wt, args...); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	f.pass()
	rec := f.git(t, "rev-parse", "HEAD")
	if got := f.steps(); len(got) != 1 || !strings.HasPrefix(got[0], "done built it; landed "+rec[:8]) {
		t.Fatalf("steps %q", got)
	}
}

// One that died after done, before the wake, only wakes.
func TestLandAfterDoneOnlyWakes(t *testing.T) {
	f := newLandFix(t)
	r, rev, _, _ := mesh.GetReady(f.ctx, f.c, "app-080")
	r.Phase, r.Commit, r.Main = mesh.PhaseLanded, f.head, f.head
	if _, err := mesh.UpdateReady(f.ctx, f.c, r, rev); err != nil {
		t.Fatal(err)
	}
	f.pass()
	if len(f.steps()) != 0 || len(f.sends(t)) != 1 {
		t.Fatalf("steps %q sends %q", f.steps(), f.sends(t))
	}
}

// While one daemon's step runs under the claim, another (a restart beside
// a step not yet ended) starts none.
func TestLandClaimKeepsOneStep(t *testing.T) {
	f := newLandFix(t)
	started, release := make(chan struct{}), make(chan struct{})
	f.land = func(ctx context.Context) (string, int) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "bmesh: timed out waiting for job x (review); nothing is pushed", 5
	}
	done := make(chan struct{})
	go func() { defer close(done); f.pass() }()
	<-started
	f.pass() // the other daemon: the claim is held
	close(release)
	<-done
	if got := f.steps(); len(got) != 1 || got[0] != "land" {
		t.Fatalf("steps %q", got)
	}
	// Timed out: the request stays for the next pass, and nobody is woken.
	if r, ok := f.ready(t); !ok || r.Phase != mesh.PhaseReady || len(f.sends(t)) != 0 {
		t.Fatalf("request %+v %v, sends %q", r, ok, f.sends(t))
	}
	assertNoClaims(t, f)
}

// A refusal wakes the lead once with land's refusal and last lines; the
// request ends and the lead is working with the refusal.
func TestLandRefusalWakesOnce(t *testing.T) {
	f := newLandFix(t)
	out := "review fail t:1234abcd\n- the claim leaks\nbmesh: refused: review failed at t:1234abcd (job x); nothing is pushed: fix what it found, commit and run bmesh land again\n"
	f.land = func(context.Context) (string, int) { return out, 3 }
	f.pass()
	refusal := "refused: review failed at t:1234abcd (job x); nothing is pushed: fix what it found, commit and run bmesh land again"
	want := "send th-1 " + RefusedText(refusal, strings.TrimRight(out, "\n"))
	if sends := f.sends(t); len(sends) != 1 || sends[0] != want {
		t.Fatalf("sends %q\nwant %q", sends, want)
	}
	if !strings.Contains(want, "bmesh: land refused and needs you: refused: review failed") || !strings.Contains(want, "- the claim leaks") ||
		!strings.HasSuffix(want, "Fix it, commit, then run `bmesh ready` again.") {
		t.Fatalf("refused wake %q", want)
	}
	if _, ok := f.ready(t); ok {
		t.Fatal("the request outlived the refused wake")
	}
	if st := f.state(t); st.State != "working" || st.By != "daemon" || st.Text != "land refused: "+refusal {
		t.Fatalf("state %+v", st)
	}
	if evs := f.daemonEvents(t, "land-refused"); len(evs) != 1 || evs[0].To != "app-080" || evs[0].Text != refusal {
		t.Fatalf("land-refused events %+v", evs)
	}
	f.pass()
	if len(f.steps()) != 1 || len(f.sends(t)) != 1 {
		t.Fatalf("a second pass acted: %q %q", f.steps(), f.sends(t))
	}
}

// A running thread holds the refused wake for the next pass; land isn't
// run again meanwhile.
func TestLandRefusalWaitsForARunningThread(t *testing.T) {
	f := newLandFix(t)
	f.land = func(context.Context) (string, int) { return "bmesh: refused: rebase onto origin/main conflicts", 3 }
	f.rn.failSends(runner.ErrRunning)
	f.pass()
	if r, ok := f.ready(t); !ok || r.Phase != mesh.PhaseRefused || r.FailingSince != "" {
		t.Fatalf("request %+v %v", r, ok)
	}
	f.rn.failSends(nil)
	f.pass()
	if len(f.steps()) != 1 || len(f.sends(t)) != 1 {
		t.Fatalf("steps %q sends %q", f.steps(), f.sends(t))
	}
	if _, ok := f.ready(t); ok || f.state(t).State != "working" {
		t.Fatalf("request left, state %+v", f.state(t))
	}
}

// A send that keeps failing for another reason blocks the lead after an
// hour.
func TestLandRefusalBlocksAfterAnHourOfFailedSends(t *testing.T) {
	f := newLandFix(t)
	f.land = func(context.Context) (string, int) { return "bmesh: refused: verify failed", 1 }
	f.rn.failSends(errors.New("t3 is down"))
	f.pass()
	if r, ok := f.ready(t); !ok || r.FailingSince == "" || f.state(t).State != "landing" {
		t.Fatalf("request %+v %v state %+v", r, ok, f.state(t))
	}
	f.now = f.now.Add(30 * time.Minute)
	f.pass()
	if f.state(t).State != "landing" {
		t.Fatalf("blocked before the hour: %+v", f.state(t))
	}
	f.now = f.now.Add(31 * time.Minute)
	f.pass()
	st := f.state(t)
	if st.State != "blocked" || !strings.Contains(st.Text, "verify failed") {
		t.Fatalf("state %+v", st)
	}
	if _, ok := f.ready(t); ok {
		t.Fatal("request left after blocking")
	}
	hist, err := mesh.History(f.ctx, f.c, "bm.app-080.blocked", time.Time{})
	if err != nil || len(hist) != 1 {
		t.Fatalf("blocked events %d %v", len(hist), err)
	}
	if len(f.steps()) != 1 {
		t.Fatalf("land ran again: %q", f.steps())
	}
}

// A hold gives state held through bmesh held and runs no land and no
// wake; the release resumes the landing, and the only wake is the last.
func TestLandWaitsForTheRelease(t *testing.T) {
	f := newLandFix(t)
	hold := mesh.LeadHold{Path: "app-080", Gate: mesh.GateLand, Text: "hub move first", By: "operator", At: f.now.UTC().Format(time.RFC3339)}
	if err := mesh.PutLeadHold(f.ctx, f.c, hold); err != nil {
		t.Fatal(err)
	}
	if _, err := mesh.Send(f.ctx, f.c, mesh.Message{From: "operator", To: "app-080", Kind: "hold", Text: "hold from operator (land): hub move first"}); err != nil {
		t.Fatal(err)
	}
	f.pass()
	f.pass()
	wakeOnce(f.ctx, f.d, f.act)
	if got := f.steps(); len(got) != 1 || got[0] != "held" || f.state(t).State != "held" || len(f.sends(t)) != 0 {
		t.Fatalf("steps %q state %+v sends %q", got, f.state(t), f.sends(t))
	}
	if _, err := mesh.DeleteLeadHold(f.ctx, f.c, "app-080"); err != nil {
		t.Fatal(err)
	}
	if _, err := mesh.Send(f.ctx, f.c, mesh.Message{From: "operator", To: "app-080", Kind: "release", Text: "released by operator"}); err != nil {
		t.Fatal(err)
	}
	wakeOnce(f.ctx, f.d, f.act)
	f.pass()
	if got := f.steps(); len(got) != 3 || got[1] != "land" || !strings.HasPrefix(got[2], "done ") {
		t.Fatalf("steps %q", got)
	}
	if evs := f.daemonEvents(t, "landing"); len(evs) == 0 || evs[0].To != "app-080" {
		t.Fatalf("landing events %+v", evs)
	}
	if sends := f.sends(t); len(sends) != 1 || !strings.Contains(sends[0], "bmesh: landed ") {
		t.Fatalf("sends %q", sends)
	}
}

// A hold that lands while land runs makes it refuse; the daemon reads the
// hold, runs held and wakes nobody.
func TestLandRefusedByAHoldRunsHeld(t *testing.T) {
	f := newLandFix(t)
	f.land = func(ctx context.Context) (string, int) {
		_ = mesh.PutLeadHold(ctx, f.c, mesh.LeadHold{Path: "app-080", Gate: mesh.GateLand, By: "operator", At: time.Now().UTC().Format(time.RFC3339)})
		return "bmesh: held before land by operator 0m ago; nothing is pushed: run bmesh held and end your turn", 3
	}
	f.pass()
	if got := f.steps(); len(got) != 2 || got[1] != "held" || len(f.sends(t)) != 0 || f.state(t).State != "held" {
		t.Fatalf("steps %q sends %q state %+v", got, f.sends(t), f.state(t))
	}
	if r, ok := f.ready(t); !ok || r.Phase != mesh.PhaseReady {
		t.Fatalf("request %+v %v", r, ok)
	}
}

// A message cancels the running step, then the request ends and the lead
// is woken for its mail.
func TestLandMessageCancelsTheStepThenWakes(t *testing.T) {
	f := newLandFix(t)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	f.land = func(ctx context.Context) (string, int) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return "bmesh: interrupted; nothing is pushed", 1
	}
	done := make(chan struct{})
	go func() { defer close(done); f.pass() }()
	<-started
	f.send(t, "app-080", 1)
	<-done
	select {
	case <-cancelled:
	default:
		t.Fatal("the step was not cancelled")
	}
	if _, ok := f.ready(t); ok {
		t.Fatal("request left")
	}
	if sends := f.sends(t); len(sends) != 1 || sends[0] != wakeLine("th-1", 1) {
		t.Fatalf("sends %q", sends)
	}
	// Nothing lands it now: it is working again, so silence applies.
	if st := f.state(t); st.State != "working" || st.Text != "ready withdrawn: a message came" || st.By != "daemon" {
		t.Fatalf("state %+v", st)
	}
	if evs := f.daemonEvents(t, "land-withdrawn"); len(evs) != 1 || evs[0].To != "app-080" {
		t.Fatalf("land-withdrawn events %+v", evs)
	}
}

// The lead's own progress withdraws the request; the daemon runs nothing
// and writes nothing over it.
func TestLandLeadsOwnEventWithdraws(t *testing.T) {
	f := newLandFix(t)
	if _, err := mesh.WriteState(f.ctx, f.c, "app-080", mesh.State{State: "working", Text: "one more fix", By: "node"}); err != nil {
		t.Fatal(err)
	}
	f.pass()
	if _, ok := f.ready(t); ok || len(f.steps()) != 0 || len(f.sends(t)) != 0 {
		t.Fatalf("request left %v, steps %q, sends %q", ok, f.steps(), f.sends(t))
	}
	if st := f.state(t); st.Text != "one more fix" || st.By != "node" {
		t.Fatalf("state %+v", st)
	}
}

// A HEAD the lead moved after ready is refused, not landed.
func TestLandRefusesAMovedHead(t *testing.T) {
	f := newLandFix(t)
	f.write(t, filepath.Join(f.wt, "c.txt"), "c\n")
	f.git(t, "add", "-A")
	f.git(t, "commit", "-q", "-m", "more")
	f.pass()
	if len(f.steps()) != 0 {
		t.Fatalf("steps %q", f.steps())
	}
	if sends := f.sends(t); len(sends) != 1 || !strings.Contains(sends[0], "land refused and needs you: HEAD moved since bmesh ready") {
		t.Fatalf("sends %q", sends)
	}
}

// node stop mid-step ends the request, which cancels the step; the
// stopped thread isn't woken.
func TestLandNodeStopCancelsTheStep(t *testing.T) {
	f := newLandFix(t)
	started := make(chan struct{})
	f.land = func(ctx context.Context) (string, int) {
		close(started)
		<-ctx.Done()
		return "bmesh: interrupted; nothing is pushed", 1
	}
	done := make(chan struct{})
	go func() { defer close(done); f.pass() }()
	<-started
	if _, err := MarkStopped(f.ctx, f.c, "app-080", "operator", "enough"); err != nil {
		t.Fatal(err)
	}
	<-done
	if _, ok := f.ready(t); ok || len(f.sends(t)) != 0 || f.state(t).State != "blocked" {
		t.Fatalf("request %v sends %q state %+v", ok, f.sends(t), f.state(t))
	}
}

// The wake pass leaves a lead with a request to the lander, and silence
// never marks it.
func TestLandingLeadIsNeitherWokenNorSilent(t *testing.T) {
	f := newLandFix(t)
	reply := mesh.Message{From: "routine.review", To: "app-080", Kind: "reply", Reply: &mesh.Reply{Job: "j1", Kind: "review", OK: true}}
	if _, err := mesh.Send(f.ctx, f.c, reply); err != nil {
		t.Fatal(err)
	}
	a := f.act
	pollOnce(f.ctx, f.d, a)
	wakeOnce(f.ctx, f.d, a)
	f.now = f.now.Add(2 * time.Hour)
	silenceOnce(f.ctx, f.d, a)
	if len(f.sends(t)) != 0 || f.state(t).State != "landing" {
		t.Fatalf("sends %q state %+v", f.sends(t), f.state(t))
	}
}

// The landing text follows land's newest land event.
func TestLandingTextFollowsLandEvents(t *testing.T) {
	f := newLandFix(t)
	f.land = func(ctx context.Context) (string, int) {
		_ = mesh.PublishEvent(ctx, f.c, mesh.Event{From: "app-080", Event: "land", Text: "waiting for job j1 (review, running on testhost)"})
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if st, _, _ := mesh.ReadState(ctx, f.c, "app-080"); st.Text == "waiting for job j1 (review, running on testhost)" {
				return "", 5
			}
			time.Sleep(10 * time.Millisecond)
		}
		return "bmesh: text never followed", 1
	}
	f.pass()
	if st := f.state(t); st.State != "landing" || st.Text != "waiting for job j1 (review, running on testhost)" || st.By != "daemon" {
		t.Fatalf("state %+v", st)
	}
}

func assertNoClaims(t *testing.T, f *landFix) {
	t.Helper()
	ls, err := mesh.ListLeases(f.ctx, f.c)
	if err != nil || len(ls) != 0 {
		t.Fatalf("leases %+v %v", ls, err)
	}
}

// ready --cancel mid-step: the step is cancelled and the request ends,
// with no wake.
func TestLandCancelEndsTheStep(t *testing.T) {
	f := newLandFix(t)
	started := make(chan struct{})
	f.land = func(ctx context.Context) (string, int) {
		close(started)
		<-ctx.Done()
		return "bmesh: interrupted; nothing is pushed", 1
	}
	done := make(chan struct{})
	go func() { defer close(done); f.pass() }()
	<-started
	r, rev, _, _ := mesh.GetReady(f.ctx, f.c, "app-080")
	r.Phase = mesh.PhaseCancelled
	if _, err := mesh.UpdateReady(f.ctx, f.c, r, rev); err != nil {
		t.Fatal(err)
	}
	<-done
	if _, ok := f.ready(t); ok || len(f.sends(t)) != 0 || len(f.steps()) != 1 {
		t.Fatalf("request %v sends %q steps %q", ok, f.sends(t), f.steps())
	}
	if st := f.state(t); st.State != "working" || st.Text != "ready withdrawn: cancelled" {
		t.Fatalf("state %+v", st)
	}
	assertNoClaims(t, f)
}
