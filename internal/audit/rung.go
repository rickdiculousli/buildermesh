// Package audit reads an agent session's transcript and says, per request,
// what it read before it acted, what its replies said, and the rung of the
// work skill's ladder its actions imply (101). The rules are few and fixed,
// and printed with what they found.
package audit

import (
	"regexp"
	"strings"
)

// Rung is a step of the work skill's ladder, as a request's actions imply
// it.
type Rung string

const (
	Answer Rung = "answer" // answered, nothing changed
	Here   Rung = "here"   // made in this session and pushed to main
	Plan   Rung = "plan"   // a plan, waiting on the operator's ok
	Leads  Rung = "leads"  // leads started
	Round  Rung = "round"  // a proposal round started
)

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
