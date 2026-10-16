package level

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// boxes is an origin and two clones of it on main: box is the checkout
// under test, peer another box's.
type boxes struct{ origin, box, peer string }

func newBoxes(t *testing.T) boxes {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	setup(t) // the plugin fakes: a level that moves refreshes plugins
	root := t.TempDir()
	b := boxes{origin: filepath.Join(root, "origin.git"), box: filepath.Join(root, "box"), peer: filepath.Join(root, "peer")}
	meshtest.Git(t, root, "init", "-q", "--bare", "-b", "main", b.origin)
	seed := filepath.Join(root, "seed")
	meshtest.Git(t, root, "init", "-q", "-b", "main", seed)
	meshtest.CommitFiles(t, seed, map[string]string{"a.txt": "one\n"})
	meshtest.Git(t, seed, "push", "-q", b.origin, "main")
	meshtest.Git(t, root, "clone", "-q", b.origin, b.box)
	meshtest.Git(t, root, "clone", "-q", b.origin, b.peer)
	return b
}

// land commits on peer and pushes it: origin moves ahead of box.
func (b boxes) land(t *testing.T, files map[string]string) string {
	t.Helper()
	sha := meshtest.CommitFiles(t, b.peer, files)
	meshtest.Git(t, b.peer, "push", "-q", "origin", "HEAD:main")
	return sha
}

func (b boxes) req(t *testing.T) Request {
	return Request{Repo: "repo", Path: b.box, StateDir: t.TempDir(), Push: true}
}

func head(t *testing.T, dir string) string { return meshtest.Git(t, dir, "rev-parse", "HEAD") }

func want(t *testing.T, o Outcome, r Result, text string) {
	t.Helper()
	if o.Result != r || o.Text() != text {
		t.Fatalf("level: %s %q, want %s %q", o.Result, o.Text(), r, text)
	}
}

func TestLevelUpToDateAndBehind(t *testing.T) {
	b := newBoxes(t)
	ctx := context.Background()
	want(t, Level(ctx, b.req(t)), UpToDate, "up to date")
	b.land(t, map[string]string{"b.txt": "two\n"})
	top := b.land(t, map[string]string{"c.txt": "three\n"})
	o := Level(ctx, b.req(t))
	want(t, o, FastForwarded, "fast-forwarded 2")
	if head(t, b.box) != top {
		t.Fatal("box did not move to origin/main")
	}
}

func TestLevelRefusals(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		prep func(t *testing.T, b boxes)
		why  string
	}{
		{"dirty", func(t *testing.T, b boxes) {
			os.WriteFile(filepath.Join(b.box, "a.txt"), []byte("changed\n"), 0o644)
		}, "dirty"},
		{"untracked", func(t *testing.T, b boxes) {
			os.WriteFile(filepath.Join(b.box, "new.txt"), []byte("x\n"), 0o644)
		}, "dirty"},
		{"other branch", func(t *testing.T, b boxes) {
			meshtest.Git(t, b.box, "checkout", "-q", "-b", "feature")
		}, "other branch"},
		{"detached", func(t *testing.T, b boxes) {
			meshtest.Git(t, b.box, "checkout", "-q", "--detach")
		}, "other branch"},
		{"diverged", func(t *testing.T, b boxes) {
			meshtest.CommitFiles(t, b.box, map[string]string{"mine.txt": "x\n"})
		}, "diverged"},
		{"merge in progress", func(t *testing.T, b boxes) {
			os.WriteFile(filepath.Join(b.box, ".git", "MERGE_HEAD"), []byte(head(t, b.box)+"\n"), 0o644)
		}, "in progress"},
		{"rebase in progress", func(t *testing.T, b boxes) {
			os.Mkdir(filepath.Join(b.box, ".git", "rebase-merge"), 0o755)
		}, "in progress"},
		{"index.lock", func(t *testing.T, b boxes) {
			os.WriteFile(filepath.Join(b.box, ".git", "index.lock"), nil, 0o644)
		}, "in progress"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBoxes(t)
			b.land(t, map[string]string{"b.txt": "two\n"})
			before := head(t, b.box)
			tc.prep(t, b)
			r := b.req(t)
			r.Branch = "main"
			o := Level(ctx, r)
			want(t, o, Refused, "refused: "+tc.why)
			if tc.name != "diverged" && head(t, b.box) != before {
				t.Fatal("a refused level moved the checkout")
			}
		})
	}
}

