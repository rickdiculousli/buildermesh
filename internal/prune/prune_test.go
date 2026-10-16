package prune

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// fixture is a repo, app, with main checked out, bmesh's worktree root
// and a T3-looking root beside it.
type fixture struct {
	root, repo, bmesh, t3, checkouts string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{root: root, repo: filepath.Join(root, "app"), bmesh: filepath.Join(root, "bmesh"),
		t3: filepath.Join(root, "t3"), checkouts: filepath.Join(root, "checkouts")}
	meshtest.Git(t, root, "init", "-q", "-b", "main", f.repo)
	meshtest.CommitFiles(t, f.repo, map[string]string{"a.txt": "a\n"})
	return f
}

// worktree adds a worktree at dir on a new branch (from base, else HEAD)
// with one commit of its own when commit.
func (f fixture) worktree(t *testing.T, dir, branch, base string, commit bool) {
	t.Helper()
	args := []string{"worktree", "add", "-q", "-b", branch, dir}
	if base != "" {
		args = append(args, base)
	}
	meshtest.Git(t, f.repo, args...)
	if commit {
		meshtest.CommitFiles(t, dir, map[string]string{filepath.Base(dir) + ".txt": branch + "\n"})
	}
}

func branches(t *testing.T, repo string) string {
	t.Helper()
	return meshtest.Git(t, repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
}

func byWhat(rows []Row) map[string]Row {
	out := map[string]Row{}
	for _, r := range rows {
		out[r.What] = r
	}
	return out
}

func TestRepoKeepsAndRemoves(t *testing.T) {
	f := newFixture(t)
	wt := func(name string) string { return filepath.Join(f.bmesh, "app", name) }
	// merged: done, clean, landed in main.
	f.worktree(t, wt("app-1.merged"), "bmesh/merged", "", true)
	meshtest.Git(t, f.repo, "merge", "-q", "--ff-only", "bmesh/merged")
	// dirty: done, merged, but with a change.
	f.worktree(t, wt("app-1.dirty"), "bmesh/dirty", "", false)
	os.WriteFile(filepath.Join(wt("app-1.dirty"), "x.txt"), []byte("x\n"), 0o644)
	// working: merged, clean, its node still working.
	f.worktree(t, wt("app-1.working"), "bmesh/working", "", false)
	// The lead's worktree is T3's; its executor's work is merged into the
	// lead's branch only.
	lead := filepath.Join(f.t3, "app-1")
	f.worktree(t, lead, "bmesh/lead", "", true)
	f.worktree(t, wt("app-1.x"), "bmesh/lead-x", "bmesh/lead", true)
	meshtest.Git(t, lead, "merge", "-q", "--ff-only", "bmesh/lead-x")
	// unmerged: done, clean, its commit nowhere else.
	f.worktree(t, wt("app-1.unmerged"), "bmesh/unmerged", "", true)
	// another box's node: no node here.
	f.worktree(t, wt("app-2.mac"), "bmesh/mac", "", false)
	// a routine checkout: not listed.
	f.worktree(t, filepath.Join(f.checkouts, "app", "verify"), "routine", "", false)
	// The sweep: a merged bmesh/* branch, an unmerged one, and a branch
	// that isn't bmesh's.
	meshtest.Git(t, f.repo, "branch", "bmesh/old")
	f.worktree(t, filepath.Join(f.root, "tmp"), "bmesh/wip", "", true)
	meshtest.Git(t, f.repo, "worktree", "remove", filepath.Join(f.root, "tmp"))
	meshtest.Git(t, f.repo, "branch", "other")

	node := func(path, wt string) mesh.Node {
		return mesh.Node{Path: path, Role: "executor", Parent: "app-1", Host: "k16", Worktree: wt}
	}
	c := Checkout{Repo: "app", Path: f.repo, Self: "k16", Worktrees: f.bmesh, Checkouts: f.checkouts, StateDir: t.TempDir(),
		Nodes: []mesh.Node{
			{Path: "app-1", Role: "lead", Host: "k16", Worktree: lead, Branch: "bmesh/lead"},
			node("app-1.merged", wt("app-1.merged")), node("app-1.dirty", wt("app-1.dirty")),
			node("app-1.working", wt("app-1.working")), node("app-1.x", wt("app-1.x")),
			node("app-1.unmerged", wt("app-1.unmerged")),
			{Path: "app-2.mac", Host: "mac", Worktree: wt("app-2.mac")},
		},
		States: map[string]mesh.State{"app-1": {State: "working"}, "app-1.merged": {State: "done"}, "app-1.dirty": {State: "done"},
			"app-1.working": {State: "working"}, "app-1.x": {State: "done"}, "app-1.unmerged": {State: "done"}},
	}
	want := map[string][2]string{
		wt("app-1.merged"):   {Remove, "node done, merged"},
		wt("app-1.dirty"):    {Keep, "uncommitted changes"},
		wt("app-1.working"):  {Keep, "node working"},
		wt("app-1.x"):        {Remove, "node done, merged into bmesh/lead"},
		wt("app-1.unmerged"): {Keep, "unmerged"},
		wt("app-2.mac"):      {Keep, "no bmesh node"},
		lead:                 {Keep, "not bmesh's (T3 cleans its own)"},
		"branch bmesh/old":   {Remove, "merged"},
		"branch bmesh/wip":   {Keep, "unmerged"},
	}
	check := func(rows []Row, removed string) {
		t.Helper()
		got := byWhat(rows)
		if len(got) != len(want) {
			t.Fatalf("rows %v", rows)
		}
		for what, w := range want {
			action := w[0]
			if action == Remove {
				action = removed
			}
			if r := got[what]; r.Action != action || r.Reason != w[1] || r.Repo != "app" {
				t.Errorf("%s: %+v, want %s %s", what, r, action, w[1])
			}
		}
	}

	// A dry run judges and touches nothing.
	beforeWT, beforeBr := meshtest.Git(t, f.repo, "worktree", "list"), branches(t, f.repo)
	rows, err := Repo(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	check(rows, Remove)
	if s := Summary(rows, false); s != "7 worktrees, 2 branches, 3 to remove, 6 kept" {
		t.Fatalf("summary %q", s)
	}
	if meshtest.Git(t, f.repo, "worktree", "list") != beforeWT || branches(t, f.repo) != beforeBr {
		t.Fatal("a dry run touched the repo")
	}

	c.Apply = true
	rows, err = Repo(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	check(rows, Removed)
	if s := Summary(rows, true); s != "7 worktrees, 2 branches, 3 to remove, 6 kept, 3 removed" {
		t.Fatalf("summary %q", s)
	}
	for _, d := range []string{wt("app-1.merged"), wt("app-1.x")} {
		if dirExists(d) {
			t.Errorf("%s kept", d)
		}
	}
	for _, d := range []string{wt("app-1.dirty"), wt("app-1.working"), wt("app-1.unmerged"), wt("app-2.mac"), lead} {
		if !dirExists(d) {
			t.Errorf("%s removed", d)
		}
	}
	got := branches(t, f.repo)
	for _, b := range []string{"bmesh/merged", "bmesh/lead-x", "bmesh/old"} {
		if strings.Contains(got+"\n", b+"\n") {
			t.Errorf("branch %s kept: %s", b, got)
		}
	}
	for _, b := range []string{"bmesh/wip", "bmesh/unmerged", "bmesh/lead", "other", "routine"} {
		if !strings.Contains(got+"\n", b+"\n") {
			t.Errorf("branch %s deleted: %s", b, got)
		}
	}

	// A second run finds nothing more to remove.
	rows, _ = Repo(t.Context(), c)
	if s := Summary(rows, true); s != "5 worktrees, 1 branches, 0 to remove, 6 kept, 0 removed" {
		t.Fatalf("again: %q %v", s, rows)
	}
}

func TestRowString(t *testing.T) {
	r := Row{Repo: "app", What: "/w/x", Action: Failed, Reason: "node done, merged", Err: "git worktree: busy"}
	if s := r.String(); s != "app /w/x failed node done, merged  (failed: git worktree: busy)" {
		t.Fatal(s)
	}
}

func TestRemoveNodeWorktree(t *testing.T) {
	f := newFixture(t)
	wt := filepath.Join(f.bmesh, "app-1")
	f.worktree(t, wt, "bmesh/x", "", true)
	n := mesh.Node{Path: "app-1", Host: "k16", RepoPath: f.repo, Worktree: wt, Branch: "bmesh/x"}

	_, err := RemoveNodeWorktree(t.Context(), n, nil, "mac")
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Text != "refused: worktree "+wt+" is on k16; run it there" {
		t.Fatalf("other host: %v", err)
	}
	_, err = RemoveNodeWorktree(t.Context(), n, nil, "k16")
	if !errors.As(err, &ref) || ref.Text != "refused: branch bmesh/x is not merged into main" {
		t.Fatalf("unmerged: %v", err)
	}
	meshtest.Git(t, f.repo, "merge", "-q", "--ff-only", "bmesh/x")
	lines, err := RemoveNodeWorktree(t.Context(), n, nil, "k16")
	if err != nil || strings.Join(lines, "; ") != "removed worktree "+wt+"; deleted branch bmesh/x" {
		t.Fatalf("%v %v", lines, err)
	}
	if lines, err := RemoveNodeWorktree(t.Context(), mesh.Node{Path: "app-2"}, nil, "k16"); err != nil || lines[0] != "app-2 has no worktree" {
		t.Fatalf("%v %v", lines, err)
	}
}
