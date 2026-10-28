package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// auditSession writes a transcript for session id: the operator asks for a
// fix, the session says it reads a file, reads it, edits it, pushes main
// and reports (report), and returns the environment naming the session.
func auditSession(t *testing.T, id, report string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "projects", "-repo", id+".jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	usage := `"usage":{"input_tokens":2,"cache_read_input_tokens":51000,"cache_creation_input_tokens":1000}`
	lines := []string{
		`{"type":"user","uuid":"u1","cwd":"/nowhere","sessionId":"` + id + `","timestamp":"2026-10-24T10:00:00Z","message":{"role":"user","content":"the total is wrong"}}`,
		`{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"text","text":"Reading cart.go first."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/r/cart.go"}}],` + usage + `}}`,
		`{"type":"user","uuid":"u2","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"func Total() int { return a - b }"}]},"toolUseResult":{"file":{"content":"func Total() int { return a - b }","startLine":1,"numLines":1,"totalLines":1}}}`,
		`{"type":"assistant","uuid":"a2","message":{"id":"m2","content":[{"type":"tool_use","id":"t2","name":"Edit","input":{"file_path":"/r/cart.go"}}],` + usage + `}}`,
		`{"type":"user","uuid":"u3","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"ok"}]}}`,
		`{"type":"assistant","uuid":"a3","message":{"id":"m3","content":[{"type":"tool_use","id":"t3","name":"Bash","input":{"command":"git commit -qam fix && git push origin HEAD:main"}}],` + usage + `}}`,
		`{"type":"user","uuid":"u4","message":{"content":[{"type":"tool_result","tool_use_id":"t3","content":"","is_error":false}]}}`,
		`{"type":"assistant","uuid":"a4","message":{"id":"m4","content":[{"type":"text","text":"` + report + `\n\nPushed."}],` + usage + `}}`,
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return map[string]string{"CLAUDE_CODE_SESSION_ID": id, "CLAUDE_CONFIG_DIR": dir}
}

func TestAuditThisSession(t *testing.T) {
	reposSetup(t)
	out, errs, code := runCLI(t, auditSession(t, "s-here", "Making it here, XS: one line in cart.go"), "audit")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, want := range []string{
		"audit s-here (claude, ",
		"ran in /nowhere; rules: unknown (/nowhere is gone or its checkout has no AGENTS.md)\n",
		"said: \"Reading cart.go first.\"\n  said last: \"Making it here, XS: one line in cart.go\"\n",
		"did: here (pushed to main, no new spec)\n",
		"first action: Edit /r/cart.go, at context 52k; read before it:\n    file /r/cart.go (whole), 9 words\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	// What it said is shown as written beside the rung its actions imply,
	// never matched against it.
	out, _, _ = runCLI(t, auditSession(t, "s-said", "Answering, no change"), "audit", "--json")
	var a struct {
		Turns []struct {
			Said     string
			SaidLast string `json:"said_last"`
			Implied  string
		}
	}
	if err := json.Unmarshal([]byte(out), &a); err != nil || len(a.Turns) != 1 || a.Turns[0].Said != "Reading cart.go first." ||
		a.Turns[0].SaidLast != "Answering, no change" || a.Turns[0].Implied != "here" || strings.Contains(out, "mismatch") {
		t.Fatalf("said: %v %s", err, out)
	}
	out, _, code = runCLI(t, map[string]string{"CLAUDE_CODE_SESSION_ID": ""}, "audit")
	if code != 0 || out != "audit: no session bmesh can read (Claude Code names its session in CLAUDE_CODE_SESSION_ID)\n" {
		t.Fatalf("no session: %d %q", code, out)
	}
	env := auditSession(t, "s-x", "")
	if _, errs, code := runCLI(t, env, "audit", "--session", "nope"); code != 1 || !strings.Contains(errs, "no transcript of session nope on this box") {
		t.Fatalf("no transcript: %d %q", code, errs)
	}
	if _, _, code := runCLI(t, env, "audit", "app-001", "--session", "s-x"); code != 2 {
		t.Fatalf("path and --session: %d", code)
	}
}

func TestStartedKeepsSessionAndAuditFindsIt(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-001"})
	putNode(t, c, ctx, mesh.Node{Path: "app-001.exec", Role: "executor"})
	putNode(t, c, ctx, mesh.Node{Path: "app-002", Host: "elsewhere", Session: "s-far"})
	env := auditSession(t, "s-lead", "Making it here, XS: one line")
	for _, p := range []string{"app-001", "app-001.exec"} {
		env["BMESH_NODE"] = p
		if _, errs, code := runCLI(t, env, "started", "--text", "go"); code != 0 {
			t.Fatalf("%s started: %d %s", p, code, errs)
		}
	}
	if n, _, _ := mesh.GetNode(ctx, c, "app-001"); n.Session != "s-lead" {
		t.Fatalf("lead's session = %q", n.Session)
	}
	if n, _, _ := mesh.GetNode(ctx, c, "app-001.exec"); n.Session != "" {
		t.Fatalf("an executor keeps its lead's session: %q", n.Session)
	}
	env["BMESH_NODE"] = ""
	if out, errs, code := runCLI(t, env, "audit", "app-001"); code != 0 || !strings.HasPrefix(out, "audit s-lead ") {
		t.Fatalf("audit app-001: %d %q %q", code, out, errs)
	}
	if _, errs, code := runCLI(t, env, "audit", "app-001.exec"); code != 3 || !strings.Contains(errs, "refused: app-001.exec's record names no session") {
		t.Fatalf("no session: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, env, "audit", "app-002"); code != 3 || !strings.Contains(errs, "refused: app-002 ran on elsewhere, not this box ("+testHost+"); run bmesh audit app-002 on elsewhere") {
		t.Fatalf("other box: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, env, "audit", "app-009"); code != 1 || !strings.Contains(errs, "node app-009 is not registered") {
		t.Fatalf("unregistered: %d %q", code, errs)
	}
}
