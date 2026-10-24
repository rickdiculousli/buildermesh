package plugin

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	buildermesh "github.com/rickdiculousli/buildermesh"
)

// tree is every file under dir with its bytes, the stamp left out.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == stampFile {
			return nil
		}
		b, err := os.ReadFile(p)
		got[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The written dir is the embedded plugin, byte for byte, and its manifests
// and skills are there.
func TestEnsureWritesTheEmbeddedPlugin(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugin")
	wrote, err := Ensure(dir, "v1 (abcd1234)")
	if err != nil || !wrote {
		t.Fatalf("wrote %v err %v", wrote, err)
	}
	want := map[string]string{}
	err = fs.WalkDir(buildermesh.Plugin, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(buildermesh.Plugin, p)
		want[p] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got := tree(t, dir)
	if len(got) != len(want) {
		t.Fatalf("%d files, want %d", len(got), len(want))
	}
	for p, b := range want {
		if got[p] != b {
			t.Fatalf("%s differs from the embedded file", p)
		}
	}
	for _, p := range []string{".claude-plugin/marketplace.json", ".claude-plugin/plugin.json", "skills/work/SKILL.md",
		"skills/lead/SKILL.md", "hooks/hooks.json", "third_party/ponytail/LICENSE"} {
		if _, ok := got[p]; !ok {
			t.Fatalf("no %s in %v", p, got)
		}
	}
	if _, label, ok := Written(dir); !ok || label != "v1 (abcd1234)" {
		t.Fatalf("written by %q %v", label, ok)
	}
}

// A current stamp leaves the dir alone; another binary's (or other files)
// rewrites it whole, dropping what the old one had.
func TestWriteOnlyOnAnotherStamp(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugin")
	v1 := fstest.MapFS{"skills/a/SKILL.md": {Data: []byte("a1")}, "skills/b/SKILL.md": {Data: []byte("b1")}}
	if wrote, err := Write(dir, v1, "r1"); err != nil || !wrote {
		t.Fatalf("first: %v %v", wrote, err)
	}
	stray := filepath.Join(dir, "skills", "a", "stray")
	if err := os.WriteFile(stray, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if wrote, err := Write(dir, v1, "r1"); err != nil || wrote {
		t.Fatalf("current: %v %v", wrote, err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatal("a current stamp rewrote the dir")
	}
	// An old dir a crash or another writer left behind goes with the next write.
	if err := os.MkdirAll(filepath.Join(filepath.Dir(dir), ".plugin-old-123", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if wrote, err := Write(dir, v1, "r2"); err != nil || !wrote {
		t.Fatalf("another binary: %v %v", wrote, err)
	}
	v2 := fstest.MapFS{"skills/a/SKILL.md": {Data: []byte("a2")}}
	if wrote, err := Write(dir, v2, "r2"); err != nil || !wrote {
		t.Fatalf("other files: %v %v", wrote, err)
	}
	if got := tree(t, dir); len(got) != 1 || got["skills/a/SKILL.md"] != "a2" {
		t.Fatalf("dir %v", got)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), ".plugin-*")); len(left) != 0 {
		t.Fatalf("left behind %v", left)
	}
}

// failing is an FS whose one file can be read once, by the stamp, and
// not again, by the copy.
type failing struct {
	fstest.MapFS
	bad   string
	opens *int
}

func (f failing) ReadFile(name string) ([]byte, error) {
	if name == f.bad {
		if *f.opens++; *f.opens > 1 {
			return nil, errors.New("fake read failure")
		}
	}
	return f.MapFS.ReadFile(name)
}

// A write that fails partway leaves the old dir whole and its stamp as it
// was, and nothing beside it.
func TestFailedWriteLeavesTheOldDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugin")
	v1 := fstest.MapFS{"skills/a/SKILL.md": {Data: []byte("a1")}, "skills/b/SKILL.md": {Data: []byte("b1")}}
	if _, err := Write(dir, v1, "r1"); err != nil {
		t.Fatal(err)
	}
	before, _, _ := Written(dir)
	v2 := failing{MapFS: fstest.MapFS{"skills/a/SKILL.md": {Data: []byte("a2")}, "skills/b/SKILL.md": {Data: []byte("b2")}},
		bad: "skills/b/SKILL.md", opens: new(int)}
	if wrote, err := Write(dir, v2, "r2"); err == nil || wrote || !strings.Contains(err.Error(), "fake read failure") {
		t.Fatalf("wrote %v err %v", wrote, err)
	}
	if got := tree(t, dir); got["skills/a/SKILL.md"] != "a1" || got["skills/b/SKILL.md"] != "b1" || len(got) != 2 {
		t.Fatalf("old dir %v", got)
	}
	if after, _, _ := Written(dir); after != before {
		t.Fatalf("stamp %q, was %q", after, before)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), ".plugin-*")); len(left) != 0 {
		t.Fatalf("left behind %v", left)
	}
}

// fakeClaude points the claude command at testdata's fake, keeping its
// marketplace and install in a state dir; it returns the state dir and
// the fake's log.
func fakeClaude(t *testing.T) (state, log string) {
	t.Helper()
	fake, err := filepath.Abs("../../testdata/fake-claude-plugin.sh")
	if err != nil {
		t.Fatal(err)
	}
	state = t.TempDir()
	log = filepath.Join(state, "log")
	t.Setenv("BMESH_CLAUDE_COMMAND", "/bin/sh "+fake)
	t.Setenv("FAKE_CLAUDE_STATE", state)
	t.Setenv("FAKE_PLUGIN_LOG", log)
	t.Setenv("FAKE_PLUGIN_FAIL", "")
	return state, log
}

func calls(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if !strings.Contains(l, " list --json") {
			out = append(out, l)
		}
	}
	return out
}