func TestLevelShaMustBeOnOrigin(t *testing.T) {
	b := newBoxes(t)
	ctx := context.Background()
	top := b.land(t, map[string]string{"b.txt": "two\n"})
	// A side branch on origin: its commit is named, it isn't on main.
	meshtest.Git(t, b.peer, "checkout", "-q", "-b", "side")
	side := meshtest.CommitFiles(t, b.peer, map[string]string{"side.txt": "x\n"})
	meshtest.Git(t, b.peer, "push", "-q", "origin", "side")
	r := b.req(t)
	r.Sha, r.Branch = side, "main"
	want(t, Level(ctx, r), Refused, "refused: "+side[:8]+" not on origin/main")
	// A commit origin doesn't have at all (a fork's).
	r.Sha = strings.Repeat("ab", 20)
	want(t, Level(ctx, r), Refused, "refused: abababab not on origin/main")
	r.Sha = top
	want(t, Level(ctx, r), FastForwarded, "fast-forwarded 1")
}

func TestLevelPinnedTag(t *testing.T) {
	b := newBoxes(t)
	ctx := context.Background()
	v1 := b.land(t, map[string]string{"b.txt": "two\n"})
	meshtest.Git(t, b.peer, "tag", "v1")
	b.land(t, map[string]string{"c.txt": "three\n"})
	meshtest.Git(t, b.peer, "push", "-q", "origin", "v1")
	r := b.req(t)
	r.Pin = "v1"
	want(t, Level(ctx, r), Pinned, "moved to v1")
	if head(t, b.box) != v1 {
		t.Fatal("box is not at v1")
	}
	want(t, Level(ctx, r), UpToDate, "up to date")
	// The sha must be on the tag: origin/main's head isn't.
	r.Sha = head(t, b.peer)
	want(t, Level(ctx, r), Refused, "refused: "+r.Sha[:8]+" not on v1")
	r.Sha, r.Pin = "", "v9"
	want(t, Level(ctx, r), Refused, "refused: no tag v9")
}

func TestLevelPushGate(t *testing.T) {
	ctx := context.Background()
	later := func() time.Time { return time.Now().Add(time.Hour) }
	for _, tc := range []struct {
		name string
		edit func(r *Request)
		text string
	}{
		{"self-level", func(r *Request) { r.Push = false }, "ahead 1, not pushed: a self-level never pushes"},
		{"mode", func(r *Request) { r.Mode = "companion" }, "ahead 1, not pushed: mode companion is not in push_modes"},
		{"push_modes", func(r *Request) { r.PushModes = []string{"publish"} }, "ahead 1, not pushed: mode ship is not in push_modes"},
		{"too new", func(r *Request) { r.Now = func() time.Time { return time.Now().Add(3 * time.Minute) } },
			"ahead 1, not pushed: the newest commit is 3m0s old (10m0s before a push)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBoxes(t)
			meshtest.CommitFiles(t, b.box, map[string]string{"mine.txt": "x\n"})
			r := b.req(t)
			r.Now = later
			tc.edit(&r)
			want(t, Level(ctx, r), Ahead, tc.text)
			if meshtest.Git(t, b.origin, "rev-parse", "main") == head(t, b.box) {
				t.Fatal("pushed anyway")
			}
		})
	}
	t.Run("pushed with its tags", func(t *testing.T) {
		b := newBoxes(t)
		meshtest.CommitFiles(t, b.box, map[string]string{"mine.txt": "x\n"})
		meshtest.Git(t, b.box, "tag", "v2") // lightweight, as pstack's update.sh makes
		mine := meshtest.CommitFiles(t, b.box, map[string]string{"mine2.txt": "y\n"})
		meshtest.Git(t, b.box, "tag", "-a", "-m", "v3", "v3")
		meshtest.Git(t, b.box, "tag", "old", "HEAD~2") // on origin's commit: not pushed
		r := b.req(t)
		r.Now = later
		r.Mode = "fork"
		want(t, Level(ctx, r), Pushed, "pushed 2")
		if got := meshtest.Git(t, b.origin, "rev-parse", "main"); got != mine {
			t.Fatalf("origin main %s, want %s", got, mine)
		}
		if tags := meshtest.Git(t, b.origin, "tag"); tags != "v2\nv3" {
			t.Fatalf("origin tags %q, want v2 and v3", tags)
		}
	})
	t.Run("second push diverged", func(t *testing.T) {
		b := newBoxes(t)
		meshtest.CommitFiles(t, b.box, map[string]string{"mine.txt": "x\n"})
		r := b.req(t)
		r.Now = later
		// The peer pushes between box's fetch and its push: a pre-push
		// hook lands the peer's commit first.
		meshtest.CommitFiles(t, b.peer, map[string]string{"theirs.txt": "y\n"})
		hook := "#!/bin/sh\nunset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE\ngit -C " + b.peer + " push -q origin HEAD:main\n"
		if err := os.WriteFile(filepath.Join(b.box, ".git", "hooks", "pre-push"), []byte(hook), 0o755); err != nil {
			t.Fatal(err)
		}
		want(t, Level(ctx, r), Refused, "refused: diverged")
	})
}

