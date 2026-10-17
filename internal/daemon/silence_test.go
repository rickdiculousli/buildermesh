package daemon

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// Review focus 4: a lead quiet past grace whose executor reported inside
// grace stays working; a lead with nothing under it goes silent and is woken.
func TestSilenceIsTreeAware(t *testing.T) {
	f := setup(t)
	a := NewActivity()
	f.lead(t, "x", "th-x", "working")
	f.setState(t, "x.alpha", "working")
	f.lead(t, "y", "th-y", "working")
	base := time.Now()
	a.Observe("bm.x.progress", base)
	a.Observe("bm.x.alpha.progress", base.Add(20*time.Minute))
	a.Observe("bm.y.progress", base)
	f.now = base.Add(25 * time.Minute)

	silenceOnce(f.ctx, f.d, a)

	for _, p := range []string{"x", "x.alpha"} {
		if s, _, _ := mesh.ReadState(f.ctx, f.c, p); s.State != "working" {
			t.Fatalf("%s: %+v", p, s)
		}
	}
	s, _, _ := mesh.ReadState(f.ctx, f.c, "y")
	if s.State != "silent" || s.By != "daemon" || s.Text != "quiet for 25m" {
		t.Fatalf("y: %+v", s)
	}
	evs := f.daemonEvents(t, "silent")
	if len(evs) != 1 || evs[0].To != "y" || evs[0].From != "daemon."+testHost {
		t.Fatalf("silent events %+v", evs)
	}
	sends := f.sends(t)
	if len(sends) != 1 || sends[0] != wakeLine("th-y", 0) {
		t.Fatalf("sends %q", sends)
	}
	if wakes := f.daemonEvents(t, "wake"); len(wakes) != 1 || wakes[0].To != "y" {
		t.Fatalf("wake events %+v", wakes)
	}

	// A second pass finds y silent, not working: nothing new.
	silenceOnce(f.ctx, f.d, a)
	if evs := f.daemonEvents(t, "silent"); len(evs) != 1 {
		t.Fatalf("silent twice: %+v", evs)
	}
	if n := len(f.sends(t)); n != 1 {
		t.Fatalf("%d sends", n)
	}
	if strings.Contains(strings.Join(f.sends(t), "\n"), "th-x") {
		t.Fatal("x woken")
	}
}

// F7: a lead waiting on a routine stays alive: the instance's progress names
// the lead as To, which counts as the lead's activity.
func TestRoutineProgressKeepsRequesterAlive(t *testing.T) {
	f := setup(t)
	f.lead(t, "app-001", "th-1", "working")
	f.lead(t, "app-002", "th-2", "working")
	base := time.Now()
	a := NewActivity()
	a.Observe("bm.app-001.progress", base)
	a.Observe("bm.app-002.progress", base)
	ev := func(to string) []byte {
		b, err := json.Marshal(mesh.Event{From: "routine.review.opus-5-5-1", Event: "progress", Text: "job j1 started", To: to})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	a.ObserveStored(mesh.Stored{Subject: "bm.routine.review.opus-5-5-1.progress", At: base.Add(20 * time.Minute), Data: ev("app-001")})
	// An inbox message's to is not activity of the recipient.
	a.ObserveStored(mesh.Stored{Subject: "bm.inbox.app-002", At: base.Add(20 * time.Minute), Data: ev("app-002")})
	f.now = base.Add(25 * time.Minute)

	silenceOnce(f.ctx, f.d, a)

	if s, _, _ := mesh.ReadState(f.ctx, f.c, "app-001"); s.State != "working" {
		t.Fatalf("app-001: %+v", s)
	}
	if s, _, _ := mesh.ReadState(f.ctx, f.c, "app-002"); s.State != "silent" {
		t.Fatalf("app-002: %+v", s)
	}
	if got := a.Last("routine.review"); !got.Equal(base.Add(20 * time.Minute)) {
		t.Fatalf("the instance's own activity: %v", got)
	}
}

// A held lead waits on its coordinator: quiet past grace, it is never
// marked silent nor woken.
func TestSilenceNeverMarksHeld(t *testing.T) {
	f := setup(t)
	a := NewActivity()
	f.lead(t, "h", "th-h", "held")
	base := time.Now()
	a.Observe("bm.h.held", base)
	f.now = base.Add(3 * time.Hour)

	silenceOnce(f.ctx, f.d, a)

	if s, _, _ := mesh.ReadState(f.ctx, f.c, "h"); s.State != "held" {
		t.Fatalf("h: %+v", s)
	}
	if evs := f.daemonEvents(t, "silent"); len(evs) != 0 {
		t.Fatalf("silent events %+v", evs)
	}
	if sends := f.sends(t); len(sends) != 0 {
		t.Fatalf("sends %q", sends)
	}
}
