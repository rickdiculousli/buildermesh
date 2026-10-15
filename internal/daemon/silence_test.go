package daemon

import (
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
