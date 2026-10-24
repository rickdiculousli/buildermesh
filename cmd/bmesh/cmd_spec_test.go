package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// landedRepo makes a repo with a bare origin and one pushed commit, the one
// a spec will name as merged; it returns the repo, that commit and its tree.
func landedRepo(t *testing.T) (repo, merged, tree string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	repo = filepath.Join(root, "repo")
	meshtest.Git(t, root, "init", "--bare", origin)
	meshtest.Git(t, root, "init", "-b", "main", repo)
	meshtest.Git(t, repo, "remote", "add", "origin", origin)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "work")
	meshtest.Git(t, repo, "push", "origin", "main")
	return repo, gitOut(t, "-C", repo, "rev-parse", "HEAD"), gitOut(t, "-C", repo, "rev-parse", "HEAD^{tree}")
}

// landSpec commits spec 012 with this frontmatter (between the fences) to
// main and pushes it.
func landSpec(t *testing.T, repo, front string) {
	t.Helper()
	dir := filepath.Join(repo, "docs", "proposals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "012-thing.md"), []byte("---\n"+front+"---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "spec")
	meshtest.Git(t, repo, "push", "origin", "main")
}

// landedCommit commits msg (no file changes) in repo and pushes it when
// push; it returns the commit.
func landedCommit(t *testing.T, repo, msg string, push bool) string {
	t.Helper()
	meshtest.Git(t, repo, "commit", "--allow-empty", "-qm", msg)
	if push {
		meshtest.Git(t, repo, "push", "-q", "origin", "main")
	}
	return gitOut(t, "-C", repo, "rev-parse", "HEAD")
}

