package daemon

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/runner"
	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp"
	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp/t3fake"
)

// onT3 points f's daemon at a fake T3 with a sign-in for it.
func (f *fixture) onT3(t *testing.T) *t3fake.Server {
	t.Helper()
	fake := t3fake.New()
	t.Cleanup(fake.Close)
	path := filepath.Join(t.TempDir(), "t3-token.json")
	exp := time.Now().Add(29 * 24 * time.Hour).UTC().Truncate(time.Second)
	if err := t3mcp.WriteToken(path, t3mcp.Token{Server: fake.URL, Token: fake.MintToken("auto", exp), ExpiresAt: exp,
		Scopes: []string{"orchestration:read", "orchestration:operate"}, Access: "auto"}); err != nil {
		t.Fatal(err)
	}
	f.d.Runner = &runner.T3{Server: fake.URL, Cred: t3mcp.FileCredential{Path: path}, Timeout: 5 * time.Second}
	return fake
}

// member registers n with an idle T3 thread (on runner kind, t3 when "")
// and sets its state.
func (f *fixture) member(t *testing.T, fake *t3fake.Server, n mesh.Node, kind string, st mesh.State) {
	t.Helper()
	thread := "mcp:" + n.Path
	n.Host, n.Runner = testHost, mesh.NodeRunner{Kind: kind, Thread: thread}
	if kind == "" {
		n.Runner.Kind = "t3"
	}
	if err := mesh.PutNode(f.ctx, f.c, n); err != nil {
		t.Fatal(err)
	}
	fake.AddThread(&t3fake.Thread{ID: thread, Runs: []t3fake.Run{{ID: "run-1", Status: "completed"}}})
	if st.State != "" {
		meshtest.ForceState(t, f.c, n.Path, st)
	}
}

// settles are the threads t3_thread_organize settled, in call order.
func settles(fake *t3fake.Server) []string {
	var out []string
	fake.Do(func(s *t3fake.Server) {
		for _, c := range s.Calls {
			if c.Tool == "t3_thread_organize" && c.Args["action"] == "settle" {
				out = append(out, c.Args["threadId"].(string))
			}
		}
	})
	return out
}

