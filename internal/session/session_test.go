package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

var defaults = config.ContextConfig{NudgeFrom: 250000, NudgeEvery: 50000, CompactFrom: 300000, CompactSoon: 400000}

// assistant is a transcript line for an assistant message whose context
// is tokens, split as Claude Code records it.
func assistant(tokens int) string {
	return fmt.Sprintf(`{"type":"assistant","isSidechain":false,"message":{"model":"claude-opus-5-5","role":"assistant","usage":{"input_tokens":2,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":300}}}`,
		tokens/4, tokens-2-tokens/4)
}

// transcript writes lines as session id's transcript under a fake Claude
// config dir and returns a getenv naming both.
func transcript(t *testing.T, id string, lines ...string) func(string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "projects", "-home-someone-repo", id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return func(k string) string {
		return map[string]string{"CLAUDE_CODE_SESSION_ID": id, "CLAUDE_CONFIG_DIR": dir}[k]
	}
}

func TestMeasureReadsNewestMainThreadUsage(t *testing.T) {
	getenv := transcript(t, "ec03a02c-1",
		`{"type":"user","message":{"role":"user","content":"hi"}}`,
		assistant(100000),
		assistant(312456),
		`{"type":"assistant","isSidechain":true,"message":{"usage":{"input_tokens":5,"cache_read_input_tokens":900000}}}`,
		`{"type":"assistant","message":{"model":"<synthetic>","usage":{"input_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"the assistant said"}]}}`,
		`{"type":"attachment","attachment":{}}`)
	r, ok := Measure(getenv)
	if !ok || r.Tokens != 312456 || r.Harness != "claude" || r.ID != "ec03a02c-1" || r.Short() != "ec03a02c" {
		t.Fatalf("Measure = %+v, %v; want 312456 from claude session ec03a02c-1", r, ok)
	}
}

func TestMeasureSaysNothingItCantRead(t *testing.T) {
	for name, getenv := range map[string]func(string) string{
		"no session": func(string) string { return "" },
		"unknown session": func(k string) string {
			return map[string]string{"CLAUDE_CODE_SESSION_ID": "nope", "CLAUDE_CONFIG_DIR": t.TempDir()}[k]
		},
		"a path for an id":     transcript(t, "x", assistant(300000)),
		"no usage":             transcript(t, "s1", `{"type":"user","message":{}}`, `{"type":"assistant","message":{"content":[]}}`),
		"not json":             transcript(t, "s2", `garbage "assistant"`),
		"compacted since":      transcript(t, "s3", assistant(380000), `{"type":"system","subtype":"compact_boundary","content":"Conversation compacted"}`),
		"empty transcript":     transcript(t, "s4"),
		"only synthetic":       transcript(t, "s5", `{"type":"assistant","message":{"model":"<synthetic>","usage":{"input_tokens":0}}}`),
		"only a sidechain":     transcript(t, "s6", `{"type":"assistant","isSidechain":true,"message":{"usage":{"input_tokens":300000}}}`),
		"another session's id": transcript(t, "s7", assistant(300000)),
	} {
		env := getenv
		switch name {
		case "a path for an id":
			env = func(k string) string {
				if k == "CLAUDE_CODE_SESSION_ID" {
					return "../x"
				}
				return getenv(k)
			}
		case "another session's id":
			env = func(k string) string {
				if k == "CLAUDE_CODE_SESSION_ID" {
					return "s8"
				}
				return getenv(k)
			}
		}
		if r, ok := Measure(env); ok {
			t.Errorf("%s: Measure = %+v; want nothing", name, r)
		}
	}
}

func TestMeasureAfterCompactionReadsTheNewContext(t *testing.T) {
	getenv := transcript(t, "c1", assistant(410000), `{"type":"system","subtype":"compact_boundary"}`, assistant(42000))
	if r, ok := Measure(getenv); !ok || r.Tokens != 42000 {
		t.Fatalf("Measure = %+v, %v; want 42000", r, ok)
	}
}

func TestMeasureReadsALargeTranscriptsTail(t *testing.T) {
	// The newest usage sits behind a tool result past the first window, in
	// a transcript bigger than the widest: the reader widens to find it and
	// never reads the start, where an older usage is.
	big := strings.Repeat("x", 3<<20)
	lines := []string{assistant(111000)}
	for range 6 {
		lines = append(lines, `{"type":"user","message":{"content":"`+big+`"}}`)
	}
	lines = append(lines, assistant(333000), `{"type":"user","message":{"content":"`+big+`"}}`)
	getenv := transcript(t, "big", lines...)
	if r, ok := Measure(getenv); !ok || r.Tokens != 333000 {
		t.Fatalf("Measure = %+v, %v; want 333000", r, ok)
	}
	// Nothing in the widest window: nothing, never the old usage.
	lines = []string{assistant(111000)}
	for range 7 {
		lines = append(lines, `{"type":"user","message":{"content":"`+big+`"}}`)
	}
	if r, ok := Measure(transcript(t, "old", lines...)); ok {
		t.Fatalf("Measure = %+v; want nothing past %d bytes", r, tailMax)
	}
}

