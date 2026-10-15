package selfupdate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func tgit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo is a git repo on main with one commit per name in commits.
func repo(t *testing.T, commits ...string) (string, []string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bm")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tgit(t, dir, "init", "-q", "-b", "main")
	var shas []string
	for _, c := range commits {
		if err := os.WriteFile(filepath.Join(dir, "f"), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
		tgit(t, dir, "add", "f")
		tgit(t, dir, "commit", "-q", "-m", c)
		shas = append(shas, tgit(t, dir, "rev-parse", "HEAD"))
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
}

func TestInstallSwapsAndKeepsPrev(t *testing.T) {
	log := fakeGo(t)
	dir, shas := repo(t, "one")
	exe := filepath.Join(t.TempDir(), "bmesh")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), dir, exe); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(exe).Output()
	if err != nil || strings.TrimSpace(string(out)) != "fake bmesh "+shas[0] {
		t.Fatalf("new binary: %q %v", out, err)
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
	err := Install(context.Background(), dir, exe)
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
	dir, err := MainCheckout(origin, "", clones, "bm")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(clones, "bm") {
		t.Fatalf("dir %s", dir)
	}
	if h, _ := Head(dir); h != shas[0] {
		t.Fatalf("head %s, want %s", h, shas[0])
	}
	tgit(t, origin, "commit", "-q", "--allow-empty", "-m", "two")
	two := tgit(t, origin, "rev-parse", "HEAD")
	if dir, err = MainCheckout(origin, "", clones, "bm"); err != nil {
		t.Fatal(err)
	}
	if h, _ := Head(dir); h != two {
		t.Fatalf("head %s, want %s", h, two)
	}
	if b := tgit(t, origin, "status", "--porcelain"); b != "" {
		t.Fatalf("operator's checkout touched: %q", b)
	}
}

func TestMainCheckoutUsesDaemonCheckout(t *testing.T) {
	origin, shas := repo(t, "one")
	root := filepath.Join(t.TempDir(), "checkouts")
	dir, err := MainCheckout(origin, root, "", "bm")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(root, "bm", "main") {
		t.Fatalf("dir %s", dir)
	}
	if h, _ := Head(dir); h != shas[0] {
		t.Fatalf("head %s", h)
	}
}
