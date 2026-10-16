package spec

import (
	"reflect"
	"strings"
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

func TestParseReplyThreeDigitsAreIDsShortNumbersArePositions(t *testing.T) {
	// No pitch: every number is a spec id.
	ids := map[int]bool{1: true, 2: true, 7: true}
	r := ParseReply("1 yes; 7 no, too big; 2 later", ids, nil)
	want := []Verdict{{ID: 1, Move: "accepted"}, {ID: 7, Move: "rejected"}, {ID: 2, Move: "later"}}
	if !reflect.DeepEqual(r.Verdicts, want) {
		t.Fatalf("%+v", r.Verdicts)
	}
	// ", too big" was its own clause with no number: reported, not guessed.
	if !reflect.DeepEqual(r.Unmatched, []string{"too big"}) {
		t.Fatalf("%q", r.Unmatched)
	}
	// A pitch: short numbers count its items, three digits name specs,
	// pitched or not; a position past the pitch is reported.
	old := map[int]bool{1: true, 3: true, 7: true, 12: true, 13: true, 14: true}
	r = ParseReply("1 and 3 yes, 007 on opus, 9 later, 012 no, 099 yes", old, []int{12, 13, 14})
	want = []Verdict{{ID: 12, Move: "accepted"}, {ID: 14, Move: "accepted"}, {ID: 7, Judge: "opus"}, {ID: 12, Move: "rejected"}}
	if !reflect.DeepEqual(r.Verdicts, want) || !reflect.DeepEqual(r.Unmatched, []string{"no position 9 in the pitch; a spec id has three digits (9 later)", "no spec 099 (099 yes)"}) {
		t.Fatalf("%+v", r)
	}
	// A first round numbers from 001 with the gap: a short number that is
	// a pitched id and a different position is ambiguous; one that is the
	// same spec either way, or an id past the pitch's length, is that spec.
	first := map[int]bool{1: true, 2: true, 3: true, 6: true, 7: true, 8: true}
	r = ParseReply("1 yes, 5 yes, 6 no, 8 later, 006 no", first, []int{1, 2, 3, 6, 7, 8})
	want = []Verdict{{ID: 1, Move: "accepted"}, {ID: 7, Move: "accepted"}, {ID: 8, Move: "later"}, {ID: 6, Move: "rejected"}}
	if !reflect.DeepEqual(r.Verdicts, want) ||
		!reflect.DeepEqual(r.Unmatched, []string{"ambiguous 6 (spec 006 or item 6, 008); write the id with three digits (6 no)"}) {
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
	// A pitch typed as one line, as a thread's --text often is.
	many := []*Spec{{ID: 1, Fields: map[string]string{"status": "proposed"}}, {ID: 2, Fields: map[string]string{"status": "proposed"}},
		{ID: 3, Fields: map[string]string{"status": "proposed"}}, {ID: 200, Fields: map[string]string{"status": "proposed"}}}
	line := "001 Build from committed data (maintenance, M, opus): no .cache anywhere. 002 Optional Whisper group (time-cost, S, sonnet): 5.6 GB venv and 18.6 s per run for 43 tests. 003 Tested drill timer (maintenance, S, opus): 200 lines of guide.js, no test."
	if got := PitchRanks(line, many); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatal(got)
	}
	// One item per line: an id cited inside a why is not an item.
	cited := "001 Ledger (feature, S, opus): replaces the scan; 003 did it by hand.\n002 Review (feature, M, opus): why. 200 lines go.\n003 Push (fix, S, sonnet): why."
	if got := PitchRanks(cited, many); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatal(got)
	}
	cited2 := "001 Ledger (feature, S, opus): replaces the scan. 002 did it by hand.\n003 Push (fix, S, sonnet): why."
	if got := PitchRanks(cited2, many); !reflect.DeepEqual(got, []int{1, 3}) {
		t.Fatal(got)
	}
	// The round joins two one-line pitches with a line break: each line is
	// a list of its own; a blocked round's text puts the pitch after a note.
	four := append(many, &Spec{ID: 4, Fields: map[string]string{"status": "proposed"}})
	merged := "001 A (f, S, opus): x. 002 Retry (again) v1.2 uploads (fix, S, opus): y.\n003 C (ux, S, opus): z (see 001). 004 D (ux, S, opus): w."
	if got := PitchRanks(merged, four); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Fatal(got)
	}
	mixed := "001 A (f, S, opus): x. 002 B (f, S, opus): y.\n003 C"
	if got := PitchRanks(mixed, four); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatal(got)
	}
	blocked := "ux blocked: no display\n\n001 A (f, S, opus): x. 002 B (f, S, opus): y."
	if got := PitchRanks(blocked, four); !reflect.DeepEqual(got, []int{1, 2}) {
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

// The replies the choose-ideas skill's description gives as examples, and
// the ones GitHub issue 5 found unmatched, all parse.
func TestParseReplyVerdictFirstAllAndBareNumbers(t *testing.T) {
	ids := map[int]bool{1: true, 2: true, 3: true, 4: true, 6: true, 7: true, 8: true, 9: true, 12: true, 13: true}
	ranks := []int{1, 2, 3, 4, 6, 7, 8, 9}
	acc := func(ids ...int) []Verdict {
		var out []Verdict
		for _, id := range ids {
			out = append(out, Verdict{ID: id, Move: "accepted"})
		}
		return out
	}
	for reply, want := range map[string]Review{
		"accept 012":                     {Verdicts: acc(12)},
		"reject 2":                       {Verdicts: []Verdict{{ID: 2, Move: "rejected"}}},
		"reject 2 too big":               {Verdicts: []Verdict{{ID: 2, Move: "rejected", Note: "too big"}}},
		"all yes":                        {Verdicts: acc(ranks...)},
		"yes to all":                     {Verdicts: acc(ranks...)},
		"all later on opus":              {Verdicts: []Verdict{{ID: 1, Move: "later", Judge: "opus"}, {ID: 2, Move: "later", Judge: "opus"}, {ID: 3, Move: "later", Judge: "opus"}, {ID: 4, Move: "later", Judge: "opus"}, {ID: 6, Move: "later", Judge: "opus"}, {ID: 7, Move: "later", Judge: "opus"}, {ID: 8, Move: "later", Judge: "opus"}, {ID: 9, Move: "later", Judge: "opus"}}},
		"1, 2 and 3 yes":                 {Verdicts: acc(1, 2, 3)},
		"accept 012 and reject 013":      {Verdicts: []Verdict{{ID: 12, Move: "accepted"}, {ID: 13, Move: "rejected"}}},
		"build 1, 2, 4 no":               {Verdicts: []Verdict{{ID: 1, Move: "accepted"}, {ID: 2, Move: "accepted"}, {ID: 4, Move: "rejected"}}},
		"yes 1 and 3 on opus":            {Verdicts: []Verdict{{ID: 1, Move: "accepted", Judge: "opus"}, {ID: 3, Move: "accepted", Judge: "opus"}}},
		"1, 2, do it later, 3 yes":       {Verdicts: acc(3), Unmatched: []string{"1", "2", "do it later"}},
		"approve 1, 2 later, 3":          {Verdicts: []Verdict{{ID: 1, Move: "accepted"}, {ID: 2, Move: "later"}}, Unmatched: []string{"3"}},
		"1 smaller and 2 no, accept 004": {Verdicts: []Verdict{{ID: 1, Move: "adjusting", Note: "smaller"}, {ID: 2, Move: "rejected"}, {ID: 4, Move: "accepted"}}},
		"Work on 1, 2, 3, 4, 6, 7, 8, and 9.": {Verdicts: acc(1, 2, 3, 4, 9), Unmatched: []string{
			"ambiguous 6 (spec 006 or item 6, 007); write the id with three digits (6)",
			"ambiguous 7 (spec 007 or item 7, 008); write the id with three digits (7)",
			"ambiguous 8 (spec 008 or item 8, 009); write the id with three digits (8)"}},
		"work on 001, 002, 003, 004, 006, 007, 008 and 009": {Verdicts: acc(1, 2, 3, 4, 6, 7, 8, 9)},
	} {
		got := ParseReply(reply, ids, ranks)
		if !reflect.DeepEqual(got.Verdicts, want.Verdicts) || !reflect.DeepEqual(got.Unmatched, want.Unmatched) {
			t.Errorf("%q:\n got %+v\nwant %+v", reply, got, want)
		}
	}
	if r := ParseReply("all yes", ids, nil); len(r.Verdicts) != 0 || len(r.Unmatched) != 1 || !strings.Contains(r.Unmatched[0], "no pitch") {
		t.Fatalf("all without a pitch: %+v", r)
	}
}
