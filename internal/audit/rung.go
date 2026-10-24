// Package audit reads an agent session's transcript and says what it read
// before it acted, the rung of the work skill's ladder each reply named and
// the rung its actions imply (101). The rules are few and fixed, and
// printed with each verdict: a mismatch shows both sides, never a pass or
// a fail. The skill test scores its cases through the same rules.
package audit

import (
	"regexp"
	"strings"
)

// Rung is a step of the work skill's ladder, as a reply names it or as a
// request's actions imply it.
type Rung string

const (
	Answer Rung = "answer" // answered, nothing changed
	Here   Rung = "here"   // made in this session and pushed to main
	Plan   Rung = "plan"   // a plan, waiting on the operator's ok
	Leads  Rung = "leads"  // leads started
	Round  Rung = "round"  // a proposal round started
)

// Words are the openings a reply names each rung with, the work skill's
// words: `Answering, no change`, `Making it here, S: <evidence>`, `Plan for
// your ok, L: <evidence>`, `Starting leads for 012 and 014`, `Starting a
// round on <repo>`.
var Words = []struct {
	Rung Rung
	Says string
}{
	{Answer, "Answering"},
	{Here, "Making it here"},
	{Plan, "Plan for your ok"},
	{Leads, "Starting leads"},
	{Round, "Starting a round"},
}

// holdLine is the QA hold's first line, which the rung line follows.
const holdLine = "holding for your qa"

// lineMarks are what a line may open with before its words: a heading, a
// quote, a list marker, bold or italics.
var lineMarks = regexp.MustCompile(`^(#+\s*|>\s*|[-*]\s+|\*\*|__|\*|_)+`)

// Named is the rung a reply names: its first line, or the line after the QA
// hold's, opening with a rung's words. ok is false when it names none.
func Named(reply string) (r Rung, line string, ok bool) {
	n := 0
	for _, l := range strings.Split(reply, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		bare := strings.ToLower(lineMarks.ReplaceAllString(l, ""))
		for _, w := range Words {
			if strings.HasPrefix(bare, strings.ToLower(w.Says)) {
				return w.Rung, l, true
			}
		}
		if n++; n > 1 || !strings.HasPrefix(bare, holdLine) {
			return "", "", false
		}
	}
	return "", "", false
}

// Facts are what a request's actions did, as the rules read them.
type Facts struct {
	Leads      bool // a bmesh build or lead start ran
	Round      bool // a bmesh ideate ran
	PushedMain bool // a push to main succeeded
	NewSpec    bool // a new file under docs/proposals/ went with it
	EndsAsking bool // the request's last reply ends on a question
}

// Rules are the implied-rung rules, first match wins, as audit prints
// them.
const Rules = "build or lead start: leads; ideate: round; a push to main with no new file under docs/proposals/: here; a last reply ending on a question with nothing pushed: plan; none of these: answer"

// Implied is the rung f implies, with the rule that decided it.
func Implied(f Facts) (Rung, string) {
	switch {
	case f.Leads:
		return Leads, "a build or lead start ran"
	case f.Round:
		return Round, "an ideate ran"
	case f.PushedMain && !f.NewSpec:
		return Here, "pushed to main, no new spec"
	case f.EndsAsking && !f.PushedMain:
		return Plan, "the last reply ends on a question, nothing pushed"
	case f.PushedMain:
		return Answer, "pushed only with a new spec, nothing started"
	}
	return Answer, "nothing pushed or started, the last reply asks nothing"
}

// EndsAsking reports whether a reply ends on a question: its last paragraph
// has a question mark.
func EndsAsking(reply string) bool {
	paras := regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(reply), -1)
	return strings.Contains(paras[len(paras)-1], "?")
}