func TestLevelHoldAndExpect(t *testing.T) {
	b := newBoxes(t)
	ctx := context.Background()
	before := head(t, b.box)
	top := b.land(t, map[string]string{"plugin.txt": "new\n"})
	r := b.req(t)
	r.Hold = true
	o := Level(ctx, r)
	want(t, o, Held, "held for approval (+1 commit)")
	if head(t, b.box) != before || o.Target != top || !strings.Contains(o.Log, "plugin.txt") {
		t.Fatalf("hold: moved or no log: target %s log %q", o.Target, o.Log)
	}
	// The approval saw top; origin moved since.
	newer := b.land(t, map[string]string{"more.txt": "x\n"})
	r.Hold, r.Expect = false, top
	o = Level(ctx, r)
	if o.Result != TargetMoved || o.Target != newer || !strings.Contains(o.Log, "more.txt") || head(t, b.box) != before {
		t.Fatalf("moved target: %s %s log %q", o.Result, o.Target, o.Log)
	}
	r.Expect = newer
	want(t, Level(ctx, r), FastForwarded, "fast-forwarded 2")
}

func TestLevelWaitsForTheLock(t *testing.T) {
	b := newBoxes(t)
	ctx := context.Background()
	b.land(t, map[string]string{"b.txt": "two\n"})
	r := b.req(t)
	unlock, err := Lock(r.StateDir, r.Repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.StateDir, "locks", "repo.lock")); err != nil {
		t.Fatal(err)
	}
	done := make(chan Outcome, 1)
	go func() { done <- Level(ctx, r) }()
	select {
	case o := <-done:
		t.Fatalf("level ran while a landing held the lock: %s", o.Text())
	case <-time.After(300 * time.Millisecond):
	}
	unlock()
	select {
	case o := <-done:
		want(t, o, FastForwarded, "fast-forwarded 1")
	case <-time.After(30 * time.Second):
		t.Fatal("level never took the lock")
	}
}

func TestLevelPluginRefreshAfterMove(t *testing.T) {
	b := newBoxes(t)
	ctx := context.Background()
	f := setup(t)
	t.Setenv("FAKE_CLAUDE_LIST", `[{"id":"p@mk","version":"1","scope":"user","folderVersion":"2"}]`)
	os.MkdirAll(filepath.Join(b.peer, ".claude-plugin"), 0o755)
	b.land(t, map[string]string{".claude-plugin/marketplace.json": `{"name":"mk"}`})
	o := Level(ctx, b.req(t))
	want(t, o, FastForwarded, "fast-forwarded 1; claude p@mk 1 -> 2")
	if !strings.Contains(strings.Join(f.lines(), "\n"), "plugin update p@mk --scope user") {
		t.Fatalf("no plugin update: %q", f.lines())
	}
}
