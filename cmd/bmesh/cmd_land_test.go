package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// landRepo is a bare origin, its main checkout on main and a lead's worktree
// on branch lead with one commit of its own; the cwd is the worktree.
type landRepo struct {
	root, origin, repo, wt string
}

// newLandRepo makes the repo with spec 012 building, an inline verify kind
// running $HOOK (a script outside the repo, which a test may fill), node
// app-012 for the worktree, and the lead's commit (b.txt).
func newLandRepo(t *testing.T) landRepo {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
	} {
		t.Setenv(k, v)
	}
	orig := landPoll
	landPoll = 20 * time.Millisecond
	t.Cleanup(func() { landPoll = orig })
	root := t.TempDir()
	r := landRepo{root: root, origin: filepath.Join(root, "origin.git"), repo: filepath.Join(root, "repo"), wt: filepath.Join(root, "wt")}
	hook := filepath.Join(root, "hook.sh")
	writeFile(t, hook, "true\n")
	t.Setenv("HOOK", hook)
	meshtest.Git(t, root, "init", "--bare", r.origin)
	meshtest.Git(t, root, "init", "-b", "main", r.repo)
	meshtest.Git(t, r.repo, "remote", "add", "origin", r.origin)
	writeFile(t, filepath.Join(r.repo, "a.txt"), "one\ntwo\nthree\n")
	writeFile(t, filepath.Join(r.repo, ".bmesh", "routines", "verify.toml"),
		"mode = \"inline\"\ndeterministic = true\ncommand = \"sh \\\"$HOOK\\\"\"\n")
	writeFile(t, filepath.Join(r.repo, "docs", "proposals", "012-thing.md"), "---\nid: 12\nstatus: building\nbranch: lead\n---\nbody\n")
	meshtest.Git(t, r.repo, "add", "-A")
	meshtest.Git(t, r.repo, "commit", "-m", "base")
	meshtest.Git(t, r.repo, "push", "origin", "main")
	meshtest.Git(t, r.repo, "worktree", "add", "-b", "lead", r.wt, "main")
	writeFile(t, filepath.Join(r.wt, "b.txt"), "b\n")
	meshtest.Git(t, r.wt, "add", "-A")
	meshtest.Git(t, r.wt, "commit", "-m", "the work")
	t.Chdir(r.wt)
	return r
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pushOther lands a commit writing file on origin's main from a clone of
// its own, as another lead would.
func (r landRepo) pushOther(t *testing.T, file, body string) string {
	t.Helper()
	other := filepath.Join(r.root, "other")
	if _, err := os.Stat(other); err != nil {
		meshtest.Git(t, r.root, "clone", "-q", "-b", "main", r.origin, other)
	}
	meshtest.Git(t, other, "pull", "-q", "--ff-only", "origin", "main")
	writeFile(t, filepath.Join(other, file), body)
	meshtest.Git(t, other, "add", "-A")
	meshtest.Git(t, other, "commit", "-m", "other "+file)
	meshtest.Git(t, other, "push", "-q", "origin", "HEAD:main")
	return gitOut(t, "-C", other, "rev-parse", "HEAD")
}

func (r landRepo) originMain(t *testing.T) string {
	t.Helper()
	return gitOut(t, "-C", r.origin, "rev-parse", "main")
}

// specOnOrigin is spec 012's frontmatter on origin's main.
func (r landRepo) specOnOrigin(t *testing.T) string {
	t.Helper()
	return gitOut(t, "-C", r.origin, "show", "main:docs/proposals/012-thing.md")
}

