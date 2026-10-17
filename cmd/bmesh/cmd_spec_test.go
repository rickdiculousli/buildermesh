package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/spec"
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

func TestDoneRequiresVerifiedMatchingResults(t *testing.T) {
	c, ctx := setup(t)
	repo, merged, tree := landedRepo(t)
	t8 := tree[:8]
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: repo, Spec: "012"})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	front := "id: 12\nstatus: done\nmerged: " + merged + "\n"
	refused := func(want string) {
		t.Helper()
		_, errs, code := runCLI(t, as("app-012"), "done", "--commit", merged)
		if code != 3 || errs != "bmesh: "+want+"\n" {
			t.Fatalf("code %d err %q, want %q", code, errs, want)
		}
		if got := stateOf(t, c, ctx, "app-012"); got != "working" {
			t.Fatalf("state %s", got)
		}
	}

	landSpec(t, repo, front)
	refused("not done: verified is empty")
	landSpec(t, repo, front+"verified:\n")
	refused("not done: verified is empty")

	verified := "verified: verify ok t:" + t8 + "; review ok t:" + t8 + " m:claude-opus-5-5\n"
	landSpec(t, repo, front+verified)
	refused("not done: verified names verify but no result for tree " + t8)

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
	refused("not done: verified names verify but no result for tree " + t8)
	put(mesh.Result{Kind: "verify", OK: true, Args: []string{"--all"}})
	refused("not done: verified names review but no result for tree " + t8)

	landSpec(t, repo, front+"verified: verify ok t:0123abcd\n")
	refused("not done: verified entry verify is for tree 0123abcd, merged tree is " + t8)
	landSpec(t, repo, "id: 12\nstatus: done\nmerged: 0000000000000000000000000000000000000000\nverified: verify ok t:"+t8+"\n")
	refused("not done: merged 0000000000000000000000000000000000000000 is not on origin/main")

	// R17: a failing entry is refused before any result is looked up.
	landSpec(t, repo, front+"verified: verify ok t:"+t8+"; review fail t:"+t8+" m:claude-opus-5-5\n")
	refused("not done: verified has a failing entry review")

	landSpec(t, repo, front+verified)
	put(mesh.Result{Kind: "review", OK: true, Model: "claude-opus-5-5"})
	if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", merged, "--text", "shipped"); code != 0 || out != "done app-012 "+merged[:8]+"\n" {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	hist, err := mesh.History(ctx, c, "bm.app-012.done", time.Time{})
	if err != nil || len(hist) != 1 || !strings.Contains(string(hist[0].Data), `"commit":"`+merged+`"`) {
		t.Fatalf("%v %v", hist, err)
	}
	if got := stateOf(t, c, ctx, "app-012"); got != "done" {
		t.Fatalf("state %s", got)
	}
	// done again for the same commit, full or abbreviated, is calm and
	// publishes nothing; another commit is still refused.
	for _, again := range []string{merged, merged[:10]} {
		if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", again); code != 0 || out != "already done at "+merged[:8]+"\n" || errs != "" {
			t.Fatalf("done again with %s: %d %q %q", again, code, out, errs)
		}
	}
	if _, errs, code := runCLI(t, as("app-012"), "done", "--commit", "0123456789ab"); code != 3 ||
		errs != "bmesh: refused: app-012 is already done at "+merged[:8]+", not 01234567\n" {
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

// A merged commit that is not on main (here: no origin, so local main) is
// refused, whatever the verified line says.
func TestDoneRefusesMergedNotOnMain(t *testing.T) {
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
	meshtest.Git(t, repo, "commit", "-am", "side")
	side, tree := gitOut(t, "-C", repo, "rev-parse", "side"), gitOut(t, "-C", repo, "rev-parse", "side^{tree}")
	meshtest.Git(t, repo, "checkout", "main")
	dir := filepath.Join(repo, "docs", "proposals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	front := "---\nid: 12\nstatus: done\nmerged: " + side + "\nverified: verify ok t:" + tree[:8] + "\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "012-thing.md"), []byte(front), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "spec")
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "verify", Tree: tree, OK: true, By: "h"}); err != nil {
		t.Fatal(err)
	}
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: repo, Spec: "012"})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	_, errs, code := runCLI(t, as("app-012"), "done", "--commit", side)
	if code != 3 || errs != "bmesh: not done: merged "+side+" is not on main\n" {
		t.Fatalf("code %d err %q", code, errs)
	}
	if got := stateOf(t, c, ctx, "app-012"); got != "working" {
		t.Fatalf("state %s", got)
	}
	// Merged into main, it is done.
	meshtest.Git(t, repo, "merge", "--no-edit", "side")
	if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", side); code != 0 {
		t.Fatalf("after merge: %d %q %q", code, out, errs)
	}
}

