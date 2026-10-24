package main

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/instance"
)

// The review case: a session run as the review routine runs one, on a
// commit of the sandbox with planted defects, scored on which of them its
// reply names. testdata/review holds the base laid over the fixture, the
// change laid over that, and the defect sheet the scoring reads.

//go:embed testdata/review
var reviewFS embed.FS

const (
	reviewDir    = "testdata/review"
	reviewSheet  = reviewDir + "/defects.md"
	reviewBranch = "review"
	reviewJob    = "skilltest-review"
	reviewMsg    = "report --daily and --from; checkout prints the receipt once the order is saved; a discount is a negative amount"
	// reviewEffort is the effort landing queues a review at for an M change.
	reviewEffort = "medium"
)

// Routine makes a cell a routine's model run rather than a plugin's
// session: the kind's allow, as the routine's claude is given it, and the
// job's effort.
type Routine struct {
	Kind   string   `json:"kind"`
	Allow  []string `json:"allow"`
	Effort string   `json:"effort,omitempty"`
}

// Defect is one planted defect, a row of the sheet.
type Defect struct {
	ID, Class string
	Files     []string // the paths its finding names one of (the base name will do)
	Names     []string // functions, flags or code, one of which it names as a word
	Lines     []lineRange
	Cues      []string // one of which it says, any case
}

type lineRange struct {
	File     string
	From, To int
}

// Sheet is the defect sheet: the planted defects and the real change's
// names, a finding about which that names no defect is a false alarm.
type Sheet struct {
	Defects []Defect
	Clean   []string
}

var sheet = mustSheet()

func mustSheet() Sheet {
	b, err := reviewFS.ReadFile(reviewSheet)
	if err != nil {
		panic(err)
	}
	s, err := parseSheet(string(b))
	if err != nil {
		panic(reviewSheet + ": " + err.Error())
	}
	return s
}

var backtickRE = regexp.MustCompile("`([^`]+)`")

// parseSheet reads the sheet's table and its "Real change:" line.
func parseSheet(text string) (Sheet, error) {
	var s Sheet
	split := func(cell string) []string {
		var out []string
		for _, f := range strings.Split(cell, ";") {
			if f = strings.TrimSpace(f); f != "" {
				out = append(out, f)
			}
		}
		return out
	}
	for _, line := range strings.Split(text, "\n") {
		if rest, ok := strings.CutPrefix(line, "Real change:"); ok {
			first, _, _ := strings.Cut(rest, ".")
			for _, m := range backtickRE.FindAllStringSubmatch(first, -1) {
				s.Clean = append(s.Clean, m[1])
			}
			continue
		}
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| defect ") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "| "), " | ")
		if len(cells) != 7 {
			return s, fmt.Errorf("a row with %d cells, want 7: %s", len(cells), line)
		}
		d := Defect{ID: cells[0], Class: cells[1], Files: split(cells[2]), Names: split(cells[3]), Cues: split(cells[5])}
		for _, r := range split(cells[4]) {
			file, span, ok := strings.Cut(r, ":")
			from, to, _ := strings.Cut(span, "-")
			a, errA := strconv.Atoi(from)
			z, errZ := strconv.Atoi(to)
			if !ok || errA != nil || errZ != nil || z < a {
				return s, fmt.Errorf("%s: lines %q, want path:from-to", d.ID, r)
			}
			d.Lines = append(d.Lines, lineRange{file, a, z})
		}
		if len(d.Files) == 0 || len(d.Names) == 0 || len(d.Cues) == 0 {
			return s, fmt.Errorf("%s: a defect needs files, names and cues", d.ID)
		}
		s.Defects = append(s.Defects, d)
	}
	if len(s.Defects) == 0 || len(s.Clean) == 0 {
		return s, fmt.Errorf("no defects or no real change")
	}
	return s, nil
}

