package spec

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// definitionRE is a bold run that is a bmesh synopsis and nothing else:
// **`bmesh release <repo> [--minor]`**, optionally closed by punctuation
// inside the bold (**`bmesh sync`:**). A bold run that goes on in words
// (**`bmesh build` repeats the warning**) talks about a command, and one
// with a backtick right outside it is quoted inside a code span.
var definitionRE = regexp.MustCompile("\\*\\*`bmesh ([^`]+)`[.:,;]?\\*\\*")

var commandWordRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// CommandName is the command a synopsis (the text after `bmesh `) names:
// its leading bare words, a subcommand's with it (`spec verified --commit
// C` is `spec verified`). flag is true when the name goes straight to a
// `--` flag, as a synopsis that adds a flag to a command does. ok is false
// when the synopsis opens with no word.
func CommandName(synopsis string) (name string, flag, ok bool) {
	f := strings.Fields(synopsis)
	n := 0
	for n < len(f) && commandWordRE.MatchString(f[n]) {
		n++
	}
	if n == 0 {
		return "", false, false
	}
	return strings.Join(f[:n], " "), n < len(f) && strings.HasPrefix(f[n], "--"), true
}

// Commands is the bmesh commands s defines: in its Proposal, each bold run
// that is a synopsis alone (definitionRE), named by CommandName, once each
// in order. A synopsis that goes straight to a flag defines the flag, not
// the command, and flags aren't checked.
func (s *Spec) Commands() []string {
	body, _ := Section(s.Body, "Proposal")
	var out []string
	for _, m := range definitionRE.FindAllStringSubmatchIndex(body, -1) {
		if m[0] > 0 && body[m[0]-1] == '`' || m[1] < len(body) && body[m[1]] == '`' {
			continue
		}
		name, flag, ok := CommandName(body[m[2]:m[3]])
		if ok && !flag && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// claimsCommands is the statuses whose specs' commands are taken: written
// and not yet landed.
var claimsCommands = []string{"accepted", "adjusting", "building"}

// CommandClashes is a warning per command s defines that main already has
// (have) or that another spec of others, accepted, adjusting or building,
// also defines: `NNN defines bmesh <name>, which MMM (<status>) also
// defines` or `..., which main already has`.
func CommandClashes(s *Spec, others []*Spec, have []string) []string {
	var out []string
	for _, name := range s.Commands() {
		if slices.Contains(have, name) {
			out = append(out, fmt.Sprintf("%s defines bmesh %s, which main already has", s.IDString(), name))
		}
		for _, o := range others {
			if o.ID == s.ID || !slices.Contains(claimsCommands, o.Status()) || !slices.Contains(o.Commands(), name) {
				continue
			}
			out = append(out, fmt.Sprintf("%s defines bmesh %s, which %s (%s) also defines", s.IDString(), name, o.IDString(), o.Status()))
		}
	}
	return out
}

// UsageCommands is the command names a `bmesh help` text lists: each line
// under `commands:` indented by two spaces, its synopsis cut at the
// description's column, named by CommandName; a first word of
// alternatives (`started|progress`) names each.
func UsageCommands(usage string) []string {
	var out []string
	in := false
	for _, l := range strings.Split(usage, "\n") {
		if l == "commands:" {
			in = true
			continue
		}
		if !in || !strings.HasPrefix(l, "  ") || strings.HasPrefix(l, "   ") {
			continue
		}
		syn, _, _ := strings.Cut(strings.TrimSpace(l), "  ")
		first, rest, _ := strings.Cut(syn, " ")
		for _, w := range strings.Split(first, "|") {
			if name, _, ok := CommandName(w + " " + rest); ok && !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
	}
	return out
}
