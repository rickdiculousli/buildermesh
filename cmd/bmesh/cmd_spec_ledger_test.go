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
	// A malformed spec is an error for every command, never counted around;
	// next-id reads main as origin has it.
	if err := os.WriteFile(filepath.Join(repo, "docs", "proposals", "015-bad.md"), []byte("---\nid: 015\nstatus: maybe\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "bad")
	meshtest.Git(t, repo, "push", "origin", "main")
	if _, errs, code := runCLI(t, nil, "spec", "next-id", "--repo", "demo"); code != 1 || !strings.Contains(errs, "015-bad.md") {
		t.Fatalf("malformed: %d %q", code, errs)
	}
}

// spec set --land lands one commit for several ids on main from a bmesh
// worktree, leaving the main checkout's branch alone, and removes the
// worktree; without --land it edits the checkout and commits nothing.
func TestSpecSetLandsOneCommitOnMain(t *testing.T) {
	_, _, repo := ledgerRepoSetup(t)
	// The main checkout is on another branch, so the landing fast-forwards
	// main through `fetch . <branch>:main`.
	meshtest.Git(t, repo, "checkout", "-q", "-b", "side")
	out, errs, code := runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "013,012", "--status", "rejected", "--field", "reason=covered by 010", "--land")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || lines[0] != "012  rejected   S   feature      eng  Thing 012" || lines[1] != "013  rejected   S   feature      eng  Thing 013" ||
		!strings.HasPrefix(lines[2], "landed ") || !strings.HasSuffix(lines[2], " on main: Specs 012, 013: rejected") {
		t.Fatalf("%q", out)
	}
	if gitOut(t, "-C", repo, "rev-parse", "main") != gitOut(t, "-C", repo, "rev-parse", "origin/main") {
		t.Fatal("main not pushed")
	}
	if msg := gitOut(t, "-C", repo, "log", "-1", "--format=%s %an", "main"); msg != "Specs 012, 013: rejected bmesh" {
		t.Fatalf("main's commit: %q", msg)
	}
	if n := gitOut(t, "-C", repo, "rev-list", "--count", "side..main"); n != "1" {
		t.Fatalf("%s commits landed", n)
	}
	if files := gitOut(t, "-C", repo, "show", "--name-only", "--format=", "main"); files != "docs/proposals/012-thing.md\ndocs/proposals/013-thing.md" {
		t.Fatalf("files %q", files)
	}
	if b := gitOut(t, "-C", repo, "rev-parse", "--abbrev-ref", "HEAD"); b != "side" {
		t.Fatalf("checkout moved to %s", b)
	}
	if spec12 := gitOut(t, "-C", repo, "show", "main:docs/proposals/012-thing.md"); !strings.Contains(spec12, "status: rejected\n") || !strings.Contains(spec12, "reason: covered by 010\n") {
		t.Fatalf("012:\n%s", spec12)
	}
	if wts := gitOut(t, "-C", repo, "worktree", "list"); strings.Contains(wts, "spec-") {
		t.Fatalf("worktree left: %s", wts)
	}
	if br := gitOut(t, "-C", repo, "branch", "--list", "demo-spec-*"); br != "" {
		t.Fatalf("branch left: %s", br)
	}
	meshtest.Git(t, repo, "checkout", "-q", "main")
	// One id; a refusal of any id lands none; an edit that changes nothing
	// lands nothing.
	out, _, code = runCLI(t, nil, "spec", "set", "--repo", repo, "--id", "14", "--status", "later", "--land")
	if code != 0 || !strings.HasSuffix(out, " on main: Spec 014: later\n") {
		t.Fatalf("one: %d %q", code, out)
	}
	before := gitOut(t, "-C", repo, "rev-parse", "main")
	if _, errs, code := runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "012,014", "--status", "proposed", "--from", "later", "--land"); code != 1 ||
		errs != "bmesh: spec 012 is rejected, not later; left unchanged\n" {
		t.Fatalf("--from: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "014,099", "--status", "proposed", "--land"); code != 1 ||
		errs != "bmesh: no spec 099 on main in "+repo+"; bmesh spec list shows the ids\n" {
		t.Fatalf("missing: %d %q", code, errs)
	}
	if out, _, code := runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "014", "--status", "later", "--land"); code != 0 || out != "014  later      S   feature      eng  Thing 014\nnothing to land\n" {
		t.Fatalf("no change: %d %q", code, out)
	}
	if gitOut(t, "-C", repo, "rev-parse", "main") != before || gitOut(t, "-C", repo, "status", "--porcelain") != "" {
		t.Fatal("a refused or empty edit landed or left changes")
	}
	// Without --land, several ids are edited in the checkout, all or none.
	if _, _, code := runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "012,014", "--status", "proposed", "--from", "later"); code != 1 ||
		gitOut(t, "-C", repo, "status", "--porcelain") != "" {
		t.Fatalf("--from wrote: %d", code)
	}
	if out, _, code := runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "012,014", "--status", "proposed"); code != 0 ||
		out != "012  proposed   S   feature      eng  Thing 012\n014  proposed   S   feature      eng  Thing 014\n" ||
		gitOut(t, "-C", repo, "rev-parse", "main") != before {
		t.Fatalf("checkout: %d %q", code, out)
	}
	if _, _, code := runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "012,x", "--status", "proposed"); code != 2 {
		t.Fatalf("bad id: %d", code)
	}
}

