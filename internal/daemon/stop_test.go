package daemon

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

// nodeEvents returns the decoded events path published of one kind.
func (f *fixture) nodeEvents(t *testing.T, path, event string) []mesh.Event {
	t.Helper()
	hist, err := mesh.History(f.ctx, f.c, "bm."+path+"."+event, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var out []mesh.Event
	for _, s := range hist {
		var ev mesh.Event
		if err := json.Unmarshal(s.Data, &ev); err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

func (f *fixture) state(t *testing.T, path string) mesh.State {
	t.Helper()
	s, _, err := mesh.ReadState(f.ctx, f.c, path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A newest run the user interrupted marks the node blocked, stopped in its
// runner, and publishes that once; the same run never marks it again, a
// later interrupted run does, and a failed run (a crash) never does.
func TestPollMarksAnInterruptedThreadStoppedOnce(t *testing.T) {
	f := setup(t)
	a := NewActivity()
	f.lead(t, "app-001", "th-1", "working")
	f.lead(t, "app-002", "th-2", "working")
	f.lead(t, "app-003", "th-3", "") // interrupted before its first event
	f.rn.setStatus("th-1", runner.Status{Known: true, Stopped: true, StoppedRun: "r1"})
	f.rn.setStatus("th-2", runner.Status{Known: true}) // its newest run failed: not a stop
	f.rn.setStatus("th-3", runner.Status{Known: true, Stopped: true, StoppedRun: "r1"})

	pollOnce(f.ctx, f.d, a)
	for _, p := range []string{"app-001", "app-003"} {
		if s := f.state(t, p); s.State != "blocked" || s.Text != "stopped in t3 by the operator" || !mesh.Stopped(s) {
			t.Fatalf("%s: %+v", p, s)
		}
		if evs := f.nodeEvents(t, p, "blocked"); len(evs) != 1 || evs[0].From != p || evs[0].Text != "stopped in t3 by the operator" {
			t.Fatalf("%s blocked events %+v", p, evs)
		}
	}
	if s := f.state(t, "app-002"); s.State != "working" {
		t.Fatalf("failed run: %+v", s)
	}
	if s, _, _ := mesh.GetRunner(f.ctx, f.c, "app-001"); !s.Stopped || s.StoppedRun != "r1" {
		t.Fatalf("sample %+v", s)
	}

	// The node moves on (an event of its own) while the thread's newest run
	// is still the stopped one: that stop is not counted again.
	f.setState(t, "app-001", "working")
	pollOnce(f.ctx, f.d, a)
	if s := f.state(t, "app-001"); s.State != "working" {
		t.Fatalf("marked twice: %+v", s)
	}
	if evs := f.nodeEvents(t, "app-001", "blocked"); len(evs) != 1 {
		t.Fatalf("published twice: %+v", evs)
	}

	// A later run interrupted is a new stop.
	f.rn.setStatus("th-1", runner.Status{Known: true, Stopped: true, StoppedRun: "r2"})
	pollOnce(f.ctx, f.d, a)
	if s := f.state(t, "app-001"); s.State != "blocked" {
		t.Fatalf("second stop: %+v", s)
	}
	if evs := f.nodeEvents(t, "app-001", "blocked"); len(evs) != 2 {
		t.Fatalf("second stop events %+v", evs)
	}

	// Never woken: not for mail, not for silence.
	if _, err := mesh.Send(f.ctx, f.c, mesh.Message{From: "app-001.x", To: "app-001", Kind: "report",
		Report: &mesh.Report{Event: "done", From: "app-001.x"}}); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	wakeOnce(f.ctx, f.d, a)
	silenceOnce(f.ctx, f.d, a)
	for _, s := range f.sends(t) {
		if strings.Contains(s, "th-1") || strings.Contains(s, "th-3") {
			t.Fatalf("a stopped thread was woken: %q", f.sends(t))
		}
	}
}

// A thread the user stopped before the poll marked it is not woken for
// silence either: silence marks it, the poll then blocks it.
func TestSilenceNeverWakesAnInterruptedThread(t *testing.T) {
	f := setup(t)
	a := NewActivity()
	f.lead(t, "app-001", "th-1", "working")
	f.rn.setStatus("th-1", runner.Status{Known: true, Stopped: true, StoppedRun: "r1"})
	a.Observe("bm.app-001.progress", f.now)
	f.now = f.now.Add(30 * time.Minute)
	silenceOnce(f.ctx, f.d, a)
	if s := f.state(t, "app-001"); s.State != "silent" {
		t.Fatalf("%+v", s)
	}
	if sends := f.sends(t); len(sends) != 0 {
		t.Fatalf("woken: %q", sends)
	}
	pollOnce(f.ctx, f.d, a)
	if s := f.state(t, "app-001"); !mesh.Stopped(s) {
		t.Fatalf("not stopped: %+v", s)
	}
}

// A node bmesh node stop stopped is never woken for silence, for a report,
// a reply, its own executor's ask or a message sent before the stop (a
// STOP sent while its thread ran); a message sent to it after the stop
// (bmesh send) resumes it, and the wake says who resumed it.
func TestAStoppedNodeWakesOnlyForASend(t *testing.T) {
	f := setup(t)
	a := NewActivity()
	f.lead(t, "app-001", "th-1", "working")
	stoppedAt := time.Now().Add(-time.Minute).UTC()
	if _, err := mesh.WriteState(f.ctx, f.c, "app-001", mesh.State{State: "blocked", Text: mesh.StoppedBy("operator", "wrong spec"), By: "node",
		At: stoppedAt.Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []mesh.Message{
		{From: "operator", To: "app-001", Kind: "message", Text: "STOP", At: stoppedAt.Add(-time.Minute).Format(time.RFC3339)},
		{From: "operator", To: "app-001", Kind: "message", Text: "STOP again", At: stoppedAt.Format(time.RFC3339)}, // the stop's own second
		{From: "app-001.x", To: "app-001", Kind: "report", Report: &mesh.Report{Event: "done", From: "app-001.x", Branch: "b", Commit: "1a2b3c4d"}},
		{From: "routine.review", To: "app-001", Kind: "reply", Reply: &mesh.Reply{Job: "j1", Kind: "review", OK: true}},
		{From: "app-001.x", To: "app-001", Kind: "message", Text: "ask from app-001.x: which file?"},
	} {
		if _, err := mesh.Send(f.ctx, f.c, m); err != nil {
			t.Fatal(err)
		}
	}
	a.Observe("bm.app-001.progress", f.now)
	f.now = f.now.Add(time.Hour)
	pollOnce(f.ctx, f.d, a)
	wakeOnce(f.ctx, f.d, a)
	silenceOnce(f.ctx, f.d, a)
	if sends := f.sends(t); len(sends) != 0 {
		t.Fatalf("woken: %q", sends)
	}
	if s := f.state(t, "app-001"); !mesh.Stopped(s) {
		t.Fatalf("silence touched it: %+v", s)
	}

	f.send(t, "app-001", 1)
	wakeOnce(f.ctx, f.d, a)
	sends := f.sends(t)
	if len(sends) != 1 || !strings.HasPrefix(sends[0], "send th-1 bmesh: resumed by rick; 6 unread: ") ||
		!strings.HasSuffix(sends[0], "; and 1 more. Run `bmesh inbox` now, then continue.") {
		t.Fatalf("sends %q", sends)
	}
}

// A blocked node that is not stopped keeps today's rule: any message wakes it.
func TestABlockedNodeStillWakesForAnExecutorsAsk(t *testing.T) {
	f := setup(t)
	a := NewActivity()
	f.lead(t, "app-001", "th-1", "blocked")
	if _, err := mesh.Send(f.ctx, f.c, mesh.Message{From: "app-001.x", To: "app-001", Kind: "message", Text: "ask from app-001.x: ?"}); err != nil {
		t.Fatal(err)
	}
	wakeOnce(f.ctx, f.d, a)
	if sends := f.sends(t); len(sends) != 1 || strings.Contains(sends[0], "resumed") {
		t.Fatalf("sends %q", sends)
	}
}

func TestResumeTextIsExact(t *testing.T) {
	got := ResumeText("operator", 1, []mesh.Message{{From: "operator", Kind: "message"}})
	if want := "bmesh: resumed by operator; 1 unread: message from operator. Run `bmesh inbox` now, then continue."; got != want {
		t.Fatalf("%q", got)
	}
}
