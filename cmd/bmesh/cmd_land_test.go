package main

import (
	"context"
	"os"
	"os/exec"
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
	want := "required: verify (the repo's verify kind)\n" +
		"lease land.repo\n" +
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
	if strings.Contains(front, "operator_steps") { // no Needs section, no steps
		t.Fatalf("spec on origin has steps:\n%s", front)
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

// waitingForLease waits until land, run in the background, says it is
// waiting for the land lease.
func waitingForLease(t *testing.T, out *meshtest.SyncBuffer) {
	t.Helper()
	meshtest.Until(t, "land to wait for the lease", func() bool {
		return strings.Contains(out.String(), "waiting for land.repo")
	})
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
	var out, errb meshtest.SyncBuffer
	res := make(chan int, 1)
	go func() { res <- run([]string{"land"}, strings.NewReader(""), &out, &errb) }()
	waitingForLease(t, &out)
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
		if code != 0 || !strings.HasPrefix(out.String(), "required: verify (the repo's verify kind)\nwaiting for land.repo (held by app-011)\nlease land.repo\n") ||
			!strings.Contains(out.String(), "landed ") {
			t.Fatalf("land: %d\n%s\nerr %q", code, out.String(), errb.String())
		}
	case <-time.After(meshtest.Within(t)):
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

// A review recorded before the rebase with no patch-id carries onto the
// line when the rebase leaves the diff unchanged, and done accepts it; it
// stops land when the rebase changed the diff.
func TestLandCarriesReviewOnlyForAnUnchangedDiff(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
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
	tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
	entry := "review ok t:" + tree[:8] + " m:claude-opus-5-5 c:" + reviewed[:8]
	if code != 0 || !strings.Contains(out, entry+" (carried from t:"+reviewed[:8]+": the rebase left the diff unchanged)\n") {
		t.Fatalf("same diff: %d\n%s\nerr %q", code, out, errs)
	}
	if front := r.specOnOrigin(t); !strings.Contains(front, "verified: "+entry+"; verify ok t:"+tree[:8]+"\n") {
		t.Fatalf("spec on origin:\n%s", front)
	}
	rec := gitOut(t, "rev-parse", "HEAD")
	if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", rec); code != 0 || out != "done app-012 "+rec[:8]+"\n" {
		t.Fatalf("done: %d %q %q", code, out, errs)
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
	if code != 0 || !strings.HasPrefix(out, "required: verify (the repo's verify kind), review (t:"+reviewed[:8]+" at HEAD)\n") ||
		!strings.Contains(out, entry+" (carried from t:"+reviewed[:8]+": the diff is unchanged)\n") ||
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

// A model's review that recorded the diff it read and did not carry read
// another diff: land refuses whatever other model's review has no patch-id,
// and leaves its recorded patch-id alone.
func TestLandRefusesToCarryAReviewThatReadAnotherDiff(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	writeFile(t, filepath.Join(r.wt, "a.txt"), "ONE\ntwo\nthree\n")
	meshtest.Git(t, r.wt, "commit", "-qam", "lead a")
	tree, commit := gitOut(t, "rev-parse", "HEAD^{tree}"), gitOut(t, "rev-parse", "HEAD")
	for _, res := range []mesh.Result{
		{Kind: "review", Tree: tree, Commit: commit, OK: true, Model: "claude-opus-5-5", PatchID: "0123456789abcdef", By: "routine.review.x"},
		{Kind: "review", Tree: tree, Commit: commit, OK: true, Model: "claude-sonnet-5-5", By: "routine.review.y"},
	} {
		if err := mesh.PutResult(ctx, c, res); err != nil {
			t.Fatal(err)
		}
	}
	r.pushOther(t, "c.txt", "c\n")
	_, errs, code := runCLI(t, as("app-012"), "land")
	if code != 3 || !strings.Contains(errs, "the rebase changed the diff") {
		t.Fatalf("land: %d %q", code, errs)
	}
	if rec, ok, err := mesh.GetResult(ctx, c, mesh.ResultKey("review", tree, nil, "claude-opus-5-5")); err != nil || !ok || rec.PatchID != "0123456789abcdef" {
		t.Fatalf("recorded patch-id changed: %+v %v %v", rec, ok, err)
	}
}

// sendTo puts m in to's inbox.
func sendTo(t *testing.T, c *mesh.Client, ctx context.Context, to string, m mesh.Message) {
	t.Helper()
	m.To = to
	if _, err := mesh.Send(ctx, c, m); err != nil {
		t.Fatal(err)
	}
}

// An unread message from the operator refuses the landing and stays unread;
// once the inbox is read, the same command lands.
func TestLandRefusesOnUnreadOperatorMail(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	if _, errs, code := runCLI(t, as("app-012"), "send", "app-012", "two reviews before landing", "--node", "operator"); code != 0 {
		t.Fatal(code, errs)
	}
	work := gitOut(t, "rev-parse", "HEAD")
	base := r.originMain(t)
	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 3 || out != "" || errs != "bmesh: refused: 1 unread message(s) from the operator; run bmesh inbox, act on them, then run bmesh land again\n" {
		t.Fatalf("land: %d %q %q", code, out, errs)
	}
	if r.originMain(t) != base || gitOut(t, "rev-parse", "HEAD") != work {
		t.Fatal("refused land moved origin or the branch")
	}
	assertNoLeases(t, c, ctx)
	p, _ := mesh.ParsePath("app-012")
	if n, err := mesh.Unread(ctx, c, p); err != nil || n != 1 {
		t.Fatalf("unread after the refusal: %d %v", n, err)
	}
	if out, errs, code := runCLI(t, as("app-012"), "inbox"); code != 0 || !strings.Contains(out, "operator: two reviews before landing") {
		t.Fatalf("inbox: %d %q %q", code, out, errs)
	}
	if out, errs, code := runCLI(t, as("app-012"), "land"); code != 0 || strings.Contains(out, "unread:") || !strings.Contains(out, "landed ") {
		t.Fatalf("land after inbox: %d\n%s\nerr %q", code, out, errs)
	}
}

// Reports, replies and other nodes' messages don't stop a landing; they are
// named on one line before it goes on.
func TestLandNamesOtherUnreadMail(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	sendTo(t, c, ctx, "app-012", mesh.Message{From: "app-012.alpha", Kind: "report", Text: "done from app-012.alpha: branch lead-alpha at 1a2b3c4d",
		Report: &mesh.Report{Event: "done", From: "app-012.alpha", Branch: "lead-alpha", Commit: "1a2b3c4d5e6f"}})
	sendTo(t, c, ctx, "app-012", mesh.Message{From: "routine.review", Kind: "reply", Reply: &mesh.Reply{Job: "j1", Kind: "review", OK: true}})
	sendTo(t, c, ctx, "app-012", mesh.Message{From: "app-012.beta", Kind: "message", Text: "ask from app-012.beta: which file?"})
	base := r.originMain(t)
	out, errs, code := runCLI(t, as("app-012"), "land")
	want := "unread: done from app-012.alpha (branch lead-alpha at 1a2b3c4d); reply to job j1 (review ok); message from app-012.beta\n" +
		"required: verify (the repo's verify kind)\n" +
		"lease land.repo\n" + "fetch origin/main " + base[:8] + "\n"
	if code != 0 || !strings.HasPrefix(out, want) || !strings.Contains(out, "landed ") {
		t.Fatalf("land: %d\n%s\nwant prefix\n%s\nerr %q", code, out, want, errs)
	}
	if r.originMain(t) == base {
		t.Fatal("nothing pushed")
	}
}

// Land run outside a node has no inbox to read.
func TestLandWithNoNodeSkipsTheInbox(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	sendTo(t, c, ctx, "app-012", mesh.Message{From: "operator", Kind: "message", Text: "hold"})
	out, errs, code := runCLI(t, nil, "land", "--spec", "012")
	if code != 0 || strings.Contains(out, "unread:") || !strings.Contains(out, "landed ") {
		t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
	}
}

// An operator message that arrives while land waits for the lease refuses
// it once the lease is taken, before the fetch.
func TestLandReadsTheInboxAgainUnderTheLease(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	key, err := mesh.AcquireLease(ctx, c, "land.repo", "app-011", time.Minute, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	base := r.originMain(t)
	t.Setenv("BMESH_NODE", "app-012")
	var out, errb meshtest.SyncBuffer
	res := make(chan int, 1)
	go func() { res <- run([]string{"land"}, strings.NewReader(""), &out, &errb) }()
	waitingForLease(t, &out)
	sendTo(t, c, ctx, "app-012", mesh.Message{From: "operator", Kind: "message", Text: "hold"})
	if err := mesh.ReleaseLease(ctx, c, key, "app-011"); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-res:
		// prepare read an empty inbox (it got as far as waiting); the read
		// under the lease found the message.
		if code != 3 || out.String() != "required: verify (the repo's verify kind)\nwaiting for land.repo (held by app-011)\nlease land.repo\n" ||
			errb.String() != "bmesh: refused: 1 unread message(s) from the operator; run bmesh inbox, act on them, then run bmesh land again\n" {
			t.Fatalf("land: %d\n%s\nerr %q", code, out.String(), errb.String())
		}
	case <-time.After(meshtest.Within(t)):
		t.Fatal("land never took the released lease")
	}
	if r.originMain(t) != base {
		t.Fatal("land pushed")
	}
	assertNoLeases(t, c, ctx)
}

// putReview records an ok review with no patch-id for HEAD's tree, as a
// review routine did before results recorded the diff they read; it
// returns the tree.
func putReview(t *testing.T, c *mesh.Client, ctx context.Context) string {
	t.Helper()
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "review", Tree: tree, Commit: gitOut(t, "rev-parse", "HEAD"),
		OK: true, Model: "claude-opus-5-5", By: "routine.review.x"}); err != nil {
		t.Fatal(err)
	}
	return tree
}

// A reviewed branch whose land stopped on a conflict and that was then
// rebased by hand still requires its review: the resolution changed the
// diff, so land refuses with the --again line and pushes nothing.
func TestLandRequiresTheReviewAfterAHandRebase(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	writeFile(t, filepath.Join(r.wt, "a.txt"), "ONE\ntwo\nthree\n")
	meshtest.Git(t, r.wt, "commit", "-qam", "lead a")
	reviewed := putReview(t, c, ctx)
	r.pushOther(t, "c.txt", "c\n")
	moved := r.pushOther(t, "a.txt", "uno\ntwo\nthree\n")
	if _, errs, code := runCLI(t, as("app-012"), "land"); code != 3 || !strings.Contains(errs, "conflicts in a.txt") {
		t.Fatalf("first land: %d %q", code, errs)
	}

	// The lead rebases by hand, keeping its line.
	if err := gitQuiet(r.wt, "rebase", "origin/main"); err == nil {
		t.Fatal("rebase did not conflict")
	}
	writeFile(t, filepath.Join(r.wt, "a.txt"), "ONE\ntwo\nthree\n")
	meshtest.Git(t, r.wt, "add", "a.txt")
	meshtest.Git(t, r.wt, "-c", "core.editor=true", "rebase", "--continue")
	rebased := gitOut(t, "rev-parse", "HEAD")

	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 3 || !strings.HasPrefix(out, "required: verify (the repo's verify kind), review (t:"+reviewed[:8]+" before the rebase at "+rebased[:8]+")\n") ||
		!strings.Contains(errs, "review has a result for t:"+reviewed[:8]) || !strings.Contains(errs, "bmesh routine queue review --again") {
		t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
	}
	if r.originMain(t) != moved {
		t.Fatal("land pushed")
	}
	assertNoLeases(t, c, ctx)
}

// A hand rebase through a conflict whose resolution left the diff as it
// was carries the review from before it onto the line, and done accepts it.
func TestLandCarriesTheReviewAcrossAHandRebase(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	// A change to a.txt and its revert: the diff is b.txt alone, and the
	// rebase still stops on a.txt.
	writeFile(t, filepath.Join(r.wt, "a.txt"), "ONE\ntwo\nthree\n")
	meshtest.Git(t, r.wt, "commit", "-qam", "lead a")
	writeFile(t, filepath.Join(r.wt, "a.txt"), "one\ntwo\nthree\n")
	meshtest.Git(t, r.wt, "commit", "-qam", "lead a back")
	reviewed := putReview(t, c, ctx)
	r.pushOther(t, "a.txt", "uno\ntwo\nthree\n")
	if _, errs, code := runCLI(t, as("app-012"), "land"); code != 3 || !strings.Contains(errs, "conflicts in a.txt") {
		t.Fatalf("first land: %d %q", code, errs)
	}
	// Resolved by hand for main's side: both a.txt commits fall away.
	meshtest.Git(t, r.wt, "rebase", "-X", "ours", "origin/main")
	rebased := gitOut(t, "rev-parse", "HEAD")

	out, errs, code := runCLI(t, as("app-012"), "land")
	tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
	entry := "review ok t:" + tree[:8] + " m:claude-opus-5-5 c:" + reviewed[:8]
	if code != 0 || !strings.HasPrefix(out, "required: verify (the repo's verify kind), review (t:"+reviewed[:8]+" before the rebase at "+rebased[:8]+")\n") ||
		!strings.Contains(out, entry+" (carried from t:"+reviewed[:8]+": the rebase left the diff unchanged)\n") {
		t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
	}
	if front := r.specOnOrigin(t); !strings.Contains(front, "verified: "+entry+"; verify ok t:"+tree[:8]+"\n") {
		t.Fatalf("spec on origin:\n%s", front)
	}
	rec := gitOut(t, "rev-parse", "HEAD")
	if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", rec); code != 0 || out != "done app-012 "+rec[:8]+"\n" {
		t.Fatalf("done: %d %q %q", code, out, errs)
	}
}

// A review's findings fixed in a commit on top land on verify alone, the
// fix's tree having no review, whether the branch is then rebased by hand
// onto a main that moved or not: the head before the rebase is the fix's.
func TestLandAFixedReviewOnVerifyAlone(t *testing.T) {
	for _, rebase := range []bool{false, true} {
		t.Run(map[bool]string{false: "unrebased", true: "rebased"}[rebase], func(t *testing.T) {
			c, ctx := setup(t)
			r := newLandRepo(t)
			putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
			putReview(t, c, ctx)
			writeFile(t, filepath.Join(r.wt, "b.txt"), "b fixed\n")
			meshtest.Git(t, r.wt, "commit", "-qam", "the review's findings")
			r.pushOther(t, "c.txt", "c\n")
			if rebase {
				meshtest.Git(t, r.wt, "fetch", "-q", "origin")
				meshtest.Git(t, r.wt, "rebase", "-q", "origin/main")
			}
			out, errs, code := runCLI(t, as("app-012"), "land")
			tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
			if code != 0 || !strings.HasPrefix(out, "required: verify (the repo's verify kind)\n") ||
				!strings.Contains(out, "verified: verify ok t:"+tree[:8]+"\n") {
				t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
			}
		})
	}
}

// gitQuiet runs git in dir and reports whether it failed.
func gitQuiet(dir string, args ...string) error {
	return exec.Command("git", append([]string{"-C", dir}, args...)...).Run()
}

// scaffoldLandRepo is newLandRepo with a ship row for it whose verify is
// the verify kind's command, and main scaffolded from that row by bmesh
// scaffold (committed and pushed), the branch rebased onto it.
func scaffoldLandRepo(t *testing.T, c *mesh.Client, ctx context.Context) landRepo {
	t.Helper()
	r := newLandRepo(t)
	writeLandRow(t, r, `"sh \"$HOOK\""`)
	if out, errs, code := runCLI(t, nil, "scaffold", "repo"); code != 0 || !strings.Contains(out, "wrote AGENTS.md\n") {
		t.Fatalf("scaffold: %d %q %q", code, out, errs)
	}
	if out, errs, code := runCLI(t, nil, "scaffold", "repo", "--check"); code != 0 || out != "" {
		t.Fatalf("scaffold left drift: %d %q %q", code, out, errs)
	}
	meshtest.Git(t, r.repo, "add", "-A")
	meshtest.Git(t, r.repo, "commit", "-m", "scaffold")
	meshtest.Git(t, r.repo, "push", "-q", "origin", "main")
	meshtest.Git(t, r.wt, "rebase", "-q", "main")
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	return r
}

// writeLandRow writes the registry with r's main checkout as ship row
// "repo", verify the TOML array items given.
func writeLandRow(t *testing.T, r landRepo, verify string) {
	t.Helper()
	writeRepos(t, "[repos.repo]\npath = \""+r.repo+"\"\nverify = ["+verify+"]\nroutines = [\"verify\"]\n")
}

func TestLandRefusesAVerifyKindTheRowDoesntWrite(t *testing.T) {
	c, ctx := setup(t)
	r := scaffoldLandRepo(t, c, ctx)
	// The lead adds a command to the verify kind by hand.
	writeFile(t, filepath.Join(r.wt, ".bmesh", "routines", "verify.toml"),
		"mode = \"inline\"\ndeterministic = true\ncommand = \"sh \\\"$HOOK\\\" && true\"\n")
	meshtest.Git(t, r.wt, "commit", "-qam", "verify by hand")
	base := r.originMain(t)
	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 3 || errs != "bmesh: refused: the branch's scaffolded files differ from what bmesh scaffold writes from repo's row; "+
		"the row's verify and setup are the source: bmesh repos propose with the new verify or setup, or restore the scaffolded files\n" {
		t.Fatalf("land: %d\n%s\n%q", code, out, errs)
	}
	for _, w := range []string{"--- a/.bmesh/routines/verify.toml\n", "-command = \"sh \\\"$HOOK\\\" && true\"\n", "+command = \"sh \\\"$HOOK\\\"\"\n"} {
		if !strings.Contains(out, w) {
			t.Errorf("land's drift lacks %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "verify ok") || r.originMain(t) != base {
		t.Fatalf("land went on past the drift:\n%s", out)
	}
	// The same edit in AGENTS.md's blocks is refused with that diff too.
	meshtest.Git(t, r.wt, "reset", "-q", "--hard", "HEAD~1")
	agents := filepath.Join(r.wt, "AGENTS.md")
	b, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, agents, strings.ReplaceAll(string(b), "`sh \"$HOOK\"`", "`sh \"$HOOK\"`, `true`"))
	meshtest.Git(t, r.wt, "commit", "-qam", "AGENTS.md by hand")
	out, errs, code = runCLI(t, as("app-012"), "land")
	if code != 3 || !strings.Contains(errs, "the row's verify and setup are the source") || !strings.Contains(out, "--- a/AGENTS.md\n") {
		t.Fatalf("land: %d\n%s\n%q", code, out, errs)
	}
}

// A row's setup is scaffolded into .bmesh/config.toml; a branch that edits
// that [setup] by hand is refused like a verify kind edited by hand.
func TestLandRefusesASetupTheRowDoesntWrite(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	writeRepos(t, "[repos.repo]\npath = \""+r.repo+"\"\nverify = [\"sh \\\"$HOOK\\\"\"]\nroutines = [\"verify\"]\nsetup = \"true\"\n")
	if out, errs, code := runCLI(t, nil, "scaffold", "repo"); code != 0 || !strings.Contains(out, "wrote .bmesh/config.toml\n") {
		t.Fatalf("scaffold: %d %q %q", code, out, errs)
	}
	meshtest.Git(t, r.repo, "add", "-A")
	meshtest.Git(t, r.repo, "commit", "-m", "scaffold")
	meshtest.Git(t, r.repo, "push", "-q", "origin", "main")
	meshtest.Git(t, r.wt, "rebase", "-q", "main")
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	writeFile(t, filepath.Join(r.wt, ".bmesh", "config.toml"), "[setup]\ncommand = \"echo by hand\"\n")
	meshtest.Git(t, r.wt, "commit", "-qam", "setup by hand")
	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 3 || !strings.Contains(errs, "the row's verify and setup are the source") ||
		!strings.Contains(out, "--- a/.bmesh/config.toml\n") || !strings.Contains(out, "+command = \"true\"\n") {
		t.Fatalf("land: %d\n%s\n%q", code, out, errs)
	}
}

func TestLandABranchThatFollowsTheRow(t *testing.T) {
	c, ctx := setup(t)
	r := scaffoldLandRepo(t, c, ctx)
	// The row gained a command and the lead scaffolds the branch from it, in
	// its worktree, the main checkout untouched.
	writeLandRow(t, r, `"sh \"$HOOK\"", "true"`)
	out, errs, code := runCLI(t, nil, "scaffold", r.wt)
	if code != 0 || out != "wrote AGENTS.md\nwrote .bmesh/routines/verify.toml\n" {
		t.Fatalf("scaffold the worktree: %d %q %q", code, out, errs)
	}
	if st := gitOut(t, "-C", r.repo, "status", "--porcelain"); st != "" {
		t.Fatalf("the main checkout changed: %s", st)
	}
	meshtest.Git(t, r.wt, "add", "-A")
	meshtest.Git(t, r.wt, "commit", "-qm", "rescaffold")
	if b, _ := os.ReadFile(filepath.Join(r.wt, ".bmesh", "routines", "verify.toml")); !strings.Contains(string(b), `command = "sh \"$HOOK\" && true"`) {
		t.Fatalf("scaffold didn't follow the row's verify:\n%s", b)
	}
	out, errs, code = runCLI(t, as("app-012"), "land")
	if code != 0 || !strings.Contains(out, "scaffold: follows the row\n") || !strings.Contains(out, "\nlanded ") {
		t.Fatalf("land: %d\n%s\n%q", code, out, errs)
	}
}

// The rules block is the repo's own: a branch that edits or re-picks it
// lands, and scaffold keeps the edit, marking it customized.
func TestLandABranchThatEditedItsRules(t *testing.T) {
	c, ctx := setup(t)
	r := scaffoldLandRepo(t, c, ctx)
	agents := filepath.Join(r.wt, "AGENTS.md")
	b, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "<!-- bmesh:rules core ") {
		t.Fatalf("scaffold wrote no core rules:\n%s", b)
	}
	writeFile(t, agents, strings.Replace(string(b), "Delete what's dead before adding.", "Keep diffs small.", 1))
	out, errs, code := runCLI(t, nil, "scaffold", r.wt, "--check")
	if code != 1 || !strings.HasPrefix(out, "AGENTS.md: rules are customized; kept as written\n") ||
		!strings.Contains(out, "+<!-- bmesh:rules customized -->\n") || strings.Contains(out, "+- Make the smallest change") {
		t.Fatalf("scaffold --check: %d %q %q", code, out, errs)
	}
	if out, errs, code := runCLI(t, nil, "scaffold", r.wt); code != 0 || out != "AGENTS.md: rules are customized; kept as written\nwrote AGENTS.md\n" {
		t.Fatalf("scaffold: %d %q %q", code, out, errs)
	}
	meshtest.Git(t, r.wt, "commit", "-qam", "our rules")
	out, errs, code = runCLI(t, as("app-012"), "land")
	if code != 0 || !strings.Contains(out, "scaffold: follows the row\n") || !strings.Contains(out, "\nlanded ") {
		t.Fatalf("land: %d\n%s\n%q", code, out, errs)
	}
}

func TestScaffoldRefusesAnUnknownRuleSet(t *testing.T) {
	setup(t)
	r := newLandRepo(t)
	writeLandRow(t, r, `"true"`)
	writeFile(t, filepath.Join(r.repo, "AGENTS.md"), "# A\n\n<!-- bmesh:rules core style -->\n<!-- /bmesh:rules -->\n")
	_, errs, code := runCLI(t, nil, "scaffold", "repo")
	if code != 1 || errs != "bmesh: AGENTS.md: unknown rule set style; bmesh has core, design, tests, tdd, prose\n" {
		t.Fatalf("scaffold: %d %q", code, errs)
	}
}

func TestLandARowThatMovedUnderAnUntouchedBranch(t *testing.T) {
	c, ctx := setup(t)
	r := scaffoldLandRepo(t, c, ctx)
	// A row published mid-build drifts files the branch never touched: not
	// the branch's drift.
	writeLandRow(t, r, `"sh \"$HOOK\"", "true"`)
	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 0 || !strings.Contains(out, "scaffold: follows the row\n") {
		t.Fatalf("land: %d\n%s\n%q", code, out, errs)
	}
}

func TestLandAnUnscaffoldedRepo(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	writeLandRow(t, r, `"sh \"$HOOK\""`)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	writeFile(t, filepath.Join(r.wt, ".bmesh", "routines", "verify.toml"),
		"mode = \"inline\"\ndeterministic = true\ncommand = \"sh \\\"$HOOK\\\" && true\"\n")
	meshtest.Git(t, r.wt, "commit", "-qam", "verify by hand")
	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 0 || !strings.Contains(out, "scaffold: not scaffolded, not checked\n") || !strings.Contains(out, "\nlanded ") {
		t.Fatalf("land: %d\n%s\n%q", code, out, errs)
	}
}

// codeRow writes the registry with r's main checkout as row "repo" whose
// code is the TOML array items given ("" for a row without code).
func codeRow(t *testing.T, r landRepo, code string) {
	t.Helper()
	row := "[repos.repo]\npath = \"" + r.repo + "\"\n"
	if code != "" {
		row += "code = [" + code + "]\n"
	}
	writeRepos(t, row)
}

// countRuns makes $HOOK log each verify run before body and returns the
// count so far.
func countRuns(t *testing.T, r landRepo, body string) func() int {
	t.Helper()
	log := filepath.Join(r.root, "runs.log")
	writeFile(t, os.Getenv("HOOK"), "echo ran >> '"+log+"'\n"+body+"\n")
	return func() int {
		b, _ := os.ReadFile(log)
		return strings.Count(string(b), "ran\n")
	}
}

// Main moves with a commit outside the row's code while verify runs: the
// retry carries the verify to the new tree, stores it there, and done
// accepts the landing.
func TestLandCarriesVerifyOverACommitOutsideTheCode(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	codeRow(t, r, `"*.txt"`)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	other := filepath.Join(r.root, "other")
	meshtest.Git(t, r.root, "clone", "-q", "-b", "main", r.origin, other)
	mark := filepath.Join(r.root, "moved")
	runs := countRuns(t, r, "test -e '"+mark+"' && exit 0\ntouch '"+mark+"'\n"+
		"cd '"+other+"' && echo art > logo.svg && git add logo.svg && git commit -qm 'art' && git push -q origin HEAD:main")
	first := gitOut(t, "rev-parse", "HEAD^{tree}")

	out, errs, code := runCLI(t, as("app-012"), "land")
	rec := gitOut(t, "rev-parse", "HEAD")
	tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
	if code != 0 || runs() != 1 || strings.Count(out, "main moved; again from the fetch\n") != 1 ||
		!strings.Contains(out, "verify ok t:"+first[:8]+" (recorded)\n") ||
		!strings.Contains(out, "verify ok t:"+tree[:8]+" (carried from t:"+first[:8]+": nothing under code changed)\n") ||
		!strings.HasSuffix(out, "landed "+rec[:8]+"\n") {
		t.Fatalf("land: %d runs %d\n%s\nerr %q", code, runs(), out, errs)
	}
	if gitOut(t, "-C", r.origin, "show", "main:logo.svg") != "art" {
		t.Fatal("not landed over the moved main")
	}
	got, ok, err := mesh.GetResult(ctx, c, mesh.ResultKey("verify", tree, nil, ""))
	if err != nil || !ok || !got.OK || got.Commit != gitOut(t, "rev-parse", "HEAD~1") ||
		!strings.HasPrefix(got.Output, "carried from t:"+first[:8]+": nothing under code changed\n") {
		t.Fatalf("stored %+v %v %v", got, ok, err)
	}
	if front := r.specOnOrigin(t); !strings.Contains(front, "verified: verify ok t:"+tree[:8]+"\n") {
		t.Fatalf("spec on origin:\n%s", front)
	}
	if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", rec); code != 0 || out != "done app-012 "+rec[:8]+"\n" {
		t.Fatalf("done: %d %q %q", code, out, errs)
	}
}

