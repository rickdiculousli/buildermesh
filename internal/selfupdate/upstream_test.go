package selfupdate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

func TestVersionFromPseudoVersion(t *testing.T) {
	stamp(t, "", "")
	for _, c := range []struct{ version, want string }{
		{"v0.0.0-20261016043719-ab7e0b543677", "ab7e0b54 2026-10-16T04:37:19Z"},
		{"v1.2.4-0.20261016043719-ab7e0b543677", "ab7e0b54 2026-10-16T04:37:19Z"},
		{"v1.2.3-pre.0.20261016043719-ab7e0b543677", "ab7e0b54 2026-10-16T04:37:19Z"},
		{"v2.0.0-20261016043719-ab7e0b543677+incompatible", "ab7e0b54 2026-10-16T04:37:19Z"},
		{"v1.2.3", "unknown"},           // a tag names no commit
		{"(devel)", "unknown"},          // a build from a checkout without VCS
		{"v0.0.0-2026-ab7e", "unknown"}, // not a pseudo-version
	} {
		v := FromBuildInfo(&debug.BuildInfo{Main: debug.Module{Path: "example.com/bm", Version: c.version}}, true)
		if v.String() != c.want {
			t.Errorf("%s: %q, want %q", c.version, v.String(), c.want)
		}
	}
	v := FromBuildInfo(&debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261016043719-ab7e0b543677"}}, true)
	if v.Revision != "ab7e0b543677" {
		t.Fatalf("revision %q", v.Revision)
	}
	// vcs settings and the stamp come first.
	bi := &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261016043719-ab7e0b543677"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: strings.Repeat("1a", 20)}}}
	if r := FromBuildInfo(bi, true).Revision; r != strings.Repeat("1a", 20) {
		t.Fatalf("vcs over pseudo: %q", r)
	}
	stamp(t, strings.Repeat("9f", 20), "2026-10-16T08:00:00Z")
	bi.Settings = nil
	if r := FromBuildInfo(bi, true).Revision; r != strings.Repeat("9f", 20) {
		t.Fatalf("stamp over pseudo: %q", r)
	}
}

func TestSameComparesByPrefix(t *testing.T) {
	full := "ab7e0b543677" + strings.Repeat("0", 28)
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{full, full, true},
		{"ab7e0b543677", full, true},
		{full, "ab7e0b54", true},
		{"ab7e0b543678", full, false},
		{"", full, false},
		{Unknown, Unknown, false},
		{"", "", false},
	} {
		if got := Same(c.a, c.b); got != c.want {
			t.Errorf("Same(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

// history is a repo whose main has a spec file and landings after base: it
// returns the repo and base.
func history(t *testing.T) (dir, base string) {
	t.Helper()
	dir, shas := repo(t, "base")
	commit := func(msg string, files map[string]string) {
		for name, body := range files {
			if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			meshtest.Git(t, dir, "add", name)
		}
		meshtest.Git(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
	}
	commit("Build 030", map[string]string{"docs/proposals/030-quiet.md": "---\nid: 030\ntitle: A quieter map\nstatus: done\n---\n"})
	commit("030: done at 1a2b3c4d", nil)
	commit("A fix", map[string]string{"f": "fix"})
	commit("031: done at 5e6f7a8b", map[string]string{"docs/proposals/031-notice.md": "---\nid: 031\ntitle: The notice\n---\n"})
	commit("099: done at 00000000", nil) // its spec is not on main
	return dir, shas[0]
}

func TestNoticeCountsAndNamesTheSpecs(t *testing.T) {
	dir, base := history(t)
	main := head(t, dir)
	line, err := Notice(dir, base, main)
	want := "bmesh: 5 commits behind main (landed since: 030 A quieter map, 031 The notice, 099); run bmesh update, then update the buildermesh plugin"
	if err != nil || line != want {
		t.Fatalf("%q %v\nwant %q", line, err, want)
	}
	// A pseudo-version's 12 hex count the same.
	if line, err := Notice(dir, base[:12], main); err != nil || line != want {
		t.Fatalf("12 hex: %q %v", line, err)
	}
	one, err := Notice(dir, meshtest.Git(t, dir, "rev-parse", "HEAD~1"), main)
	if err != nil || one != "bmesh: 1 commit behind main (landed since: 099); run bmesh update, then update the buildermesh plugin" {
		t.Fatalf("one behind: %q %v", one, err)
	}
	two, err := Notice(dir, meshtest.Git(t, dir, "rev-parse", "HEAD~2"), meshtest.Git(t, dir, "rev-parse", "HEAD~1"))
	if err != nil || two != "bmesh: 1 commit behind main (landed since: 031 The notice); run bmesh update, then update the buildermesh plugin" {
		t.Fatalf("031 alone: %q %v", two, err)
	}
	// Current, unknown, or off main's history: nothing.
	for _, own := range []string{main, main[:12], "", Unknown, strings.Repeat("e", 40)} {
		if line, err := Notice(dir, own, main); err != nil || line != "" {
			t.Fatalf("%q: %q %v", own, line, err)
		}
	}
}

func TestNoticeFileWrittenAndRemoved(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	if ReadNotice(state) != "" {
		t.Fatal("notice without a file")
	}
	if err := WriteNotice(state, "bmesh: 2 commits behind main"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(state, "update.txt")); string(b) != "bmesh: 2 commits behind main\n" {
		t.Fatalf("file %q", b)
	}
	if ReadNotice(state) != "bmesh: 2 commits behind main" {
		t.Fatalf("read %q", ReadNotice(state))
	}
	for range 2 { // removing twice is fine
		if err := WriteNotice(state, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(NoticePath(state)); !os.IsNotExist(err) {
		t.Fatalf("file left: %v", err)
	}
}

func TestFetchUpstreamAndInstallModule(t *testing.T) {
	log := fakeGo(t)
	src, shas := repo(t, "one", "two")
	t.Setenv("BMESH_UPSTREAM_URL", src)
	dir := UpstreamDir(t.TempDir())
	got, unlock, err := FetchUpstream(dir, "example.com/bm")
	if err != nil || got != shas[1] {
		t.Fatalf("head %q %v", got, err)
	}
	unlock()
	if !Contains(dir, shas[0], got) {
		t.Fatal("the fetch has no history")
	}
	meshtest.Git(t, src, "commit", "-q", "--allow-empty", "-m", "three")
	got, unlock, err = FetchUpstream(dir, "example.com/bm") // follows main
	if err != nil || got != head(t, src) {
		t.Fatalf("second fetch %q %v", got, err)
	}
	unlock()

	exe := filepath.Join(t.TempDir(), "bmesh")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := InstallModule(context.Background(), "example.com/bm", exe, got); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(exe).Output(); err != nil || strings.TrimSpace(string(out)) != "fake bmesh "+got {
		t.Fatalf("new binary: %q %v", out, err)
	}
	if b, _ := os.ReadFile(Prev(exe)); string(b) != "old" {
		t.Fatalf("prev %q", b)
	}
	if b, _ := os.ReadFile(log); strings.TrimSpace(string(b)) != "install example.com/bm@"+got {
		t.Fatalf("go log %q", b)
	}
	if Installed(context.Background(), exe) != got[:8] || Installed(context.Background(), Prev(exe)) != "" {
		t.Fatal("Installed")
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".bmesh-update-*")); len(left) != 0 {
		t.Fatalf("temporary dirs left: %v", left)
	}

	// A build naming another commit is refused, the binary kept.
	t.Setenv("FAKE_GO_REVISION", "eeeeeeee")
	err = InstallModule(context.Background(), "example.com/bm", exe, shas[0])
	var refused *Refused
	if !errors.As(err, &refused) || refused.Reported != "eeeeeeee" {
		t.Fatalf("err %v", err)
	}
	if out, _ := exec.Command(exe).Output(); strings.TrimSpace(string(out)) != "fake bmesh "+got {
		t.Fatalf("binary replaced: %q", out)
	}
	if _, _, err := FetchUpstream(dir, ""); err == nil {
		t.Fatal("no module fetched")
	}
}
