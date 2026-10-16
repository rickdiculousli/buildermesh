package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

// withRevision makes the binary's build info name rev ("" = no VCS).
func withRevision(t *testing.T, rev string) {
	t.Helper()
	saved := readBuildInfo
	t.Cleanup(func() { readBuildInfo = saved })
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		bi := &debug.BuildInfo{Main: debug.Module{Path: "example.com/bm"}}
		if rev != "" {
			bi.Settings = []debug.BuildSetting{{Key: "vcs.revision", Value: rev}, {Key: "vcs.time", Value: "2026-10-15T09:00:00Z"}}
		}
		return bi, true
	}
}

func TestVersionReadsBuildInfo(t *testing.T) {
	t.Setenv("BMESH_CONFIG", filepath.Join(t.TempDir(), "absent", "config.toml")) // no setup needed
	withRevision(t, "1a2b3c4d5e6f7a8b9c0d1a2b3c4d5e6f7a8b9c0d")
	out, errOut, code := runCLI(t, nil, "version")
	if code != 0 || out != "bmesh 1a2b3c4d 2026-10-15T09:00:00Z\n" || errOut != "" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	withRevision(t, "")
	if out, _, code := runCLI(t, nil, "version"); code != 0 || out != "bmesh unknown\n" {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestRevisionHint(t *testing.T) {
	c, ctx := setup(t)
	daemonRev, other := strings.Repeat("1a", 20), strings.Repeat("9e", 20)
	withRevision(t, other)
	// No daemon has started: no hint.
	if _, errOut, code := runCLI(t, nil, "mesh-status"); code != 0 || errOut != "" {
		t.Fatalf("code %d err %q", code, errOut)
	}
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "daemon.k16", Event: "started", Text: daemonRev}); err != nil {
		t.Fatal(err)
	}
	hint := "bmesh: k16 runs 1a1a1a1a, this is 9e9e9e9e; run bmesh update\n"
	_, errOut, code := runCLI(t, nil, "mesh-status")
	if code != 0 || errOut != hint {
		t.Fatalf("code %d err %q, want %q", code, errOut, hint)
	}
	// On one stream (2>&1), the hint comes once, after the command's output
	// and its error, and leaves the exit code alone.
	both := func(args ...string) (string, int) {
		t.Helper()
		var b bytes.Buffer
		code := run(args, strings.NewReader(""), &b, &b)
		return b.String(), code
	}
	if err := mesh.PutNode(ctx, c, mesh.Node{Path: "app-001", Role: "lead", Host: "testhost"}); err != nil {
		t.Fatal(err)
	}
	if out, code := both("--node", "app-001", "inbox"); code != 0 || out != "inbox: empty\n"+hint {
		t.Fatalf("inbox: code %d out %q", code, out)
	}
	t.Chdir(t.TempDir()) // no node here
	if out, code := both("inbox"); code != exitUsage || out != "bmesh: no node for this directory; pass --node or set BMESH_NODE\n"+hint {
		t.Fatalf("refused inbox: code %d out %q", code, out)
	}
	// Commands that reach no hub get none.
	if _, errOut, _ := runCLI(t, nil, "version"); errOut != "" {
		t.Fatalf("version: %q", errOut)
	}
	if _, errOut, _ := runCLI(t, nil, "spec", "template"); errOut != "" {
		t.Fatalf("spec template: %q", errOut)
	}
	withRevision(t, daemonRev)
	if _, errOut, code := runCLI(t, nil, "mesh-status"); code != 0 || errOut != "" {
		t.Fatalf("same revision: code %d err %q", code, errOut)
	}
	withRevision(t, "") // a binary that does not know its revision says nothing
	if _, errOut, _ := runCLI(t, nil, "mesh-status"); errOut != "" {
		t.Fatalf("unknown revision: %q", errOut)
	}
}

