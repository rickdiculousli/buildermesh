package selfupdate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// bareOrigin is a bare repo pushed from a work repo with one code commit,
// and the work repo to commit and tag more in; push sends main and tags.
func bareOrigin(t *testing.T) (origin, work string, push func()) {
	t.Helper()
	work, _ = repo(t, "one")
	origin = filepath.Join(t.TempDir(), "origin.git")
	meshtest.Git(t, filepath.Dir(origin), "init", "-q", "--bare", "-b", "main", origin)
	push = func() { meshtest.Git(t, work, "push", "-q", "--tags", origin, "main") }
	push()
	return origin, work, push
}

// specs is the code of a row without code: everything but the spec files.
var specs = CodePaths(nil, nil, "docs/proposals/{id}-*.md")

// code commits a change outside the spec files in dir; ledger a spec file.
func code(t *testing.T, dir, name string) string {
	t.Helper()
	return meshtest.CommitFiles(t, dir, map[string]string{name: name})
}

func ledger(t *testing.T, dir, name string) string {
	t.Helper()
	return meshtest.CommitFiles(t, dir, map[string]string{"docs/proposals/" + name + ".md": name})
}

// CodeHead and CodeSame read a row's code, and the routine kinds always;
// without code, everything but the spec files is code, a README beside
// them included.
// A row's setup_inputs are code beside its code: a commit that only bumps
// a lockfile is a code head, and changes the code.
func TestCodePathsIncludeSetupInputs(t *testing.T) {
	dir, _ := repo(t, "one")
	paths := CodePaths([]string{"*.go"}, []string{":(glob)**/package-lock.json"}, "docs/proposals/{id}-*.md")
	if want := []string{"*.go", ":(glob)**/package-lock.json", AlwaysCode}; !slices.Equal(paths, want) {
		t.Fatalf("paths %q, want %q", paths, want)
	}
	goFile := meshtest.CommitFiles(t, dir, map[string]string{"x.go": "package x\n"})
	lock := meshtest.CommitFiles(t, dir, map[string]string{"web/package-lock.json": "{}\n"})
	ledger(t, dir, "001-a")
	if sha, err := CodeHead(dir, "HEAD", paths); err != nil || sha != lock {
		t.Fatalf("head %s %v, want the lockfile commit %s", sha, err, lock)
	}
	if same, err := CodeSame(dir, goFile, lock, paths); err != nil || same {
		t.Fatalf("a lockfile-only commit read as no code change: %v %v", same, err)
	}
}