// A verify recorded for the branch carries over a commit main gained
// outside the code, and is run again for one inside it: a file the row's
// code lists, the routine kinds whatever it lists, and without code
// everything but the spec files, a README beside them included.
func TestLandCarriesVerifyOnlyOutsideTheCode(t *testing.T) {
	kindFile := ".bmesh/routines/verify.toml"
	for _, tc := range []struct {
		name, code, file, body string
		carried                bool
		inputs                 string
	}{
		{"readme outside the code", `"*.txt"`, "README.md", "# app\n", true, ""},
		{"a lockfile outside the code", `"*.txt"`, "web/package-lock.json", "{}\n", true, ""},
		{"a setup input", `"*.txt"`, "web/package-lock.json", "{}\n", false, `":(glob)**/package-lock.json"`},
		{"a listed file", `"*.txt"`, "c.txt", "c\n", false, ""},
		{"the verify kind", `"*.txt"`, kindFile, "mode = \"inline\"\ndeterministic = true\ncommand = \"sh \\\"$HOOK\\\"\"\n# again\n", false, ""},
		{"no code: a spec file", "", "docs/proposals/013-other.md", "---\nid: 13\nstatus: accepted\n---\n", true, ""},
		{"no code: the spec README", "", "docs/proposals/README.md", "# specs\n", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ctx := setup(t)
			r := newLandRepo(t)
			codeRow(t, r, tc.code)
			if tc.inputs != "" {
				writeRepos(t, "[repos.repo]\npath = \""+r.repo+"\"\ncode = ["+tc.code+"]\nsetup = \"true\"\nsetup_inputs = ["+tc.inputs+"]\n")
			}
			putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
			runs := countRuns(t, r, "true")
			before := gitOut(t, "rev-parse", "HEAD^{tree}")
			if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "verify", Tree: before, Commit: gitOut(t, "rev-parse", "HEAD"),
				OK: true, By: "app-012"}); err != nil {
				t.Fatal(err)
			}
			r.pushOther(t, tc.file, tc.body)

			out, errs, code := runCLI(t, as("app-012"), "land")
			tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
			if code != 0 {
				t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
			}
			carried := "verify ok t:" + tree[:8] + " (carried from t:" + before[:8] + ": nothing under code changed)\n"
			ran := "verify ok t:" + tree[:8] + " (recorded)\n"
			if tc.carried && (runs() != 0 || !strings.Contains(out, carried)) || !tc.carried && (runs() != 1 || !strings.Contains(out, ran)) {
				t.Fatalf("carried %v: runs %d\n%s", tc.carried, runs(), out)
			}
			if front := r.specOnOrigin(t); !strings.Contains(front, "verified: verify ok t:"+tree[:8]+"\n") {
				t.Fatalf("spec on origin:\n%s", front)
			}
		})
	}
}

