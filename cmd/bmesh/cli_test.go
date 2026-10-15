package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

const testHost = "testhost"

// setup starts a server, points BMESH_CONFIG at a config naming it, provisions
// it (the daemon's job outside tests) and returns a client for arranging state.
func setup(t *testing.T) (*mesh.Client, context.Context) {
	t.Helper()
	url := meshtest.Start(t)
	cfg := filepath.Join(t.TempDir(), "config.toml")
	body := "[mesh]\nnats = \"" + url + "\"\n[host]\nname = \"" + testHost + "\"\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMESH_CONFIG", cfg)
	t.Setenv("BMESH_NODE", "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	c, err := mesh.Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if err := mesh.Provision(ctx, c); err != nil {
		t.Fatal(err)
	}
	return c, ctx
}

// runCLI runs the CLI in-process with env set for the test. BMESH_NODE is
// cleared unless env sets it. (The package's own run is the entry point, so
// this helper can't share its name.)
func runCLI(t *testing.T, env map[string]string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	t.Setenv("BMESH_NODE", "")
	for k, v := range env {
		t.Setenv(k, v)
	}
	var out, errb bytes.Buffer
	code = run(args, strings.NewReader(""), &out, &errb)
	return out.String(), errb.String(), code
}

func as(p string) map[string]string { return map[string]string{"BMESH_NODE": p} }

func putNode(t *testing.T, c *mesh.Client, ctx context.Context, n mesh.Node) {
	t.Helper()
	if n.Role == "" {
		n.Role = "lead"
	}
	if n.Host == "" {
		n.Host = testHost
	}
	if err := mesh.PutNode(ctx, c, n); err != nil {
		t.Fatal(err)
	}
}

func stateOf(t *testing.T, c *mesh.Client, ctx context.Context, p string) string {
	t.Helper()
	s, _, err := mesh.ReadState(ctx, c, p)
	if err != nil {
		t.Fatal(err)
	}
	return s.State
}

func TestEventsMoveState(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-001"})
	for _, step := range []struct{ event, want string }{
		{"started", "working"}, {"ask", "asking"}, {"blocked", "blocked"},
	} {
		if out, errs, code := runCLI(t, as("app-001"), step.event, "--text", "t-"+step.event); code != 0 {
			t.Fatalf("%s: code %d out %q err %q", step.event, code, out, errs)
		}
		if got := stateOf(t, c, ctx, "app-001"); got != step.want {
			t.Fatalf("after %s: state %s, want %s", step.event, got, step.want)
		}
	}
	hist, err := mesh.History(ctx, c, mesh.TreeFilter("app-001"), time.Time{})
	if err != nil || len(hist) != 3 || hist[2].Subject != "bm.app-001.blocked" {
		t.Fatalf("history %v %v", hist, err)
	}
	// blocked -> asking is illegal: refused, nothing published.
	_, errs, code := runCLI(t, as("app-001"), "ask", "--text", "late")
	if code != 3 || !strings.HasPrefix(errs, "bmesh: refused: ") {
		t.Fatalf("code %d err %q", code, errs)
	}
	if hist, _ := mesh.History(ctx, c, "bm.>", time.Time{}); len(hist) != 3 {
		t.Fatalf("refused event published: %v", hist)
	}
	out, _, code := runCLI(t, as("app-001"), "state")
	if code != 0 || !strings.HasPrefix(out, "blocked since ") || !strings.HasSuffix(out, ": t-blocked\n") {
		t.Fatalf("state: %d %q", code, out)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// specRepo makes a repo with a bare origin whose main holds spec 012 with
// the given status and merged, pushed.
func specRepo(t *testing.T, status, merged string) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	repo := filepath.Join(root, "repo")
	runGit(t, root, "init", "--bare", origin)
	runGit(t, root, "init", "-b", "main", repo)
	runGit(t, repo, "remote", "add", "origin", origin)
	dir := filepath.Join(repo, "docs", "proposals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	text := "---\nid: 12\nstatus: " + status + "\nmerged: " + merged + "\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "012-thing.md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "spec")
	runGit(t, repo, "push", "origin", "main")
	return repo
}

func TestDoneRefusesUnlandedSpec(t *testing.T) {
	c, ctx := setup(t)
	repo := specRepo(t, "building", "")
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: repo, Spec: "012"})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	_, errs, code := runCLI(t, as("app-012"), "done", "--commit", "abc123")
	if code != 3 || errs != "bmesh: not done: status is building\n" {
		t.Fatalf("code %d err %q", code, errs)
	}
	if hist, _ := mesh.History(ctx, c, "bm.app-012.done", time.Time{}); len(hist) != 0 {
		t.Fatalf("done published: %v", hist)
	}
	if got := stateOf(t, c, ctx, "app-012"); got != "working" {
		t.Fatalf("state %s", got)
	}
	if _, _, code := runCLI(t, as("app-012"), "done"); code != 2 {
		t.Fatalf("done without --commit: code %d", code)
	}
	if _, errs, code := runCLI(t, as("nobody"), "done", "--commit", "x"); code != 1 || errs != "bmesh: node nobody is not registered\n" {
		t.Fatalf("unregistered: %d %q", code, errs)
	}
}

