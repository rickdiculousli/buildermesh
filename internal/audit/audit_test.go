package audit

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The transcripts in testdata are real Claude Code sessions of the work
// skill, cut from the skill test's cells (scripts/skilltest, sandbox repo
// shop): only user, assistant and system lines, thinking dropped, tool
// output clipped, replies whole, the cell's paths and the home rewritten.
// wrong is a session whose named rung its actions don't bear out: it named
// a plan in the turn it started a lead.
func TestRealTranscripts(t *testing.T) {
	for _, c := range []struct {
		file           string
		named, implied Rung
		acted          string
		reads          []string // kinds and names it read before acting, in order, a prefix
	}{
		{"here", Here, Here, "git commit", []string{"skill buildermesh:work", "bmesh mesh-status"}},
		{"answer", Answer, Answer, "", []string{"skill buildermesh:work"}},
		{"plan", Plan, Plan, "", []string{"skill buildermesh:work"}},
		{"leads", Leads, Leads, "bmesh node show", []string{"skill buildermesh:work", "bmesh mesh-status; spec list"}},
		{"round", Round, Round, "bmesh ideate", []string{"skill buildermesh:work"}},
		{"inflight", Answer, Answer, "bmesh node show", []string{"skill buildermesh:work"}},
		{"wrong", Plan, Leads, "bmesh build", []string{"skill buildermesh:work"}},
	} {
		a, err := File(filepath.Join("testdata", c.file+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if len(a.Turns) != 1 || a.Calls == 0 || a.Tokens == 0 || a.Cwd == "" {
			t.Fatalf("%s: %d turns, %d calls, %d tokens, cwd %q", c.file, len(a.Turns), a.Calls, a.Tokens, a.Cwd)
		}
		tn := a.Turns[0]
		if tn.Named != c.named || tn.Implied != c.implied || tn.Mismatch != (c.named != c.implied) || tn.Acted != c.acted {
			t.Errorf("%s: named %q implied %q (%s) mismatch %v acted %q; want %q %q %q", c.file, tn.Named, tn.Implied, tn.Rule, tn.Mismatch, tn.Acted, c.named, c.implied, c.acted)
		}
		if (tn.Acted == "") != (tn.Context == 0) {
			t.Errorf("%s: acted %q at context %d", c.file, tn.Acted, tn.Context)
		}
		var reads []string
		for _, r := range tn.Read {
			reads = append(reads, r.Kind+" "+r.Name)
			if r.Words == 0 && r.Kind != "bmesh" {
				t.Errorf("%s: %s %s read no words", c.file, r.Kind, r.Name)
			}
		}
		if len(reads) < len(c.reads) || !slices.Equal(reads[:len(c.reads)], c.reads) {
			t.Errorf("%s: read %q, want it to open with %q", c.file, reads, c.reads)
		}
	}
}

// A resumed session writes earlier lines again; a turn is read once, a
// refused command is no action, and a sidechain is summed apart.
func TestTranscriptLines(t *testing.T) {
	lines := strings.Join([]string{
		`{"type":"user","uuid":"u1","cwd":"/w","sessionId":"s1","message":{"role":"user","content":"fix it"}}`,
		`{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"bmesh nope"}}],"usage":{"input_tokens":1,"cache_read_input_tokens":9000,"cache_creation_input_tokens":0}}}`,
		`{"type":"user","uuid":"u2","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"unknown command","is_error":true}]}}`,
		`{"type":"assistant","uuid":"s1","isSidechain":true,"message":{"id":"x1","content":[{"type":"text","text":"Starting leads for 1"}],"usage":{"input_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`,
		`{"type":"assistant","uuid":"a2","message":{"id":"m2","content":[{"type":"text","text":"Answering, no change.\n\nIt was fine."}],"usage":{"input_tokens":1,"cache_read_input_tokens":9500,"cache_creation_input_tokens":0}}}`,
		`{"type":"user","uuid":"u3","message":{"role":"user","content":"<task-notification>done</task-notification>"}}`,
		`{"type":"user","uuid":"u1","cwd":"/w","sessionId":"s1","message":{"role":"user","content":"fix it"}}`,
	}, "\n")
	a, err := Transcript(strings.NewReader(lines))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Turns) != 1 || a.Calls != 2 || a.Tokens != 9501 || a.Subagents.Calls != 1 || a.Subagents.Read != 5 {
		t.Fatalf("%+v", a)
	}
	tn := a.Turns[0]
	if tn.Acted != "" || tn.Named != Answer || tn.Implied != Answer || tn.Mismatch || len(tn.Read) != 1 || tn.Read[0].Name != "nope" {
		t.Fatalf("%+v", tn)
	}
}