func TestRevisionHintPrefersThisHostsDaemon(t *testing.T) {
	c, ctx := setup(t)
	own, newer := strings.Repeat("1a", 20), strings.Repeat("9e", 20)
	withRevision(t, own)
	// Only another host has a daemon: the hub's newest is used.
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "daemon.mac", Event: "started", Text: newer}); err != nil {
		t.Fatal(err)
	}
	hint := "bmesh: mac runs 9e9e9e9e, this is 1a1a1a1a; run bmesh update\n"
	if _, errOut, code := runCLI(t, nil, "mesh-status"); code != 0 || errOut != hint {
		t.Fatalf("no own daemon: code %d err %q, want %q", code, errOut, hint)
	}
	// This host's daemon runs this revision; another host's newer started
	// differs, and is not this host's business.
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "daemon." + testHost, Event: "started", Text: own}); err != nil {
		t.Fatal(err)
	}
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "daemon.mac", Event: "started", Text: newer}); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := runCLI(t, nil, "mesh-status"); code != 0 || errOut != "" {
		t.Fatalf("own daemon at this revision: code %d err %q", code, errOut)
	}
}

// updateBox is a box with repo "bm" on main (two commits, a go.mod naming
// example.com/bm), a checkouts root, the fake go, and a binary holding "old".
func updateBox(t *testing.T) (exe string, shas []string) {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "bm")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "init", "-q", "-b", "main")
	for i, body := range []string{"module example.com/bm\n", "module example.com/bm\n\ngo 1.26\n"} {
		if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		meshtest.Git(t, repo, "add", "go.mod")
		meshtest.Git(t, repo, "commit", "-q", "-m", "c"+string(rune('1'+i)))
		shas = append(shas, meshtest.Git(t, repo, "rev-parse", "HEAD"))
	}
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte("[runner]\ncheckouts = \""+filepath.Join(dir, "checkouts")+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "repos.toml"), []byte("[repos.bm]\npath = \""+repo+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMESH_CONFIG", cfg)
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-go.sh"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMESH_GO_COMMAND", "sh "+fake)
	t.Setenv("FAKE_GO_LOG", "")
	t.Setenv("FAKE_GO_REVISION", "")
	t.Setenv("INVOCATION_ID", "")
	exe = filepath.Join(dir, "bin", "bmesh")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := executable
	t.Cleanup(func() { executable = saved })
	executable = func() (string, error) { return exe, nil }
	return exe, shas
}

func TestUpdateUpToDate(t *testing.T) {
	exe, shas := updateBox(t)
	withRevision(t, shas[1])
	out, errOut, code := runCLI(t, nil, "update")
	if code != 0 || out != "up to date\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatalf("binary %q", b)
	}
}

func TestUpdateBuildsMainOverBinary(t *testing.T) {
	exe, shas := updateBox(t)
	withRevision(t, shas[0])
	out, errOut, code := runCLI(t, nil, "update") // the repo is found by its go.mod
	if want := shas[0][:8] + " -> " + shas[1][:8] + "\n"; code != 0 || out != want {
		t.Fatalf("code %d out %q err %q, want %q", code, out, errOut, want)
	}
	got, err := exec.Command(exe).Output()
	if err != nil || strings.TrimSpace(string(got)) != "fake bmesh "+shas[1] {
		t.Fatalf("binary: %q %v", got, err)
	}
	if b, _ := os.ReadFile(filepath.Join(filepath.Dir(exe), "bmesh.prev")); string(b) != "old" {
		t.Fatalf("bmesh.prev %q", b)
	}
	// A binary from a commit main lacks is left alone.
	withRevision(t, strings.Repeat("e", 40))
	if _, errOut, code := runCLI(t, nil, "update", "--repo", "bm"); code != exitRefused || !strings.Contains(errOut, "does not contain") {
		t.Fatalf("code %d err %q", code, errOut)
	}
}