// A branch whose last commit is outside the code, rebased over a ledger
// commit, lands on a carry from its verified parent; a branch with only
// docs since main lands on main's verify. Neither runs it, and done
// accepts the carried entry.
func TestLandCarriesVerifyFromTheBranchRun(t *testing.T) {
	for _, tc := range []struct {
		name     string
		docsOnly bool
	}{
		{"docs after the verified work", false},
		{"a docs-only branch", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ctx := setup(t)
			r := newLandRepo(t)
			codeRow(t, r, `"*.txt"`)
			putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
			if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
				t.Fatal(code, errs)
			}
			runs := countRuns(t, r, "true")
			if tc.docsOnly {
				meshtest.Git(t, r.wt, "reset", "-q", "--hard", "main")
			}
			verified := gitOut(t, "rev-parse", "HEAD^{tree}")
			if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "verify", Tree: verified, Commit: gitOut(t, "rev-parse", "HEAD"),
				OK: true, By: "app-012"}); err != nil {
				t.Fatal(err)
			}
			commitDoc(t, "README.md", "# app\n")
			r.pushOther(t, "docs/proposals/013-other.md", "---\nid: 13\nstatus: accepted\n---\n")

			out, errs, code := runCLI(t, as("app-012"), "land")
			rec := gitOut(t, "rev-parse", "HEAD")
			tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
			if code != 0 || runs() != 0 ||
				!strings.Contains(out, "verify ok t:"+tree[:8]+" (carried from t:"+verified[:8]+": nothing under code changed)\n") {
				t.Fatalf("land: %d runs %d\n%s\nerr %q", code, runs(), out, errs)
			}
			if front := r.specOnOrigin(t); !strings.Contains(front, "verified: verify ok t:"+tree[:8]+"\n") {
				t.Fatalf("spec on origin:\n%s", front)
			}
			if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", rec); code != 0 || out != "done app-012 "+rec[:8]+"\n" {
				t.Fatalf("done: %d %q %q", code, out, errs)
			}
		})
	}
}

