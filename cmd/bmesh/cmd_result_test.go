package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// gitRepo makes a repo with one commit and changes into it.
func gitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	meshtest.Git(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "a")
	t.Chdir(repo)
	return repo
}

func gitOut(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func TestResultRecordAndFind(t *testing.T) {
	c, ctx := setup(t)
	gitRepo(t)
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")
	commit := gitOut(t, "rev-parse", "HEAD")

	_, errs, code := runCLI(t, nil, "result", "find", "verify")
	if code != 3 || errs != "bmesh: no result for verify."+tree+"\n" {
		t.Fatalf("find before record: %d %q", code, errs)
	}
	// Each subcommand's usage error names its own flags.
	if _, errs, code := runCLI(t, nil, "result", "record", "verify"); code != 2 || !strings.Contains(errs, "--ok|--fail [--commit C] [--model M] [--output FILE|-]") {
		t.Fatalf("record without --ok/--fail: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "result", "record"); code != 2 || errs != "bmesh: "+resultRecordUsage+"\n" {
		t.Fatalf("record without a kind: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "result", "find"); code != 2 || errs != "bmesh: "+resultFindUsage+"\n" {
		t.Fatalf("find without a kind: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "result", "record", "-h"); code != 2 || !strings.Contains(errs, "[--commit] [--fail] [--model] [--ok] [--output]") {
		t.Fatalf("record -h: %d %q", code, errs)
	}
	if _, _, code := runCLI(t, nil, "result", "record", "verify", "--ok", "--fail"); code != 2 {
		t.Fatalf("record with both: %d", code)
	}
	out, errs, code := runCLI(t, nil, "result", "record", "verify", "--ok", "--commit", "HEAD")
	if code != 0 || out != "recorded verify."+tree+"\n" {
		t.Fatalf("record: %d %q %q", code, out, errs)
	}
	out, errs, code = runCLI(t, nil, "result", "find", "verify")
	f := strings.Fields(out)
	if code != 0 || len(f) != 4 || f[0] != "ok" || f[1] != tree || f[3] != testHost {
		t.Fatalf("find: %d %q %q", code, out, errs)
	}

	// Args make a separate record; a long output is truncated.
	big := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", 70<<10)), 0o644); err != nil {
		t.Fatal(err)
	}
	key := mesh.ResultKey("verify", tree, []string{"--fast", "a b"}, "")
	out, errs, code = runCLI(t, as("app-001"), "result", "record", "verify", "--fail", "--output", big, "--", "--fast", "a b")
	if code != 0 || out != "recorded "+key+"\n" {
		t.Fatalf("record args: %d %q %q", code, out, errs)
	}
	out, errs, code = runCLI(t, nil, "result", "find", "verify", "--json", "--", "--fast", "a b")
	var r mesh.Result
	if code != 0 || json.Unmarshal([]byte(out), &r) != nil {
		t.Fatalf("find json: %d %q %q", code, out, errs)
	}
	if r.OK || r.Commit != commit || r.By != "app-001" || len(r.Args) != 2 ||
		!strings.HasSuffix(r.Output, "\n[truncated]") || len(r.Output) > 64<<10+len("\n[truncated]") {
		t.Fatalf("record %+v (output %d bytes)", r, len(r.Output))
	}
	if got, ok, err := mesh.GetResult(ctx, c, "verify."+tree); err != nil || !ok || !got.OK {
		t.Fatalf("no-args record changed: %+v %v %v", got, ok, err)
	}

	// A model makes a separate record; without --model, find lists every
	// model's result at the tree.
	if _, errs, code := runCLI(t, nil, "result", "find", "review"); code != 3 || errs != "bmesh: no result for review."+tree+"\n" {
		t.Fatalf("find before any: %d %q", code, errs)
	}
	out, errs, code = runCLI(t, nil, "result", "record", "review", "--ok", "--model", "claude-opus-5-5")
	if code != 0 || out != "recorded review."+tree+".opus-5-5\n" {
		t.Fatalf("record model: %d %q %q", code, out, errs)
	}
	if out, errs, code := runCLI(t, nil, "result", "record", "review", "--fail", "--model", "claude-sonnet-5-5"); code != 0 {
		t.Fatalf("record second model: %d %q %q", code, out, errs)
	}
	out, errs, code = runCLI(t, nil, "result", "find", "review")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != 0 || len(lines) != 2 || !strings.HasPrefix(lines[0], "ok "+tree+" ") || !strings.HasSuffix(lines[0], " m:claude-opus-5-5") ||
		!strings.HasPrefix(lines[1], "fail "+tree+" ") || !strings.HasSuffix(lines[1], " m:claude-sonnet-5-5") {
		t.Fatalf("find without model: %d %q %q", code, out, errs)
	}
	// Args still pick the record: none was made with these.
	if _, errs, code := runCLI(t, nil, "result", "find", "review", "--", "x"); code != 3 {
		t.Fatalf("find with other args: %d %q", code, errs)
	}
	out, errs, code = runCLI(t, nil, "result", "find", "review", "--model", "claude-opus-5-5", "--json")
	r = mesh.Result{}
	if code != 0 || json.Unmarshal([]byte(out), &r) != nil || !r.OK || r.Model != "claude-opus-5-5" {
		t.Fatalf("find model: %d %q %q", code, out, errs)
	}
}

// A model id with a space or ";" would break a verified line: refused.
func TestModelIDsValidated(t *testing.T) {
	c, ctx := setup(t)
	kindRepo(t, map[string]string{
		"review": "mode = \"inline\"\nmodels = [\"m1\"]\nprompt = \"check\"\n",
	})
	for _, args := range [][]string{
		{"result", "record", "review", "--ok", "--model", "m1; verify ok t:00000000"},
		{"result", "record", "review", "--ok", "--model", "m 1"},
		{"routine", "queue", "review", "--model", "m1;x"},
		{"routine", "run", "review", "--model", "m 1"},
	} {
		_, errs, code := runCLI(t, as("app-001"), args...)
		if code != 2 || !strings.Contains(errs, "want no spaces or \";\"") {
			t.Fatalf("%v: %d %q", args, code, errs)
		}
	}
	if rs, err := mesh.ListAllResults(ctx, c); err != nil || len(rs) != 0 {
		t.Fatalf("recorded %+v %v", rs, err)
	}
}

// A result for HEAD is refused while the worktree is dirty; another commit's
// result is not about the files, so it may be recorded.
func TestResultRecordRefusesDirtyHead(t *testing.T) {
	c, ctx := setup(t)
	repo := gitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "b")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{}, {"--commit", "HEAD"}, {"--commit", gitOut(t, "rev-parse", "HEAD")}} {
		_, errs, code := runCLI(t, nil, append([]string{"result", "record", "verify", "--ok"}, args...)...)
		if code != 3 || errs != "bmesh: worktree is dirty; commit first\n" {
			t.Fatalf("%v: %d %q", args, code, errs)
		}
	}
	if rs, err := mesh.ListAllResults(ctx, c); err != nil || len(rs) != 0 {
		t.Fatalf("recorded %+v %v", rs, err)
	}
	prev := gitOut(t, "rev-parse", "HEAD~1^{tree}")
	if out, errs, code := runCLI(t, nil, "result", "record", "verify", "--ok", "--commit", "HEAD~1"); code != 0 || out != "recorded verify."+prev+"\n" {
		t.Fatalf("other commit: %d %q %q", code, out, errs)
	}
}
