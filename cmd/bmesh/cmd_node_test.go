package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp/t3fake"
)

// stateAt puts path in state, dated at.
func stateAt(t *testing.T, c *mesh.Client, path, state string, at time.Time) {
	t.Helper()
	meshtest.ForceState(t, c, path, mesh.State{State: state, By: "node", At: at.UTC().Format(time.RFC3339)})
}

func listed(out string) []string {
	var paths []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n")[1:] {
		if f := strings.Fields(l); len(f) > 0 && f[0] != "…" {
			paths = append(paths, f[0])
		}
	}
	return paths
}

func TestMeshStatusHidesFinishedNodes(t *testing.T) {
	c, ctx := setup(t)
	day := time.Now().Add(-25 * time.Hour)
	for _, n := range []struct{ path, role, parent, state string }{
		{"app-001", "lead", "", "done"},      // a day old: hidden
		{"app-002", "lead", "", "blocked"},   // a day old, but waits on the operator
		{"app-003", "lead", "", "done"},      // an hour old
		{"app-004", "lead", "", "working"},   // a day in, still working
		{"app-round-1", "round", "", "done"}, // the whole round is a day old: hidden
		{"app-round-1.eng", "engineer", "app-round-1", "done"},
		{"app-round-1.ux", "engineer", "app-round-1", "done"},
		{"app-round-2", "round", "", "done"}, // one child is blocked: all shown
		{"app-round-2.eng", "engineer", "app-round-2", "done"},
		{"app-round-2.ux", "engineer", "app-round-2", "blocked"},
	} {
		putNode(t, c, ctx, mesh.Node{Path: n.path, Role: n.role, Parent: n.parent})
		at := day
		if n.path == "app-003" {
			at = time.Now().Add(-time.Hour)
		}
		stateAt(t, c, n.path, n.state, at)
	}

	out, errs, code := runCLI(t, nil, "mesh-status")
	want := "app-002 app-003 app-004 app-round-2 app-round-2.eng app-round-2.ux"
	if code != 0 || strings.Join(listed(out), " ") != want {
		t.Fatalf("%d %q %q, want %s", code, out, errs, want)
	}
	if !strings.HasSuffix(out, "\n… 4 finished nodes older than 24h (--all shows them)\n") {
		t.Fatalf("closing line: %q", out)
	}

	out, _, code = runCLI(t, nil, "mesh-status", "--all")
	if code != 0 || len(listed(out)) != 10 || strings.Contains(out, "…") {
		t.Fatalf("--all: %d %q", code, out)
	}
	out, _, code = runCLI(t, nil, "mesh-status", "--json")
	var rows []statusRow
	if code != 0 || json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 10 {
		t.Fatalf("--json: %d %q", code, out)
	}

	// keep_done = "48h": the day-old nodes show again.
	cfg := os.Getenv("BMESH_CONFIG")
	body, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, append(body, "[status]\nkeep_done = \"2d\"\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, code = runCLI(t, nil, "mesh-status")
	if code != 0 || len(listed(out)) != 10 || strings.Contains(out, "…") {
		t.Fatalf("keep_done 2d: %d %q", code, out)
	}
}

func TestNodeForget(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-001", Worktree: "/somewhere/app-001"})
	putNode(t, c, ctx, mesh.Node{Path: "app-001.x", Role: "executor", Parent: "app-001"})
	lead := mesh.Path{"app-001"}
	if err := mesh.EnsureInbox(ctx, c, lead); err != nil {
		t.Fatal(err)
	}
	if err := mesh.PutRunner(ctx, c, "app-001", mesh.RunnerSample{Known: true}); err != nil {
		t.Fatal(err)
	}
	stateAt(t, c, "app-001", "working", time.Now())
	stateAt(t, c, "app-001.x", "working", time.Now())

	_, errs, code := runCLI(t, nil, "node", "forget", "app-001")
	if code != 3 || errs != "bmesh: refused: app-001 is working; only a done or blocked node is forgotten\n" {
		t.Fatalf("working: %d %q", code, errs)
	}
	stateAt(t, c, "app-001", "done", time.Now())
	_, errs, code = runCLI(t, nil, "node", "forget", "app-001")
	if code != 3 || errs != "bmesh: refused: app-001.x under app-001 is working\n" {
		t.Fatalf("working executor: %d %q", code, errs)
	}
	if _, _, code := runCLI(t, nil, "node", "forget", "nobody"); code != 3 {
		t.Fatalf("unknown node: %d", code)
	}

	stateAt(t, c, "app-001.x", "blocked", time.Now())
	out, errs, code := runCLI(t, nil, "node", "forget", "app-001")
	if code != 0 || out != "forgot app-001\nworktree /somewhere/app-001 kept\n" {
		t.Fatalf("forget: %d %q %q", code, out, errs)
	}
	if _, ok, err := mesh.GetNode(ctx, c, "app-001"); err != nil || ok {
		t.Fatalf("node kept: %v %v", ok, err)
	}
	if _, rev, err := mesh.ReadState(ctx, c, "app-001"); err != nil || rev != 0 {
		t.Fatalf("state kept: rev %d %v", rev, err)
	}
	if _, ok, err := mesh.GetRunner(ctx, c, "app-001"); err != nil || ok {
		t.Fatalf("runner kept: %v %v", ok, err)
	}
	if _, err := c.JS.Consumer(ctx, mesh.StreamMesh, mesh.InboxConsumer(lead)); err == nil {
		t.Fatal("inbox consumer kept")
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "app-001.x"); !ok {
		t.Fatal("executor forgotten with its lead")
	}
}

func TestNodeForgetWorktree(t *testing.T) {
	c, ctx := setup(t)
	repo := gitRepo(t)
	real, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "app-001")
	meshtest.Git(t, real, "worktree", "add", "-q", "-b", "t3code/lead", wt)
	// The node names the branch dispatch chose; the runner renamed it since.
	putNode(t, c, ctx, mesh.Node{Path: "app-001", RepoPath: real, Worktree: wt, Branch: "t3code/c4422981"})
	stateAt(t, c, "app-001", "done", time.Now())

	// Dirty: refused, nothing touched.
	if err := os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errs, code := runCLI(t, nil, "node", "forget", "app-001", "--worktree")
	if code != 3 || errs != "bmesh: refused: worktree "+wt+" is dirty\n" {
		t.Fatalf("dirty: %d %q", code, errs)
	}
	// Committed but not on main: refused.
	meshtest.Git(t, wt, "add", "-A")
	meshtest.Git(t, wt, "commit", "-q", "-m", "b")
	_, errs, code = runCLI(t, nil, "node", "forget", "app-001", "--worktree")
	if code != 3 || errs != "bmesh: refused: branch t3code/lead is not merged into main\n" {
		t.Fatalf("unmerged: %d %q", code, errs)
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "app-001"); !ok || !dirExists(wt) {
		t.Fatal("a refusal touched the node or the worktree")
	}

	meshtest.Git(t, real, "merge", "-q", "--ff-only", "t3code/lead")
	out, errs, code := runCLI(t, nil, "node", "forget", "app-001", "--worktree")
	want := "removed worktree " + wt + "\ndeleted branch t3code/lead\nforgot app-001\n"
	if code != 0 || out != want {
		t.Fatalf("forget: %d %q %q, want %q", code, out, errs, want)
	}
	if dirExists(wt) || worktrees(t, real) != 1 {
		t.Fatal("worktree kept")
	}
	if b := gitLines(t, real, "branch", "--list", "t3code/lead"); len(b) != 0 {
		t.Fatalf("branch kept: %v", b)
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "app-001"); ok {
		t.Fatal("node kept")
	}
}