func TestUpdateRefusesBuildOfAnotherRevision(t *testing.T) {
	exe, shas := updateBox(t)
	withRevision(t, shas[0])
	t.Setenv("FAKE_GO_REVISION", "eeeeeeee")
	out, errOut, code := runCLI(t, nil, "update")
	want := "bmesh: update: built binary reports eeeeeeee, not " + shas[1][:8] + "; keeping this binary\n"
	if code != exitRefused || out != "" || errOut != want {
		t.Fatalf("code %d out %q err %q, want %q", code, out, errOut, want)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatalf("binary %q", b)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), "bmesh.prev")); !os.IsNotExist(err) {
		t.Fatalf("bmesh.prev written: %v", err)
	}
}

// withModuleInstall makes the binary's build info that of `go install
// <module>/cmd/bmesh@<rev>`: no vcs settings, a pseudo-version naming rev.
func withModuleInstall(t *testing.T, module, rev string) {
	t.Helper()
	saved := readBuildInfo
	t.Cleanup(func() { readBuildInfo = saved })
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Path: module, Version: "v0.0.0-20261016043719-" + rev[:12]}}, true
	}
}

func TestVersionFromModuleInstall(t *testing.T) {
	t.Setenv("BMESH_CONFIG", filepath.Join(t.TempDir(), "absent", "config.toml"))
	withModuleInstall(t, "example.com/bm", "ab7e0b543677")
	if out, errOut, code := runCLI(t, nil, "version"); code != 0 || out != "bmesh ab7e0b54 2026-10-16T04:37:19Z\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestRevisionHintComparesByPrefix(t *testing.T) {
	c, ctx := setup(t)
	full := "ab7e0b543677" + strings.Repeat("0", 28)
	withModuleInstall(t, "example.com/bm", full)
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "daemon." + testHost, Event: "started", Text: full}); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := runCLI(t, nil, "mesh-status"); code != 0 || errOut != "" {
		t.Fatalf("12 hex against the full commit: code %d err %q", code, errOut)
	}
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "daemon." + testHost, Event: "started", Text: strings.Repeat("9e", 20)}); err != nil {
		t.Fatal(err)
	}
	if _, errOut, _ := runCLI(t, nil, "mesh-status"); errOut != "bmesh: "+testHost+" runs 9e9e9e9e, this is ab7e0b54; run bmesh update\n" {
		t.Fatalf("another commit: %q", errOut)
	}
}

// upstreamBox is a box with no registry row, the fake go, a binary holding
// "old" and a repo standing in for module example.com/up's main (two
// commits), which it returns.
func upstreamBox(t *testing.T) (exe, goLog string, shas []string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "up")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, src, "init", "-q", "-b", "main")
	for _, msg := range []string{"c1", "c2"} {
		meshtest.Git(t, src, "commit", "-q", "--allow-empty", "-m", msg)
		shas = append(shas, meshtest.Git(t, src, "rev-parse", "HEAD"))
	}
	t.Setenv("BMESH_UPSTREAM_URL", src)
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte("[runner]\ncheckouts = \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMESH_CONFIG", cfg)
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-go.sh"))
	if err != nil {
		t.Fatal(err)
	}
	goLog = filepath.Join(dir, "go.log")
	t.Setenv("BMESH_GO_COMMAND", "sh "+fake)
	t.Setenv("FAKE_GO_LOG", goLog)
	t.Setenv("FAKE_GO_EXIT", "")
	t.Setenv("FAKE_GO_REVISION", "")
	t.Setenv("INVOCATION_ID", "")
	exe = filepath.Join(dir, "bin", "bmesh")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := executable
	t.Cleanup(func() { executable = saved })
	executable = func() (string, error) { return exe, nil }
	return exe, goLog, shas
}

