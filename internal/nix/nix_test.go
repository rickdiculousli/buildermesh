package nix

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// fixture is a checkout app of origin.git with one pushed commit, and a
// routine checkout of it under checkouts/app.
type fixture struct {
	root, origin, app, checkouts string
}

func setup(t *testing.T) fixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	root := t.TempDir()
	f := fixture{root: root, origin: filepath.Join(root, "origin.git"), app: filepath.Join(root, "repos", "app"),
		checkouts: filepath.Join(root, "checkouts")}
	meshtest.Git(t, root, "init", "-q", "--bare", "-b", "main", f.origin)
	meshtest.Git(t, root, "clone", "-q", f.origin, f.app)
	meshtest.CommitFiles(t, f.app, map[string]string{"a.txt": "one\n", ".gitignore": "*.log\n"})
	meshtest.Git(t, f.app, "push", "-q", "origin", "HEAD:main")
	meshtest.Git(t, f.app, "worktree", "add", "-q", "--detach", filepath.Join(f.checkouts, "app", "verify"), "origin/main")
	return f
}

func (f fixture) req() Request {
	return Request{Repo: "app", Path: f.app, RowPath: "~/repos/app", Origin: f.origin, Checkouts: f.checkouts}
}

func (f fixture) write(t *testing.T, rel, text string) {
	t.Helper()
	p := filepath.Join(f.app, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestNixRefusesTouchingNothing(t *testing.T) {
	for _, c := range []struct {
		name string
		prep func(t *testing.T, f fixture) Request
		want string
	}{
		{"uncommitted", func(t *testing.T, f fixture) Request { f.write(t, "a.txt", "two\n"); return f.req() },
			"uncommitted or untracked changes"},
		{"untracked", func(t *testing.T, f fixture) Request { f.write(t, "new.txt", "x\n"); return f.req() },
			"uncommitted or untracked changes"},
		{"a dirty linked worktree", func(t *testing.T, f fixture) Request {
			wt := filepath.Join(f.root, "wt")
			meshtest.Git(t, f.app, "worktree", "add", "-q", "-b", "side", wt, "main")
			os.WriteFile(filepath.Join(wt, "x.txt"), []byte("x"), 0o644)
			return f.req()
		}, "worktree " + "WT" + " has uncommitted"},
		{"a linked worktree on an unmerged branch", func(t *testing.T, f fixture) Request {
			wt := filepath.Join(f.root, "wt")
			meshtest.Git(t, f.app, "worktree", "add", "-q", "-b", "side", wt, "main")
			meshtest.CommitFiles(t, wt, map[string]string{"s.txt": "s\n"})
			meshtest.Git(t, wt, "push", "-q", "origin", "side") // on a remote, still not merged
			return f.req()
		}, "is on side, not merged into main"},
		{"a commit on no remote", func(t *testing.T, f fixture) Request {
			meshtest.CommitFiles(t, f.app, map[string]string{"b.txt": "b\n"})
			return f.req()
		}, "has commits on no remote"},
		{"a tag on no remote", func(t *testing.T, f fixture) Request {
			meshtest.CommitFiles(t, f.app, map[string]string{"b.txt": "b\n"})
			meshtest.Git(t, f.app, "tag", "v1")
			meshtest.Git(t, f.app, "reset", "-q", "--hard", "origin/main")
			return f.req()
		}, "has commits on no remote"},
		{"a stash", func(t *testing.T, f fixture) Request {
			f.write(t, "a.txt", "two\n")
			meshtest.Git(t, f.app, "stash", "-q")
			return f.req()
		}, "has a stash"},
		{"ignored files", func(t *testing.T, f fixture) Request { f.write(t, "debug.log", "x\n"); return f.req() },
			"has ignored files (debug.log)"},
		{"no origin", func(t *testing.T, f fixture) Request {
			meshtest.Git(t, f.app, "remote", "remove", "origin")
			return f.req()
		}, "has no origin"},
		{"another origin", func(t *testing.T, f fixture) Request {
			r := f.req()
			r.Origin = "me/other"
			return r
		}, "not the registry's me/other"},
		{"this box's row names another remote", func(t *testing.T, f fixture) Request {
			r := f.req()
			r.RowRemote = "me/other"
			return r
		}, "this box's registry row says remote me/other"},
		{"a path whose name isn't the row's", func(t *testing.T, f fixture) Request {
			r := f.req()
			r.RowPath = "~/repos/other"
			return r
		}, "whose name isn't the row's other"},
		{"a symlink to another directory", func(t *testing.T, f fixture) Request {
			link := filepath.Join(f.root, "elsewhere", "app")
			os.MkdirAll(filepath.Dir(link), 0o755)
			if err := os.Symlink(f.root, link); err != nil {
				t.Fatal(err)
			}
			r := f.req()
			r.Path = link
			return r
		}, "whose name isn't the row's app"},
		{"a subdirectory, not the top level", func(t *testing.T, f fixture) Request {
			sub := filepath.Join(f.app, "sub", "app")
			os.MkdirAll(sub, 0o755)
			os.WriteFile(filepath.Join(sub, "k"), []byte("k"), 0o644)
			meshtest.Git(t, f.app, "add", "-A")
			meshtest.Git(t, f.app, "commit", "-q", "-m", "sub")
			meshtest.Git(t, f.app, "push", "-q", "origin", "HEAD:main")
			r := f.req()
			r.Path = sub
			return r
		}, "is not a checkout's top level"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t)
			req := c.prep(t, f)
			o := Nix(context.Background(), req)
			want := strings.ReplaceAll(c.want, "WT", filepath.Join(f.root, "wt"))
			if o.Result != Refused || !strings.Contains(o.Text, want) {
				t.Fatalf("%s: %+v, want refused with %q", c.name, o, want)
			}
			if !exists(f.app) || !exists(filepath.Join(f.checkouts, "app", "verify")) {
				t.Fatal("a refusal touched the checkouts")
			}
		})
	}
}

func TestNixRemovesTheCheckoutItsWorktreesAndRoutineCheckouts(t *testing.T) {
	f := setup(t)
	wt := filepath.Join(f.root, "t3", "lead")
	meshtest.Git(t, f.app, "worktree", "add", "-q", "-b", "landed", wt, "main") // merged: at main
	f.write(t, "debug.log", "x\n")
	req := f.req()
	req.Origin = f.origin + "/" // the same remote, written another way
	req.IgnoredOK = true
	o := Nix(context.Background(), req)
	if o.Result != Removed || !o.OK() || !strings.Contains(o.Text, "removed "+f.app) || !strings.Contains(o.Text, "1 linked worktree") {
		t.Fatalf("%+v", o)
	}
	for _, p := range []string{f.app, wt, filepath.Join(f.checkouts, "app")} {
		if exists(p) {
			t.Errorf("%s is still there", p)
		}
	}
	// Again: absent.
	if o := Nix(context.Background(), req); o.Result != Absent || !o.OK() {
		t.Fatalf("again: %+v", o)
	}
}

func TestNixOfAnAbsentCheckoutStillRemovesItsRoutineCheckouts(t *testing.T) {
	f := setup(t)
	if err := os.RemoveAll(f.app); err != nil {
		t.Fatal(err)
	}
	o := Nix(context.Background(), f.req())
	if o.Result != Absent || !strings.Contains(o.Text, "removed "+filepath.Join(f.checkouts, "app")) || exists(filepath.Join(f.checkouts, "app")) {
		t.Fatalf("%+v", o)
	}
}
