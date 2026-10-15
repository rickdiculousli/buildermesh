package spec

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The reply grammar: the operator's review of a round, typed in plain
// words ("1 and 3 yes, 2 smaller, 4 no, 5 later, 3 on opus"). A reply is
// clauses separated by commas, semicolons or line breaks; a clause names
// specs by number and says what happens to them.

// Verdict is one clause's effect on one spec.
type Verdict struct {
	ID    int
	Move  string // accepted, rejected, later, adjusting, or "" for a judge note alone
	Note  string // rejected: the reason; adjusting: the instruction
	Judge string // set by "on fable|opus|sonnet"
}

// Review is a parsed reply.
type Review struct {
	Verdicts  []Verdict
	Unmatched []string // clauses that named no spec, or no verdict
}

var (
	clauseSep = regexp.MustCompile(`\s*[,;\n]+\s*`)
	numberRE  = regexp.MustCompile(`^#?(\d+)[.:)]?$`)
	yesWords  = map[string]bool{"yes": true, "accept": true, "accepted": true, "ok": true, "go": true, "build": true, "approve": true, "approved": true}
	noWords   = map[string]bool{"no": true, "reject": true, "rejected": true, "nope": true}
	fillers   = map[string]bool{"and": true, "&": true, "+": true, "please": true, "spec": true, "specs": true}
)

// ParseReply reads reply against the ledger. ids are the spec ids that
// exist; ranks are the ids of the specs the operator is reviewing, in pitch
// order, so a number that is no spec id but a position in that list names
// the spec at it (ids win over ranks). A clause's leading numbers (joined
// by "and" or the like) name its specs; what follows is its verdict: yes/accept to accepted, no/reject to rejected (the clause's other
// words as the reason), later to later, "on <judge>" sets the judge (alone
// it moves nothing), and any other words are an instruction, so the spec
// becomes adjusting with them as its note. A clause with no number, no
// verdict, or numbers that match nothing is reported, not guessed at.
func ParseReply(reply string, ids map[int]bool, ranks []int) Review {
	var r Review
	for _, clause := range clauses(reply) {
		// The numbers lead the clause ("1 and 3 yes"); once a verdict or
		// instruction word is read, a later number is part of the words
		// ("13 split into 14 parts" names only 013).
		var nums []int
		var words []string
		for _, w := range strings.Fields(clause) {
			if m := numberRE.FindStringSubmatch(w); m != nil && len(words) == 0 {
				n, _ := strconv.Atoi(m[1])
				nums = append(nums, n)
				continue
			}
			if len(words) == 0 && fillers[strings.ToLower(w)] {
				continue
			}
			words = append(words, w)
		}
		if len(nums) == 0 {
			r.Unmatched = append(r.Unmatched, clause)
			continue
		}
		var targets []int
		var missing []string
		for _, n := range nums {
			switch {
			case ids[n]:
				targets = append(targets, n)
			case n >= 1 && n <= len(ranks):
				targets = append(targets, ranks[n-1])
			default:
				missing = append(missing, strconv.Itoa(n))
			}
		}
		if len(missing) > 0 {
			r.Unmatched = append(r.Unmatched, "no spec "+strings.Join(missing, ", ")+" ("+clause+")")
		}
		if len(targets) == 0 {
			continue
		}
		v, ok := verdictOf(words)
		if !ok {
			r.Unmatched = append(r.Unmatched, clause)
			continue
		}
		for _, id := range targets {
			v.ID = id
			r.Verdicts = append(r.Verdicts, v)
		}
	}
	return r
}