// overlay writes the files under testdata/review/<part> over the sandbox.
func overlay(sandbox, part string) error {
	root := reviewDir + "/" + part
	return fs.WalkDir(reviewFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := reviewFS.ReadFile(p)
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(sandbox, filepath.FromSlash(strings.TrimPrefix(p, root+"/"))), string(b))
	})
}

// commitChange lays the change over the sandbox's base and commits it on
// its own branch, which the sandbox is left on, and returns the job's text
// as the routine would send it for that commit.
func commitChange(sandbox string, genv []string) (string, error) {
	if err := overlay(sandbox, "change"); err != nil {
		return "", err
	}
	for _, s := range [][]string{{"checkout", "-q", "-b", reviewBranch}, {"add", "-A"}, {"commit", "-q", "--no-verify", "-m", reviewMsg}} {
		if _, err := git(sandbox, genv, s...); err != nil {
			return "", err
		}
	}
	commit, err := git(sandbox, genv, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	tree, err := git(sandbox, genv, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", err
	}
	base, err := git(sandbox, genv, "rev-parse", "HEAD^")
	if err != nil {
		return "", err
	}
	return instance.JobText(instance.Job{ID: reviewJob, Commit: commit, Tree: tree, Text: "base: " + base}), nil
}

// routineRun is a routine case's run on arm a, and its one turn's words:
// the arm's kind prompt and the job text, in one message as the routine's
// first job sends them, for the change committed on the sandbox.
func routineRun(k *Case, a Arm, sandbox string, genv []string) (*Routine, string, error) {
	kd := a.Review
	if k.Routine.Kind != "review" || kd == nil {
		return nil, "", fmt.Errorf("arm %s has no %s kind (its examples/routines/%s.toml)", a.Name, k.Routine.Kind, k.Routine.Kind)
	}
	job, err := commitChange(sandbox, genv)
	if err != nil {
		return nil, "", err
	}
	rt := *k.Routine
	rt.Allow = append([]string{}, kd.Allow...)
	return &rt, kd.Prompt + "\n\n" + job, nil
}

// routineArgs is the claude command line for a routine's model run, as the
// routine builds it (its first job: the kind's prompt and the job text in
// one message, the kind's allow as its tools), with the stream for the
// harness to read and no settings of this box's.
func (r *runner) routineArgs(words string, rt *Routine) []string {
	args := []string{"-p", words, "--model", r.model, "--output-format", "stream-json", "--verbose"}
	if rt.Effort != "" {
		args = append(args, "--effort", rt.Effort)
	}
	return append(append(args, "--setting-sources", "project,local"), instance.SessionFlags(rt.Allow)...)
}

// Scoring a review's reply.

var (
	headingRE  = regexp.MustCompile(`^#{1,6}\s`)
	numberedRE = regexp.MustCompile(`^\s*(\*\*)?\d+[.)]\s`)
	trailerRE  = regexp.MustCompile(`(?i)^[\s*_>#` + "`" + `]*(not checked|verdict)\s*:`)
	niceRE     = regexp.MustCompile(`(?i)nice to have|\bnit\b`)
)

// findings splits a reply into its findings: the sections under its
// headings, else its numbered items, else the whole of it, with the
// "Not checked:" and "VERDICT:" lines left out.
func findings(reply string) []string {
	var lines []string
	for _, l := range strings.Split(reply, "\n") {
		if !trailerRE.MatchString(l) {
			lines = append(lines, l)
		}
	}
	for _, re := range []*regexp.Regexp{headingRE, numberedRE} {
		var out []string
		cur := -1
		for _, l := range lines {
			if re.MatchString(l) {
				out = append(out, "")
				cur++
			}
			if cur >= 0 {
				out[cur] += l + "\n"
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if strings.TrimSpace(strings.Join(lines, "\n")) == "" {
		return nil
	}
	return []string{strings.Join(lines, "\n")}
}

var lineRefRE = regexp.MustCompile(`(?i)(?:([A-Za-z_][\w./-]*\.go):|\blines?\s+)(\d+)(?:\s*[-–]\s*(\d+))?`)

func word(text, w string) bool {
	return regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(w) + `($|[^A-Za-z0-9_])`).MatchString(text)
}

// namesFile reports whether a finding names the path or its base name.
func namesFile(text, p string) bool {
	return strings.Contains(text, p) || strings.Contains(text, path.Base(p))
}

// names reports whether one finding names the defect: one of its files,
// one of its names or a line in one of its ranges, and one of its cues.
func (d Defect) names(text string) bool {
	file := false
	for _, f := range d.Files {
		file = file || namesFile(text, f)
	}
	if !file {
		return false
	}
	where := false
	for _, n := range d.Names {
		where = where || word(text, n)
	}
	for _, m := range lineRefRE.FindAllStringSubmatch(text, -1) {
		a, _ := strconv.Atoi(m[2])
		z := a
		if m[3] != "" {
			z, _ = strconv.Atoi(m[3])
		}
		for _, r := range d.Lines {
			if m[1] != "" && path.Base(m[1]) != path.Base(r.File) || m[1] == "" && !namesFile(text, r.File) {
				continue
			}
			where = where || a <= r.To && z >= r.From
		}
	}
	if !where {
		return false
	}
	low := strings.ToLower(text)
	for _, c := range d.Cues {
		if strings.Contains(low, strings.ToLower(c)) {
			return true
		}
	}
	return false
}

func caught(d Defect) Pred {
	return Pred{plantedPrefix + d.ID, func(s *State) (bool, string) {
		for i, f := range findings(s.Reply()) {
			if d.names(f) {
				return true, fmt.Sprintf("finding %d names %s (%s)", i+1, d.ID, d.Class)
			}
		}
		return false, fmt.Sprintf("no finding names %s (%s) in %s", d.ID, d.Class, strings.Join(d.Files, " or "))
	}}
}

// verdictFail is the reply failing the change as the routine reads it
// (instance.Verdict): a VERDICT line whose value isn't ok.
func verdictFail() Pred {
	return Pred{"verdict_fail", func(s *State) (bool, string) {
		v, found := instance.Verdict(s.Reply())
		if !found {
			return false, "the reply has no VERDICT line"
		}
		return !strings.EqualFold(v, "ok"), "VERDICT: " + v
	}}
}

// noFalseAlarm is no finding, short of a nice-to-have, about the real
// change that names no planted defect.
func noFalseAlarm(sh Sheet) Pred {
	return Pred{"no_false_alarm", func(s *State) (bool, string) {
		var alarms []string
		for i, f := range findings(s.Reply()) {
			first, _, _ := strings.Cut(strings.TrimSpace(f), "\n")
			if niceRE.MatchString(first) {
				continue
			}
			clean := ""
			for _, n := range sh.Clean {
				if word(f, n) {
					clean = n
					break
				}
			}
			if clean == "" {
				continue
			}
			named := false
			for _, d := range sh.Defects {
				named = named || d.names(f)
			}
			if !named {
				alarms = append(alarms, fmt.Sprintf("finding %d (%s): %s", i+1, clean, clip(first, 80)))
			}
		}
		if len(alarms) > 0 {
			return false, "about the real change, naming no defect: " + strings.Join(alarms, "; ")
		}
		return true, "no finding about the real change but the defects"
	}}
}

func reviewCase() *Case {
	k := &Case{
		Name:    "review",
		Routine: &Routine{Kind: "review", Effort: reviewEffort},
		Setup:   func(e Env) error { return overlay(e.Sandbox, "base") },
	}
	for _, d := range sheet.Defects {
		k.Expect = append(k.Expect, caught(d))
	}
	k.Expect = append(k.Expect, verdictFail(), noFalseAlarm(sheet))
	k.Refs = []Ref{
		{Name: "named every defect", Play: func(r *Rehearsal) { r.Reply(reviewGoodReply) }},
		{Name: "named every defect, failed with a note", Play: func(r *Rehearsal) {
			r.Reply(strings.Replace(reviewGoodReply, "VERDICT: fail", "**VERDICT: fail (four findings)**", 1))
		}},
		{Name: "named every defect, as numbered items", Play: func(r *Rehearsal) {
			r.Reply(regexp.MustCompile(`(?m)^### (\d)\. `).ReplaceAllString(reviewGoodReply, "$1. "))
		}},
		{Name: "named every defect, and a nice-to-have on the real change", Play: func(r *Rehearsal) {
			r.Reply(strings.Replace(reviewGoodReply, "Not checked:", "### 5. Nice to have: WriteDays could name its columns\n\nreport/report.go, `WriteDays`: a header line.\n\nNot checked:", 1))
		}},
		{Name: "named none", Fails: plantedPrefix + "off-by-one", Play: func(r *Rehearsal) {
			r.Reply("### 1. Should fix: the receipt says Totl\n\ncart/receipt.go:19 spells the total's label `Totl`.\n\nVERDICT: fail")
		}},
		{Name: "vague", Fails: plantedPrefix + "quadratic", Play: func(r *Rehearsal) {
			r.Reply("The change looks reasonable, though there may be edge cases in the new report code and in checkout.\n\nVERDICT: fail")
		}},
		{Name: "named the places, never what breaks", Fails: plantedPrefix + "off-by-one", Play: func(r *Rehearsal) {
			r.Reply("### 1. report/report.go `From`\n\nRead it.\n\n### 2. report/report.go `Days`\n\nRead it.\n\n### 3. cmd/shop/main.go `checkout`\n\nRead it.\n\n### 4. cart/receipt.go `Receipt`\n\nRead it.\n\nVERDICT: fail")
		}},
		{Name: "every defect, passed anyway", Fails: "verdict_fail", Play: func(r *Rehearsal) {
			r.Reply(strings.Replace(reviewGoodReply, "VERDICT: fail", "VERDICT: ok", 1))
		}},
		{Name: "a false alarm on the real change", Fails: "no_false_alarm", Play: func(r *Rehearsal) {
			r.Reply(strings.Replace(reviewGoodReply, "Not checked:", "### 5. Must fix: report --daily drops the summary\n\ncmd/shop/main.go, `reportCmd`: with `--daily` the totals are gone.\n\nNot checked:", 1))
		}},
	}
	return k
}

// reviewGoodReply is a review that names every planted defect.
const reviewGoodReply = "### 1. Must fix: --from leaves out the order it names\n\nreport/report.go:53-58, `From`. Order ids start at 1 (`store.Add` sets `len(orders) + 1`), so `orders[id:]` skips the order with that id, and `--from 3` with three orders reports nothing. Fix: `orders[id-1:]`, and `id > len(orders)`.\n\n" +
	"### 2. Must fix: the receipt never shows the discount again\n\ncart/cart.go:72, `Discount` now returns -500 for SAVE5, but `Receipt` in cart/receipt.go:16 prints the discount line only when `d > 0`: with SAVE5 the line is gone. Fix: `d < 0`, printing `Money(d)`.\n\n" +
	"### 3. Must fix: checkout saves a negative total\n\ncmd/shop/main.go, `checkout`: moving the receipt after the save dropped the guard on `c.Total() < 0`. An empty cart with SAVE5 totals -5.00 (`Total` doesn't floor it), and is now saved. Fix: put the guard back before `st.Add`.\n\n" +
	"### 4. Should fix: report --daily is quadratic\n\nreport/report.go, `Days`: for each order it scans every order again, O(n²) in the orders, and the store keeps every order forever. Fix: one pass with a map from date to its Day.\n\n" +
	"Not checked: nothing outside the repo.\n\nVERDICT: fail"