func TestSettleFinishedFamilies(t *testing.T) {
	f := setup(t)
	fake := f.onT3(t)
	done := mesh.State{State: "done"}
	stopped := mesh.State{State: "blocked", Text: mesh.StoppedBy("rick", "")}
	// Finished: one lead done, one stopped.
	f.member(t, fake, mesh.Node{Path: "demo-001", Role: "lead", Family: "demo-build-1"}, "", done)
	f.member(t, fake, mesh.Node{Path: "demo-002", Role: "lead", Family: "demo-build-1"}, "", stopped)
	// One lead blocked: nothing settles.
	f.member(t, fake, mesh.Node{Path: "demo-003", Role: "lead", Family: "demo-build-2"}, "", done)
	f.member(t, fake, mesh.Node{Path: "demo-004", Role: "lead", Family: "demo-build-2"}, "", mesh.State{State: "blocked", Text: "needs a key"})
	// On the tmux runner: skipped.
	f.member(t, fake, mesh.Node{Path: "demo-005", Role: "lead", Family: "demo-build-3"}, "tmux", done)
	// Done, but its closing turn still runs.
	f.member(t, fake, mesh.Node{Path: "demo-006", Role: "lead", Family: "demo-build-4"}, "", done)
	fake.Do(func(s *t3fake.Server) { s.Threads["mcp:demo-006"].ActiveRunID = "run-1" })
	// Done, with its final wake not yet delivered.
	f.member(t, fake, mesh.Node{Path: "demo-007", Role: "lead", Family: "demo-build-5"}, "", done)
	if err := mesh.CreateReady(f.ctx, f.c, mesh.Ready{Path: "demo-007", Spec: "007", Head: "abc", Phase: mesh.PhaseLanded}); err != nil {
		t.Fatal(err)
	}
	// A fixer and its fix lead, both done: the lead settles, the fixer, a coordinator, never.
	f.member(t, fake, mesh.Node{Path: "demo-fix-1", Role: "fixer", Family: "demo-fix-1"}, "", done)
	f.member(t, fake, mesh.Node{Path: "demo-008", Role: "lead", Family: "demo-fix-1"}, "", done)
	// A round that settled done, and one whose engineer is done but the round is blocked.
	if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: "demo-round-1", Role: "round", Host: testHost}); err != nil {
		t.Fatal(err)
	}
	meshtest.ForceState(t, f.c, "demo-round-1", done)
	f.member(t, fake, mesh.Node{Path: "demo-round-1.eng", Role: "engineer", Parent: "demo-round-1"}, "", done)
	if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: "demo-round-2", Role: "round", Host: testHost}); err != nil {
		t.Fatal(err)
	}
	meshtest.ForceState(t, f.c, "demo-round-2", mesh.State{State: "blocked", Text: "ux blocked"})
	f.member(t, fake, mesh.Node{Path: "demo-round-2.eng", Role: "engineer", Parent: "demo-round-2"}, "", done)

	settleOnce(f.ctx, f.d)
	want := []string{"mcp:demo-001", "mcp:demo-002", "mcp:demo-008", "mcp:demo-round-1.eng"}
	if got := settles(fake); !slices.Equal(got, want) {
		t.Fatalf("settled %v, want %v", got, want)
	}
	for _, p := range []string{"demo-001", "demo-002", "demo-008", "demo-round-1.eng"} {
		if n, _, _ := mesh.GetNode(f.ctx, f.c, p); n.Settled == "" {
			t.Errorf("%s not marked settled", p)
		}
	}
	var texts []string
	for _, ev := range f.daemonEvents(t, "settled") {
		texts = append(texts, ev.Text)
	}
	if want := []string{"demo-build-1: 2 threads", "demo-fix-1: 1 thread", "demo-round-1: 1 thread"}; !slices.Equal(texts, want) {
		t.Fatalf("settled events %q, want %q", texts, want)
	}

	// A second pass changes nothing.
	settleOnce(f.ctx, f.d)
	if got := settles(fake); !slices.Equal(got, want) {
		t.Fatalf("second pass settled %v", got)
	}
	if n := len(f.daemonEvents(t, "settled")); n != 3 {
		t.Fatalf("second pass published: %d settled events", n)
	}

	// The closing turn ends and the final wake is delivered: those settle.
	fake.Do(func(s *t3fake.Server) { s.Threads["mcp:demo-006"].ActiveRunID = "" })
	if _, err := mesh.DeleteReady(f.ctx, f.c, "demo-007"); err != nil {
		t.Fatal(err)
	}
	settleOnce(f.ctx, f.d)
	if got := settles(fake)[len(want):]; !slices.Equal(got, []string{"mcp:demo-006", "mcp:demo-007"}) {
		t.Fatalf("third pass settled %v", got)
	}
	all := []string{"mcp:demo-001", "mcp:demo-002", "mcp:demo-008", "mcp:demo-round-1.eng", "mcp:demo-006", "mcp:demo-007"}
	fake.Do(func(s *t3fake.Server) {
		for id, th := range s.Threads {
			if th.Settled != slices.Contains(all, id) {
				t.Errorf("thread %s settled %v", id, th.Settled)
			}
		}
	})

	// A message wakes a settled lead: its thread is settled again once that turn ends.
	f.send(t, "demo-001", 1)
	wakeOnce(f.ctx, f.d, NewActivity())
	if n, _, _ := mesh.GetNode(f.ctx, f.c, "demo-001"); n.Settled != "" {
		t.Fatal("a woken lead kept its settled mark")
	}
	settleOnce(f.ctx, f.d) // the woken turn runs: nothing
	if got := settles(fake); len(got) != len(all) {
		t.Fatalf("settled a running thread: %v", got)
	}
	fake.Do(func(s *t3fake.Server) {
		th := s.Threads["mcp:demo-001"]
		th.ActiveRunID, th.Runs[len(th.Runs)-1].Status = "", "completed"
	})
	settleOnce(f.ctx, f.d)
	if got := settles(fake)[len(all):]; !slices.Equal(got, []string{"mcp:demo-001"}) {
		t.Fatalf("after the woken turn settled %v", got)
	}
}

func TestSettleRetriesWhatT3Refused(t *testing.T) {
	f := setup(t)
	fake := f.onT3(t)
	f.member(t, fake, mesh.Node{Path: "demo-001", Role: "lead", Family: "demo-build-1"}, "", mesh.State{State: "done"})
	f.member(t, fake, mesh.Node{Path: "demo-002", Role: "lead", Family: "demo-build-1"}, "", mesh.State{State: "done"})
	// T3 no longer has demo-002's thread: it counts as settled.
	fake.Do(func(s *t3fake.Server) { delete(s.Threads, "mcp:demo-002") })
	var tokens map[string]string
	fake.Do(func(s *t3fake.Server) { tokens, s.Tokens = s.Tokens, map[string]string{} }) // T3 refuses the sign-in
	settleOnce(f.ctx, f.d)
	if n, _, _ := mesh.GetNode(f.ctx, f.c, "demo-001"); n.Settled != "" {
		t.Fatal("marked settled while T3 refused")
	}
	if n := len(f.daemonEvents(t, "settled")); n != 0 {
		t.Fatalf("%d settled events while T3 refused", n)
	}
	// The next pass, with T3 back, settles both.
	fake.Do(func(s *t3fake.Server) { s.Tokens = tokens })
	settleOnce(f.ctx, f.d)
	if got := settles(fake); !slices.Equal(got, []string{"mcp:demo-001"}) {
		t.Fatalf("settled %v", got)
	}
	evs := f.daemonEvents(t, "settled")
	if len(evs) != 1 || evs[0].Text != "demo-build-1: 2 threads" {
		t.Fatalf("settled events %+v", evs)
	}
}
