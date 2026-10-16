package selfupdate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// repo is a git repo on main with one commit per name in commits.
func repo(t *testing.T, commits ...string) (string, []string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bm")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, dir, "init", "-q", "-b", "main")
	var shas []string
	for _, c := range commits {
		if err := os.WriteFile(filepath.Join(dir, "f"), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
		meshtest.Git(t, dir, "add", "f")
		meshtest.Git(t, dir, "commit", "-q", "-m", c)
		shas = append(shas, meshtest.Git(t, dir, "rev-parse", "HEAD"))
	}
	return dir, shas
}

// fakeGo points the build at testdata/fake-go.sh, outside any systemd unit.
func fakeGo(t *testing.T) (log string) {
	t.Helper()
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-go.sh"))
	if err != nil {
		t.Fatal(err)
	}
	log = filepath.Join(t.TempDir(), "go.log")
	t.Setenv("BMESH_GO_COMMAND", "sh "+fake)
	t.Setenv("FAKE_GO_LOG", log)
	t.Setenv("FAKE_GO_EXIT", "")
	t.Setenv("FAKE_GO_REVISION", "")
	t.Setenv("INVOCATION_ID", "")
	return log
}

func TestVersionFromBuildInfo(t *testing.T) {
	bi := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "1a2b3c4d5e6f7a8b9c0d1a2b3c4d5e6f7a8b9c0d"},
		{Key: "vcs.time", Value: "2026-10-15T09:00:00Z"},
		{Key: "vcs.modified", Value: "true"},
	}}
	v := FromBuildInfo(bi, true)
	if v.String() != "1a2b3c4d 2026-10-15T09:00:00Z modified" {
		t.Fatalf("%q", v.String())
	}
	bi.Settings[2].Value = "false"
	if s := FromBuildInfo(bi, true).String(); s != "1a2b3c4d 2026-10-15T09:00:00Z" {
		t.Fatalf("%q", s)
	}
	if s := FromBuildInfo(&debug.BuildInfo{}, true).String(); s != "unknown" {
		t.Fatalf("no vcs: %q", s)
	}
	if s := FromBuildInfo(nil, false).String(); s != "unknown" || Short("") != "unknown" {
		t.Fatalf("no build info: %q", s)
	}
	// A stamped build: vcs settings win, the stamp stands in without them.
	stamp(t, "9f8e7d6c5b4a39281706f5e4d3c2b1a098765432", "2026-10-16T08:00:00Z")
	bi.Settings[2].Value = "true"
	if s := FromBuildInfo(bi, true).String(); s != "1a2b3c4d 2026-10-15T09:00:00Z modified" {
		t.Fatalf("vcs over stamp: %q", s)
	}
	for _, v := range []Version{FromBuildInfo(&debug.BuildInfo{}, true), FromBuildInfo(nil, false)} {
		if v.String() != "9f8e7d6c 2026-10-16T08:00:00Z" || v.Revision != stampRevision {
			t.Fatalf("stamp only: %+v", v)
		}
	}
}

// stamp sets what Install's -ldflags would, for the test.
func stamp(t *testing.T, rev, at string) {
	t.Helper()
	savedRev, savedTime := stampRevision, stampTime
	t.Cleanup(func() { stampRevision, stampTime = savedRev, savedTime })
	stampRevision, stampTime = rev, at
}

func TestInstallSwapsAndKeepsPrev(t *testing.T) {
	log := fakeGo(t)
	dir, shas := repo(t, "one")
	exe := filepath.Join(t.TempDir(), "bmesh")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), dir, exe, shas[0]); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(exe).Output()
	if err != nil || strings.TrimSpace(string(out)) != "fake bmesh "+shas[0] {
		t.Fatalf("new binary: %q %v", out, err)
	}
	if out, _ := exec.Command(exe, "version").Output(); !strings.HasPrefix(string(out), "bmesh "+shas[0][:8]+" ") {
		t.Fatalf("new binary's version: %q", out)
	}
	if b, _ := os.ReadFile(Prev(exe)); string(b) != "old" || Prev(exe) != filepath.Join(filepath.Dir(exe), "bmesh.prev") {
		t.Fatalf("prev %q at %s", b, Prev(exe))
	}
	if b, _ := os.ReadFile(log); strings.TrimSpace(string(b)) != "build "+shas[0] {
		t.Fatalf("go log %q", b)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".bmesh-update-*")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