func TestSpecVerifiedComposesAndWrites(t *testing.T) {
	c, ctx := setup(t)
	repo := gitRepo(t)
	dir := filepath.Join(repo, "docs", "proposals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(dir, "012-thing.md")
	if err := os.WriteFile(specPath, []byte("---\nid: 12\nstatus: building\n---\nbody\n"), 0o644); err != nil {
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
	if _, _, code := runCLI(t, nil, "spec", "verified", "--write"); code != 2 {
		t.Fatalf("--write without a spec: %d", code)
	}
	if _, errs, code := runCLI(t, nil, "spec", "verified", "--write", "--spec", "099"); code != 3 || !strings.Contains(errs, "099") {
		t.Fatalf("missing spec: %d %q", code, errs)
	}
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: repo, Spec: "012"})
	out, errs, code := runCLI(t, as("app-012"), "spec", "verified", "--write")
	if code != 0 || out != line+"wrote "+specPath+"\n" {
		t.Fatalf("write: %d %q %q", code, out, errs)
	}
	b, _ := os.ReadFile(specPath)
	if want := "---\nid: 12\nstatus: building\n" + line + "---\nbody\n"; string(b) != want {
		t.Fatalf("file:\n%s", b)
	}
	if _, _, code := runCLI(t, nil, "spec"); code != 2 {
		t.Fatalf("bare spec: %d", code)
	}

	// --commit composes from that commit's tree, not HEAD's, and writes into
	// this worktree's spec file.
	reviewed := gitOut(t, "rev-parse", "HEAD")
	meshtest.Git(t, repo, "commit", "-qam", "verified")
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "review", Tree: tree, OK: true, Model: "claude-opus-5-5", By: "h"}); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := runCLI(t, nil, "spec", "verified"); code != 3 || errs != none {
		t.Fatalf("HEAD has none: %d %q", code, errs)
	}
	line = "verified: review ok t:" + tree[:8] + " m:claude-opus-5-5; verify ok t:" + tree[:8] + "\n"
	out, errs, code = runCLI(t, nil, "spec", "verified", "--spec", "012", "--commit", reviewed[:8], "--write")
	if code != 0 || out != line+"wrote "+specPath+"\n" {
		t.Fatalf("--commit: %d %q %q", code, out, errs)
	}
	if b, _ := os.ReadFile(specPath); !strings.Contains(string(b), line) {
		t.Fatalf("file:\n%s", b)
	}
	if _, errs, code := runCLI(t, nil, "spec", "verified", "--commit", "HEAD"); code != 3 || errs != none {
		t.Fatalf("--commit HEAD: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "spec", "verified", "--commit", "nope"); code != 2 || !strings.Contains(errs, `--commit "nope" is not a commit`) {
		t.Fatalf("bad commit: %d %q", code, errs)
	}
}

// stepsRepo is ledgerRepoSetup with spec 015 landed done with four steps
// for the operator: one with a check that passes here, one of this box's
// whose check fails, one of another box's and one with no check.
func stepsRepo(t *testing.T) (*mesh.Client, context.Context, string) {
	t.Helper()
	c, ctx, repo := ledgerRepoSetup(t)
	steps := `[{"n":1,"box":"","text":"set the key","check":"test -f a.txt","done":""},` +
		`{"n":2,"box":"` + testHost + `","text":"restart","check":"false","done":""},` +
		`{"n":3,"box":"otherbox","text":"on the other box","check":"true","done":""},` +
		`{"n":4,"box":"","text":"by hand","done":""}]`
	meshtest.CommitFiles(t, repo, map[string]string{"docs/proposals/015-steps.md": "---\nid: 15\ntitle: Steps here\nkind: feature\n" +
		"size: S\njudge: opus\nstatus: done\nlens: eng\noperator_steps: " + steps + "\n---\n\nbody\n"})
	meshtest.Git(t, repo, "push", "-q", "origin", "main")
	return c, ctx, repo
}

