package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

func TestSettleRound(t *testing.T) {
	done := func(text string) mesh.State { return mesh.State{State: "done", Text: text} }
	for name, tc := range map[string]struct {
		eng, ux     mesh.State
		state, text string
		ok          bool
	}{
		"open":        {eng: done("a"), ux: mesh.State{State: "working"}},
		"both done":   {eng: done("1 a\n2 b"), ux: done("3 c"), state: "done", text: "1 a\n2 b\n3 c", ok: true},
		"eng blocked": {eng: mesh.State{State: "blocked", Text: "no app"}, ux: mesh.State{State: "working"}, state: "blocked", text: "eng blocked: no app", ok: true},
		"ux blocked after eng done": {eng: done("1 a"), ux: mesh.State{State: "blocked", Text: "no display"}, state: "blocked",
			text: "ux blocked: no display\n\n1 a", ok: true},
		"idle":   {},
		"silent": {eng: done("a"), ux: mesh.State{State: "silent"}},
	} {
		state, text, ok := SettleRound(tc.eng, tc.ux)
		if ok != tc.ok || state != tc.state || text != tc.text {
			t.Errorf("%s: got %q %q %v, want %q %q %v", name, state, text, ok, tc.state, tc.text, tc.ok)
		}
	}
}

// round registers a round with its two engineer children on the test host.
func (f *fixture) round(t *testing.T, path string) {
	t.Helper()
	if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: path, Role: "round", Repo: "demo", Host: testHost}); err != nil {
		t.Fatal(err)
	}
	f.setState(t, path, "working")
	for _, c := range []string{"eng", "ux"} {
		if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: path + "." + c, Role: "engineer", Parent: path, Repo: "demo", Host: testHost,
			Runner: mesh.NodeRunner{Kind: "t3", Thread: "th-" + c}}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRoundsOnceSettlesWhenBothChildrenAreDone(t *testing.T) {
	f := setup(t)
	f.round(t, "demo-round-1")
	roundsOnce(f.ctx, f.d)
	if st, _, _ := mesh.ReadState(f.ctx, f.c, "demo-round-1"); st.State != "working" {
		t.Fatalf("settled early: %+v", st)
	}
	f.setState(t, "demo-round-1.eng", "working")
	if _, err := mesh.WriteState(f.ctx, f.c, "demo-round-1.eng", mesh.State{State: "done", Text: "013 A (feature, S, opus): why", By: "node"}); err != nil {
		t.Fatal(err)
	}
	roundsOnce(f.ctx, f.d)
	if st, _, _ := mesh.ReadState(f.ctx, f.c, "demo-round-1"); st.State != "working" {
		t.Fatalf("settled with one child: %+v", st)
	}
	f.setState(t, "demo-round-1.ux", "working")
	if _, err := mesh.WriteState(f.ctx, f.c, "demo-round-1.ux", mesh.State{State: "done", Text: "018 B (ux, M, fable): look", By: "node"}); err != nil {
		t.Fatal(err)
	}
	roundsOnce(f.ctx, f.d)
	st, _, _ := mesh.ReadState(f.ctx, f.c, "demo-round-1")
	if st.State != "done" || st.By != "daemon" || st.Text != "013 A (feature, S, opus): why\n018 B (ux, M, fable): look" {
		t.Fatalf("%+v", st)
	}
	hist, err := mesh.History(f.ctx, f.c, "bm.demo-round-1.done", time.Time{})
	if err != nil || len(hist) != 1 {
		t.Fatalf("done events %d %v", len(hist), err)
	}
	ev, _ := decodeEvent(hist[0].Data)
	if ev.From != "demo-round-1" || ev.Text != st.Text {
		t.Fatalf("%+v", ev)
	}
	// Settled once: a second pass publishes nothing more.
	roundsOnce(f.ctx, f.d)
	if hist, _ := mesh.History(f.ctx, f.c, "bm.demo-round-1.done", time.Time{}); len(hist) != 1 {
		t.Fatalf("done events %d", len(hist))
	}
}

