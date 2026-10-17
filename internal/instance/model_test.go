package instance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/kind"
)

// fakeClaude returns the claude command that runs testdata/fake-claude.sh.
func fakeClaude(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-claude.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return "sh " + abs
}

// runLogged runs one job with a fresh $FAKE_LOG and returns the reply, the
// argv line and the stdin the fake saw.
func runLogged(t *testing.T, m *Model, j Job) (Reply, string, string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "log")
	t.Setenv("FAKE_LOG", log)
	r := m.Run(context.Background(), j)
	argv, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := os.ReadFile(log + ".stdin")
	if err != nil {
		t.Fatal(err)
	}
	return r, strings.TrimSpace(string(argv)), string(stdin)
}

// flag returns the value after name in a space-joined argv, or "".
func flag(argv, name string) string {
	f := strings.Fields(argv)
	for i := 0; i+1 < len(f); i++ {
		if f[i] == name {
			return f[i+1]
		}
	}
	return ""
}

func TestModelInstanceResumesAndRecycles(t *testing.T) {
	k := kind.Kind{Name: "review", Model: "m-test", Prompt: "SETUP PROMPT", Allow: []string{"Bash(go test:*)"},
		RecycleUses: 2, RecycleContext: 120000, RecycleIdle: time.Hour}
	m := NewModel(k, "m-test", fakeClaude(t))
	j := Job{ID: "j1", Kind: "review", Commit: "c0ffee", Tree: "7ree", Args: []string{"x", "y"}, Text: "look at it", Dir: t.TempDir()}
	t.Setenv("FAKE_RESULT", "fine")

	r, argv, stdin := runLogged(t, m, j)
	if !r.OK || r.Output != "fine" || r.Context != 1000 {
		t.Fatalf("first reply %+v", r)
	}
	for _, want := range []string{"-p ", "--model m-test", "--output-format json", "--allowedTools Read,Grep,Glob,LS,Bash(go test:*)", "--max-turns 40"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("first argv %q lacks %q", argv, want)
		}
	}
	if flag(argv, "--effort") != "" {
		t.Fatalf("first argv %q: want no --effort for a job without one", argv)
	}
	id := flag(argv, "--session-id")
	if id == "" || id != m.SessionID() || flag(argv, "--resume") != "" {
		t.Fatalf("first argv %q: want --session-id %s and no --resume", argv, m.SessionID())
	}
	for _, want := range []string{"SETUP PROMPT\n\n", "job: j1 commit: c0ffee tree: 7ree", "look at it", "args: x y"} {
		if !strings.Contains(stdin, want) {
			t.Fatalf("first stdin %q lacks %q", stdin, want)
		}
	}
	now := time.Now()
	if m.Uses() != 1 || m.ShouldRecycle(now) {
		t.Fatalf("after one use: uses %d, ShouldRecycle %v", m.Uses(), m.ShouldRecycle(now))
	}
	if !m.ShouldRecycle(now.Add(2 * time.Hour)) {
		t.Fatal("idle past recycle_idle: want ShouldRecycle")
	}

	j.Effort = "high"
	_, argv, stdin = runLogged(t, m, j)
	if flag(argv, "--effort") != "high" {
		t.Fatalf("second argv %q: want --effort high", argv)
	}
	j.Effort = ""
	if flag(argv, "--resume") != id || flag(argv, "--session-id") != "" {
		t.Fatalf("second argv %q: want --resume %s and no --session-id", argv, id)
	}
	if strings.Contains(stdin, "SETUP PROMPT") || !strings.Contains(stdin, "look at it") {
		t.Fatalf("second stdin %q: want the job text without the prompt", stdin)
	}
	if m.Uses() != 2 || !m.ShouldRecycle(time.Now()) {
		t.Fatalf("after recycle_uses: uses %d, want ShouldRecycle", m.Uses())
	}

	m.Recycle()
	if m.Uses() != 0 || m.ShouldRecycle(time.Now().Add(2*time.Hour)) {
		t.Fatalf("after Recycle: uses %d, ShouldRecycle %v", m.Uses(), m.ShouldRecycle(time.Now().Add(2*time.Hour)))
	}
	_, argv, stdin = runLogged(t, m, j)
	nid := flag(argv, "--session-id")
	if nid == "" || nid == id || flag(argv, "--resume") != "" || !strings.Contains(stdin, "SETUP PROMPT") {
		t.Fatalf("after Recycle argv %q stdin %q: want a new --session-id and the prompt", argv, stdin)
	}

	t.Setenv("FAKE_CACHE", "200000")
	r, argv, _ = runLogged(t, m, j)
	if flag(argv, "--resume") != nid || r.Context != 201000 || m.LastContext() != 201000 {
		t.Fatalf("argv %q reply %+v: want --resume %s and context 201000", argv, r, nid)
	}
	if !m.ShouldRecycle(time.Now()) {
		t.Fatal("context past recycle_context: want ShouldRecycle")
	}
}

