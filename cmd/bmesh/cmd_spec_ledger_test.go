package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// ledgerRepoSetup is setup with a repo (bare origin, main pushed) holding
// specs 012 and 013 proposed and 014 building, registered as demo, bmesh
// worktrees under the temp dir; it returns the repo path.
func ledgerRepoSetup(t *testing.T) (*mesh.Client, context.Context, string) {
	t.Helper()
	c, ctx := setup(t)
	repo, _, _ := landedRepo(t)
	dir := filepath.Join(repo, "docs", "proposals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(id, status string) {
		text := "---\nid: " + id + "\ntitle: Thing " + id + "\nkind: feature\nsize: S\njudge: sonnet\nstatus: " + status +
			"\nlens: eng\n---\n\n## Opportunity\nx\n\n## Proposal\nThe change.\n"
		if err := os.WriteFile(filepath.Join(dir, id+"-thing.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("012", "proposed")
	write("013", "proposed")
	write("014", "building")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("not a spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "specs")
	meshtest.Git(t, repo, "push", "origin", "main")
	cfg := os.Getenv("BMESH_CONFIG")
	f, err := os.OpenFile(cfg, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("[runner]\nworktrees = \"" + filepath.Join(t.TempDir(), "wt") + "\"\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\n")
	return c, ctx, repo
}

// pitchRound registers a settled round for demo whose pitch was 013, 012,
// 014 (so rank 2 is 012), a newer round still open whose engineer pitched
// the other way round, which is not a pitch yet, and a newer one still
// that blocked with no pitch.
func pitchRound(t *testing.T, c *mesh.Client, ctx context.Context) {
	t.Helper()
	putNode(t, c, ctx, mesh.Node{Path: "demo-round-1", Role: "round", Repo: "demo"})
	if _, err := mesh.WriteState(ctx, c, "demo-round-1", mesh.State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mesh.WriteState(ctx, c, "demo-round-1", mesh.State{State: "done", By: "daemon",
		Text: "013 Thing 013 (feature, S, sonnet): second\n012 Thing 012 (feature, S, sonnet): first\n014 Thing 014: building"}); err != nil {
		t.Fatal(err)
	}
	putNode(t, c, ctx, mesh.Node{Path: "demo-round-2", Role: "round", Repo: "demo"})
	putNode(t, c, ctx, mesh.Node{Path: "demo-round-2.eng", Role: "engineer", Parent: "demo-round-2", Repo: "demo"})
	if _, err := mesh.WriteState(ctx, c, "demo-round-2", mesh.State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mesh.WriteState(ctx, c, "demo-round-2.eng", mesh.State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mesh.WriteState(ctx, c, "demo-round-2.eng", mesh.State{State: "done", By: "node", Text: "012 first\n013 second"}); err != nil {
		t.Fatal(err)
	}
	// A newer round that blocked before any pitch is passed over.
	putNode(t, c, ctx, mesh.Node{Path: "demo-round-3", Role: "round", Repo: "demo"})
	if _, err := mesh.WriteState(ctx, c, "demo-round-3", mesh.State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mesh.WriteState(ctx, c, "demo-round-3", mesh.State{State: "blocked", By: "node", Text: "eng blocked: did not start: no route"}); err != nil {
		t.Fatal(err)
	}
}

func TestSpecListNextIDSetTemplate(t *testing.T) {
	_, _, repo := ledgerRepoSetup(t)
	out, errs, code := runCLI(t, nil, "spec", "list", "--repo", "demo")
	if code != 0 || out != "012  proposed   S   feature      eng  Thing 012\n013  proposed   S   feature      eng  Thing 013\n014  building   S   feature      eng  Thing 014\n" {
		t.Fatalf("list: %d %q %q", code, out, errs)
	}
	out, _, _ = runCLI(t, nil, "spec", "list", "--repo", repo, "--status", "building", "--json")
	var rows []map[string]string
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 || rows[0]["id"] != "014" || !strings.HasSuffix(rows[0]["path"], "014-thing.md") {
		t.Fatalf("json: %q %v", out, err)
	}
	if _, _, code := runCLI(t, nil, "spec", "list", "--repo", "demo", "--status", "maybe"); code != 2 {
		t.Fatalf("bad status: code %d", code)
	}
	out, _, code = runCLI(t, nil, "spec", "next-id", "--repo", "demo")
	if code != 0 || out != "015\n" {
		t.Fatalf("next-id: %d %q", code, out)
	}
	out, errs, code = runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "12", "--status", "building", "--from", "accepted")
	if code != 1 || !strings.Contains(errs, "spec 012 is proposed, not accepted; left unchanged") {
		t.Fatalf("set from: %d %q %q", code, out, errs)
	}
	out, errs, code = runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "012", "--status", "accepted", "--from", "proposed,later",
		"--field", "judge=opus", "--field", "branch=t3code/ab")
	if code != 0 || out != "012  accepted   S   feature      eng  Thing 012\n" {
		t.Fatalf("set: %d %q %q", code, out, errs)
	}
	b, _ := os.ReadFile(filepath.Join(repo, "docs", "proposals", "012-thing.md"))
	if !strings.Contains(string(b), "judge: opus\n") || !strings.Contains(string(b), "branch: t3code/ab\n---") || !strings.Contains(string(b), "## Proposal\nThe change.\n") {
		t.Fatalf("file:\n%s", b)
	}
	if _, errs, code := runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "99", "--status", "accepted"); code != 1 || !strings.Contains(errs, "no spec 099") {
		t.Fatalf("missing: %d %q", code, errs)
	}
	if _, _, code := runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "12", "--status", "maybe"); code != 2 {
		t.Fatalf("bad status: code %d", code)
	}
	out, _, code = runCLI(t, nil, "spec", "template", "--repo", "demo")
	if code != 0 || !strings.HasPrefix(out, "---\nid: {{id}}\n") || !strings.Contains(out, "## Needs from the operator\n") {
		t.Fatalf("template: %d %q", code, out)
	}
	// A repo's override wins.
	if err := os.MkdirAll(filepath.Join(repo, ".bmesh", "briefs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".bmesh", "briefs", "half-pager.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _, code := runCLI(t, nil, "spec", "template", "--repo", "demo"); code != 0 || out != "mine\n" {
		t.Fatalf("override: %d %q", code, out)
	}
	// A malformed spec is an error for every command, never counted around.
	if err := os.WriteFile(filepath.Join(repo, "docs", "proposals", "015-bad.md"), []byte("---\nid: 015\nstatus: maybe\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := runCLI(t, nil, "spec", "next-id", "--repo", "demo"); code != 1 || !strings.Contains(errs, "015-bad.md") {
		t.Fatalf("malformed: %d %q", code, errs)
	}
}

func TestSpecReviewLandsVerdictsOnMain(t *testing.T) {
	c, ctx, repo := ledgerRepoSetup(t)
	pitchRound(t, c, ctx)
	// The main checkout is on another branch, so the landing fast-forwards
	// main through `fetch . <branch>:main`.
	meshtest.Git(t, repo, "checkout", "-q", "-b", "side")
	out, errs, code := runCLI(t, nil, "choose-ideas", "demo", "012 yes on opus, 013 smaller, 014 no, 2 later, 99 yes, hmm")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	want := []string{
		"012 Thing 012: proposed -> accepted on opus",
		"013 Thing 013: proposed -> adjusting (smaller)",
		"014 Thing 014: building, left unchanged (not proposed, later or adjusting)",
		"unmatched: no position 99 in the pitch; a spec id has three digits (99 yes)",
		"unmatched: hmm",
	}
	// "2 later": rank 2 of the latest round's pitch (013 then 012) is 012,
	// which also said yes; the last move wins and the judge stays.
	want[0] = "012 Thing 012: proposed -> later on opus"
	for i, w := range want {
		if i >= len(lines) || lines[i] != w {
			t.Fatalf("line %d: %q, want %q\n%s", i, lines[min(i, len(lines)-1)], w, out)
		}
	}
	if !strings.HasPrefix(lines[len(lines)-1], "landed ") || !strings.HasSuffix(lines[len(lines)-1], ": Review: 012 later, 013 adjusting") {
		t.Fatalf("last line %q", lines[len(lines)-1])
	}
	// main moved and was pushed; the side checkout was not touched.
	if gitOut(t, "-C", repo, "rev-parse", "main") != gitOut(t, "-C", repo, "rev-parse", "origin/main") {
		t.Fatal("main not pushed")
	}
	if msg := gitOut(t, "-C", repo, "log", "-1", "--format=%s", "main"); msg != "Review: 012 later, 013 adjusting" {
		t.Fatalf("main's commit: %q", msg)
	}
	if b := gitOut(t, "-C", repo, "rev-parse", "--abbrev-ref", "HEAD"); b != "side" {
		t.Fatalf("checkout moved to %s", b)
	}
	spec12 := gitOut(t, "-C", repo, "show", "main:docs/proposals/012-thing.md")
	if !strings.Contains(spec12, "status: later\n") || !strings.Contains(spec12, "judge: opus\n") || strings.Contains(spec12, "Adjust (") {
		t.Fatalf("012:\n%s", spec12)
	}
	spec13 := gitOut(t, "-C", repo, "show", "main:docs/proposals/013-thing.md")
	if !strings.Contains(spec13, "status: adjusting\n") || !strings.Contains(spec13, "## Proposal\nAdjust (20") || !strings.Contains(spec13, "): smaller\nThe change.") {
		t.Fatalf("013:\n%s", spec13)
	}
	if wts := gitOut(t, "-C", repo, "worktree", "list"); strings.Contains(wts, "review-") {
		t.Fatalf("worktree left: %s", wts)
	}
	if br := gitOut(t, "-C", repo, "branch", "--list", "demo-review-*"); br != "" {
		t.Fatalf("branch left: %s", br)
	}
	// A second review: a rejection carries its reason; a second instruction
	// adds a second note; a number beyond the pitch and the ledger is reported.
	out, errs, code = runCLI(t, nil, "choose-ideas", repo, "013 only the CLI part; 012 no too broad, 4 yes")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	if !strings.Contains(out, "013 Thing 013: adjusting -> adjusting (only the CLI part)\n") || !strings.Contains(out, "012 Thing 012: later -> rejected (too broad)\n") ||
		!strings.Contains(out, "unmatched: no position 4 in the pitch; a spec id has three digits (4 yes)\n") {
		t.Fatalf("%q", out)
	}
	spec13 = gitOut(t, "-C", repo, "show", "main:docs/proposals/013-thing.md")
	if strings.Count(spec13, "Adjust (") != 2 || !strings.Contains(spec13, "): only the CLI part\nAdjust (") {
		t.Fatalf("013:\n%s", spec13)
	}
	if spec12 := gitOut(t, "-C", repo, "show", "main:docs/proposals/012-thing.md"); !strings.Contains(spec12, "status: rejected\nreason: too broad\n") &&
		!strings.Contains(spec12, "reason: too broad\n") {
		t.Fatalf("012:\n%s", spec12)
	}
	// Positions stay what the pitch said after specs move: 1 is still 013.
	out, _, code = runCLI(t, nil, "choose-ideas", "demo", "1 on opus, 3 yes")
	if code != 0 || !strings.Contains(out, "013 Thing 013: adjusting -> adjusting on opus\n") || !strings.Contains(out, "014 Thing 014: building, left unchanged") {
		t.Fatalf("%d %q", code, out)
	}
	// A judge note alone on an adjusting spec sets the judge and adds no note.
	out, _, code = runCLI(t, nil, "choose-ideas", "demo", "013 on fable")
	if code != 0 || !strings.Contains(out, "013 Thing 013: adjusting -> adjusting on fable\n") {
		t.Fatalf("%d %q", code, out)
	}
	spec13 = gitOut(t, "-C", repo, "show", "main:docs/proposals/013-thing.md")
	if !strings.Contains(spec13, "judge: fable\n") || strings.Count(spec13, "Adjust (") != 2 {
		t.Fatalf("013:\n%s", spec13)
	}
	// Nothing matched: nothing to land, no commit.
	before := gitOut(t, "-C", repo, "rev-parse", "main")
	out, _, code = runCLI(t, nil, "choose-ideas", "demo", "99 yes")
	if code != 0 || !strings.HasSuffix(out, "nothing to land\n") || gitOut(t, "-C", repo, "rev-parse", "main") != before {
		t.Fatalf("%d %q", code, out)
	}
	if _, _, code := runCLI(t, nil, "choose-ideas", "demo"); code != 2 {
		t.Fatalf("usage: %d", code)
	}
}

func TestSpecReviewOnMainCheckoutMergesFastForward(t *testing.T) {
	_, _, repo := ledgerRepoSetup(t)
	out, errs, code := runCLI(t, nil, "choose-ideas", "demo", "012 reject: dup, 1 yes")
	if code != 0 || !strings.Contains(out, "012 Thing 012: proposed -> rejected (dup)\n") || !strings.HasPrefix(out, "note: no settled round with a pitch, so numbers are spec ids only\n") ||
		!strings.Contains(out, "unmatched: no spec 1 (1 yes)\n") {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "docs", "proposals", "012-thing.md")); !strings.Contains(string(b), "status: rejected\nreason: dup\n") &&
		!strings.Contains(string(b), "reason: dup\n") {
		t.Fatalf("checkout not fast-forwarded:\n%s", b)
	}
	if gitOut(t, "-C", repo, "rev-parse", "main") != gitOut(t, "-C", repo, "rev-parse", "origin/main") {
		t.Fatal("main not pushed")
	}
}
