package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// kindRepo is gitRepo with kind files committed under .bmesh/routines.
func kindRepo(t *testing.T, kinds map[string]string) string {
	t.Helper()
	repo := gitRepo(t)
	dir := filepath.Join(repo, ".bmesh", "routines")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range kinds {
		if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "kinds")
	return repo
}

func TestQueueAnswersFromRecordEvenWhenFailed(t *testing.T) {
	c, ctx := setup(t)
	kindRepo(t, map[string]string{"verify": "deterministic = true\ncommand = \"true\"\n"})
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "verify", Tree: tree, OK: false, Output: "broke\n", By: "routine.verify.c-1"}); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runCLI(t, as("app-001"), "routine", "queue", "verify")
	if code != 0 || !strings.HasPrefix(out, "answered from result verify."+tree) {
		t.Fatalf("queue: %d %q %q", code, out, errs)
	}
	me, _ := mesh.ParsePath("app-001")
	msgs, err := mesh.Fetch(ctx, c, me, true)
	if err != nil || len(msgs) != 1 || msgs[0].Kind != "reply" || msgs[0].Reply == nil {
		t.Fatalf("inbox %+v %v", msgs, err)
	}
	if r := msgs[0].Reply; r.OK || r.Output != "broke\n" || r.ResultKey != "verify."+tree || r.By != "routine.verify.c-1" ||
		msgs[0].From != "routine.verify" {
		t.Fatalf("reply %+v from %s", r, msgs[0].From)
	}
	if n, err := mesh.Depth(ctx, c, "verify"); err != nil || n != 0 {
		t.Fatalf("depth %d %v", n, err)
	}
	out, errs, code = runCLI(t, as("app-001"), "routine", "queue", "verify", "--rerun")
	if code != 0 || !strings.HasPrefix(out, "queued ") {
		t.Fatalf("rerun: %d %q %q", code, out, errs)
	}
	if n, err := mesh.Depth(ctx, c, "verify"); err != nil || n != 1 {
		t.Fatalf("depth after rerun %d %v", n, err)
	}
	_, errs, code = runCLI(t, as("app-001"), "routine", "queue", "lint")
	if code != 3 || !strings.HasPrefix(errs, "bmesh: no kind lint in ") {
		t.Fatalf("unknown kind: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, as("app-001"), "routine", "queue", "verify", "--model", "m1"); code != 2 {
		t.Fatalf("model on a command kind: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "routine", "queue", "verify"); code != 2 {
		t.Fatalf("no identity: %d %q", code, errs)
	}
}

func TestQueueDedupeIsModelAware(t *testing.T) {
	c, ctx := setup(t)
	kindRepo(t, map[string]string{
		"review": "deterministic = true\nmodels = [\"m1\", \"m2\"]\nprompt = \"check\"\n",
	})
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "review", Tree: tree, OK: false, Output: "bad\n", By: "x", Model: "m1"}); err != nil {
		t.Fatal(err)
	}
	// No --model is the first model, m1, whose record answers.
	out, errs, code := runCLI(t, as("app-001"), "routine", "queue", "review")
	if code != 0 || !strings.HasPrefix(out, "answered from result review."+tree+".m1 ") {
		t.Fatalf("queue: %d %q %q", code, out, errs)
	}
	if n, err := mesh.Depth(ctx, c, "review"); err != nil || n != 0 {
		t.Fatalf("depth %d %v", n, err)
	}
	// m2 has no record, so it queues.
	out, errs, code = runCLI(t, as("app-001"), "routine", "queue", "review", "--model", "m2")
	if code != 0 || !strings.HasPrefix(out, "queued ") {
		t.Fatalf("queue m2: %d %q %q", code, out, errs)
	}
	if n, err := mesh.Depth(ctx, c, "review"); err != nil || n != 1 {
		t.Fatalf("depth after m2 %d %v", n, err)
	}
}