func TestSendInboxRoundTrip(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "a"})
	putNode(t, c, ctx, mesh.Node{Path: "b"})
	out, errs, code := runCLI(t, as("a"), "send", "b", "hello there")
	if code != 0 || !strings.HasPrefix(out, "sent ") {
		t.Fatalf("send: %d %q %q", code, out, errs)
	}
	out, _, code = runCLI(t, as("b"), "inbox", "--peek")
	if code != 0 || !strings.HasSuffix(out, " a: hello there\n") {
		t.Fatalf("peek: %d %q", code, out)
	}
	out, _, code = runCLI(t, as("b"), "inbox")
	if code != 0 || !strings.HasSuffix(out, " a: hello there\n") || strings.Count(out, "\n") != 1 {
		t.Fatalf("inbox: %d %q", code, out)
	}
	out, _, code = runCLI(t, as("b"), "inbox")
	if code != 0 || out != "" {
		t.Fatalf("second inbox: %d %q", code, out)
	}
	// stdin text and the JSON form.
	var o, e bytes.Buffer
	t.Setenv("BMESH_NODE", "a")
	if code := run([]string{"send", "b", "-"}, strings.NewReader("from stdin\n"), &o, &e); code != 0 {
		t.Fatal(code, e.String())
	}
	out, _, code = runCLI(t, as("b"), "--json", "inbox")
	if code != 0 || !strings.Contains(out, `"text":"from stdin"`) || !strings.Contains(out, `"from":"a"`) {
		t.Fatalf("json inbox: %d %q", code, out)
	}
}

func TestGlobalFlagsAfterSubcommand(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "demo-000"})
	cfg := os.Getenv("BMESH_CONFIG")
	t.Setenv("BMESH_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	out, errs, code := runCLI(t, nil, "send", "demo-000", "hello", "--node", "operator", "--config", cfg)
	if code != 0 || !strings.HasPrefix(out, "sent ") {
		t.Fatalf("send: %d %q %q", code, out, errs)
	}
	out, errs, code = runCLI(t, nil, "inbox", "--node", "demo-000", "--config", cfg)
	if code != 0 || !strings.HasSuffix(out, " operator: hello\n") {
		t.Fatalf("inbox: %d %q %q", code, out, errs)
	}
	// Without the late --config the missing file's defaults apply.
	if _, errs, code := runCLI(t, nil, "inbox", "--node", "demo-000"); code == 0 || !strings.Contains(errs, "[mesh] nats is empty") {
		t.Fatalf("no --config: %d %q", code, errs)
	}
}

func TestIdentityFromWorktree(t *testing.T) {
	c, ctx := setup(t)
	wt := t.TempDir()
	runGit(t, wt, "init", "-b", "main")
	real, err := filepath.EvalSymlinks(wt)
	if err != nil {
		t.Fatal(err)
	}
	putNode(t, c, ctx, mesh.Node{Path: "app-007", Worktree: real})
	sub := filepath.Join(wt, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	out, errs, code := runCLI(t, nil, "whoami")
	if code != 0 || out != "app-007\n" {
		t.Fatalf("whoami: %d %q %q", code, out, errs)
	}
	out, _, code = runCLI(t, nil, "--node", "other", "whoami")
	if code != 0 || out != "other\n" {
		t.Fatalf("--node: %d %q", code, out)
	}
	// Re-registered through a symlink to the worktree, found from the real path.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	putNode(t, c, ctx, mesh.Node{Path: "app-007", Worktree: link})
	t.Chdir(real)
	out, errs, code = runCLI(t, nil, "whoami")
	if code != 0 || out != "app-007\n" {
		t.Fatalf("symlinked worktree: %d %q %q", code, out, errs)
	}
	t.Chdir(t.TempDir())
	_, errs, code = runCLI(t, nil, "whoami")
	if code != 2 || errs != "bmesh: no node for this directory; pass --node or set BMESH_NODE\n" {
		t.Fatalf("elsewhere: %d %q", code, errs)
	}
}

func TestWatchExitsWhenAllTerminal(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "a"})
	putNode(t, c, ctx, mesh.Node{Path: "b"})
	for _, w := range []struct{ p, s string }{{"a", "working"}, {"a", "done"}, {"b", "working"}, {"b", "blocked"}} {
		if _, err := mesh.WriteState(ctx, c, w.p, mesh.State{State: w.s, By: "node"}); err != nil {
			t.Fatal(err)
		}
	}
	out, errs, code := runCLI(t, nil, "watch", "--nodes", "a,b", "--timeout", "10s")
	if code != 0 || !strings.HasSuffix(out, "watch-done\n") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
}

