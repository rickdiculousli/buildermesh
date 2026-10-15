package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// leadSetup is setup plus a t3 runner backed by testdata/fake-t3.sh and a
// [repos] demo entry; it returns the fake's log.
func leadSetup(t *testing.T) (*mesh.Client, string) {
	t.Helper()
	c, _ := setup(t)
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-t3.sh"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMESH_T3_COMMAND", "sh "+fake)
	log := filepath.Join(t.TempDir(), "t3.log")
	t.Setenv("FAKE_T3_LOG", log)
	repo := t.TempDir()
	cfg := os.Getenv("BMESH_CONFIG")
	f, err := os.OpenFile(cfg, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	extra := "[runner]\nkind = \"t3\"\n[repos]\ndemo = \"" + repo + "\"\n[models]\nlead = \"lead-model\"\n"
	if _, err := f.WriteString(extra); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return c, log
}

func TestLeadStartRegistersNode(t *testing.T) {
	c, log := leadSetup(t)
	ctx := t.Context()
	out, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	var got struct{ Path, Thread, Branch, Worktree string }
	if err := json.Unmarshal([]byte(out), &got); err != nil || strings.Count(out, "\n") != 1 {
		t.Fatalf("output %q: %v", out, err)
	}
	n, ok, err := mesh.GetNode(ctx, c, "demo-012")
	if err != nil || !ok {
		t.Fatalf("node %v %v", ok, err)
	}
	if got.Path != "demo-012" || n.Runner.Thread != got.Thread || n.Runner.Kind != "t3" || len(got.Thread) != 36 ||
		n.Branch != "t3code/"+got.Thread || n.Worktree != "/wt/"+got.Thread || got.Worktree != n.Worktree {
		t.Fatalf("output %+v node %+v", got, n)
	}
	if n.Role != "lead" || n.Repo != "demo" || n.Spec != "012" || n.Host != testHost || n.Model != "lead-model" ||
		n.StartedAt == "" || n.RepoPath == "" {
		t.Fatalf("node %+v", n)
	}
	if s := stateOf(t, c, ctx, "demo-012"); s != "idle" {
		t.Fatalf("state %s", s)
	}
	l, _ := os.ReadFile(log)
	if !strings.Contains(string(l), "--model lead-model --title [build] demo 012 --prompt-file /dev/null --thread-id "+got.Thread) {
		t.Fatalf("log %q", l)
	}
}

func TestLeadStartRefusesTakenPath(t *testing.T) {
	c, log := leadSetup(t)
	ctx := t.Context()
	if out, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null"); code != 0 {
		t.Fatalf("first: %d %q %q", code, out, errs)
	}
	if _, err := mesh.WriteState(ctx, c, "demo-012", mesh.State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(log)
	out, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null")
	if code != 3 || out != "" || errs != "bmesh: refused: path demo-012 is taken (state working); use --path demo-012-2\n" {
		t.Fatalf("second: %d %q %q", code, out, errs)
	}
	if after, _ := os.ReadFile(log); string(after) != string(before) {
		t.Fatalf("runner called on refusal: %q", after)
	}
	// --path picks a free one.
	out, errs, code = runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null",
		"--path", "demo-012-2")
	if code != 0 || !strings.Contains(out, `"path":"demo-012-2"`) {
		t.Fatalf("--path: %d %q %q", code, out, errs)
	}
	// A done path can be started again, and reads idle afterwards.
	if _, err := mesh.WriteState(ctx, c, "demo-012", mesh.State{State: "done", By: "node"}); err != nil {
		t.Fatal(err)
	}
	if out, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null"); code != 0 {
		t.Fatalf("restart done: %d %q %q", code, out, errs)
	}
	if s := stateOf(t, c, ctx, "demo-012"); s != "idle" {
		t.Fatalf("state after restart %s", s)
	}
	if _, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012"); code != 2 {
		t.Fatalf("no --brief: %d %q", code, errs)
	}
}

func TestLeadStartTimeoutNamesThread(t *testing.T) {
	c, log := leadSetup(t)
	t.Setenv("BMESH_T3_TIMEOUT", "300ms")
	t.Setenv("FAKE_T3_START_SLEEP", "3")
	out, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null")
	m := regexp.MustCompile(`^bmesh: start timed out; thread id ([0-9a-f-]{36}) may exist\n$`).FindStringSubmatch(errs)
	if code != 1 || out != "" || m == nil {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	if l, _ := os.ReadFile(log); !strings.Contains(string(l), "--thread-id "+m[1]) {
		t.Fatalf("thread id not the one passed: %q", l)
	}
	if _, ok, err := mesh.GetNode(t.Context(), c, "demo-012"); ok || err != nil {
		t.Fatalf("node registered after timeout: %v %v", ok, err)
	}
}
