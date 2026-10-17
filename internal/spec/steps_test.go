package spec

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

func TestParseSteps(t *testing.T) {
	cases := []struct {
		name, body string
		want       []Step
	}{
		{"empty", "", nil},
		{"none", "None.", nil},
		{"nothing", "Nothing: it works as it is.", nil},
		{"bullets", "- set the key\n- restart the daemon\n", []Step{
			{N: 1, Text: "set the key"}, {N: 2, Text: "restart the daemon"}}},
		{"numbers and stars", "1. one\n2) two\n* three\n+ four", []Step{
			{N: 1, Text: "one"}, {N: 2, Text: "two"}, {N: 3, Text: "three"}, {N: 4, Text: "four"}}},
		{"boxes", "- [ricks-macbook] run `bmesh update`\n- [k16]\n- [Not a box] keep it\n- [a link](http://x) stays", []Step{
			{N: 1, Box: "ricks-macbook", Text: "run `bmesh update`"},
			{N: 2, Box: "k16", Text: ""},
			{N: 3, Text: "[Not a box] keep it"},
			{N: 4, Text: "[a link](http://x) stays"}}},
		{"checkboxes", "- [ ] open\n- [x] [box-1] ticked in markdown", []Step{
			{N: 1, Text: "open"}, {N: 2, Box: "box-1", Text: "ticked in markdown"}}},
		{"check", "- [k16] set `[quota] remind_topic` check: `grep -q remind_topic ~/.config/bmesh/config.toml`\n" +
			"- disable the timer; check: ! systemctl --user is-enabled old.timer\n" +
			"- Double-check: nothing to run\n" +
			"- an empty check:", []Step{
			{N: 1, Box: "k16", Text: "set `[quota] remind_topic`", Check: "grep -q remind_topic ~/.config/bmesh/config.toml"},
			{N: 2, Text: "disable the timer", Check: "! systemctl --user is-enabled old.timer"},
			{N: 3, Text: "Double-check: nothing to run"},
			{N: 4, Text: "an empty check:"}}},
		{"continuations", "- first line\n  goes on\n  - nested\n- second\nruns on\n\n  after a gap, indented", []Step{
			{N: 1, Text: "first line goes on - nested"},
			{N: 2, Text: "second runs on after a gap, indented"}}},
		{"lead-in", "On the Mac:\n- install the daemon;\n- run the join.\n\nThen archive the repo.", []Step{
			{N: 1, Text: "On the Mac: install the daemon;"},
			{N: 2, Text: "On the Mac: run the join."},
			{N: 3, Text: "Then archive the repo."}}},
		{"lead-in with a box", "[ricks-macbook] On the Mac:\n- one\n- [k16] two", []Step{
			{N: 1, Box: "ricks-macbook", Text: "On the Mac: one"},
			{N: 2, Box: "k16", Text: "On the Mac: two"}}},
		{"prose", "Whether the notice is on by default,\nas written, or opt-in.\n\nA second thought: maybe.", []Step{
			{N: 1, Text: "Whether the notice is on by default, as written, or opt-in."},
			{N: 2, Text: "A second thought: maybe."}}},
		{"colon not before a list", "Approve holds:\n\nnothing else", []Step{
			{N: 1, Text: "Approve holds:"}, {N: 2, Text: "nothing else"}}},
		{"crlf", "- a\r\n- b\r\n", []Step{{N: 1, Text: "a"}, {N: 2, Text: "b"}}},
	}
	for _, c := range cases {
		if got := ParseSteps(c.body); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}

func TestStepsRoundTrip(t *testing.T) {
	text := "---\nid: 7\nstatus: building\n---\n\n## Proposal\nx\n\n## Needs from the operator\n- [k16] a <b> & c check: `test -f x`\n- two\n"
	s, err := Parse(text, "docs/proposals/007-x.md")
	if err != nil {
		t.Fatal(err)
	}
	steps := s.NeedsSteps()
	if len(steps) != 2 || steps[0].Check != "test -f x" {
		t.Fatalf("%+v", steps)
	}
	v, err := EncodeSteps(steps)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"n":1,"box":"k16","text":"a <b> & c","check":"test -f x","done":""},{"n":2,"box":"","text":"two","done":""}]`
	if v != want {
		t.Fatalf("encoded %s", v)
	}
	out, err := Update(s, Field{StepsKey, v})
	if err != nil {
		t.Fatal(err)
	}
	n, err := Parse(out, s.Path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := n.Steps()
	if err != nil || !reflect.DeepEqual(got, steps) {
		t.Fatalf("%+v %v", got, err)
	}
	if fm, err := Frontmatter(out); err != nil || fm[StepsKey] != v {
		t.Fatalf("frontmatter %q %v", fm[StepsKey], err)
	}
	if Open(got) != 2 {
		t.Fatal(Open(got))
	}
	got[1].Done, got[1].Note = "2026-10-17T10:00:00Z k16", "by hand"
	if Open(got) != 1 || got[1].String() != "2. two -- done 2026-10-17T10:00:00Z k16: by hand" ||
		got[0].String() != "1. [k16] a <b> & c (check: test -f x)" {
		t.Fatalf("%d %q %q", Open(got), got[0], got[1])
	}
	again := CarryTicks(got, ParseSteps("- two\n- three\n- two"))
	if again[0].Done == "" || again[1].Done != "" || again[2].Done != "" { // one tick carries once
		t.Fatalf("carried %+v", again)
	}
	kept := KeepBoxes(ParseSteps("- [k16] a\n- [optional] b\n- c"), func(b string) bool { return b == "k16" })
	if kept[0].Box != "k16" || kept[1].Box != "" || kept[1].Text != "[optional] b" || kept[2].Text != "c" {
		t.Fatalf("kept %+v", kept)
	}
	if v, err := EncodeSteps(nil); err != nil || v != "[]" {
		t.Fatalf("no steps: %q %v", v, err)
	}
	if _, err := DecodeSteps("[{"); err == nil {
		t.Fatal("bad json read")
	}
}

func TestCheckStep(t *testing.T) {
	repo := t.TempDir()
	meshtest.Git(t, repo, "init", "-b", "main")
	steps := `[{"n":1,"box":"box-a","text":"a","check":"test -f a.txt","done":""},` +
		`{"n":2,"box":"box-a","text":"b","check":"echo nope; exit 3","done":""},` +
		`{"n":3,"box":"box-a","text":"c","done":""},{"n":4,"box":"box-b","text":"d","check":"true","done":""}]`
	meshtest.CommitFiles(t, repo, map[string]string{
		"a.txt":                   "a\n",
		"docs/proposals/007-x.md": "---\nid: 7\nstatus: done\noperator_steps: " + steps + "\n---\nbody\n",
	})
	ctx := context.Background()
	for _, c := range []struct {
		n    int
		ok   bool
		text string
	}{
		{1, true, "exit 0"},
		{2, false, "exit 3: nope"},
		{3, false, "refused: step 3 has no check"},
		{4, false, `refused: step 4 is for "box-b", not box-a`},
		{5, false, "refused: spec 007 has no step 5"},
	} {
		ok, text := CheckStep(ctx, repo, "docs/proposals/{id}-*.md", "box-a", "007", c.n)
		if ok != c.ok || text != c.text {
			t.Errorf("step %d: %v %q, want %v %q", c.n, ok, text, c.ok, c.text)
		}
	}
	if ok, text := CheckStep(ctx, repo, "docs/proposals/{id}-*.md", "box-a", "008", 1); ok || !strings.HasPrefix(text, "refused: no spec 008") {
		t.Errorf("missing spec: %v %q", ok, text)
	}
}