// stepsOnOrigin is spec 015's steps on origin's main.
func stepsOnOrigin(t *testing.T, repo string) []spec.Step {
	t.Helper()
	meshtest.Git(t, repo, "fetch", "-q", "origin")
	s, err := spec.Parse(gitOut(t, "-C", repo, "show", "origin/main:docs/proposals/015-steps.md"), "015-steps.md")
	if err != nil {
		t.Fatal(err)
	}
	steps, err := s.Steps()
	if err != nil || len(steps) != 4 {
		t.Fatalf("%+v %v", steps, err)
	}
	return steps
}

func TestLandCopiesOperatorSteps(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	writeFile(t, filepath.Join(r.wt, "docs", "proposals", "012-thing.md"), "---\nid: 12\nstatus: building\nbranch: lead\n---\n\n"+
		"## Proposal\nx\n\n## Needs from the operator\nOn the Mac:\n- [ricks-macbook] run `bmesh update` check: `bmesh version`\n- answer the list\n")
	meshtest.Git(t, r.wt, "commit", "-qam", "needs")
	out, errs, code := runCLI(t, as("app-012"), "land")
	if code != 0 || !strings.Contains(out, "operator steps: 2 (bmesh spec steps 012)\n") {
		t.Fatalf("land: %d %q %q", code, out, errs)
	}
	want := `operator_steps: [{"n":1,"box":"ricks-macbook","text":"On the Mac: run ` + "`bmesh update`" + `","check":"bmesh version","done":""},` +
		`{"n":2,"box":"","text":"On the Mac: answer the list","done":""}]` + "\n"
	if got := r.specOnOrigin(t); !strings.Contains(got, "status: done\n") || !strings.Contains(got, want) {
		t.Fatalf("spec on origin:\n%s", got)
	}
}

// A spec landed again whose Needs section is now empty drops its steps.
func TestLandDropsStepsTheNeedsNoLongerList(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	writeFile(t, filepath.Join(r.wt, "docs", "proposals", "012-thing.md"), "---\nid: 12\nstatus: building\nbranch: lead\n"+
		`operator_steps: [{"n":1,"box":"","text":"old","done":""}]`+"\n---\n\n## Needs from the operator\nNone.\n")
	meshtest.Git(t, r.wt, "commit", "-qam", "needs")
	if out, errs, code := runCLI(t, as("app-012"), "land"); code != 0 || strings.Contains(out, "operator steps:") {
		t.Fatalf("land: %d %q %q", code, out, errs)
	}
	if got := r.specOnOrigin(t); !strings.Contains(got, "operator_steps: []\n") {
		t.Fatalf("spec on origin:\n%s", got)
	}
}