// An executor whose commits reached main through its lead's rebase (new
// SHAs) has landed; its parent's branch, gone since, is not asked about.
func TestNodeForgetWorktreeRebasedExecutor(t *testing.T) {
	c, ctx := setup(t)
	repo := gitRepo(t)
	real, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "app-001.x")
	meshtest.Git(t, real, "worktree", "add", "-q", "-b", "t3code/lead-x", wt)
	if err := os.WriteFile(filepath.Join(wt, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, wt, "add", "-A")
	meshtest.Git(t, wt, "commit", "-q", "-m", "x")
	putNode(t, c, ctx, mesh.Node{Path: "app-001", Branch: "t3code/lead"}) // its branch is gone
	putNode(t, c, ctx, mesh.Node{Path: "app-001.x", Role: "executor", Parent: "app-001", RepoPath: real, Worktree: wt, Branch: "t3code/lead-x"})
	stateAt(t, c, "app-001.x", "done", time.Now())

	_, errs, code := runCLI(t, nil, "node", "forget", "app-001.x", "--worktree")
	if code != 3 || errs != "bmesh: refused: branch t3code/lead-x is not merged into main\n" {
		t.Fatalf("unmerged: %d %q", code, errs)
	}
	// main moves, then the work lands replayed on top of it.
	if err := os.WriteFile(filepath.Join(real, "m.txt"), []byte("m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, real, "add", "-A")
	meshtest.Git(t, real, "commit", "-q", "-m", "m")
	meshtest.Git(t, real, "cherry-pick", "t3code/lead-x")
	out, errs, code := runCLI(t, nil, "node", "forget", "app-001.x", "--worktree")
	if code != 0 || out != "removed worktree "+wt+"\ndeleted branch t3code/lead-x\nforgot app-001.x\n" {
		t.Fatalf("forget: %d %q %q", code, out, errs)
	}
}

func TestNodeForgetIdle(t *testing.T) {
	c, fake := leadSetup(t) // the t3 runner, on a fake T3
	ctx := t.Context()
	idle := func(path string, n mesh.Node) {
		n.Path = path
		if n.Runner.Kind == "" {
			n.Runner = mesh.NodeRunner{Kind: "t3", Thread: "mcp:th-" + path}
			fake.AddThread(&t3fake.Thread{ID: n.Runner.Thread})
		}
		putNode(t, c, ctx, n)
	}
	forget := func(path string, env map[string]string) (string, string, int) {
		return runCLI(t, env, "node", "forget", path)
	}

	// No event of its own (a child's and the daemon's don't count) and a
	// stopped thread: forgotten.
	idle("app-001", mesh.Node{})
	putNode(t, c, ctx, mesh.Node{Path: "app-001.x", Role: "executor", Parent: "app-001"})
	stateAt(t, c, "app-001.x", "done", time.Now())
	for _, ev := range []mesh.Event{{From: "app-001.x", Event: "done"}, {From: "daemon." + testHost, Event: "wake", To: "app-001"}} {
		if err := mesh.PublishEvent(ctx, c, ev); err != nil {
			t.Fatal(err)
		}
	}
	if out, errs, code := forget("app-001", nil); code != 0 || out != "forgot app-001\n" {
		t.Fatalf("stopped: %d %q %q", code, out, errs)
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "app-001"); ok {
		t.Fatal("app-001 still registered")
	}

	// An event of its own: refused, whatever its thread says.
	idle("app-002", mesh.Node{})
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "app-002", Event: "progress"}); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := forget("app-002", nil); code != 3 || errs != "bmesh: refused: app-002 is idle but published progress; only an idle node that never reported is forgotten\n" {
		t.Fatalf("own event: %d %q", code, errs)
	}

	// Its thread running, or the runner failing: refused.
	idle("app-003", mesh.Node{})
	fake.Do(func(s *t3fake.Server) {
		s.Threads["mcp:th-app-003"].ActiveRunID = "r1"
		s.Threads["mcp:th-app-003"].Runs = []t3fake.Run{{ID: "r1", Status: "running"}}
	})
	if _, errs, code := forget("app-003", nil); code != 3 || errs != "bmesh: refused: app-003 is idle and its thread mcp:th-app-003 is running; stop it first\n" {
		t.Fatalf("running: %d %q", code, errs)
	}
	fake.Do(func(s *t3fake.Server) { delete(s.Threads, "mcp:th-app-003") })
	if _, errs, code := forget("app-003", nil); code != 3 || !strings.HasPrefix(errs, "bmesh: refused: app-003 is idle and its thread may be running (") {
		t.Fatalf("status fails: %d %q", code, errs)
	}

	// Another host, or another runner kind: refused, naming the host.
	idle("app-004", mesh.Node{Host: "otherhost"})
	if _, errs, code := forget("app-004", nil); code != 3 || errs != "bmesh: refused: app-004 is idle and its thread is on otherhost; run it on otherhost\n" {
		t.Fatalf("other host: %d %q", code, errs)
	}
	idle("app-006", mesh.Node{})
	n6, _, _ := mesh.GetNode(ctx, c, "app-006")
	n6.Host = ""
	if err := mesh.PutNode(ctx, c, n6); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := forget("app-006", nil); code != 3 || errs != "bmesh: refused: app-006 is idle and names no host\n" {
		t.Fatalf("no host: %d %q", code, errs)
	}
	idle("app-005", mesh.Node{Runner: mesh.NodeRunner{Kind: "tmux", Thread: "w"}})
	if _, errs, code := forget("app-005", nil); code != 3 || errs != "bmesh: refused: app-005 is idle and its runner is \"tmux\", not this box's t3; run it on "+testHost+"\n" {
		t.Fatalf("other kind: %d %q", code, errs)
	}

	// A runner that can't tell (tmux: Known false): refused.
	cfg := os.Getenv("BMESH_CONFIG")
	body, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(strings.Replace(string(body), "kind = \"t3\"", "kind = \"tmux\"", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := forget("app-005", nil); code != 3 || errs != "bmesh: refused: app-005 is idle and its thread may be running; stop it first\n" {
		t.Fatalf("unknown: %d %q", code, errs)
	}
	for _, p := range []string{"app-002", "app-003", "app-004", "app-005"} {
		if _, ok, _ := mesh.GetNode(ctx, c, p); !ok {
			t.Fatalf("%s forgotten", p)
		}
	}
}

// node forget --worktree of another box's node asks that box: it removes
// the worktree there and both forget the node; with that box's daemon
// stopped, the job waits and the node stays.
func TestNodeForgetWorktreeOnAnotherBox(t *testing.T) {
	tb := twoBoxSetup(t)
	wt := tb.doneNode(t, tb.mac, "app-001", "bmesh/a")
	_, stopMac := serveBox(t, tb.mac)

	out, errs, code := tb.k16.run(t, "", "node", "forget", "app-001", "--worktree", "--wait", "20s")
	want := "mac: removed worktree " + wt + "; deleted branch bmesh/a; forgot app-001\nforgot app-001\n"
	if code != 0 || out != want {
		t.Fatalf("forget: %d %q %s, want %q", code, out, errs, want)
	}
	if dirExists(wt) {
		t.Fatal("worktree kept")
	}
	if _, ok, _ := mesh.GetNode(tb.ctx, tb.c, "app-001"); ok {
		t.Fatal("node kept")
	}

	// A refusal there: exit 3, nothing forgotten.
	wt = filepath.Join(tb.wts["mac"], "app", "app-002")
	meshtest.Git(t, tb.repo["mac"], "worktree", "add", "-q", "-b", "bmesh/b", wt)
	meshtest.CommitFiles(t, wt, map[string]string{"b.txt": "b\n"})
	if err := mesh.PutNode(tb.ctx, tb.c, mesh.Node{Path: "app-002", Role: "lead", Host: "mac", Worktree: wt}); err != nil {
		t.Fatal(err)
	}
	meshtest.ForceState(t, tb.c, "app-002", mesh.State{State: "done", By: "node"})
	_, errs, code = tb.k16.run(t, "", "node", "forget", "app-002", "--worktree", "--wait", "20s")
	if code != exitRefused || errs != "bmesh: mac: refused: branch bmesh/b is not merged into main\n" {
		t.Fatalf("unmerged: %d %q", code, errs)
	}
	if _, ok, _ := mesh.GetNode(tb.ctx, tb.c, "app-002"); !ok || !dirExists(wt) {
		t.Fatal("a refusal forgot the node or removed its worktree")
	}

	// mac asleep: queued, the node stays.
	stopMac()
	meshtest.Git(t, tb.repo["mac"], "merge", "-q", "--ff-only", "bmesh/b")
	out, errs, code = tb.k16.run(t, "", "node", "forget", "app-002", "--worktree", "--wait", "1s")
	if code != 0 || out != "queued; the node stays until mac answers\n" {
		t.Fatalf("asleep: %d %q %s", code, out, errs)
	}
	if _, ok, _ := mesh.GetNode(tb.ctx, tb.c, "app-002"); !ok {
		t.Fatal("an unanswered forget forgot the node")
	}
	// mac wakes and takes it.
	serveBox(t, tb.mac)
	waitFor(t, "mac forgets app-002", func() bool {
		_, ok, _ := mesh.GetNode(tb.ctx, tb.c, "app-002")
		return !ok && !dirExists(wt)
	})
}

func TestNodeForgetWorktreeOnAnotherBoxWithHostsOff(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-001", Host: "otherhost", Worktree: "/there/app-001"})
	stateAt(t, c, "app-001", "done", time.Now())
	_, errs, code := runCLI(t, nil, "node", "forget", "app-001", "--worktree")
	if code != exitRefused || errs != "bmesh: refused: worktree /there/app-001 is on otherhost; run it there\n" {
		t.Fatalf("%d %q", code, errs)
	}
}

// fullState reads path's state.
func fullState(t *testing.T, c *mesh.Client, path string) mesh.State {
	t.Helper()
	s, _, err := mesh.ReadState(t.Context(), c, path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// interrupts is the threads t3_thread_interrupt was called for.
func interrupts(fake *t3fake.Server) []string {
	var out []string
	fake.Do(func(s *t3fake.Server) {
		for _, c := range s.Calls {
			if c.Tool == "t3_thread_interrupt" {
				out = append(out, c.Args["threadId"].(string))
			}
		}
	})
	return out
}

// node stop on a lead stops its executors first, then interrupts its
// thread and writes it blocked, stopped by whoever ran it; forget then
// clears it. A done node is a no-op.
func TestNodeStopLeadAndExecutors(t *testing.T) {
	c, fake := leadSetup(t)
	ctx := t.Context()
	putNode(t, c, ctx, mesh.Node{Path: "app-001", Role: "lead", Host: testHost, Runner: mesh.NodeRunner{Kind: "t3", Thread: "mcp:th-1"}})
	fake.AddThread(&t3fake.Thread{ID: "mcp:th-1", ActiveRunID: "r1", Runs: []t3fake.Run{{ID: "r1", Status: "running"}}})
	stateAt(t, c, "app-001", "working", time.Now())
	putNode(t, c, ctx, mesh.Node{Path: "app-001.x", Role: "executor", Parent: "app-001", Host: testHost})
	stateAt(t, c, "app-001.x", "working", time.Now())
	putNode(t, c, ctx, mesh.Node{Path: "app-001.y", Role: "executor", Parent: "app-001", Host: testHost})
	stateAt(t, c, "app-001.y", "done", time.Now())

	out, errs, code := runCLI(t, nil, "node", "stop", "app-001", "--text", "wrong spec")
	if code != 0 || out != "stopped app-001.x\nstopped app-001\n" {
		t.Fatalf("stop: %d %q %q", code, out, errs)
	}
	for _, p := range []string{"app-001", "app-001.x"} {
		if s := fullState(t, c, p); s.State != "blocked" || s.Text != "stopped by "+testHost+": wrong spec" {
			t.Fatalf("%s: %+v", p, s)
		}
	}
	if s := fullState(t, c, "app-001.y"); s.State != "done" {
		t.Fatalf("a done executor was touched: %+v", s)
	}
	if got := interrupts(fake); len(got) != 1 || got[0] != "mcp:th-1" {
		t.Fatalf("interrupts %q", got)
	}
	fake.Do(func(s *t3fake.Server) {
		if th := s.Threads["mcp:th-1"]; th.ActiveRunID != "" || th.Runs[0].Status != "interrupted" {
			t.Errorf("thread %+v", th)
		}
	})

	// Again: already blocked; its thread is interrupted again (a stop whose
	// interrupt failed is run again), which an idle thread takes as nothing.
	if out, errs, code := runCLI(t, nil, "node", "stop", "app-001"); code != 0 || out != "already blocked\n" {
		t.Fatalf("again: %d %q %q", code, out, errs)
	}
	if got := interrupts(fake); len(got) != 2 {
		t.Fatalf("again: interrupts %q", got)
	}
	if out, errs, code := runCLI(t, nil, "node", "forget", "app-001"); code != 0 || out != "forgot app-001\n" {
		t.Fatalf("forget: %d %q %q", code, out, errs)
	}

	putNode(t, c, ctx, mesh.Node{Path: "app-002", Role: "lead", Host: testHost, Runner: mesh.NodeRunner{Kind: "t3", Thread: "mcp:th-2"}})
	stateAt(t, c, "app-002", "done", time.Now())
	if out, errs, code := runCLI(t, nil, "node", "stop", "app-002"); code != 0 || out != "already done\n" {
		t.Fatalf("done: %d %q %q", code, out, errs)
	}
	if got := interrupts(fake); len(got) != 2 {
		t.Fatalf("a done node was interrupted: %q", got)
	}

	// A node registered but never started (idle) is stopped too.
	putNode(t, c, ctx, mesh.Node{Path: "app-003", Role: "lead", Host: testHost, Runner: mesh.NodeRunner{Kind: "t3", Thread: "mcp:th-3"}})
	fake.AddThread(&t3fake.Thread{ID: "mcp:th-3"})
	if out, errs, code := runCLI(t, nil, "node", "stop", "app-003"); code != 0 || out != "stopped app-003\n" {
		t.Fatalf("idle: %d %q %q", code, out, errs)
	}
	if s := fullState(t, c, "app-003"); s.State != "blocked" || s.Text != "stopped by "+testHost {
		t.Fatalf("idle: %+v", s)
	}
}

// node stop on a round stops both children, then settles the round
// blocked; a round whose children are all gone is blocked directly, and
// forget clears it whatever its state.
func TestNodeStopRound(t *testing.T) {
	c, fake := leadSetup(t)
	ctx := t.Context()
	putNode(t, c, ctx, mesh.Node{Path: "app-round-3", Role: "round", Repo: "app", Host: testHost})
	stateAt(t, c, "app-round-3", "silent", time.Now())
	for _, ch := range []string{"eng", "ux"} {
		p, th := "app-round-3."+ch, "mcp:th-"+ch
		putNode(t, c, ctx, mesh.Node{Path: p, Role: "engineer", Parent: "app-round-3", Host: testHost, Runner: mesh.NodeRunner{Kind: "t3", Thread: th}})
		fake.AddThread(&t3fake.Thread{ID: th, ActiveRunID: "r1", Runs: []t3fake.Run{{ID: "r1", Status: "running"}}})
		stateAt(t, c, p, "working", time.Now())
	}
	out, errs, code := runCLI(t, nil, "node", "stop", "app-round-3")
	if code != 0 || out != "stopped app-round-3.eng\nstopped app-round-3.ux\napp-round-3 blocked\n" {
		t.Fatalf("stop: %d %q %q", code, out, errs)
	}
	if s := fullState(t, c, "app-round-3"); s.State != "blocked" || s.Text != "eng blocked: stopped by "+testHost {
		t.Fatalf("round: %+v", s)
	}
	if got := interrupts(fake); len(got) != 2 {
		t.Fatalf("interrupts %q", got)
	}
	if out, errs, code := runCLI(t, nil, "node", "forget", "app-round-3"); code != 0 || out != "forgot app-round-3\n" {
		t.Fatalf("forget: %d %q %q", code, out, errs)
	}

	// Round 4's children were forgotten before it settled: it stays silent
	// with nothing to settle from.
	putNode(t, c, ctx, mesh.Node{Path: "app-round-4", Role: "round", Repo: "app", Host: testHost})
	stateAt(t, c, "app-round-4", "silent", time.Now())
	if out, errs, code := runCLI(t, nil, "node", "forget", "app-round-4"); code != 0 || out != "forgot app-round-4\n" {
		t.Fatalf("forget childless: %d %q %q", code, out, errs)
	}
	putNode(t, c, ctx, mesh.Node{Path: "app-round-5", Role: "round", Repo: "app", Host: testHost})
	stateAt(t, c, "app-round-5", "silent", time.Now())
	if out, errs, code := runCLI(t, nil, "node", "stop", "app-round-5", "--text", "children gone"); code != 0 || out != "app-round-5 blocked\n" {
		t.Fatalf("stop childless: %d %q %q", code, out, errs)
	}
	if s := fullState(t, c, "app-round-5"); s.State != "blocked" || s.Text != "stopped by "+testHost+": children gone" {
		t.Fatalf("childless: %+v", s)
	}
}

// A thread on another box with [hosts] off is refused before anything is
// stopped.
func TestNodeStopOnAnotherBoxWithHostsOff(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-001", Role: "lead", Host: testHost})
	stateAt(t, c, "app-001", "working", time.Now())
	putNode(t, c, ctx, mesh.Node{Path: "app-001.x", Role: "executor", Parent: "app-001", Host: "otherhost",
		Runner: mesh.NodeRunner{Kind: "t3", Thread: "mcp:x"}})
	stateAt(t, c, "app-001.x", "working", time.Now())
	_, errs, code := runCLI(t, nil, "node", "stop", "app-001")
	if code != exitRefused || errs != "bmesh: refused: app-001.x runs on otherhost; run it on otherhost\n" {
		t.Fatalf("%d %q", code, errs)
	}
	for _, p := range []string{"app-001", "app-001.x"} {
		if s := fullState(t, c, p); s.State != "working" {
			t.Fatalf("%s touched: %+v", p, s)
		}
	}
}

// node stop of another box's thread sends it stop-thread: that box
// interrupts it through its own runner and writes it stopped. With that
// box asleep the node is marked stopped from here, and the box stops the
// thread when it wakes.
func TestNodeStopOnAnotherBox(t *testing.T) {
	tb := twoBoxSetup(t)
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-tmux.sh"))
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "tmux.log")
	t.Setenv("BMESH_TMUX_COMMAND", fake)
	t.Setenv("FAKE_TMUX_LOG", log)
	escapes := func() int {
		b, _ := os.ReadFile(log)
		return strings.Count(string(b), "Escape")
	}
	for _, n := range []mesh.Node{
		{Path: "app-001", Role: "lead"},
		{Path: "app-round-1", Role: "round"},
		{Path: "app-round-1.eng", Role: "engineer", Parent: "app-round-1"},
		{Path: "app-round-1.ux", Role: "engineer", Parent: "app-round-1"},
	} {
		n.Host = "mac"
		if n.Role != "round" {
			n.Runner = mesh.NodeRunner{Kind: "tmux", Thread: n.Path}
		}
		if err := mesh.PutNode(tb.ctx, tb.c, n); err != nil {
			t.Fatal(err)
		}
		meshtest.ForceState(t, tb.c, n.Path, mesh.State{State: "working", By: "node"})
	}
	_, stopMac := serveBox(t, tb.mac)

	out, errs, code := tb.k16.run(t, "", "node", "stop", "app-001", "--text", "enough", "--wait", "20s")
	if code != 0 || out != "mac: stopped app-001\n" {
		t.Fatalf("stop: %d %q %s", code, out, errs)
	}
	if s, _, _ := mesh.ReadState(tb.ctx, tb.c, "app-001"); s.State != "blocked" || s.Text != "stopped by k16: enough" {
		t.Fatalf("state %+v", s)
	}
	if n := escapes(); n != 1 {
		t.Fatalf("%d interrupts on mac", n)
	}

	// mac asleep: both of a round's threads are asked at once, within one
	// wait, and marked stopped from here, so the round settles; mac
	// interrupts the threads when it takes the jobs.
	stopMac()
	out, errs, code = tb.k16.run(t, "", "node", "stop", "app-round-1", "--wait", "1s")
	want := "queued; app-round-1.eng is marked stopped and mac stops its thread when it answers\n" +
		"queued; app-round-1.ux is marked stopped and mac stops its thread when it answers\n" +
		"app-round-1 blocked\n"
	if code != 0 || out != want {
		t.Fatalf("asleep: %d %q %s", code, out, errs)
	}
	if s, _, _ := mesh.ReadState(tb.ctx, tb.c, "app-round-1.ux"); s.State != "blocked" || s.Text != "stopped by k16" {
		t.Fatalf("asleep state %+v", s)
	}
	serveBox(t, tb.mac)
	waitFor(t, "mac interrupts both threads", func() bool { return escapes() == 3 })
}

// wipeFixture is a lead with two executors (one with a worktree merged
// into the lead's branch, the lead's merged into main), its siblings
// app-06 and app-066-2 whose paths share its text, and feedback in three
// families: the lead's own, the siblings' shared one, and another.
type wipeFixture struct {
	c        *mesh.Client
	ctx      context.Context
	repo     string
	leadWT   string
	execWT   string
	briefDir string
}

func newWipeFixture(t *testing.T) wipeFixture {
	t.Helper()
	c, ctx := setup(t)
	f := wipeFixture{c: c, ctx: ctx, briefDir: filepath.Join(t.TempDir(), "bmesh", "briefs")}
	t.Setenv("BMESH_CACHE_DIR", filepath.Dir(filepath.Dir(f.briefDir)))
	repo := gitRepo(t)
	real, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	f.repo = real
	f.leadWT = filepath.Join(t.TempDir(), "app-066")
	f.execWT = filepath.Join(t.TempDir(), "app-066.x")
	meshtest.Git(t, real, "worktree", "add", "-q", "-b", "bmesh/lead", f.leadWT)
	meshtest.Git(t, f.leadWT, "worktree", "add", "-q", "-b", "bmesh/lead-x", f.execWT)
	meshtest.CommitFiles(t, f.execWT, map[string]string{"x.txt": "x\n"})
	meshtest.Git(t, f.leadWT, "merge", "-q", "--ff-only", "bmesh/lead-x")
	meshtest.Git(t, real, "merge", "-q", "--ff-only", "bmesh/lead")

	putNode(t, c, ctx, mesh.Node{Path: "app-066", RepoPath: real, Worktree: f.leadWT, Branch: "bmesh/lead", Family: "app-build-1"})
	putNode(t, c, ctx, mesh.Node{Path: "app-066.x", Role: "executor", Parent: "app-066", RepoPath: real, Worktree: f.execWT, Branch: "bmesh/lead-x"})
	putNode(t, c, ctx, mesh.Node{Path: "app-066.y", Role: "executor", Parent: "app-066"})
	putNode(t, c, ctx, mesh.Node{Path: "app-06", Family: "app-build-2"})
	putNode(t, c, ctx, mesh.Node{Path: "app-066-2", Family: "app-build-2"})
	if err := os.MkdirAll(f.briefDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for p, family := range map[string]string{"app-066": "app-build-1", "app-06": "app-build-2", "app-066-2": "app-build-2"} {
		if err := os.WriteFile(filepath.Join(f.briefDir, p+".md"), []byte("brief\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := mesh.PutLeadHold(ctx, c, mesh.LeadHold{Path: p, Gate: "land", By: "operator"}); err != nil {
			t.Fatal(err)
		}
		if _, err := mesh.PublishFeedback(ctx, c, mesh.Feedback{From: p, Family: family, Text: "lesson"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mesh.PublishFeedback(ctx, c, mesh.Feedback{From: "other-001", Family: "app-build-3", Text: "other"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"app-066", "app-066.x", "app-066.y", "app-06", "app-066-2"} {
		pp, _ := mesh.ParsePath(p)
		if err := mesh.EnsureInbox(ctx, c, pp); err != nil {
			t.Fatal(err)
		}
		if _, err := mesh.Send(ctx, c, mesh.Message{ID: "m-" + p, From: "operator", To: p, Kind: "text", Text: "hi"}); err != nil {
			t.Fatal(err)
		}
		if err := mesh.PutRunner(ctx, c, p, mesh.RunnerSample{Known: true}); err != nil {
			t.Fatal(err)
		}
		if err := mesh.PublishEvent(ctx, c, mesh.Event{From: p, Event: "started"}); err != nil {
			t.Fatal(err)
		}
		stateAt(t, c, p, "blocked", time.Now())
	}
	return f
}

// left is what the mesh still holds of path: its event and inbox
// messages, and the buckets with any history of its key.
func (f wipeFixture) left(t *testing.T, path string) (events, inbox int, keys []string) {
	t.Helper()
	ev, err := mesh.History(f.ctx, f.c, "bm."+path+".*", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	in, err := mesh.History(f.ctx, f.c, "bm.inbox."+path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []string{mesh.BucketNodes, mesh.BucketState, mesh.BucketRunner, mesh.BucketHolds} {
		kv, err := f.c.JS.KeyValue(f.ctx, b)
		if err != nil {
			t.Fatal(err)
		}
		if h, err := kv.History(f.ctx, path); err == nil && len(h) > 0 {
			keys = append(keys, b)
		}
	}
	return len(ev), len(in), keys
}

func TestNodeForgetWipe(t *testing.T) {
	f := newWipeFixture(t)
	c, ctx := f.c, f.ctx

	// Refused while anything in it runs, with the stop line.
	stateAt(t, c, "app-066.x", "working", time.Now())
	_, errs, code := runCLI(t, nil, "node", "forget", "app-066", "--wipe")
	if code != exitRefused || errs != "bmesh: refused: app-066.x under app-066 is working; stop it first: bmesh node stop app-066\n" {
		t.Fatalf("working executor: %d %q", code, errs)
	}
	stateAt(t, c, "app-066", "working", time.Now())
	_, errs, code = runCLI(t, nil, "node", "forget", "app-066", "--wipe")
	if code != exitRefused || errs != "bmesh: refused: app-066 is working; stop it first: bmesh node stop app-066\n" {
		t.Fatalf("working: %d %q", code, errs)
	}
	stateAt(t, c, "app-066", "blocked", time.Now())
	stateAt(t, c, "app-066.x", "done", time.Now())
	if _, _, code := runCLI(t, nil, "node", "forget", "app-066", "--dry-run"); code != exitUsage {
		t.Fatalf("--dry-run without --wipe: %d", code)
	}
	if _, errs, code := runCLI(t, nil, "node", "forget", "routine.verify", "--wipe"); code != exitRefused || !strings.Contains(errs, "routine") {
		t.Fatalf("routine: %d %q", code, errs)
	}

	// --dry-run says what goes and changes nothing.
	want := "app-066: 1 event, 4 keys, inbox, 1 feedback, brief, worktree, branch\n" +
		"%[1]s app-066.x: 1 event, 3 keys, inbox, worktree, branch\n" +
		"%[1]s app-066.y: 1 event, 3 keys, inbox\n"
	out, errs, code := runCLI(t, nil, "node", "forget", "app-066", "--wipe", "--dry-run")
	if code != 0 || out != fmt.Sprintf("would wipe "+want, "would wipe") {
		t.Fatalf("dry run: %d %q %q", code, out, errs)
	}
	for _, p := range []string{"app-066", "app-066.x", "app-066.y"} {
		if ev, in, keys := f.left(t, p); ev != 1 || in != 1 || len(keys) < 3 {
			t.Fatalf("dry run touched %s: %d %d %v", p, ev, in, keys)
		}
	}
	if !dirExists(f.leadWT) || !dirExists(f.execWT) {
		t.Fatal("dry run removed a worktree")
	}

	out, errs, code = runCLI(t, nil, "node", "forget", "app-066", "--wipe")
	if code != 0 || out != fmt.Sprintf("wiped "+want, "wiped") {
		t.Fatalf("wipe: %d %q %q", code, out, errs)
	}

	// What goes: every message, key history and consumer of the three.
	for _, p := range []string{"app-066", "app-066.x", "app-066.y"} {
		if ev, in, keys := f.left(t, p); ev != 0 || in != 0 || len(keys) != 0 {
			t.Fatalf("%s left: %d events, %d inbox, keys in %v", p, ev, in, keys)
		}
		pp, _ := mesh.ParsePath(p)
		if _, err := c.JS.Consumer(ctx, mesh.StreamMesh, mesh.InboxConsumer(pp)); err == nil {
			t.Fatalf("%s: inbox consumer kept", p)
		}
	}
	if fb, err := mesh.ListFeedback(ctx, c, "app-build-1"); err != nil || len(fb) != 0 {
		t.Fatalf("the lead's family's feedback kept: %v %v", fb, err)
	}
	if _, err := os.Stat(filepath.Join(f.briefDir, "app-066.md")); !os.IsNotExist(err) {
		t.Fatalf("brief kept: %v", err)
	}
	if dirExists(f.leadWT) || dirExists(f.execWT) || worktrees(t, f.repo) != 1 {
		t.Fatal("worktree kept")
	}
	if b := gitLines(t, f.repo, "branch", "--list", "bmesh/*"); len(b) != 0 {
		t.Fatalf("branches kept: %v", b)
	}
	wiped, err := mesh.History(ctx, c, "bm.daemon.*.wiped", time.Time{})
	if err != nil || len(wiped) != 1 || wiped[0].Subject != "bm.daemon."+testHost+".wiped" {
		t.Fatalf("wiped line: %v %v", wiped, err)
	}
	var ev mesh.Event
	if err := json.Unmarshal(wiped[0].Data, &ev); err != nil || ev.Text != "app-066 by "+testHost {
		t.Fatalf("wiped text: %q %v", ev.Text, err)
	}

	// What stays: the siblings whose paths share its text, and the other
	// families' feedback.
	for _, p := range []string{"app-06", "app-066-2"} {
		if ev, in, keys := f.left(t, p); ev != 1 || in != 1 || len(keys) != 4 {
			t.Fatalf("sibling %s lost: %d events, %d inbox, keys in %v", p, ev, in, keys)
		}
		if _, err := os.Stat(filepath.Join(f.briefDir, p+".md")); err != nil {
			t.Fatalf("sibling %s brief: %v", p, err)
		}
	}
	for fam, n := range map[string]int{"app-build-2": 2, "app-build-3": 1} {
		if fb, err := mesh.ListFeedback(ctx, c, fam); err != nil || len(fb) != n {
			t.Fatalf("%s feedback: %d %v, want %d", fam, len(fb), err, n)
		}
	}

	// A family with a node outside the wipe keeps its feedback.
	out, errs, code = runCLI(t, nil, "node", "forget", "app-06", "--wipe")
	if code != 0 || out != "wiped app-06: 1 event, 4 keys, inbox, brief\n" {
		t.Fatalf("wipe app-06: %d %q %q", code, out, errs)
	}
	if fb, err := mesh.ListFeedback(ctx, c, "app-build-2"); err != nil || len(fb) != 2 {
		t.Fatalf("shared family's feedback: %d %v", len(fb), err)
	}
	if ev, in, keys := f.left(t, "app-066-2"); ev != 1 || in != 1 || len(keys) != 4 {
		t.Fatalf("app-066-2 lost: %d events, %d inbox, keys in %v", ev, in, keys)
	}
	// Twice is the same: the node is gone.
	if _, errs, code := runCLI(t, nil, "node", "forget", "app-066", "--wipe"); code != exitRefused || errs != "bmesh: refused: no node app-066\n" {
		t.Fatalf("again: %d %q", code, errs)
	}
}

// Another box's node has its worktree and brief removed there, through
// forget-worktree; the mesh is wiped once that box answers.
func TestNodeForgetWipeOnAnotherBox(t *testing.T) {
	tb := twoBoxSetup(t)
	cache := t.TempDir()
	t.Setenv("BMESH_CACHE_DIR", cache)
	wt := tb.doneNode(t, tb.mac, "app-001", "bmesh/a")
	briefs := filepath.Join(cache, "bmesh", "briefs")
	if err := os.MkdirAll(briefs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(briefs, "app-001.md"), []byte("brief\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mesh.PublishEvent(tb.ctx, tb.c, mesh.Event{From: "app-001", Event: "done"}); err != nil {
		t.Fatal(err)
	}

	// mac asleep: the mesh is untouched until it answers.
	out, errs, code := tb.k16.run(t, "", "node", "forget", "app-001", "--wipe", "--wait", "1s")
	if code != 0 || out != "queued; nothing is wiped from the mesh until mac answers: run it again then\n" {
		t.Fatalf("asleep: %d %q %s", code, out, errs)
	}
	if _, ok, _ := mesh.GetNode(tb.ctx, tb.c, "app-001"); !ok {
		t.Fatal("an unanswered wipe forgot the node")
	}
	serveBox(t, tb.mac)
	waitFor(t, "mac removes the worktree", func() bool { return !dirExists(wt) })
	if _, ok, _ := mesh.GetNode(tb.ctx, tb.c, "app-001"); !ok {
		t.Fatal("mac forgot the node of a wipe")
	}

	out, errs, code = tb.k16.run(t, "", "node", "forget", "app-001", "--wipe", "--wait", "20s")
	if code != 0 || out != "wiped app-001: 1 event, 2 keys, worktree\n" {
		t.Fatalf("wipe: %d %q %s", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(briefs, "app-001.md")); !os.IsNotExist(err) {
		t.Fatalf("brief kept: %v", err)
	}
	if h, err := mesh.History(tb.ctx, tb.c, "bm.app-001.>", time.Time{}); err != nil || len(h) != 0 {
		t.Fatalf("events kept: %d %v", len(h), err)
	}
}
