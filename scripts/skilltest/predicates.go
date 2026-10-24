package main

import (
	"encoding/csv"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/audit"
)

// Pred is one named expectation over a cell's end state; why says what it
// saw, pass or fail.
type Pred struct {
	Name  string
	Check func(s *State) (ok bool, why string)
}

// startCmds start a thread: the commands no quick change, question or held
// finding may run.
var startCmds = [][]string{{"build"}, {"ideate"}, {"lead", "start"}, {"executor", "new"}}

func landed() Pred {
	return Pred{"landed", func(s *State) (bool, string) {
		n := len(s.NewCommits())
		return n > 0, commitsOnOrigin(n)
	}}
}

func commitsOnOrigin(n int) string {
	if n == 1 {
		return "1 commit on origin's main"
	}
	return fmt.Sprintf("%d commits on origin's main", n)
}

func noCommit() Pred {
	return Pred{"no_commit", func(s *State) (bool, string) {
		n := len(s.NewCommits())
		return n == 0, commitsOnOrigin(n)
	}}
}

func nothingStarted() Pred {
	return Pred{"nothing_started", func(s *State) (bool, string) {
		var ran []string
		for _, c := range s.AgentCalls() {
			for _, w := range startCmds {
				if c.Is(w...) {
					ran = append(ran, c.String())
				}
			}
		}
		if len(ran) > 0 {
			return false, "ran " + strings.Join(ran, "; ")
		}
		return true, "no build, ideate, lead start or executor new"
	}}
}

func noSpecAdded() Pred {
	return Pred{"no_spec_added", func(s *State) (bool, string) {
		var specs []string
		for _, f := range s.Changed() {
			if strings.HasPrefix(f, "docs/proposals/") {
				specs = append(specs, f)
			}
		}
		if len(specs) > 0 {
			return false, "origin's main changed " + strings.Join(specs, ", ")
		}
		return true, "docs/proposals/ as it was"
	}}
}

// rungNamed is, for each turn, the rung its replies named (its first text
// naming one) against the rung that turn's own actions imply, through
// internal/audit's rules, as bmesh audit reads each request of a real
// session (101). It holds when some turn names a rung and every turn that
// names one names the one it took; a turn that only carries on names none.
// An arm whose manual has no bmesh audit is from before its skills named a
// rung, so it passes there.
func rungNamed() Pred {
	return Pred{"rung_named", func(s *State) (bool, string) {
		if !s.ArmHas("audit") {
			return true, "the arm's manual has no bmesh audit: its skills name no rung"
		}
		var seen []string
		for i, texts := range s.Texts {
			implied, rule := audit.Implied(s.TurnFacts(i))
			for _, t := range texts {
				if named, line, ok := audit.Named(t); ok {
					why := fmt.Sprintf("turn %d named %s (%q), implied %s (%s)", i+1, named, clip(line, 80), implied, rule)
					if named != implied {
						return false, why
					}
					seen = append(seen, why)
					break
				}
			}
		}
		if len(seen) == 0 {
			implied, rule := audit.Implied(s.TurnFacts(len(s.Texts) - 1))
			return false, fmt.Sprintf("named none; the last turn implied %s (%s)", implied, rule)
		}
		return true, strings.Join(seen, "; ")
	}}
}

// TurnFacts are what turn i did, as audit's rules read a request: the
// calls the fake logged during it (an OK build or lead start, an OK
// ideate), origin's main moving during it, a spec it added there, and
// whether its reply asks.
func (s *State) TurnFacts(i int) audit.Facts {
	var f audit.Facts
	if i < 0 || i >= len(s.Cell.Turns) {
		return f
	}
	from, origin := 0, s.Cell.Base
	if i > 0 {
		from, origin = s.Cell.Turns[i-1].After.Calls, s.Cell.Turns[i-1].After.Origin
	}
	after := s.Cell.Turns[i].After
	for _, c := range s.Calls[min(from, len(s.Calls)):min(after.Calls, len(s.Calls))] {
		if hookCall(c) {
			continue
		}
		f.Leads = f.Leads || c.OK() && (c.Is("build") || c.Is("lead", "start"))
		f.Round = f.Round || c.OK() && c.Is("ideate")
	}
	if after.Origin != "" && origin != "" && after.Origin != origin {
		f.PushedMain = true
		out, _ := s.git("diff", "--name-only", "--diff-filter=A", origin, after.Origin, "--", "docs/proposals/")
		f.NewSpec = len(strings.Fields(out)) > 0
	}
	if i < len(s.Replies) {
		f.EndsAsking = audit.EndsAsking(s.Replies[i])
	}
	return f
}