func TestRoundsOnceBlocksWhenAChildBlocks(t *testing.T) {
	f := setup(t)
	f.round(t, "demo-round-2")
	f.setState(t, "demo-round-2.ux", "working")
	if _, err := mesh.WriteState(f.ctx, f.c, "demo-round-2.ux", mesh.State{State: "blocked", Text: "the app will not run", By: "node"}); err != nil {
		t.Fatal(err)
	}
	roundsOnce(f.ctx, f.d)
	st, _, _ := mesh.ReadState(f.ctx, f.c, "demo-round-2")
	if st.State != "blocked" || st.Text != "ux blocked: the app will not run" {
		t.Fatalf("%+v", st)
	}
	if hist, _ := mesh.History(f.ctx, f.c, "bm.demo-round-2.blocked", time.Time{}); len(hist) != 1 {
		t.Fatalf("blocked events %d", len(hist))
	}
	// Nothing new: no second publication.
	roundsOnce(f.ctx, f.d)
	if hist, _ := mesh.History(f.ctx, f.c, "bm.demo-round-2.blocked", time.Time{}); len(hist) != 1 {
		t.Fatalf("blocked events %d", len(hist))
	}
	// The engineer finishes: the round stays blocked but says so again with the pitch.
	f.setState(t, "demo-round-2.eng", "working")
	if _, err := mesh.WriteState(f.ctx, f.c, "demo-round-2.eng", mesh.State{State: "done", Text: "013 A: why", By: "node"}); err != nil {
		t.Fatal(err)
	}
	roundsOnce(f.ctx, f.d)
	st, _, _ = mesh.ReadState(f.ctx, f.c, "demo-round-2")
	if st.State != "blocked" || st.Text != "ux blocked: the app will not run\n\n013 A: why" {
		t.Fatalf("%+v", st)
	}
	hist, _ := mesh.History(f.ctx, f.c, "bm.demo-round-2.blocked", time.Time{})
	if len(hist) != 2 {
		t.Fatalf("blocked events %d", len(hist))
	}
	if ev, _ := decodeEvent(hist[1].Data); ev.Text != st.Text {
		t.Fatalf("%+v", ev)
	}
	// The UI/UX engineer is unblocked and finishes: the round is done after all.
	if _, err := mesh.WriteState(f.ctx, f.c, "demo-round-2.ux", mesh.State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mesh.WriteState(f.ctx, f.c, "demo-round-2.ux", mesh.State{State: "done", Text: "018 B: look", By: "node"}); err != nil {
		t.Fatal(err)
	}
	roundsOnce(f.ctx, f.d)
	st, _, _ = mesh.ReadState(f.ctx, f.c, "demo-round-2")
	if st.State != "done" || st.Text != "013 A: why\n018 B: look" {
		t.Fatalf("%+v", st)
	}
	if hist, _ := mesh.History(f.ctx, f.c, "bm.demo-round-2.done", time.Time{}); len(hist) != 1 {
		t.Fatalf("done events %d", len(hist))
	}
}

func TestPushRoundProposed(t *testing.T) {
	f, n := pushSetup(t)
	f.round(t, "demo-round-3")
	stop := startPush(t, f.d)
	f.event(t, mesh.Event{From: "demo-round-3", Event: "done", Text: "013 A: why\n018 B: look"})
	got := n.waitRequests(t, 1)
	stop()
	r := got[0]
	if r.Title != "demo round 3 proposed" || r.Priority != "default" || r.Tags != "memo" || r.Body != "013 A: why\n018 B: look" ||
		r.Click != "" || r.Actions != "view, Open thread, http://t3.test/t/th-eng" {
		t.Fatalf("%+v", r)
	}
	req, ok := Push(mesh.Event{From: "demo-round-3", Event: "blocked", Text: "ux blocked: x"}, mesh.Node{Path: "demo-round-3", Role: "round", Repo: "demo"},
		f.d.Cfg.Notify, "")
	if !ok || req.Title != "demo round 3 blocked" || req.Priority != "high" || req.Tags != "no_entry" || req.Body != "ux blocked: x" {
		t.Fatalf("%+v %v", req, ok)
	}
	if _, ok := Push(mesh.Event{From: "demo-round-3", Event: "ask"}, mesh.Node{Path: "demo-round-3", Role: "round"}, f.d.Cfg.Notify, ""); ok {
		t.Fatal("a round's ask pushed")
	}
	if _, ok := Push(mesh.Event{From: "demo-round-3.eng", Event: "done"}, mesh.Node{Path: "demo-round-3.eng", Role: "engineer"}, f.d.Cfg.Notify, ""); ok {
		t.Fatal("an engineer's done pushed")
	}
	req, ok = Push(mesh.Event{From: "demo-round-3.eng", Event: "ask", Text: "which?"}, mesh.Node{Path: "demo-round-3.eng", Role: "engineer"},
		f.d.Cfg.Notify, "http://t3.test/t/th-eng")
	if !ok || req.Title != "demo-round-3.eng asks" || req.Body != "which?" || req.Actions != "view, Open thread, http://t3.test/t/th-eng" {
		t.Fatalf("an engineer's ask: %+v %v", req, ok)
	}
}

func TestPollAndWakeCoverEngineers(t *testing.T) {
	f := setup(t)
	f.round(t, "demo-round-4")
	f.setState(t, "demo-round-4.eng", "working")
	f.send(t, "demo-round-4.eng", 1)
	pollOnce(f.ctx, f.d, nil)
	if s, ok, _ := mesh.GetRunner(f.ctx, f.c, "demo-round-4.eng"); !ok || !s.Known || s.Running {
		t.Fatalf("engineer not polled: %+v %v", s, ok)
	}
	if _, ok, _ := mesh.GetRunner(f.ctx, f.c, "demo-round-4"); ok {
		t.Fatal("the round itself was polled")
	}
	act := NewActivity()
	wakeOnce(f.ctx, f.d, act)
	if sends := f.sends(t); len(sends) != 1 || !strings.HasPrefix(sends[0], "send th-eng ") {
		t.Fatalf("sends %q", sends)
	}
}