func TestDoneChecksTheLandedCommitsTrailers(t *testing.T) {
	c, ctx := setup(t)
	repo, _, tree := landedRepo(t)
	t8 := tree[:8]
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: repo, Spec: "012"})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	refused := func(commit, want string) {
		t.Helper()
		_, errs, code := runCLI(t, as("app-012"), "done", "--commit", commit)
		if code != 3 || errs != "bmesh: "+want+"\n" {
			t.Fatalf("code %d err %q, want %q", code, errs, want)
		}
		if got := stateOf(t, c, ctx, "app-012"); got != "working" {
			t.Fatalf("state %s", got)
		}
	}
	verified := "Verified: verify ok t:" + t8 + "; review ok t:" + t8 + " m:claude-opus-5-5"

	local := landedCommit(t, repo, "work\n\nSpec: 012\n"+verified, false)
	refused(local, "not done: "+local[:8]+" is not on origin/main")
	refused("0123456789ab", "not done: 01234567 is not on origin/main")
	meshtest.Git(t, repo, "push", "-q", "origin", "main")
	refused(local, "not done: verified names verify but no result for tree "+t8)

	bare := landedCommit(t, repo, "work\n\nSpec: 012", true)
	refused(bare, "not done: "+bare[:8]+" has no Verified trailer")
	failing := landedCommit(t, repo, "work\n\nSpec: 012\nVerified: verify ok t:"+t8+"; review fail t:"+t8+" m:claude-opus-5-5", true)
	refused(failing, "not done: verified has a failing entry review")
	other := landedCommit(t, repo, "work\n\nSpec: 012\nVerified: verify ok t:0123abcd", true)
	refused(other, "not done: verified entry verify is for tree 0123abcd, merged tree is "+t8)

	put := func(r mesh.Result) {
		t.Helper()
		r.Tree, r.By = tree, "h"
		if err := mesh.PutResult(ctx, c, r); err != nil {
			t.Fatal(err)
		}
	}
	// A failed verify, and a review by another model, do not match.
	put(mesh.Result{Kind: "verify", OK: false})
	put(mesh.Result{Kind: "review", OK: true, Model: "claude-sonnet-5-5"})
	refused(local, "not done: verified names verify but no result for tree "+t8)
	put(mesh.Result{Kind: "verify", OK: true, Args: []string{"--all"}})
	refused(local, "not done: verified names review but no result for tree "+t8)
	put(mesh.Result{Kind: "review", OK: true, Model: "claude-opus-5-5"})

	// The Spec trailer must name the node's spec.
	wrong := landedCommit(t, repo, "work\n\nSpec: 013\n"+verified, true)
	refused(wrong, "not done: "+wrong[:8]+" landed spec 013, not 012")
	none := landedCommit(t, repo, "work\n\n"+verified, true)
	refused(none, "not done: "+none[:8]+" has no Spec trailer")

	if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", local, "--text", "shipped"); code != 0 || out != "done app-012 "+local[:8]+"\n" {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	hist, err := mesh.History(ctx, c, "bm.app-012.done", time.Time{})
	if err != nil || len(hist) != 1 || !strings.Contains(string(hist[0].Data), `"commit":"`+local+`"`) {
		t.Fatalf("%v %v", hist, err)
	}
	if got := stateOf(t, c, ctx, "app-012"); got != "done" {
		t.Fatalf("state %s", got)
	}
	// done again for the same commit, full or abbreviated, is calm and
	// publishes nothing; another commit is still refused.
	for _, again := range []string{local, local[:10]} {
		if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", again); code != 0 || out != "already done at "+local[:8]+"\n" || errs != "" {
			t.Fatalf("done again with %s: %d %q %q", again, code, out, errs)
		}
	}
	if _, errs, code := runCLI(t, as("app-012"), "done", "--commit", "0123456789ab"); code != 3 ||
		errs != "bmesh: refused: app-012 is already done at "+local[:8]+", not 01234567\n" {
		t.Fatalf("done for another commit: %d %q", code, errs)
	}
	if hist, err := mesh.History(ctx, c, "bm.app-012.done", time.Time{}); err != nil || len(hist) != 1 {
		t.Fatalf("done events after the repeats: %d %v", len(hist), err)
	}
	// A lead's done is its own event: no inbox hears of it.
	if hist, err := mesh.History(ctx, c, "bm.inbox.>", time.Time{}); err != nil || len(hist) != 0 {
		t.Fatalf("inbox messages after a lead's done: %d %v", len(hist), err)
	}

	// An executor has no spec to check.
	putNode(t, c, ctx, mesh.Node{Path: "app-012.x1", Role: "executor"})
	if _, errs, code := runCLI(t, as("app-012.x1"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	if _, _, code := runCLI(t, as("app-012.x1"), "done"); code != 2 {
		t.Fatalf("executor done without --commit: %d", code)
	}
	if out, errs, code := runCLI(t, as("app-012.x1"), "done", "--commit", "abc123"); code != 0 {
		t.Fatalf("executor: %d %q %q", code, out, errs)
	}
	if got := stateOf(t, c, ctx, "app-012.x1"); got != "done" {
		t.Fatalf("executor state %s", got)
	}
}

// A lead that landed with the release before names its `NNN: done at`
// record commit: done reads the spec on main, as it did.
func TestDoneTakesAnOldRecordCommit(t *testing.T) {
	c, ctx := setup(t)
	repo, merged, tree := landedRepo(t)
	t8 := tree[:8]
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: repo, Spec: "012"})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	front := "id: 12\nstatus: done\nmerged: " + merged + "\n"
	record := func(front string) string {
		t.Helper()
		landSpec(t, repo, front)
		meshtest.Git(t, repo, "commit", "--amend", "-qm", "012: done at "+merged[:8])
		meshtest.Git(t, repo, "push", "-qf", "origin", "main")
		return gitOut(t, "-C", repo, "rev-parse", "HEAD")
	}
	rec := record(front)
	if _, errs, code := runCLI(t, as("app-012"), "done", "--commit", rec); code != 3 || errs != "bmesh: not done: verified is empty\n" {
		t.Fatalf("empty: %d %q", code, errs)
	}
	rec = record("id: 12\nstatus: building\n")
	if _, errs, code := runCLI(t, as("app-012"), "done", "--commit", rec); code != 3 || errs != "bmesh: not done: status is building\n" {
		t.Fatalf("building: %d %q", code, errs)
	}
	rec = record(front + "verified: verify ok t:" + t8 + "\n")
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "verify", Tree: tree, OK: true, By: "h"}); err != nil {
		t.Fatal(err)
	}
	if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", rec); code != 0 || out != "done app-012 "+rec[:8]+"\n" {
		t.Fatalf("done: %d %q %q", code, out, errs)
	}
}

