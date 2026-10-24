package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The feedback cases: bmesh's fix lead ruling on the issues naming its
// repo (fix), and the work skill draining a family's queue at its
// watch-done (drain). The fake answers feedback list from canned issues and
// leaves out what landed or was drained; scoring reads origin's main, the
// drains the session ran and its report.

// issue is one pending feedback issue as the fake lists it.
type issue struct {
	Seq                      int
	From, Repo, Spec, Family string
	Where, Text              string
}

// line is the issue as `feedback list <family>` prints it, or with family
// as `feedback list --repo R` does.
func (i issue) line(family bool) string {
	attrs := i.Repo + " " + i.Spec
	if family {
		attrs += ", family " + i.Family
	}
	if i.Where != "" {
		attrs += ", where " + i.Where
	}
	return fmt.Sprintf("%d %s (%s): %s\n", i.Seq, i.From, attrs, i.Text)
}

// feedbackCanned is the fake's queue for the issues: each family's list
// and each repo's, keyed as fake/bmesh reads them.
func feedbackCanned(issues ...issue) map[string]string {
	out := map[string]string{}
	for _, i := range issues {
		out["feedback-list."+i.Family] += i.line(false)
		out["feedback-list.repo."+i.Repo] += i.line(true)
	}
	return out
}

var (
	seqRE = regexp.MustCompile(`#(\d+)`)
	// fixSubjectRE is the binary's (internal/spec): a subject it reads as
	// fixing those issues, so one it would miss scores nothing here.
	fixSubjectRE = regexp.MustCompile(`^(?:Fix|Quick fix from (\S+)) (#\d+(?:, #\d+)*): `)
	fromFeedRE   = regexp.MustCompile(`(?m)^from_feedback:\s*(.*)$`)
)

func seqs(text string) []int {
	var out []int
	for _, m := range seqRE.FindAllStringSubmatch(text, -1) {
		out = append(out, mustAtoi(m[1]))
	}
	return out
}

// fixCommit is a commit on origin's main whose subject names issues: a
// fixer's Fix #a, #b: or a coordinator's Quick fix from <family> #a:.
type fixCommit struct {
	SHA, Subject, Family string // Family is a quick fix's, "" for a fixer's
	Seqs                 []int
}

func (s *State) fixCommits() []fixCommit {
	out, err := s.git("log", "--format=%H %s", s.Cell.Base+".."+mainRef)
	if err != nil {
		return nil
	}
	var fs []fixCommit
	for _, l := range strings.Split(out, "\n") {
		sha, subj, _ := strings.Cut(l, " ")
		if m := fixSubjectRE.FindStringSubmatch(subj); m != nil {
			fs = append(fs, fixCommit{sha, subj, m[1], seqs(m[2])})
		}
	}
	return fs
}

// newSpec is a spec added on origin's main since the base.
type newSpec struct {
	Path, Status string
	From         []int // the sequences its from_feedback names
}

func (s *State) newSpecs() []newSpec {
	out, err := s.git("diff", "--name-only", "--diff-filter=A", s.Cell.Base, mainRef, "--", "docs/proposals/")
	if err != nil {
		return nil
	}
	var specs []newSpec
	for _, f := range strings.Fields(out) {
		if !strings.HasSuffix(f, ".md") || filepath.Base(f) == "README.md" {
			continue
		}
		t := s.Show(f)
		n := newSpec{Path: f}
		if m := statusRE.FindStringSubmatch(t); m != nil {
			n.Status = m[1]
		}
		if m := fromFeedRE.FindStringSubmatch(t); m != nil {
			n.From = seqs(m[1])
		}
		specs = append(specs, n)
	}
	return specs
}

