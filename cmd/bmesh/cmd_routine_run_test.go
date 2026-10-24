package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

// marker points $MARK at a file outside the repo, so a command can show it
// ran without dirtying the worktree.
func marker(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "mark")
	t.Setenv("MARK", p)
	return p
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestInlineRunsAndRecords(t *testing.T) {
	c, ctx := setup(t)
	kindRepo(t, map[string]string{
		"verify": "mode = \"inline\"\ndeterministic = true\ncommand = \"echo ran $BMESH_TREE $BMESH_FROM\"\n",
		"broken": "mode = \"inline\"\ndeterministic = true\ncommand = \"echo nope; exit 3\"\n",
		"notes":  "mode = \"inline\"\ncommand = \"echo hi\"\n",
	})
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")
	head := gitOut(t, "rev-parse", "HEAD")
	key := mesh.ResultKey("verify", tree, []string{"a", "b"}, "")

	// The run's output, then its verdict line, which is not recorded.
	ran := "ran " + tree + " " + testHost + " a b\n"
	out, errs, code := runCLI(t, nil, "routine", "run", "verify", "--", "a", "b")
	if code != 0 || out != ran+"verify ok t:"+tree[:8]+" (recorded)\n" || errs != "" {
		t.Fatalf("run: %d %q %q", code, out, errs)
	}
	r, ok, err := mesh.GetResult(ctx, c, key)
	if err != nil || !ok || !r.OK || r.Output != ran || r.By != testHost || r.Commit != head ||
		r.Tree != tree || !slices.Equal(r.Args, []string{"a", "b"}) {
		t.Fatalf("result %+v %v %v", r, ok, err)
	}
	if out, errs, code := runCLI(t, nil, "result", "find", "verify", "--", "a", "b"); code != 0 || !strings.HasPrefix(out, "ok "+tree) {
		t.Fatalf("find: %d %q %q", code, out, errs)
	}

	out, errs, code = runCLI(t, nil, "routine", "run", "broken")
	if code != 1 || out != "nope\nbroken fail t:"+tree[:8]+" (recorded)\n" || errs != "" {
		t.Fatalf("broken: %d %q %q", code, out, errs)
	}
	if r, ok, err := mesh.GetResult(ctx, c, "broken."+tree); err != nil || !ok || r.OK || r.Output != "nope\n" {
		t.Fatalf("broken result %+v %v %v", r, ok, err)
	}

	// A non-deterministic kind records too, and a second run replaces the
	// record rather than being answered from it.
	out, errs, code = runCLI(t, nil, "routine", "run", "notes")
	if code != 0 || out != "hi\nnotes ok t:"+tree[:8]+" (recorded)\n" || errs != "" {
		t.Fatalf("notes: %d %q %q", code, out, errs)
	}
	rs, err := mesh.ListResults(ctx, c, "notes")
	if err != nil || len(rs) != 1 || !rs[0].OK || rs[0].Output != "hi\n" || rs[0].Model != "" {
		t.Fatalf("notes recorded %+v %v", rs, err)
	}
	first := rs[0].At
	time.Sleep(1100 * time.Millisecond) // At has one-second resolution
	out, errs, code = runCLI(t, nil, "routine", "run", "notes")
	if code != 0 || out != "hi\nnotes ok t:"+tree[:8]+" (recorded)\n" || errs != "" {
		t.Fatalf("notes again: %d %q %q", code, out, errs)
	}
	if rs, err := mesh.ListResults(ctx, c, "notes"); err != nil || len(rs) != 1 || rs[0].At == first {
		t.Fatalf("notes not replaced %+v %v", rs, err)
	}
}

