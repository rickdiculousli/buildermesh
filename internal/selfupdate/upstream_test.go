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
		{"v1.2.3", "v1.2.3"},            // a tag names no commit: the release alone
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

// history is a repo whose main has two landed specs, a fix between them,
// and a landing whose commit it doesn't know: 030's code commit followed
// by its old `done at` ledger commit, 031's landed with its trailers. It
// returns the repo and its commits by name: base, c030, fix, c031 (the
// code head) and main.
func history(t *testing.T) (string, map[string]string) {
	t.Helper()
	dir, shas := repo(t, "base")
	at := map[string]string{"base": shas[0]}
	commit := func(name, msg string, files map[string]string) {
		for file, body := range files {
			if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(file)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			meshtest.Git(t, dir, "add", file)
		}
		meshtest.Git(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
		at[name] = head(t, dir)
	}
	commit("c030", "A quieter map", map[string]string{"q": "q"})
	commit("", "030: done at "+at["c030"][:8], map[string]string{"docs/proposals/030-quiet.md": "---\nid: 030\ntitle: A quieter map\nstatus: done\n---\n"})
	commit("fix", "A fix", map[string]string{"f": "fix"})
	commit("c031", "The notice\n\nSpec: 031\nVerified: verify ok t:00000000", map[string]string{"n": "n",
		"docs/proposals/031-notice.md": "---\nid: 031\ntitle: The notice\nstatus: done\n---\n"})
	commit("main", "099: done at 00000000", nil) // a commit this repo doesn't know
	return dir, at
}

func TestNoticeCountsAndNamesTheSpecs(t *testing.T) {
	dir, at := history(t)
	latest, err := Follow(dir, "main", TrackLatest, specs)
	if err != nil || latest.Commit != at["c031"] || latest.Tag != "" {
		t.Fatalf("latest %+v %v", latest, err)
	}
	notice := func(own string, tg Target) string {
		t.Helper()
		line, err := Notice(dir, own, "main", tg, specs)
		if err != nil {
			t.Fatal(err)
		}
		return line
	}
	// Code commits only: the ledger commits between them don't count.
	want := "bmesh: 3 commits behind main (landed since: 030 A quieter map, 031 The notice, 099); run bmesh update, then update the buildermesh plugin"
	if got := notice(at["base"], latest); got != want {
		t.Fatalf("%q\nwant %q", got, want)
	}
	// A pseudo-version's 12 hex count the same.
	if got := notice(at["base"][:12], latest); got != want {
		t.Fatalf("12 hex: %q", got)
	}
	// A spec whose code the binary has is not named again.
	if got := notice(at["c030"], latest); got != "bmesh: 2 commits behind main (landed since: 031 The notice, 099); run bmesh update, then update the buildermesh plugin" {
		t.Fatalf("from 030: %q", got)
	}
	if got := notice(at["fix"], latest); got != "bmesh: 1 commit behind main (landed since: 031 The notice, 099); run bmesh update, then update the buildermesh plugin" {
		t.Fatalf("one behind: %q", got)
	}
	// Current, ahead (a ledger commit past the code head), unknown, or off
	// main's history: nothing.
	for _, own := range []string{at["c031"], at["c031"][:12], at["main"], "", Unknown, strings.Repeat("e", 40)} {
		if got := notice(own, latest); got != "" {
			t.Fatalf("%q: %q", own, got)
		}
	}

	// Releases name the tag and what the binary runs.
	meshtest.Git(t, dir, "tag", "-a", "-m", "v0.1.0", "v0.1.0", at["c030"])
	meshtest.Git(t, dir, "tag", "-a", "-m", "v0.2.0", "v0.2.0", at["c031"])
	rel, err := Follow(dir, "main", TrackRelease, specs)
	if err != nil || rel.Commit != at["c031"] || rel.Tag != "v0.2.0" || rel.NoTag {
		t.Fatalf("release %+v %v", rel, err)
	}
	if got := notice(at["c030"], rel); got != "bmesh: v0.2.0 is out (you run v0.1.0: 031, 099); run bmesh update, then update the buildermesh plugin" {
		t.Fatalf("release: %q", got)
	}
	if got, want := notice(at["fix"], rel), "bmesh: v0.2.0 is out (you run v0.1.0+2 ("+at["fix"][:8]+"): 031, 099); run bmesh update, then update the buildermesh plugin"; got != want {
		t.Fatalf("release past a tag: %q\nwant %q", got, want)
	}
	if got := notice(at["main"], rel); got != "" {
		t.Fatalf("ahead of the release: %q", got)
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
	// go resolves a tagged commit to its tag, which names no revision: the
	// stamp names it all the same.
	t.Setenv("FAKE_GO_TAGGED", "1")
	if err := InstallModule(context.Background(), dir, "example.com/bm", exe, Target{Commit: got}, nil); err != nil {
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
	err = InstallModule(context.Background(), dir, "example.com/bm", exe, Target{Commit: shas[0]}, nil)
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
