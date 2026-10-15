package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// executorSetup is setup plus a git repo that is lead demo-012's worktree
// (cwd) and a [runner] worktrees root; it returns the client, the lead's
// worktree and the root.
func executorSetup(t *testing.T) (*mesh.Client, string, string) {
	t.Helper()
	c, ctx := setup(t)
	repo := gitRepo(t)
	runGit(t, repo, "checkout", "-b", "t3code/lead")
	root := filepath.Join(t.TempDir(), "worktrees")
	cfg := os.Getenv("BMESH_CONFIG")
	f, err := os.OpenFile(cfg, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("[runner]\nworktrees = \"" + root + "\"\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	real, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	putNode(t, c, ctx, mesh.Node{Path: "demo-012", Repo: "demo", RepoPath: real, Worktree: real, Branch: "t3code/lead", Spec: "012"})
	return c, real, root
}

// worktrees counts the repo's registered worktrees.
func worktrees(t *testing.T, dir string) int {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "worktree ") {
			n++
		}
	}
	return n
}

func gitLines(t *testing.T, dir string, args ...string) []string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.Fields(string(out))
}

func TestExecutorNewMakesWorktreeBranchNode(t *testing.T) {
	c, repo, root := executorSetup(t)
	ctx := t.Context()
	out, errs, code := runCLI(t, nil, "executor", "new", "--name", "alpha", "--model", "claude-sonnet-5-5")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	first, rest, _ := strings.Cut(out, "\n")
	var got struct{ Path, Branch, Worktree string }
	if err := json.Unmarshal([]byte(first), &got); err != nil {
		t.Fatalf("first line %q: %v", first, err)
	}
	wt := filepath.Join(root, "demo", "demo-012.alpha")
	if got.Path != "demo-012.alpha" || got.Branch != "t3code/lead-alpha" || got.Worktree != wt {
		t.Fatalf("output %+v", got)
	}
	if !strings.Contains(rest, "You are executor demo-012.alpha in buildermesh. Work only in "+wt+" on branch t3code/lead-alpha:") ||
		!strings.Contains(rest, "Every bmesh command carries --node demo-012.alpha.") || !strings.Contains(rest, "starts with `cd "+wt+" &&`") ||
		!strings.Contains(rest, "`cd "+wt+" && bmesh --node demo-012.alpha started --text") || !strings.Contains(rest, "`buildermesh:executor` skill") {
		t.Fatalf("header %q", rest)
	}
	if b := gitLines(t, wt, "rev-parse", "--abbrev-ref", "HEAD"); len(b) != 1 || b[0] != "t3code/lead-alpha" {
		t.Fatalf("worktree branch %v", b)
	}
	if h, l := gitLines(t, wt, "rev-parse", "HEAD"), gitLines(t, repo, "rev-parse", "HEAD"); h[0] != l[0] {
		t.Fatalf("worktree head %v, lead head %v", h, l)
	}
	n, ok, err := mesh.GetNode(ctx, c, "demo-012.alpha")
	if err != nil || !ok {
		t.Fatalf("node %v %v", ok, err)
	}
	if n.Role != "executor" || n.Parent != "demo-012" || n.Branch != "t3code/lead-alpha" || n.Worktree != wt ||
		n.Repo != "demo" || n.RepoPath != repo || n.Host != testHost || n.Model != "claude-sonnet-5-5" || n.StartedAt == "" {
		t.Fatalf("node %+v", n)
	}
	if s := stateOf(t, c, ctx, "demo-012.alpha"); s != "idle" {
		t.Fatalf("state %s", s)
	}
	// The executor finds itself from its worktree, and its inbox exists.
	t.Chdir(wt)
	if out, errs, code := runCLI(t, nil, "whoami"); code != 0 || out != "demo-012.alpha\n" {
		t.Fatalf("whoami: %d %q %q", code, out, errs)
	}
	if _, err := c.JS.Consumer(ctx, mesh.StreamMesh, "inbox-demo-012--alpha"); err != nil {
		t.Fatalf("inbox consumer: %v", err)
	}
	// --json prints only the JSON line; --base picks another start.
	t.Chdir(repo)
	out, errs, code = runCLI(t, nil, "executor", "new", "--name", "beta", "--base", "HEAD~0", "--json")
	if code != 0 || strings.Count(out, "\n") != 1 || !strings.Contains(out, `"path":"demo-012.beta"`) {
		t.Fatalf("--json: %d %q %q", code, out, errs)
	}
	if _, errs, code := runCLI(t, nil, "executor", "new", "--name", "Bad_Name"); code != 2 || !strings.Contains(errs, "name") {
		t.Fatalf("bad name: %d %q", code, errs)
	}
}

