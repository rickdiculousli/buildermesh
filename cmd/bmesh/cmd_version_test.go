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

func TestRestartCommand(t *testing.T) {
	for goos, want := range map[string]string{
		"linux":   "systemctl --user restart bmesh",
		"darwin":  "launchctl kickstart -k gui/$(id -u)/com.buildermesh.bmesh",
		"windows": "taskkill /F /IM bmesh.exe & schtasks /Run /TN buildermesh",
		"freebsd": "stop bmesh serve and start it again",
	} {
		if got := restartCommand(goos); got != want {
			t.Errorf("%s: %q, want %q", goos, got, want)
		}
	}
}

// withGOOS makes the restart name goos's service manager.
func withGOOS(t *testing.T, goos string) {
	t.Helper()
	saved := hostGOOS
	t.Cleanup(func() { hostGOOS = saved })
	hostGOOS = goos
}

func TestUpdateNamesDaemonRestart(t *testing.T) {
	c, ctx := setup(t)
	hub, err := os.ReadFile(os.Getenv("BMESH_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	_, shas := updateBox(t)
	f, err := os.OpenFile(os.Getenv("BMESH_CONFIG"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(hub); err != nil {
		t.Fatal(err)
	}
	f.Close()
	started := func(rev string) {
		t.Helper()
		if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "daemon." + testHost, Event: "started", Text: rev, Body: "2026-10-14T09:00:00Z"}); err != nil {
			t.Fatal(err)
		}
	}
	// No started for this host: no line.
	withRevision(t, shas[1])
	if out, errOut, code := runCLI(t, nil, "update"); code != 0 || out != "up to date\n" || errOut != "" {
		t.Fatalf("no daemon: code %d out %q err %q", code, out, errOut)
	}
	started(shas[0])
	for _, goos := range []string{"linux", "darwin", "windows"} {
		withGOOS(t, goos)
		want := "up to date\nthis box's daemon still runs " + shas[0][:8] + "; restart it so it runs " + shas[1][:8] + ": " + restartCommand(goos) + "\n"
		if out, errOut, code := runCLI(t, nil, "update"); code != 0 || out != want || errOut != "" {
			t.Fatalf("%s: code %d out %q err %q, want %q", goos, code, out, errOut, want)
		}
	}
	// After a swap the line names the binary now at the path, and the
	// hint, which compares the revision the swap left behind, is not
	// printed.
	withGOOS(t, "linux")
	withRevision(t, shas[0])
	started(strings.Repeat("9e", 20))
	want := shas[0][:8] + " -> " + shas[1][:8] + "\nthis box's daemon still runs 9e9e9e9e; restart it so it runs " + shas[1][:8] + ": systemctl --user restart bmesh\n"
	if out, errOut, code := runCLI(t, nil, "update"); code != 0 || out != want || errOut != "" {
		t.Fatalf("swap: code %d out %q err %q, want %q", code, out, errOut, want)
	}
	// The daemon runs the binary at the path: no line.
	started(shas[1])
	withRevision(t, shas[1])
	if out, errOut, code := runCLI(t, nil, "update"); code != 0 || out != "up to date\n" || errOut != "" {
		t.Fatalf("matching: code %d out %q err %q", code, out, errOut)
	}
}

func TestRevisionHintNamesDaemonRestart(t *testing.T) {
	c, ctx := setup(t)
	withGOOS(t, "darwin")
	bin, daemonRev := strings.Repeat("1a", 20), strings.Repeat("9e", 20)
	withRevision(t, bin) // committed 2026-10-15T09:00:00Z
	started := func(from, at string) {
		t.Helper()
		if err := mesh.PublishEvent(ctx, c, mesh.Event{From: from, Event: "started", Text: daemonRev, Body: at}); err != nil {
			t.Fatal(err)
		}
	}
	// Another host's older daemon is that host's to update.
	started("daemon.mac", "2026-10-14T09:00:00Z")
	if _, errOut, _ := runCLI(t, nil, "mesh-status"); errOut != "bmesh: mac runs 9e9e9e9e, this is 1a1a1a1a; run bmesh update\n" {
		t.Fatalf("another host: %q", errOut)
	}
	// This host's daemon runs an older commit: restart it.
	started("daemon."+testHost, "2026-10-14T09:00:00Z")
	want := "bmesh: this box's daemon runs 9e9e9e9e, this binary is 1a1a1a1a; restart the daemon: launchctl kickstart -k gui/$(id -u)/com.buildermesh.bmesh\n"
	if _, errOut, code := runCLI(t, nil, "mesh-status"); code != 0 || errOut != want {
		t.Fatalf("older own daemon: code %d err %q, want %q", code, errOut, want)
	}
	// A newer daemon, or one whose time is unknown, means this binary is
	// behind.
	update := "bmesh: " + testHost + " runs 9e9e9e9e, this is 1a1a1a1a; run bmesh update\n"
	for _, at := range []string{"2026-10-16T09:00:00Z", ""} {
		started("daemon."+testHost, at)
		if _, errOut, _ := runCLI(t, nil, "mesh-status"); errOut != update {
			t.Fatalf("own daemon at %q: %q, want %q", at, errOut, update)
		}
	}
}

// update follows the newest release; --latest goes past it to the code
// head once, and a later update keeps that newer binary.
func TestUpdateFollowsReleasesAndLatestOnce(t *testing.T) {
	exe, shas := updateBox(t)
	repo := filepath.Join(filepath.Dir(filepath.Dir(exe)), "bm")
	meshtest.Git(t, repo, "tag", "-a", "-m", "v0.1.0", "v0.1.0", shas[0])
	code := meshtest.CommitFiles(t, repo, map[string]string{"x.go": "package x\n"})
	meshtest.CommitFiles(t, repo, map[string]string{"docs/proposals/001-x.md": "x"}) // ledger after it

	withRevision(t, shas[0])
	if out, errOut, c := runCLI(t, nil, "update"); c != 0 || out != "up to date\n" {
		t.Fatalf("on the release: code %d out %q err %q", c, out, errOut)
	}
	out, errOut, c := runCLI(t, nil, "update", "--latest")
	if want := "v0.1.0 -> v0.1.0+2 (" + code[:8] + ")\n"; c != 0 || out != want {
		t.Fatalf("--latest: code %d out %q err %q, want %q", c, out, errOut, want)
	}
	if got, err := exec.Command(exe).Output(); err != nil || strings.TrimSpace(string(got)) != "fake bmesh "+code {
		t.Fatalf("binary: %q %v", got, err)
	}
	withRevision(t, code)
	if out, errOut, c := runCLI(t, nil, "update"); c != 0 || out != "ahead of v0.1.0, up to date\n" {
		t.Fatalf("ahead: code %d out %q err %q", c, out, errOut)
	}
	if got, _ := exec.Command(exe).Output(); strings.TrimSpace(string(got)) != "fake bmesh "+code {
		t.Fatalf("moved back: %q", got)
	}
	// The next release reaches a box still on the last one.
	meshtest.Git(t, repo, "tag", "-a", "-m", "v0.2.0", "v0.2.0", code)
	withRevision(t, shas[0])
	if out, errOut, c := runCLI(t, nil, "update"); c != 0 || out != "v0.1.0 -> v0.2.0\n" {
		t.Fatalf("release: code %d out %q err %q", c, out, errOut)
	}
	if _, _, c := runCLI(t, nil, "update", "--notice", "--latest"); c != exitUsage {
		t.Fatalf("--notice --latest: code %d", c)
	}
}

// withTaggedInstall makes the binary's build info that of `go install
// <module>/cmd/bmesh@<tag>`: a version and no commit.
func withTaggedInstall(t *testing.T, module, tag string) {
	t.Helper()
	saved := readBuildInfo
	t.Cleanup(func() { readBuildInfo = saved })
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Path: module, Version: tag}}, true
	}
}

