package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	if err := mesh.Provision(ctx, c, 0); err != nil {
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

// writeRepos writes body as the repos.toml beside the test's config.
func writeRepos(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(os.Getenv("BMESH_CONFIG")), "repos.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
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
	out, _, code := runCLI(t, as("app-001"), "thread-status")
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
	if code != 0 || out != "inbox: empty\n" {
		t.Fatalf("second inbox: %d %q", code, out)
	}
	// --json prints nothing for an empty inbox.
	if out, _, code := runCLI(t, as("b"), "inbox", "--json"); code != 0 || out != "" {
		t.Fatalf("empty json inbox: %d %q", code, out)
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
	// Without the late --config the missing file means a box not set up:
	// refused outside a terminal.
	if _, errs, code := runCLI(t, nil, "inbox", "--node", "demo-000"); code != exitRefused || !strings.Contains(errs, "run `bmesh init`") {
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
	out, errs, code := runCLI(t, nil, "mesh-status")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 3 || strings.Join(strings.Fields(lines[0]), " ") != "PATH ROLE STATE SINCE RUNNER UNREAD" {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	fa, fb := strings.Fields(lines[1]), strings.Fields(lines[2])
	if fa[0] != "a" || fa[2] != "working" || fa[4] != "running" || fa[5] != "0" ||
		fb[0] != "b" || fb[1] != "executor" || fb[2] != "idle" || fb[4] != "?" || fb[5] != "1" {
		t.Fatalf("%q", out)
	}
	out, _, code = runCLI(t, nil, "mesh-status", "--json")
	if code != 0 || !strings.HasPrefix(out, "[") || !strings.Contains(out, `"unread":1`) {
		t.Fatalf("%d %q", code, out)
	}

	cfg := filepath.Join(t.TempDir(), "down.toml")
	if err := os.WriteFile(cfg, []byte("[mesh]\nnats = \"nats://127.0.0.1:1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errs, code = runCLI(t, map[string]string{"BMESH_CONFIG": cfg}, "mesh-status")
	if code != 1 || !strings.HasPrefix(errs, "bmesh: can't reach NATS at nats://127.0.0.1:1: ") {
		t.Fatalf("%d %q", code, errs)
	}
	if _, _, code := runCLI(t, nil, "bogus"); code != 2 {
		t.Fatalf("unknown command: %d", code)
	}
}

func TestExecutorAskGoesToParent(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012"})
	putNode(t, c, ctx, mesh.Node{Path: "app-012.alpha", Role: "executor", Parent: "app-012"})
	if _, errs, code := runCLI(t, as("app-012.alpha"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	if out, errs, code := runCLI(t, as("app-012.alpha"), "ask", "--text", "which db?"); code != 0 || out != "ask app-012.alpha\n" {
		t.Fatalf("ask: %d %q %q", code, out, errs)
	}
	lead, _ := mesh.ParsePath("app-012")
	msgs, err := mesh.Fetch(ctx, c, lead, true)
	if err != nil || len(msgs) != 1 || msgs[0].Kind != "message" || msgs[0].From != "app-012.alpha" ||
		msgs[0].Text != "ask from app-012.alpha: which db?" {
		t.Fatalf("lead inbox %+v %v", msgs, err)
	}
	if hist, _ := mesh.History(ctx, c, "bm.app-012.alpha.ask", time.Time{}); len(hist) != 1 {
		t.Fatalf("ask event: %v", hist)
	}
	if got := stateOf(t, c, ctx, "app-012.alpha"); got != "asking" {
		t.Fatalf("state %s", got)
	}
	// The same ask again (a retry) is one message; a different one is another.
	if _, errs, code := runCLI(t, as("app-012.alpha"), "ask", "--text", "which db?"); code != 0 {
		t.Fatal(code, errs)
	}
	if _, errs, code := runCLI(t, as("app-012.alpha"), "ask", "--text", "which port?"); code != 0 {
		t.Fatal(code, errs)
	}
	if n, err := mesh.Unread(ctx, c, lead); err != nil || n != 1 {
		t.Fatalf("lead unread after retry and a new ask: %d %v", n, err)
	}
	if msgs, err := mesh.Fetch(ctx, c, lead, true); err != nil || len(msgs) != 1 || msgs[0].Text != "ask from app-012.alpha: which port?" {
		t.Fatalf("lead inbox %+v %v", msgs, err)
	}
	// A lead's ask stays its own: nothing is sent anywhere.
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	if _, errs, code := runCLI(t, as("app-012"), "ask", "--text", "ship it?"); code != 0 {
		t.Fatal(code, errs)
	}
	if n, err := mesh.Unread(ctx, c, lead); err != nil || n != 0 {
		t.Fatalf("lead inbox after its own ask: %d %v", n, err)
	}
	if hist, _ := mesh.History(ctx, c, "bm.inbox.>", time.Time{}); len(hist) != 2 {
		t.Fatalf("inbox messages: %d", len(hist))
	}
}

func TestInboxPrintsReplies(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012"})
	me, _ := mesh.ParsePath("app-012")
	reply := mesh.Reply{Job: "J1", Kind: "review", Tree: "t", OK: false, Output: "### high: breaks\nVERDICT: fail", By: "routine.review.x-1"}
	if err := mesh.SendReply(ctx, c, mesh.RoutinePath("review"), me, reply); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runCLI(t, as("app-012"), "--json", "inbox", "--peek")
	if code != 0 || !strings.Contains(out, `"output":"### high: breaks\nVERDICT: fail"`) {
		t.Fatalf("json inbox: %d %q %q", code, out, errs)
	}
	out, errs, code = runCLI(t, as("app-012"), "inbox")
	if code != 0 || !strings.HasSuffix(out, " routine.review: reply to job J1: review fail, 2 lines; bmesh routine wait J1 prints it\n") ||
		strings.Count(out, "\n") != 1 {
		t.Fatalf("inbox: %d %q %q", code, out, errs)
	}
}

func TestInboxEmptyReturnsAtOnce(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012"})
	start := time.Now()
	out, errs, code := runCLI(t, as("app-012"), "inbox")
	if took := time.Since(start); code != 0 || out != "inbox: empty\n" || took > 500*time.Millisecond {
		t.Fatalf("empty inbox: %d %q %q in %v", code, out, errs, took)
	}
}

func TestExecutorDoneAndBlockedReportToParent(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012"})
	// No worktree: the report names the recorded branch and the commit as given.
	putNode(t, c, ctx, mesh.Node{Path: "app-012.alpha", Role: "executor", Parent: "app-012", Branch: "t3code/x-alpha"})
	lead, _ := mesh.ParsePath("app-012")
	// Each event prints one line: `<event> <path>`, done its commit too.
	run := func(args ...string) {
		t.Helper()
		want := args[0] + " app-012.alpha\n"
		if args[0] == "done" {
			want = "done app-012.alpha " + args[2][:8] + "\n"
		}
		if out, errs, code := runCLI(t, as("app-012.alpha"), args...); code != 0 || out != want {
			t.Fatalf("%v: %d %q %q", args, code, out, errs)
		}
	}
	run("started")
	run("blocked", "--text", "no db")
	msgs, err := mesh.Fetch(ctx, c, lead, true)
	if err != nil || len(msgs) != 1 || msgs[0].Kind != "report" || msgs[0].From != "app-012.alpha" ||
		msgs[0].Text != "blocked from app-012.alpha: no db" || !strings.HasPrefix(msgs[0].ID, "blocked-app-012.alpha-") ||
		msgs[0].Report == nil || *msgs[0].Report != (mesh.Report{Event: "blocked", From: "app-012.alpha"}) {
		t.Fatalf("lead inbox after blocked %+v %v", msgs, err)
	}
	// The same block again (a retry) is no second message.
	run("started")
	run("blocked", "--text", "no db")
	if n, err := mesh.Unread(ctx, c, lead); err != nil || n != 0 {
		t.Fatalf("lead unread after a repeated block: %d %v", n, err)
	}
	run("started")
	run("progress", "--text", "half way")
	run("done", "--commit", "1a2b3c4d5e6f", "--text", "parser and its tests")
	out, errs, code := runCLI(t, as("app-012"), "inbox", "--json")
	var got mesh.Message
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil {
		t.Fatalf("lead inbox --json: %d %q %q", code, out, errs)
	}
	if got.ID != "done-app-012.alpha-1a2b3c4d5e6f" || got.Kind != "report" ||
		got.Text != "done from app-012.alpha: branch t3code/x-alpha at 1a2b3c4d: parser and its tests" || got.Report == nil ||
		*got.Report != (mesh.Report{Event: "done", From: "app-012.alpha", Branch: "t3code/x-alpha", Commit: "1a2b3c4d5e6f"}) {
		t.Fatalf("done report %q", out)
	}
	if !strings.Contains(out, `"report":{"event":"done","from":"app-012.alpha","branch":"t3code/x-alpha","commit":"1a2b3c4d5e6f"}`) {
		t.Fatalf("report field %q", out)
	}
	// A retry of the same done is one message.
	if err := mesh.ResetState(ctx, c, "app-012.alpha"); err != nil {
		t.Fatal(err)
	}
	run("started")
	run("done", "--commit", "1a2b3c4d5e6f", "--text", "parser and its tests")
	if n, err := mesh.Unread(ctx, c, lead); err != nil || n != 0 {
		t.Fatalf("lead unread after a repeated done: %d %v", n, err)
	}
	// One block and one done; progress is an event only.
	if hist, err := mesh.History(ctx, c, "bm.inbox.>", time.Time{}); err != nil || len(hist) != 2 {
		t.Fatalf("inbox messages: %d %v", len(hist), err)
	}
}

func TestExecutorDoneNeedsCommitSkipsSpec(t *testing.T) {
	c, ctx := setup(t)
	// No RepoPath and no spec: a lead's done would fail its spec lookup.
	putNode(t, c, ctx, mesh.Node{Path: "app-012.alpha", Role: "executor", Parent: "app-012"})
	if _, errs, code := runCLI(t, as("app-012.alpha"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	if _, errs, code := runCLI(t, as("app-012.alpha"), "done", "--text", "built"); code != 2 || !strings.Contains(errs, "--commit") {
		t.Fatalf("without --commit: %d %q", code, errs)
	}
	if got := stateOf(t, c, ctx, "app-012.alpha"); got != "working" {
		t.Fatalf("state after refusal %s", got)
	}
	out, errs, code := runCLI(t, as("app-012.alpha"), "done", "--commit", "abc1234", "--text", "built")
	if code != 0 || out != "done app-012.alpha abc1234\n" || errs != "" {
		t.Fatalf("done: %d %q %q", code, out, errs)
	}
	if got := stateOf(t, c, ctx, "app-012.alpha"); got != "done" {
		t.Fatalf("state %s", got)
	}
	hist, err := mesh.History(ctx, c, "bm.app-012.alpha.done", time.Time{})
	if err != nil || len(hist) != 1 || !strings.Contains(string(hist[0].Data), `"commit":"abc1234"`) {
		t.Fatalf("done event %v %v", hist, err)
	}
}
