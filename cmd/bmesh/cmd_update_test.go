package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/plugin"
	"github.com/rickdiculousli/buildermesh/internal/upgrade"
)

// Notes for notesBox: v0.1.0's start-over, v0.2.0's ask and run step,
// v0.3.0's check, each step leaving a line in <state dir>/ran.
const (
	notesV010 = `summary = "start over"
start_over = true
loses = "the old store"

[[step]]
kind = "run"
why = "uninstall the old install"
command = "echo start-over >> \"$BMESH_STATE_DIR/ran\""
confirm = true
`
	notesV020 = `summary = "track follows releases"

[[step]]
kind = "ask"
key = "[update] track"
changed = "boxes now update to releases, not every landing."
question = "Follow releases or every landing?"
choices = ["release", "latest"]
default = "release"

[[step]]
kind = "run"
why = "restart bmesh-hub"
command = "echo v0.2.0 >> \"$BMESH_STATE_DIR/ran\""
`
	notesV030 = `[[step]]
kind = "check"
command = "cat \"$BMESH_STATE_DIR/ok\""
expect = "ok"
why = "the box is ready"

[[step]]
kind = "ask"
key = "[hub] max_file_store"
question = "How much store for the hub?"
default = "8GB"
when = '[hub] role == "hub"'
`
)

// notesBox is updateBox with a bare origin and four releases' worth of
// history: shas[0] from before v0.1.0, v0.1.0 at shas[1], v0.2.0 and v0.3.0
// after it, and the notes of all three written on main after the last tag.
// The state dir is the test's own.
func notesBox(t *testing.T) (exe, repo string, shas []string, tags map[string]string) {
	t.Helper()
	exe, shas = updateBox(t)
	repo = filepath.Join(filepath.Dir(filepath.Dir(exe)), "bm")
	origin := filepath.Join(filepath.Dir(repo), "origin.git")
	meshtest.Git(t, filepath.Dir(repo), "init", "-q", "--bare", "-b", "main", origin)
	meshtest.Git(t, repo, "remote", "add", "origin", origin)
	tags = map[string]string{"v0.1.0": shas[1]}
	meshtest.Git(t, repo, "tag", "-a", "-m", "v0.1.0", "v0.1.0", shas[1])
	tags["v0.2.0"] = meshtest.CommitFiles(t, repo, map[string]string{"x.go": "package x\n"})
	meshtest.Git(t, repo, "tag", "-a", "-m", "v0.2.0", "v0.2.0")
	tags["v0.3.0"] = meshtest.CommitFiles(t, repo, map[string]string{"y.go": "package x\n"})
	meshtest.Git(t, repo, "tag", "-a", "-m", "v0.3.0", "v0.3.0")
	meshtest.CommitFiles(t, repo, map[string]string{
		upgrade.Path("v0.1.0"): notesV010, upgrade.Path("v0.2.0"): notesV020, upgrade.Path("v0.3.0"): notesV030})
	meshtest.Git(t, repo, "push", "-q", "origin", "main", "--tags")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return exe, repo, shas, tags
}

// asNewBinary makes the binary update builds walk as this test binary does
// (testdata/fake-go.sh execs $FAKE_GO_BMESH for any command but version).
func asNewBinary(t *testing.T) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_GO_BMESH", self)
	t.Setenv("BMESH_TEST_AS_BMESH", "1")
	t.Setenv("BMESH_TEST_REJECT", "")
}

func stateFile(t *testing.T, name string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "bmesh", name))
	return string(b)
}

func binaryIs(t *testing.T, exe, want string) {
	t.Helper()
	if want == "old" {
		if b, _ := os.ReadFile(exe); string(b) != "old" {
			t.Fatalf("binary %q, want the old one kept", b)
		}
		return
	}
	got, err := exec.Command(exe).Output()
	if err != nil || strings.TrimSpace(string(got)) != "fake bmesh "+want {
		t.Fatalf("binary: %q %v, want %s", got, err, want)
	}
}

