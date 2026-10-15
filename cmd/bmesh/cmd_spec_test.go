package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// landedRepo makes a repo with a bare origin and one pushed commit, the one
// a spec will name as merged; it returns the repo, that commit and its tree.
func landedRepo(t *testing.T) (repo, merged, tree string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	repo = filepath.Join(root, "repo")
	runGit(t, root, "init", "--bare", origin)
	runGit(t, root, "init", "-b", "main", repo)
	runGit(t, repo, "remote", "add", "origin", origin)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "work")
	runGit(t, repo, "push", "origin", "main")
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
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "spec")
	runGit(t, repo, "push", "origin", "main")
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
	refused("not done: merged 0000000000000000000000000000000000000000 is not a commit")

	landSpec(t, repo, front+verified)
	put(mesh.Result{Kind: "review", OK: true, Model: "claude-opus-5-5"})
	if out, errs, code := runCLI(t, as("app-012"), "done", "--commit", merged, "--text", "shipped"); code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	hist, err := mesh.History(ctx, c, "bm.app-012.done", time.Time{})
	if err != nil || len(hist) != 1 || !strings.Contains(string(hist[0].Data), `"commit":"`+merged+`"`) {
		t.Fatalf("%v %v", hist, err)
	}
	if got := stateOf(t, c, ctx, "app-012"); got != "done" {
		t.Fatalf("state %s", got)
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
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "spec")
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
}
