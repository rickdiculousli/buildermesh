package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	poll, mainPoll, every := landPoll, landMainPoll, landEventEvery
	landPoll, landMainPoll = 20*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { landPoll, landMainPoll, landEventEvery = poll, mainPoll, every })
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
		"verify ok t:" + tree[:8] + " (recorded)\n" +
		"lease land.repo\n" +
		"fetch origin/main " + base[:8] + "\n" +
		"rebase: on " + base[:8] + " already\n" +
		"verify ok t:" + tree[:8] + " (ran before the lease)\n" +
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

// Main moves each time verify runs, three times (the first run being
// verify before the lease, which the rebase under it takes in): each push
// under the lease is refused, the record commit taken back and land starts
// again from the fetch, and lands on the third try.
func TestLandStartsAgainWhileMainMoves(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	other := filepath.Join(r.root, "other")
	meshtest.Git(t, r.root, "clone", "-q", "-b", "main", r.origin, other)
	runs := filepath.Join(r.root, "runs")
	writeFile(t, os.Getenv("HOOK"), "echo >> '"+runs+"'\nn=$(wc -l < '"+runs+"')\ntest $n -le 3 || exit 0\n"+
		"cd '"+other+"' && echo $n > c.txt && git add c.txt && git commit -qm 'other c' && git push -q origin HEAD:main\n")
	work := gitOut(t, "rev-parse", "HEAD")

	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 0 || strings.Count(out, "main moved; again from the fetch\n") != 2 || !strings.HasSuffix(out, "landed "+gitOut(t, "rev-parse", "HEAD")[:8]+"\n") {
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

// Main moving on every try is given up on at --wait (exit 5), with nothing
// pushed and the branch as the rebase left it.
func TestLandTimesOutWhenMainKeepsMoving(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	other := filepath.Join(r.root, "other")
	meshtest.Git(t, r.root, "clone", "-q", "-b", "main", r.origin, other)
	writeFile(t, os.Getenv("HOOK"), "cd '"+other+"' && date +%s%N >> c.txt && git add c.txt && git commit -qm 'other c' && git push -q origin HEAD:main\n")

	out, errs, code := runCLI(t, as("app-012"), "land", "--wait", "1s")
	if code != exitTimeout || errs != "bmesh: timed out waiting for main to stop moving; nothing is pushed\n" || !strings.Contains(out, "main moved; again") {
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
		if code != 0 || !strings.HasPrefix(out.String(), "required: verify (the repo's verify kind)\nverify ok t:"+gitOut(t, "rev-parse", "HEAD~1^{tree}")[:8]+" (recorded)\n"+
			"waiting for land.repo (held by app-011)\nlease land.repo\n") ||
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

// landBG runs land as node in the background; its exit code arrives on
// the channel.
func landBG(t *testing.T, node string, args ...string) (out, errb *meshtest.SyncBuffer, res chan int) {
	t.Helper()
	t.Setenv("BMESH_NODE", node)
	out, errb, res = &meshtest.SyncBuffer{}, &meshtest.SyncBuffer{}, make(chan int, 1)
	go func() { res <- run(append([]string{"land"}, args...), strings.NewReader(""), out, errb) }()
	return out, errb, res
}

// landEnds waits for land in the background to end, and fails unless it
// exits code.
func landEnds(t *testing.T, res chan int, code int, out, errb *meshtest.SyncBuffer) {
	t.Helper()
	select {
	case got := <-res:
		if got != code {
			t.Fatalf("land: %d, want %d\n%s\nerr %q", got, code, out.String(), errb.String())
		}
	case <-time.After(meshtest.Within(t)):
		t.Fatalf("land never ended\n%s", out.String())
	}
}

// until waits until out says what.
func until(t *testing.T, out *meshtest.SyncBuffer, what string) {
	t.Helper()
	meshtest.Until(t, "land to say "+what, func() bool { return strings.Contains(out.String(), what) })
}

// Local main ahead of origin (someone's unpushed work) is waited for, not
// pushed, before the lease: land lands once it is pushed.
func TestLandWaitsForMainAheadOfOrigin(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	writeFile(t, filepath.Join(r.repo, "d.txt"), "d\n")
	meshtest.Git(t, r.repo, "add", "d.txt")
	meshtest.Git(t, r.repo, "commit", "-m", "coordinator's")
	theirs := gitOut(t, "-C", r.repo, "rev-parse", "main")
	out, errb, res := landBG(t, "app-012")
	until(t, out, "waiting for main in "+r.repo+" to be pushed: it has 1 commits origin/main doesn't")
	if strings.Contains(out.String(), "lease") || r.originMain(t) == theirs {
		t.Fatalf("land went on, or pushed main:\n%s", out.String())
	}
	meshtest.Git(t, r.repo, "push", "-q", "origin", "main")
	landEnds(t, res, 0, out, errb)
	if gitOut(t, "rev-parse", "HEAD~2") != theirs || r.originMain(t) != gitOut(t, "rev-parse", "HEAD") {
		t.Fatalf("not landed on the pushed main:\n%s", out.String())
	}
	if strings.Count(out.String(), "waiting for main in") != 1 {
		t.Fatalf("the wait named more than once:\n%s", out.String())
	}
	assertNoLeases(t, c, ctx)
}

// Unpushed commits on main that appear while land waits for the lease are
// waited for under it.
func TestLandWaitsForMainAheadUnderTheLease(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	key, err := mesh.AcquireLease(ctx, c, "land.repo", "app-011", time.Minute, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	out, errb, res := landBG(t, "app-012")
	waitingForLease(t, out)
	writeFile(t, filepath.Join(r.repo, "d.txt"), "d\n")
	meshtest.Git(t, r.repo, "add", "d.txt")
	meshtest.Git(t, r.repo, "commit", "-m", "coordinator's")
	if err := mesh.ReleaseLease(ctx, c, key, "app-011"); err != nil {
		t.Fatal(err)
	}
	until(t, out, "lease land.repo\nwaiting for main in "+r.repo+" to be pushed")
	if leases, err := mesh.ListLeases(ctx, c); err != nil || len(leases) != 1 || leases[0].Holder != "app-012" {
		t.Fatalf("leases while waiting %+v %v", leases, err)
	}
	meshtest.Git(t, r.repo, "push", "-q", "origin", "main")
	landEnds(t, res, 0, out, errb)
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
		"verify ok t:" + gitOut(t, "rev-parse", "HEAD~1^{tree}")[:8] + " (recorded)\n" +
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
// it there, before the lease is taken.
func TestLandReadsTheInboxWhileItWaitsForTheLease(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	key, err := mesh.AcquireLease(ctx, c, "land.repo", "app-011", time.Minute, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	base := r.originMain(t)
	out, errb, res := landBG(t, "app-012")
	waitingForLease(t, out)
	sendTo(t, c, ctx, "app-012", mesh.Message{From: "operator", Kind: "message", Text: "hold"})
	landEnds(t, res, 3, out, errb)
	// prepare read an empty inbox (it got as far as waiting); the read
	// while it waited found the message.
	if out.String() != "required: verify (the repo's verify kind)\nverify ok t:"+gitOut(t, "rev-parse", "HEAD^{tree}")[:8]+" (recorded)\n"+
		"waiting for land.repo (held by app-011)\n" ||
		errb.String() != "bmesh: refused: 1 unread message(s) from the operator; run bmesh inbox, act on them, then run bmesh land again\n" {
		t.Fatalf("land:\n%s\nerr %q", out.String(), errb.String())
	}
	if r.originMain(t) != base {
		t.Fatal("land pushed")
	}
	if err := mesh.ReleaseLease(ctx, c, key, "app-011"); err != nil {
		t.Fatal(err)
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
// The review is named as left behind, from the commit it read.
func TestLandAFixedReviewOnVerifyAlone(t *testing.T) {
	for _, rebase := range []bool{false, true} {
		t.Run(map[bool]string{false: "unrebased", true: "rebased"}[rebase], func(t *testing.T) {
			c, ctx := setup(t)
			r := newLandRepo(t)
			putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
			reviewed := putReview(t, c, ctx)
			at := gitOut(t, "rev-parse", "HEAD")
			writeFile(t, filepath.Join(r.wt, "b.txt"), "b fixed\n")
			meshtest.Git(t, r.wt, "commit", "-qam", "the review's findings")
			r.pushOther(t, "c.txt", "c\n")
			if rebase {
				meshtest.Git(t, r.wt, "fetch", "-q", "origin")
				meshtest.Git(t, r.wt, "rebase", "-q", "origin/main")
			}
			out, errs, code := runCLI(t, as("app-012"), "land")
			tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
			if code != 0 || !strings.HasPrefix(out, "required: verify (the repo's verify kind); left behind by later commits: review ok t:"+
				reviewed[:8]+" at "+at[:8]+" (not on the verified line)\n") ||
				!strings.Contains(out, "verified: verify ok t:"+tree[:8]+"\n") {
				t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
			}
		})
	}
}

// A review on an earlier commit of the branch, followed by a commit to the
// spec's own text, is not required and not on the verified line, but land
// names it, ok or failed, with the tree and commit it read.
func TestLandNamesAReviewLeftBehind(t *testing.T) {
	for _, ok := range []bool{true, false} {
		t.Run(map[bool]string{true: "ok", false: "fail"}[ok], func(t *testing.T) {
			c, ctx := setup(t)
			r := newLandRepo(t)
			putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
			reviewed := gitOut(t, "rev-parse", "HEAD^{tree}")
			at := gitOut(t, "rev-parse", "HEAD")
			if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "review", Tree: reviewed, Commit: at,
				OK: ok, Model: "claude-opus-5-5", By: "routine.review.x"}); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(r.wt, "docs", "proposals", "012-thing.md"), "---\nid: 12\nstatus: building\nbranch: lead\n---\nbody, as built\n")
			meshtest.Git(t, r.wt, "commit", "-qam", "012: as built")

			out, errs, code := runCLI(t, as("app-012"), "land")
			tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
			verdict := map[bool]string{true: "ok", false: "fail"}[ok]
			want := "required: verify (the repo's verify kind); left behind by later commits: review " + verdict +
				" t:" + reviewed[:8] + " at " + at[:8] + " (not on the verified line)\n"
			if code != 0 || !strings.HasPrefix(out, want) {
				t.Fatalf("land: %d\n%s\nwant prefix\n%s\nerr %q", code, out, want, errs)
			}
			if front := r.specOnOrigin(t); !strings.Contains(front, "verified: verify ok t:"+tree[:8]+"\n") {
				t.Fatalf("spec on origin:\n%s", front)
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
	if _, under, _ := strings.Cut(out, "lease land.repo\n"); strings.Contains(under, "verify ok") || r.originMain(t) != base {
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
	if code != 1 || errs != "bmesh: AGENTS.md: unknown rule set style; bmesh has core, design, tests, tdd, prose, ponytail\n" {
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

// Main moves with a commit outside the row's code while verify runs before
// the lease: the rebase under it carries the verify to the new tree, stores
// it there, and done accepts the landing.
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
	if code != 0 || runs() != 1 ||
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
			head := gitOut(t, "rev-parse", "HEAD^{tree}")
			r.pushOther(t, "docs/proposals/013-other.md", "---\nid: 13\nstatus: accepted\n---\n")

			out, errs, code := runCLI(t, as("app-012"), "land")
			rec := gitOut(t, "rev-parse", "HEAD")
			tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
			if code != 0 || runs() != 0 ||
				!strings.Contains(out, "verify ok t:"+head[:8]+" (carried from t:"+verified[:8]+": nothing under code changed)\n") ||
				!strings.Contains(out, "verify ok t:"+tree[:8]+" (carried from t:"+head[:8]+": nothing under code changed)\n") {
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

// The lead's branch adds spec 020 while another writer lands its own 020:
// the rebase is clean, the ledger it would push is not, and land refuses
// before the push.
func TestLandRefusesADuplicateID(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	writeFile(t, filepath.Join(r.wt, "docs", "proposals", "020-mine.md"), "---\nid: 20\nstatus: proposed\n---\nmine\n")
	meshtest.Git(t, r.wt, "add", "-A")
	meshtest.Git(t, r.wt, "commit", "-m", "spec 020")
	theirs := r.pushOther(t, "docs/proposals/020-theirs.md", "---\nid: 20\nstatus: proposed\n---\ntheirs\n")

	_, errs, code := runCLI(t, as("app-012"), "land")
	if code != 3 || !strings.Contains(errs, "refused: 020-mine.md and 020-theirs.md share id 020; renumber the newer with bmesh spec next-id") {
		t.Fatalf("land: %d %q", code, errs)
	}
	if r.originMain(t) != theirs {
		t.Fatal("origin main moved")
	}
	assertNoLeases(t, c, ctx)
}

// reviewLandRepo is newLandRepo with a pooled review kind on model m1 and
// spec 012 sized size ("" for none), both committed on the lead's branch,
// the repo in the registry and node app-012 landing it.
func reviewLandRepo(t *testing.T, c *mesh.Client, ctx context.Context, size string) landRepo {
	t.Helper()
	r := newLandRepo(t)
	writeFile(t, filepath.Join(r.wt, ".bmesh", "routines", "review.toml"), "mode = \"pooled\"\nmodels = [\"m1\"]\nprompt = \"check\"\n")
	front := "---\nid: 12\nstatus: building\nbranch: lead\n"
	if size != "" {
		front += "size: " + size + "\n"
	}
	writeFile(t, filepath.Join(r.wt, "docs", "proposals", "012-thing.md"), front+"---\nbody\n")
	meshtest.Git(t, r.wt, "add", "-A")
	meshtest.Git(t, r.wt, "commit", "-qm", "the review kind")
	writeRepos(t, "[repos.repo]\npath = \""+r.repo+"\"\n")
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012", Repo: "repo"})
	return r
}

var queuedRE = regexp.MustCompile(`review: queued (\S+)`)

// queuedJob waits for land to queue a review, and returns the job.
func queuedJob(t *testing.T, c *mesh.Client, ctx context.Context, out *meshtest.SyncBuffer, n int) mesh.Job {
	t.Helper()
	var id string
	meshtest.Until(t, "land to queue a review", func() bool {
		m := queuedRE.FindAllStringSubmatch(out.String(), -1)
		if len(m) < n {
			return false
		}
		id = m[n-1][1]
		return strings.Contains(out.String(), "waiting for job "+id)
	})
	j, ok, err := mesh.FindJob(ctx, c, "review", id)
	if err != nil || !ok {
		t.Fatalf("job %s: %v %v", id, ok, err)
	}
	return j
}

// answer takes job j off the review kind's inbox and replies to it as a
// daemon's review routine would: recorded (a result at its tree on m1 and
// the reply naming it) when record, else a reply that recorded nothing.
func answer(t *testing.T, c *mesh.Client, ctx context.Context, j mesh.Job, ok, record bool, output string) {
	t.Helper()
	if _, found, err := mesh.FetchMatch(ctx, c, mesh.RoutinePath("review"), func(m mesh.Message) bool {
		return m.Kind == "job" && m.Job != nil && m.Job.ID == j.ID
	}); err != nil || !found {
		t.Fatalf("job %s on the inbox: %v %v", j.ID, found, err)
	}
	key := ""
	if record {
		if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "review", Tree: j.Tree, Commit: j.Commit, OK: ok, Model: "m1",
			Output: output, By: "routine.review.m1-1"}); err != nil {
			t.Fatal(err)
		}
		key = mesh.ResultKey("review", j.Tree, nil, "m1")
	}
	to, _ := mesh.ParsePath("app-012")
	if err := mesh.SendReply(ctx, c, mesh.RoutinePath("review"), to, mesh.Reply{Job: j.ID, Kind: "review", Tree: j.Tree, OK: ok,
		Output: output, ResultKey: key, By: "routine.review.m1-1"}); err != nil {
		t.Fatal(err)
	}
}

// unanswered lists the review jobs no reply answers.
func unanswered(t *testing.T, c *mesh.Client, ctx context.Context) []mesh.QueuedJob {
	t.Helper()
	jobs, err := mesh.KindJobs(ctx, c, "review")
	if err != nil {
		t.Fatal(err)
	}
	return jobs
}

// An M spec with no review gets one on HEAD at medium, on the kind's first
// model with the branch's base, waits for it and lands with it on the
// verified line; the reply is acked, and land's event repeats while it
// waits.
func TestLandQueuesTheSizedReview(t *testing.T) {
	c, ctx := setup(t)
	r := reviewLandRepo(t, c, ctx, "M")
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	landEventEvery = 20 * time.Millisecond
	head := gitOut(t, "rev-parse", "HEAD")
	base := gitOut(t, "merge-base", "origin/main", "HEAD")
	out, errb, res := landBG(t, "app-012")
	j := queuedJob(t, c, ctx, out, 1)
	if j.Commit != head || j.Effort != "medium" || j.Model != "m1" || !j.Go || j.Text != "base: "+base || j.Repo != "repo" {
		t.Fatalf("job %+v", j)
	}
	if gitOut(t, "-C", r.origin, "rev-parse", "lead") != head {
		t.Fatal("the branch was not pushed for the review")
	}
	p, _ := mesh.ParsePath("app-012")
	meshtest.Until(t, "land events to repeat", func() bool {
		evs, err := mesh.History(ctx, c, mesh.EventSubject(p, "land"), time.Time{})
		return err == nil && len(evs) >= 3 && strings.Contains(string(evs[len(evs)-1].Data), "waiting for job "+j.ID)
	})
	answer(t, c, ctx, j, true, true, "fine\nVERDICT: ok")
	landEnds(t, res, 0, out, errb)
	tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
	for _, w := range []string{"review: queued " + j.ID + " (medium, size M)\n", "waiting for job " + j.ID + " (review, waiting)\n",
		"job " + j.ID + " (review): ok t:" + tree[:8] + "\n", "review: on the branch already (ok at t:" + tree[:8] + ")\n",
		"verified: review ok t:" + tree[:8] + " m:m1; verify ok t:" + tree[:8] + "\n"} {
		if !strings.Contains(out.String(), w) {
			t.Fatalf("land lacks %q:\n%s", w, out.String())
		}
	}
	if n, err := mesh.Unread(ctx, c, p); err != nil || n != 0 {
		t.Fatalf("unread after land: %d %v", n, err)
	}
	evs, err := mesh.History(ctx, c, mesh.EventSubject(p, "land"), time.Time{})
	if err != nil || len(evs) < 3 {
		t.Fatalf("land events %d %v", len(evs), err)
	}
	rec := gitOut(t, "rev-parse", "HEAD")
	if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", rec); code != 0 {
		t.Fatalf("done: %d %q %q", code, out, errs)
	}
}

// An S spec queues no review and lands on verify alone; --size L queues one
// at high; a spec with no size and no --size refuses.
func TestLandSizesTheReview(t *testing.T) {
	t.Run("S", func(t *testing.T) {
		c, ctx := setup(t)
		reviewLandRepo(t, c, ctx, "S")
		out, errs, code := runCLI(t, as("app-012"), "land")
		tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
		if code != 0 || !strings.Contains(out, "review: none (size S)\n") || !strings.Contains(out, "verified: verify ok t:"+tree[:8]+"\n") {
			t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
		}
		if jobs := unanswered(t, c, ctx); len(jobs) != 0 {
			t.Fatalf("jobs %+v", jobs)
		}
	})
	t.Run("--size L", func(t *testing.T) {
		c, ctx := setup(t)
		reviewLandRepo(t, c, ctx, "M")
		out, errb, res := landBG(t, "app-012", "--size", "L")
		j := queuedJob(t, c, ctx, out, 1)
		if j.Effort != "high" || !strings.Contains(out.String(), "review: queued "+j.ID+" (high, size L)\n") {
			t.Fatalf("job %+v\n%s", j, out.String())
		}
		answer(t, c, ctx, j, true, true, "VERDICT: ok")
		landEnds(t, res, 0, out, errb)
	})
	t.Run("no size", func(t *testing.T) {
		c, ctx := setup(t)
		r := reviewLandRepo(t, c, ctx, "")
		base := r.originMain(t)
		_, errs, code := runCLI(t, as("app-012"), "land")
		if code != exitUsage || errs != "bmesh: land needs --size: spec 012 has no size\n" || r.originMain(t) != base {
			t.Fatalf("land: %d %q", code, errs)
		}
		if _, errs, code := runCLI(t, as("app-012"), "land", "--size", "Q"); code != exitUsage || !strings.Contains(errs, `--size "Q"`) {
			t.Fatalf("bad size: %d %q", code, errs)
		}
	})
}

// A review queued by hand is waited for, not queued again, and started
// with a go when it was queued without one; so is land's own after it
// timed out (exit 5) and is run again.
func TestLandWaitsForAReviewAlreadyQueued(t *testing.T) {
	t.Run("by hand", func(t *testing.T) {
		c, ctx := setup(t)
		reviewLandRepo(t, c, ctx, "M")
		qout, errs, code := runCLI(t, as("app-012"), "routine", "queue", "review", "--effort", "medium")
		id := strings.TrimSpace(strings.TrimPrefix(qout, "queued "))
		if code != 0 || id == "" {
			t.Fatalf("queue: %d %q %q", code, qout, errs)
		}
		out, errb, res := landBG(t, "app-012")
		until(t, out, "review: waiting for job "+id+"\nwaiting for job "+id+" (review, waiting)\n")
		if jobs := unanswered(t, c, ctx); len(jobs) != 1 || strings.Contains(out.String(), "review: queued") {
			t.Fatalf("a second review: %+v\n%s", jobs, out.String())
		}
		msgs, err := mesh.InboxAfter(ctx, c, mesh.RoutinePath("review"), 0)
		if err != nil || len(msgs) != 2 || msgs[0].Kind != "job" || msgs[1].Text != "go" || msgs[1].From != "app-012" {
			t.Fatalf("the kind's inbox: %+v %v", msgs, err)
		}
		j, _, _ := mesh.FindJob(ctx, c, "review", id)
		answer(t, c, ctx, j, true, true, "VERDICT: ok")
		landEnds(t, res, 0, out, errb)
	})
	t.Run("after exit 5", func(t *testing.T) {
		c, ctx := setup(t)
		r := reviewLandRepo(t, c, ctx, "M")
		base := r.originMain(t)
		out, errs, code := runCLI(t, as("app-012"), "land", "--wait", "200ms")
		m := queuedRE.FindStringSubmatch(out)
		if code != exitTimeout || m == nil || errs != "bmesh: timed out waiting for job "+m[1]+" (review); nothing is pushed\n" || r.originMain(t) != base {
			t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
		}
		bout, berr, res := landBG(t, "app-012")
		until(t, bout, "review: waiting for job "+m[1]+"\n")
		if jobs := unanswered(t, c, ctx); len(jobs) != 1 || strings.Contains(bout.String(), "review: queued") {
			t.Fatalf("a second review: %+v\n%s", jobs, bout.String())
		}
		j, _, _ := mesh.FindJob(ctx, c, "review", m[1])
		answer(t, c, ctx, j, true, true, "VERDICT: ok")
		landEnds(t, res, 0, bout, berr)
	})
}

// A branch reviewed ok queues none and lands with it; one reviewed fail
// and then fixed queues none and lands on verify alone; one reviewed and
// rebased by hand queues none, and the review carries.
func TestLandQueuesNoSecondReview(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		c, ctx := setup(t)
		reviewLandRepo(t, c, ctx, "M")
		reviewed := putReview(t, c, ctx)
		out, errs, code := runCLI(t, as("app-012"), "land")
		tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
		if code != 0 || !strings.Contains(out, "review: on the branch already (ok at t:"+reviewed[:8]+")\n") ||
			!strings.Contains(out, "verified: review ok t:"+tree[:8]+" m:claude-opus-5-5") {
			t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
		}
		if jobs := unanswered(t, c, ctx); len(jobs) != 0 {
			t.Fatalf("jobs %+v", jobs)
		}
	})
	t.Run("fail then fixed", func(t *testing.T) {
		c, ctx := setup(t)
		rp := reviewLandRepo(t, c, ctx, "M")
		tree := gitOut(t, "rev-parse", "HEAD^{tree}")
		if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "review", Tree: tree, Commit: gitOut(t, "rev-parse", "HEAD"),
			OK: false, Model: "m1", By: "routine.review.m1-1"}); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(rp.wt, "b.txt"), "b fixed\n")
		meshtest.Git(t, rp.wt, "commit", "-qam", "the review's findings")
		out, errs, code := runCLI(t, as("app-012"), "land")
		landedTree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
		if code != 0 || !strings.Contains(out, "review: on the branch already (fail at t:"+tree[:8]+")\n") ||
			!strings.Contains(out, "verified: verify ok t:"+landedTree[:8]+"\n") {
			t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
		}
		if jobs := unanswered(t, c, ctx); len(jobs) != 0 {
			t.Fatalf("jobs %+v", jobs)
		}
	})
	t.Run("rebased by hand", func(t *testing.T) {
		c, ctx := setup(t)
		rp := reviewLandRepo(t, c, ctx, "M")
		reviewed := putReview(t, c, ctx)
		rp.pushOther(t, "c.txt", "c\n")
		meshtest.Git(t, rp.wt, "fetch", "-q", "origin")
		meshtest.Git(t, rp.wt, "rebase", "-q", "origin/main")
		out, errs, code := runCLI(t, as("app-012"), "land")
		tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
		if code != 0 || !strings.Contains(out, "review: on the branch already (ok at t:"+reviewed[:8]+")\n") ||
			!strings.Contains(out, "review ok t:"+tree[:8]+" m:claude-opus-5-5 c:"+reviewed[:8]) {
			t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
		}
		if jobs := unanswered(t, c, ctx); len(jobs) != 0 {
			t.Fatalf("jobs %+v", jobs)
		}
	})
}

// A failing review refuses with its findings; a reply whose model run never
// answered is queued once more, and a second refuses; any other reply that
// recorded nothing refuses at once. Every reply land read is acked.
func TestLandReadsTheReviewsReply(t *testing.T) {
	p, _ := mesh.ParsePath("app-012")
	t.Run("fail", func(t *testing.T) {
		c, ctx := setup(t)
		reviewLandRepo(t, c, ctx, "M")
		out, errb, res := landBG(t, "app-012")
		j := queuedJob(t, c, ctx, out, 1)
		answer(t, c, ctx, j, false, true, "### high: b.txt is wrong\nVERDICT: fail")
		landEnds(t, res, 3, out, errb)
		if !strings.Contains(out.String(), "### high: b.txt is wrong\nVERDICT: fail\n") ||
			errb.String() != "bmesh: refused: review failed at t:"+j.Tree[:8]+" (job "+j.ID+"); nothing is pushed: fix what it found, commit and run bmesh land again\n" {
			t.Fatalf("land:\n%s\nerr %q", out.String(), errb.String())
		}
		if n, err := mesh.Unread(ctx, c, p); err != nil || n != 0 {
			t.Fatalf("unread: %d %v", n, err)
		}
	})
	t.Run("unjudged twice", func(t *testing.T) {
		c, ctx := setup(t)
		reviewLandRepo(t, c, ctx, "M")
		out, errb, res := landBG(t, "app-012")
		j := queuedJob(t, c, ctx, out, 1)
		answer(t, c, ctx, j, false, false, "model run failed: rate limited; nothing recorded")
		var again mesh.Job
		meshtest.Until(t, "land to queue the job once more", func() bool {
			m := regexp.MustCompile(`queued (\S+) once more`).FindStringSubmatch(out.String())
			if m == nil {
				return false
			}
			var ok bool
			again, ok, _ = mesh.FindJob(ctx, c, "review", m[1])
			return ok
		})
		if again.Effort != "medium" || again.Commit != j.Commit || again.Text != j.Text || again.ID == j.ID || !again.Go {
			t.Fatalf("queued again %+v, was %+v", again, j)
		}
		answer(t, c, ctx, again, false, false, "model run failed: rate limited; nothing recorded")
		landEnds(t, res, 3, out, errb)
		if !strings.Contains(errb.String(), "refused: job "+again.ID+" (review) recorded nothing") {
			t.Fatalf("land:\n%s\nerr %q", out.String(), errb.String())
		}
		if n, err := mesh.Unread(ctx, c, p); err != nil || n != 0 {
			t.Fatalf("unread: %d %v", n, err)
		}
	})
	t.Run("another failure", func(t *testing.T) {
		c, ctx := setup(t)
		reviewLandRepo(t, c, ctx, "M")
		out, errb, res := landBG(t, "app-012")
		j := queuedJob(t, c, ctx, out, 1)
		answer(t, c, ctx, j, false, false, "lease lost")
		landEnds(t, res, 3, out, errb)
		if !strings.Contains(out.String(), "lease lost\n") || !strings.Contains(errb.String(), "refused: job "+j.ID+" (review) recorded nothing") ||
			strings.Contains(out.String(), "once more") {
			t.Fatalf("land:\n%s\nerr %q", out.String(), errb.String())
		}
	})
}

// A rebase that changes the diff the review read releases the lease, queues
// the review again with the new base, works out what land requires again
// and lands after it.
func TestLandQueuesTheReviewAgainAfterItsRebase(t *testing.T) {
	c, ctx := setup(t)
	rp := reviewLandRepo(t, c, ctx, "M")
	writeFile(t, filepath.Join(rp.wt, "a.txt"), "ONE\ntwo\nthree\n")
	meshtest.Git(t, rp.wt, "commit", "-qam", "lead a")
	reviewed := putReview(t, c, ctx)
	moved := rp.pushOther(t, "a.txt", "one\ntwo\nTHREE\n")
	out, errb, res := landBG(t, "app-012")
	until(t, out, "review: queued ")
	j := queuedJob(t, c, ctx, out, 1)
	for _, w := range []string{"review: on the branch already (ok at t:" + reviewed[:8] + ")\n",
		"review has a result for t:" + reviewed[:8] + " but none for t:" + j.Tree[:8] + ", and the rebase changed the diff\n" +
			"released land.repo\npush origin lead " + j.Commit[:8] + "\nreview: queued " + j.ID + " again (the rebase changed the diff)\n"} {
		if !strings.Contains(out.String(), w) {
			t.Fatalf("land lacks %q:\n%s", w, out.String())
		}
	}
	if j.Text != "base: "+moved || j.Effort != "medium" {
		t.Fatalf("job %+v", j)
	}
	assertNoLeases(t, c, ctx)
	answer(t, c, ctx, j, true, true, "VERDICT: ok")
	landEnds(t, res, 0, out, errb)
	tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
	if tree != j.Tree || !strings.Contains(out.String(), "verified: review ok t:"+tree[:8]+" m:m1; verify ok t:"+tree[:8]+"\n") {
		t.Fatalf("land:\n%s", out.String())
	}
}

// While land waits, a hold refuses it, and so does a message from the
// operator, even one already read; a commit made while it waits for the
// lease refuses under it. Nothing is pushed.
func TestLandStopsForWhatNeedsTheLead(t *testing.T) {
	p, _ := mesh.ParsePath("app-012")
	t.Run("hold", func(t *testing.T) {
		c, ctx := setup(t)
		r := reviewLandRepo(t, c, ctx, "M")
		base := r.originMain(t)
		out, errb, res := landBG(t, "app-012")
		queuedJob(t, c, ctx, out, 1)
		if err := mesh.PutLeadHold(ctx, c, mesh.LeadHold{Path: "app-012", Gate: mesh.GateLand, Text: "wait", By: "operator",
			At: time.Now().UTC().Format(time.RFC3339)}); err != nil {
			t.Fatal(err)
		}
		landEnds(t, res, 3, out, errb)
		if !strings.Contains(errb.String(), "held before land by operator") || r.originMain(t) != base {
			t.Fatalf("land: %q", errb.String())
		}
	})
	t.Run("operator mail, read", func(t *testing.T) {
		c, ctx := setup(t)
		r := reviewLandRepo(t, c, ctx, "M")
		base := r.originMain(t)
		out, errb, res := landBG(t, "app-012")
		queuedJob(t, c, ctx, out, 1)
		sendTo(t, c, ctx, "app-012", mesh.Message{From: "operator", Kind: "message", Text: "two reviews"})
		if _, err := mesh.Fetch(ctx, c, p, true); err != nil {
			t.Fatal(err)
		}
		landEnds(t, res, 3, out, errb)
		// Land reads its mail before every pause of its wait: one that fell
		// between the send and the read refuses the mail as unread. Either
		// way nothing is pushed.
		read := "bmesh: refused: 1 message(s) from the operator since land started; nothing is pushed: " +
			"run bmesh inbox, act on them, then run bmesh land again\n"
		unread := "bmesh: refused: 1 unread message(s) from the operator; run bmesh inbox, act on them, then run bmesh land again\n"
		if e := errb.String(); e != read && e != unread || r.originMain(t) != base {
			t.Fatalf("land: %q", e)
		}
	})
	t.Run("a commit", func(t *testing.T) {
		c, ctx := setup(t)
		r := newLandRepo(t)
		putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
		key, err := mesh.AcquireLease(ctx, c, "land.repo", "app-011", time.Minute, 1, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		base := r.originMain(t)
		out, errb, res := landBG(t, "app-012")
		waitingForLease(t, out)
		was := gitOut(t, "rev-parse", "HEAD")
		r.commitOn(t, "e.txt", "e\n")
		if err := mesh.ReleaseLease(ctx, c, key, "app-011"); err != nil {
			t.Fatal(err)
		}
		landEnds(t, res, 3, out, errb)
		if errb.String() != "bmesh: refused: HEAD moved while land waited ("+gitOut(t, "rev-parse", "HEAD")[:8]+", was "+was[:8]+
			"); nothing is pushed: run bmesh land again\n" || r.originMain(t) != base {
			t.Fatalf("land:\n%s\nerr %q", out.String(), errb.String())
		}
		assertNoLeases(t, c, ctx)
	})
}

// Land run outside a node queues no review and waits on no job.
func TestLandOutsideANodeQueuesNoReview(t *testing.T) {
	c, ctx := setup(t)
	reviewLandRepo(t, c, ctx, "M")
	if _, errs, code := runCLI(t, as("app-012"), "routine", "queue", "review", "--go"); code != 0 {
		t.Fatal(code, errs)
	}
	out, errs, code := runCLI(t, nil, "land", "--spec", "012")
	tree := gitOut(t, "rev-parse", "HEAD~1^{tree}")
	if code != 0 || strings.Contains(out, "review:") || strings.Contains(out, "waiting for job") ||
		!strings.Contains(out, "verified: verify ok t:"+tree[:8]+"\n") {
		t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
	}
	if jobs := unanswered(t, c, ctx); len(jobs) != 1 {
		t.Fatalf("jobs %+v", jobs)
	}
	p, _ := mesh.ParsePath("app-012")
	if evs, err := mesh.History(ctx, c, mesh.EventSubject(p, "land"), time.Time{}); err != nil || len(evs) != 0 {
		t.Fatalf("land events %d %v", len(evs), err)
	}
}