func TestExecutorNewRefusesDuplicateName(t *testing.T) {
	c, repo, _ := executorSetup(t)
	if out, errs, code := runCLI(t, nil, "executor", "new", "--name", "alpha", "--json"); code != 0 {
		t.Fatalf("first: %d %q %q", code, out, errs)
	}
	before := gitLines(t, repo, "branch", "--list")
	out, errs, code := runCLI(t, nil, "executor", "new", "--name", "alpha", "--json")
	if code != 3 || out != "" || errs != "bmesh: refused: executor demo-012.alpha exists (state idle); remove it or pick another name\n" {
		t.Fatalf("second: %d %q %q", code, out, errs)
	}
	if after := gitLines(t, repo, "branch", "--list"); strings.Join(after, " ") != strings.Join(before, " ") {
		t.Fatalf("branches changed: %v -> %v", before, after)
	}
	if n := worktrees(t, repo); n != 2 {
		t.Fatalf("worktrees: %d", n)
	}
	// A done executor's name can be reused: the path is reset to idle.
	ctx := t.Context()
	for _, s := range []string{"working", "done"} {
		if _, err := mesh.WriteState(ctx, c, "demo-012.alpha", mesh.State{State: s, By: "node"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, errs, code := runCLI(t, nil, "executor", "remove", "alpha"); code != 0 {
		t.Fatalf("remove: %d %q", code, errs)
	}
	if out, errs, code := runCLI(t, nil, "executor", "new", "--name", "alpha", "--json"); code != 0 {
		t.Fatalf("reuse: %d %q %q", code, out, errs)
	}
	if s := stateOf(t, c, ctx, "demo-012.alpha"); s != "idle" {
		t.Fatalf("state after reuse %s", s)
	}
}

func TestExecutorNewRollsBackOnWorktreeFailure(t *testing.T) {
	c, repo, root := executorSetup(t)
	// The root is a file, so the worktree directory can't be made.
	if err := os.WriteFile(root, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runCLI(t, nil, "executor", "new", "--name", "alpha")
	if code != 1 || out != "" || !strings.Contains(errs, "git worktree add") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	if _, ok, err := mesh.GetNode(t.Context(), c, "demo-012.alpha"); ok || err != nil {
		t.Fatalf("node registered: %v %v", ok, err)
	}
	if b := gitLines(t, repo, "branch", "--list", "t3code/lead-alpha"); len(b) != 0 {
		t.Fatalf("branch left behind: %v", b)
	}
	if n := worktrees(t, repo); n != 1 {
		t.Fatalf("worktree registered: %d", n)
	}
	// A hook that fails after the worktree is made: the worktree and the
	// branch are both removed, and the name can be tried again.
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	hooks := filepath.Join(t.TempDir(), "hooks")
	if err := os.Mkdir(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "config", "core.hooksPath", hooks)
	out, errs, code = runCLI(t, nil, "executor", "new", "--name", "alpha")
	if code != 1 || out != "" || !strings.Contains(errs, "git worktree add") {
		t.Fatalf("hook failure: %d %q %q", code, out, errs)
	}
	if b := gitLines(t, repo, "branch", "--list", "t3code/lead-alpha"); len(b) != 0 {
		t.Fatalf("branch left behind after hook failure: %v", b)
	}
	if n := worktrees(t, repo); n != 1 || exists(filepath.Join(root, "demo", "demo-012.alpha")) {
		t.Fatalf("worktree left behind after hook failure: %d", n)
	}
	runGit(t, repo, "config", "--unset", "core.hooksPath")
	if _, errs, code := runCLI(t, nil, "executor", "new", "--name", "alpha", "--json"); code != 0 {
		t.Fatalf("retry: %d %q", code, errs)
	}
}

func TestExecutorListShowsTree(t *testing.T) {
	c, repo, root := executorSetup(t)
	ctx := t.Context()
	for _, n := range []string{"alpha", "beta"} {
		if _, errs, code := runCLI(t, nil, "executor", "new", "--name", n, "--json"); code != 0 {
			t.Fatalf("new %s: %d %q", n, code, errs)
		}
	}
	putNode(t, c, ctx, mesh.Node{Path: "other-001"})
	if _, errs, code := runCLI(t, as("demo-012.alpha"), "started", "--text", "going"); code != 0 {
		t.Fatal(code, errs)
	}
	if _, errs, code := runCLI(t, as("demo-012"), "send", "demo-012.beta", "hi"); code != 0 {
		t.Fatal(code, errs)
	}
	wt := filepath.Join(root, "demo", "demo-012.alpha")
	if err := os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, wt, "add", "-A")
	runGit(t, wt, "commit", "-m", "b")
	head := gitLines(t, wt, "rev-parse", "--short", "HEAD")[0]
	out, errs, code := runCLI(t, nil, "executor", "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 3 || strings.Join(strings.Fields(lines[0]), " ") != "PATH STATE BRANCH HEAD UNREAD" {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	fa, fb := strings.Fields(lines[1]), strings.Fields(lines[2])
	if fa[0] != "demo-012.alpha" || fa[1] != "working" || fa[2] != "t3code/lead-alpha" || fa[3] != head || fa[4] != "0" ||
		fb[0] != "demo-012.beta" || fb[1] != "idle" || fb[2] != "t3code/lead-beta" || fb[4] != "1" {
		t.Fatalf("%q", out)
	}
	if strings.Contains(out, "other-001") {
		t.Fatalf("another lead's node listed: %q", out)
	}
	// From an executor's worktree the list is still its lead's tree; --json is machine-readable.
	t.Chdir(wt)
	out, _, code = runCLI(t, nil, "executor", "list", "--json")
	if code != 0 || !strings.HasPrefix(out, "[") || !strings.Contains(out, `"path":"demo-012.beta"`) || !strings.Contains(out, `"head":"`+head+`"`) {
		t.Fatalf("%d %q", code, out)
	}
	t.Chdir(repo)
}

func TestExecutorRemoveRefusesUnmerged(t *testing.T) {
	c, repo, root := executorSetup(t)
	ctx := t.Context()
	if _, errs, code := runCLI(t, nil, "executor", "new", "--name", "alpha", "--json"); code != 0 {
		t.Fatalf("new: %d %q", code, errs)
	}
	wt := filepath.Join(root, "demo", "demo-012.alpha")
	if err := os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, wt, "add", "-A")
	runGit(t, wt, "commit", "-m", "b")
	out, errs, code := runCLI(t, nil, "executor", "remove", "alpha")
	if code != 3 || out != "" || errs != "bmesh: refused: branch t3code/lead-alpha is not merged into t3code/lead; merge it or pass --force\n" {
		t.Fatalf("unmerged: %d %q %q", code, out, errs)
	}
	if !exists(wt) {
		t.Fatal("worktree removed on refusal")
	}
	runGit(t, repo, "merge", "--ff-only", "t3code/lead-alpha")
	out, errs, code = runCLI(t, nil, "executor", "remove", "alpha")
	if code != 0 || out != "removed demo-012.alpha\n" {
		t.Fatalf("merged: %d %q %q", code, out, errs)
	}
	if exists(wt) {
		t.Fatal("worktree still there")
	}
	if b := gitLines(t, repo, "branch", "--list", "t3code/lead-alpha"); len(b) != 0 {
		t.Fatalf("branch still there: %v", b)
	}
	if s := stateOf(t, c, ctx, "demo-012.alpha"); s != "done" {
		t.Fatalf("state %s", s)
	}
	if _, errs, code := runCLI(t, nil, "executor", "remove", "alpha"); code != 3 || !strings.Contains(errs, "no executor demo-012.alpha") {
		t.Fatalf("remove twice: %d %q", code, errs)
	}
	// Mail sent to the removed executor does not reach a successor of its name.
	if _, errs, code := runCLI(t, as("demo-012"), "send", "demo-012.alpha", "late"); code != 0 {
		t.Fatal(code, errs)
	}
	if _, errs, code := runCLI(t, nil, "executor", "new", "--name", "alpha", "--json"); code != 0 {
		t.Fatalf("new again: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "executor", "remove", "alpha"); code != 0 {
		t.Fatalf("remove again: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "executor", "new", "--name", "alpha", "--json"); code != 0 {
		t.Fatalf("new a third time: %d %q", code, errs)
	}
	if out, _, code := runCLI(t, as("demo-012.alpha"), "inbox"); code != 0 || out != "" {
		t.Fatalf("successor inherited mail: %d %q", code, out)
	}
	// --force removes an unmerged, dirty executor.
	if _, errs, code := runCLI(t, nil, "executor", "new", "--name", "beta", "--json"); code != 0 {
		t.Fatalf("new beta: %d %q", code, errs)
	}
	wtb := filepath.Join(root, "demo", "demo-012.beta")
	if err := os.WriteFile(filepath.Join(wtb, "c.txt"), []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, wtb, "add", "-A")
	runGit(t, wtb, "commit", "-m", "c")
	if err := os.WriteFile(filepath.Join(wtb, "d.txt"), []byte("d\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := runCLI(t, nil, "executor", "remove", "beta", "--force"); code != 0 {
		t.Fatalf("--force: %d %q", code, errs)
	}
	if exists(wtb) || len(gitLines(t, repo, "branch", "--list", "t3code/lead-beta")) != 0 {
		t.Fatal("--force left the worktree or branch")
	}
}