// cannedSeqs are the sequences of the cell's canned list for a family.
func (s *State) cannedSeqs(family string) []int {
	b, _ := os.ReadFile(filepath.Join(s.Dir, "canned", "feedback-list."+family))
	var out []int
	for _, l := range strings.Split(string(b), "\n") {
		if n, err := strconv.Atoi(strings.SplitN(l, " ", 2)[0]); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// drainedSeqs are the sequences the session's feedback drain calls named,
// sorted, once each: --seq's, or a family's canned issues up to --through.
func (s *State) drainedSeqs() []int {
	var out []int
	for _, c := range s.AgentCalls() {
		if !c.OK() || !c.Is("feedback", "drain") {
			continue
		}
		_, rest := c.Cmd()
		if v := flagValue(rest, "seq"); v != "" {
			for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '#' }) {
				if n, err := strconv.Atoi(f); err == nil {
					out = append(out, n)
				}
			}
			continue
		}
		pos := positionals(rest)
		if len(pos) < 2 {
			continue
		}
		through := flagValue(rest, "through")
		for _, n := range s.cannedSeqs(pos[1]) {
			if through == "" || n <= mustAtoi(through) {
				out = append(out, n)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func seqList(ns []int) string {
	var f []string
	for _, n := range ns {
		f = append(f, strconv.Itoa(n))
	}
	return "[" + strings.Join(f, " ") + "]"
}

// drainsExactly is the session's drains naming exactly the sequences.
func drainsExactly(want ...int) Pred {
	return Pred{"drains_exactly", func(s *State) (bool, string) {
		got := s.drainedSeqs()
		return slices.Equal(got, want), "drained " + seqList(got) + ", want " + seqList(want)
	}}
}

// namedOnMain is whether a fix commit or a new spec on origin's main names
// the sequence, and which.
func (s *State) namedOnMain(seq int) (bool, string) {
	for _, f := range s.fixCommits() {
		if slices.Contains(f.Seqs, seq) {
			return true, "the commit " + clip(f.Subject, 60) + " names it"
		}
	}
	for _, n := range s.newSpecs() {
		if slices.Contains(n.From, seq) {
			return true, n.Path + " names it in from_feedback"
		}
	}
	return false, "nothing on origin's main names it"
}

// specFrom is a new spec on origin's main whose from_feedback names seq,
// reading one of the statuses.
func specFrom(name string, seq int, statuses ...string) Pred {
	return Pred{name, func(s *State) (bool, string) {
		var seen []string
		for _, n := range s.newSpecs() {
			if slices.Contains(n.From, seq) {
				if slices.Contains(statuses, n.Status) {
					return true, fmt.Sprintf("%s reads %s, from_feedback #%d", n.Path, n.Status, seq)
				}
				seen = append(seen, n.Path+" reads "+n.Status)
			}
		}
		if len(seen) > 0 {
			return false, strings.Join(seen, "; ") + ", want " + strings.Join(statuses, " or ")
		}
		return false, fmt.Sprintf("no new spec on origin's main names #%d in from_feedback", seq)
	}}
}

func noAskOrBlocked() Pred {
	return Pred{"no_ask_or_blocked", func(s *State) (bool, string) {
		for _, c := range s.AgentCalls() {
			if c.Is("ask") || c.Is("blocked") {
				return false, "ran " + c.String()
			}
		}
		return true, "no ask and no blocked"
	}}
}

// headSHA is HEAD where a rehearsal's session works.
func headSHA(r *Rehearsal) string {
	if r.err != nil {
		return "00000000"
	}
	sha, err := git(r.sandbox(), gitEnv(r.Dir), "rev-parse", "HEAD")
	r.fail(err)
	return sha
}

// writeSpec writes a new spec at a rehearsal, with the frontmatter given.
func writeSpec(r *Rehearsal, file, front, title string) {
	r.Write(file, "---\n"+front+"---\n\n## Opportunity\n"+title+"\n\n## Proposal\n\n## What you'll see\n\n## Verification\n\n## Risks and the alternative considered\n\n## Needs from the operator\n")
}

// The fix case: bmesh's fix lead in the sandbox, which stands for bmesh's
// own repo.
const (
	fixNode    = "shop-fix-1"
	fixBug     = 41 // an XS bug with a where
	fixDup     = 42 // the same bug in other words, from another lead
	fixCovered = 43 // what spec 012 on main covers
	fixM       = 44 // an M that adds a command: no-new-surface proposes it
)

var fixIssues = []issue{
	{fixBug, "shop-013", repoName, "013", "shop-build-2", "cart/cart.go",
		"an empty cart with discount code SAVE5 totals -5.00; Total subtracts the discount from a subtotal of 0"},
	{fixDup, "shop-014", repoName, "014", "shop-build-3", "",
		"checkout with SAVE5 and nothing in the cart printed a negative total on the receipt"},
	{fixCovered, "shop-013", repoName, "013", "shop-build-2", "store/store.go",
		"two shop checkout runs at the same moment saved one order: the second rename dropped the first, and both got the same id"},
	{fixM, "shop-014", repoName, "014", "shop-build-3", "cmd/shop",
		"there is no way to refund an order: a refund is a hand edit of orders.json, and report still counts it; it wants a shop refund <id> command that the store and the report both know"},
}

const fixedTotal = "An empty cart with a code totals 0.00, not -5.00"

// fixTheTotal makes the fix of the bug at a rehearsal, committed as subject.
func fixTheTotal(r *Rehearsal, subject string) {
	r.Edit("cart/cart.go", totalBug, totalFixed)
	r.Write("cart/empty_test.go", "package cart\n\nimport \"testing\"\n\nfunc TestEmptyCartWithCode(t *testing.T) {\n\tif got := (Cart{Code: \"SAVE5\"}).Total(); got != 0 {\n\t\tt.Fatalf(\"total %d, want 0\", got)\n\t}\n}\n")
	r.Commit(subject + "\n\nruled: root, failing-first\nfailing-first: TestEmptyCartWithCode fails before (total -500) and passes after.")
}

const refundSpecFile = "docs/proposals/015-orders-can-be-refunded.md"

// proposeRefund lands the M issue as a spec at a rehearsal, reading status.
func proposeRefund(r *Rehearsal, status string) {
	front := "id: 015\ntitle: Orders can be refunded\nkind: feature\nsize: M\njudge: opus\nstatus: " + status + "\nproposed_by: " + fixNode + "\nproposed_on: 2026-10-24\n"
	if status == "accepted" {
		front += "ruled: [root, failing-first]\nfix_of: 014\n"
	}
	front += "from_feedback: shop-build-3 (shop-014) #44\n"
	r.Call("--node", fixNode, "spec", "next-id", "--repo", ".")
	writeSpec(r, refundSpecFile, front, "Proposed under no-new-surface: it adds a shop refund command.")
	r.Commit("Fix 015: " + status + " from shop-build-3 #44")
	r.Push()
}

func fixCase() *Case {
	fixed := hidden("bug_fixed", "cart", "TestSkilltestHiddenEmptyCartTotal")
	titled := Pred{fmt.Sprintf("fix_commit_%d", fixBug), func(s *State) (bool, string) {
		for _, f := range s.fixCommits() {
			if f.Family == "" && slices.Contains(f.Seqs, fixBug) {
				return true, "origin's main has " + clip(f.Subject, 80)
			}
		}
		return false, fmt.Sprintf("no Fix #%d: commit on origin's main", fixBug)
	}}
	dup := Pred{"duplicate_ruled_with_it", func(s *State) (bool, string) {
		var with *fixCommit
		for _, f := range s.fixCommits() {
			switch {
			case slices.Contains(f.Seqs, fixDup) && !slices.Contains(f.Seqs, fixBug):
				return false, fmt.Sprintf("#%d fixed on its own: %s", fixDup, clip(f.Subject, 80))
			case slices.Contains(f.Seqs, fixBug):
				with = &f
			}
		}
		if with != nil && slices.Contains(with.Seqs, fixDup) {
			return true, "one commit names both: " + clip(with.Subject, 80)
		}
		dupRE := regexp.MustCompile(fmt.Sprintf(`\b%d\b`, fixDup))
		bugRE := regexp.MustCompile(fmt.Sprintf(`\b%d\b`, fixBug))
		for _, l := range strings.Split(lastDoneText(s), "\n") {
			if !dupRE.MatchString(l) {
				continue
			}
			if bugRE.MatchString(l) || with != nil && strings.Contains(l, with.SHA[:7]) {
				return true, "the report's line for it names the fix: " + clip(l, 80)
			}
			return false, "the report's line for it names no fix: " + clip(l, 80)
		}
		return false, fmt.Sprintf("neither the fix nor the report rules on #%d with #%d", fixDup, fixBug)
	}}
	covered := Pred{"covered_dropped", func(s *State) (bool, string) {
		if ok, why := s.namedOnMain(fixCovered); ok {
			return false, fmt.Sprintf("#%d: %s", fixCovered, why)
		}
		if !slices.Contains(s.drainedSeqs(), fixCovered) {
			return false, fmt.Sprintf("#%d named on main by nothing, and never drained", fixCovered)
		}
		return true, fmt.Sprintf("#%d named on main by nothing, and drained", fixCovered)
	}}
	report := Pred{"report_names_all", func(s *State) (bool, string) {
		t := lastDoneText(s)
		if t == "" {
			return false, "no done with a --text"
		}
		var missing []string
		for _, i := range fixIssues {
			if !regexp.MustCompile(fmt.Sprintf(`\b%d\b`, i.Seq)).MatchString(t) {
				missing = append(missing, strconv.Itoa(i.Seq))
			}
		}
		if len(missing) > 0 {
			return false, "the report names no " + strings.Join(missing, ", ")
		}
		return true, "the report names all four"
	}}
	all := []int{fixBug, fixDup, fixCovered, fixM}
	start := func(r *Rehearsal) {
		r.Call("--node", fixNode, "started", "--text", "ruling on 4 issues")
		r.Call("--node", fixNode, "feedback", "list", "--repo", "shop")
	}
	drain := func(r *Rehearsal, seqs string) { r.Call("--node", fixNode, "feedback", "drain", "--seq", seqs) }
	done := func(r *Rehearsal, text string) {
		r.Call("--node", fixNode, "inbox")
		r.Call("--node", fixNode, "done", "--commit", clip(headSHA(r), 8), "--text", text)
		r.Reply(text)
	}
	const (
		dropped  = "43: dropped (drop: 012 covers it)"
		proposed = "44: proposed 015 (no-new-surface: adds a shop refund command)"
	)
	// ruled is the reference pass: one fix for both, the drop, the spec,
	// each drained as it lands.
	ruled := func(r *Rehearsal) {
		start(r)
		fixTheTotal(r, "Fix #41, #42: "+fixedTotal)
		r.Call("--node", fixNode, "routine", "run", "verify")
		r.Push()
		sha := clip(headSHA(r), 8)
		drain(r, "41,42")
		drain(r, "43")
		proposeRefund(r, "proposed")
		drain(r, "44")
		done(r, "Waits on you: 015 (a shop refund command) is proposed for your pick.\n41: fixed by "+sha+" (root, failing-first)\n42: fixed by "+sha+" (root, failing-first)\n"+dropped+"\n"+proposed)
	}
	return &Case{
		Name:   "fix",
		Lead:   &Lead{Node: fixNode, Brief: "fix"},
		Canned: func(e Env) map[string]string { return feedbackCanned(fixIssues...) },
		Expect: []Pred{fixed, testsPass(), titled, dup, covered, specFrom("m_specced", fixM, "proposed", "accepted"),
			drainsExactly(all...), report, noAskOrBlocked()},
		Refs: []Ref{
			{Name: "ruled on all four", Play: ruled},
			{Name: "the duplicate in the report, one drain", Play: func(r *Rehearsal) {
				start(r)
				fixTheTotal(r, "Fix #41: "+fixedTotal)
				r.Push()
				sha := clip(headSHA(r), 8)
				proposeRefund(r, "proposed")
				drain(r, "41,42,43,44")
				done(r, "Waits on you: 015 is proposed.\n41: fixed by "+sha+" (root, failing-first)\n42: fixed by "+sha+" (a duplicate)\n"+dropped+"\n"+proposed)
			}},
			{Name: "the M one accepted and built", Play: func(r *Rehearsal) {
				start(r)
				fixTheTotal(r, "Fix #41, #42: "+fixedTotal)
				r.Push()
				drain(r, "41,42")
				drain(r, "43")
				proposeRefund(r, "accepted")
				drain(r, "44")
				r.Call("--node", fixNode, "build", "shop", "015", "--family", fixNode)
				done(r, "Nothing waits on you.\n41: fixed by #41's commit (root, failing-first)\n42: with 41\n"+dropped+"\n44: fixed by 015 (root, failing-first)")
			}},
			{Name: "never fixed", Fails: "bug_fixed", Play: func(r *Rehearsal) {
				start(r)
				r.Edit("cart/receipt.go", `Money(c.Total()))`, `Money(max(0, c.Total())))`)
				r.Commit("Fix #41, #42: the receipt never shows a negative total")
				r.Push()
				drain(r, "41,42,43")
				proposeRefund(r, "proposed")
				drain(r, "44")
				done(r, "41, 42: fixed\n"+dropped+"\n"+proposed)
			}},
			{Name: "fixed under another title", Fails: titled.Name, Play: func(r *Rehearsal) {
				start(r)
				fixTheTotal(r, fixedTotal)
				r.Push()
				proposeRefund(r, "proposed")
				drain(r, "41,42,43,44")
				done(r, "41: fixed\n42: with 41\n"+dropped+"\n"+proposed)
			}},
			// Titles the binary doesn't read as fixing #41: it would list it again.
			{Name: "titled #41,#42 with no space", Fails: titled.Name, Play: func(r *Rehearsal) {
				start(r)
				fixTheTotal(r, "Fix #41,#42: "+fixedTotal)
				r.Push()
				proposeRefund(r, "proposed")
				drain(r, "41,42,43,44")
				done(r, "41: fixed\n42: with 41\n"+dropped+"\n"+proposed)
			}},
			{Name: "titled with no space after the colon", Fails: titled.Name, Play: func(r *Rehearsal) {
				start(r)
				fixTheTotal(r, "Fix #41, #42:"+fixedTotal)
				r.Push()
				proposeRefund(r, "proposed")
				drain(r, "41,42,43,44")
				done(r, "41: fixed\n42: with 41\n"+dropped+"\n"+proposed)
			}},
			{Name: "the duplicate fixed on its own", Fails: "duplicate_ruled_with_it", Play: func(r *Rehearsal) {
				start(r)
				fixTheTotal(r, "Fix #41: "+fixedTotal)
				r.Write("cart/receipt_test.go", "package cart\n\nimport (\n\t\"strings\"\n\t\"testing\"\n)\n\nfunc TestEmptyReceiptWithCode(t *testing.T) {\n\tif r := Receipt(Cart{Code: \"SAVE5\"}); strings.Contains(r, \"-5.00\\n\") && !strings.Contains(r, \"Discount\") {\n\t\tt.Fatal(r)\n\t}\n}\n")
				r.Commit("Fix #42: the empty receipt shows no negative total")
				r.Push()
				proposeRefund(r, "proposed")
				drain(r, "41,42,43,44")
				done(r, "41: fixed\n42: fixed\n"+dropped+"\n"+proposed)
			}},
			{Name: "the duplicate left", Fails: "duplicate_ruled_with_it", Play: func(r *Rehearsal) {
				start(r)
				fixTheTotal(r, "Fix #41: "+fixedTotal)
				r.Push()
				proposeRefund(r, "proposed")
				drain(r, "41,42,43,44")
				done(r, "41: fixed (root)\n42: dropped (drop: could not reproduce)\n"+dropped+"\n"+proposed)
			}},
			{Name: "the covered one specced", Fails: "covered_dropped", Play: func(r *Rehearsal) {
				start(r)
				fixTheTotal(r, "Fix #41, #42: "+fixedTotal)
				r.Push()
				proposeRefund(r, "proposed")
				writeSpec(r, "docs/proposals/016-checkouts-take-a-lock.md",
					"id: 016\ntitle: Checkouts take a lock\nsize: S\nstatus: proposed\nfrom_feedback: shop-build-2 (shop-013) #43\n", "Two checkouts at once save one order.")
				r.Commit("Fix 016: proposed from shop-build-2 #43")
				r.Push()
				drain(r, "41,42,43,44")
				done(r, "41: fixed\n42: with 41\n43: proposed 016\n"+proposed)
			}},
			{Name: "the M one never specced", Fails: "m_specced", Play: func(r *Rehearsal) {
				start(r)
				fixTheTotal(r, "Fix #41, #42: "+fixedTotal)
				r.Push()
				drain(r, "41,42,43,44")
				done(r, "41: fixed\n42: with 41\n"+dropped+"\n44: dropped (drop: product direction)")
			}},
			{Name: "a drain missing", Fails: "drains_exactly", Play: func(r *Rehearsal) {
				start(r)
				fixTheTotal(r, "Fix #41, #42: "+fixedTotal)
				r.Push()
				drain(r, "41,42,43")
				proposeRefund(r, "proposed")
				done(r, "41: fixed\n42: with 41\n"+dropped+"\n"+proposed)
			}},
			{Name: "a wrong drain", Fails: "drains_exactly", Play: func(r *Rehearsal) {
				start(r)
				fixTheTotal(r, "Fix #41, #42: "+fixedTotal)
				r.Push()
				proposeRefund(r, "proposed")
				drain(r, "41,42,43,44,45")
				done(r, "41: fixed\n42: with 41\n"+dropped+"\n"+proposed)
			}},
			{Name: "no report", Fails: "report_names_all", Play: func(r *Rehearsal) {
				start(r)
				fixTheTotal(r, "Fix #41, #42: "+fixedTotal)
				r.Push()
				proposeRefund(r, "proposed")
				drain(r, "41,42,43,44")
				r.Reply("All four ruled on.")
			}},
			{Name: "a report missing one", Fails: "report_names_all", Play: func(r *Rehearsal) {
				start(r)
				fixTheTotal(r, "Fix #41, #42: "+fixedTotal)
				r.Push()
				proposeRefund(r, "proposed")
				drain(r, "41,42,43,44")
				done(r, "41 and 42: fixed in one commit\n"+proposed)
			}},
			{Name: "asked", Fails: "no_ask_or_blocked", Play: func(r *Rehearsal) {
				r.Call("--node", fixNode, "ask", "--text", "is a refund command product direction?")
				ruled(r)
			}},
		},
	}
}

// lastDoneText is the --text of the session's last done that succeeded.
func lastDoneText(s *State) string {
	t := ""
	for _, c := range s.AgentCalls() {
		if c.OK() && c.Is("done") {
			_, rest := c.Cmd()
			t = flagValue(rest, "text")
		}
	}
	return t
}

// The drain case: the work skill, as the operator's coordinator, builds
// 013 and at its family's watch-done drains the queue its lead left.
const (
	drainFamily = "shop-build-1"
	drainXS     = 51 // the receipt's typo, one cause
	drainBmesh  = 52 // bmesh's own: its fixer's
	drainLarger = 53 // needs a design: a proposed spec
)

var drainIssues = []issue{
	{drainXS, "shop-013", repoName, "013", drainFamily, "cart/receipt.go",
		"the receipt's last line says Totl instead of Total"},
	{drainBmesh, "shop-013", "buildermesh", "013", drainFamily, "bmesh land",
		"bmesh land ran the whole verify again after a rebase that brought in no commits"},
	{drainLarger, "shop-013", repoName, "013", drainFamily, "store/store.go",
		"every order rewrites the whole orders.json, so checkout slows as the store grows; an append-only log or a file per day needs a design"},
}

func drainCase() *Case {
	quick := Pred{fmt.Sprintf("quick_fix_%d", drainXS), func(s *State) (bool, string) {
		for _, f := range s.fixCommits() {
			if f.Family == drainFamily && slices.Contains(f.Seqs, drainXS) {
				return true, "origin's main has " + clip(f.Subject, 80)
			}
		}
		return false, fmt.Sprintf("no Quick fix from %s #%d: commit on origin's main", drainFamily, drainXS)
	}}
	left := Pred{"bmesh_one_pending", func(s *State) (bool, string) {
		if slices.Contains(s.drainedSeqs(), drainBmesh) {
			return false, fmt.Sprintf("#%d drained", drainBmesh)
		}
		if ok, why := s.namedOnMain(drainBmesh); ok {
			return false, fmt.Sprintf("#%d: %s", drainBmesh, why)
		}
		return true, fmt.Sprintf("#%d never drained, and nothing on origin's main names it", drainBmesh)
	}}
	words := "build 013"
	begin := func(r *Rehearsal) {
		r.Call("build", "shop", "013")
		r.Call("watch", "--nodes", "shop-013", "--timeout", "30m")
		r.Call("feedback", "list", drainFamily)
	}
	quickFix := func(r *Rehearsal, subject string) {
		r.Edit("cart/receipt.go", `"Totl"`, `"Total"`)
		r.Commit(subject)
		r.Call("routine", "run", "verify")
		r.Push()
	}
	propose := func(r *Rehearsal, status string) {
		r.Call("spec", "next-id", "--repo", "shop")
		writeSpec(r, "docs/proposals/015-the-store-appends-each-order.md",
			"id: 015\ntitle: The store appends each order\nkind: refinement\nsize: M\njudge: opus\nstatus: "+status+"\nproposed_by: operator\nproposed_on: 2026-10-24\nfrom_feedback: "+drainFamily+" (shop-013) #53\n",
			"Every order rewrites the whole orders.json.")
		r.Commit("Feedback 015: " + status + " from " + drainFamily)
		r.Push()
	}
	const typoSubject = "Quick fix from shop-build-1 #51: the receipt says Total"
	return &Case{
		Name:  "drain",
		Turns: []string{words},
		Steer: goAhead,
		Canned: func(e Env) map[string]string {
			out := feedbackCanned(drainIssues...)
			// The lead lands 013 as the watch runs, so its done line names a
			// commit on origin's main that sets 013 done.
			out["watch-land"] = "013"
			out["watch"] = e.Ago(0) + " shop-013 started building 013\n" + e.Ago(0) + " shop-013 done 013 landed at @LANDED@\nwatch-done; next: bmesh feedback list " + drainFamily + "\n"
			return out
		},
		Expect: []Pred{quick, hidden("typo_fixed", "cart", "TestSkilltestHiddenReceiptSaysTotal"), testsPass(), left,
			specFrom("larger_proposed", drainLarger, "proposed"), drainsExactly(drainXS, drainLarger), rungNamed()},
		Refs: []Ref{
			{Name: "drained", Play: func(r *Rehearsal) {
				r.Say("Starting leads for 013")
				begin(r)
				quickFix(r, typoSubject)
				propose(r, "proposed")
				r.Call("feedback", "drain", "--seq", "51,53")
				r.Reply("013 landed. Its lead's feedback: 51 fixed (the receipt says Total), 53 proposed as 015 for your pick, 52 is bmesh's and waits for its fixer.")
			}},
			{Name: "drained one at a time", Play: func(r *Rehearsal) {
				begin(r)
				quickFix(r, typoSubject)
				r.Call("feedback", "drain", "--seq", "51")
				propose(r, "proposed")
				r.Call("feedback", "drain", "--seq", "53")
				r.Reply("Starting leads for 013: it landed; 51 fixed, 53 proposed as 015, 52 to the fixer.")
			}},
			{Name: "never fixed", Fails: "typo_fixed", Play: func(r *Rehearsal) {
				begin(r)
				r.Write("cart/receipt_note.go", "package cart\n\n// The receipt's total line is labelled Total.\n")
				r.Commit(typoSubject)
				r.Push()
				propose(r, "proposed")
				r.Call("feedback", "drain", "--seq", "51,53")
				r.Reply("Done.")
			}},
			{Name: "titled with no space after the colon", Fails: quick.Name, Play: func(r *Rehearsal) {
				begin(r)
				quickFix(r, "Quick fix from shop-build-1 #51:the receipt says Total")
				propose(r, "proposed")
				r.Call("feedback", "drain", "--seq", "51,53")
				r.Reply("Done.")
			}},
			{Name: "fixed under another title", Fails: quick.Name, Play: func(r *Rehearsal) {
				begin(r)
				quickFix(r, "The receipt says Total")
				propose(r, "proposed")
				r.Call("feedback", "drain", "--seq", "51,53")
				r.Reply("Done.")
			}},
			{Name: "the bmesh one drained", Fails: left.Name, Play: func(r *Rehearsal) {
				begin(r)
				quickFix(r, typoSubject)
				propose(r, "proposed")
				r.Call("feedback", "drain", "--seq", "51,52,53")
				r.Reply("Done.")
			}},
			{Name: "the family drained whole", Fails: left.Name, Play: func(r *Rehearsal) {
				begin(r)
				quickFix(r, typoSubject)
				propose(r, "proposed")
				r.Call("feedback", "drain", drainFamily)
				r.Reply("Done.")
			}},
			{Name: "the bmesh one specced here", Fails: left.Name, Play: func(r *Rehearsal) {
				begin(r)
				quickFix(r, typoSubject)
				propose(r, "proposed")
				writeSpec(r, "docs/proposals/016-land-skips-a-needless-verify.md",
					"id: 016\ntitle: land skips a needless verify\nstatus: proposed\nfrom_feedback: "+drainFamily+" (shop-013) #52\n", "bmesh land verifies twice.")
				r.Commit("Feedback 016: proposed from " + drainFamily)
				r.Push()
				r.Call("feedback", "drain", "--seq", "51,53")
				r.Reply("Done.")
			}},
			{Name: "the larger one dropped", Fails: "larger_proposed", Play: func(r *Rehearsal) {
				begin(r)
				quickFix(r, typoSubject)
				r.Call("feedback", "drain", "--seq", "51,53")
				r.Reply("Done.")
			}},
			{Name: "the larger one accepted", Fails: "larger_proposed", Play: func(r *Rehearsal) {
				begin(r)
				quickFix(r, typoSubject)
				propose(r, "accepted")
				r.Call("feedback", "drain", "--seq", "51,53")
				r.Reply("Done.")
			}},
			{Name: "never drained", Fails: "drains_exactly", Play: func(r *Rehearsal) {
				begin(r)
				quickFix(r, typoSubject)
				propose(r, "proposed")
				r.Reply("Done.")
			}},
		},
	}
}
