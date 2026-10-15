package daemon

import (
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

func TestWakeTextIsExact(t *testing.T) {
	want := "bmesh: you have 2 unread messages. Run `bmesh inbox` now, then continue."
	if got := WakeText(2); got != want {
		t.Fatalf("%q", got)
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
	if len(evs) != 1 || evs[0].To != "app-001" || evs[0].From != "daemon."+testHost || evs[0].Text != WakeText(2) {
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
		To: "app-001", Text: WakeText(1)}); err != nil {
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