// clauses splits a reply at commas, semicolons and line breaks, and inside
// one of those at a number that follows a spoken verdict ("3 yes and 4 no"
// is two clauses; "13 split into 14 parts" is one, since "split into" is an
// instruction, not a verdict). A filler left dangling before the cut
// ("and") is dropped.
func clauses(reply string) []string {
	var out []string
	for _, clause := range clauseSep.Split(strings.TrimSpace(reply), -1) {
		var cur []string
		leading, spoken := true, false
		flush := func() {
			for len(cur) > 0 && fillers[strings.ToLower(cur[len(cur)-1])] {
				cur = cur[:len(cur)-1]
			}
			if len(cur) > 0 {
				out = append(out, strings.Join(cur, " "))
			}
			cur, leading, spoken = nil, true, false
		}
		for _, w := range strings.Fields(clause) {
			isNum := numberRE.MatchString(w)
			if isNum && !leading && spoken {
				flush()
			}
			if !isNum && !fillers[strings.ToLower(w)] {
				leading = false
				if spokenVerdict(w) {
					spoken = true
				}
			}
			cur = append(cur, w)
		}
		flush()
	}
	return out
}

// spokenVerdict reports whether w is a verdict word (yes, no, later, a
// judge name) rather than part of an instruction.
func spokenVerdict(w string) bool {
	w = strings.ToLower(strings.Trim(w, ".!:\"'()"))
	if yesWords[w] || noWords[w] || w == "later" {
		return true
	}
	_, judge := Judges[w]
	return judge
}

// verdictOf reads a clause's words (its numbers removed). Words that are
// not verdicts stay as written: they are the note the next thread works
// from.
func verdictOf(words []string) (Verdict, bool) {
	var v Verdict
	var rest []string
	for i := 0; i < len(words); i++ {
		w := strings.ToLower(strings.Trim(words[i], ".!:\"'()"))
		switch {
		case w == "":
		case w == "on" && i+1 < len(words):
			j := strings.ToLower(strings.Trim(words[i+1], ".!:\"'()"))
			if _, ok := Judges[j]; ok {
				v.Judge = j
				i++
				continue
			}
			rest = append(rest, words[i])
		case yesWords[w] && v.Move == "":
			v.Move = "accepted"
		case noWords[w] && v.Move == "":
			v.Move = "rejected"
		case w == "later" && v.Move == "":
			v.Move = "later"
		default:
			rest = append(rest, words[i])
		}
	}
	note := strings.TrimSpace(strings.Join(rest, " "))
	switch v.Move {
	case "rejected":
		v.Note = note
	case "accepted", "later":
		if note != "" {
			// "yes but smaller": an instruction rides on the verdict.
			v.Move, v.Note = "adjusting", note
		}
	case "":
		if note != "" {
			v.Move, v.Note = "adjusting", note
		} else if v.Judge == "" {
			return Verdict{}, false
		}
	}
	return v, true
}

// Reviewable are the statuses a review may move a spec from.
var Reviewable = []string{"proposed", "later", "adjusting"}

// IsReviewable reports whether a spec in this status may be reviewed.
func IsReviewable(status string) bool {
	for _, s := range Reviewable {
		if s == status {
			return true
		}
	}
	return false
}

// Ranks are the ids the operator sees as a ranked list: every reviewable
// spec, lowest id first (a round numbers its specs in rank order).
func Ranks(specs []*Spec) []int {
	var out []int
	for _, s := range specs {
		if IsReviewable(s.Status()) {
			out = append(out, s.ID)
		}
	}
	sort.Ints(out)
	return out
}

// IDSet is the set of ids among specs.
func IDSet(specs []*Spec) map[int]bool {
	out := map[int]bool{}
	for _, s := range specs {
		out[s.ID] = true
	}
	return out
}

// AdjustNote is the line a review adds under the Proposal heading of an
// adjusting spec: the instruction, dated, for the thread that reworks the
// body. The body is returned with the line in place (appended when the
// heading is missing).
func AdjustNote(body, date, note string) string {
	line := "Adjust (" + date + "): " + note + "\n"
	lines := strings.SplitAfter(body, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "## Proposal" {
			out := append([]string{}, lines[:i+1]...)
			out = append(out, line)
			out = append(out, lines[i+1:]...)
			return strings.Join(out, "")
		}
	}
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return body + "\n" + line
}