func TestInlineRefusesDirtyTree(t *testing.T) {
	c, ctx := setup(t)
	repo := kindRepo(t, map[string]string{
		"verify": "mode = \"inline\"\ndeterministic = true\ncommand = \"touch \\\"$MARK\\\"\"\n",
		"notes":  "mode = \"inline\"\ncommand = \"touch \\\"$MARK\\\"\"\n",
	})
	mark := marker(t)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Every inline run records for HEAD's tree, deterministic or not.
	for _, k := range []string{"verify", "notes"} {
		_, errs, code := runCLI(t, nil, "routine", "run", k)
		if code != 3 || errs != "bmesh: worktree is dirty; commit first\n" {
			t.Fatalf("%s dirty: %d %q", k, code, errs)
		}
		if exists(mark) {
			t.Fatalf("%s ran on a dirty tree", k)
		}
		if rs, err := mesh.ListResults(ctx, c, k); err != nil || len(rs) != 0 {
			t.Fatalf("%s recorded %+v %v", k, rs, err)
		}
	}

	// An inline run runs HEAD; any other commit is refused.
	meshtest.Git(t, repo, "checkout", "a.txt")
	_, errs, code := runCLI(t, nil, "routine", "run", "verify", "--commit", "HEAD~1")
	if code != 3 || errs != "bmesh: inline runs run HEAD; check out HEAD~1 or queue the job\n" || exists(mark) {
		t.Fatalf("other commit: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "routine", "run", "verify", "--commit", "HEAD"); code != 0 || !exists(mark) {
		t.Fatalf("clean: %d %q", code, errs)
	}
}

func TestInlineAnswersFromRecord(t *testing.T) {
	c, ctx := setup(t)
	kindRepo(t, map[string]string{
		"verify": "mode = \"inline\"\ndeterministic = true\ncommand = \"touch \\\"$MARK\\\"; echo fixed\"\n",
	})
	mark := marker(t)
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "verify", Tree: tree, OK: false, Output: "broke\n", By: "elsewhere"}); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runCLI(t, nil, "routine", "run", "verify")
	if code != 1 || out != "broke\nverify fail t:"+tree[:8]+" (answered from the record)\n" || errs != "" || exists(mark) {
		t.Fatalf("answered: %d %q %q ran=%v", code, out, errs, exists(mark))
	}
	out, errs, code = runCLI(t, nil, "routine", "run", "verify", "--rerun")
	if code != 0 || out != "fixed\nverify ok t:"+tree[:8]+" (recorded)\n" || errs != "" || !exists(mark) {
		t.Fatalf("rerun: %d %q %q", code, out, errs)
	}
	if r, ok, err := mesh.GetResult(ctx, c, "verify."+tree); err != nil || !ok || !r.OK || r.By != testHost {
		t.Fatalf("record not replaced: %+v %v %v", r, ok, err)
	}
}