func TestInstallFailedBuildLeavesBinary(t *testing.T) {
	fakeGo(t)
	t.Setenv("FAKE_GO_EXIT", "1")
	dir, _ := repo(t, "one")
	exe := filepath.Join(t.TempDir(), "bmesh")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Install(context.Background(), dir, exe, head(t, dir))
	if err == nil || !strings.Contains(err.Error(), "fake build failed") {
		t.Fatalf("err %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatalf("binary %q", b)
	}
	if _, err := os.Stat(Prev(exe)); !os.IsNotExist(err) {
		t.Fatalf("prev written: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".bmesh-update-*")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

func head(t *testing.T, dir string) string {
	t.Helper()
	h, err := Head(dir)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestInstallRefusesWrongRevision(t *testing.T) {
	fakeGo(t)
	t.Setenv("FAKE_GO_REVISION", "eeeeeeee")
	dir, shas := repo(t, "one")
	exe := filepath.Join(t.TempDir(), "bmesh")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Install(context.Background(), dir, exe, shas[0])
	var refused *Refused
	if !errors.As(err, &refused) || err.Error() != "built binary reports eeeeeeee, not "+shas[0][:8]+"; keeping this binary" {
		t.Fatalf("err %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatalf("binary %q", b)
	}
	if _, err := os.Stat(Prev(exe)); !os.IsNotExist(err) {
		t.Fatalf("prev written: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".bmesh-update-*")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

// TestInstallRealBuildInWorktree builds this module, copied into a repo of
// its own, from a detached git worktree of it, where go writes no VCS stamp.
func TestInstallRealBuildInWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("short: no real build")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go on PATH")
	}
	t.Setenv("BMESH_GO_COMMAND", "")
	t.Setenv("INVOCATION_ID", "")
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "bm")
	files := strings.Split(strings.TrimRight(meshtest.Git(t, root, "ls-files", "-z", "--cached", "--others", "--exclude-standard"), "\x00"), "\x00")
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(root, f))
		if errors.Is(err, os.ErrNotExist) {
			continue // deleted, not yet committed
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(src, filepath.Dir(f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	meshtest.Git(t, src, "init", "-q", "-b", "main")
	meshtest.Git(t, src, "add", "-A")
	meshtest.Git(t, src, "commit", "-q", "-m", "copy")
	want := meshtest.Git(t, src, "rev-parse", "HEAD")
	wt := filepath.Join(t.TempDir(), "main")
	meshtest.Git(t, src, "worktree", "add", "-q", "--detach", wt, "main")

	exe := filepath.Join(t.TempDir(), "bmesh")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), wt, exe, want); err != nil {
		t.Fatal(err)
	}
	at := meshtest.Git(t, src, "show", "-s", "--format=%ct", "HEAD")
	sec, _ := strconv.ParseInt(at, 10, 64)
	out, err := exec.Command(exe, "version").Output()
	if w := "bmesh " + want[:8] + " " + time.Unix(sec, 0).UTC().Format(time.RFC3339) + "\n"; err != nil || string(out) != w {
		t.Fatalf("version %q %v, want %q", out, err, w)
	}
}

func TestContains(t *testing.T) {
	dir, shas := repo(t, "one", "two")
	if !Contains(dir, shas[0], shas[1]) || !Contains(dir, shas[1], shas[1]) {
		t.Fatal("ancestor not contained")
	}
	if Contains(dir, shas[1], shas[0]) || Contains(dir, "", shas[1]) || Contains(dir, strings.Repeat("e", 40), shas[1]) {
		t.Fatal("contained what main lacks")
	}
}

func TestMainCheckoutClonesAndFollows(t *testing.T) {
	origin, shas := repo(t, "one")
	clones := filepath.Join(t.TempDir(), "update")
	dir, unlock, err := MainCheckout(origin, "", clones, "bm")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(clones, "bm") {
		t.Fatalf("dir %s", dir)
	}
	if h, _ := Head(dir); h != shas[0] {
		t.Fatalf("head %s, want %s", h, shas[0])
	}
	meshtest.Git(t, origin, "commit", "-q", "--allow-empty", "-m", "two")
	two := meshtest.Git(t, origin, "rev-parse", "HEAD")
	unlock()
	if dir, unlock, err = MainCheckout(origin, "", clones, "bm"); err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if h, _ := Head(dir); h != two {
		t.Fatalf("head %s, want %s", h, two)
	}
	if b := meshtest.Git(t, origin, "status", "--porcelain"); b != "" {
		t.Fatalf("operator's checkout touched: %q", b)
	}
}

func TestMainCheckoutUsesDaemonCheckout(t *testing.T) {
	origin, shas := repo(t, "one")
	root := filepath.Join(t.TempDir(), "checkouts")
	dir, unlock, err := MainCheckout(origin, root, "", "bm")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if dir != filepath.Join(root, "bm", "main") {
		t.Fatalf("dir %s", dir)
	}
	if h, _ := Head(dir); h != shas[0] {
		t.Fatalf("head %s", h)
	}
}

func TestLockWaitsForHolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "main")
	unlock, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan func())
	go func() {
		u, err := Lock(dir)
		if err != nil {
			t.Error(err)
		}
		got <- u
	}()
	select {
	case <-got:
		t.Fatal("second holder got the lock")
	case <-time.After(300 * time.Millisecond):
	}
	unlock()
	select {
	case u := <-got:
		u()
	case <-time.After(5 * time.Second):
		t.Fatal("lock not passed on")
	}
}
