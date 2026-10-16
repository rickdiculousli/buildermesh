package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// onOS makes this box's OS goos for the test.
func onOS(t *testing.T, goos string) {
	t.Helper()
	saved := hostOS
	hostOS = goos
	t.Cleanup(func() { hostOS = saved })
}

// commitKind writes a kind file in the cwd repo and commits it, returning the commit.
func commitKind(t *testing.T, repo, name, body string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, ".bmesh", "routines", name+".toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", name)
	return gitOut(t, "rev-parse", "HEAD")
}

func TestQueueOSKindNeedsPushAndSaysWhere(t *testing.T) {
	c, ctx := setup(t)
	onOS(t, "linux")
	repo := kindRepo(t, map[string]string{"mac-build": "os = \"darwin\"\ndeterministic = true\ncommand = \"make\"\n"})
	_, errs, code := runCLI(t, as("app-001"), "routine", "queue", "mac-build")
	if code != 3 || errs != "bmesh: push first: mac-build runs on darwin, which fetches the commit from origin\n" {
		t.Fatalf("unpushed: %d %q", code, errs)
	}
	if n, err := mesh.Depth(ctx, c, "mac-build"); err != nil || n != 0 {
		t.Fatalf("depth %d %v", n, err)
	}
	// A recorded answer queues nothing, so it needs no push.
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "mac-build", Tree: tree, OK: true, Output: "built\n", By: "routine.mac-build.cmd-1"}); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runCLI(t, as("app-001"), "routine", "queue", "mac-build")
	if code != 0 || !strings.HasPrefix(out, "answered from result mac-build."+tree) {
		t.Fatalf("recorded: %d %q %q", code, out, errs)
	}
	bare := filepath.Join(t.TempDir(), "origin.git")
	meshtest.Git(t, t.TempDir(), "init", "-q", "--bare", bare)
	meshtest.Git(t, repo, "remote", "add", "origin", bare)
	meshtest.Git(t, repo, "push", "-q", "origin", "main")
	out, errs, code = runCLI(t, as("app-001"), "routine", "queue", "mac-build", "--rerun")
	id, ok := strings.CutPrefix(strings.TrimSpace(out), "queued ")
	if code != 0 || !ok || !strings.HasSuffix(id, " (runs on darwin)") {
		t.Fatalf("pushed: %d %q %q", code, out, errs)
	}
	// On its own OS the line is plain.
	onOS(t, "darwin")
	out, errs, code = runCLI(t, as("app-001"), "routine", "queue", "mac-build", "--rerun")
	if code != 0 || !strings.HasPrefix(out, "queued ") || strings.Contains(out, "runs on") {
		t.Fatalf("on darwin: %d %q %q", code, out, errs)
	}
}

func TestQueueRefusesCommitOSMainLacks(t *testing.T) {
	c, ctx := setup(t)
	onOS(t, "linux")
	repo := kindRepo(t, map[string]string{
		"surface": "mode = \"fresh\"\ndeterministic = true\ncommand = \"shot\"\n",
		"lint":    "command = \"lint\"\n",
	})
	meshtest.Git(t, repo, "checkout", "-q", "-b", "side")
	moved := commitKind(t, repo, "surface", "mode = \"fresh\"\nos = \"darwin\"\ndeterministic = true\ncommand = \"safari\"\n")
	_, errs, code := runCLI(t, as("app-001"), "routine", "queue", "surface")
	want := "bmesh: surface runs on darwin at " + moved[:8] + " but anywhere on main, so a job could reach a linux box: " +
		"land the os change first, or give it a kind name of its own\n"
	if code != 3 || errs != want {
		t.Fatalf("moved: %d %q, want %q", code, errs, want)
	}
	// After the record lookup: a recorded answer is no job.
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "surface", Tree: tree, OK: true, Output: "shot\n", By: "x"}); err != nil {
		t.Fatal(err)
	}
	if out, errs, code := runCLI(t, as("app-001"), "routine", "queue", "surface"); code != 0 || !strings.HasPrefix(out, "answered from result ") {
		t.Fatalf("recorded: %d %q %q", code, out, errs)
	}

	commitKind(t, repo, "mac-build", "os = \"darwin\"\ncommand = \"make\"\n")
	if _, errs, code := runCLI(t, as("app-001"), "routine", "queue", "mac-build"); code != 3 || errs != "bmesh: mac-build is not on main: land it first\n" {
		t.Fatalf("not on main: %d %q", code, errs)
	}
	// A commit that gives the kind no os runs anywhere: not checked.
	commitKind(t, repo, "lint", "command = \"lint --strict\"\n")
	if out, errs, code := runCLI(t, as("app-001"), "routine", "queue", "lint"); code != 0 || !strings.HasPrefix(out, "queued ") || strings.Contains(out, "runs on") {
		t.Fatalf("no os: %d %q %q", code, out, errs)
	}
	// With an origin, main is origin/main; one that can't be read is named.
	bare := filepath.Join(t.TempDir(), "origin.git")
	meshtest.Git(t, t.TempDir(), "init", "-q", "--bare", bare)
	meshtest.Git(t, repo, "remote", "add", "origin", bare)
	_, errs, code = runCLI(t, as("app-001"), "routine", "queue", "mac-build")
	if code != 3 || !strings.HasPrefix(errs, "bmesh: cannot read kind mac-build on origin/main: ") {
		t.Fatalf("unreadable main: %d %q", code, errs)
	}
	// Main moved to darwin too: the same os passes, to the push check.
	meshtest.Git(t, repo, "push", "-q", "origin", "side:main")
	out, errs, code := runCLI(t, as("app-001"), "routine", "queue", "mac-build")
	if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), "(runs on darwin)") {
		t.Fatalf("on main: %d %q %q", code, out, errs)
	}
}

func TestRunRefusesOSKindOfAnotherOS(t *testing.T) {
	setup(t)
	onOS(t, "linux")
	kindRepo(t, map[string]string{"mac-build": "os = \"darwin\"\ndeterministic = true\ncommand = \"touch ran\"\n"})
	for _, args := range [][]string{{"routine", "run", "mac-build"}, {"routine", "run", "mac-build", "--here"}} {
		_, errs, code := runCLI(t, as("app-001"), args...)
		if code != 3 || errs != "bmesh: mac-build runs on darwin; this box is linux: queue it\n" {
			t.Fatalf("%v: %d %q", args, code, errs)
		}
	}
	if exists("ran") {
		t.Fatal("ran on the wrong OS")
	}
}

func TestStatusShowsInstanceHost(t *testing.T) {
	c, ctx := setup(t)
	kindRepo(t, map[string]string{"mac-build": "os = \"darwin\"\ncommand = \"make\"\n"})
	putNode(t, c, ctx, mesh.Node{Path: "routine.mac-build.cmd-1", Role: "routine", Host: "ricks-macbook"})
	out, errs, code := runCLI(t, nil, "routine", "status", "mac-build")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 2 || strings.Join(strings.Fields(lines[0]), " ") != "KIND MODE DETERMINISTIC RUN DEPTH INSTANCE HOST STATE MODEL" ||
		strings.Join(strings.Fields(lines[1]), " ") != "mac-build pooled false make 0 routine.mac-build.cmd-1 ricks-macbook idle -" {
		t.Fatalf("status: %d %q %q", code, out, errs)
	}
	out, _, code = runCLI(t, nil, "routine", "status", "mac-build", "--json")
	if code != 0 || !strings.Contains(out, `"members":[{"path":"routine.mac-build.cmd-1","host":"ricks-macbook","state":"idle"`) {
		t.Fatalf("status json: %d %q", code, out)
	}
}