// commitOn writes file on the lead's branch and commits it.
func (r landRepo) commitOn(t *testing.T, file, body string) {
	t.Helper()
	writeFile(t, filepath.Join(r.wt, file), body)
	meshtest.Git(t, r.wt, "add", "-A")
	meshtest.Git(t, r.wt, "commit", "-qm", "lead "+file)
}

// Main and the branch each append a test to one file: land resolves the
// rebase's stop (kept both, main's first) and lands.
func TestLandResolvesTwoAppendedTests(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	r.pushOther(t, "x_test.go", "package x\n")
	meshtest.Git(t, r.wt, "pull", "-q", "--rebase", "origin", "main")
	r.commitOn(t, "x_test.go", "package x\n\nfunc TestB(t *testing.T) {}\n")
	r.pushOther(t, "x_test.go", "package x\n\nfunc TestA(t *testing.T) {}\n")

	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 0 || !strings.Contains(out, "\nrebase: resolved x_test.go (kept both)\nrebase onto ") {
		t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
	}
	want := "package x\n\nfunc TestA(t *testing.T) {}\n\nfunc TestB(t *testing.T) {}\n"
	if got := gitOut(t, "-C", r.origin, "show", "main:x_test.go") + "\n"; got != want {
		t.Fatalf("x_test.go on origin:\n%s", got)
	}
}

