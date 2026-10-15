package spec

import (
	"regexp"
	"strconv"
	"strings"
)

// The reply grammar: the operator's review of a round, typed in plain
// words ("1 and 3 yes, 2 smaller, 4 no, 5 later, 3 on opus"). A reply is
// clauses; a clause is the numbers of the specs it is about, then a verdict
// phrase whose first word decides what happens to them.

// Verdict is one clause's effect on one spec.
type Verdict struct {
	ID    int
	Move  string // accepted, rejected, later, adjusting, or "" for a judge note alone
	Note  string // rejected: the reason; adjusting: the instruction; both as written
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

// word is a token lowercased with punctuation trimmed, for matching.
func word(w string) string { return strings.ToLower(strings.Trim(w, ".!:\"'()")) }

func isNumber(w string) bool { return numberRE.MatchString(w) }

func isJudge(w string) bool {
	_, ok := Judges[word(w)]
	return ok
}

// spokenAt reports whether tokens[i] opens a spoken verdict: yes, no,
// later, or "on <judge>".
func spokenAt(tokens []string, i int) bool {
	w := word(tokens[i])
	if yesWords[w] || noWords[w] || w == "later" {
		return true
	}
	return w == "on" && i+1 < len(tokens) && isJudge(tokens[i+1])
}

// ParseReply reads reply against the ledger. ids are the spec ids that
// exist; ranks are the ids of the pitch the operator is answering, in its
// order. A number names the pitched spec with that id when there is one,
// else the pitch line at that position, else the spec with that id (one
// outside the pitch), so "1 and 3 yes" to a pitch of 012 to 016 means its
// first and third lines. A clause is its leading numbers
// (joined by "and" or the like) and the phrase after them, whose first
// word decides: yes/accept to accepted, no/reject to rejected with the rest
// as the reason, later to later, "on <judge>" alone to a judge note, and
// any other first word makes the whole phrase an instruction: the spec
// becomes adjusting with it as its note. Reasons and instructions are kept
// as written, so a verdict word or a number inside one is just a word.
// "on <judge>" right after yes or later sets the judge as well. A clause
// with no number, no phrase, or a number that matches nothing is reported,
// not guessed at.
func ParseReply(reply string, ids map[int]bool, ranks []int) Review {
	var r Review
	pitched := map[int]bool{}
	for _, id := range ranks {
		pitched[id] = true
	}
	for _, clause := range clauses(reply) {
		tokens := strings.Fields(clause)
		var nums []int
		i := 0
		for ; i < len(tokens); i++ {
			if m := numberRE.FindStringSubmatch(tokens[i]); m != nil {
				n, _ := strconv.Atoi(m[1])
				nums = append(nums, n)
				continue
			}
			if !fillers[word(tokens[i])] {
				break
			}
		}
		if len(nums) == 0 {
			r.Unmatched = append(r.Unmatched, clause)
			continue
		}
		var targets []int
		var missing []string
		for _, n := range nums {
			switch {
			case pitched[n]:
				targets = append(targets, n)
			case n >= 1 && n <= len(ranks):
				targets = append(targets, ranks[n-1])
			case ids[n]:
				targets = append(targets, n)
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
		v, ok := verdictOf(tokens[i:])
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
// one of those, once its phrase has begun, at a number that a spoken
// verdict follows ("3 yes and 4 no" and "11 smaller and 12 no" are two
// clauses each; "4 no because 5 covers it" is one). A filler left dangling
// before the cut ("and") is dropped.
func clauses(reply string) []string {
	var out []string
	for _, clause := range clauseSep.Split(strings.TrimSpace(reply), -1) {
		tokens := strings.Fields(clause)
		start := 0
		inPhrase := false
		for i := 0; i < len(tokens); i++ {
			if isNumber(tokens[i]) {
				if inPhrase && opensClause(tokens, i) {
					out = append(out, strings.Join(trimFillers(tokens[start:i]), " "))
					start, inPhrase = i, false
				}
				continue
			}
			if !fillers[word(tokens[i])] {
				inPhrase = true
			}
		}
		if rest := trimFillers(tokens[start:]); len(rest) > 0 {
			out = append(out, strings.Join(rest, " "))
		}
	}
	return out
}

// opensClause reports whether the numbers at tokens[i:] are followed by a
// spoken verdict, so they open a clause of their own.
func opensClause(tokens []string, i int) bool {
	for ; i < len(tokens); i++ {
		if isNumber(tokens[i]) || fillers[word(tokens[i])] {
			continue
		}
		return spokenAt(tokens, i)
	}
	return false
}

func trimFillers(tokens []string) []string {
	for len(tokens) > 0 && fillers[word(tokens[len(tokens)-1])] {
		tokens = tokens[:len(tokens)-1]
	}
	return tokens
}

// verdictOf reads a clause's phrase (the tokens after its numbers).
func verdictOf(tokens []string) (Verdict, bool) {
	tokens = trimFillers(tokens)
	if len(tokens) == 0 {
		return Verdict{}, false
	}
	var v Verdict
	first := word(tokens[0])
	rest := tokens[1:]
	switch {
	case yesWords[first]:
		v.Move = "accepted"
	case noWords[first]:
		v.Move = "rejected"
	case first == "later":
		v.Move = "later"
	case first == "on" && len(tokens) >= 2 && isJudge(tokens[1]):
		v.Judge = word(tokens[1])
		rest = tokens[2:]
	default:
		v.Move = "adjusting"
		v.Note = strings.Join(tokens, " ")
		return v, true
	}
	if v.Move == "rejected" {
		v.Note = strings.Join(rest, " ")
		return v, true
	}
	// "yes on opus", "later on sonnet": the judge rides on the verdict.
	if len(rest) >= 2 && word(rest[0]) == "on" && isJudge(rest[1]) {
		v.Judge = word(rest[1])
		rest = rest[2:]
	}
	// "yes but smaller", "on opus but shorter": an instruction rides on it.
	if note := strings.Join(trimFillers(rest), " "); note != "" {
		v.Move, v.Note = "adjusting", note
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

// pitchItem is where a pitch item starts: at a line start or after a
// sentence end, with the spec's three-digit id ("001 Title (kind, size,
// judge): why. 002 ..." on one line reads the same as one per line; "200
// lines" inside a why does not start one).
var pitchItem = regexp.MustCompile(`(?m)(?:^|[.;!?]\s+)\s*#?(\d{3})\b`)

// PitchRanks reads the ranked list the operator saw, the pitch (the
// engineer's items, then the UI/UX engineer's, each `NNN title ...`), into
// the ids it names in order: every item whose number is a spec, a repeat
// kept once. The list never shrinks as specs are reviewed, so a position
// means the same spec in every reply to that pitch.
func PitchRanks(pitch string, specs []*Spec) []int {
	ids := IDSet(specs)
	var out []int
	seen := map[int]bool{}
	for _, m := range pitchItem.FindAllStringSubmatch(pitch, -1) {
		n, _ := strconv.Atoi(m[1])
		if ids[n] && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
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
