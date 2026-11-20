package daemon

import (
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

func TestForgetDoneOnceForgetsOnlyLongDoneLeftovers(t *testing.T) {
	f := setup(t)
	old := f.now.Add(-31 * 24 * time.Hour).UTC().Format(time.RFC3339)
	recent := f.now.Add(-time.Hour).UTC().Format(time.RFC3339)
	put := func(n mesh.Node, state, at string) {
		t.Helper()
		if n.Host == "" {
			n.Host = testHost
		}
		if err := mesh.PutNode(f.ctx, f.c, n); err != nil {
			t.Fatal(err)
		}
		meshtest.ForceState(t, f.c, n.Path, mesh.State{State: state, At: at, By: "node"})
	}
	put(mesh.Node{Path: "gone", Role: "lead", Worktree: "/nowhere/gone"}, "done", old)
	put(mesh.Node{Path: "tree", Role: "lead", Worktree: t.TempDir()}, "done", old)
	put(mesh.Node{Path: "dad", Role: "lead"}, "done", old)
	put(mesh.Node{Path: "dad.kid", Role: "executor", Parent: "dad"}, "done", old)
	put(mesh.Node{Path: "fresh", Role: "lead"}, "done", recent)
	put(mesh.Node{Path: "stuck", Role: "lead"}, "blocked", old)
	put(mesh.Node{Path: "elsewhere", Role: "lead", Host: "other"}, "done", old)
	put(mesh.Node{Path: "routine.verify.cmd-1", Role: "routine"}, "done", old)
	put(mesh.Node{Path: "round-1", Role: "round"}, "blocked", old)
	put(mesh.Node{Path: "round-1.eng", Role: "engineer", Parent: "round-1"}, "done", old)
	t3 := mesh.NodeRunner{Kind: "t3", Thread: "th"}
	put(mesh.Node{Path: "unsettled", Role: "lead", Runner: t3}, "done", old)
	put(mesh.Node{Path: "settled", Role: "lead", Runner: t3, Settled: old}, "done", old)
	paths := func() map[string]bool {
		t.Helper()
		nodes, err := mesh.ListNodes(f.ctx, f.c)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]bool{}
		for _, n := range nodes {
			m[n.Path] = true
		}
		return m
	}

	f.d.Cfg.Status.ForgetDone = 0
	forgetDoneOnce(f.ctx, f.d)
	if len(paths()) != 12 {
		t.Fatalf("forget_done 0 forgot: %v", paths())
	}

	f.d.Cfg.Status.ForgetDone = 30 * 24 * time.Hour
	forgetDoneOnce(f.ctx, f.d)
	got := paths()
	for _, p := range []string{"gone", "dad.kid", "settled"} {
		if got[p] {
			t.Errorf("%s kept", p)
		}
	}
	for _, p := range []string{"tree", "dad", "fresh", "stuck", "elsewhere", "routine.verify.cmd-1", "round-1.eng", "unsettled"} {
		if !got[p] {
			t.Errorf("%s forgotten", p)
		}
	}
	if s, _, _ := mesh.ReadState(f.ctx, f.c, "gone"); s.State != "idle" {
		t.Errorf("gone's state stays: %+v", s)
	}

	forgetDoneOnce(f.ctx, f.d) // its child gone, the parent goes
	if paths()["dad"] {
		t.Error("dad kept once its child was forgotten")
	}
}