// --install on a box whose buildermesh marketplace reads a checkout
// removes it, adds the plugin dir and installs from it; a second run
// changes nothing.
func TestInstallSwitchesTheMarketplace(t *testing.T) {
	state, log := fakeClaude(t)
	dir := t.TempDir()
	old := `[{"name":"buildermesh","source":"directory","path":"/home/x/repos/buildermesh"},{"name":"other","source":"github","repo":"o/other"}]`
	if err := os.WriteFile(filepath.Join(state, "marketplaces.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := Install(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"removed marketplace buildermesh (/home/x/repos/buildermesh)", "added marketplace buildermesh (" + dir + ")",
		"installed buildermesh@buildermesh (user scope)"}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines %q", lines)
	}
	if got := calls(t, log); strings.Join(got, "\n") != "plugin marketplace remove buildermesh\nplugin marketplace add "+dir+
		"\nplugin install buildermesh@buildermesh --scope user" {
		t.Fatalf("calls %q", got)
	}
	os.Remove(log)
	if lines, err := Install(context.Background(), dir); err != nil || len(lines) != 0 {
		t.Fatalf("second run: %q %v", lines, err)
	}
	if got := calls(t, log); len(got) != 0 {
		t.Fatalf("second run changed %q", got)
	}
	s, err := Status(context.Background())
	if err != nil || !s.Reads(dir) {
		t.Fatalf("status %+v %v", s, err)
	}
}

// A box with no buildermesh marketplace gets it added and installed; one
// from GitHub is replaced like a checkout's.
func TestInstallFreshAndFromGitHub(t *testing.T) {
	state, log := fakeClaude(t)
	dir := t.TempDir()
	if lines, err := Install(context.Background(), dir); err != nil || len(lines) != 2 {
		t.Fatalf("fresh: %q %v", lines, err)
	}
	if got := calls(t, log); len(got) != 2 || !strings.HasPrefix(got[0], "plugin marketplace add") {
		t.Fatalf("fresh calls %q", got)
	}
	gh := `[{"name":"buildermesh","source":"github","repo":"rickdiculousli/buildermesh"}]`
	if err := os.WriteFile(filepath.Join(state, "marketplaces.json"), []byte(gh), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := Install(context.Background(), dir)
	if err != nil || len(lines) != 3 || lines[0] != "removed marketplace buildermesh (rickdiculousli/buildermesh)" {
		t.Fatalf("github: %q %v", lines, err)
	}
}

// An install that fails once the old marketplace is gone says how to put
// it back.
func TestInstallFailureSaysHowToRestore(t *testing.T) {
	state, _ := fakeClaude(t)
	old := `[{"name":"buildermesh","source":"directory","path":"/home/x/repos/buildermesh"}]`
	if err := os.WriteFile(filepath.Join(state, "marketplaces.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_PLUGIN_FAIL", "install")
	t.Setenv("FAKE_PLUGIN_EXIT", "1")
	lines, err := Install(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "to put the old marketplace back: claude plugin marketplace add /home/x/repos/buildermesh && claude plugin install buildermesh@buildermesh --scope user") {
		t.Fatalf("err %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines %q", lines)
	}
}

// Without claude, Install and Status say so.
func TestNoClaude(t *testing.T) {
	t.Setenv("BMESH_CLAUDE_COMMAND", "")
	t.Setenv("PATH", t.TempDir())
	if _, err := Install(context.Background(), t.TempDir()); !errors.Is(err, ErrNoClaude) {
		t.Fatalf("install: %v", err)
	}
	if _, err := Status(context.Background()); !errors.Is(err, ErrNoClaude) {
		t.Fatalf("status: %v", err)
	}
}
