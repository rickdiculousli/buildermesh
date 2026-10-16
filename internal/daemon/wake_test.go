package daemon

import (
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

func TestWakeTextIsExact(t *testing.T) {
	if got, want := WakeText(2, nil), "bmesh: 2 unread. Run `bmesh inbox` now, then continue."; got != want {
		t.Fatalf("count alone %q", got)
	}
	done := mesh.Message{From: "app-012.alpha", Kind: "report",
		Report: &mesh.Report{Event: "done", From: "app-012.alpha", Branch: "t3code/x-alpha", Commit: "1a2b3c4d5e6f7a8b"}}
	blocked := mesh.Message{From: "app-012.beta", Kind: "report", Report: &mesh.Report{Event: "blocked", From: "app-012.beta"}}
	reply := mesh.Message{From: "routine.review", Kind: "reply", Reply: &mesh.Reply{Job: "9f2kQ", Kind: "review", OK: false}}
	msg := mesh.Message{From: "operator", Kind: "message", Text: "yes"}
	want := "bmesh: 3 unread: done from app-012.alpha (branch t3code/x-alpha at 1a2b3c4d); reply to job 9f2kQ (review fail); " +
		"message from operator. Run `bmesh inbox` now, then continue."
	if got := WakeText(3, []mesh.Message{done, reply, msg}); got != want {
		t.Fatalf("three kinds %q", got)
	}
	// Five items, then how many more; the count stays Unread's.
	want = "bmesh: 6 unread: done from app-012.alpha (branch t3code/x-alpha at 1a2b3c4d); blocked from app-012.beta; " +
		"reply to job 9f2kQ (review fail); message from operator; message from operator; and 2 more. " +
		"Run `bmesh inbox` now, then continue."
	if got := WakeText(6, []mesh.Message{done, blocked, reply, msg, msg, msg, msg}); got != want {
		t.Fatalf("capped %q", got)
	}
}

// The wake names what waits, read from the stream without taking it: the
// lead's inbox still holds every message after the wake.
func TestWakeNamesPendingWithoutFetching(t *testing.T) {
	f := setup(t)
	a := NewActivity()
	f.lead(t, "app-001", "th-1", "working")
	rep := mesh.Message{From: "app-001.alpha", To: "app-001", Kind: "report", Text: "done from app-001.alpha: branch b at 1a2b3c4d",
		Report: &mesh.Report{Event: "done", From: "app-001.alpha", Branch: "b", Commit: "1a2b3c4d5e"}}
	if _, err := mesh.Send(f.ctx, f.c, rep); err != nil {
		t.Fatal(err)
	}
	f.send(t, "app-001", 1)
	pollOnce(f.ctx, f.d, a)
	wakeOnce(f.ctx, f.d, a)
	want := "send th-1 bmesh: 2 unread: done from app-001.alpha (branch b at 1a2b3c4d); message from rick. Run `bmesh inbox` now, then continue."
	if sends := f.sends(t); len(sends) != 1 || sends[0] != want {
		t.Fatalf("sends %q", sends)
	}
	p, _ := mesh.ParsePath("app-001")
	if n, err := mesh.Unread(f.ctx, f.c, p); err != nil || n != 2 {
		t.Fatalf("unread after the wake: %d %v", n, err)
	}
}

func TestWakeSendsOnceWhenIdleWithUnread(t *testing.T) {
	f := setup(t)
	a := NewActivity()
	f.lead(t, "app-001", "th-1", "working")
	f.lead(t, "app-002", "th-2", "working") // nothing unread: never woken
	f.send(t, "app-001", 2)

	pollOnce(f.ctx, f.d, a)
	s, ok, err := mesh.GetRunner(f.ctx, f.c, "app-001")
	if err != nil || !ok || !s.Known || s.Running {
		t.Fatalf("sample %+v %v %v", s, ok, err)
	}
	wakeOnce(f.ctx, f.d, a)
	sends := f.sends(t)
	if len(sends) != 1 || sends[0] != wakeLine("th-1", 2) {
		t.Fatalf("sends %q", sends)
	}
	evs := f.daemonEvents(t, "wake")
	if len(evs) != 1 || evs[0].To != "app-001" || evs[0].From != "daemon."+testHost || evs[0].Text != WakeText(2, fromRick(2)) {
		t.Fatalf("wake events %+v", evs)
	}

	f.now = f.now.Add(5 * time.Minute)
	pollOnce(f.ctx, f.d, a)
	wakeOnce(f.ctx, f.d, a)
	if n := len(f.sends(t)); n != 1 {
		t.Fatalf("second wake inside 10 min: %d sends", n)
	}

	f.now = f.now.Add(6 * time.Minute)
	pollOnce(f.ctx, f.d, a)
	wakeOnce(f.ctx, f.d, a)
	if n := len(f.sends(t)); n != 2 {
		t.Fatalf("no wake after 11 min: %d sends", n)
	}
}

// Review focus 1: the thread starts a turn between Status and Send.
func TestWakeRetriesWhenRunnerBecomesRunning(t *testing.T) {
	f := setup(t)
	a := NewActivity()
	f.lead(t, "app-001", "th-1", "working")
	f.send(t, "app-001", 1)

	t.Setenv("FAKE_T3_SEND_EXIT", "4")
	pollOnce(f.ctx, f.d, a)
	wakeOnce(f.ctx, f.d, a)
	if evs := f.daemonEvents(t, "wake"); len(evs) != 0 {
		t.Fatalf("wake published although not delivered: %+v", evs)
	}
	if !a.LastWake("app-001").IsZero() {
		t.Fatal("undelivered wake recorded")
	}

	t.Setenv("FAKE_T3_SEND_EXIT", "0")
	f.now = f.now.Add(time.Minute)
	pollOnce(f.ctx, f.d, a)
	wakeOnce(f.ctx, f.d, a)
	sends := f.sends(t)
	if len(sends) != 2 || sends[1] != wakeLine("th-1", 1) {
		t.Fatalf("sends %q", sends)
	}
	if evs := f.daemonEvents(t, "wake"); len(evs) != 1 || evs[0].To != "app-001" {
		t.Fatalf("wake events %+v", evs)
	}
}

// Review focus 5: sent wakes are stream events, so a fresh daemon keeps the
// one-wake-per-10-minutes rule.
func TestWakeOncePer10MinAcrossRestart(t *testing.T) {
	f := setup(t)
	f.lead(t, "app-001", "th-1", "working")
	f.send(t, "app-001", 1)
	if err := mesh.PublishEvent(f.ctx, f.c, mesh.Event{From: "daemon." + testHost, Event: "wake",
		To: "app-001", Text: WakeText(1, nil)}); err != nil {
		t.Fatal(err)
	}
	a := NewActivity()
	if err := a.Rebuild(f.ctx, f.c, time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	f.now = time.Now().Add(3 * time.Minute)
	pollOnce(f.ctx, f.d, a)
	wakeOnce(f.ctx, f.d, a)
	if sends := f.sends(t); len(sends) != 0 {
		t.Fatalf("woken again after restart: %q", sends)
	}
	f.now = time.Now().Add(11 * time.Minute)
	pollOnce(f.ctx, f.d, a)
	wakeOnce(f.ctx, f.d, a)
	if sends := f.sends(t); len(sends) != 1 {
		t.Fatalf("no wake after 11 min: %q", sends)
	}
}

// A done or blocked lead is woken for a message sent to it, never for a late
// report alone.
func TestWakeFinishedLeadOnlyForMessage(t *testing.T) {
	f := setup(t)
	a := NewActivity()
	f.lead(t, "app-001", "th-1", "done")
	f.lead(t, "app-002", "th-2", "blocked")
	rep := mesh.Message{From: "app-001.alpha", To: "app-001", Kind: "report", Text: "done from app-001.alpha: branch b at 1a2b3c4d",
		Report: &mesh.Report{Event: "done", From: "app-001.alpha", Branch: "b", Commit: "1a2b3c4d5e"}}
	if _, err := mesh.Send(f.ctx, f.c, rep); err != nil {
		t.Fatal(err)
	}
	f.send(t, "app-002", 1)
	pollOnce(f.ctx, f.d, a)
	wakeOnce(f.ctx, f.d, a)
	if sends := f.sends(t); len(sends) != 1 || sends[0] != "send th-2 bmesh: 1 unread: message from rick. Run `bmesh inbox` now, then continue." {
		t.Fatalf("sends %q", sends)
	}

	f.send(t, "app-001", 1)
	pollOnce(f.ctx, f.d, a)
	wakeOnce(f.ctx, f.d, a)
	want := "send th-1 bmesh: 2 unread: done from app-001.alpha (branch b at 1a2b3c4d); message from rick. Run `bmesh inbox` now, then continue."
	if sends := f.sends(t); len(sends) != 2 || sends[1] != want {
		t.Fatalf("sends %q", sends)
	}
}