func TestUpdatePlanListsTheStepsBetween(t *testing.T) {
	exe, _, shas, tags := notesBox(t)
	withRevision(t, shas[1])
	out, errOut, code := runCLI(t, nil, "update", "--plan", "--json")
	if code != 0 {
		t.Fatalf("code %d err %q", code, errOut)
	}
	var p planOut
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if p.From != "v0.1.0" || p.To != "v0.3.0" || strings.Join(p.Releases, ",") != "v0.2.0,v0.3.0" || p.Asks != 1 || p.Steps != 2 ||
		p.StartOver || p.Config != "ok" || len(p.Plan) != 4 || p.Plan[0].ID != "v0.2.0#1" || p.Plan[0].Default != "release" ||
		p.Plan[2].Kind != upgrade.Check || p.Plan[3].Applies {
		t.Fatalf("plan %+v", p)
	}
	out, _, code = runCLI(t, nil, "update", "--plan")
	if code != 0 || !strings.HasPrefix(out, "v0.1.0 -> v0.3.0: 1 ask, 2 steps\n  v0.2.0#1 ask [update] track: ") {
		t.Fatalf("text plan: code %d out %q", code, out)
	}
	binaryIs(t, exe, "old")
	if stateFile(t, "upgrade.json") != "" {
		t.Fatal("--plan wrote the walk's state")
	}
	// A current binary with nothing pending plans nothing, from any
	// directory.
	sd := filepath.Join(os.Getenv("XDG_STATE_HOME"), "bmesh")
	if err := upgrade.WriteInstalled(sd, upgrade.Record{Version: "v0.3.0", Revision: tags["v0.3.0"]}); err != nil {
		t.Fatal(err)
	}
	withRevision(t, tags["v0.3.0"])
	t.Chdir(t.TempDir())
	out, errOut, code = runCLI(t, nil, "update", "--plan", "--json")
	p = planOut{}
	if code != 0 || json.Unmarshal([]byte(out), &p) != nil || p.Asks+p.Steps != 0 || len(p.Releases) != 0 || p.To != "v0.3.0" {
		t.Fatalf("current: code %d out %q err %q", code, out, errOut)
	}
}

// A walk with no terminal needs every ask answered; a config the new
// binary rejects keeps the old binary.
func TestUpdateRefusesBeforeTheSwap(t *testing.T) {
	exe, _, shas, _ := notesBox(t)
	asNewBinary(t)
	withRevision(t, shas[1])
	_, errOut, code := runCLI(t, nil, "update", "--apply")
	if code != exitRefused || !strings.Contains(errOut, "1 ask waits on an answer ([update] track): pass --answer") {
		t.Fatalf("no answer: code %d err %q", code, errOut)
	}
	binaryIs(t, exe, "old")
	if _, _, code := runCLI(t, nil, "update", "--answer", "track=sometimes"); code != exitUsage {
		t.Fatalf("an answer outside the choices: code %d", code)
	}
	binaryIs(t, exe, "old")
	t.Setenv("BMESH_TEST_REJECT", "[runner] t3_server is empty")
	_, errOut, code = runCLI(t, nil, "update", "--defaults", "--apply")
	if code != exitRefused || !strings.Contains(errOut, "config check failed: config ") || !strings.Contains(errOut, "[runner] t3_server is empty") {
		t.Fatalf("rejected config: code %d err %q", code, errOut)
	}
	binaryIs(t, exe, "old")
}

