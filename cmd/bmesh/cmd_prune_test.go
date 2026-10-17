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
)

// twoBoxes is mac (the admin) and k16 on one hub, both accepting ping,
// prune, forget-worktree and stop-thread, each with its own checkout of app and bmesh's
// worktrees under its own root; c is a client for arranging nodes.
type twoBoxes struct {
	mac, k16 box
	c        *mesh.Client
	ctx      context.Context
	repo     map[string]string // box name -> its app checkout
	wts      map[string]string // box name -> its [runner] worktrees
}

func twoBoxSetup(t *testing.T) *twoBoxes {
	t.Helper()
	atScreenForTest(t)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	url := meshtest.Start(t)
	tb := &twoBoxes{mac: newBox(t, "mac", url), k16: newBox(t, "k16", url), repo: map[string]string{}, wts: map[string]string{}}
	root := t.TempDir()
	for _, b := range []box{tb.mac, tb.k16} {
		tb.wts[b.name] = filepath.Join(root, b.name, "worktrees")
		body, err := os.ReadFile(b.cfg)
		if err != nil {
			t.Fatal(err)
		}
		body = []byte(strings.Replace(string(body), "kind = \"tmux\"\n", "kind = \"tmux\"\nworktrees = \""+tb.wts[b.name]+"\"\n", 1))
		if err := os.WriteFile(b.cfg, body, 0o600); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, b.name, "app")
		meshtest.Git(t, root, "init", "-q", "-b", "main", dir)
		meshtest.CommitFiles(t, dir, map[string]string{"a.txt": "a\n"})
		tb.repo[b.name] = dir
		if err := os.WriteFile(filepath.Join(filepath.Dir(b.cfg), "repos.toml"), []byte("[repos.app]\npath = \""+dir+"\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	accept := "ping,prune,forget-worktree,stop-thread"
	if out, errs, code := tb.mac.run(t, "y\nn\npw\npw\n", "init", "--hosts", "--admin"); code != 0 {
		t.Fatalf("admin init: %d %s %s", code, out, errs)
	}
	codeStr, wait := invite(t, tb.mac, "pw\n", "k16", "--accept", accept)
	if out, errs, code := tb.k16.run(t, "\nn\nn\n"+codeStr+"\n", "init", "--hosts"); code != 0 {
		t.Fatalf("join: %d %s %s", code, out, errs)
	}
	if res, c := wait(); c != 0 {
		t.Fatalf("invite: %d %s", c, res)
	}
	if out, errs, code := tb.mac.run(t, "pw\n", "hosts", "set", "mac", "--accept", accept); code != 0 {
		t.Fatalf("set: %d %s %s", code, out, errs)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	c, err := mesh.Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	tb.c, tb.ctx = c, ctx
	return tb
}

// doneNode gives box a done lead, path, whose worktree on branch is
// clean and merged into main; it returns the worktree.
func (tb *twoBoxes) doneNode(t *testing.T, b box, path, branch string) string {
	t.Helper()
	repo := tb.repo[b.name]
	wt := filepath.Join(tb.wts[b.name], "app", path)
	meshtest.Git(t, repo, "worktree", "add", "-q", "-b", branch, wt)
	meshtest.CommitFiles(t, wt, map[string]string{path + ".txt": "x\n"})
	meshtest.Git(t, repo, "merge", "-q", "--ff-only", branch)
	if err := mesh.PutNode(tb.ctx, tb.c, mesh.Node{Path: path, Role: "lead", Host: b.name, RepoPath: repo, Worktree: wt, Branch: branch}); err != nil {
		t.Fatal(err)
	}
	meshtest.ForceState(t, tb.c, path, mesh.State{State: "done", By: "node"})
	return wt
}

func TestPruneAllBoxes(t *testing.T) {
	tb := twoBoxSetup(t)
	macWT := tb.doneNode(t, tb.mac, "app-001", "bmesh/a")
	k16WT := tb.doneNode(t, tb.k16, "app-002", "bmesh/b")
	serveBox(t, tb.mac)

	out, errs, code := tb.k16.run(t, "", "prune", "--all", "--wait", "20s")
	want := "k16: app " + k16WT + " remove node done, merged\n" +
		"k16: 1 worktrees, 0 branches, 1 to remove, 0 kept\n" +
		"mac: app " + macWT + " remove node done, merged\n" +
		"mac: 1 worktrees, 0 branches, 1 to remove, 0 kept\n"
	if code != 0 || out != want {
		t.Fatalf("dry run: %d %q %s, want %q", code, out, errs, want)
	}
	if !dirExists(macWT) || !dirExists(k16WT) {
		t.Fatal("a dry run removed a worktree")
	}
	out, errs, code = tb.k16.run(t, "", "prune", "--all", "--apply", "--wait", "20s")
	want = "k16: app " + k16WT + " removed node done, merged\n" +
		"k16: 1 worktrees, 0 branches, 1 to remove, 0 kept, 1 removed\n" +
		"mac: app " + macWT + " removed node done, merged\n" +
		"mac: 1 worktrees, 0 branches, 1 to remove, 0 kept, 1 removed\n"
	if code != 0 || out != want {
		t.Fatalf("apply: %d %q %s, want %q", code, out, errs, want)
	}
	if dirExists(macWT) || dirExists(k16WT) {
		t.Fatal("a worktree is still there")
	}
}

func TestPruneWithHostsOffPrunesThisBox(t *testing.T) {
	c, ctx := setup(t)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	root := t.TempDir()
	repo, wts := filepath.Join(root, "app"), filepath.Join(root, "worktrees")
	meshtest.Git(t, root, "init", "-q", "-b", "main", repo)
	meshtest.CommitFiles(t, repo, map[string]string{"a.txt": "a\n"})
	writeRepos(t, "[repos.app]\npath = \""+repo+"\"\n")
	cfg := os.Getenv("BMESH_CONFIG")
	body, _ := os.ReadFile(cfg)
	if err := os.WriteFile(cfg, append(body, "[runner]\nworktrees = \""+wts+"\"\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(wts, "app", "app-001")
	meshtest.Git(t, repo, "worktree", "add", "-q", "-b", "bmesh/a", wt)
	meshtest.CommitFiles(t, wt, map[string]string{"b.txt": "b\n"})
	putNode(t, c, ctx, mesh.Node{Path: "app-001", Worktree: wt})
	stateAt(t, c, "app-001", "done", time.Now())

	out, errs, code := runCLI(t, nil, "prune", "--repo", "app")
	want := testHost + ": app " + wt + " keep unmerged\n" + testHost + ": 1 worktrees, 0 branches, 0 to remove, 1 kept\n"
	if code != 0 || out != want {
		t.Fatalf("%d %q %q, want %q", code, out, errs, want)
	}
	if _, errs, code := runCLI(t, nil, "prune", "--repo", "nope"); code != exitRefused || errs != "bmesh: nope is not in the registry\n" {
		t.Fatalf("unknown repo: %d %q", code, errs)
	}
}