// A resumed run whose output does not parse leaves the session behind: the
// next run starts a new one with the prompt, though recycle_uses is 0.
func TestModelRotatesSessionAfterBadOutput(t *testing.T) {
	k := kind.Kind{Name: "review", Model: "m-test", Prompt: "SETUP PROMPT"}
	m := NewModel(k, "m-test", fakeClaude(t))
	j := Job{ID: "j1", Kind: "review", Text: "look", Dir: t.TempDir()}
	t.Setenv("FAKE_RESULT", "fine")
	_, argv, _ := runLogged(t, m, j)
	id := flag(argv, "--session-id")
	_, argv, _ = runLogged(t, m, j)
	if flag(argv, "--resume") != id {
		t.Fatalf("second argv %q: want --resume %s", argv, id)
	}
	for _, exit := range []string{"0", "1"} {
		t.Setenv("FAKE_NOJSON", "1")
		t.Setenv("FAKE_EXIT", exit)
		r, argv, _ := runLogged(t, m, j)
		if r.OK || flag(argv, "--resume") != id {
			t.Fatalf("exit %s: reply %+v argv %q: want a failed --resume %s", exit, r, argv, id)
		}
		t.Setenv("FAKE_NOJSON", "")
		t.Setenv("FAKE_EXIT", "0")
		_, argv, stdin := runLogged(t, m, j)
		nid := flag(argv, "--session-id")
		if nid == "" || nid == id || flag(argv, "--resume") != "" || !strings.Contains(stdin, "SETUP PROMPT") {
			t.Fatalf("after bad output (exit %s) argv %q stdin %q: want a new --session-id and the prompt", exit, argv, stdin)
		}
		_, argv, _ = runLogged(t, m, j)
		if flag(argv, "--resume") != nid {
			t.Fatalf("argv %q: want --resume %s", argv, nid)
		}
		id = nid
	}
}

func TestModelVerdictParsing(t *testing.T) {
	k := kind.Kind{Name: "review", Model: "m-test", Prompt: "p"}
	j := Job{ID: "j1", Kind: "review", Text: "t", Dir: t.TempDir()}
	for _, c := range []struct {
		name, result, exit string
		ok, judged         bool
	}{
		{"fail verdict", `findings...\nVERDICT: fail`, "0", false, true},
		{"ok verdict", "VERDICT: ok", "0", true, true},
		{"last verdict wins", `VERDICT: fail\nfixed\nVERDICT: OK `, "0", true, true},
		{"exit 1 beats ok verdict", `VERDICT: ok`, "1", false, false},
		{"no verdict exit 0", "all good", "0", true, true},
		{"no verdict exit 2", "broken", "2", false, false},
		{"bold line", `findings\n**VERDICT: fail**`, "0", false, true},
		{"bold key", `**VERDICT:** fail`, "0", false, true},
		{"bold value", `VERDICT: **ok**`, "0", true, true},
		{"heading and code", "## VERDICT: `fail`", "0", false, true},
		{"quoted", `> VERDICT: _fail_`, "0", false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("FAKE_RESULT", c.result)
			t.Setenv("FAKE_EXIT", c.exit)
			t.Setenv("FAKE_CACHE", "500")
			r := NewModel(k, "m-test", fakeClaude(t)).Run(context.Background(), j)
			if r.OK != c.ok || r.Judged != c.judged {
				t.Fatalf("OK, Judged = %v, %v, want %v, %v (%+v)", r.OK, r.Judged, c.ok, c.judged, r)
			}
			if r.Output != strings.ReplaceAll(c.result, `\n`, "\n") || c.exit == "0" && r.Context != 1500 {
				t.Fatalf("reply %+v: want the result text, and context 1500 on exit 0", r)
			}
		})
	}

	// R18: a prompt that asks for a verdict fails a reply without one.
	asks := kind.Kind{Name: "review", Model: "m-test", Prompt: "Review it. End with VERDICT: ok or VERDICT: fail."}
	for _, c := range []struct {
		result string
		ok     bool
	}{{"all good", false}, {"**VERDICT:** ok", true}, {"VERDICT: fail", false}} {
		t.Run("prompt asks: "+c.result, func(t *testing.T) {
			t.Setenv("FAKE_RESULT", c.result)
			r := NewModel(asks, "m-test", fakeClaude(t)).Run(context.Background(), j)
			if r.OK != c.ok || !r.Judged || r.Output != c.result {
				t.Fatalf("reply %+v: want OK %v, judged, and the result text", r, c.ok)
			}
		})
	}

	t.Run("is_error", func(t *testing.T) {
		t.Setenv("FAKE_RESULT", "VERDICT: ok")
		t.Setenv("FAKE_IS_ERROR", "1")
		r := NewModel(k, "m-test", fakeClaude(t)).Run(context.Background(), j)
		if r.OK || r.Judged || r.Output != "VERDICT: ok" {
			t.Fatalf("reply %+v: want unjudged with the result text", r)
		}
	})

	t.Run("not json", func(t *testing.T) {
		r := NewModel(k, "m-test", "echo").Run(context.Background(), j)
		if r.OK || r.Judged || !strings.HasPrefix(r.Output, "[bad claude output]") {
			t.Fatalf("reply %+v: want an unjudged bad output", r)
		}
		if got := r.Unjudged(); got != "model run failed: [bad claude output]; nothing recorded" {
			t.Fatalf("Unjudged %q", got)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		t.Setenv("FAKE_SLEEP", "5")
		kt := k
		kt.Timeout = time.Second
		start := time.Now()
		r := NewModel(kt, "m-test", fakeClaude(t)).Run(context.Background(), j)
		if r.OK || r.Judged || !strings.Contains(r.Output, "timeout") || time.Since(start) > 3*time.Second {
			t.Fatalf("reply %+v after %s: want a quick timeout", r, time.Since(start))
		}
	})
}

func TestParseResultForms(t *testing.T) {
	for _, in := range []string{
		`[{"type":"system"},{"type":"result","result":"r","usage":{"input_tokens":7}}]`,
		"{\"type\":\"system\"}\n{\"type\":\"result\",\"result\":\"r\",\"usage\":{\"input_tokens\":7}}\n",
	} {
		r, ok := parseResult([]byte(in))
		if !ok || r.Result != "r" || r.Usage.InputTokens != 7 {
			t.Fatalf("parseResult(%s) = %+v, %v", in, r, ok)
		}
	}
	if _, ok := parseResult([]byte(`{"type":"result"} trailing junk`)); ok {
		t.Fatal("junk after the result: want not parsed")
	}
}