func TestInlineRefusesNonInlineWithoutHere(t *testing.T) {
	setup(t)
	kindRepo(t, map[string]string{"verify": "command = \"touch \\\"$MARK\\\"\"\n"})
	mark := marker(t)
	_, errs, code := runCLI(t, nil, "routine", "run", "verify")
	if code != 3 || errs != "bmesh: kind verify is pooled; queue it, or pass --here to run it in this worktree\n" || exists(mark) {
		t.Fatalf("pooled: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "routine", "run", "verify", "--here"); code != 0 || !exists(mark) {
		t.Fatalf("--here: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "routine", "run", "lint"); code != 3 || !strings.HasPrefix(errs, "bmesh: no kind lint in ") ||
		!strings.HasSuffix(errs, "; bmesh routine list shows its kinds\n") {
		t.Fatalf("unknown kind: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "routine", "run", "verify", "--here", "--model", "m1"); code != 2 {
		t.Fatalf("model on a command kind: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "routine", "run", "verify", "--here", "--effort", "high"); code != 2 || !strings.Contains(errs, "--effort is for a model kind") {
		t.Fatalf("effort on a command kind: %d %q", code, errs)
	}
}

// A repo with no verify kind is told how its verify is recorded.
func TestInlineRunNamesHowAVerifyWithNoKindIsRecorded(t *testing.T) {
	setup(t)
	kindRepo(t, map[string]string{"lint": "mode = \"inline\"\ncommand = \"true\"\n"})
	_, errs, code := runCLI(t, nil, "routine", "run", "verify")
	if code != 3 || !strings.HasPrefix(errs, "bmesh: no kind verify in ") ||
		!strings.HasSuffix(errs, "; run the repo's verify commands, then record them: bmesh result record verify --ok (or --fail)\n") {
		t.Fatalf("no verify kind: %d %q", code, errs)
	}
}

func TestInlineHoldsSlotLease(t *testing.T) {
	c, ctx := setup(t)
	repo := kindRepo(t, map[string]string{
		"game": "mode = \"inline\"\nslots = [\"game\"]\n" +
			"command = \"touch \\\"$MARK\\\"; while [ ! -e \\\"$GO\\\" ]; do sleep 0.05; done; echo played\"\n",
	})
	if err := os.WriteFile(filepath.Join(repo, ".bmesh", "config.toml"), []byte("[slots]\ngame = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "slots")
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")
	mark := marker(t)
	goFile := filepath.Join(t.TempDir(), "go")
	t.Setenv("GO", goFile)

	key, err := mesh.AcquireLease(ctx, c, "game", "someone", time.Minute, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, errs, code := runCLI(t, nil, "routine", "run", "game")
	if code != 3 || errs != "bmesh: slot game is busy\n" || exists(mark) {
		t.Fatalf("busy: %d %q", code, errs)
	}
	if err := mesh.ReleaseLease(ctx, c, key, "someone"); err != nil {
		t.Fatal(err)
	}

	res := make(chan [3]any, 1)
	go func() {
		var out, errb bytes.Buffer
		code := run([]string{"routine", "run", "game"}, strings.NewReader(""), &out, &errb)
		res <- [3]any{code, out.String(), errb.String()}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !exists(mark) {
		if time.Now().After(deadline) {
			t.Fatal("the command never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	leases, err := mesh.ListLeases(ctx, c)
	if err != nil || len(leases) != 1 || leases[0].Key != "game.0" || leases[0].Holder != testHost {
		t.Fatalf("lease during the run: %+v %v", leases, err)
	}
	if err := os.WriteFile(goFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-res:
		if r[0] != 0 || r[1] != "played\ngame ok t:"+tree[:8]+" (recorded)\n" || r[2] != "" {
			t.Fatalf("run: %v", r)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run never finished")
	}
	assertNoLeases(t, c, ctx)
}

func TestInlineModelKindThroughFake(t *testing.T) {
	c, ctx := setup(t)
	fake, err := filepath.Abs("../../testdata/fake-claude.sh")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "claude.log")
	t.Setenv("BMESH_CLAUDE_COMMAND", "sh "+fake)
	t.Setenv("FAKE_LOG", log)
	t.Setenv("FAKE_RESULT", "looks fine\nVERDICT: ok")
	kindRepo(t, map[string]string{
		"review": "mode = \"inline\"\ndeterministic = true\nmodels = [\"m1\", \"m2\"]\nprompt = \"check\"\n",
	})
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")

	out, errs, code := runCLI(t, nil, "routine", "run", "review", "--model", "m2", "--text", "look at it")
	if code != 0 || out != "looks fine\nVERDICT: ok\nreview ok t:"+tree[:8]+" m:m2 (recorded)\n" || errs != "" {
		t.Fatalf("run: %d %q %q", code, out, errs)
	}
	if r, ok, err := mesh.GetResult(ctx, c, "review."+tree+".m2"); err != nil || !ok || !r.OK || r.Model != "m2" {
		t.Fatalf("result %+v %v %v", r, ok, err)
	}
	b, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(b), "--model m2 ") {
		t.Fatalf("claude argv %q %v", b, err)
	}
	if stdin, err := os.ReadFile(log + ".stdin"); err != nil || !strings.Contains(string(stdin), "look at it") {
		t.Fatalf("claude stdin %q %v", stdin, err)
	}
	if strings.Contains(string(b), "--effort") {
		t.Fatalf("claude argv %q: want no --effort without one", b)
	}

	// --effort reaches claude; an effort the runner can't start at is a usage error.
	if _, errs, code := runCLI(t, nil, "routine", "run", "review", "--model", "m2", "--effort", "medium", "--rerun"); code != 0 {
		t.Fatalf("run --effort: %d %q", code, errs)
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "--effort medium ") {
		t.Fatalf("claude argv %q: want --effort medium", b)
	}
	if _, errs, code := runCLI(t, nil, "routine", "run", "review", "--effort", "huge"); code != 2 || !strings.Contains(errs, `effort "huge"`) {
		t.Fatalf("run --effort huge: %d %q", code, errs)
	}

	// Default model is the first; a rerun replaces the record.
	t.Setenv("FAKE_RESULT", "VERDICT: fail")
	if _, errs, code := runCLI(t, nil, "routine", "run", "review", "--rerun"); code != 1 {
		t.Fatalf("rerun: %d %q", code, errs)
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "--model m1 ") {
		t.Fatalf("default model: %q", b)
	}
	if r, ok, _ := mesh.GetResult(ctx, c, "review."+tree+".m1"); !ok || r.OK || r.Model != "m1" {
		t.Fatalf("m1 record %+v %v", r, ok)
	}
	if r, _, _ := mesh.GetResult(ctx, c, "review."+tree+".m2"); !r.OK {
		t.Fatal("m2 record changed by the m1 run")
	}
	if rs, err := mesh.ListResults(ctx, c, "review"); err != nil || len(rs) != 2 {
		t.Fatalf("review results %+v %v", rs, err)
	}
	if _, errs, code := runCLI(t, nil, "routine", "run", "review", "--model", "m3"); code != 2 {
		t.Fatalf("unknown model: %d %q", code, errs)
	}
}

// routine run --here refuses a model run whose claude never answered and
// records nothing; a reply without the verdict its prompt asks for is still
// recorded as a fail.
func TestInlineModelRunThatNeverAnsweredRecordsNothing(t *testing.T) {
	c, ctx := setup(t)
	fake, err := filepath.Abs("../../testdata/fake-claude.sh")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMESH_CLAUDE_COMMAND", "sh "+fake)
	t.Setenv("FAKE_IS_ERROR", "1")
	t.Setenv("FAKE_EXIT", "1")
	t.Setenv("FAKE_RESULT", "Failed to authenticate: OAuth session expired")
	kindRepo(t, map[string]string{
		"review": "mode = \"pooled\"\nmodels = [\"m1\"]\nprompt = \"End with VERDICT: ok or VERDICT: fail.\"\n",
	})
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")

	out, errs, code := runCLI(t, nil, "routine", "run", "review", "--here")
	if code != 3 || out != "" || errs != "bmesh: model run failed: Failed to authenticate: OAuth session expired; nothing recorded: run it again\n" {
		t.Fatalf("run: %d %q %q", code, out, errs)
	}
	if r, ok, err := mesh.GetResult(ctx, c, "review."+tree+".m1"); err != nil || ok {
		t.Fatalf("recorded %+v %v", r, err)
	}

	t.Setenv("FAKE_IS_ERROR", "")
	t.Setenv("FAKE_EXIT", "")
	t.Setenv("FAKE_RESULT", "all good")
	if out, errs, code := runCLI(t, nil, "routine", "run", "review", "--here"); code != 1 || !strings.HasSuffix(out, "review fail t:"+tree[:8]+" m:m1 (recorded)\n") {
		t.Fatalf("no verdict: %d %q %q", code, out, errs)
	}
	if r, ok, err := mesh.GetResult(ctx, c, "review."+tree+".m1"); err != nil || !ok || r.OK {
		t.Fatalf("no verdict: record %+v %v %v", r, ok, err)
	}
}

// setupLog points $SETUP_LOG at a file outside the repo a setup command
// appends to, and returns a count of its runs.
func setupLog(t *testing.T) func() int {
	t.Helper()
	p := filepath.Join(t.TempDir(), "setup.log")
	t.Setenv("SETUP_LOG", p)
	return func() int {
		b, _ := os.ReadFile(p)
		return strings.Count(string(b), "setup\n")
	}
}

// commitAll writes files into the cwd repo and commits them.
func commitAll(t *testing.T, repo string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-qm", "files")
}

const installSetup = `mkdir -p node_modules && touch node_modules/dep && echo setup >> \"$SETUP_LOG\"`

// routine run brings the worktree's installs current before a command kind
// runs: again when an input changed, not when none did, once with no inputs.
func TestInlineRunsSetupWhenStale(t *testing.T) {
	c, ctx := setup(t)
	setups := setupLog(t)
	repo := kindRepo(t, map[string]string{"verify": "mode = \"inline\"\ncommand = \"test -f node_modules/dep && echo deps\"\n"})
	commitAll(t, repo, map[string]string{".gitignore": "node_modules/\n", "web/lock.json": "1\n",
		".bmesh/config.toml": "[setup]\ncommand = \"" + installSetup + "\"\ninputs = [\"web/lock.json\"]\n"})
	run := func(want int) {
		t.Helper()
		out, errs, code := runCLI(t, nil, "routine", "run", "verify")
		if code != 0 || !strings.HasPrefix(out, "deps\nverify ok") || setups() != want {
			t.Fatalf("run: %d %q %q setups %d, want %d", code, out, errs, setups(), want)
		}
	}
	run(1)
	run(1)
	commitAll(t, repo, map[string]string{"a.txt": "b\n"})
	run(1)
	commitAll(t, repo, map[string]string{"web/lock.json": "2\n"})
	run(2)
	// With no inputs it runs once in the worktree.
	commitAll(t, repo, map[string]string{".bmesh/config.toml": "[setup]\ncommand = \"" + installSetup + "\"\n"})
	run(3)
	commitAll(t, repo, map[string]string{"web/lock.json": "3\n"})
	run(3)

	// A setup that leaves an untracked file fails, and records nothing.
	commitAll(t, repo, map[string]string{".bmesh/config.toml": "[setup]\ncommand = \"touch stray.txt\"\n"})
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")
	out, errs, code := runCLI(t, nil, "routine", "run", "verify")
	if code != 3 || out != "" || errs != "bmesh: setup dirtied the tree: stray.txt; nothing recorded: fix the setup (bmesh setup runs it alone), then run it again\n" {
		t.Fatalf("dirty: %d %q %q", code, out, errs)
	}
	if _, ok, err := mesh.GetResult(ctx, c, mesh.ResultKey("verify", tree, nil, "")); err != nil || ok {
		t.Fatalf("a dirtying setup recorded: %v %v", ok, err)
	}
}

// noHub points BMESH_CONFIG at a single box whose hub address nothing
// listens on, as when bmesh serve isn't running, and returns the address.
func noHub(t *testing.T) string {
	t.Helper()
	addr := closedAddr(t)
	singleBox(t, addr)
	return addr
}

// With no hub, routine run still runs the kind on HEAD: a command kind
// after the worktree's setup, a model kind through claude, with no slot
// leased and nothing answered from a record or recorded, as its verdict
// line says. It exits as the run did, and runs as the node given, if any.
func TestInlineRunsWithNoHub(t *testing.T) {
	addr := noHub(t)
	setups := setupLog(t)
	fake, err := filepath.Abs("../../testdata/fake-claude.sh")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMESH_CLAUDE_COMMAND", "sh "+fake)
	t.Setenv("FAKE_RESULT", "looks fine\nVERDICT: ok")
	repo := kindRepo(t, map[string]string{
		"verify": "mode = \"inline\"\ndeterministic = true\nslots = [\"game\"]\n" +
			"command = \"test -f node_modules/dep && echo ran $BMESH_TREE $BMESH_FROM\"\n",
		"broken": "mode = \"inline\"\ndeterministic = true\ncommand = \"echo nope; exit 3\"\n",
		"review": "mode = \"inline\"\ndeterministic = true\nmodels = [\"m1\"]\nprompt = \"check\"\n",
	})
	commitAll(t, repo, map[string]string{".gitignore": "node_modules/\n",
		".bmesh/config.toml": "[slots]\ngame = 1\n[setup]\ncommand = \"" + installSetup + "\"\n"})
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")
	notRecorded := " (not recorded: no hub at " + addr + ")\n"

	ran := "ran " + tree + " " + testHost + "\n"
	out, errs, code := runCLI(t, nil, "routine", "run", "verify")
	if code != 0 || out != ran+"verify ok t:"+tree[:8]+notRecorded || setups() != 1 ||
		!strings.HasPrefix(errs, "bmesh: setup ran (") {
		t.Fatalf("verify: %d %q %q setups %d", code, out, errs, setups())
	}
	// No record answers a second run: it runs again, here as the node given.
	out, errs, code = runCLI(t, as("app-001"), "routine", "run", "verify")
	if code != 0 || out != "ran "+tree+" app-001\nverify ok t:"+tree[:8]+notRecorded || errs != "" || setups() != 1 {
		t.Fatalf("again: %d %q %q setups %d", code, out, errs, setups())
	}
	out, errs, code = runCLI(t, nil, "routine", "run", "broken")
	if code != 1 || out != "nope\nbroken fail t:"+tree[:8]+notRecorded || errs != "" {
		t.Fatalf("broken: %d %q %q", code, out, errs)
	}
	out, errs, code = runCLI(t, nil, "routine", "run", "review")
	if code != 0 || out != "looks fine\nVERDICT: ok\nreview ok t:"+tree[:8]+" m:m1"+notRecorded || errs != "" {
		t.Fatalf("review: %d %q %q", code, out, errs)
	}

	// Nothing serving [mesh] nats is no hub too.
	writeFile(t, os.Getenv("BMESH_CONFIG"), "[mesh]\nnats = \"nats://"+addr+"\"\n[host]\nname = \""+testHost+"\"\n")
	out, errs, code = runCLI(t, nil, "routine", "run", "verify")
	if code != 0 || out != ran+"verify ok t:"+tree[:8]+notRecorded || errs != "" {
		t.Fatalf("[mesh] nats: %d %q %q", code, out, errs)
	}
}

// With no hub, a dirty tree is still refused and routine queue still needs
// the hub; a failure to connect that isn't for want of a hub (a token file
// it can't read, a token the hub refuses, a listen port of 0) fails as it
// did. None of them runs the kind.
func TestInlineNoHubRefusals(t *testing.T) {
	addr := noHub(t)
	repo := kindRepo(t, map[string]string{
		"verify": "mode = \"inline\"\ncommand = \"touch \\\"$MARK\\\"\"\n",
		"lint":   "command = \"touch \\\"$MARK\\\"\"\n",
	})
	mark := marker(t)
	writeFile(t, filepath.Join(repo, "a.txt"), "changed\n")
	if _, errs, code := runCLI(t, nil, "routine", "run", "verify"); code != 3 || errs != "bmesh: worktree is dirty; commit first\n" || exists(mark) {
		t.Fatalf("dirty: %d %q ran=%v", code, errs, exists(mark))
	}
	meshtest.Git(t, repo, "checkout", "a.txt")
	if _, errs, code := runCLI(t, nil, "routine", "queue", "lint"); code != 1 || errs != "bmesh: no hub at "+addr+": is bmesh serve running?\n" {
		t.Fatalf("queue: %d %q", code, errs)
	}

	hub, err := mesh.StartHub(mesh.HubOptions{Name: "hubbox", Token: "right", Listen: "127.0.0.1:0", Domain: "hub", Store: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Shutdown)
	wrong := filepath.Join(t.TempDir(), "token")
	writeFile(t, wrong, "wrong\n")
	for _, c := range []struct{ mesh, want string }{
		{fmt.Sprintf("nats = \"\"\nlisten = %q\ntoken_file = %q", addr, filepath.Join(t.TempDir(), "missing")), "bmesh: token: "},
		{fmt.Sprintf("nats = %q\ntoken_file = %q", hub.ClientURL(), wrong), "bmesh: can't reach NATS at " + hub.ClientURL() + ": nats: Authorization Violation"},
		{"nats = \"\"\nlisten = \"127.0.0.1:0\"", "bmesh: [mesh] listen 127.0.0.1:0 lets bmesh serve pick a port"},
	} {
		writeFile(t, os.Getenv("BMESH_CONFIG"), "[mesh]\n"+c.mesh+"\n[host]\nname = \""+testHost+"\"\n")
		out, errs, code := runCLI(t, nil, "routine", "run", "verify")
		if code != 1 || out != "" || !strings.HasPrefix(errs, c.want) || exists(mark) {
			t.Fatalf("%s: %d %q %q ran=%v", c.mesh, code, out, errs, exists(mark))
		}
	}
}

// commitDoc commits file with body in the cwd's worktree and returns the
// new tree.
func commitDoc(t *testing.T, file, body string) string {
	t.Helper()
	writeFile(t, file, body)
	meshtest.Git(t, ".", "add", "-A")
	meshtest.Git(t, ".", "commit", "-qm", "write "+file)
	return gitOut(t, "rev-parse", "HEAD^{tree}")
}

// runVerify runs verify in the cwd and returns its output, failing on
// anything but a pass.
func runVerify(t *testing.T, args ...string) string {
	t.Helper()
	out, errs, code := runCLI(t, nil, append([]string{"routine", "run", "verify"}, args...)...)
	if code != 0 || errs != "" {
		t.Fatalf("run: %d %q %q", code, out, errs)
	}
	return out
}

// A verify run over commits outside the row's code answers from the last
// tree it ran at, stored for the new tree as carried, and passes over a
// carried record for its source; a commit inside the code, the routine
// kinds whatever code lists, and --rerun each run.
func TestRunVerifyCarriesAcrossDocsCommits(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	codeRow(t, r, `"*.txt"`)
	runs := countRuns(t, r, "true")
	ran := gitOut(t, "rev-parse", "HEAD^{tree}")
	if out := runVerify(t); runs() != 1 || out != "verify ok t:"+ran[:8]+" (recorded)\n" {
		t.Fatalf("first: runs %d %q", runs(), out)
	}
	carried := func(tree, from string) string {
		return "verify ok t:" + tree[:8] + " (carried from t:" + from[:8] + ": nothing under code changed)\n"
	}
	readme := commitDoc(t, "README.md", "# app\n")
	if out := runVerify(t); runs() != 1 || out != carried(readme, ran) {
		t.Fatalf("readme: runs %d %q", runs(), out)
	}
	got, ok, err := mesh.GetResult(ctx, c, mesh.ResultKey("verify", readme, nil, ""))
	if err != nil || !ok || !got.OK || got.Commit != gitOut(t, "rev-parse", "HEAD") || got.By != testHost ||
		!strings.HasPrefix(got.Output, "carried from t:"+ran[:8]+": nothing under code changed\n") {
		t.Fatalf("stored %+v %v %v", got, ok, err)
	}
	again := commitDoc(t, "README.md", "# app, again\n")
	if out := runVerify(t); runs() != 1 || out != carried(again, ran) {
		t.Fatalf("a carried record is not a source: runs %d %q", runs(), out)
	}
	if out := runVerify(t); runs() != 1 || out != "carried from t:"+ran[:8]+": nothing under code changed\nverify ok t:"+again[:8]+" (answered from the record)\n" {
		t.Fatalf("repeat: runs %d %q", runs(), out)
	}

	code := commitDoc(t, "c.txt", "c\n")
	if out := runVerify(t); runs() != 2 || out != "verify ok t:"+code[:8]+" (recorded)\n" {
		t.Fatalf("code: runs %d %q", runs(), out)
	}
	kindFile := commitDoc(t, ".bmesh/routines/verify.toml", "mode = \"inline\"\ndeterministic = true\ncommand = \"sh \\\"$HOOK\\\"\"\n# again\n")
	if out := runVerify(t); runs() != 3 || out != "verify ok t:"+kindFile[:8]+" (recorded)\n" {
		t.Fatalf("the verify kind: runs %d %q", runs(), out)
	}
	notes := commitDoc(t, "NOTES.md", "notes\n")
	if out := runVerify(t, "--rerun"); runs() != 4 || out != "verify ok t:"+notes[:8]+" (recorded)\n" {
		t.Fatalf("--rerun: runs %d %q", runs(), out)
	}
}

// A branch with only docs since main carries from main's verified tip.
func TestRunVerifyCarriesFromMainOnADocsBranch(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	codeRow(t, r, `"*.txt"`)
	runs := countRuns(t, r, "true")
	base := gitOut(t, "rev-parse", "main^{tree}")
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "verify", Tree: base, Commit: gitOut(t, "rev-parse", "main"), OK: true, By: "k16"}); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, ".", "checkout", "-qb", "docs", "main")
	commitDoc(t, "README.md", "# app\n")
	tree := commitDoc(t, "docs/guide.md", "guide\n")
	if out := runVerify(t); runs() != 0 || out != "verify ok t:"+tree[:8]+" (carried from t:"+base[:8]+": nothing under code changed)\n" {
		t.Fatalf("runs %d %q", runs(), out)
	}
	// spec verified composes the carried record as any other.
	out, errs, code := runCLI(t, nil, "spec", "verified")
	if code != 0 || out != "verified: verify ok t:"+tree[:8]+"\n" {
		t.Fatalf("spec verified: %d %q %q", code, out, errs)
	}
}

// The nearest record decides: a fail there runs verify, whatever ok is
// older.
func TestRunVerifyRunsOverANearerFail(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	codeRow(t, r, `"*.txt"`)
	runs := countRuns(t, r, "true")
	runVerify(t)
	failed := commitDoc(t, "README.md", "# app\n")
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "verify", Tree: failed, OK: false, Output: "flaked\n", By: "k16"}); err != nil {
		t.Fatal(err)
	}
	tree := commitDoc(t, "NOTES.md", "notes\n")
	if out := runVerify(t); runs() != 2 || out != "verify ok t:"+tree[:8]+" (recorded)\n" {
		t.Fatalf("runs %d %q", runs(), out)
	}
}

// The run follows first parents: a merge that brings no code is in it, one
// that brings code ends it.
func TestRunVerifyThroughAMerge(t *testing.T) {
	setup(t)
	r := newLandRepo(t)
	codeRow(t, r, `"*.txt"`)
	runs := countRuns(t, r, "true")
	ran := gitOut(t, "rev-parse", "HEAD^{tree}")
	runVerify(t)
	merge := func(file string) string {
		t.Helper()
		meshtest.Git(t, ".", "checkout", "-qb", "side-"+file, "main")
		commitDoc(t, file, file+"\n")
		meshtest.Git(t, ".", "checkout", "-q", "lead")
		meshtest.Git(t, ".", "merge", "-q", "--no-ff", "-m", "merge "+file, "side-"+file)
		return gitOut(t, "rev-parse", "HEAD^{tree}")
	}
	docs := merge("guide.md")
	if out := runVerify(t); runs() != 1 || out != "verify ok t:"+docs[:8]+" (carried from t:"+ran[:8]+": nothing under code changed)\n" {
		t.Fatalf("docs merge: runs %d %q", runs(), out)
	}
	code := merge("c.txt")
	if out := runVerify(t); runs() != 2 || out != "verify ok t:"+code[:8]+" (recorded)\n" {
		t.Fatalf("code merge: runs %d %q", runs(), out)
	}
}

// The run is at most selfupdate.CodeRunMax commits: a verified tree that
// many commits down is reached, one more is not.
func TestRunVerifyWalksAtMostTheCap(t *testing.T) {
	setup(t)
	r := newLandRepo(t)
	codeRow(t, r, `"*.txt"`)
	runs := countRuns(t, r, "true")
	ran := gitOut(t, "rev-parse", "HEAD^{tree}")
	runVerify(t)
	var tree string
	for i := range selfupdate.CodeRunMax - 1 {
		tree = commitDoc(t, "README.md", fmt.Sprintf("# app %d\n", i))
	}
	if out := runVerify(t); runs() != 1 || out != "verify ok t:"+tree[:8]+" (carried from t:"+ran[:8]+": nothing under code changed)\n" {
		t.Fatalf("at the cap: runs %d %q", runs(), out)
	}
	commitDoc(t, "README.md", "# app, one more\n")
	commitDoc(t, "README.md", "# app, past the cap\n")
	if runVerify(t); runs() != 2 {
		t.Fatalf("past the cap: runs %d", runs())
	}
}

// Without code in the row, a spec file is outside the code and the specs'
// README is not.
func TestRunVerifyWithoutCode(t *testing.T) {
	setup(t)
	r := newLandRepo(t)
	codeRow(t, r, "")
	runs := countRuns(t, r, "true")
	runVerify(t)
	commitDoc(t, "docs/proposals/013-other.md", "---\nid: 13\nstatus: accepted\n---\n")
	if out := runVerify(t); runs() != 1 || !strings.Contains(out, "(carried from t:") {
		t.Fatalf("a spec file: runs %d %q", runs(), out)
	}
	commitDoc(t, "docs/proposals/README.md", "# specs\n")
	if out := runVerify(t); runs() != 2 || !strings.HasSuffix(out, "(recorded)\n") {
		t.Fatalf("the specs' README: runs %d %q", runs(), out)
	}
}