// A tagged module install resolves its tag on the module's main and
// installs the next release by its tag.
func TestUpdateTaggedModuleInstall(t *testing.T) {
	exe, goLog, shas := upstreamBox(t)
	src := os.Getenv("BMESH_UPSTREAM_URL")
	meshtest.Git(t, src, "tag", "-a", "-m", "v0.1.0", "v0.1.0", shas[0])
	meshtest.Git(t, src, "tag", "-a", "-m", "v0.2.0", "v0.2.0", shas[1])
	withTaggedInstall(t, "example.com/up", "v0.1.0")
	if out, _, c := runCLI(t, nil, "version"); c != 0 || out != "bmesh v0.1.0\n" {
		t.Fatalf("version: code %d out %q", c, out)
	}
	out, errOut, c := runCLI(t, nil, "update")
	if want := "v0.1.0 -> v0.2.0\n" + pluginLine + "\n"; c != 0 || out != want {
		t.Fatalf("code %d out %q err %q, want %q", c, out, errOut, want)
	}
	if b, _ := os.ReadFile(goLog); strings.TrimSpace(string(b)) != "install example.com/up@v0.2.0" {
		t.Fatalf("go log %q", b)
	}
	if got, err := exec.Command(exe).Output(); err != nil || strings.TrimSpace(string(got)) != "fake bmesh v0.2.0" {
		t.Fatalf("binary: %q %v", got, err)
	}
	withTaggedInstall(t, "example.com/up", "v0.2.0")
	if out, _, c := runCLI(t, nil, "update"); c != 0 || out != "up to date\n" {
		t.Fatalf("current: code %d out %q", c, out)
	}
}