// A landed commit that is not on main (here: no origin, so local main) is
// refused, whatever its trailers say.
func TestDoneRefusesACommitNotOnMain(t *testing.T) {
	c, ctx := setup(t)
	repo := filepath.Join(t.TempDir(), "repo")
	meshtest.Git(t, filepath.Dir(repo), "init", "-b", "main", repo)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "base")
	meshtest.Git(t, repo, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	tree := gitOut(t, "-C", repo, "write-tree")
	meshtest.Git(t, repo, "commit", "-qm", "side\n\nSpec: 012\nVerified: verify ok t:"+tree[:8])
	side := gitOut(t, "-C", repo, "rev-parse", "side")
	meshtest.Git(t, repo, "checkout", "-q", "main")
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "verify", Tree: tree, OK: true, By: "h"}); err != nil {
		t.Fatal(err)
	}
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: repo, Spec: "012"})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	_, errs, code := runCLI(t, as("app-012"), "done", "--commit", side)
	if code != 3 || errs != "bmesh: not done: "+side[:8]+" is not on main\n" {
		t.Fatalf("code %d err %q", code, errs)
	}
	if got := stateOf(t, c, ctx, "app-012"); got != "working" {
		t.Fatalf("state %s", got)
	}
	// Fast-forwarded onto main, it is done.
	meshtest.Git(t, repo, "merge", "-q", "--ff-only", "side")
	if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", side); code != 0 {
		t.Fatalf("after merge: %d %q %q", code, out, errs)
	}
}

