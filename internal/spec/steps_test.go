package spec

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
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

func TestOperatorSteps(t *testing.T) {
	needs := "\n## Proposal\nx\n\n## Needs from the operator\n- [k16] a <b> & c check: `test -f x`\n- [optional] two\n"
	parse := func(front string) *Spec {
		t.Helper()
		s, err := Parse("---\nid: 7\n"+front+"---\n"+needs, "docs/proposals/007-x.md")
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	isBox := func(b string) bool { return b == "k16" }
	// Landed with trailers: the Needs section, boxes as the roster names them.
	got, err := parse("status: done\n").OperatorSteps(isBox)
	want := []Step{{N: 1, Box: "k16", Text: "a <b> & c", Check: "test -f x"}, {N: 2, Text: "[optional] two"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v %v", got, err)
	}
	// Not landed: none.
	if got, _ := parse("status: accepted\n").OperatorSteps(isBox); got != nil {
		t.Fatalf("accepted: %+v", got)
	}
	// Landed before trailers: operator_steps as recorded, ticks included,
	// and none without the field.
	old := parse(`status: done` + "\nmerged: abc\n" + `operator_steps: [{"n":1,"box":"","text":"one","done":"2026-10-17T10:00:00Z k16"}]` + "\n")
	if got, err := old.OperatorSteps(isBox); err != nil || len(got) != 1 || got[0].Done == "" || Open(got) != 0 {
		t.Fatalf("legacy: %+v %v", got, err)
	}
	if got, _ := parse("status: done\nmerged: abc\n").OperatorSteps(isBox); got != nil {
		t.Fatalf("legacy with none: %+v", got)
	}
	// The hub's ticks lay on by number; a tick the file has stays.
	ticked := Ticked(want, []mesh.StepTick{{N: 2, Done: "2026-10-17T10:00:00Z k16", Note: "by hand"}, {N: 9, Done: "x"}})
	if Open(ticked) != 1 || ticked[1].String() != "2. [optional] two -- done 2026-10-17T10:00:00Z k16: by hand" ||
		ticked[0].String() != "1. [k16] a <b> & c (check: test -f x)" {
		t.Fatalf("%d %q %q", Open(ticked), ticked[0], ticked[1])
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