func TestLandCleanThenDone(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	work := gitOut(t, "rev-parse", "HEAD")
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")
	base := r.originMain(t)

	out, errs, code := runCLI(t, as("app-012"), "land")
	rec := gitOut(t, "rev-parse", "HEAD")
	want := "lease land.repo\n" +
		"fetch origin/main " + base[:8] + "\n" +
		"rebase: on " + base[:8] + " already\n" +
		"verify ok t:" + tree[:8] + " (recorded)\n" +
		"verified: verify ok t:" + tree[:8] + "\n" +
		"spec 012 done: merged " + work[:8] + ", branch lead\n" +
		"commit " + rec[:8] + " 012: done at " + work[:8] + "\n" +
		"push origin main " + rec[:8] + "\n" +
		"main " + rec[:8] + "\n" +
		"released land.repo\n" +
		"landed " + rec[:8] + "\n"
	if code != 0 || out != want || errs != "" {
		t.Fatalf("land: %d\n%s\nwant\n%s\nerr %q", code, out, want, errs)
	}
	if got := gitOut(t, "rev-parse", "HEAD~1"); got != work {
		t.Fatalf("record commit's parent %s, want %s", got, work)
	}
	if r.originMain(t) != rec || gitOut(t, "-C", r.repo, "rev-parse", "main") != rec {
		t.Fatalf("main not at %s", rec)
	}
	front := r.specOnOrigin(t)
	for _, w := range []string{"status: done\n", "branch: lead\n", "merged: " + work + "\n", "verified: verify ok t:" + tree[:8] + "\n"} {
		if !strings.Contains(front, w) {
			t.Fatalf("spec on origin lacks %q:\n%s", w, front)
		}
	}
	assertNoLeases(t, c, ctx)
	// The landed spec satisfies done.
	if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", rec); code != 0 || out != "done app-012 "+rec[:8]+"\n" {
		t.Fatalf("done: %d %q %q", code, out, errs)
	}
	// Landed, it refuses a second land: the spec no longer reads building.
	if _, errs, code := runCLI(t, as("app-012"), "land"); code != 3 || !strings.Contains(errs, "refused: nothing to land") {
		t.Fatalf("second land: %d %q", code, errs)
	}
}

// Main moves while verify runs (the hook pushes once): the push is refused,
// the record commit is taken back and land starts again from the fetch.
func TestLandRetriesOnceWhenMainMoved(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	other := filepath.Join(r.root, "other")
	meshtest.Git(t, r.root, "clone", "-q", "-b", "main", r.origin, other)
	mark := filepath.Join(r.root, "moved")
	writeFile(t, os.Getenv("HOOK"), "test -e '"+mark+"' && exit 0\ntouch '"+mark+"'\n"+
		"cd '"+other+"' && echo c > c.txt && git add c.txt && git commit -qm 'other c' && git push -q origin HEAD:main\n")
	work := gitOut(t, "rev-parse", "HEAD")

	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 0 || strings.Count(out, "main moved; again from the fetch\n") != 1 || !strings.HasSuffix(out, "landed "+gitOut(t, "rev-parse", "HEAD")[:8]+"\n") {
		t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
	}
	moved := gitOut(t, "-C", other, "rev-parse", "HEAD")
	merged := gitOut(t, "rev-parse", "HEAD~1")
	if gitOut(t, "rev-parse", "HEAD~2") != moved || merged == work {
		t.Fatalf("not rebased onto the moved main: HEAD~2 %s, moved %s", gitOut(t, "rev-parse", "HEAD~2"), moved)
	}
	if r.originMain(t) != gitOut(t, "rev-parse", "HEAD") {
		t.Fatal("origin main is not the record commit")
	}
	if front := r.specOnOrigin(t); !strings.Contains(front, "merged: "+merged+"\n") ||
		!strings.Contains(front, "verified: verify ok t:"+gitOut(t, "rev-parse", "HEAD~1^{tree}")[:8]+"\n") {
		t.Fatalf("spec on origin:\n%s", front)
	}
	if n := gitOut(t, "rev-list", "--count", "--grep", "012: done", "HEAD"); n != "1" {
		t.Fatalf("%s record commits", n)
	}
	assertNoLeases(t, c, ctx)
}

