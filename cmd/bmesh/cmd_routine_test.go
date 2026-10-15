package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	out, errs, code = runCLI(t, nil, "routine", "status", "review")
	if code != 0 || !strings.Contains(out, "routine.review.m1-1") || !strings.Contains(out, " m1") {
		t.Fatalf("status: %d %q %q", code, out, errs)
	}
	// Without a kind: the kinds in the mesh, found from node paths and consumers.
	t.Chdir(t.TempDir())
	out, errs, code = runCLI(t, nil, "routine", "status")
	if code != 0 || !strings.Contains(out, "routine.review.m1-1") || strings.Contains(out, "verify") {
		t.Fatalf("status all: %d %q %q", code, out, errs)
	}
}
