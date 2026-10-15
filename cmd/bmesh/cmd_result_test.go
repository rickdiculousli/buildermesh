package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// gitRepo makes a repo with one commit and changes into it.
func gitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "a")
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
	if _, _, code := runCLI(t, nil, "result", "record", "verify"); code != 2 {
		t.Fatalf("record without --ok/--fail: %d", code)
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

	// A model makes a separate record, found only with that model.
	out, errs, code = runCLI(t, nil, "result", "record", "review", "--ok", "--model", "claude-opus-5-5")
	if code != 0 || out != "recorded review."+tree+".opus-5-5\n" {
		t.Fatalf("record model: %d %q %q", code, out, errs)
	}
	if _, errs, code := runCLI(t, nil, "result", "find", "review"); code != 3 || errs != "bmesh: no result for review."+tree+"\n" {
		t.Fatalf("find without model: %d %q", code, errs)
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
