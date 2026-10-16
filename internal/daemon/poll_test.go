package daemon

import (
	"bytes"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// branchLead registers a lead whose node records branch `recorded` and whose
// worktree is a repo on `main`.
func (f *fixture) branchLead(t *testing.T, path, recorded string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), "wt")
	gitT(t, filepath.Dir(wt), "init", "-q", "-b", "main", wt)
	gitT(t, wt, "commit", "-q", "--allow-empty", "-m", "x")
	gitT(t, wt, "checkout", "-q", "-b", recorded)
	if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: path, Role: "lead", Host: testHost, Branch: recorded, Worktree: wt,
		Runner: mesh.NodeRunner{Kind: "t3", Thread: "th-" + path}}); err != nil {
		t.Fatal(err)
	}
	return wt
}

func (f *fixture) nodeBranch(t *testing.T, path string) string {
	t.Helper()
	n, ok, err := mesh.GetNode(f.ctx, f.c, path)
	if err != nil || !ok {
		t.Fatalf("node %s: %v %v", path, ok, err)
	}
	return n.Branch
}

func TestPollFollowsARenamedBranch(t *testing.T) {
	f := setup(t)
	var logs bytes.Buffer
	f.d.Log = slog.New(slog.NewTextHandler(&logs, nil))
	wt := f.branchLead(t, "demo-005", "t3code/b0ee2f6d")
	pollOnce(f.ctx, f.d, nil)
	if b := f.nodeBranch(t, "demo-005"); b != "t3code/b0ee2f6d" {
		t.Fatalf("unrenamed branch changed to %q", b)
	}
	gitT(t, wt, "branch", "-m", "t3code/command-center-reorg")
	pollOnce(f.ctx, f.d, nil)
	if b := f.nodeBranch(t, "demo-005"); b != "t3code/command-center-reorg" {
		t.Fatalf("branch after rename: %q", b)
	}
	if !strings.Contains(logs.String(), "branch: demo-005 t3code/b0ee2f6d -> t3code/command-center-reorg") {
		t.Fatalf("log: %s", logs.String())
	}
}

func TestPollLeavesBranchOnDetachedOrMissingWorktree(t *testing.T) {
	f := setup(t)
	wt := f.branchLead(t, "demo-006", "t3code/aaaa")
	gitT(t, wt, "checkout", "-q", "--detach")
	pollOnce(f.ctx, f.d, nil)
	if b := f.nodeBranch(t, "demo-006"); b != "t3code/aaaa" {
		t.Fatalf("detached worktree changed the branch to %q", b)
	}
	wt2 := f.branchLead(t, "demo-007", "t3code/bbbb")
	gitT(t, wt2, "checkout", "-q", "main")
	if err := exec.Command("rm", "-rf", wt2).Run(); err != nil {
		t.Fatal(err)
	}
	pollOnce(f.ctx, f.d, nil)
	if b := f.nodeBranch(t, "demo-007"); b != "t3code/bbbb" {
		t.Fatalf("missing worktree changed the branch to %q", b)
	}
}

func TestNoteRunnerLogsEachChangeOnce(t *testing.T) {
	var buf bytes.Buffer
	d := Deps{Log: slog.New(slog.NewTextHandler(&buf, nil))}
	path := "note-" + t.Name()
	noteRunner(d, path, runner.Status{Running: true, Waiting: true})
	noteRunner(d, path, runner.Status{Running: true, Waiting: true})
	noteRunner(d, path, runner.Status{UsageLimit: true, LimitResetAt: "2026-10-16T09:00:00Z"})
	noteRunner(d, path, runner.Status{})
	noteRunner(d, path, runner.Status{Running: true, Waiting: true})
	got := buf.String()
	if strings.Count(got, "t3: "+path+" waits on a question or approval") != 2 ||
		strings.Count(got, "t3: "+path+" hit a usage limit; it resets at 2026-10-16T09:00:00Z") != 1 {
		t.Fatalf("log:\n%s", got)
	}
}