func TestWaitReturnsReplyExitByOK(t *testing.T) {
	c, ctx := setup(t)
	me, _ := mesh.ParsePath("app-001")
	if _, err := mesh.Send(ctx, c, mesh.Message{From: "x", To: "app-001", Text: "unrelated"}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []mesh.Reply{
		{Job: "j-ok", Kind: "verify", OK: true, Output: "all good\n"},
		{Job: "j-fail", Kind: "verify", OK: false, Output: "broke\n"},
	} {
		if err := mesh.SendReply(ctx, c, mesh.RoutinePath("verify"), me, r); err != nil {
			t.Fatal(err)
		}
	}
	out, errs, code := runCLI(t, as("app-001"), "routine", "wait", "j-fail", "--timeout", "10s")
	if code != 1 || out != "broke\n" {
		t.Fatalf("fail: %d %q %q", code, out, errs)
	}
	out, errs, code = runCLI(t, as("app-001"), "routine", "wait", "j-ok", "--timeout", "10s")
	if code != 0 || out != "all good\n" {
		t.Fatalf("ok: %d %q %q", code, out, errs)
	}
	_, errs, code = runCLI(t, as("app-001"), "routine", "wait", "j-none", "--timeout", "2s")
	if code != 5 || errs != "bmesh: timed out waiting for j-none\n" {
		t.Fatalf("timeout: %d %q", code, errs)
	}
	if n, err := mesh.Unread(ctx, c, me); err != nil || n != 1 {
		t.Fatalf("unrelated message consumed: %d %v", n, err)
	}
}

func TestRoutineListStatusAndGo(t *testing.T) {
	c, ctx := setup(t)
	kindRepo(t, map[string]string{
		"verify": "deterministic = true\ncommand = \"make test\"\n",
		"review": "models = [\"m1\", \"m2\"]\n",
	})
	putNode(t, c, ctx, mesh.Node{Path: "routine.review.m1-1", Role: "routine", Model: "m1"})
	out, errs, code := runCLI(t, as("app-001"), "routine", "go", "review")
	if code != 0 || !strings.HasPrefix(out, "sent ") {
		t.Fatalf("go: %d %q %q", code, out, errs)
	}
	out, errs, code = runCLI(t, nil, "routine", "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 3 || strings.Join(strings.Fields(lines[0]), " ") != "KIND MODE DETERMINISTIC RUN INSTANCES DEPTH" {
		t.Fatalf("list: %d %q %q", code, out, errs)
	}
	if got := strings.Join(strings.Fields(lines[1]), " "); got != "review pooled false m1,m2 1 1" {
		t.Fatalf("review row %q", got)
	}
	if got := strings.Join(strings.Fields(lines[2]), " "); got != "verify pooled true make test 0 0" {
		t.Fatalf("verify row %q", got)
	}
	out, _, code = runCLI(t, nil, "routine", "list", "--json")
	if code != 0 || strings.Count(out, "\n") != 2 || !strings.Contains(out, `"kind":"review"`) {
		t.Fatalf("list json: %d %q", code, out)
	}
	out, errs, code = runCLI(t, as("app-001"), "routine", "queue", "review")
	id, ok := strings.CutPrefix(strings.TrimSpace(out), "queued ")
	if code != 0 || !ok {
		t.Fatalf("queue: %d %q %q", code, out, errs)
	}
	out, errs, code = runCLI(t, nil, "routine", "status", "review")
	if code != 0 || !strings.Contains(out, "routine.review.m1-1") || !strings.Contains(out, " m1") {
		t.Fatalf("status: %d %q %q", code, out, errs)
	}
	// Then the kind's unanswered jobs: waiting, as no daemon holds it.
	_, jobs, _ := strings.Cut(out, "\n\n")
	jl := strings.Split(strings.TrimSpace(jobs), "\n")
	if len(jl) != 2 || strings.Join(strings.Fields(jl[0]), " ") != "JOB KIND STATE HOLDER FROM" ||
		strings.Join(strings.Fields(jl[1]), " ") != id+" review waiting - app-001" {
		t.Fatalf("status jobs: %q", out)
	}
	out, _, code = runCLI(t, nil, "routine", "status", "review", "--json")
	if code != 0 || !strings.Contains(out, `"jobs":[{"id":"`+id+`","from":"app-001","state":"waiting"`) {
		t.Fatalf("status json: %d %q", code, out)
	}
	// Without a kind: the kinds in the mesh, found from node paths and consumers.
	t.Chdir(t.TempDir())
	out, errs, code = runCLI(t, nil, "routine", "status")
	if code != 0 || !strings.Contains(out, "routine.review.m1-1") || strings.Contains(out, "verify") {
		t.Fatalf("status all: %d %q %q", code, out, errs)
	}
}

// queuedRepo queues verify as app-001 from the cwd and returns the job's repo.
func queuedRepo(t *testing.T, c *mesh.Client, ctx context.Context) string {
	t.Helper()
	out, errs, code := runCLI(t, as("app-001"), "routine", "queue", "verify")
	if code != 0 || !strings.HasPrefix(out, "queued ") {
		t.Fatalf("queue: %d %q %q", code, out, errs)
	}
	msgs, err := mesh.Fetch(ctx, c, mesh.RoutinePath("verify"), true)
	if err != nil || len(msgs) != 1 || msgs[0].Job == nil {
		t.Fatalf("kind inbox %+v %v", msgs, err)
	}
	return msgs[0].Job.Repo
}

// R16: a job queued from a linked worktree (where leads live) names the
// [repos] entry of its main repo, not the worktree's directory.
func TestQueueFromLinkedWorktreeNamesConfiguredRepo(t *testing.T) {
	c, ctx := setup(t)
	repo := kindRepo(t, map[string]string{"verify": "command = \"true\"\n"})
	wt := filepath.Join(t.TempDir(), "thread-1")
	runGit(t, repo, "worktree", "add", "--detach", wt)
	t.Chdir(wt)
	// No [repos] entry: the main repo's base name.
	if got := queuedRepo(t, c, ctx); got != filepath.Base(repo) {
		t.Fatalf("no [repos]: repo %q, want %q", got, filepath.Base(repo))
	}
	writeRepos(t, "[repos.other]\npath = \""+t.TempDir()+"\"\n[repos.demo]\npath = \""+repo+"\"\n")
	if got := queuedRepo(t, c, ctx); got != "demo" {
		t.Fatalf("[repos]: repo %q, want demo", got)
	}
	// A registered node naming its repo is believed.
	putNode(t, c, ctx, mesh.Node{Path: "app-001", Repo: "named"})
	if got := queuedRepo(t, c, ctx); got != "named" {
		t.Fatalf("node: repo %q, want named", got)
	}
}

// A review is queued once per branch: a review result for a commit of
// this branch, not on main, refuses a second one without --again; a
// result for a landed commit in the history does not count.
func TestQueueReviewOncePerBranch(t *testing.T) {
	c, ctx := setup(t)
	repo := kindRepo(t, map[string]string{"review": "models = [\"m1\"]\nprompt = \"check\"\n"})
	landed := gitOut(t, "rev-parse", "HEAD")
	runGit(t, repo, "checkout", "-q", "-b", "work")
	os.WriteFile(filepath.Join(repo, "b.txt"), []byte("b\n"), 0o644)
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-qm", "b")
	first := gitOut(t, "rev-parse", "HEAD")
	// A review of main's commit (another lead's, landed) does not count.
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "review", Tree: "t0", Commit: landed, OK: true, By: "routine.review.m1-1", Model: "m1", At: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runCLI(t, as("app-001"), "routine", "queue", "review")
	if code != 0 || !strings.HasPrefix(out, "queued ") {
		t.Fatalf("first: %d %q %q", code, out, errs)
	}
	// The review's result for this branch's commit: a second queue is refused, naming it.
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "review", Tree: "t1", Commit: first, OK: false, By: "routine.review.m1-1", Model: "m1", At: "2026-01-02T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, "b.txt"), []byte("fixed\n"), 0o644)
	runGit(t, repo, "commit", "-qam", "fix")
	out, errs, code = runCLI(t, as("app-001"), "routine", "queue", "review")
	if code != 3 || out != "" || !strings.Contains(errs, "this branch already has a review: review.t1.m1 (fail, commit "+first[:8]) ||
		!strings.Contains(errs, "with --again") {
		t.Fatalf("second: %d %q %q", code, out, errs)
	}
	if n, err := mesh.Depth(ctx, c, "review"); err != nil || n != 1 {
		t.Fatalf("depth %d %v", n, err)
	}
	out, errs, code = runCLI(t, as("app-001"), "routine", "queue", "review", "--again")
	if code != 0 || !strings.HasPrefix(out, "queued ") {
		t.Fatalf("--again: %d %q %q", code, out, errs)
	}
	// Another branch from main is unreviewed.
	runGit(t, repo, "checkout", "-q", "-b", "other", "main")
	os.WriteFile(filepath.Join(repo, "c.txt"), []byte("c\n"), 0o644)
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-qm", "c")
	if out, errs, code := runCLI(t, as("app-001"), "routine", "queue", "review"); code != 0 || !strings.HasPrefix(out, "queued ") {
		t.Fatalf("other branch: %d %q %q", code, out, errs)
	}
}

func TestWaitFindsReplyInboxAlreadyRead(t *testing.T) {
	c, ctx := setup(t)
	me, _ := mesh.ParsePath("app-001")
	if err := mesh.SendReply(ctx, c, mesh.RoutinePath("verify"), me, mesh.Reply{Job: "j1", Kind: "verify", Output: "broke\n"}); err != nil {
		t.Fatal(err)
	}
	if out, errs, code := runCLI(t, as("app-001"), "inbox"); code != 0 || !strings.Contains(out, "reply to job j1: verify fail, 1 lines;") {
		t.Fatalf("inbox: %d %q %q", code, out, errs)
	}
	start := time.Now()
	out, errs, code := runCLI(t, as("app-001"), "routine", "wait", "j1", "--timeout", "10s")
	if took := time.Since(start); code != 1 || out != "broke\n" || took > time.Second {
		t.Fatalf("wait: %d %q %q in %v", code, out, errs, took)
	}
}

func TestWaitAcksPendingReply(t *testing.T) {
	c, ctx := setup(t)
	me, _ := mesh.ParsePath("app-001")
	if err := mesh.SendReply(ctx, c, mesh.RoutinePath("verify"), me, mesh.Reply{Job: "j1", Kind: "verify", OK: true, Output: "fine\n"}); err != nil {
		t.Fatal(err)
	}
	if n, err := mesh.Unread(ctx, c, me); err != nil || n != 1 {
		t.Fatalf("unread before: %d %v", n, err)
	}
	if out, errs, code := runCLI(t, as("app-001"), "routine", "wait", "j1", "--timeout", "10s"); code != 0 || out != "fine\n" {
		t.Fatalf("wait: %d %q %q", code, out, errs)
	}
	if n, err := mesh.Unread(ctx, c, me); err != nil || n != 0 {
		t.Fatalf("unread after: %d %v", n, err)
	}
}

func TestWaitFollowsALateReply(t *testing.T) {
	c, ctx := setup(t)
	me, _ := mesh.ParsePath("app-001")
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = mesh.SendReply(ctx, c, mesh.RoutinePath("verify"), me, mesh.Reply{Job: "j1", Kind: "verify", OK: true, Output: "late\n"})
	}()
	if out, errs, code := runCLI(t, as("app-001"), "routine", "wait", "j1", "--timeout", "10s"); code != 0 || out != "late\n" {
		t.Fatalf("wait: %d %q %q", code, out, errs)
	}
}

