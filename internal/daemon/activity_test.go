package daemon

import (
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

func TestActivityObserveIsTreeAware(t *testing.T) {
	a := NewActivity()
	t0 := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	a.Observe("bm.x.progress", t0)
	a.Observe("bm.x.alpha.progress", t0.Add(20*time.Minute))
	a.Observe("bm.x.alpha.started", t0.Add(time.Minute)) // older: max wins
	a.Observe("bm.xy.progress", t0.Add(30*time.Minute))  // not under x
	a.Observe("bm.inbox.x", t0.Add(40*time.Minute))      // inbox is not activity
	a.Observe("bm.daemon.h.silent", t0.Add(40*time.Minute))
	if got := a.Last("x"); !got.Equal(t0.Add(20 * time.Minute)) {
		t.Fatalf("Last(x) = %v", got)
	}
	if got := a.Last("x.alpha"); !got.Equal(t0.Add(20 * time.Minute)) {
		t.Fatalf("Last(x.alpha) = %v", got)
	}
	if got := a.Last("nobody"); !got.IsZero() {
		t.Fatalf("Last(nobody) = %v", got)
	}
	if !a.LastWake("x").IsZero() {
		t.Fatal("wake recorded from a non-wake event")
	}
	a.ObserveWake("x", t0)
	a.ObserveWake("x", t0.Add(-time.Minute))
	if got := a.LastWake("x"); !got.Equal(t0) {
		t.Fatalf("LastWake(x) = %v", got)
	}
}

// Review focus 5: a restarted daemon rebuilds last activity from the stream,
// so a node active 5 minutes ago is neither zero nor marked silent.
func TestActivityRebuiltFromHistory(t *testing.T) {
	f := setup(t)
	f.lead(t, "app-001", "th-1", "working")
	if err := mesh.PublishEvent(f.ctx, f.c, mesh.Event{From: "app-001", Event: "progress", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	hist, err := mesh.History(f.ctx, f.c, "bm.app-001.progress", time.Time{})
	if err != nil || len(hist) != 1 {
		t.Fatalf("%v %v", hist, err)
	}
	a := NewActivity()
	if err := a.Rebuild(f.ctx, f.c, time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := a.Last("app-001"); !got.Equal(hist[0].At) {
		t.Fatalf("Last = %v, want %v", got, hist[0].At)
	}
	f.now = hist[0].At.Add(5 * time.Minute)
	silenceOnce(f.ctx, f.d, a)
	if s, _, _ := mesh.ReadState(f.ctx, f.c, "app-001"); s.State != "working" {
		t.Fatalf("state %+v", s)
	}
}