// A file both sides end without a final newline keeps it so when the
// conflict at its end is resolved.
func TestLandResolvesAtAFileEndWithNoNewline(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	r.pushOther(t, "x.txt", "a")
	meshtest.Git(t, r.wt, "pull", "-q", "--rebase", "origin", "main")
	r.commitOn(t, "x.txt", "a\nc")
	r.pushOther(t, "x.txt", "a\nb")

	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 0 || !strings.Contains(out, "rebase: resolved x.txt (kept both, applied both)\n") {
		t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
	}
	if b, err := exec.Command("git", "-C", r.origin, "show", "main:x.txt").Output(); err != nil || string(b) != "a\nb\nc" {
		t.Fatalf("x.txt on origin: %q %v", b, err)
	}
}

// A resolution that keeps both sides but breaks what verify checks is
// refused by verify, naming land's merge, with the branch left rebased.
func TestLandVerifyRefusesAResolutionThatBreaks(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	writeFile(t, os.Getenv("HOOK"), "test \"$(grep -c '^func A' x.go)\" -le 1\n")
	r.pushOther(t, "x.go", "package x\n")
	meshtest.Git(t, r.wt, "pull", "-q", "--rebase", "origin", "main")
	r.commitOn(t, "x.go", "package x\n\nfunc A() int { return 2 }\n")
	moved := r.pushOther(t, "x.go", "package x\n\nfunc A() int { return 1 }\n")

	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 1 || !strings.Contains(out, "rebase: resolved x.go (kept both)\n") ||
		!strings.Contains(errs, "after the rebase resolved x.go by rule: that is land's merge, and the branch is left rebased; fix it forward") {
		t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
	}
	if r.originMain(t) != moved {
		t.Fatal("land pushed")
	}
	if err := gitQuiet(r.wt, "merge-base", "--is-ancestor", moved, "HEAD"); err != nil || gitOut(t, "status", "--porcelain") != "" {
		t.Fatal("the branch is not left rebased and clean")
	}
	assertNoLeases(t, c, ctx)
}

