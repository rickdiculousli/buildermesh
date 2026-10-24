package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// claudeSession writes a transcript whose newest assistant message read
// tokens, for session id under a fake Claude config dir, and returns the
// environment naming it.
func claudeSession(t *testing.T, id string, tokens int) map[string]string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "projects", "-repo", id+".jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	line := fmt.Sprintf(`{"type":"assistant","message":{"usage":{"input_tokens":2,"cache_read_input_tokens":%d,"cache_creation_input_tokens":1000}}}`, tokens-1002)
	if err := os.WriteFile(p, []byte(`{"type":"user","message":{}}`+"\n"+line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return map[string]string{"CLAUDE_CODE_SESSION_ID": id, "CLAUDE_CONFIG_DIR": dir, "XDG_STATE_HOME": t.TempDir()}
}

func TestCommandEndsWithContextLineOnStderrOnce(t *testing.T) {
	reposSetup(t)
	env := claudeSession(t, "ec03a02c-aaaa", 304123)
	out, errs, code := runCLI(t, env, "repos", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 2 {
		t.Fatalf("stdout isn't the rows alone: %v\n%s", err, out)
	}
	want := "bmesh: this session's context is 304k. Compact at your next milestone, after updating your state file; never while exploring or investigating.\n"
	if !strings.HasSuffix(errs, want) || strings.Count(errs, "context is") != 1 {
		t.Fatalf("stderr = %q; want it to end with %q", errs, want)
	}
	if _, errs, _ = runCLI(t, env, "repos"); strings.Contains(errs, "context is") {
		t.Fatalf("said twice: %q", errs)
	}
	// A refused command is still a command the session ran.
	other := claudeSession(t, "bbbb", 260000)
	if _, errs, code = runCLI(t, other, "repos", "show", "nope"); code == 0 || !strings.HasSuffix(errs, "context is 260k. You'll hear again every 50k.\n") {
		t.Fatalf("refused: exit %d, %q", code, errs)
	}
}

func TestContextQuietBelowNudgeAndWithoutSession(t *testing.T) {
	reposSetup(t)
	if _, errs, _ := runCLI(t, claudeSession(t, "low", 120000), "repos"); errs != "" {
		t.Fatalf("below nudge_from: %q", errs)
	}
	env := claudeSession(t, "gone", 350000)
	env["CLAUDE_CODE_SESSION_ID"] = "another"
	if _, errs, _ := runCLI(t, env, "repos"); errs != "" {
		t.Fatalf("unknown session: %q", errs)
	}
	off := claudeSession(t, "off", 350000)
	os.WriteFile(os.Getenv("BMESH_CONFIG"), []byte("[context]\nnudge_from = 0\n"), 0o600)
	if _, errs, _ := runCLI(t, off, "repos"); errs != "" {
		t.Fatalf("nudge_from = 0: %q", errs)
	}
}

func TestContextCommand(t *testing.T) {
	reposSetup(t)
	env := claudeSession(t, "ec03a02c-1234", 312456)
	out, errs, code := runCLI(t, env, "context")
	if code != 0 || errs != "" || out != "context 312k (claude session ec03a02c): Compact at your next milestone, after updating your state file; never while exploring or investigating.\n" {
		t.Fatalf("context: exit %d, %q, %q", code, out, errs)
	}
	// It heard the 300k nudge: the next command says nothing.
	if _, errs, _ = runCLI(t, env, "repos"); errs != "" {
		t.Fatalf("after context: %q", errs)
	}
	out, _, _ = runCLI(t, env, "context", "--json")
	var j struct {
		Measured bool
		Harness  string
		Session  string
		Tokens   int
		Advice   string
	}
	if err := json.Unmarshal([]byte(out), &j); err != nil || !j.Measured || j.Harness != "claude" || j.Session != "ec03a02c-1234" || j.Tokens != 312456 || !strings.HasPrefix(j.Advice, "Compact at your next milestone") {
		t.Fatalf("--json: %v %s", err, out)
	}
	out, _, _ = runCLI(t, claudeSession(t, "early", 90000), "context")
	if out != "context 90k (claude session early): Nothing to do before 250k.\n" {
		t.Fatalf("early: %q", out)
	}
	none := map[string]string{"CLAUDE_CODE_SESSION_ID": ""}
	out, errs, code = runCLI(t, none, "context")
	if code != 0 || errs != "" || out != "context: no session bmesh can measure (Claude Code names its session in CLAUDE_CODE_SESSION_ID)\n" {
		t.Fatalf("no session: exit %d, %q, %q", code, out, errs)
	}
	if out, _, _ = runCLI(t, none, "context", "--json"); out != "{\"measured\":false}\n" {
		t.Fatalf("no session --json: %q", out)
	}
	if _, _, code = runCLI(t, none, "context", "x"); code != exitUsage {
		t.Fatalf("a positional: exit %d", code)
	}
}

func TestContextNeedsNoSetup(t *testing.T) {
	t.Setenv("BMESH_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	if out, errs, code := runCLI(t, claudeSession(t, "fresh", 90000), "context"); code != 0 || !strings.HasPrefix(out, "context 90k") {
		t.Fatalf("unset-up box: exit %d, %q, %q", code, out, errs)
	}
}