func TestCodeHeadAndCodeSame(t *testing.T) {
	dir, _ := repo(t, "one")
	listed := CodePaths([]string{"*.go", "cmd/**"}, nil, "docs/proposals/{id}-*.md")
	head := func(code []string) string {
		t.Helper()
		sha, err := CodeHead(dir, "HEAD", code)
		if err != nil {
			t.Fatal(err)
		}
		return sha
	}
	same := func(a, b string, code []string) bool {
		t.Helper()
		ok, err := CodeSame(dir, a, b, code)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	goFile := meshtest.CommitFiles(t, dir, map[string]string{"internal/x/x.go": "package x\n"})
	readme := code(t, dir, "README.md")
	art := code(t, dir, "art/logo.svg")
	spec := ledger(t, dir, "001-a")
	if got := head(listed); got != goFile {
		t.Fatalf("with code: head %s, want the .go commit %s", got, goFile)
	}
	if got := head(specs); got != art {
		t.Fatalf("without code: head %s, want the svg commit %s", got, art)
	}
	if !same(goFile, spec, listed) || same(goFile, spec, specs) || !same(art, spec, specs) {
		t.Fatal("CodeSame: a README, an svg and a spec are not code with code set; only the spec without it")
	}
	if same(readme+"^{tree}", goFile+"~1^{tree}", listed) {
		t.Fatal("CodeSame across the .go commit")
	}
	indexReadme := meshtest.CommitFiles(t, dir, map[string]string{"docs/proposals/README.md": "how\n"})
	if head(specs) != indexReadme || same(spec, indexReadme, specs) {
		t.Fatal("without code, a README beside the spec files is code")
	}
	kind := meshtest.CommitFiles(t, dir, map[string]string{".bmesh/routines/verify.toml": "mode = \"inline\"\n"})
	if head(listed) != kind || same(indexReadme, kind, listed) {
		t.Fatal("a routine kind is code whatever code says")
	}
	if _, err := CodeSame(dir, "nope", kind, listed); err == nil {
		t.Fatal("CodeSame of an unknown rev")
	}
}

func TestFollowReleaseAndLatestOverATaggedOrigin(t *testing.T) {
	origin, work, push := bareOrigin(t)
	t.Setenv("BMESH_UPSTREAM_URL", origin)
	dir := UpstreamDir(t.TempDir())
	follow := func(track string) Target {
		t.Helper()
		_, unlock, err := FetchUpstream(dir, "example.com/bm")
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		tg, err := Follow(dir, "refs/heads/main", track, specs)
		if err != nil {
			t.Fatal(err)
		}
		return tg
	}
	// No tag yet: release follows the code head, and says so.
	c1 := code(t, work, "a")
	ledger(t, work, "001-a")
	push()
	if tg := follow(TrackRelease); tg != (Target{Commit: c1, NoTag: true}) {
		t.Fatalf("no tag %+v, want the code head %s", tg, c1[:8])
	}
	if tg := follow(TrackLatest); tg != (Target{Commit: c1}) {
		t.Fatalf("latest %+v", tg)
	}
	// A tag, pushed: release follows it; a later untagged code commit and a
	// ledger commit don't move it.
	meshtest.Git(t, work, "tag", "-a", "-m", "v0.1.0", "v0.1.0", c1)
	c2 := code(t, work, "b")
	ledger(t, work, "002-b")
	push()
	if tg := follow(TrackRelease); tg != (Target{Commit: c1, Tag: "v0.1.0"}) {
		t.Fatalf("release %+v", tg)
	}
	if tg := follow(TrackLatest); tg != (Target{Commit: c2}) {
		t.Fatalf("latest past the tag %+v", tg)
	}
	// The newest release wins by version, not by name order.
	meshtest.Git(t, work, "tag", "-a", "-m", "v0.10.0", "v0.10.0", c2)
	meshtest.Git(t, work, "tag", "-a", "-m", "v0.9.0", "v0.9.0", c1)
	meshtest.Git(t, work, "tag", "v0.11.0-rc1", c2) // a pre-release is no release
	push()
	if tg := follow(TrackRelease); tg != (Target{Commit: c2, Tag: "v0.10.0"}) {
		t.Fatalf("newest %+v", tg)
	}
	if tg := (Target{Commit: c2, Tag: "v0.10.0"}); tg.Ref() != "v0.10.0" || (Target{Commit: c2}).Ref() != c2 {
		t.Fatal("Ref")
	}
}

func TestStandAndLabel(t *testing.T) {
	dir, shas := repo(t, "one", "two", "three")
	meshtest.Git(t, dir, "tag", "-a", "-m", "v0.1.0", "v0.1.0", shas[0])
	for _, c := range []struct {
		own, target string
		want        Standing
	}{
		{shas[1], shas[1], Current},
		{shas[1][:12], shas[1], Current},
		{shas[2], shas[1], Ahead},
		{shas[0], shas[1], Behind},
		{"", shas[1], Apart},
		{strings.Repeat("e", 40), shas[1], Apart},
	} {
		if got := Stand(dir, c.own, c.target); got != c.want {
			t.Errorf("Stand(%.8s, %.8s) = %v, want %v", c.own, c.target, got, c.want)
		}
	}
	for commit, want := range map[string]string{
		shas[0]: "v0.1.0",
		shas[2]: "v0.1.0+2 (" + shas[2][:8] + ")",
		"":      "unknown",
	} {
		if got := Label(dir, commit); got != want {
			t.Errorf("Label(%.8s) = %q, want %q", commit, got, want)
		}
	}
	untagged, other := repo(t, "x")
	if got := Label(untagged, other[0]); got != other[0][:8] {
		t.Errorf("untagged %q", got)
	}
}

// A `go install <module>@v0.2.0` build names a version and no commit: it
// reads as that release, and an update resolves the tag to its commit.
func TestTaggedInstallResolvesItsCommit(t *testing.T) {
	stamp(t, "", "")
	v := FromBuildInfo(&debug.BuildInfo{Main: debug.Module{Path: "example.com/bm", Version: "v0.2.0"}}, true)
	if v.Revision != "" || v.Release != "v0.2.0" || v.String() != "v0.2.0" || v.Own() != "v0.2.0" {
		t.Fatalf("version %+v %q own %q", v, v.String(), v.Own())
	}
	if v := FromBuildInfo(&debug.BuildInfo{Main: debug.Module{Version: "v0.2.0-rc1"}}, true); v.Own() != "" || v.String() != Unknown {
		t.Fatalf("pre-release %+v", v)
	}
	if v := (Version{Revision: "abc", Release: "v0.2.0"}); v.Own() != "abc" {
		t.Fatalf("own %q", v.Own())
	}

	origin, work, push := bareOrigin(t)
	c1 := code(t, work, "a")
	meshtest.Git(t, work, "tag", "-a", "-m", "v0.2.0", "v0.2.0", c1)
	push()
	t.Setenv("BMESH_UPSTREAM_URL", origin)
	dir := UpstreamDir(t.TempDir())
	_, unlock, err := FetchUpstream(dir, "example.com/bm")
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if got := OwnCommit(dir, "v0.2.0"); got != c1 {
		t.Fatalf("OwnCommit %q, want %s", got, c1)
	}
	if got := OwnCommit(dir, "v0.3.0"); got != "" {
		t.Fatalf("an unknown tag resolved to %q", got)
	}
	if got := OwnCommit(dir, c1[:8]); got != c1[:8] {
		t.Fatalf("a revision %q", got)
	}

	// Installed reads the release a tagged binary prints.
	exe := filepath.Join(t.TempDir(), "bmesh")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho bmesh v0.2.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Installed(context.Background(), exe); got != "v0.2.0" {
		t.Fatalf("Installed %q", got)
	}

	// The module install names the tag, stamped with its commit.
	log := fakeGo(t)
	if err := InstallModule(context.Background(), dir, "example.com/bm", exe, Target{Commit: c1, Tag: "v0.2.0"}, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(log); strings.TrimSpace(string(b)) != "install example.com/bm@v0.2.0" {
		t.Fatalf("go log %q", b)
	}
	if got := Installed(context.Background(), exe); got != c1[:8] {
		t.Fatalf("installed %q, want %s", got, c1[:8])
	}
}

// Install builds a release behind the checkout's head there and leaves the
// checkout where it was.
func TestInstallBuildsTheTargetAndMovesBack(t *testing.T) {
	log := fakeGo(t)
	origin, work, push := bareOrigin(t)
	c1 := code(t, work, "a")
	meshtest.Git(t, work, "tag", "-a", "-m", "v0.1.0", "v0.1.0", c1)
	code(t, work, "b")
	push()
	dir, unlock, err := MainCheckout(origin, "", filepath.Join(t.TempDir(), "update"), "bm")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	main, _ := Head(dir)
	tg, err := Follow(dir, "HEAD", TrackRelease, specs)
	if err != nil || tg.Commit != c1 || tg.Tag != "v0.1.0" {
		t.Fatalf("the clone's release %+v %v", tg, err)
	}
	exe := filepath.Join(t.TempDir(), "bmesh")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), dir, exe, tg.Commit, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(log); strings.TrimSpace(string(b)) != "build "+c1 {
		t.Fatalf("go log %q", b)
	}
	if out, _ := exec.Command(exe, "version").Output(); !strings.HasPrefix(string(out), "bmesh v0.1.0 ("+c1[:8]) {
		t.Fatalf("version %q", out)
	}
	if h, _ := Head(dir); h != main {
		t.Fatalf("checkout left at %s, want %s", h, main)
	}
}
