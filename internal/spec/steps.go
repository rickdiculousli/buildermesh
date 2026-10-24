package spec

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// NeedsSection is the heading a spec's steps for the operator live under.
const NeedsSection = "Needs from the operator"

// StepsKey is the frontmatter key bmesh land copied the steps into, before
// a landing carried trailers; an old spec still reads it.
const StepsKey = "operator_steps"

// Step is one step for the operator: its number in the list, the box it
// is for ("" when the step names none), its words, the command that tells
// it is done ("" when it has none) and, once ticked, `<time> <box>`; Note
// is the words a tick added.
type Step struct {
	N     int    `json:"n"`
	Box   string `json:"box"`
	Text  string `json:"text"`
	Check string `json:"check,omitempty"`
	Done  string `json:"done"`
	Note  string `json:"note,omitempty"`
}

var (
	itemRE  = regexp.MustCompile(`^([-*+]|\d{1,3}[.)])\s+(.*)$`)
	boxRE   = regexp.MustCompile(`^\[([a-z0-9][a-z0-9-]*)\](?:\s+|$)`)
	tickRE  = regexp.MustCompile(`^\[[ xX]\]\s+`)
	checkRE = regexp.MustCompile(`(?:^|\s)check:\s*`)
)

// ParseSteps reads a Needs section's body as a list: each top-level `-`,
// `*`, `+` or numbered line is a step, the lines under it (indented, or
// running on with no blank line between) part of it. A step may start with
// a box in brackets (`[ricks-macbook] ...`) and end with `check: <command>`
// (backticks around the command dropped). A paragraph of prose is a step of
// its own, except one ending in `:` that leads straight into a list, whose
// words go in front of each of that list's steps. A body that is empty, or
// whose first word is `nothing` or `none`, has no steps.
func ParseSteps(body string) []Step {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if f := strings.Fields(body); len(f) == 0 || isNone(f[0]) {
		return nil
	}
	type block struct {
		item  bool
		lines []string
	}
	var blocks []*block
	var cur *block
	blank := true
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			blank = true
			continue
		}
		indented := raw != strings.TrimLeft(raw, " \t")
		m := itemRE.FindStringSubmatch(line)
		switch {
		case m != nil && !indented:
			cur = &block{item: true, lines: []string{m[2]}}
			blocks = append(blocks, cur)
		case cur != nil && cur.item && (indented || !blank):
			cur.lines = append(cur.lines, line) // a step's own lines, a nested list's too
		case cur != nil && !cur.item && !blank:
			cur.lines = append(cur.lines, line)
		default:
			cur = &block{lines: []string{line}}
			blocks = append(blocks, cur)
		}
		blank = false
	}
	var steps []Step
	lead := ""
	for i, b := range blocks {
		text := strings.Join(b.lines, " ")
		if !b.item {
			if strings.HasSuffix(text, ":") && i+1 < len(blocks) && blocks[i+1].item {
				lead = text
				continue
			}
			lead = ""
		}
		s := parseStep(text, lead)
		s.N = len(steps) + 1
		steps = append(steps, s)
	}
	return steps
}

func isNone(word string) bool {
	w := strings.ToLower(strings.Trim(word, ".,:;!`*_\"'"))
	return w == "nothing" || w == "none"
}

// parseStep reads one step's words: a checkbox and a box prefix off the
// front, a `check:` off the end. A box on the lead-in counts for a step
// that names none.
func parseStep(text, lead string) Step {
	var s Step
	text = tickRE.ReplaceAllString(text, "")
	if m := boxRE.FindStringSubmatch(text); m != nil {
		s.Box, text = m[1], text[len(m[0]):]
	}
	if lead != "" {
		if m := boxRE.FindStringSubmatch(lead); m != nil {
			if s.Box == "" {
				s.Box = m[1]
			}
			lead = lead[len(m[0]):]
		}
		text = lead + " " + text
	}
	if loc := checkRE.FindStringIndex(text); loc != nil {
		cmd := strings.TrimSpace(text[loc[1]:])
		if strings.HasPrefix(cmd, "`") {
			if end := strings.LastIndex(cmd, "`"); end > 0 {
				cmd = cmd[1:end]
			}
		}
		if cmd = strings.TrimSpace(cmd); cmd != "" {
			s.Check = cmd
			text = strings.TrimRight(text[:loc[0]], " ;,—-")
		}
	}
	s.Text = strings.TrimSpace(text)
	return s
}

// OperatorSteps is the steps a landed spec has for the operator, with the
// ticks its file records. A spec landed before trailers has its
// operator_steps, ticks included, or none when it has no such field (it
// carries merged); another that reads done has its Needs section, a
// bracketed word kept as a box only when isBox says it is one
// (KeepBoxes); any other spec has none.
func (s *Spec) OperatorSteps(isBox func(string) bool) ([]Step, error) {
	_, recorded := s.Fields[StepsKey]
	switch {
	case recorded || s.Fields["merged"] != "":
		return DecodeSteps(s.Fields[StepsKey])
	case s.Status() == "done":
		return KeepBoxes(s.NeedsSteps(), isBox), nil
	}
	return nil, nil
}

// NeedsSteps is the spec's Needs section read as steps, never ticked.
func (s *Spec) NeedsSteps() []Step {
	body, _ := Section(s.Body, NeedsSection)
	return ParseSteps(body)
}

// Words is the step as its line says it, its box included: what a tick on
// the hub names, so the tick stays with its step when the list changes.
func (s Step) Words() string {
	if s.Box == "" {
		return s.Text
	}
	return "[" + s.Box + "] " + s.Text
}

// Ticked is steps with the hub's ticks laid on: each tick on the first
// step not yet ticked whose words it names; a tick no step names is
// dropped.
func Ticked(steps []Step, ticks []mesh.StepTick) []Step {
	for _, t := range ticks {
		for i := range steps {
			if steps[i].Done == "" && steps[i].Words() == t.Step {
				steps[i].Done, steps[i].Note = t.Done, t.Note
				break
			}
		}
	}
	return steps
}

// DecodeSteps reads an operator_steps value: a JSON list on one line.
func DecodeSteps(v string) ([]Step, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	var steps []Step
	if err := json.Unmarshal([]byte(v), &steps); err != nil {
		return nil, ledgerErr("%s: %v", StepsKey, err)
	}
	return steps, nil
}

// Open counts the steps not yet ticked.
func Open(steps []Step) int {
	n := 0
	for _, s := range steps {
		if s.Done == "" {
			n++
		}
	}
	return n
}

// String is a step as `bmesh spec steps` lists it: `<n>. [<box>] <text>`,
// then `(check: <command>)` and `done <when>[: <note>]` when it has them.
func (s Step) String() string {
	line := fmt.Sprintf("%d. ", s.N)
	if s.Box != "" {
		line += "[" + s.Box + "] "
	}
	line += s.Text
	if s.Check != "" {
		line += " (check: " + s.Check + ")"
	}
	if s.Done != "" {
		line += " -- done " + s.Done
		if s.Note != "" {
			line += ": " + s.Note
		}
	}
	return line
}

// KeepBoxes puts a bracketed word that names no box (isBox says) back in
// front of its step's words, so `[optional] ...` is words, not a box; a
// nil isBox keeps every box.
func KeepBoxes(steps []Step, isBox func(string) bool) []Step {
	if isBox == nil {
		return steps
	}
	for i := range steps {
		if b := steps[i].Box; b != "" && !isBox(b) {
			steps[i].Text = strings.TrimSpace("[" + b + "] " + steps[i].Text)
			steps[i].Box = ""
		}
	}
	return steps
}