func TestSpecVerifiedComposes(t *testing.T) {
	c, ctx := setup(t)
	repo := gitRepo(t)
	dir := filepath.Join(repo, "docs", "proposals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(dir, "012-thing.md")
	if err := os.WriteFile(specPath, []byte("---\nid: 12\nstatus: accepted\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "spec")
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")

	const none = "bmesh: no results for HEAD's tree; run the routines first\n"
	if _, errs, code := runCLI(t, nil, "spec", "verified"); code != 3 || errs != none {
		t.Fatalf("no results: %d %q", code, errs)
	}
	for _, r := range []mesh.Result{
		{Kind: "verify", OK: true},
		{Kind: "review", OK: false, Model: "claude-opus-5-5"},
	} {
		r.Tree, r.By = tree, "h"
		if err := mesh.PutResult(ctx, c, r); err != nil {
			t.Fatal(err)
		}
	}
	line := "verified: review fail t:" + tree[:8] + " m:claude-opus-5-5; verify ok t:" + tree[:8] + "\n"
	if out, errs, code := runCLI(t, nil, "spec", "verified"); code != 0 || out != line {
		t.Fatalf("print: %d %q %q", code, out, errs)
	}
	// It writes nothing, and --write and --spec are gone.
	if b, _ := os.ReadFile(specPath); string(b) != "---\nid: 12\nstatus: accepted\n---\nbody\n" {
		t.Fatalf("file:\n%s", b)
	}
	for _, gone := range []string{"--write", "--spec"} {
		if _, _, code := runCLI(t, nil, "spec", "verified", gone, "012"); code != 2 {
			t.Fatalf("%s: %d", gone, code)
		}
	}
	if _, _, code := runCLI(t, nil, "spec"); code != 2 {
		t.Fatalf("bare spec: %d", code)
	}

	// --commit composes from that commit's tree, not HEAD's.
	reviewed := gitOut(t, "rev-parse", "HEAD")
	if err := os.WriteFile(specPath, []byte("---\nid: 12\nstatus: done\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "commit", "-qam", "done")
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "review", Tree: tree, OK: true, Model: "claude-opus-5-5", By: "h"}); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := runCLI(t, nil, "spec", "verified"); code != 3 || errs != none {
		t.Fatalf("HEAD has none: %d %q", code, errs)
	}
	line = "verified: review ok t:" + tree[:8] + " m:claude-opus-5-5; verify ok t:" + tree[:8] + "\n"
	if out, errs, code := runCLI(t, nil, "spec", "verified", "--commit", reviewed[:8]); code != 0 || out != line {
		t.Fatalf("--commit: %d %q %q", code, out, errs)
	}
	if _, errs, code := runCLI(t, nil, "spec", "verified", "--commit", "HEAD"); code != 3 || errs != none {
		t.Fatalf("--commit HEAD: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "spec", "verified", "--commit", "nope"); code != 2 || !strings.Contains(errs, `--commit "nope" is not a commit`) {
		t.Fatalf("bad commit: %d %q", code, errs)
	}
}

// stepsRepo is ledgerRepoSetup with spec 015 landed done with four steps
// for the operator in its Needs section: one with a check that passes
// here, one of this box's whose check fails, one of another box's and one
// with no check; and spec 016 landed before trailers, its operator_steps
// recorded with one ticked.
func stepsRepo(t *testing.T) (*mesh.Client, context.Context, string) {
	t.Helper()
	c, ctx, repo := ledgerRepoSetup(t)
	needs := "- set the key check: `test -f a.txt`\n- [" + testHost + "] restart check: `false`\n" +
		"- [otherbox] on the other box check: `true`\n- by hand\n"
	old := `[{"n":1,"box":"","text":"old one","done":"2026-10-17T10:00:00Z k16"},{"n":2,"box":"","text":"old two","done":""}]`
	meshtest.CommitFiles(t, repo, map[string]string{
		"docs/proposals/015-steps.md": "---\nid: 15\ntitle: Steps here\nkind: feature\n" +
			"size: S\njudge: opus\nstatus: done\nlens: eng\n---\n\n## Proposal\nx\n\n## Needs from the operator\n" + needs,
		"docs/proposals/016-old.md": "---\nid: 16\ntitle: Old steps\nstatus: done\nmerged: abc\noperator_steps: " + old +
			"\n---\n\n## Needs from the operator\n- not read\n",
	})
	meshtest.Git(t, repo, "push", "-q", "origin", "main")
	return c, ctx, repo
}

// ticksOnHub is spec 015's ticks on the hub, by step number, after
// checking nothing was committed on origin's main since head.
func ticksOnHub(t *testing.T, c *mesh.Client, ctx context.Context, repo, head string) map[int]mesh.StepTick {
	t.Helper()
	meshtest.Git(t, repo, "fetch", "-q", "origin")
	if now := gitOut(t, "-C", repo, "rev-parse", "origin/main"); now != head {
		t.Fatalf("a commit on main: %s -> %s", head, now)
	}
	ticks, err := mesh.StepTicks(ctx, c, "demo", "015")
	if err != nil {
		t.Fatal(err)
	}
	out := map[int]mesh.StepTick{}
	for _, k := range ticks {
		out[k.N] = k
	}
	return out
}

func TestSpecStepsListAndTick(t *testing.T) {
	c, ctx, repo := stepsRepo(t)
	head := gitOut(t, "-C", repo, "rev-parse", "origin/main")
	out, errs, code := runCLI(t, nil, "spec", "steps", "015", "--repo", "demo")
	if code != 0 || out != "015 Steps here: 4 of 4 open\n1. set the key (check: test -f a.txt)\n2. ["+testHost+"] restart (check: false)\n"+
		"3. [otherbox] on the other box (check: true)\n4. by hand\n" {
		t.Fatalf("list: %d %q %q", code, out, errs)
	}
	if out, _, code := runCLI(t, nil, "spec", "steps", "014", "--repo", "demo"); code != 0 || out != "014 Thing 014: no operator steps\n" {
		t.Fatalf("no steps: %d %q", code, out)
	}
	if out, errs, code := runCLI(t, nil, "spec", "steps", "--repo", "demo"); code != 0 ||
		out != "operator: demo 015 Steps here: 4 of 4 open (bmesh spec steps 015 --repo demo)\n"+
			"operator: demo 016 Old steps: 1 of 2 open (bmesh spec steps 016 --repo demo)\n" {
		t.Fatalf("open: %d %q %q", code, out, errs)
	}

	out, errs, code = runCLI(t, nil, "spec", "steps", "15", "--repo", "demo", "--tick", "4,2", "--note", "did it")
	if code != 0 || out != "ticked 015 4, 2; 2 of 4 open\n" {
		t.Fatalf("tick: %d %q %q", code, out, errs)
	}
	ticks := ticksOnHub(t, c, ctx, repo, head)
	for _, k := range []int{2, 4} {
		if !strings.HasSuffix(ticks[k].Done, "Z "+testHost) || ticks[k].Note != "did it" {
			t.Fatalf("step %d: %+v", k, ticks[k])
		}
	}
	if len(ticks) != 2 {
		t.Fatalf("%+v", ticks)
	}
	if out, errs, code := runCLI(t, nil, "spec", "steps", "--repo", "demo"); code != 0 ||
		!strings.HasPrefix(out, "operator: demo 015 Steps here: 2 of 4 open (bmesh spec steps 015 --repo demo)\n") {
		t.Fatalf("open after: %d %q %q", code, out, errs)
	}
	if out, _, _ := runCLI(t, nil, "spec", "steps", "015", "--repo", "demo"); !strings.Contains(out, "\n4. by hand -- done "+ticks[4].Done+": did it\n") {
		t.Fatalf("list after: %q", out)
	}

	// Ticking a done step again changes nothing.
	out, errs, code = runCLI(t, nil, "spec", "steps", "015", "--repo", "demo", "--tick", "4")
	if code != 0 || out != "step 4 already done "+ticks[4].Done+"\n" {
		t.Fatalf("again: %d %q %q", code, out, errs)
	}
	for _, bad := range [][]string{{"--tick", "9"}, {"--tick", "x"}, {"--tick", "0"}, {"--note", "w"}, {"--tick", "1", "--check"}} {
		if _, errs, code := runCLI(t, nil, append([]string{"spec", "steps", "015", "--repo", "demo"}, bad...)...); code != 2 {
			t.Fatalf("%v: %d %q", bad, code, errs)
		}
	}
	if _, errs, code := runCLI(t, nil, "spec", "steps", "014", "--repo", "demo", "--tick", "1"); code != 3 ||
		!strings.Contains(errs, "spec 014 has no operator steps") {
		t.Fatalf("no steps tick: %d %q", code, errs)
	}
	// A spec landed before trailers keeps its recorded tick and takes new
	// ones on the hub.
	if out, errs, code := runCLI(t, nil, "spec", "steps", "016", "--repo", "demo", "--tick", "1,2"); code != 0 ||
		out != "step 1 already done 2026-10-17T10:00:00Z k16\nticked 016 2; 0 of 2 open\n" {
		t.Fatalf("legacy tick: %d %q %q", code, out, errs)
	}

	// Every step ticked: no line.
	if _, errs, code := runCLI(t, nil, "spec", "steps", "015", "--repo", "demo", "--tick", "1,3"); code != 0 {
		t.Fatal(code, errs)
	}
	if out, _, code := runCLI(t, nil, "spec", "steps", "--repo", "demo"); code != 0 || out != "" {
		t.Fatalf("all done: %d %q", code, out)
	}
	ticksOnHub(t, c, ctx, repo, head)
}

func TestSpecStepsCheck(t *testing.T) {
	c, ctx, repo := stepsRepo(t)
	head := gitOut(t, "-C", repo, "rev-parse", "origin/main")
	out, errs, code := runCLI(t, nil, "spec", "steps", "015", "--repo", "demo", "--check")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != 0 || len(lines) != 4 || lines[0] != "1. "+testHost+": exit 0" || lines[1] != "2. "+testHost+": exit 1" ||
		lines[2] != "3. otherbox: not run: hosts are off here; run bmesh spec steps 015 --check on otherbox" ||
		lines[3] != "ticked 015 1; 3 of 4 open" {
		t.Fatalf("check: %d %q %q", code, out, errs)
	}
	ticks := ticksOnHub(t, c, ctx, repo, head)
	if !strings.HasSuffix(ticks[1].Done, "Z "+testHost) || ticks[1].Note != "check passed" || len(ticks) != 1 {
		t.Fatalf("%+v", ticks)
	}
	// Again: the passed step is done, the failing one ticks nothing.
	out, errs, code = runCLI(t, nil, "spec", "steps", "015", "--repo", "demo", "--check")
	if code != 0 || strings.Contains(out, "1. ") || strings.Contains(out, "ticked") {
		t.Fatalf("again: %d %q %q", code, out, errs)
	}
	if ticks := ticksOnHub(t, c, ctx, repo, head); len(ticks) != 1 {
		t.Fatalf("%+v", ticks)
	}
}

// With hosts on and this box never joined, asking the other boxes fails;
// the pass here is ticked all the same, and the failure is still reported.
func TestSpecStepsCheckTicksHereWhenOtherBoxesFail(t *testing.T) {
	c, ctx, repo := stepsRepo(t)
	head := gitOut(t, "-C", repo, "rev-parse", "origin/main")
	cfg := os.Getenv("BMESH_CONFIG")
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, append(b, "[hosts]\nenabled = true\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runCLI(t, nil, "spec", "steps", "015", "--repo", "demo", "--check")
	if code != 3 || !strings.Contains(errs, "no host key") || !strings.Contains(out, "ticked 015 1; 3 of 4 open\n") {
		t.Fatalf("check: %d %q %q", code, out, errs)
	}
	if ticks := ticksOnHub(t, c, ctx, repo, head); ticks[1].Note != "check passed" || len(ticks) != 1 {
		t.Fatalf("%+v", ticks)
	}
}

// writeProposal rewrites spec id's Proposal in repo as body, keeping its
// frontmatter, commits and pushes it.
func writeProposal(t *testing.T, repo, id, body string) {
	t.Helper()
	p := filepath.Join(repo, "docs", "proposals", id+"-thing.md")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(string(b), "## Proposal\n")
	if err := os.WriteFile(p, []byte(head+"## Proposal\n"+body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "commit", "-qam", "proposal "+id)
	meshtest.Git(t, repo, "push", "-q", "origin", "main")
}

func TestChooseIdeasWarnsOfCommandClashes(t *testing.T) {
	_, _, repo := ledgerRepoSetup(t)
	// 012 and 013 both define bmesh frob; 014 (building) defines land,
	// which main has too; 013 only mentions bmesh land.
	writeProposal(t, repo, "012", "- **`bmesh frob <repo> [--minor]`.** It frobs.")
	writeProposal(t, repo, "013", "- **`bmesh frob <path>|--family F`** lifts a hold.\n- It runs before `bmesh land`, and **`bmesh land` keeps it**.")
	writeProposal(t, repo, "014", "- **`bmesh land [--spec NNN]`**")
	out, errs, code := runCLI(t, nil, "choose-ideas", "demo", "012 and 013 yes")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	// Accepted together, the pair warns once, from the higher id.
	if !strings.Contains(out, "013 Thing 013: proposed -> accepted\nwarning: 013 defines bmesh frob, which 012 (accepted) also defines\nlanded ") ||
		strings.Count(out, "warning:") != 1 {
		t.Fatalf("%q", out)
	}
	// A spec defining a command main has warns, and so does one another
	// spec, building, defines; the reply still lands.
	writeProposal(t, repo, "012", "- **`bmesh land [--spec NNN]`**\n- **`bmesh spec verified [--commit C]`** composes for C.")
	runCLI(t, nil, "spec", "set", "--repo", repo, "--id", "012", "--status", "proposed")
	meshtest.Git(t, repo, "commit", "-qam", "012 proposed")
	meshtest.Git(t, repo, "push", "-q", "origin", "main")
	out, errs, code = runCLI(t, nil, "choose-ideas", "demo", "012 yes")
	want := "warning: 012 defines bmesh land, which main already has\n" +
		"warning: 012 defines bmesh land, which 014 (building) also defines\n" +
		"warning: 012 defines bmesh spec verified, which main already has\nlanded "
	if code != 0 || !strings.Contains(out, want) {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	if !strings.Contains(gitOut(t, "-C", repo, "show", "main:docs/proposals/012-thing.md"), "status: accepted\n") {
		t.Fatal("012 not accepted on main")
	}
}

// next-id claims on the mesh's counter: a claimed id is never handed out
// again, --peek claims nothing, --count claims a block, and a counter
// behind main catches up to it.
func TestSpecNextIDClaimsOnTheMesh(t *testing.T) {
	_, _, repo := ledgerRepoSetup(t) // 012 to 014 on main
	next := func(want string, args ...string) {
		t.Helper()
		out, errs, code := runCLI(t, nil, append([]string{"spec", "next-id", "--repo", "demo"}, args...)...)
		if code != 0 || out != want+"\n" || errs != "" {
			t.Fatalf("next-id %v: %d %q %q, want %s", args, code, out, errs, want)
		}
	}
	next("015")
	next("016") // 015 is claimed, though nothing landed it
	next("017", "--peek")
	next("017", "--peek")
	next("017")
	next("018-020", "--count", "3")
	next("021")
	// A spec landed by plain git, past the counter: the counter catches up.
	writeFile(t, filepath.Join(repo, "docs", "proposals", "030-hand.md"), "---\nid: 030\nstatus: proposed\n---\nx\n")
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "by hand")
	meshtest.Git(t, repo, "push", "origin", "main")
	next("031", "--peek")
	next("031")
	// A worktree of the repo claims on the repo's counter.
	wt := filepath.Join(t.TempDir(), "wt")
	meshtest.Git(t, repo, "worktree", "add", "-q", "-b", "side", wt, "main")
	t.Chdir(wt)
	if out, errs, code := runCLI(t, nil, "spec", "next-id"); code != 0 || out != "032\n" {
		t.Fatalf("from a worktree: %d %q %q", code, out, errs)
	}
	// An unregistered repo, its directory name no KV key could hold, claims
	// from its main checkout and its worktrees on one counter.
	loose := filepath.Join(t.TempDir(), "my repo+x")
	meshtest.Git(t, filepath.Dir(loose), "init", "-q", "-b", "main", loose)
	writeFile(t, filepath.Join(loose, "docs", "proposals", "004-a.md"), "---\nid: 004\nstatus: proposed\n---\nx\n")
	meshtest.Git(t, loose, "add", "-A")
	meshtest.Git(t, loose, "commit", "-q", "-m", "spec")
	looseWT := filepath.Join(t.TempDir(), "a1b2")
	meshtest.Git(t, loose, "worktree", "add", "-q", "-b", "side", looseWT, "main")
	for _, c := range []struct{ dir, want string }{{loose, "005\n"}, {looseWT, "006\n"}} {
		if out, errs, code := runCLI(t, nil, "spec", "next-id", "--repo", c.dir); code != 0 || out != c.want {
			t.Fatalf("unregistered %s: %d %q %q", c.dir, code, out, errs)
		}
	}
	if _, _, code := runCLI(t, nil, "spec", "next-id", "--count", "0"); code != exitUsage {
		t.Fatalf("--count 0: code %d", code)
	}
}

// Two writers on two connections claiming at once never get the same id.
func TestSpecIDClaimsNeverCollide(t *testing.T) {
	c, ctx := setup(t)
	other, err := mesh.Connect(ctx, c.NC.ConnectedUrl(), "", "hub", "other", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	const per = 40
	got := make([][]int, 2)
	var wg sync.WaitGroup
	for i, cl := range []*mesh.Client{c, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < per; k++ {
				n := 1 + k%3
				first, err := mesh.ClaimSpecIDs(ctx, cl, "demo", 7, n)
				if err != nil {
					t.Error(err)
					return
				}
				for id := first; id < first+n; id++ {
					got[i] = append(got[i], id)
				}
			}
		}()
	}
	wg.Wait()
	seen := map[int]bool{}
	for _, ids := range got {
		for _, id := range ids {
			if id <= 7 || seen[id] {
				t.Fatalf("id %d handed out twice or below main's highest", id)
			}
			seen[id] = true
		}
	}
	// Every id from 8 to the counter was handed out once: the blocks are
	// contiguous and leave no hole between them.
	top, err := mesh.PeekSpecID(ctx, c, "demo", 0)
	if err != nil || len(seen) != top-8 {
		t.Fatalf("%d ids, next %d, %v", len(seen), top, err)
	}
}

// With no mesh, next-id reads main and says nothing is claimed.
func TestSpecNextIDWithNoMesh(t *testing.T) {
	repo, _, _ := landedRepo(t)
	writeFile(t, filepath.Join(repo, "docs", "proposals", "065-last.md"), "---\nid: 065\nstatus: proposed\n---\nx\n")
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "spec")
	meshtest.Git(t, repo, "push", "origin", "main")
	cfg := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, cfg, "[mesh]\nnats = \"nats://127.0.0.1:1\"\n[host]\nname = \""+testHost+"\"\n")
	t.Setenv("BMESH_CONFIG", cfg)
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\n")
	out, errs, code := runCLI(t, nil, "spec", "next-id", "--repo", "demo")
	if code != 0 || out != "066\n" || errs != "next-id: no mesh, so 066 is not claimed: another writer may take it\n" {
		t.Fatalf("next-id: %d %q %q", code, out, errs)
	}
	out, errs, code = runCLI(t, nil, "spec", "next-id", "--repo", "demo", "--count", "5")
	if code != 0 || out != "066-070\n" || errs != "next-id: no mesh, so 066-070 are not claimed: another writer may take them\n" {
		t.Fatalf("next-id --count: %d %q %q", code, out, errs)
	}
}