// spec set --land refuses a main it can't level and a push origin refuses,
// keeping that commit on its branch.
func TestSpecSetLandRefusals(t *testing.T) {
	_, _, repo := ledgerRepoSetup(t)
	origin := gitOut(t, "-C", repo, "remote", "get-url", "origin")
	hook := filepath.Join(origin, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho no pushes today >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	head := gitOut(t, "-C", repo, "rev-parse", "main")
	_, errs, code := runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "012", "--status", "later", "--land")
	if code != 3 || !strings.HasPrefix(errs, "bmesh: refused: push main: git push: ") || !strings.Contains(errs, "no pushes today") ||
		!strings.Contains(errs, "; the spec is committed on branch demo-spec-") {
		t.Fatalf("push: %d %q", code, errs)
	}
	branch := gitOut(t, "-C", repo, "branch", "--list", "--format=%(refname:short)", "demo-spec-*")
	if branch == "" || gitOut(t, "-C", repo, "log", "-1", "--format=%s", branch) != "Spec 012: later" || gitOut(t, "-C", repo, "rev-parse", "main") != head {
		t.Fatalf("branch %q, main moved: %v", branch, gitOut(t, "-C", repo, "rev-parse", "main") != head)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	// Main has a commit origin hasn't, and origin one main hasn't.
	commitFile(t, repo, "b.txt", "b\n", "pushed")
	meshtest.Git(t, repo, "reset", "-q", "--hard", "HEAD~1")
	if err := os.WriteFile(filepath.Join(repo, "c.txt"), []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-qm", "local")
	_, errs, code = runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "012", "--status", "later", "--land")
	if code != 3 || !strings.HasPrefix(errs, "bmesh: refused: main in "+repo+" is not level with origin: ") {
		t.Fatalf("not level: %d %q", code, errs)
	}
}

// pushDuplicate lands 013-other.md, a second spec 013, on main by plain
// git, as an operator by hand might, and returns origin's main.
func pushDuplicate(t *testing.T, repo string) string {
	t.Helper()
	text := "---\nid: 013\ntitle: Other\nkind: feature\nsize: S\njudge: sonnet\nstatus: proposed\nlens: eng\n---\n\n## Proposal\nx\n"
	if err := os.WriteFile(filepath.Join(repo, "docs", "proposals", "013-other.md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "by hand")
	meshtest.Git(t, repo, "push", "origin", "main")
	return gitOut(t, "-C", repo, "rev-parse", "origin/main")
}

const dupRefusal = "refused: 013-other.md and 013-thing.md share id 013; renumber the newer with bmesh spec next-id"

// spec set, with --land or without, refuses a ledger with a duplicate id
// before anything is pushed.
func TestLedgerWritersRefuseADuplicateID(t *testing.T) {
	_, _, repo := ledgerRepoSetup(t)
	head := pushDuplicate(t, repo)
	if _, errs, code := runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "12", "--status", "rejected", "--land"); code != 3 || !strings.Contains(errs, dupRefusal) {
		t.Fatalf("spec set --land: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "spec", "set", "--repo", "demo", "--id", "12", "--status", "accepted"); code != 3 || !strings.Contains(errs, dupRefusal) {
		t.Fatalf("spec set: %d %q", code, errs)
	}
	if got := gitOut(t, "-C", repo, "rev-parse", "origin/main"); got != head {
		t.Fatalf("origin main moved to %s", got)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "docs", "proposals", "012-thing.md")); !strings.Contains(string(b), "status: proposed\n") {
		t.Fatalf("spec 012 changed:\n%s", b)
	}
	// A reader still says it is malformed.
	if _, errs, code := runCLI(t, nil, "spec", "list", "--repo", "demo"); code != 1 || !strings.Contains(errs, "malformed: 013-other.md and 013-thing.md share id 013") {
		t.Fatalf("list: %d %q", code, errs)
	}
}
