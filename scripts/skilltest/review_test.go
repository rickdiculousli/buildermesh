package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/instance"
	"github.com/rickdiculousli/buildermesh/internal/kind"
)

// reviewFile is a file of the sandbox as the review's change leaves it.
func reviewFile(t *testing.T, rel string) string {
	t.Helper()
	for _, p := range []string{reviewDir + "/change/" + rel, reviewDir + "/base/" + rel} {
		if b, err := reviewFS.ReadFile(p); err == nil {
			return string(b)
		}
	}
	b, err := os.ReadFile(filepath.Join(testFixture(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The sheet says where each defect is, and the code agrees: each of its
// line ranges holds one of its names, and each of its files exists.
func TestDefectSheet(t *testing.T) {
	if len(sheet.Defects) != 4 {
		t.Fatalf("%d defects, want the four classes", len(sheet.Defects))
	}
	for _, d := range sheet.Defects {
		for _, f := range d.Files {
			reviewFile(t, f)
		}
		for _, r := range d.Lines {
			lines := strings.Split(reviewFile(t, r.File), "\n")
			if r.To > len(lines) {
				t.Errorf("%s: %s has %d lines, the sheet says %d-%d", d.ID, r.File, len(lines), r.From, r.To)
				continue
			}
			span := strings.Join(lines[r.From-1:r.To], "\n")
			found := false
			for _, n := range d.Names {
				found = found || strings.Contains(span, n)
			}
			if !found {
				t.Errorf("%s: %s:%d-%d names none of %v:\n%s", d.ID, r.File, r.From, r.To, d.Names, span)
			}
		}
	}
}

// The commit under review passes the fixture's tests, as one does once
// verify has run, and its diff leaves out the code that makes each defect
// wrong: receipt.go and the store are read past it.
func TestReviewChange(t *testing.T) {
	need(t, "git", "go")
	dir := t.TempDir()
	arm := Arm{Name: "test", Commands: []string{"help"}, Review: &kind0}
	cs, err := setupCell(dir, findCase("review"), arm, testFixture(t), thisBox(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sandbox := filepath.Join(dir, sandboxDir)
	changed, err := git(sandbox, gitEnv(dir), "diff", "--name-only", cs.Base+"..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	want := "README.md cart/cart.go cmd/shop/main.go cmd/shop/main_test.go report/report.go report/report_test.go"
	if got := strings.Join(strings.Fields(changed), " "); got != want {
		t.Errorf("the change touches %s, want %s", got, want)
	}
	if branch, _ := git(sandbox, gitEnv(dir), "branch", "--show-current"); branch != reviewBranch {
		t.Errorf("the sandbox is on %q, want %s", branch, reviewBranch)
	}
	if origin, _ := git(dir, gitEnv(dir), "--git-dir", filepath.Join(dir, originDir), "rev-parse", "main"); origin != cs.Base {
		t.Errorf("origin's main is %s, want the base %s", origin, cs.Base)
	}
	code, out, errs := run(sandbox, 5*time.Minute, append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off", "GOWORK=off", "GOFLAGS="), "go", "test", "-count=1", "./...")
	if code != 0 {
		t.Errorf("the change fails the fixture's tests:\n%s%s", out, errs)
	}
	if len(cs.Words) != 1 || !strings.HasPrefix(cs.Words[0], kind0.Prompt+"\n\njob: "+reviewJob+" commit: ") || !strings.HasSuffix(cs.Words[0], "\nbase: "+cs.Base+"\n") {
		t.Errorf("words %q", cs.Words)
	}
	if cs.Routine == nil || !reflect.DeepEqual(cs.Routine.Allow, kind0.Allow) || cs.Routine.Effort != reviewEffort {
		t.Errorf("routine %+v", cs.Routine)
	}
}

var kind0 = mustKind()

// mustKind is this tree's review kind, as an arm of it reads it.
func mustKind() kind.Kind {
	b, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(reviewKindPath)))
	if err != nil {
		panic(err)
	}
	k, err := kind.Parse("review", b)
	if err != nil {
		panic(err)
	}
	return k
}

const fakeReviewClaude = `#!/bin/sh
# A fake reviewer: it records how it was run and replies with the reply file.
printf '%s\0' "$@" >"$FAKE_CLAUDE_DIR/args"
pwd >"$FAKE_CLAUDE_DIR/cwd"
git branch --show-current >"$FAKE_CLAUDE_DIR/branch"
cat "$FAKE_CLAUDE_DIR/reply.json"
`

// A review cell runs claude as the review routine runs it, on the change's
// commit, without the plugin, and is scored on the defects its reply names.
func TestRunReview(t *testing.T) {
	need(t, "git", "go", "sh")
	root := repoRoot(t)
	arm, err := prepareArm("cand", root, root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if arm.Review == nil || arm.Review.Prompt != kind0.Prompt {
		t.Fatalf("the arm's review kind: %+v", arm.Review)
	}
	tools := t.TempDir()
	claude := filepath.Join(tools, "claude")
	if err := os.WriteFile(claude, []byte(fakeReviewClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "result": reviewGoodReply, "session_id": "fake-review", "num_turns": 9})
	if err := os.WriteFile(filepath.Join(tools, "reply.json"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	host := findHost()
	host.Home = t.TempDir()
	r := &runner{claude: claude, model: "claude-opus-5-5", timeout: 3 * time.Minute, env: append(os.Environ(), "FAKE_CLAUDE_DIR="+tools), host: host}
	row, err := r.runCell(context.Background(), cellPlan{n: 1, k: findCase("review"), arm: arm, run: 1}, testFixture(t), t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if named, missed := planted(row); !row.Pass || len(named) != 4 || len(missed) != 0 || row.Steers != 0 || row.PluginHook {
		t.Errorf("row %+v", row)
	}
	args, _ := os.ReadFile(filepath.Join(tools, "args"))
	got := strings.Split(strings.TrimSuffix(string(args), "\x00"), "\x00")
	want := append([]string{"--model", "claude-opus-5-5", "--output-format", "stream-json", "--verbose", "--effort", reviewEffort,
		"--setting-sources", "project,local"}, instance.SessionFlags(kind0.Allow)...)
	if len(got) < 2 || got[0] != "-p" || !strings.HasPrefix(got[1], kind0.Prompt) || !reflect.DeepEqual(got[len(got)-len(want):], want) {
		t.Errorf("claude was run as %q, want -p <prompt and job> then %q", got, want)
	}
	if strings.Contains(string(args), "--plugin-dir") {
		t.Error("the reviewer was given the plugin")
	}
	if b, _ := os.ReadFile(filepath.Join(tools, "branch")); strings.TrimSpace(string(b)) != reviewBranch {
		t.Errorf("the reviewer ran on branch %q", b)
	}
	if cwd, _ := os.ReadFile(filepath.Join(tools, "cwd")); filepath.Base(strings.TrimSpace(string(cwd))) != sandboxDir {
		t.Errorf("the reviewer ran in %s", cwd)
	}
}

// The summary opens with the cases whose pass differs between the arms,
// run by run, and lists the planted defects each review run named.
func TestSummaryFlips(t *testing.T) {
	row := func(k, arm string, run int, pass bool) Row { return Row{Case: k, Arm: arm, Run: run, Pass: pass} }
	res := Results{Date: "test", Arms: []Arm{{Name: "base"}, {Name: "cand"}}, Rows: []Row{
		row("bug", "base", 1, true), row("bug", "cand", 1, true),
		row("bug", "base", 2, false), row("bug", "cand", 2, true),
		row("tweak", "base", 1, true),
		row("qa", "base", 1, true), {Case: "qa", Arm: "cand", Run: 1, Error: "arm cand has no review kind"},
		{Case: "review", Arm: "cand", Run: 1, Predicates: []PredResult{{plantedPrefix + "off-by-one", true, ""}, {plantedPrefix + "quadratic", false, ""}, {"verdict_fail", true, ""}}},
	}}
	sortRows(res.Rows)
	md := summary(res)
	flip := strings.Index(md, "## Flipped\n\n- bug run 2: base fail, cand pass\n\n")
	table := strings.Index(md, "| case | arm |")
	if flip < 0 || table < flip || strings.Contains(md, "bug run 1") || strings.Contains(md, "tweak run 1") || strings.Contains(md, "qa run 1") {
		t.Errorf("summary.md doesn't open with the one flip:\n%s", md)
	}
	if !strings.Contains(md, "- review / cand #1: named 1 of 2 [off-by-one]; missed [quadratic]") {
		t.Errorf("summary.md doesn't list the planted defects:\n%s", md)
	}
	res.Rows = res.Rows[:2]
	if md := summary(res); !strings.Contains(md, "No case flipped between the arms.") {
		t.Errorf("no flip:\n%s", md)
	}
	res.Arms, res.Rows = res.Arms[1:], []Row{row("bug", "cand", 1, true), row("bug", "cand", 2, false)}
	if md := summary(res); strings.Contains(md, "Flipped") {
		t.Errorf("one arm has nothing to flip against:\n%s", md)
	}
}