// --defaults writes the defaults (keeping config.toml.prev), a failing step
// stops the walk after the swap, and the next update resumes after the
// last step done.
func TestUpdateWalksTheNotesAndResumes(t *testing.T) {
	exe, _, shas, tags := notesBox(t)
	asNewBinary(t)
	withRevision(t, shas[1])
	out, errOut, code := runCLI(t, nil, "update", "--defaults", "--apply")
	if code != exitError || !strings.Contains(errOut, "step v0.3.0#1 failed") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	for _, want := range []string{"v0.1.0 -> v0.3.0: 1 ask, 2 steps\n", "v0.1.0 -> v0.3.0\n", "v0.2.0#2 restart bmesh-hub: "} {
		if !strings.Contains(out, want) {
			t.Fatalf("out %q lacks %q", out, want)
		}
	}
	binaryIs(t, exe, tags["v0.3.0"])
	cfg, _ := os.ReadFile(os.Getenv("BMESH_CONFIG"))
	if !strings.Contains(string(cfg), "[update]\ntrack = \"release\"\n") {
		t.Fatalf("config %q", cfg)
	}
	if _, err := os.Stat(os.Getenv("BMESH_CONFIG") + ".prev"); err != nil {
		t.Fatalf("config.toml.prev: %v", err)
	}
	if got := stateFile(t, "ran"); got != "v0.2.0\n" {
		t.Fatalf("ran %q", got)
	}
	if stateFile(t, "installed") != "" {
		t.Fatal("installed written before the walk finished")
	}
	// The new binary runs the next update: it resumes after v0.2.0#2.
	if err := os.WriteFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "bmesh", "ok"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withRevision(t, tags["v0.3.0"])
	out, errOut, code = runCLI(t, nil, "update", "--apply")
	if code != 0 || !strings.HasPrefix(out, "up to date\nv0.3.0#1 check ") || !strings.HasSuffix(out, "ok\nat v0.3.0\n") {
		t.Fatalf("resume: code %d out %q err %q", code, out, errOut)
	}
	if got := stateFile(t, "ran"); got != "v0.2.0\n" {
		t.Fatalf("ran again: %q", got)
	}
	if rec := stateFile(t, "installed"); !strings.HasPrefix(rec, "v0.3.0 "+tags["v0.3.0"]+" ") {
		t.Fatalf("installed %q", rec)
	}
	if stateFile(t, "upgrade.json") != "" {
		t.Fatal("upgrade.json left after the walk")
	}
	if out, _, code := runCLI(t, nil, "update"); code != 0 || out != "up to date\n" {
		t.Fatalf("after: code %d out %q", code, out)
	}
}

// --answer overrides a default; without --apply and a terminal the steps
// wait.
func TestUpdateAnswerAndStepsWait(t *testing.T) {
	exe, _, shas, tags := notesBox(t)
	asNewBinary(t)
	withRevision(t, shas[1])
	out, errOut, code := runCLI(t, nil, "update", "--answer", "track=latest")
	if code != 0 || !strings.Contains(out, "the steps from v0.2.0#2 wait: run bmesh update --apply") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	binaryIs(t, exe, tags["v0.3.0"])
	if cfg, _ := os.ReadFile(os.Getenv("BMESH_CONFIG")); !strings.Contains(string(cfg), "track = \"latest\"") {
		t.Fatalf("config %q", cfg)
	}
	if stateFile(t, "ran") != "" {
		t.Fatal("a step ran without --apply")
	}
}