// A stop with one file a rule resolves and one whose line both sides
// rewrote is aborted, the refusal naming each.
func TestLandNamesTheFilesNoRuleResolves(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	r.pushOther(t, "x.go", "package x\n")
	meshtest.Git(t, r.wt, "pull", "-q", "--rebase", "origin", "main")
	writeFile(t, filepath.Join(r.wt, "a.txt"), "ONE\ntwo\nthree\n")
	r.commitOn(t, "x.go", "package x\n\nfunc B() {}\n")
	work := gitOut(t, "rev-parse", "HEAD")
	r.pushOther(t, "a.txt", "uno\ntwo\nthree\n")
	moved := r.pushOther(t, "x.go", "package x\n\nfunc A() {}\n")

	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 3 || errs != "bmesh: refused: rebase onto origin/main conflicts in a.txt, which no rule resolves (a rule resolves x.go); "+
		"it is aborted and nothing is pushed. Rebase yourself, resolve it, re-validate, then run bmesh land again\n" {
		t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
	}
	if r.originMain(t) != moved || gitOut(t, "rev-parse", "HEAD") != work || gitOut(t, "status", "--porcelain") != "" {
		t.Fatal("the refusal changed origin or the branch")
	}
}

// A rebase that stops at two of the branch's commits resolves each stop:
// a test appended to a Go file, then a sentence added to a paragraph.
func TestLandResolvesARebaseThatStopsTwice(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	r.pushOther(t, "x_test.go", "package x\n")
	r.pushOther(t, "doc.md", "# Doc\n\nThe box runs jobs. It logs each.\n")
	meshtest.Git(t, r.wt, "pull", "-q", "--rebase", "origin", "main")
	r.commitOn(t, "x_test.go", "package x\n\nfunc TestB(t *testing.T) {}\n")
	r.commitOn(t, "doc.md", "# Doc\n\nThe box runs jobs. It logs each. The branch adds that.\n")
	r.pushOther(t, "x_test.go", "package x\n\nfunc TestA(t *testing.T) {}\n")
	r.pushOther(t, "doc.md", "# Doc\n\nThe box runs jobs. Main adds this. It logs each.\n")

	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 0 || !strings.Contains(out, "\nrebase: resolved x_test.go (kept both)\nrebase: resolved doc.md (by sentence)\nrebase onto ") {
		t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
	}
	if got := gitOut(t, "-C", r.origin, "show", "main:doc.md"); got != "# Doc\n\nThe box runs jobs. Main adds this. It logs each. The branch adds that." {
		t.Fatalf("doc.md on origin: %q", got)
	}
}