func TestWatchFollowsUntilDoneAndTimesOut(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "a"})
	if _, errs, code := runCLI(t, as("a"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	_, errs, code := runCLI(t, nil, "watch", "--nodes", "a", "--timeout", "1s")
	if code != 5 || errs != "bmesh: watch timed out\n" {
		t.Fatalf("timeout: %d %q", code, errs)
	}
	var out, errb bytes.Buffer
	res := make(chan int, 1)
	go func() {
		res <- run([]string{"watch", "--nodes", "a", "--timeout", "20s"}, strings.NewReader(""), &out, &errb)
	}()
	time.Sleep(1500 * time.Millisecond)
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "a.x", Event: "progress", Text: "half"}); err != nil {
		t.Fatal(err)
	}
	if err := mesh.PutRunner(ctx, c, "a", mesh.RunnerSample{Running: true, Known: true}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // let the runner change print before the exit can win
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "a", Event: "done", Text: "fin"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mesh.WriteState(ctx, c, "a", mesh.State{State: "done", By: "node"}); err != nil {
		t.Fatal(err)
	}
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "a", Event: "progress", Text: "nudge"}); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-res:
		s := out.String()
		if code != 0 || !strings.Contains(s, " a.x progress half\n") || !strings.Contains(s, " a runner running\n") ||
			!strings.HasSuffix(s, "watch-done\n") {
			t.Fatalf("%d %q %q", code, s, errb.String())
		}
	case <-time.After(25 * time.Second):
		t.Fatal("watch did not exit")
	}
}

func TestStatusAndUnreachable(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "a"})
	putNode(t, c, ctx, mesh.Node{Path: "b", Role: "executor"})
	if _, errs, code := runCLI(t, as("a"), "started", "--text", "go"); code != 0 {
		t.Fatal(code, errs)
	}
	if err := mesh.PutRunner(ctx, c, "a", mesh.RunnerSample{Running: true, Known: true}); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := runCLI(t, as("a"), "send", "b", "hi"); code != 0 {
		t.Fatal(code, errs)
	}
	out, errs, code := runCLI(t, nil, "status")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 3 || strings.Join(strings.Fields(lines[0]), " ") != "PATH ROLE STATE SINCE RUNNER UNREAD" {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	fa, fb := strings.Fields(lines[1]), strings.Fields(lines[2])
	if fa[0] != "a" || fa[2] != "working" || fa[4] != "running" || fa[5] != "0" ||
		fb[0] != "b" || fb[1] != "executor" || fb[2] != "idle" || fb[4] != "?" || fb[5] != "1" {
		t.Fatalf("%q", out)
	}
	out, _, code = runCLI(t, nil, "status", "--json")
	if code != 0 || !strings.HasPrefix(out, "[") || !strings.Contains(out, `"unread":1`) {
		t.Fatalf("%d %q", code, out)
	}

	cfg := filepath.Join(t.TempDir(), "down.toml")
	if err := os.WriteFile(cfg, []byte("[mesh]\nnats = \"nats://127.0.0.1:1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errs, code = runCLI(t, map[string]string{"BMESH_CONFIG": cfg}, "status")
	if code != 1 || !strings.HasPrefix(errs, "bmesh: can't reach NATS at nats://127.0.0.1:1: ") {
		t.Fatalf("%d %q", code, errs)
	}
	if _, _, code := runCLI(t, nil, "bogus"); code != 2 {
		t.Fatalf("unknown command: %d", code)
	}
}