// From a revision before v0.1.0 the walk starts with v0.1.0's start-over;
// from v0.1.0 it walks the two later releases in order.
func TestUpdateWalkFromBeforeTheFirstRelease(t *testing.T) {
	_, repo, shas, tags := notesBox(t)
	withRevision(t, tags["v0.3.0"])
	plan := func(from string) planOut {
		t.Helper()
		out, errOut, code := runCLI(t, nil, "update", "--plan", "--json", "--from", from, "--dir", repo)
		var p planOut
		if code != 0 || json.Unmarshal([]byte(out), &p) != nil {
			t.Fatalf("plan from %s: code %d out %q err %q", from, code, out, errOut)
		}
		return p
	}
	if p := plan(shas[0]); strings.Join(p.Releases, ",") != "v0.1.0,v0.2.0,v0.3.0" || !p.StartOver || p.Config != "start-over" ||
		p.Plan[0].ID != "v0.1.0#1" || !p.Plan[0].Confirm {
		t.Fatalf("from before v0.1.0: %+v", p)
	}
	if p := plan(shas[1]); strings.Join(p.Releases, ",") != "v0.2.0,v0.3.0" || p.StartOver {
		t.Fatalf("from v0.1.0: %+v", p)
	}
	if p := plan(""); strings.Join(p.Releases, ",") != "v0.2.0,v0.3.0" {
		t.Fatalf("no record: %+v", p)
	}
	walk := []string{"update", "--walk", "--from", shas[0], "--dir", repo, "--defaults", "--apply"}
	_, errOut, code := runCLI(t, nil, walk...)
	if code != exitRefused || !strings.Contains(errOut, "step v0.1.0#1 asks first: pass --yes") {
		t.Fatalf("no --yes: code %d err %q", code, errOut)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "bmesh", "ok"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runCLI(t, nil, append(walk, "--yes")...)
	if code != 0 || !strings.HasSuffix(out, "at v0.3.0\n") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	if got := stateFile(t, "ran"); got != "start-over\nv0.2.0\n" {
		t.Fatalf("ran %q", got)
	}
}

// A binary swapped in without a walk (by an older one, or the daemon)
// names the notes pending since this box's record, and update walks them.
func TestUpdatePendingNotes(t *testing.T) {
	_, _, _, tags := notesBox(t)
	withTaggedInstall(t, "example.com/bm", "v0.3.0")
	want := "bmesh: upgrade notes pending since v0.1.0: run bmesh update\n"
	if out, _, code := runCLI(t, nil, "update", "--notice"); code != 0 || out != want {
		t.Fatalf("no record: code %d out %q", code, out)
	}
	sd := filepath.Join(os.Getenv("XDG_STATE_HOME"), "bmesh")
	if err := upgrade.WriteInstalled(sd, upgrade.Record{Version: "v0.2.0", Revision: tags["v0.2.0"]}); err != nil {
		t.Fatal(err)
	}
	if out, _, _ := runCLI(t, nil, "update", "--notice"); out != "bmesh: upgrade notes pending since v0.2.0: run bmesh update\n" {
		t.Fatalf("record v0.2.0: %q", out)
	}
	if err := os.WriteFile(filepath.Join(sd, "ok"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runCLI(t, nil, "update", "--apply")
	if code != 0 || !strings.Contains(out, "v0.2.0 -> v0.3.0: 1 step\n") || !strings.HasSuffix(out, "at v0.3.0\n") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	if out, _, _ := runCLI(t, nil, "update", "--notice"); out != "" {
		t.Fatalf("after the walk: %q", out)
	}
}

// The walk after a swap is the new binary's: it writes the plugin it
// carries first, once; the walk before the swap writes nothing.
func TestUpdateWalkWritesThePlugin(t *testing.T) {
	_, repo, _, tags := notesBox(t)
	withRevision(t, tags["v0.3.0"])
	dir := plugin.Dir(filepath.Join(os.Getenv("XDG_STATE_HOME"), "bmesh"))
	walk := []string{"update", "--walk", "--from", tags["v0.3.0"], "--dir", repo}
	if _, errOut, code := runCLI(t, nil, append(walk, "--before-swap")...); code != 0 {
		t.Fatalf("before the swap: code %d err %q", code, errOut)
	}
	if _, _, ok := plugin.Written(dir); ok {
		t.Fatal("the walk before the swap wrote the plugin")
	}
	out, errOut, code := runCLI(t, nil, walk...)
	if code != 0 || !strings.HasPrefix(out, "plugin: wrote "+dir+" (bmesh ") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "work", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if out, _, code := runCLI(t, nil, walk...); code != 0 || strings.Contains(out, "plugin:") {
		t.Fatalf("second walk: code %d out %q", code, out)
	}
}