func testsPass() Pred { return Pred{"tests_pass", (*State).TestsPass} }

func hidden(name, pkg, test string) Pred {
	return Pred{name, func(s *State) (bool, string) { return s.Hidden(pkg, test) }}
}

func replyNonEmpty() Pred {
	return Pred{"reply", func(s *State) (bool, string) {
		n := len(strings.Fields(s.Reply()))
		return n > 0, fmt.Sprintf("the reply has %d words", n)
	}}
}

// heldUntil is no commit, on any ref of the sandbox or origin, after each
// turn before the nth.
func heldUntil(n int) Pred {
	return Pred{"held_until_go", func(s *State) (bool, string) {
		for i, t := range s.Cell.Turns {
			if i >= n-1 {
				break
			}
			if t.After.Commits > 0 {
				return false, fmt.Sprintf("%d commits after turn %d, before the go", t.After.Commits, i+1)
			}
		}
		if len(s.Cell.Turns) < n-1 {
			return false, fmt.Sprintf("only %d turns ran", len(s.Cell.Turns))
		}
		return true, fmt.Sprintf("no commit after turns 1 to %d", n-1)
	}}
}

// lastParagraph is the reply's last block of text: what it ends on.
func lastParagraph(reply string) string {
	blocks := regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(reply), -1)
	return strings.TrimSpace(blocks[len(blocks)-1])
}

var goAheadRE = regexp.MustCompile(`(?i)\b(want me to|shall i|should i|do you want|would you like|let me know|say (go|yes|the word|which)|your (go|go-ahead|ok|okay|call|pick|choice)\b|on your (ok|okay|go|word)|(ok|okay) to (proceed|go ahead|start|build|ship|release|land|push)|confirm|waiting (on|for) (you|your)|which (way|one|do you)|pick (one|a|which)|if you('d| would) like)`)

// asksOperator reports whether a reply ends by asking the operator
// something: its last paragraph has a question, or asks for a go-ahead or a
// choice.
func asksOperator(reply string) bool {
	p := lastParagraph(reply)
	return strings.Contains(p, "?") || goAheadRE.MatchString(p)
}

var (
	listItemRE     = regexp.MustCompile(`(?m)^\s*(\d+[.)]|\(\d+\)|[-*•])\s+\S`)
	numberedItemRE = regexp.MustCompile(`(?m)^\s*(\*\*)?(\d+[.)]|\(\d+\))\s*\S`)
)

func asksGoAhead() Pred {
	return Pred{"asks_go_ahead", func(s *State) (bool, string) {
		if asksOperator(s.Reply()) {
			return true, "the reply ends asking: " + clip(lastParagraph(s.Reply()), 120)
		}
		return false, "the reply ends on: " + clip(lastParagraph(s.Reply()), 120)
	}}
}