func TestHeardSaysEachCrossingOnceInOrder(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	r := Reading{Harness: "claude", ID: "s"}
	var said []string
	for _, n := range []int{100000, 249999, 250000, 260000, 299000, 304000, 310000, 351000, 352000, 404000, 420000, 455000} {
		r.Tokens = n
		if l := Heard(dir, defaults, r, now); l != "" {
			said = append(said, l)
		}
	}
	want := []string{
		"bmesh: this session's context is 250k. You'll hear again every 50k.",
		"bmesh: this session's context is 304k. Compact at your next milestone, after updating your state file; never while exploring or investigating.",
		"bmesh: this session's context is 351k. Compact at your next milestone, after updating your state file; never while exploring or investigating.",
		"bmesh: this session's context is 404k. Compact at the next safe point, after updating your state file.",
		"bmesh: this session's context is 455k. Compact at the next safe point, after updating your state file.",
	}
	if strings.Join(said, "\n") != strings.Join(want, "\n") {
		t.Fatalf("said:\n%s\nwant:\n%s", strings.Join(said, "\n"), strings.Join(want, "\n"))
	}
}

func TestHeardJumpSaysTheHighestOnce(t *testing.T) {
	dir := t.TempDir()
	r := Reading{Harness: "claude", ID: "s", Tokens: 420000}
	if l := Heard(dir, defaults, r, time.Now()); !strings.Contains(l, "420k. Compact at the next safe point") {
		t.Fatalf("Heard = %q", l)
	}
	if l := Heard(dir, defaults, r, time.Now()); l != "" {
		t.Fatalf("again: %q; want nothing", l)
	}
}

func TestHeardDropResets(t *testing.T) {
	dir := t.TempDir()
	r := Reading{Harness: "claude", ID: "s", Tokens: 410000}
	Heard(dir, defaults, r, time.Now())
	r.Tokens = 60000 // compacted
	if l := Heard(dir, defaults, r, time.Now()); l != "" {
		t.Fatalf("drop: %q; want nothing", l)
	}
	r.Tokens = 251000
	if l := Heard(dir, defaults, r, time.Now()); !strings.HasPrefix(l, "bmesh: this session's context is 251k.") {
		t.Fatalf("after the drop: %q; want the 250k nudge again", l)
	}
	// A drop to a lower nudge says nothing, and the crossings past it are said again.
	r.Tokens = 405000
	Heard(dir, defaults, r, time.Now())
	r.Tokens = 310000
	if l := Heard(dir, defaults, r, time.Now()); l != "" {
		t.Fatalf("drop to 310k: %q; want nothing", l)
	}
	r.Tokens = 352000
	if l := Heard(dir, defaults, r, time.Now()); !strings.Contains(l, "352k") {
		t.Fatalf("after the drop to 310k: %q; want 350k said again", l)
	}
}

func TestHeardSessionsApartAndOff(t *testing.T) {
	dir := t.TempDir()
	a := Reading{Harness: "claude", ID: "a", Tokens: 260000}
	b := Reading{Harness: "claude", ID: "b", Tokens: 260000}
	if Heard(dir, defaults, a, time.Now()) == "" || Heard(dir, defaults, b, time.Now()) == "" {
		t.Fatal("each session hears its own crossing")
	}
	off := defaults
	off.NudgeFrom = 0
	if l := Heard(dir, off, Reading{ID: "c", Tokens: 900000}, time.Now()); l != "" {
		t.Fatalf("nudge_from = 0: %q; want nothing", l)
	}
	if _, err := os.Stat(filepath.Join(dir, "context", "c")); !os.IsNotExist(err) {
		t.Fatalf("nudge_from = 0 wrote state: %v", err)
	}
}

func TestHeardPrunesWeekOldSessions(t *testing.T) {
	dir := t.TempDir()
	Heard(dir, defaults, Reading{ID: "old", Tokens: 260000}, time.Now())
	old := filepath.Join(dir, "context", "old")
	week := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(old, week, week); err != nil {
		t.Fatal(err)
	}
	Heard(dir, defaults, Reading{ID: "new", Tokens: 260000}, time.Now())
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("a session unchanged 8 days is kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "context", "new")); err != nil {
		t.Fatal(err)
	}
}

func TestLevelAndAdvice(t *testing.T) {
	for _, tc := range []struct {
		c      config.ContextConfig
		tokens int
		level  int
		advice string
	}{
		{defaults, 249999, 0, ""},
		{defaults, 299999, 250000, "You'll hear again every 50k."},
		{defaults, 300000, 300000, "Compact at your next milestone, after updating your state file; never while exploring or investigating."},
		{defaults, 999999, 950000, "Compact at the next safe point, after updating your state file."},
		{config.ContextConfig{NudgeFrom: 250000, CompactFrom: 320000}, 330000, 320000, "Compact at your next milestone, after updating your state file; never while exploring or investigating."},
		{config.ContextConfig{NudgeFrom: 250000, CompactFrom: 320000}, 300000, 250000, "You'll hear again at 320k."},
		{config.ContextConfig{NudgeFrom: 250000, NudgeEvery: 50000, CompactFrom: 320000}, 325000, 320000, "Compact at your next milestone, after updating your state file; never while exploring or investigating."},
		{config.ContextConfig{NudgeFrom: 250000}, 900000, 250000, ""},
	} {
		level := Level(tc.c, tc.tokens)
		if level != tc.level || Advice(tc.c, level) != tc.advice {
			t.Errorf("%+v at %d: level %d, %q; want %d, %q", tc.c, tc.tokens, level, Advice(tc.c, level), tc.level, tc.advice)
		}
	}
}
