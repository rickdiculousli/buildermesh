package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// NeedsSection is the heading a spec's steps for the operator live under.
const NeedsSection = "Needs from the operator"

// StepsKey is the frontmatter key bmesh land copies the steps into.
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

// Steps is the spec's operator_steps as recorded; nil when it has none.
func (s *Spec) Steps() ([]Step, error) {
	return DecodeSteps(s.Fields[StepsKey])
}

// NeedsSteps is the spec's Needs section read as steps, never ticked.
func (s *Spec) NeedsSteps() []Step {
	body, _ := Section(s.Body, NeedsSection)
	return ParseSteps(body)
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

// EncodeSteps is steps as an operator_steps value: JSON on one line, with
// `<`, `>` and `&` kept as written.
func EncodeSteps(steps []Step) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(steps); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
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

// CarryTicks is steps with the ticks of the same steps (same box and
// words) in old kept, so a spec landed again keeps what was done.
func CarryTicks(old, steps []Step) []Step {
	for i := range steps {
		for _, o := range old {
			if o.Done != "" && o.Box == steps[i].Box && o.Text == steps[i].Text {
				steps[i].Done, steps[i].Note = o.Done, o.Note
				break
			}
		}
	}
	return steps
}