func TestWaitTimeoutFromKind(t *testing.T) {
	c, ctx := setup(t)
	kindRepo(t, map[string]string{"slow": "command = \"true\"\ntimeout = \"5m\"\n"})
	out, errs, code := runCLI(t, as("app-001"), "routine", "queue", "slow")
	id, ok := strings.CutPrefix(strings.TrimSpace(out), "queued ")
	if code != 0 || !ok {
		t.Fatalf("queue: %d %q %q", code, out, errs)
	}
	if d := waitTimeout(ctx, c, id); d != 26*time.Minute {
		t.Fatalf("known kind: %v", d)
	}
	if d := waitTimeout(ctx, c, "no-such-job"); d != 20*time.Minute {
		t.Fatalf("unknown job: %v", d)
	}
	// Without --timeout the CLI looks the kind up before it has waited on anything.
	me, _ := mesh.ParsePath("app-001")
	if err := mesh.SendReply(ctx, c, mesh.RoutinePath("slow"), me, mesh.Reply{Job: id, Kind: "slow", OK: true, Output: "done\n"}); err != nil {
		t.Fatal(err)
	}
	if out, errs, code := runCLI(t, as("app-001"), "routine", "wait", id); code != 0 || out != "done\n" {
		t.Fatalf("wait: %d %q %q", code, out, errs)
	}
}
