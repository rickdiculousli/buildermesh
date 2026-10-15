package spec

import (
	"reflect"
	"testing"
)

func TestParseReplyTodaysGrammar(t *testing.T) {
	ids := map[int]bool{12: true, 13: true, 14: true, 15: true, 16: true}
	ranks := []int{12, 13, 14, 15, 16}
	r := ParseReply("1 and 3 yes, 2 smaller, 4 no, 5 later, 3 on opus", ids, ranks)
	want := []Verdict{
		{ID: 12, Move: "accepted"}, {ID: 14, Move: "accepted"},
		{ID: 13, Move: "adjusting", Note: "smaller"},
		{ID: 15, Move: "rejected"},
		{ID: 16, Move: "later"},
		{ID: 14, Judge: "opus"},
	}
	if !reflect.DeepEqual(r.Verdicts, want) || len(r.Unmatched) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestParseReplyPitchedIDsThenRanksThenOtherIDs(t *testing.T) {
	ids := map[int]bool{1: true, 2: true, 7: true}
	r := ParseReply("1 yes; 7 no, too big; 2 later", ids, []int{7, 2, 1})
	want := []Verdict{{ID: 1, Move: "accepted"}, {ID: 7, Move: "rejected"}, {ID: 2, Move: "later"}}
	if !reflect.DeepEqual(r.Verdicts, want) {
		t.Fatalf("%+v", r.Verdicts)
	}
	// ", too big" was its own clause with no number: reported, not guessed.
	if !reflect.DeepEqual(r.Unmatched, []string{"too big"}) {
		t.Fatalf("%q", r.Unmatched)
	}
	// A repo with history: 001 and 003 exist but were not pitched, so "1
	// and 3" count lines of the pitch; 7 is an older spec outside it.
	old := map[int]bool{1: true, 3: true, 7: true, 12: true, 13: true, 14: true}
	r = ParseReply("1 and 3 yes, 7 on opus, 9 later", old, []int{12, 13, 14})
	want = []Verdict{{ID: 12, Move: "accepted"}, {ID: 14, Move: "accepted"}, {ID: 7, Judge: "opus"}}
	if !reflect.DeepEqual(r.Verdicts, want) || !reflect.DeepEqual(r.Unmatched, []string{"no spec 9 (9 later)"}) {
		t.Fatalf("%+v", r)
	}
}

func TestParseReplyRejectReasonAndJudgeOnVerdict(t *testing.T) {
	ids := map[int]bool{12: true, 13: true}
	r := ParseReply("012 no: duplicates the cache work\n013 yes on sonnet", ids, nil)
	want := []Verdict{{ID: 12, Move: "rejected", Note: "duplicates the cache work"}, {ID: 13, Move: "accepted", Judge: "sonnet"}}
	if !reflect.DeepEqual(r.Verdicts, want) {
		t.Fatalf("%+v", r.Verdicts)
	}
}

func TestParseReplyReportsUnmatched(t *testing.T) {
	ids := map[int]bool{12: true}
	r := ParseReply("99 yes, 12, do the thing, 12 yes but only the CLI part", ids, nil)
	if len(r.Verdicts) != 1 || r.Verdicts[0] != (Verdict{ID: 12, Move: "adjusting", Note: "but only the CLI part"}) {
		t.Fatalf("%+v", r.Verdicts)
	}
	if !reflect.DeepEqual(r.Unmatched, []string{"no spec 99 (99 yes)", "12", "do the thing"}) {
		t.Fatalf("%q", r.Unmatched)
	}
	if e := ParseReply("", ids, nil); len(e.Verdicts) != 0 || len(e.Unmatched) != 0 {
		t.Fatalf("%+v", e)
	}
}

func TestParseReplyNumbersInsideAnInstructionAreWords(t *testing.T) {
	ids := map[int]bool{2: true, 13: true, 14: true}
	r := ParseReply("13 split into 14 parts; 14 smaller, cap at 2 files; 13 and 14 yes", ids, nil)
	want := []Verdict{
		{ID: 13, Move: "adjusting", Note: "split into 14 parts"},
		{ID: 14, Move: "adjusting", Note: "smaller"},
		{ID: 13, Move: "accepted"}, {ID: 14, Move: "accepted"},
	}
	if !reflect.DeepEqual(r.Verdicts, want) {
		t.Fatalf("%+v", r.Verdicts)
	}
	if !reflect.DeepEqual(r.Unmatched, []string{"cap at 2 files"}) {
		t.Fatalf("%q", r.Unmatched)
	}
}

func TestParseReplyANumberAfterASpokenVerdictStartsAClause(t *testing.T) {
	ids := map[int]bool{2: true, 3: true, 4: true, 5: true}
	r := ParseReply("3 yes and 4 no too big and vague; 2 merge with spec 3 and drop the cache; 5 later 2 on opus", ids, nil)
	want := []Verdict{
		{ID: 3, Move: "accepted"},
		{ID: 4, Move: "rejected", Note: "too big and vague"},
		{ID: 2, Move: "adjusting", Note: "merge with spec 3 and drop the cache"},
		{ID: 5, Move: "later"},
		{ID: 2, Judge: "opus"},
	}
	if !reflect.DeepEqual(r.Verdicts, want) || len(r.Unmatched) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestParseReplyFirstWordDecides(t *testing.T) {
	ids := map[int]bool{2: true, 4: true, 5: true, 6: true}
	r := ParseReply("2 make it work with no network; 4 no because 5 covers it; 5 do not build the cli twice; 6 later on sonnet; 2 on opus but shorter", ids, nil)
	want := []Verdict{
		{ID: 2, Move: "adjusting", Note: "make it work with no network"},
		{ID: 4, Move: "rejected", Note: "because 5 covers it"},
		{ID: 5, Move: "adjusting", Note: "do not build the cli twice"},
		{ID: 6, Move: "later", Judge: "sonnet"},
		{ID: 2, Move: "adjusting", Note: "but shorter", Judge: "opus"},
	}
	if !reflect.DeepEqual(r.Verdicts, want) || len(r.Unmatched) != 0 {
		t.Fatalf("%+v", r)
	}
	if c := clauses("3 yes and 4 no, 4 no because 5 covers it and 6 later, 11 smaller and 12 no"); !reflect.DeepEqual(c, []string{"3 yes", "4 no", "4 no because 5 covers it", "6 later", "11 smaller", "12 no"}) {
		t.Fatalf("%q", c)
	}
}

func TestRanksAndAdjustNote(t *testing.T) {
	specs := []*Spec{
		{ID: 3, Fields: map[string]string{"status": "done"}},
		{ID: 5, Fields: map[string]string{"status": "proposed"}},
		{ID: 4, Fields: map[string]string{"status": "later"}},
		{ID: 8, Fields: map[string]string{"status": "proposed"}},
	}
	pitch := "005 A (feature, S, opus): why\n003 B: done already\n  008 C (ux, M, fable): look\n005 A again\n099 nobody\nno number here"
	if got := PitchRanks(pitch, specs); !reflect.DeepEqual(got, []int{5, 3, 8}) {
		t.Fatal(got)
	}
	if got := PitchRanks("", specs); len(got) != 0 {
		t.Fatal(got)
	}
	body := "## Opportunity\nslow\n\n## Proposal\nThe change.\n"
	got := AdjustNote(body, "2026-10-15", "smaller")
	if got != "## Opportunity\nslow\n\n## Proposal\nAdjust (2026-10-15): smaller\nThe change.\n" {
		t.Fatalf("%q", got)
	}
	if got := AdjustNote("no headings", "d", "n"); got != "no headings\n\nAdjust (d): n\n" {
		t.Fatalf("%q", got)
	}
}
