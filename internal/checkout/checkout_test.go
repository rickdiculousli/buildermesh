package checkout

import (
	"os"
	"os/exec"
	"path/filepath"
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

func commitFile(t *testing.T, repo, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tgit(t, repo, "add", name)
	tgit(t, repo, "commit", "-q", "-m", "add "+name)
	return tgit(t, repo, "rev-parse", "HEAD")
}

func newRepo(t *testing.T) (repo string, c1, c2 string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	tgit(t, repo, "init", "-q")
	c1 = commitFile(t, repo, "a.txt", "one\n")
	c2 = commitFile(t, repo, "b.txt", "two\n")
	return
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestEnsureThenMoveToOlderCommit(t *testing.T) {
	repo, c1, _ := newRepo(t)
	root := t.TempDir()
	dir, err := Ensure(repo, root, "routine.verify.sonnet-5-5-1")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "repo", "routine.verify.sonnet-5-5-1"); dir != want {
		t.Fatalf("dir = %s, want %s", dir, want)
	}
	if !exists(filepath.Join(dir, "b.txt")) {
		t.Fatal("b.txt missing at HEAD")
	}
	if err := os.WriteFile(filepath.Join(dir, "junk.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MoveTo(dir, c1); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dir, "b.txt")) {
		t.Error("b.txt still present")
	}
	if exists(filepath.Join(dir, "junk.txt")) {
		t.Error("junk.txt still present")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "one\n" {
		t.Errorf("a.txt = %q, want restored", b)
	}
	if err := MoveTo(dir, "--bogus"); err == nil {
		t.Error("MoveTo accepted a bad commit")
	}
}

func TestRemoveForgetsWorktree(t *testing.T) {
	repo, _, _ := newRepo(t)
	dir, err := Ensure(repo, t.TempDir(), "main")
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	listed := func() bool {
		return strings.Contains(tgit(t, repo, "worktree", "list", "--porcelain"), "worktree "+real+"\n")
	}
	if !listed() {
		t.Fatal("worktree not listed before Remove")
	}
	if err := Remove(repo, dir); err != nil {
		t.Fatal(err)
	}
	if exists(dir) {
		t.Error("dir still exists")
	}
	if listed() {
		t.Error("worktree still listed after Remove")
	}
}

func TestEnsureIsIdempotent(t *testing.T) {
	repo, _, _ := newRepo(t)
	root := t.TempDir()
	d1, err := Ensure(repo, root, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d1, "marker"), []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	d2, err := Ensure(repo, root, "main")
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Errorf("%s != %s", d1, d2)
	}
	if !exists(filepath.Join(d2, "marker")) {
		t.Error("second Ensure recreated the checkout")
	}
}

func TestEnsureReplacesNonWorktreeDir(t *testing.T) {
	repo, _, _ := newRepo(t)
	root := t.TempDir()
	stale := filepath.Join(root, "repo", "main")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "stale"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := Ensure(repo, root, "main")
	if err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dir, "stale")) || !exists(filepath.Join(dir, ".git")) {
		t.Error("stale dir not replaced by a worktree")
	}
}

func TestEnsureReplacesStaleWorktree(t *testing.T) {
	repo, _, c2 := newRepo(t)
	root := t.TempDir()
	dir, err := Ensure(repo, root, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(repo, ".git", "worktrees")); err != nil {
		t.Fatal(err)
	}
	dir, err = Ensure(repo, root, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := MoveTo(dir, "HEAD"); err != nil {
		t.Fatal(err)
	}

	// Repo deleted and recreated at the same path.
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	tgit(t, repo, "init", "-q")
	commitFile(t, repo, "z.txt", "z\n")
	dir, err = Ensure(repo, root, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := MoveTo(dir, "HEAD"); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(dir, "z.txt")) || exists(filepath.Join(dir, "b.txt")) {
		t.Errorf("checkout not from the recreated repo (old head %s)", c2)
	}
}

func TestEnsureRejectsBadArguments(t *testing.T) {
	repo, _, _ := newRepo(t)
	if _, err := Ensure(repo, "", "main"); err == nil {
		t.Error("empty root accepted")
	}
	for _, r := range []string{".", "..", "/"} {
		if _, err := Ensure(r, t.TempDir(), "main"); err == nil {
			t.Errorf("repo %q accepted", r)
		}
	}
}

func TestEnsureRejectsBadInstance(t *testing.T) {
	repo, _, _ := newRepo(t)
	for _, inst := range []string{"../x", "", "a/b", ".hidden", "Up"} {
		if _, err := Ensure(repo, t.TempDir(), inst); err == nil {
			t.Errorf("instance %q accepted", inst)
		}
	}
}

func TestFetchNoOriginIsNoop(t *testing.T) {
	repo, _, _ := newRepo(t)
	if err := Fetch(repo); err != nil {
		t.Fatal(err)
	}
}

func TestFetchUpdatesOriginRef(t *testing.T) {
	origin, _, _ := newRepo(t)
	clone := filepath.Join(t.TempDir(), "clone")
	if out, err := exec.Command("git", "clone", "-q", origin, clone).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	c3 := commitFile(t, origin, "c.txt", "three\n")
	if err := Fetch(clone); err != nil {
		t.Fatal(err)
	}
	dir, err := Ensure(clone, t.TempDir(), "main")
	if err != nil {
		t.Fatal(err)
	}
	branch := tgit(t, origin, "rev-parse", "--abbrev-ref", "HEAD")
	if err := MoveTo(dir, "origin/"+branch); err != nil {
		t.Fatal(err)
	}
	if got := tgit(t, dir, "rev-parse", "HEAD"); got != c3 {
		t.Errorf("HEAD = %s, want %s", got, c3)
	}
}