func TestUpdateFromUpstreamWithoutRow(t *testing.T) {
	exe, goLog, shas := upstreamBox(t)
	if err := selfupdate.WriteNotice(config.StateDir(), "bmesh: 1 commit behind main"); err != nil {
		t.Fatal(err)
	}
	withModuleInstall(t, "example.com/up", shas[0])
	out, errOut, code := runCLI(t, nil, "update")
	want := shas[0][:8] + " -> " + shas[1][:8] + "\n" + pluginLine + "\n"
	if code != 0 || out != want {
		t.Fatalf("code %d out %q err %q, want %q", code, out, errOut, want)
	}
	if got, err := exec.Command(exe).Output(); err != nil || strings.TrimSpace(string(got)) != "fake bmesh "+shas[1] {
		t.Fatalf("binary: %q %v", got, err)
	}
	if b, _ := os.ReadFile(filepath.Join(filepath.Dir(exe), "bmesh.prev")); string(b) != "old" {
		t.Fatalf("bmesh.prev %q", b)
	}
	if b, _ := os.ReadFile(goLog); strings.TrimSpace(string(b)) != "install example.com/up@"+shas[1] {
		t.Fatalf("go log %q", b)
	}
	if selfupdate.ReadNotice(config.StateDir()) != "" {
		t.Fatal("the notice outlived the update")
	}
	withModuleInstall(t, "example.com/up", shas[1])
	if out, _, code := runCLI(t, nil, "update"); code != 0 || out != "up to date\n" {
		t.Fatalf("current: code %d out %q", code, out)
	}
	// A binary from a commit main lacks is left alone, as from a row.
	withModuleInstall(t, "example.com/up", strings.Repeat("e", 12))
	if _, errOut, code := runCLI(t, nil, "update"); code != exitRefused || !strings.Contains(errOut, "example.com/up's main ("+shas[1][:8]+") does not contain this binary's eeeeeeee") {
		t.Fatalf("code %d err %q", code, errOut)
	}
}

func TestUpdateFromUpstreamRefusesAnotherRevision(t *testing.T) {
	exe, _, shas := upstreamBox(t)
	withModuleInstall(t, "example.com/up", shas[0])
	t.Setenv("FAKE_GO_REVISION", "eeeeeeee")
	out, errOut, code := runCLI(t, nil, "update")
	want := "bmesh: update: built binary reports eeeeeeee, not " + shas[1][:8] + "; keeping this binary\n"
	if code != exitRefused || out != "" || errOut != want {
		t.Fatalf("code %d out %q err %q, want %q", code, out, errOut, want)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatalf("binary %q", b)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), "bmesh.prev")); !os.IsNotExist(err) {
		t.Fatalf("bmesh.prev written: %v", err)
	}
}

func TestUpdateNotice(t *testing.T) {
	t.Setenv("BMESH_CONFIG", filepath.Join(t.TempDir(), "absent", "config.toml")) // no setup, no init
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("BMESH_UPSTREAM_URL", filepath.Join(t.TempDir(), "unreachable"))
	if out, errOut, code := runCLI(t, nil, "update", "--notice"); code != 0 || out != "" || errOut != "" {
		t.Fatalf("no file: code %d out %q err %q", code, out, errOut)
	}
	line := "bmesh: 2 commits behind main; run bmesh update, then update the buildermesh plugin"
	if err := selfupdate.WriteNotice(config.StateDir(), line); err != nil {
		t.Fatal(err)
	}
	if out, errOut, code := runCLI(t, nil, "update", "--notice"); code != 0 || out != line+"\n" || errOut != "" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	if _, _, code := runCLI(t, nil, "update", "--notice", "--repo", "x"); code != exitUsage {
		t.Fatalf("--notice with --repo: code %d", code)
	}
}

func TestMeshStatusClosesWithNotice(t *testing.T) {
	setup(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	line := "bmesh: 2 commits behind main; run bmesh update, then update the buildermesh plugin"
	if err := selfupdate.WriteNotice(config.StateDir(), line); err != nil {
		t.Fatal(err)
	}
	out, _, code := runCLI(t, nil, "mesh-status")
	if code != 0 || !strings.HasSuffix(out, "\n"+line+"\n") {
		t.Fatalf("code %d out %q", code, out)
	}
}
