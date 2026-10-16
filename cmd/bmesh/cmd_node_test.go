package main

import (
	"encoding/json"
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