func TestSpecStepsListAndTick(t *testing.T) {
	_, _, repo := stepsRepo(t)
	out, errs, code := runCLI(t, nil, "spec", "steps", "015", "--repo", "demo")
	if code != 0 || out != "015 Steps here: 4 of 4 open\n1. set the key (check: test -f a.txt)\n2. ["+testHost+"] restart (check: false)\n"+
		"3. [otherbox] on the other box (check: true)\n4. by hand\n" {
		t.Fatalf("list: %d %q %q", code, out, errs)
	}
	if out, _, code := runCLI(t, nil, "spec", "steps", "014", "--repo", "demo"); code != 0 || out != "014 Thing 014: no operator steps\n" {
		t.Fatalf("no steps: %d %q", code, out)
	}
	if out, errs, code := runCLI(t, nil, "spec", "steps", "--repo", "demo"); code != 0 ||
		out != "operator: demo 015 Steps here: 4 of 4 open (bmesh spec steps 015 --repo demo)\n" {
		t.Fatalf("open: %d %q %q", code, out, errs)
	}

	out, errs, code = runCLI(t, nil, "spec", "steps", "15", "--repo", "demo", "--tick", "4,2", "--note", "did it")
	head := gitOut(t, "-C", repo, "rev-parse", "origin/main")
	if code != 0 || !strings.HasPrefix(out, "landed ") || !strings.HasSuffix(out, " on main: Steps: 015 4, 2 done; 2 of 4 open\n") {
		t.Fatalf("tick: %d %q %q", code, out, errs)
	}
	if msg := gitOut(t, "-C", repo, "log", "-1", "--format=%s", "origin/main"); msg != "Steps: 015 4, 2 done" {
		t.Fatalf("commit %q", msg)
	}
	steps := stepsOnOrigin(t, repo)
	for _, k := range []int{1, 3} {
		if steps[k].Done == "" || !strings.HasSuffix(steps[k].Done, "Z "+testHost) || steps[k].Note != "did it" {
			t.Fatalf("step %d: %+v", k+1, steps[k])
		}
	}
	if steps[0].Done != "" || steps[2].Done != "" {
		t.Fatalf("%+v", steps)
	}
	if out, errs, code := runCLI(t, nil, "spec", "steps", "--repo", "demo"); code != 0 ||
		out != "operator: demo 015 Steps here: 2 of 4 open (bmesh spec steps 015 --repo demo)\n" {
		t.Fatalf("open after: %d %q %q", code, out, errs)
	}

	// Ticking a done step again lands nothing.
	head = gitOut(t, "-C", repo, "rev-parse", "origin/main")
	out, errs, code = runCLI(t, nil, "spec", "steps", "015", "--repo", "demo", "--tick", "4")
	if code != 0 || out != "step 4 already done "+steps[3].Done+"\n" {
		t.Fatalf("again: %d %q %q", code, out, errs)
	}
	meshtest.Git(t, repo, "fetch", "-q", "origin")
	if now := gitOut(t, "-C", repo, "rev-parse", "origin/main"); now != head {
		t.Fatalf("landed again: %s -> %s", head, now)
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

	// Every step ticked: no line.
	if _, errs, code := runCLI(t, nil, "spec", "steps", "015", "--repo", "demo", "--tick", "1,3"); code != 0 {
		t.Fatal(code, errs)
	}
	if out, _, code := runCLI(t, nil, "spec", "steps", "--repo", "demo"); code != 0 || out != "" {
		t.Fatalf("all done: %d %q", code, out)
	}
}

func TestSpecStepsCheck(t *testing.T) {
	_, _, repo := stepsRepo(t)
	out, errs, code := runCLI(t, nil, "spec", "steps", "015", "--repo", "demo", "--check")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != 0 || len(lines) != 4 || lines[0] != "1. "+testHost+": exit 0" || lines[1] != "2. "+testHost+": exit 1" ||
		lines[2] != "3. otherbox: not run: hosts are off here; run bmesh spec steps 015 --check on otherbox" ||
		!strings.HasSuffix(lines[3], " on main: Steps: 015 1 done; 3 of 4 open") {
		t.Fatalf("check: %d %q %q", code, out, errs)
	}
	steps := stepsOnOrigin(t, repo)
	if !strings.HasSuffix(steps[0].Done, "Z "+testHost) || steps[0].Note != "check passed" || steps[1].Done != "" || steps[2].Done != "" {
		t.Fatalf("%+v", steps)
	}
	// Again: the passed step is done, the failing one lands nothing.
	head := gitOut(t, "-C", repo, "rev-parse", "origin/main")
	out, errs, code = runCLI(t, nil, "spec", "steps", "015", "--repo", "demo", "--check")
	if code != 0 || strings.Contains(out, "1. ") || strings.Contains(out, "landed") {
		t.Fatalf("again: %d %q %q", code, out, errs)
	}
	meshtest.Git(t, repo, "fetch", "-q", "origin")
	if now := gitOut(t, "-C", repo, "rev-parse", "origin/main"); now != head {
		t.Fatalf("landed again: %s -> %s", head, now)
	}
}

// With hosts on and this box never joined, asking the other boxes fails;
// the pass here lands all the same, and the failure is still reported.
func TestSpecStepsCheckLandsHereWhenOtherBoxesFail(t *testing.T) {
	_, _, repo := stepsRepo(t)
	cfg := os.Getenv("BMESH_CONFIG")
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, append(b, "[hosts]\nenabled = true\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runCLI(t, nil, "spec", "steps", "015", "--repo", "demo", "--check")
	if code != 3 || !strings.Contains(errs, "no host key") || !strings.Contains(out, " on main: Steps: 015 1 done; 3 of 4 open\n") {
		t.Fatalf("check: %d %q %q", code, out, errs)
	}
	if steps := stepsOnOrigin(t, repo); steps[0].Note != "check passed" || steps[2].Done != "" {
		t.Fatalf("%+v", steps)
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
