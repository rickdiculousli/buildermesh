package spec

import (
	"fmt"
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
// order. A number of three or more digits (012, as ids are written) is a
// spec id; one of one or two digits is a position in the pitch when there
// is one (so "1 and 3 yes" to a pitch of 012 to 016 means its first and
// third items), and a spec id when there is none. A short number that is
// both a pitched id and a different position (6 to a pitch of 001-003 and
// 006-010) is reported as ambiguous, not guessed. "all" names every pitched
// spec. A clause is its leading numbers (joined by "and" or the like) and
// the phrase after them, whose first word decides: yes/accept to accepted,
// no/reject to rejected with the rest as the reason, later to later, "on
// <judge>" alone to a judge note, and any other first word makes the whole
// phrase an instruction: the spec becomes adjusting with it as its note.
// Reasons and instructions are kept as written, so a verdict word or a
// number inside one is just a word. "on <judge>" right after yes or later
// sets the judge as well. A verdict may also come first ("accept 012",
// "reject 2 too big", "work on 1, 2 and 3", "all yes"): it then carries to
// the clauses of bare numbers after it. A clause of bare numbers otherwise
// takes the verdict of the clause right after it that has one ("1, 2 and 3
// yes"); a clause with no number in between ends both. A clause with no
// number, numbers no verdict reaches, or a number that matches nothing is
// reported, not guessed at.
func ParseReply(reply string, ids map[int]bool, ranks []int) Review {
	var r Review
	pitched := map[int]bool{}
	for _, id := range ranks {
		pitched[id] = true
	}
	apply := func(v Verdict, targets []int) {
		for _, id := range targets {
			v.ID = id
			r.Verdicts = append(r.Verdicts, v)
		}
	}
	var carry *Verdict   // a verdict said first, for the bare clauses after it
	var pending []int    // bare clauses' specs, waiting for the next verdict
	var waiting []string // those clauses, reported when no verdict comes
	for _, clause := range clauses(reply) {
		for _, p := range parts(clause) {
			if len(p.nums) == 0 {
				r.Unmatched = append(r.Unmatched, waiting...)
				r.Unmatched = append(r.Unmatched, p.text)
				pending, waiting, carry = nil, nil, nil
				continue
			}
			targets := resolve(&r, p, ids, ranks, pitched)
			if len(p.phrase) == 0 {
				switch {
				case carry != nil:
					apply(*carry, targets)
				case len(targets) > 0:
					pending = append(pending, targets...)
					waiting = append(waiting, p.text)
				}
				continue
			}
			v, ok := verdictOf(p.phrase)
			if !ok {
				r.Unmatched = append(r.Unmatched, waiting...)
				r.Unmatched = append(r.Unmatched, p.text)
				pending, waiting, carry = nil, nil, nil
				continue
			}
			apply(v, pending)
			pending, waiting = nil, nil
			apply(v, targets)
			carry = nil
			if p.lead {
				carry = &v
			}
		}
	}
	r.Unmatched = append(r.Unmatched, waiting...)
	return r
}

// part is one clause read: its numbers ("all" for every pitched spec) and
// its phrase, the verdict's words first when the verdict was said first.
type part struct {
	text   string
	nums   []string
	phrase []string
	lead   bool
}

// parts reads a clause. A verdict said first ("accept 012 and reject 013")
// ends its phrase where another verdict said first begins.
func parts(clause string) []part {
	tokens := strings.Fields(clause)
	i := 0
	for i < len(tokens) && fillers[word(tokens[i])] {
		i++
	}
	lead, n := leadAt(tokens, i)
	i += n
	var nums []string
	for ; i < len(tokens); i++ {
		if m := numberRE.FindStringSubmatch(tokens[i]); m != nil {
			nums = append(nums, m[1])
			continue
		}
		if word(tokens[i]) == "all" {
			nums = append(nums, "all")
			continue
		}
		if !fillers[word(tokens[i])] {
			break
		}
	}
	p := part{text: clause, nums: nums, phrase: tokens[i:]}
	if lead == nil {
		return []part{p}
	}
	p.lead = true
	for j := i; j < len(tokens); j++ {
		if _, n := leadAt(tokens, j); n > 0 && (j == i || fillers[word(tokens[j-1])]) {
			p.text = strings.Join(trimFillers(tokens[:j]), " ")
			p.phrase = append(lead, trimFillers(tokens[i:j])...)
			return append([]part{p}, parts(strings.Join(tokens[j:], " "))...)
		}
		if !fillers[word(tokens[j])] {
			break
		}
	}
	p.phrase = append(lead, tokens[i:]...)
	return []part{p}
}

// leadAt reports whether tokens[i:] opens with a verdict said before the
// numbers it is about ("accept 012", "reject 2", "work on 1", "yes to
// all"): the verdict as a phrase verdictOf reads, and how many tokens it
// takes. A number or "all" must follow it.
func leadAt(tokens []string, i int) ([]string, int) {
	if i >= len(tokens) {
		return nil, 0
	}
	var verdict []string
	n := 0
	switch w := word(tokens[i]); {
	case w == "work" && i+1 < len(tokens) && word(tokens[i+1]) == "on":
		verdict, n = []string{"yes"}, 2
	case yesWords[w] || noWords[w] || w == "later":
		verdict, n = []string{tokens[i]}, 1
	default:
		return nil, 0
	}
	if i+n < len(tokens) && word(tokens[i+n]) == "to" {
		n++
	}
	if i+n < len(tokens) && (isNumber(tokens[i+n]) || word(tokens[i+n]) == "all") {
		return verdict, n
	}
	return nil, 0
}

// resolve turns a part's numbers into spec ids, reporting the ones that
// name nothing or could name two.
func resolve(r *Review, p part, ids map[int]bool, ranks []int, pitched map[int]bool) []int {
	var targets []int
	var noSpec, noPosition, ambiguous []string
	for _, digits := range p.nums {
		if digits == "all" {
			if len(ranks) == 0 {
				r.Unmatched = append(r.Unmatched, "all: there is no pitch to answer; name the specs ("+p.text+")")
			}
			targets = append(targets, ranks...)
			continue
		}
		n, _ := strconv.Atoi(digits)
		switch {
		case len(digits) >= 3 || len(ranks) == 0:
			if ids[n] {
				targets = append(targets, n)
			} else {
				noSpec = append(noSpec, digits)
			}
		case pitched[n] && n <= len(ranks) && ranks[n-1] != n:
			// "6" to a pitch of 001-003 and 006-010: spec 006, or the
			// sixth item (009)? Neither is guessed.
			ambiguous = append(ambiguous, fmt.Sprintf("%s (spec %03d or item %s, %03d)", digits, n, digits, ranks[n-1]))
		case pitched[n]:
			targets = append(targets, n)
		case n >= 1 && n <= len(ranks):
			targets = append(targets, ranks[n-1])
		default:
			noPosition = append(noPosition, digits)
		}
	}
	if len(noSpec) > 0 {
		r.Unmatched = append(r.Unmatched, "no spec "+strings.Join(noSpec, ", ")+" ("+p.text+")")
	}
	if len(noPosition) > 0 {
		r.Unmatched = append(r.Unmatched, "no position "+strings.Join(noPosition, ", ")+" in the pitch; a spec id has three digits ("+p.text+")")
	}
	if len(ambiguous) > 0 {
		r.Unmatched = append(r.Unmatched, "ambiguous "+strings.Join(ambiguous, ", ")+"; write the id with three digits ("+p.text+")")
	}
	return targets
}

// clauses splits a reply at commas, semicolons and line breaks, and inside
// one of those, once its phrase has begun (a verdict said before its
// numbers has not begun it), at a number that a spoken verdict follows ("3 yes and 4 no" and "11 smaller and 12 no" are two
// clauses each; "4 no because 5 covers it" is one). A filler left dangling
// before the cut ("and") is dropped.
func clauses(reply string) []string {
	var out []string
	for _, clause := range clauseSep.Split(strings.TrimSpace(reply), -1) {
		tokens := strings.Fields(clause)
		start := 0
		inPhrase, numbered := false, false
		for i := 0; i < len(tokens); i++ {
			if !inPhrase && !numbered {
				// "accept 012 and 3 yes": a verdict said first is not a
				// phrase begun.
				if _, n := leadAt(tokens, i); n > 0 {
					i += n - 1
					continue
				}
			}
			if isNumber(tokens[i]) {
				if inPhrase && opensClause(tokens, i) {
					out = append(out, strings.Join(trimFillers(tokens[start:i]), " "))
					start, inPhrase = i, false
				}
				numbered = true
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

// A pitch item starts with the spec's three-digit id at a line start
// (lineItem: one item per line, the shape the skills ask for). A line that
// holds a whole list typed in one go is told by its items' shape, `NNN
// title (kind, size, judge):` after a sentence end (shapedItem: the title
// may hold brackets and a dot inside a word, v1.2, but no sentence end, a
// `.;!?` before a space; the three fields are anything but brackets); an
// id cited inside a why ("; 004 did it by hand") has no such shape.
var (
	lineItem   = regexp.MustCompile(`^\s*#?(\d{3})\b`)
	shapedItem = regexp.MustCompile(`(?:^|[.;!?]\s+)\s*#?(\d{3})\s(?:[^\n.;!?]|[.;!?][^\s\n])*?\(([^(),\n]+),\s*([^(),\n]+),\s*([^(),\n]+)\):`)
)

// PitchRanks reads the ranked list the operator saw, the pitch (the
// engineer's items, then the UI/UX engineer's, each `NNN title (kind,
// size, judge): why`), into the ids it names in order: every item whose
// number is a spec, a repeat kept once. Each line is read on its own: one
// holding two or more shaped items is a list typed on one line and gives
// them all; any other line gives the id it starts with, if any. The list
// never shrinks as specs are reviewed, so a position means the same spec
// in every reply to that pitch.
func PitchRanks(pitch string, specs []*Spec) []int {
	ids := IDSet(specs)
	var out []int
	seen := map[int]bool{}
	add := func(m []string) {
		n, _ := strconv.Atoi(m[1])
		if ids[n] && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, line := range strings.Split(pitch, "\n") {
		if shaped := shapedItem.FindAllStringSubmatch(line, -1); len(shaped) > 1 {
			for _, m := range shaped {
				add(m)
			}
			continue
		}
		if m := lineItem.FindStringSubmatch(line); m != nil {
			add(m)
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