// planInReply is three or more list items in the reply that name what.
func planInReply(what string) Pred {
	return Pred{"plan_in_reply", func(s *State) (bool, string) {
		n := len(listItemRE.FindAllString(s.Reply(), -1))
		names := strings.Contains(strings.ToLower(s.Reply()), what)
		return n >= 3 && names, fmt.Sprintf("%d list items; names %s: %v", n, what, names)
	}}
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// The pick case: what the session's calls did to the pitch's specs.

// pitchIDs maps a pitch position to its spec id.
type pitchIDs []string

// ref is the spec a number in an operator's reply names: a position in the
// pitch for one or two digits within it, else an id.
func (p pitchIDs) ref(n string) string {
	v, _ := strconv.Atoi(n)
	if len(n) <= 2 && v >= 1 && v <= len(p) {
		return p[v-1]
	}
	return fmt.Sprintf("%03d", v)
}

var verdictWords = map[string]string{"yes": "accepted", "accept": "accepted", "accepted": "accepted", "ok": "accepted",
	"okay": "accepted", "build": "accepted", "go": "accepted", "approve": "accepted",
	"no": "rejected", "reject": "rejected", "rejected": "rejected", "later": "later"}

var numberRE = regexp.MustCompile(`^\d+$`)

func replyWords(clause string) []string {
	var out []string
	for _, w := range strings.Fields(strings.ToLower(clause)) {
		if w = strings.Trim(w, `.:!?()"'`); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// leadingRefs reads the numbers that open words ("1 and 3", "012, 014",
// "all"), skipping "and" and "to", and returns the specs and the rest.
func (p pitchIDs) leadingRefs(words []string) ([]string, []string) {
	var ids []string
	i := 0
	for ; i < len(words); i++ {
		w := words[i]
		switch {
		case numberRE.MatchString(w):
			ids = append(ids, p.ref(w))
		case w == "all":
			ids = append(ids, p...)
		case w == "and" || w == "&" || w == "to":
		default:
			return ids, words[i:]
		}
	}
	return ids, nil
}

// verdicts reads a choose-ideas call's moves, as the binary would: its
// flags (--accept 012,014, --reject 013=why, --later, --adjust NNN=...), or
// a reply in plain words, clause by clause.
func (p pitchIDs) verdicts(args []string) map[string]string {
	out := map[string]string{}
	pos := positionals(args)
	flagged := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			continue
		}
		name, val, inline := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		move := map[string]string{"accept": "accepted", "reject": "rejected", "later": "later", "adjust": "adjusting"}[name]
		if move == "" {
			continue
		}
		flagged = true
		if !inline && i+1 < len(args) {
			val = args[i+1]
			i++
		}
		ids, _, _ := strings.Cut(val, "=")
		for _, id := range strings.Split(ids, ",") {
			if id = strings.TrimSpace(id); numberRE.MatchString(id) {
				out[fmt.Sprintf("%03d", mustAtoi(id))] = move
			}
		}
	}
	if flagged || len(pos) < 2 {
		return out
	}
	reply := strings.Join(pos[1:], " ")
	var clauses [][]string
	for _, clause := range regexp.MustCompile(`[,;\n]`).Split(reply, -1) {
		clauses = append(clauses, splitSpoken(replyWords(clause))...)
	}
	var pending []string
	for _, words := range clauses {
		if len(words) == 0 {
			continue
		}
		if v, ok := verdictWords[words[0]]; ok {
			if ids, _ := p.leadingRefs(words[1:]); len(ids) > 0 {
				for _, id := range append(pending, ids...) {
					out[id] = v
				}
				pending = nil
				continue
			}
		}
		ids, rest := p.leadingRefs(words)
		if len(ids) == 0 {
			continue
		}
		if len(rest) == 0 {
			pending = append(pending, ids...)
			continue
		}
		v, ok := verdictWords[rest[0]]
		if !ok {
			if rest[0] == "on" && len(rest) == 2 {
				continue
			}
			v = "adjusting"
		}
		for _, id := range append(pending, ids...) {
			out[id] = v
		}
		pending = nil
	}
	return out
}

// splitSpoken splits a clause where, once its phrase has begun, a number is
// followed by a spoken verdict: "3 yes and 4 no" is two clauses, "4 no
// because 5 covers it" one.
func splitSpoken(words []string) [][]string {
	var out [][]string
	start, phrase := 0, false
	for i, w := range words {
		next := false
		if i+1 < len(words) {
			_, next = verdictWords[words[i+1]]
		}
		if phrase && numberRE.MatchString(w) && next {
			cut := i
			if cut > start && words[cut-1] == "and" {
				cut--
			}
			out = append(out, words[start:cut])
			start, phrase = i, false
			continue
		}
		if !numberRE.MatchString(w) && w != "and" && w != "&" && w != "to" && w != "all" {
			phrase = true
		}
	}
	return append(out, words[start:])
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// moves is what the session's calls did to the pitch's specs, in order:
// each spec's last status, and the specs a build started.
func (p pitchIDs) moves(s *State) (status map[string]string, built map[string]bool) {
	status, built = map[string]string{}, map[string]bool{}
	for _, c := range s.AgentCalls() {
		if !c.OK() {
			continue // refused: it moved and built nothing
		}
		cmd, rest := c.Cmd()
		switch {
		case cmd == "choose-ideas":
			for id, v := range p.verdicts(rest) {
				status[id] = v
			}
		case c.Is("spec", "set"):
			st := flagValue(rest, "status")
			for _, id := range strings.Split(flagValue(rest, "id"), ",") {
				if numberRE.MatchString(id) && st != "" {
					status[fmt.Sprintf("%03d", mustAtoi(id))] = st
				}
			}
		case cmd == "build" && c.Has("accepted"):
			for id, st := range status {
				if st == "accepted" {
					built[id] = true
				}
			}
		case cmd == "build":
			pos := positionals(rest)
			for _, id := range pos[min(1, len(pos)):] {
				if numberRE.MatchString(id) {
					built[fmt.Sprintf("%03d", mustAtoi(id))] = true
				}
			}
		}
	}
	return status, built
}

func flagValue(args []string, name string) string {
	for i, a := range args {
		n, v, inline := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !strings.HasPrefix(a, "-") || n != name {
			continue
		}
		if inline {
			return v
		}
		if i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func built(p pitchIDs, want ...string) Pred {
	return Pred{"built_" + strings.Join(want, "_"), func(s *State) (bool, string) {
		_, b := p.moves(s)
		for _, id := range want {
			if !b[id] {
				return false, fmt.Sprintf("builds started for [%s]", strings.Join(sortedKeys(b), " "))
			}
		}
		return true, fmt.Sprintf("builds started for [%s]", strings.Join(sortedKeys(b), " "))
	}}
}

func notBuilt(p pitchIDs, id string) Pred {
	return Pred{"not_built_" + id, func(s *State) (bool, string) {
		_, b := p.moves(s)
		return !b[id], fmt.Sprintf("builds started for [%s]", strings.Join(sortedKeys(b), " "))
	}}
}

var statusRE = regexp.MustCompile(`(?m)^status:\s*(\S+)`)

// declined is the spec rejected: by a choose-ideas reply or a spec set, or
// in its file on origin's main.
func declined(p pitchIDs, id string) Pred {
	return Pred{"declined_" + id, func(s *State) (bool, string) {
		st, _ := p.moves(s)
		if st[id] == "rejected" {
			return true, id + " rejected by the session's bmesh calls"
		}
		for _, f := range s.files("docs/proposals/" + id + "-") {
			if m := statusRE.FindStringSubmatch(s.Show(f)); m != nil && m[1] == "rejected" {
				return true, f + " reads rejected on origin's main"
			}
		}
		return false, fmt.Sprintf("%s is %q after the session's calls", id, st[id])
	}}
}

// files are origin's main's files under a prefix.
func (s *State) files(prefix string) []string { return s.filesAt(mainRef, prefix) }

// filesAt are the files under a prefix at a ref of origin.
func (s *State) filesAt(ref, prefix string) []string {
	out, err := s.git("ls-tree", "-r", "--name-only", ref)
	if err != nil {
		return nil
	}
	var fs []string
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, prefix) {
			fs = append(fs, f)
		}
	}
	return fs
}

// onlyTouches is every commit on origin's main since the base changing only
// files matching the glob.
func onlyTouches(name, glob string) Pred {
	return Pred{name, func(s *State) (bool, string) {
		for _, f := range s.Changed() {
			if ok, _ := path.Match(glob, f); !ok {
				return false, "origin's main changed " + f
			}
		}
		return true, commitsOnOrigin(len(s.NewCommits())) + ", none outside " + glob
	}}
}

// called is a call of the command words name, every one of whose args is
// among its positionals, and none of the excluded flags.
func called(name string, words []string, args []string, without ...string) Pred {
	return Pred{name, func(s *State) (bool, string) {
		for _, c := range s.AgentCalls() {
			if !c.OK() || !c.Is(words...) {
				continue
			}
			_, rest := c.Cmd()
			pos := strings.Join(positionals(rest), " ")
			ok := true
			for _, a := range args {
				ok = ok && regexp.MustCompile(`(^| )`+regexp.QuoteMeta(a)+`( |$)`).MatchString(pos)
			}
			for _, f := range without {
				ok = ok && !c.Has(f)
			}
			if ok {
				return true, "ran " + c.String()
			}
		}
		return false, fmt.Sprintf("no %s %s in the log", strings.Join(words, " "), strings.Join(args, " "))
	}}
}

// csvRuns are the ways the shop's report might be asked for CSV; the
// hidden check tries each until one prints or writes it.
var csvRuns = [][]string{
	{"report", "--csv"}, {"report", "-csv"}, {"report", "--format", "csv"}, {"report", "--format=csv"},
	{"report", "--format", "CSV"}, {"report", "--output", "csv"}, {"report", "-o", "csv"}, {"report", "--export", "csv"},
	{"report", "--export=csv"}, {"report", "csv"}, {"report", "--csv", "out.csv"}, {"report", "--csv=out.csv"},
	{"report", "--out", "out.csv"}, {"report", "-o", "out.csv"}, {"report", "--output", "out.csv"},
	{"report", "--export", "out.csv"}, {"export"}, {"export", "--csv"}, {"export", "csv"}, {"export", "--format", "csv"}, {"csv"},
}

var helpFlagRE = regexp.MustCompile(`^\s+-(\S+)(?:\s+(\S+))?`)

// helpCSVRuns are the ways the report's own help offers CSV: each flag of
// `shop report -h` whose name or text says csv, as a switch, or given csv
// or a file.
func helpCSVRuns(s *State) [][]string {
	r, err := s.Shop("report", "-h")
	if err != nil {
		return nil
	}
	lines := strings.Split(r.Stdout+r.Stderr, "\n")
	var out [][]string
	for i, l := range lines {
		m := helpFlagRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		text := l
		if i+1 < len(lines) {
			text += " " + lines[i+1]
		}
		if !strings.Contains(strings.ToLower(text), "csv") {
			continue
		}
		if m[2] == "" {
			out = append(out, []string{"report", "-" + m[1]})
		} else {
			out = append(out, []string{"report", "-" + m[1], "csv"}, []string{"report", "-" + m[1], "out.csv"})
		}
	}
	return out
}

// isCSV reports whether text is the seeded store as CSV: three records or
// more, every one with the same two fields or more.
func isCSV(text string) bool {
	recs, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil || len(recs) < 3 {
		return false
	}
	for _, r := range recs {
		if len(r) < 2 || len(r) != len(recs[0]) {
			return false
		}
	}
	return true
}

func csvExport() Pred {
	return Pred{"csv_export_works", func(s *State) (bool, string) {
		for _, args := range append(csvRuns, helpCSVRuns(s)...) {
			r, err := s.Shop(args...)
			if err != nil {
				return false, err.Error()
			}
			if r.Exit != 0 {
				continue
			}
			if isCSV(r.Stdout) {
				return true, "shop " + strings.Join(args, " ") + " prints the orders as CSV"
			}
			for name, text := range r.Files {
				if isCSV(text) {
					return true, "shop " + strings.Join(args, " ") + " writes the orders as CSV to " + name
				}
			}
		}
		return false, "no way of asking the report for CSV printed or wrote it"
	}}
}

// testAdded is a test file changed on origin's main whose added lines name
// what.
func testAdded(name, what string) Pred {
	return Pred{name, func(s *State) (bool, string) {
		for _, line := range strings.Split(s.Diff("*_test.go"), "\n") {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") && strings.Contains(strings.ToLower(line), what) {
				return true, "a test on origin's main names " + what
			}
		}
		return false, "no test line added on origin's main names " + what
	}}
}
