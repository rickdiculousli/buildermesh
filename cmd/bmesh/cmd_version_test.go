package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
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

// updateBox is a box with repo "bm" on main (two commits, a go.mod naming
// example.com/bm), a checkouts root, the fake go, and a binary holding "old".
func updateBox(t *testing.T) (exe string, shas []string) {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "bm")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "init", "-q", "-b", "main")
	for i, body := range []string{"module example.com/bm\n", "module example.com/bm\n\ngo 1.26\n"} {
		if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		gitT(t, repo, "add", "go.mod")
		gitT(t, repo, "commit", "-q", "-m", "c"+string(rune('1'+i)))
		shas = append(shas, gitT(t, repo, "rev-parse", "HEAD"))
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

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
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