// Main moving on every try is refused after the one retry, with nothing
// pushed and the branch as the rebase left it.
func TestLandRefusesWhenMainKeepsMoving(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	other := filepath.Join(r.root, "other")
	meshtest.Git(t, r.root, "clone", "-q", "-b", "main", r.origin, other)
	writeFile(t, os.Getenv("HOOK"), "cd '"+other+"' && date +%s%N >> c.txt && git add c.txt && git commit -qm 'other c' && git push -q origin HEAD:main\n")

	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 3 || !strings.Contains(errs, "main moved again before the push") || strings.Count(out, "main moved; again") != 1 {
		t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
	}
	if r.originMain(t) != gitOut(t, "-C", other, "rev-parse", "HEAD") {
		t.Fatal("land pushed")
	}
	if strings.Contains(gitOut(t, "log", "--format=%s", "-1"), "done") || gitOut(t, "status", "--porcelain") != "" {
		t.Fatal("record commit or changes left on the branch")
	}
	if b, _ := os.ReadFile(filepath.Join(r.wt, "docs", "proposals", "012-thing.md")); !strings.Contains(string(b), "status: building") {
		t.Fatalf("spec in the worktree:\n%s", b)
	}
	assertNoLeases(t, c, ctx)
}

// Another lead holds the lease: land waits, naming it, and lands once it
// is released.
func TestLandWaitsForHeldLease(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	key, err := mesh.AcquireLease(ctx, c, "land.repo", "app-011", time.Minute, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	base := r.originMain(t)
	t.Setenv("BMESH_NODE", "app-012")
	var out, errb bytes.Buffer
	res := make(chan int, 1)
	go func() { res <- run([]string{"land"}, strings.NewReader(""), &out, &errb) }()
	time.Sleep(500 * time.Millisecond)
	select {
	case code := <-res:
		t.Fatalf("land finished under a held lease: %d %q %q", code, out.String(), errb.String())
	default:
	}
	if r.originMain(t) != base {
		t.Fatal("land pushed under a held lease")
	}
	if err := mesh.ReleaseLease(ctx, c, key, "app-011"); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-res:
		if code != 0 || !strings.HasPrefix(out.String(), "waiting for land.repo (held by app-011)\nlease land.repo\n") ||
			!strings.Contains(out.String(), "landed ") {
			t.Fatalf("land: %d\n%s\nerr %q", code, out.String(), errb.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("land never took the released lease")
	}
	if r.originMain(t) == base {
		t.Fatal("nothing pushed")
	}
	// A lease that stays held is given up on after --wait.
	if _, err := mesh.AcquireLease(ctx, c, "land.repo", "app-011", time.Minute, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := runCLI(t, as("app-012"), "land", "--spec", "012", "--wait", "100ms"); code != exitTimeout ||
		errs != "bmesh: timed out waiting for land.repo, held by app-011\n" {
		t.Fatalf("held: %d %q", code, errs)
	}
}

// A rebase that conflicts stops: aborted, nothing pushed, the branch as it
// was.
func TestLandStopsOnConflict(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	writeFile(t, filepath.Join(r.wt, "a.txt"), "ONE\ntwo\nthree\n")
	meshtest.Git(t, r.wt, "commit", "-qam", "lead a")
	work := gitOut(t, "rev-parse", "HEAD")
	moved := r.pushOther(t, "a.txt", "uno\ntwo\nthree\n")

	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 3 || errs != "bmesh: refused: rebase onto origin/main conflicts in a.txt; it is aborted and nothing is pushed. "+
		"Rebase yourself, resolve it, re-validate, then run bmesh land again\n" {
		t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
	}
	if r.originMain(t) != moved || gitOut(t, "rev-parse", "HEAD") != work || gitOut(t, "status", "--porcelain") != "" {
		t.Fatal("conflict changed origin or the branch")
	}
	assertNoLeases(t, c, ctx)
}

// Local main ahead of origin (someone's unpushed work) is refused before
// the rebase.
func TestLandRefusesMainAheadOfOrigin(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	writeFile(t, filepath.Join(r.repo, "d.txt"), "d\n")
	meshtest.Git(t, r.repo, "add", "d.txt")
	meshtest.Git(t, r.repo, "commit", "-m", "coordinator's")
	work := gitOut(t, "rev-parse", "HEAD")
	_, errs, code := runCLI(t, as("app-012"), "land")
	if code != 3 || !strings.Contains(errs, "has 1 commits origin/main doesn't") {
		t.Fatalf("land: %d %q", code, errs)
	}
	if gitOut(t, "rev-parse", "HEAD") != work {
		t.Fatal("branch moved")
	}
	assertNoLeases(t, c, ctx)
}

// A review recorded before the rebase with no patch-id carries when the
// rebase leaves the diff unchanged, off the line, and stops land when it
// changed it.
func TestLandCarriesReviewOnlyForAnUnchangedDiff(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	writeFile(t, filepath.Join(r.wt, "a.txt"), "ONE\ntwo\nthree\n")
	meshtest.Git(t, r.wt, "commit", "-qam", "lead a")
	review := func() string {
		t.Helper()
		tree := gitOut(t, "rev-parse", "HEAD^{tree}")
		if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "review", Tree: tree, Commit: gitOut(t, "rev-parse", "HEAD"),
			OK: true, Model: "claude-opus-5-5", By: "routine.review.x"}); err != nil {
			t.Fatal(err)
		}
		return tree
	}
	reviewed := review()

	// Main changes a line next to the lead's: the diff's context moved.
	r.pushOther(t, "a.txt", "one\ntwo\nTHREE\n")
	_, errs, code := runCLI(t, as("app-012"), "land")
	if code != 3 || !strings.Contains(errs, "review has a result for t:"+reviewed[:8]) || !strings.Contains(errs, "the rebase changed the diff") {
		t.Fatalf("changed diff: %d %q", code, errs)
	}
	// Reviewed again on the rebased tree, after a run there that failed:
	// the newest result counts, as spec verified composes it.
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "review", Tree: gitOut(t, "rev-parse", "HEAD^{tree}"), Args: []string{"x"},
		OK: false, Model: "claude-opus-5-5", By: "routine.review.x", At: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	reviewed = review()

	// Main changes another file: the diff is the same and the review carries.
	r.pushOther(t, "c.txt", "c\n")
	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 0 || !strings.Contains(out, "review ok t:"+reviewed[:8]+" carried: the rebase left the diff unchanged (not on the verified line)\n") {
		t.Fatalf("same diff: %d\n%s\nerr %q", code, out, errs)
	}
	if front := r.specOnOrigin(t); !strings.Contains(front, "verified: verify ok t:") || strings.Contains(front, "review") {
		t.Fatalf("spec on origin:\n%s", front)
	}
}

// A review that recorded the patch-id of the diff it read lands on the
// verified line carried from the tree it read, and done accepts it; a
// changed hunk needs a review of its own.
func TestLandPutsACarriedReviewOnTheLine(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	writeFile(t, filepath.Join(r.wt, "a.txt"), "ONE\ntwo\nthree\n")
	meshtest.Git(t, r.wt, "commit", "-qam", "lead a")
	reviewed := gitOut(t, "rev-parse", "HEAD^{tree}")
	base := gitOut(t, "merge-base", "origin/main", "HEAD")
	if _, errs, code := runCLI(t, as("app-012"), "result", "record", "review", "--ok", "--base", base); code != 2 || !strings.Contains(errs, "--base needs --model") {
		t.Fatalf("base without model: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, as("app-012"), "result", "record", "review", "--ok", "--model", "claude-opus-5-5", "--base", "HEAD"); code != 3 || !strings.Contains(errs, "empty diff") {
		t.Fatalf("empty diff: %d %q", code, errs)
	}
	if out, errs, code := runCLI(t, as("app-012"), "result", "record", "review", "--ok", "--model", "claude-opus-5-5", "--base", base); code != 0 {
		t.Fatalf("record: %d %q %q", code, out, errs)
	}
	if rec, ok, err := mesh.GetResult(ctx, c, mesh.ResultKey("review", reviewed, nil, "claude-opus-5-5")); err != nil || !ok || rec.PatchID == "" {
		t.Fatalf("recorded %+v %v %v", rec, ok, err)
	}

	// Main moves in another file: a new tree, the same diff.
	r.pushOther(t, "c.txt", "c\n")
	out, errs, code := runCLI(t, as("app-012"), "land")
	merged := gitOut(t, "rev-parse", "HEAD~1")
	tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
	entry := "review ok t:" + tree[:8] + " m:claude-opus-5-5 c:" + reviewed[:8]
	if code != 0 || !strings.Contains(out, entry+" (carried from t:"+reviewed[:8]+": the diff is unchanged)\n") ||
		!strings.Contains(out, "verified: "+entry+"; verify ok t:"+tree[:8]+"\n") {
		t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
	}
	if front := r.specOnOrigin(t); !strings.Contains(front, "verified: "+entry+"; verify ok t:"+tree[:8]+"\n") ||
		!strings.Contains(front, "merged: "+merged+"\n") {
		t.Fatalf("spec on origin:\n%s", front)
	}
	rec := gitOut(t, "rev-parse", "HEAD")
	if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", rec); code != 0 || out != "done app-012 "+rec[:8]+"\n" {
		t.Fatalf("done: %d %q %q", code, out, errs)
	}
}

// done matches a carried entry by a model result at the tree it names whose
// patch-id is the diff merged landed, and refuses any other.
func TestDoneMatchesCarriedEntryAtItsTree(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	lead := gitOut(t, "rev-parse", "HEAD")
	reviewed := gitOut(t, "rev-parse", "HEAD^{tree}")
	patch, err := checkout.PatchID(ctx, r.wt, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	put := func(p string) {
		t.Helper()
		if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "review", Tree: reviewed, Commit: lead, OK: true,
			Model: "claude-opus-5-5", By: "h", PatchID: p}); err != nil {
			t.Fatal(err)
		}
	}
	// The lead's work lands on a main that moved: the same diff, a new tree.
	meshtest.Git(t, r.repo, "pull", "-q", "--ff-only", "origin", "main")
	writeFile(t, filepath.Join(r.repo, "c.txt"), "c\n")
	meshtest.Git(t, r.repo, "add", "-A")
	meshtest.Git(t, r.repo, "commit", "-qm", "other")
	meshtest.Git(t, r.repo, "cherry-pick", lead)
	merged := gitOut(t, "-C", r.repo, "rev-parse", "main")
	tree := gitOut(t, "-C", r.repo, "rev-parse", "main^{tree}")
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "verify", Tree: tree, OK: true, By: "h"}); err != nil {
		t.Fatal(err)
	}
	line := "review ok t:" + tree[:8] + " m:claude-opus-5-5 c:" + reviewed[:8] + "; verify ok t:" + tree[:8]
	writeFile(t, filepath.Join(r.repo, "docs", "proposals", "012-thing.md"),
		"---\nid: 12\nstatus: done\nbranch: lead\nmerged: "+merged+"\nverified: "+line+"\n---\nbody\n")
	meshtest.Git(t, r.repo, "commit", "-qam", "012 done")
	meshtest.Git(t, r.repo, "push", "-q", "origin", "main")
	rec := gitOut(t, "-C", r.repo, "rev-parse", "main")
	refused := func(what string) {
		t.Helper()
		if _, errs, code := runCLI(t, as("app-012"), "done", "--commit", rec); code != 3 ||
			!strings.Contains(errs, "not done: verified names review but no result for tree "+reviewed[:8]) {
			t.Fatalf("%s: %d %q", what, code, errs)
		}
	}
	// Recorded without a patch-id, or with another diff's: the entry claims
	// what no result shows.
	put("")
	refused("no patch-id")
	put("0123456789abcdef0123456789abcdef01234567")
	refused("another diff")
	put(patch)
	if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", rec); code != 0 {
		t.Fatalf("done: %d %q %q", code, out, errs)
	}
}
