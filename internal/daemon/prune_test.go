package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// pruneSetup is levelSetup with the test host accepting prune and
// forget-worktree and bmesh's worktrees under wts; the loop isn't started.
func pruneSetup(t *testing.T) (l *levelFixture, wts string) {
	t.Helper()
	l = levelSetup(t, false)
	r := l.roster
	for i := range r.Hosts {
		if r.Hosts[i].Name == testHost {
			r.Hosts[i].Accept = []string{"ping", "level", "prune", "forget-worktree"}
		}
	}
	r.Sign(hostKey("admin"))
	if err := hosts.SaveLocal(l.d.StateDir, hosts.Local{Pinned: hostPub("admin"), Roster: r}); err != nil {
		t.Fatal(err)
	}
	l.roster = r
	wts = filepath.Join(t.TempDir(), "worktrees")
	l.d.Cfg.Runner.Worktrees = wts
	return l, wts
}

// worktree adds a worktree of the box's checkout at dir on a new branch,
// with a commit of its own.
func (l *levelFixture) worktree(t *testing.T, dir, branch string) {
	t.Helper()
	meshtest.Git(t, l.box, "worktree", "add", "-q", "-b", branch, dir)
	meshtest.CommitFiles(t, dir, map[string]string{filepath.Base(dir) + ".txt": "x\n"})
}

func (l *levelFixture) node(t *testing.T, n mesh.Node, state string) {
	t.Helper()
	if n.Role == "" {
		n.Role = "executor"
	}
	if err := mesh.PutNode(l.ctx, l.c, n); err != nil {
		t.Fatal(err)
	}
	if state != "" {
		meshtest.ForceState(t, l.c, n.Path, mesh.State{State: state, By: "node"})
	}
}

func TestPruneJobDryRunThenApply(t *testing.T) {
	l, wts := pruneSetup(t)
	merged, open := filepath.Join(wts, "plug", "plug-1.a"), filepath.Join(wts, "plug", "plug-1.b")
	l.worktree(t, merged, "bmesh/a")
	meshtest.Git(t, l.box, "merge", "-q", "--ff-only", "bmesh/a")
	l.worktree(t, open, "bmesh/b")
	l.node(t, mesh.Node{Path: "plug-1.a", Host: testHost, Worktree: merged}, "done")
	l.node(t, mesh.Node{Path: "plug-1.b", Host: testHost, Worktree: open}, "done")
	l.startLevel(t)

	ask := func(repo string, args ...string) hosts.Reply {
		j := hosts.NewJob(hostKey("mac"), "mac", testHost, "prune", repo, args, 2, time.Now(), time.Hour)
		l.send(t, j)
		return l.reply(t, j, 1)
	}
	want := "plug " + merged + " remove node done, merged\nplug " + open + " keep unmerged\n" +
		"2 worktrees, 0 branches, 1 to remove, 1 kept"
	if r := ask(""); !r.OK || r.Text != want {
		t.Fatalf("dry run: %+v\nwant %q", r, want)
	}
	if _, err := os.Stat(merged); err != nil {
		t.Fatal("a dry run removed a worktree")
	}
	want = "plug " + merged + " removed node done, merged\nplug " + open + " keep unmerged\n" +
		"2 worktrees, 0 branches, 1 to remove, 1 kept, 1 removed"
	if r := ask("plug", "apply"); !r.OK || r.Text != want {
		t.Fatalf("apply: %+v\nwant %q", r, want)
	}
	if _, err := os.Stat(merged); err == nil {
		t.Fatal("the merged worktree is still there")
	}
	if r := ask("nothere"); !r.OK || r.Text != "no checkout of nothere here" {
		t.Fatalf("absent: %+v", r)
	}
	if r := ask("", "force"); r.OK || r.Text != "refused: unknown argument force" {
		t.Fatalf("bad arg: %+v", r)
	}
}

func TestForgetWorktreeJob(t *testing.T) {
	l, wts := pruneSetup(t)
	wt := filepath.Join(wts, "plug", "plug-1.a")
	l.worktree(t, wt, "bmesh/a")
	l.node(t, mesh.Node{Path: "plug-1.a", Host: testHost, RepoPath: l.box, Worktree: wt}, "working")
	l.node(t, mesh.Node{Path: "plug-2.a", Host: "mac", Worktree: "/mac/plug-2.a"}, "done")
	l.startLevel(t)

	ask := func(path string) hosts.Reply {
		j := hosts.NewJob(hostKey("mac"), "mac", testHost, "forget-worktree", "", []string{path}, 2, time.Now(), time.Hour)
		l.send(t, j)
		return l.reply(t, j, 1)
	}
	for path, want := range map[string]string{
		"plug-1.a": "refused: plug-1.a is working; only a done or blocked node is forgotten",
		"plug-2.a": "refused: plug-2.a is on mac, not " + testHost,
		"plug-9":   "refused: no node plug-9",
	} {
		if r := ask(path); r.OK || r.Text != want {
			t.Fatalf("%s: %+v, want %q", path, r, want)
		}
	}
	meshtest.ForceState(t, l.c, "plug-1.a", mesh.State{State: "done", By: "node"})
	if r := ask("plug-1.a"); r.OK || r.Text != "refused: branch bmesh/a is not merged into main" {
		t.Fatalf("unmerged: %+v", r)
	}
	if _, ok, _ := mesh.GetNode(l.ctx, l.c, "plug-1.a"); !ok {
		t.Fatal("a refusal forgot the node")
	}
	meshtest.Git(t, l.box, "merge", "-q", "--ff-only", "bmesh/a")
	r := ask("plug-1.a")
	if want := "removed worktree " + wt + "; deleted branch bmesh/a; forgot plug-1.a"; !r.OK || r.Text != want {
		t.Fatalf("forget: %+v, want %q", r, want)
	}
	if _, ok, _ := mesh.GetNode(l.ctx, l.c, "plug-1.a"); ok {
		t.Fatal("node kept")
	}
	if strings.Contains(meshtest.Git(t, l.box, "branch"), "bmesh/a") {
		t.Fatal("branch kept")
	}
}
