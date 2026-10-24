package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/brief"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

// Case is one scripted ask: the operator's words, turn by turn, what the
// fake answers beyond its defaults, and the expectations a cell's end state
// is scored on. When the turns are done, the expectations don't hold yet
// and the last reply asks the operator something, the harness answers with
// Steer, at most maxSteers times; a case whose asks are expected, or that
// no go-ahead could mend, has no Steer.
type Case struct {
	Name   string
	Turns  []string
	Steer  string
	Setup  func(e Env) error             // edits to the fixture before its commit
	Canned func(e Env) map[string]string // the fake's outputs, by key (see fake/bmesh)
	Lead   *Lead                         // a lead's case: its words are the build brief
	Round  *Round                        // a round thread's case: its words are the round brief
	// A routine's case: its one turn is the arm's kind prompt and the job,
	// run as the routine runs it, not as a session of the plugin.
	Routine *Routine
	Expect  []Pred
	Refs    []Ref
}

// Lead makes a case a lead's: the session is lead Node building spec Spec,
// in a worktree of the sandbox on its own branch, and its one turn is its
// brief: the build brief from the arm's own template, with Words as the
// operator's words, or with Brief "fix" the fix lead's from this tree's
// internal/brief (Spec empty). With StopFirst the fake's first ready or
// land refuses because the spec doesn't read done.
type Lead struct {
	Node, Spec string
	Brief      string // "" is the build brief; "fix" the fix lead's
	Words      string
	StopFirst  bool
}

// worktree is the lead's worktree, relative to the cell, and branch its
// branch, as bmesh lead start names them.
func (l *Lead) worktree() string { return filepath.Join("worktrees", repoName, l.Node) }
func (l *Lead) branch() string   { return "bmesh/" + l.Node }

// brief is the lead's brief in the sandbox, as bmesh build (or bmesh fix)
// writes it for a repo with no setup command.
func (l *Lead) brief(a Arm, sandbox, host string, now time.Time) (string, error) {
	if l.Brief == "fix" {
		values := brief.RowValues(row(sandbox, host), sandbox)
		values["node"], values["date"], values["plain_words"] = l.Node, now.UTC().Format("2006-01-02"), brief.PlainWords
		text, err := brief.Render("", "fix", values)
		if err != nil {
			return "", err
		}
		return brief.WithSetup(text, brief.SetupStep("", "")), nil
	}
	specs, err := spec.Scan(sandbox, "docs/proposals/{id}-*.md")
	if err != nil {
		return "", err
	}
	id, _ := strconv.Atoi(l.Spec)
	s, ok := spec.Find(specs, id)
	if !ok {
		return "", fmt.Errorf("no spec %s in the sandbox", l.Spec)
	}
	values := brief.BuildValues(row(sandbox, host), sandbox, s.Text, s.Path, s.IDString(), l.Node, now.UTC().Format("2006-01-02"))
	values["operator_words"] = brief.OperatorWords(l.Words)
	tmpl, _, err := armTemplate(a, "build")
	if err != nil {
		return "", err
	}
	text, err := brief.Fill(tmpl, values)
	if err != nil {
		return "", fmt.Errorf("the arm's build template %w", err)
	}
	return brief.WithSetup(text, brief.SetupStep("", "")), nil
}

// Round makes a case a round thread's: the session is round 1's eng
// thread in its worktree of the sandbox on its own branch, as bmesh ideate
// makes one, and its one turn is the round brief rendered from the arm's
// own engineer template, numbered from NextID.
type Round struct {
	NextID int
}

const (
	roundPath   = "shop-round-1"
	roundNode   = roundPath + ".eng"
	roundThread = "5d0c7a1e-0000-4000-8000-000000000097"
)

func (rd *Round) worktree() string { return filepath.Join("worktrees", repoName, rd.branch()) }
func (rd *Round) branch() string   { return roundPath + "-eng" }

// legacyRound are the round values an engineer template from before 097
// was written for (it has no {{title}}): the engineer, three to five
// ideas, beside a UI/UX engineer with a block of its own.
var legacyRound = map[string]string{
	"round_ask":     "Find what would most improve this repo and bring back three to five ranked ideas, each a half-page spec or a pitch line (see What you pitch), that the operator can pick or decline in a sentence.",
	"round_partner": "The UI/UX engineer runs beside you on the same round and lands on the same `main`.",
	"ux_ids":        " The UI/UX engineer has a block of its own, so a gap is expected.",
}

// legacy reports whether the arm's engineer template is from before 097,
// when every round had a UI/UX thread beside the engineer.
func legacy(text string) bool { return !strings.Contains(text, "{{title}}") }

// template is the arm's engineer template.
func (rd *Round) template(a Arm) (string, error) {
	text, _, err := armTemplate(a, "engineer")
	return text, err
}

// armTemplate is the arm's own brief template name, so a brief change is
// measured as a skill change is: its file, or this tree's for an arm with
// no directory (a rehearsal's) or none of that name (own is false then).
func armTemplate(a Arm, name string) (text string, own bool, err error) {
	if a.Dir != "" {
		b, err := os.ReadFile(filepath.Join(a.Dir, "internal", "brief", "templates", name+".md"))
		if err == nil {
			return string(b), true, nil
		}
		if !os.IsNotExist(err) {
			return "", false, err
		}
	}
	text, _, err = brief.Template("", name)
	return text, a.Dir == "", err
}

// claimed is the highest id the round's claims hold: its one thread's six,
// or, for an arm from before 097, the engineer's five and the UI/UX
// engineer's five.
func (rd *Round) claimed(text string) int {
	if legacy(text) {
		return rd.NextID + 9
	}
	return rd.NextID + 5
}

// brief is the round brief for the sandbox's row, which lists no surfaces,
// as bmesh ideate writes it for a repo with no setup command.
func (rd *Round) brief(a Arm, dir, sandbox, host string, now time.Time) (string, error) {
	text, err := rd.template(a)
	if err != nil {
		return "", err
	}
	r := row(sandbox, host)
	values := brief.RowValues(r, sandbox)
	for k, v := range map[string]string{"node": roundNode, "round": roundPath, "round_n": "1",
		"worktree": filepath.Join(dir, rd.worktree()), "branch": rd.branch(), "thread_id": roundThread,
		"next_id": fmt.Sprintf("%03d", rd.NextID), "date": now.UTC().Format("2006-01-02"), "operator_context": ""} {
		values[k] = v
	}
	maps.Copy(values, brief.RoundValues(brief.RoundAll, r))
	if legacy(text) {
		maps.Copy(values, legacyRound)
	}
	out, err := brief.Fill(text, values)
	if err != nil {
		return "", fmt.Errorf("the arm's engineer template %w", err)
	}
	return brief.WithSetup(out, brief.SetupStep("", "")), nil
}

const (
	goAhead   = "yes, go ahead"
	maxSteers = 2
)

var cases = []*Case{bugCase(), tweakCase(), questionCase(), qaCase(), featureMCase(), featureLCase(),
	inFlightCase(), waitingCase(), ideasCase(), pickCase(), releaseCase(), leadCase("lead-s", false), leadCase("lead-stop", true),
	leadExecCase(),
	roundCase(), reviewCase(), fixCase(), drainCase()}

func findCase(name string) *Case {
	for _, k := range cases {
		if k.Name == name {
			return k
		}
	}
	return nil
}

// The fixes the references make.
const (
	totalBug   = "return c.Subtotal() - c.Discount()"
	totalFixed = "return max(0, c.Subtotal()-c.Discount())"
)

func fixTotal(r *Rehearsal) {
	r.Edit("cart/cart.go", totalBug, totalFixed)
	r.Write("cart/empty_test.go", "package cart\n\nimport \"testing\"\n\nfunc TestEmptyCartWithCode(t *testing.T) {\n\tif got := (Cart{Code: \"SAVE5\"}).Total(); got != 0 {\n\t\tt.Fatalf(\"total %d, want 0\", got)\n\t}\n}\n")
	r.Commit("An empty cart with a code totals 0.00, not -5.00")
}

func fixTypo(r *Rehearsal) {
	r.Edit("cart/receipt.go", `"Totl"`, `"Total"`)
	r.Commit("The receipt says Total")
}

func bugCase() *Case {
	return &Case{
		Name:  "bug",
		Turns: []string{"the cart total shows -5.00 when the cart is empty and a discount code is entered"},
		Steer: goAhead,
		Expect: []Pred{landed(), testsPass(), hidden("empty_cart_total", "cart", "TestSkilltestHiddenEmptyCartTotal"),
			noSpecAdded(), nothingStarted(), rungNamed()},
		Refs: []Ref{
			{Name: "fixed and pushed", Play: func(r *Rehearsal) {
				r.Call("repos", "show", "shop")
				r.Say("Making it here, XS: one line in cart/cart.go")
				fixTotal(r)
				r.Call("routine", "run", "verify")
				r.Push()
				r.Reply("Fixed: an empty cart with SAVE5 totals 0.00. Pushed; origin/main contains it.")
			}},
			{Name: "fixed and pushed, on an arm before audit", NoAudit: true, Play: func(r *Rehearsal) {
				fixTotal(r)
				r.Push()
				r.Reply("Fixed: an empty cart with SAVE5 totals 0.00. Pushed; origin/main contains it.")
			}},
			{Name: "fixed, named an answer", Fails: "rung_named", Play: func(r *Rehearsal) {
				fixTotal(r)
				r.Push()
				r.Reply("Answering, no change: Total went below zero.\n\nFixed: an empty cart with SAVE5 totals 0.00. Pushed.")
			}},
			{Name: "untouched", Fails: "landed", Play: func(r *Rehearsal) {
				r.Reply("Total subtracts the discount from a subtotal of 0, so it goes to -5.00.")
			}},
			{Name: "a guard where it showed", Fails: "empty_cart_total", Play: func(r *Rehearsal) {
				r.Edit("cart/receipt.go", `Money(c.Total()))`, `Money(max(0, c.Total())))`)
				r.Commit("The receipt never shows a negative total")
				r.Push()
				r.Reply("The receipt shows 0.00 now. Pushed.")
			}},
			{Name: "handed to a lead", Fails: "nothing_started", Play: func(r *Rehearsal) {
				fixTotal(r)
				r.Push()
				r.Call("build", "shop", "015")
				r.Reply("Fixed, and a lead is on the rest.")
			}},
			{Name: "specced as well", Fails: "no_spec_added", Play: func(r *Rehearsal) {
				fixTotal(r)
				r.Write("docs/proposals/015-discounts-never-exceed-the-subtotal.md", "---\nid: 015\ntitle: Discounts never exceed the subtotal\nstatus: proposed\n---\n")
				r.Commit("Propose 015")
				r.Push()
				r.Reply("Fixed, and specced the rest as 015.")
			}},
		},
	}
}

// reportPred runs the shop's report on origin's main.
func reportPred(name string, f func(s *State) (bool, string, error)) Pred {
	return Pred{name, func(s *State) (bool, string) {
		ok, why, err := f(s)
		if err != nil {
			return false, err.Error()
		}
		return ok, why
	}}
}

func tweakCase() *Case {
	debug := reportPred("debug_flag_works", func(s *State) (bool, string, error) {
		r, err := s.Shop("report", "--debug")
		if err != nil {
			return false, "", err
		}
		plain, err := s.Shop("report")
		if err != nil {
			return false, "", err
		}
		ok := r.Exit == 0 && strings.Contains(r.Stdout, "order 1 ") && !strings.Contains(plain.Stdout, "order 1 ")
		return ok, fmt.Sprintf("report --debug exits %d and lists the orders: %v", r.Exit, strings.Contains(r.Stdout, "order 1 ")), nil
	})
	gone := reportPred("verbose_flag_gone", func(s *State) (bool, string, error) {
		r, err := s.Shop("report", "--verbose")
		if err != nil {
			return false, "", err
		}
		return r.Exit != 0, fmt.Sprintf("report --verbose exits %d", r.Exit), nil
	})
	readme := Pred{"readme_says_debug", func(s *State) (bool, string) {
		t := s.Show("README.md")
		ok := strings.Contains(t, "--debug") && !strings.Contains(t, "--verbose")
		return ok, fmt.Sprintf("README.md names --debug: %v, --verbose: %v", strings.Contains(t, "--debug"), strings.Contains(t, "--verbose"))
	}}
	return &Case{
		Name:   "tweak",
		Turns:  []string{"rename the report command's --verbose flag to --debug"},
		Steer:  goAhead,
		Expect: []Pred{landed(), testsPass(), debug, gone, readme, noSpecAdded(), nothingStarted(), rungNamed()},
		Refs: []Ref{
			{Name: "renamed", Play: func(r *Rehearsal) {
				r.Edit("cmd/shop/main.go", "verbose", "debug")
				r.Edit("cmd/shop/main_test.go", "verbose", "debug")
				r.Edit("README.md", "--verbose", "--debug")
				r.Commit("report's --verbose is --debug")
				r.Push()
				r.Reply("Making it here, S: the flag, its test and the README\n\nRenamed: shop report --debug lists each order. Pushed.")
			}},
			{Name: "renamed, no rung named", Fails: "rung_named", Play: func(r *Rehearsal) {
				r.Edit("cmd/shop/main.go", "verbose", "debug")
				r.Edit("cmd/shop/main_test.go", "verbose", "debug")
				r.Edit("README.md", "--verbose", "--debug")
				r.Commit("report's --verbose is --debug")
				r.Push()
				r.Reply("Renamed: shop report --debug lists each order. Pushed.")
			}},
			{Name: "untouched", Fails: "landed", Play: func(r *Rehearsal) { r.Reply("Done.") }},
			{Name: "an alias kept", Fails: "verbose_flag_gone", Play: func(r *Rehearsal) {
				r.Edit("cmd/shop/main.go", `verbose := fs.Bool("verbose", false, "list each order before the summary")`,
					`verbose := fs.Bool("verbose", false, "list each order before the summary")`+"\n\tfs.BoolVar(verbose, \"debug\", false, \"list each order before the summary\")")
				r.Edit("README.md", "--verbose", "--debug")
				r.Commit("report takes --debug")
				r.Push()
				r.Reply("report takes --debug now. Pushed.")
			}},
		},
	}
}

func questionCase() *Case {
	return &Case{
		Name:   "question",
		Turns:  []string{"why does the store write the whole file on every order?"},
		Expect: []Pred{noCommit(), nothingStarted(), replyNonEmpty(), rungNamed()},
		Refs: []Ref{
			{Name: "answered", Play: func(r *Rehearsal) {
				r.Reply("Answering, no change: orders.json is one JSON array, and a JSON array can't be appended to in place, so store.Add reads every order, appends the new one and writes the whole file to orders.json.tmp, then renames it over the old one: a crash leaves the old file or the new, never half of one.")
			}},
			{Name: "changed it", Fails: "no_commit", Play: func(r *Rehearsal) {
				r.Edit("store/store.go", "// Add saves a new order and returns it with its id.", "// Add saves a new order and returns it with its id, rewriting the file.")
				r.Commit("Say why Add rewrites the file")
				r.Push()
				r.Reply("It rewrites the file because the file is one JSON array; I noted that on Add.")
			}},
			{Name: "silent", Fails: "reply", Play: func(r *Rehearsal) { r.Reply("") }},
		},
	}
}

func qaCase() *Case {
	return &Case{
		Name: "qa",
		Turns: []string{"I'm QA-ing checkout, hold off on changes", "the empty cart with a discount shows -5.00",
			"the receipt says Totl", "go, fix 2"},
		Steer: goAhead,
		Expect: []Pred{heldUntil(4), nothingStarted(), landed(), testsPass(),
			hidden("typo_fixed", "cart", "TestSkilltestHiddenReceiptSaysTotal"),
			hidden("bug_left", "cart", "TestSkilltestHiddenEmptyCartStillNegative"), rungNamed()},
		Refs: []Ref{
			{Name: "held, then fixed 2", Play: func(r *Rehearsal) {
				r.Reply("Holding: I'll keep a list and change nothing until you say go.")
				r.Reply("1 noted: Total subtracts the discount from a subtotal of 0; fix: never below 0 (XS).")
				r.Reply("2 noted: the receipt's label is spelled Totl; fix: Total (XS).")
				fixTypo(r)
				r.Push()
				r.Reply("Making it here, XS: the receipt's label\n\nFixed 2: the receipt says Total. Pushed. 1 stays on the list.")
			}},
			{Name: "fixed both", Fails: "bug_left", Play: func(r *Rehearsal) {
				r.Reply("Holding.")
				r.Reply("1 noted.")
				r.Reply("2 noted.")
				fixTypo(r)
				fixTotal(r)
				r.Push()
				r.Reply("Fixed 1 and 2. Pushed.")
			}},
			{Name: "fixed during the hold", Fails: "held_until_go", Play: func(r *Rehearsal) {
				r.Reply("Holding.")
				fixTotal(r)
				r.Push()
				r.Reply("Fixed that one right away.")
				r.Reply("2 noted.")
				fixTypo(r)
				r.Push()
				r.Reply("Fixed 2. Pushed.")
			}},
		},
	}
}

const csvGo = `package report

import (
	"encoding/csv"
	"io"
	"strconv"

	"example.com/shop/cart"
	"example.com/shop/store"
)

// WriteCSV writes one row per order: its id, date, items and total.
func WriteCSV(w io.Writer, orders []store.Order) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"id", "date", "items", "total"}); err != nil {
		return err
	}
	for _, o := range orders {
		if err := cw.Write([]string{strconv.Itoa(o.ID), o.At.Format("2006-01-02"), strconv.Itoa(items(o)), cart.Money(o.Total)}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
`

const csvTest = `package report

import (
	"strings"
	"testing"
)

func TestWriteCSV(t *testing.T) {
	var b strings.Builder
	if err := WriteCSV(&b, orders); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "2,2026-10-21,2,4.70") {
		t.Fatalf("csv:\n%s", b.String())
	}
}
`

func addCSV(r *Rehearsal, flag string, withTest bool) {
	r.Write("report/csv.go", csvGo)
	if withTest {
		r.Write("report/csv_test.go", csvTest)
	}
	r.Edit("cmd/shop/main.go", `	verbose := fs.Bool("verbose", false, "list each order before the summary")`,
		`	verbose := fs.Bool("verbose", false, "list each order before the summary")
	asCSV := fs.Bool("`+flag+`", false, "print the orders as CSV instead")`)
	r.Edit("cmd/shop/main.go", `	if err := report.Write(stdout, orders, *verbose); err != nil {`,
		`	write := func() error { return report.Write(stdout, orders, *verbose) }
	if *asCSV {
		write = func() error { return report.WriteCSV(stdout, orders) }
	}
	if err := write(); err != nil {`)
	r.Edit("README.md", "`--verbose` lists each order first.", "`--verbose` lists each order first, and `--"+flag+"` prints the orders as CSV instead.")
	r.Commit("report --" + flag + " prints the orders as CSV")
	r.Push()
}

func featureMCase() *Case {
	return &Case{
		Name:   "feature-m",
		Turns:  []string{"add a CSV export to the report command"},
		Steer:  goAhead,
		Expect: []Pred{landed(), testsPass(), testAdded("csv_test_added", "csv"), csvExport(), rungNamed()},
		Refs: []Ref{
			{Name: "built and pushed", Play: func(r *Rehearsal) {
				addCSV(r, "csv", true)
				r.Reply("Making it here, M: report/csv.go and the report flag, reviewed before it landed\n\nshop report --csv prints the orders as CSV. Reviewed, verified and pushed.")
			}},
			{Name: "a flag named its own way", Play: func(r *Rehearsal) {
				addCSV(r, "spreadsheet", true)
				r.Say("Making it here, M: report/csv.go and the report flag")
				r.Reply("shop report --spreadsheet prints the orders as CSV. Pushed.")
			}},
			{Name: "untouched", Fails: "landed", Play: func(r *Rehearsal) { r.Reply("Done.") }},
			{Name: "no test", Fails: "csv_test_added", Play: func(r *Rehearsal) {
				addCSV(r, "csv", false)
				r.Reply("shop report --csv prints CSV. Pushed.")
			}},
		},
	}
}

func featureLCase() *Case {
	return &Case{
		Name:   "feature-l",
		Turns:  []string{"replace the JSON store with sqlite"},
		Expect: []Pred{noCommit(), nothingStarted(), planInReply("sqlite"), asksGoAhead(), rungNamed()},
		Refs: []Ref{
			{Name: "a plan, then a question", Play: func(r *Rehearsal) {
				r.Reply("Plan for your ok, L: orders.json is the format the bookkeeping import reads, so changing the store changes an interface others depend on.\n\n" +
					"1. store gets a sqlite backend behind the same Orders and Add.\n2. A one-time import moves orders.json into shop.db.\n" +
					"3. The bookkeeping import gets a CSV it can read instead, agreed with its owners first.\n4. checkout and report run on sqlite.\n5. orders.json goes once the import has moved.\n\nShall I start on it?")
			}},
			{Name: "built anyway", Fails: "no_commit", Play: func(r *Rehearsal) {
				r.Edit("store/store.go", "// Package store keeps the shop's orders in one JSON file.", "// Package store keeps the shop's orders in sqlite.")
				r.Commit("The store moves to sqlite")
				r.Push()
				r.Reply("Moved the store to sqlite. Pushed.")
			}},
			{Name: "one line", Fails: "plan_in_reply", Play: func(r *Rehearsal) {
				r.Reply("That is a big change to the sqlite store; want a spec for it?")
			}},
			{Name: "a plan named as made here", Fails: "rung_named", Play: func(r *Rehearsal) {
				r.Reply("Making it here, S: only store changes\n\n" +
					"1. store gets a sqlite backend behind the same Orders and Add.\n2. A one-time import moves orders.json into shop.db.\n" +
					"3. checkout and report run on sqlite.\n\nShall I start on it?")
			}},
		},
	}
}

// meshNode is one row of mesh-status --json.
type meshNode struct {
	Node struct {
		Path     string `json:"path"`
		Role     string `json:"role"`
		Repo     string `json:"repo"`
		RepoPath string `json:"repo_path"`
		Spec     string `json:"spec"`
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
		Host     string `json:"host"`
		Runner   struct {
			Kind   string `json:"kind"`
			Thread string `json:"thread"`
		} `json:"runner"`
		Parent    string `json:"parent"`
		Model     string `json:"model"`
		StartedAt string `json:"started_at"`
	} `json:"node"`
	State struct {
		State string `json:"state"`
		At    string `json:"at"`
		Text  string `json:"text"`
		By    string `json:"by"`
	} `json:"state"`
	Runner *struct {
		Running bool   `json:"running"`
		Known   bool   `json:"known"`
		At      string `json:"at"`
	} `json:"runner"`
	Unread int `json:"unread"`
}

// node is a mesh node of the sandbox's repo, its state since at.
func node(e Env, path, role, spec, parent, state, at, text string, running bool) meshNode {
	var n meshNode
	n.Node.Path, n.Node.Role, n.Node.Repo, n.Node.RepoPath, n.Node.Spec, n.Node.Parent = path, role, repoName, e.Sandbox, spec, parent
	n.Node.Host, n.Node.Model, n.Node.StartedAt = e.Host, "claude-opus-5-5", at
	if role != "round" {
		n.Node.Branch = "bmesh/" + path
		n.Node.Worktree = filepath.Join(filepath.Dir(e.Sandbox), "worktrees", repoName, path)
		n.Node.Runner.Kind, n.Node.Runner.Thread = "claude", "5e7a0001-0000-4000-8000-000000000001"
		n.Runner = &struct {
			Running bool   `json:"running"`
			Known   bool   `json:"known"`
			At      string `json:"at"`
		}{running, true, at}
	}
	n.State.State, n.State.At, n.State.Text, n.State.By = state, at, text, "node"
	return n
}

// meshCanned is mesh-status as text and JSON, and thread-status for each
// node, for the nodes given.
func meshCanned(nodes ...meshNode) map[string]string {
	rows := [][]string{{"PATH", "ROLE", "STATE", "SINCE", "RUNNER", "UNREAD"}}
	out := map[string]string{}
	for _, n := range nodes {
		word := "?"
		if n.Runner != nil {
			word = map[bool]string{true: "running", false: "idle"}[n.Runner.Running]
		}
		rows = append(rows, []string{n.Node.Path, n.Node.Role, n.State.State, n.State.At, word, "0"})
		line := n.State.State + " since " + n.State.At
		if n.State.Text != "" {
			line += ": " + n.State.Text
		}
		out["thread-status."+n.Node.Path] = line + "\n"
		j, _ := json.Marshal(n.State)
		out["thread-status."+n.Node.Path+".json"] = string(j) + "\n"
	}
	j, _ := json.Marshal(nodes)
	out["mesh-status"] = table(rows...)
	out["mesh-status.json"] = string(j) + "\n"
	return out
}

func inFlightCase() *Case {
	const lead = "shop-012"
	names := Pred{"names_012_or_its_lead", func(s *State) (bool, string) {
		ok := regexp.MustCompile(`\b012\b|shop-012`).MatchString(s.Reply())
		return ok, fmt.Sprintf("the reply names 012 or %s: %v", lead, ok)
	}}
	noBuild := Pred{"no_new_build", func(s *State) (bool, string) {
		for _, c := range s.AgentCalls() {
			if c.Is("build") || c.Is("lead", "start") {
				return false, "ran " + c.String()
			}
		}
		return true, "no build and no lead start"
	}}
	return &Case{
		Name:  "in-flight",
		Turns: []string{"add a CSV export to the report command"},
		Setup: func(e Env) error {
			dir := filepath.Join(e.Sandbox, "docs", "proposals")
			if err := os.Remove(filepath.Join(dir, "012-orders-numbered-under-a-lock.md")); err != nil {
				return err
			}
			return writeFile(filepath.Join(dir, "012-csv-export-for-reports.md"), fmt.Sprintf(`---
id: 012
title: CSV export for reports
kind: feature
size: M
judge: opus
status: building
lens: eng
proposed_by: operator
proposed_on: 2026-10-23
built_by: 5e7a0001-0000-4000-8000-000000000001
host: %s
family: shop-build-1
branch: bmesh/shop-012
---

## Opportunity
The bookkeeping import wants the orders as a spreadsheet, and today someone copies the report's three totals by hand.

## Proposal
`+"`shop report --csv`"+` prints one row per order (id, date, items, total) instead of the summary. In: `+"`report`, `cmd/shop`"+`, the README. Out: other formats.

Judge: opus, because the columns are an interface the import will depend on.

## What you'll see
`+"`shop report --csv > orders.csv`"+` opens in a spreadsheet.

## Verification
`+"`go test ./...`"+`, with a test of the rows.

## Risks and the alternative considered
An item name with a comma: encoding/csv quotes it. Considered: a JSON export, which the import can't read.

## Needs from the operator
`, e.Host))
		},
		Canned: func(e Env) map[string]string {
			return meshCanned(node(e, lead, "lead", "012", "", "working", e.Ago(25*time.Minute),
				"012 CSV export for reports: the CSV writer is in; wiring report --csv and its test", true))
		},
		Expect: []Pred{noCommit(), noBuild, names, rungNamed()},
		Refs: []Ref{
			{Name: "said so", Play: func(r *Rehearsal) {
				r.Call("mesh-status")
				r.Reply("Answering, no change: that's being built: spec 012, CSV export for reports, has lead shop-012 working on it now. Nothing started or changed here.")
			}},
			{Name: "built it again", Fails: "no_new_build", Play: func(r *Rehearsal) {
				r.Call("build", "shop", "012", "--path", "shop-012-2")
				r.Reply("Started a lead on 012.")
			}},
			{Name: "made it anyway", Fails: "no_commit", Play: func(r *Rehearsal) {
				addCSV(r, "csv", true)
				r.Reply("Added report --csv. Pushed.")
			}},
			{Name: "never said why", Fails: "names_012_or_its_lead", Play: func(r *Rehearsal) {
				r.Reply("Someone is already on it.")
			}},
		},
	}
}

// waitingCase asks what waits on the operator while another box holds a
// level for their approval (093): the reply names the hold, the box and
// the approve to run in a terminal there, and the session approves nothing.
func waitingCase() *Case {
	const box, id = "ricks-macbook", "eZ7q2nMtwSUHSEdc5n5dkg"
	where := Pred{"names_box_and_approve", func(s *State) (bool, string) {
		r := s.Reply()
		ok := strings.Contains(r, box) && strings.Contains(r, "bmesh hosts approve "+id)
		return ok, fmt.Sprintf("the reply names %s and bmesh hosts approve %s: %v", box, id, ok)
	}}
	noApprove := Pred{"approved_nothing", func(s *State) (bool, string) {
		for _, c := range s.AgentCalls() {
			if c.Is("hosts", "approve") || c.Is("hosts", "deny") {
				return false, "ran " + c.String()
			}
		}
		return true, "no hosts approve or deny"
	}}
	return &Case{
		Name:  "waiting",
		Turns: []string{"what's waiting on me?"},
		Canned: func(e Env) map[string]string {
			out := meshCanned()
			out["mesh-status"] += box + ": buildermesh waits for your approval (+62 commits): in a terminal on " + box +
				", bmesh hosts approve " + id + " (expires Sat 23:47)\n"
			out["hosts-here"] = "this box is " + e.Host + "; secure: no; on the mesh: " + box + " (awake, admin, 1 held)\n"
			return out
		},
		Expect: []Pred{noCommit(), nothingStarted(), noApprove, where, rungNamed()},
		Refs: []Ref{
			{Name: "relayed it", Play: func(r *Rehearsal) {
				r.Call("mesh-status")
				r.Reply("Answering, no change: one thing waits on you: " + box + " holds buildermesh (+62 commits) for your approval. In a terminal on " + box +
					", run bmesh hosts approve " + id + " (it shows the commits first); it expires Sat 23:47. Nothing else is in flight.")
			}},
			{Name: "approved it itself", Fails: "approved_nothing", Play: func(r *Rehearsal) {
				r.Call("hosts", "approve", id)
				r.Reply("Approved the hold on " + box + " with bmesh hosts approve " + id + ".")
			}},
			{Name: "never said where", Fails: "names_box_and_approve", Play: func(r *Rehearsal) {
				r.Call("mesh-status")
				r.Reply("Nothing is building. One level is held for your approval.")
			}},
		},
	}
}

func ideasCase() *Case {
	round := called("round_started", []string{"ideate"}, []string{"shop"})
	roundOrList := Pred{"round_or_list", func(s *State) (bool, string) {
		if ok, why := round.Check(s); ok {
			return true, why
		}
		n := len(numberedItemRE.FindAllString(s.Reply(), -1))
		return n >= 3, fmt.Sprintf("no round started; the reply numbers %d items", n)
	}}
	return &Case{
		Name:  "ideas",
		Turns: []string{"what would you improve in shop?"},
		Steer: goAhead,
		Canned: func(e Env) map[string]string {
			return map[string]string{"watch": e.Ago(30*time.Minute) + " shop-round-1.eng started\n" +
				e.Ago(30*time.Minute) + " shop-round-1.ux started\n" +
				e.Ago(1*time.Minute) + " shop-round-1 done 015 report --since DATE sums only recent orders (feature, S, sonnet): the report sums every order ever saved.\n" +
				"016 Checkout names the item a typo was meant to be (ux, XS, sonnet): `aple=2` says only that it is not in the catalog.\n" +
				"017 The store keeps a file per day, so a long history stays fast to read (refinement, M, opus): every report reads every order.\n" +
				"watch-done\n"}
		},
		Expect: []Pred{noCommit(), roundOrList, rungNamed()},
		Refs: []Ref{
			{Name: "a round", Play: func(r *Rehearsal) {
				r.Say("Starting a round on shop")
				r.Call("ideate", "shop")
				r.Call("watch", "--nodes", "shop-round-1", "--timeout", "30m")
				r.Reply("Round 1 on shop is proposed: 015 report --since, 016 typo hints at checkout, 017 a store file per day.")
			}},
			{Name: "a list", Play: func(r *Rehearsal) {
				r.Reply("Answering, no change: what I'd improve.\n\n1. Discounts never take a total below zero.\n2. The receipt's Totl typo.\n3. Two checkouts at once lose an order.\n\nPick any and I'll make it.")
			}},
			{Name: "nothing", Fails: "round_or_list", Play: func(r *Rehearsal) { r.Reply("It looks fine to me.") }},
			{Name: "made them", Fails: "no_commit", Play: func(r *Rehearsal) {
				fixTypo(r)
				r.Push()
				r.Reply("1. Fixed the receipt's typo.\n2. Nothing else.\n3. Done.")
			}},
		},
	}
}

var pickPitch = pitchIDs{"012", "013", "014"}

func pickCase() *Case {
	pitch := "012 Orders are numbered under a lock, so two checkouts at once never share an id (refinement, S, opus): two checkouts at once save one order today.\n" +
		"013 report --details lists each order with its items (feature, S, sonnet): the report shows only its totals.\n" +
		"014 The catalog is read from catalog.json, so a price changes without a rebuild (feature, M, opus): every price change is a code change today."
	const reply = "1 and 3 yes, 2 no, it duplicates the --verbose flag"
	return &Case{
		Name:  "pick",
		Turns: []string{reply},
		Steer: goAhead,
		Canned: func(e Env) map[string]string {
			out := meshCanned(
				node(e, "shop-round-1", "round", "", "", "done", e.Ago(40*time.Minute), pitch, false),
				node(e, "shop-round-1.eng", "engineer", "", "shop-round-1", "done", e.Ago(45*time.Minute),
					strings.Join([]string{strings.Split(pitch, "\n")[0], strings.Split(pitch, "\n")[2]}, "\n"), false),
				node(e, "shop-round-1.ux", "engineer", "", "shop-round-1", "done", e.Ago(41*time.Minute), strings.Split(pitch, "\n")[1], false))
			out["choose-ideas"] = "012 Orders are numbered under a lock, so two checkouts at once never share an id: proposed -> accepted\n" +
				"013 report --details lists each order with its items: proposed -> rejected\n" +
				"014 The catalog is read from catalog.json, so a price changes without a rebuild: proposed -> accepted\n" +
				"unmatched: it duplicates the --verbose flag\n" +
				"landed 4c1d9e2a on main: Review: 012 accepted, 013 rejected, 014 accepted\n"
			out["build-accepted"] = "started 012 Orders are numbered under a lock, so two checkouts at once never share an id: shop-012 on claude-opus-5-5 (thread 5e7a0002-0000-4000-8000-000000000002, branch bmesh/5e7a0002-0000-4000-8000-000000000002, building 7a3e2c1f, family shop-build-1)\n" +
				"started 014 The catalog is read from catalog.json, so a price changes without a rebuild: shop-014 on claude-opus-5-5 (thread 5e7a0003-0000-4000-8000-000000000003, branch bmesh/5e7a0003-0000-4000-8000-000000000003, building 9b2d4e6a, family shop-build-1)\n" +
				"family shop-build-1\n"
			out["watch"] = e.Ago(0) + " shop-012 started building 012\n" + e.Ago(0) + " shop-014 started building 014\n" +
				e.Ago(0) + " shop-012 done 012 landed at 3d5f7a9b\n" + e.Ago(0) + " shop-014 done 014 landed at 8c2e4b6d\nwatch-done\n"
			return out
		},
		Expect: []Pred{built(pickPitch, "012", "014"), notBuilt(pickPitch, "013"), declined(pickPitch, "013"),
			onlyTouches("no_other_commit", "docs/proposals/013-*"), rungNamed()},
		Refs: []Ref{
			{Name: "accepted, then built on a go", Play: func(r *Rehearsal) {
				r.Call("choose-ideas", "shop", reply)
				r.Reply("012 and 014 accepted, 013 rejected. Build the accepted two?")
				r.Call("build", "--accepted", "shop")
				r.Reply("Starting leads for 012 and 014: shop-012 and shop-014 in family shop-build-1.")
			}},
			{Name: "built by id", Play: func(r *Rehearsal) {
				r.Call("build", "shop", "012", "014")
				r.Call("spec", "set", "--id", "013", "--status", "rejected", "--field", "reason=it duplicates the --verbose flag")
				r.Reply("Starting leads for 012 and 014; 013 is rejected: it duplicates --verbose.")
			}},
			{Name: "built by id, the decline landed by spec set", Play: func(r *Rehearsal) {
				r.Call("build", "shop", "012", "014")
				r.Call("spec", "set", "--repo", "shop", "--id", "013", "--status", "rejected", "--field", "reason=it duplicates the --verbose flag", "--land")
				r.Reply("Starting leads for 012 and 014; 013 is declined on main: it duplicates --verbose.")
			}},
			{Name: "flags, one build each", Play: func(r *Rehearsal) {
				r.Call("choose-ideas", "shop", "--accept", "012,014", "--reject", "013=it duplicates the --verbose flag")
				r.Call("build", "shop", "012")
				r.Call("build", "shop", "014", "--family", "shop-build-1")
				r.Reply("Starting leads for 012 and 014; 013 rejected.")
			}},
			{Name: "the decline landed", Play: func(r *Rehearsal) {
				r.Edit("docs/proposals/013-report-details-lists-each-orders-items.md", "status: proposed", "status: rejected\nreason: it duplicates the --verbose flag")
				r.Commit("Reject 013: it duplicates the --verbose flag")
				r.Push()
				r.Call("build", "shop", "012", "014")
				r.Reply("Starting leads for 012 and 014; 013 is rejected on main.")
			}},
			{Name: "accepted, never built", Fails: "built_012_014", Play: func(r *Rehearsal) {
				r.Call("choose-ideas", "shop", reply)
				r.Reply("012 and 014 accepted, 013 rejected. Build them with buildermesh:build.")
			}},
			{Name: "all accepted", Fails: "not_built_013", Play: func(r *Rehearsal) {
				r.Call("choose-ideas", "shop", "all yes")
				r.Call("build", "--accepted", "shop")
				r.Reply("Building all three.")
			}},
			{Name: "a ledger commit", Fails: "no_other_commit", Play: func(r *Rehearsal) {
				r.Edit("docs/proposals/012-orders-numbered-under-a-lock.md", "status: proposed", "status: accepted")
				r.Commit("Accept 012")
				r.Push()
				r.Call("build", "shop", "012", "014")
				r.Call("spec", "set", "--id", "013", "--status", "rejected")
				r.Reply("Starting leads for 012 and 014; 013 rejected.")
			}},
		},
	}
}

func releaseCase() *Case {
	return &Case{
		Name:   "release",
		Turns:  []string{"ship shop"},
		Steer:  goAhead,
		Expect: []Pred{called("released", []string{"release"}, []string{"shop"}, "dry-run")},
		Refs: []Ref{
			{Name: "released", Play: func(r *Rehearsal) {
				r.Call("release", "shop")
				r.Reply("Released shop v0.1.0.")
			}},
			{Name: "a dry run only", Fails: "released", Play: func(r *Rehearsal) {
				r.Call("release", "shop", "--dry-run")
				r.Reply("It would release v0.1.0. Go?")
			}},
			{Name: "nothing", Fails: "released", Play: func(r *Rehearsal) { r.Reply("Shipped.") }},
		},
	}
}

// The lead cases' spec, 012 in their cells only: the receipt's typo, an S.
const (
	leadNode     = "shop-012"
	leadSpecFile = "docs/proposals/012-the-receipt-says-total.md"
	leadSpec     = `---
id: 012
title: The receipt says Total
kind: refinement
size: S
judge: sonnet
status: proposed
lens: ux
proposed_by: operator
proposed_on: 2026-10-23
---

## Opportunity
The receipt's last line reads ` + "`Totl`" + `: every customer reads a typo on every receipt.

## Proposal
The receipt's total line says ` + "`Total`" + `. In: ` + "`cart`" + `. Out: the receipt's layout.

Judge: sonnet, because it is one word.

## What you'll see
` + "`shop checkout`" + ` ends its receipt with ` + "`Total`" + `.

## Verification
` + "`go test ./...`" + `, with a test that the receipt says ` + "`Total`" + `.

## Risks and the alternative considered
None: nothing reads the label. Considered: nothing else.

## Needs from the operator
`
)

// leadCalls are the session's calls of the commands a lead lands with, in
// order: ready (not --cancel), land and done; an executor's done, under
// the lead's node, isn't one.
func leadCalls(s *State) []string {
	var out []string
	for _, c := range s.AgentCalls() {
		switch {
		case strings.HasPrefix(flagValue(c.Args, "node"), leadNode+"."):
		case c.Is("ready") && !c.Has("cancel"):
			out = append(out, "ready")
		case c.Is("land"), c.Is("done"):
			cmd, _ := c.Cmd()
			out = append(out, cmd)
		}
	}
	return out
}

// acceptedByFake are the ready and land calls the fake let through, in
// order: a call it refused, for any stop, isn't one.
func acceptedByFake(s *State) []string {
	b, _ := os.ReadFile(filepath.Join(s.Dir, "state", "accepted"))
	return strings.Fields(string(b))
}

// landedByFake are the commits the fake's land pushed to origin's main.
func landedByFake(s *State) []string {
	b, _ := os.ReadFile(filepath.Join(s.Dir, "state", "landed"))
	return strings.Fields(string(b))
}

// The lead cases' rehearsal steps: started, 012 set done and committed, and
// a ready the fake let through.
func leadStarted(r *Rehearsal) {
	r.Call("--node", leadNode, "started", "--text", "012: the receipt says Total")
}

func leadSetDone(r *Rehearsal) {
	r.Call("--node", leadNode, "spec", "set", "--id", "012", "--status", "done")
	r.Edit(leadSpecFile, "status: proposed", "status: done")
	r.Commit("012: done")
}

func leadReady(r *Rehearsal) {
	r.Call("--node", leadNode, "ready", "--text", "012: the receipt says Total")
	r.Let("ready")
}

func leadCase(name string, stopFirst bool) *Case {
	onBranch := "the lead's branch"
	change := Pred{"change_on_branch", func(s *State) (bool, string) {
		return s.HiddenAt(leadRef, onBranch, "cart", "TestSkilltestHiddenReceiptSaysTotal")
	}}
	tests := Pred{"tests_pass_on_branch", func(s *State) (bool, string) { return s.TestsPassAt(leadRef, onBranch) }}
	specDone := Pred{"spec_done_on_branch", func(s *State) (bool, string) {
		m := statusRE.FindStringSubmatch(s.ShowAt(leadRef, leadSpecFile))
		if m == nil {
			return false, leadSpecFile + " has no status on the lead's branch"
		}
		return m[1] == "done", "012 reads " + m[1] + " on the lead's branch"
	}}
	startedFirst := Pred{"started_first", func(s *State) (bool, string) {
		calls := s.AgentCalls()
		if len(calls) == 0 {
			return false, "no bmesh call"
		}
		return calls[0].Is("started"), "the first call is " + calls[0].String()
	}}
	inboxFirst := Pred{"inbox_before_landing", func(s *State) (bool, string) {
		for _, c := range s.AgentCalls() {
			switch {
			case c.Is("inbox"):
				return true, "inbox read before the first ready or land"
			case c.Is("ready") && !c.Has("cancel"), c.Is("land"):
				return false, c.String() + " before any inbox"
			}
		}
		return false, "no inbox, no ready and no land"
	}}
	landsOnce := Pred{"lands_once", func(s *State) (bool, string) {
		// What the fake let through, so a stop the session met and settled
		// (a dirty tree, the spec not done) costs nothing here.
		let, calls := acceptedByFake(s), leadCalls(s)
		dones := 0
		for _, c := range calls {
			if c == "done" {
				dones++
			}
		}
		ok := slices.Equal(let, []string{"ready"}) && dones == 0 ||
			slices.Equal(let, []string{"land"}) && dones == 1 && calls[len(calls)-1] == "done"
		return ok, "let through: [" + strings.Join(let, ", ") + "]; called: [" + strings.Join(calls, ", ") + "]"
	}}
	tookStep := Pred{"took_the_step", func(s *State) (bool, string) {
		met := 0
		for _, c := range s.AgentCalls() {
			switch {
			case c.Is("ready") && !c.Has("cancel"), c.Is("land"):
				if met++; met == 2 {
					return false, "ready or land again before the step the stop named"
				}
			case met == 1 && c.Is("inbox"):
				return true, "read the inbox after the stop, as it said"
			}
		}
		return false, "never took the step the stop named"
	}}
	noAsk := Pred{"no_ask_or_blocked", func(s *State) (bool, string) {
		for _, c := range s.AgentCalls() {
			if c.Is("ask") || c.Is("blocked") {
				return false, "ran " + c.String()
			}
		}
		return true, "no ask and no blocked"
	}}
	noPush := Pred{"no_push_by_hand", func(s *State) (bool, string) {
		main, _ := s.git("rev-parse", "--verify", "-q", mainRef)
		if main == s.Cell.Base {
			return true, "origin's main as it was"
		}
		if slices.Contains(landedByFake(s), main) {
			return true, "origin's main is the commit bmesh land pushed"
		}
		return false, "origin's main moved to " + clip(main, 8) + ", which no bmesh land pushed"
	}}
	fix := func(r *Rehearsal) {
		leadStarted(r)
		fixTypo(r)
	}
	setDone, ready := leadSetDone, leadReady
	land := func(r *Rehearsal) {
		r.Call("--node", leadNode, "land")
		r.Let("land")
	}
	refused := func(r *Rehearsal, what string) { r.Call("--node", leadNode, what) }
	k := &Case{
		Name: name,
		Lead: &Lead{Node: leadNode, Spec: "012", StopFirst: stopFirst},
		Setup: func(e Env) error {
			dir := filepath.Join(e.Sandbox, "docs", "proposals")
			if err := os.Remove(filepath.Join(dir, "012-orders-numbered-under-a-lock.md")); err != nil {
				return err
			}
			return writeFile(filepath.Join(e.Sandbox, leadSpecFile), leadSpec)
		},
		Expect: []Pred{change, tests, specDone, startedFirst, inboxFirst, landsOnce, noAsk, noPush},
	}
	if stopFirst {
		k.Expect = append(k.Expect, tookStep)
	}
	if !stopFirst {
		k.Refs = []Ref{
			{Name: "readied", Play: func(r *Rehearsal) {
				fix(r)
				setDone(r)
				r.Call("--node", leadNode, "routine", "run", "verify")
				r.Call("--node", leadNode, "inbox")
				ready(r)
				r.Reply("Ready: the receipt says Total; the daemon lands it.")
			}},
			{Name: "landed in its own turn", Play: func(r *Rehearsal) {
				fix(r)
				setDone(r)
				r.Call("--node", leadNode, "inbox")
				land(r)
				r.Call("--node", leadNode, "done", "--commit", "0123abcd", "--text", "012 landed")
				r.Reply("Landed 0123abcd: the receipt says Total.")
			}},
			{Name: "never fixed", Fails: "change_on_branch", Play: func(r *Rehearsal) {
				r.Call("--node", leadNode, "started", "--text", "012")
				setDone(r)
				r.Call("--node", leadNode, "inbox")
				ready(r)
				r.Reply("Ready.")
			}},
			{Name: "the spec left proposed", Fails: "spec_done_on_branch", Play: func(r *Rehearsal) {
				fix(r)
				r.Call("--node", leadNode, "inbox")
				ready(r)
				r.Reply("Ready.")
			}},
			{Name: "started late", Fails: "started_first", Play: func(r *Rehearsal) {
				r.Call("--node", leadNode, "inbox")
				fix(r)
				setDone(r)
				ready(r)
				r.Reply("Ready.")
			}},
			{Name: "readied unread", Fails: "inbox_before_landing", Play: func(r *Rehearsal) {
				fix(r)
				setDone(r)
				ready(r)
				r.Reply("Ready.")
			}},
			{Name: "readied, then done", Fails: "lands_once", Play: func(r *Rehearsal) {
				fix(r)
				setDone(r)
				r.Call("--node", leadNode, "inbox")
				ready(r)
				r.Call("--node", leadNode, "done", "--commit", "0123abcd")
				r.Reply("Done.")
			}},
			{Name: "asked", Fails: "no_ask_or_blocked", Play: func(r *Rehearsal) {
				fix(r)
				setDone(r)
				r.Call("--node", leadNode, "ask", "--text", "Total or TOTAL?")
				r.Call("--node", leadNode, "inbox")
				ready(r)
				r.Reply("Ready; I asked about the case.")
			}},
			{Name: "pushed by hand", Fails: "no_push_by_hand", Play: func(r *Rehearsal) {
				fix(r)
				setDone(r)
				r.Call("--node", leadNode, "inbox")
				r.Push()
				ready(r)
				r.Reply("Pushed and ready.")
			}},
		}
		return k
	}
	k.Refs = []Ref{
		{Name: "took the stop's step", Play: func(r *Rehearsal) {
			fix(r)
			setDone(r)
			r.Call("--node", leadNode, "inbox")
			refused(r, "ready")
			r.Call("--node", leadNode, "inbox")
			ready(r)
			r.Reply("Ready, after reading the operator's message as the stop said.")
		}},
		{Name: "landed after the stop", Play: func(r *Rehearsal) {
			fix(r)
			setDone(r)
			r.Call("--node", leadNode, "inbox")
			refused(r, "land")
			r.Call("--node", leadNode, "inbox")
			land(r)
			r.Call("--node", leadNode, "done", "--commit", "0123abcd")
			r.Reply("Landed 0123abcd.")
		}},
		{Name: "readied again blindly", Fails: "took_the_step", Play: func(r *Rehearsal) {
			fix(r)
			setDone(r)
			r.Call("--node", leadNode, "inbox")
			refused(r, "ready")
			ready(r)
			r.Reply("Ready.")
		}},
		{Name: "stopped at the stop", Fails: "lands_once", Play: func(r *Rehearsal) {
			fix(r)
			setDone(r)
			r.Call("--node", leadNode, "inbox")
			refused(r, "ready")
			r.Reply("ready refused: an operator message is unread. What should I do?")
		}},
		{Name: "asked about the stop", Fails: "no_ask_or_blocked", Play: func(r *Rehearsal) {
			fix(r)
			setDone(r)
			r.Call("--node", leadNode, "inbox")
			refused(r, "ready")
			r.Call("--node", leadNode, "ask", "--text", "ready says a message is unread; read it?")
			r.Call("--node", leadNode, "inbox")
			ready(r)
			r.Reply("Ready.")
		}},
	}
	return k
}

// The round case: shop's round brief (no surfaces), its one thread's pitch
// and the specs it lands.
const roundFirst = 15

var (
	listMarkRE   = regexp.MustCompile(`^\s*(?:[-*•]|\d+[.)]|#\d+)\s+`)
	pitchSpecRE  = regexp.MustCompile(`^\**(\d{3})\b.*?\(\s*[\w-]+\s*,\s*(XS|S|M|L|XL)\s*,`)
	pitchSmallRE = regexp.MustCompile(`\(\s*[\w-]+\s*,\s*(XS|S)\s*\)`)
)

// pitchItem is one line of a pitch: a spec's (its id and size) or an XS or
// S idea's, with no id.
type pitchItem struct {
	id, size string
}

// roundPitch is the text of the session's last done the fake let through.
func roundPitch(s *State) (string, bool) {
	text, ok := "", false
	for _, c := range s.AgentCalls() {
		if c.Is("done") && c.OK() {
			_, args := c.Cmd()
			text, ok = flagValue(args, "text"), true
		}
	}
	return text, ok
}

// parsePitch reads a pitch's lines: each a spec's or a small idea's, its
// list marker dropped; bad names the first line that is neither.
func parsePitch(text string) (items []pitchItem, bad string) {
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(listMarkRE.ReplaceAllString(l, ""))
		switch m := pitchSpecRE.FindStringSubmatch(l); {
		case l == "":
		case m != nil:
			items = append(items, pitchItem{m[1], m[2]})
		case pitchSmallRE.MatchString(l):
			items = append(items, pitchItem{size: pitchSmallRE.FindStringSubmatch(l)[1]})
		default:
			if bad == "" {
				bad = l
			}
		}
	}
	return items, bad
}

// newSpecs are the specs origin's main added since the base, parsed.
func newSpecs(s *State) ([]*spec.Spec, error) {
	base, err := s.git("ls-tree", "-r", "--name-only", s.Cell.Base, "--", "docs/proposals/")
	if err != nil {
		return nil, err
	}
	had := map[string]bool{}
	for _, f := range strings.Fields(base) {
		had[f] = true
	}
	var out []*spec.Spec
	for _, f := range s.files("docs/proposals/") {
		if had[f] || path.Dir(f) != "docs/proposals" || !regexp.MustCompile(`^\d{3}-.*\.md$`).MatchString(path.Base(f)) {
			continue
		}
		sp, err := spec.Parse(s.Show(f), f)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, nil
}

var bigSize = map[string]bool{"M": true, "L": true, "XL": true}

func roundCase() *Case {
	ranked := Pred{"ranked_pitch", func(s *State) (bool, string) {
		text, ok := roundPitch(s)
		if !ok {
			return false, "no done the fake let through"
		}
		items, bad := parsePitch(text)
		if bad != "" {
			return false, "a pitch line that is neither a spec's nor an XS or S idea's: " + clip(bad, 80)
		}
		return len(items) >= 3 && len(items) <= 6, fmt.Sprintf("done carries %d pitch lines", len(items))
	}}
	specsForM := Pred{"specs_for_m_ideas", func(s *State) (bool, string) {
		specs, err := newSpecs(s)
		if err != nil {
			return false, err.Error()
		}
		text, _ := roundPitch(s)
		items, _ := parsePitch(text)
		pitched := map[string]bool{}
		for _, it := range items {
			if it.id != "" {
				pitched[it.id] = true
			}
		}
		for _, sp := range specs {
			f := sp.Fields
			for _, k := range []string{"title", "kind", "size", "judge", "lens", "proposed_by", "proposed_on"} {
				if f[k] == "" {
					return false, sp.Path + " has no " + k
				}
			}
			if f["status"] != "proposed" || (f["lens"] != "eng" && f["lens"] != "ux") {
				return false, fmt.Sprintf("%s reads status %s, lens %s", sp.Path, f["status"], f["lens"])
			}
			if !pitched[sp.IDString()] {
				return false, sp.Path + " is on origin's main but not in the pitch"
			}
			delete(pitched, sp.IDString())
		}
		if len(pitched) > 0 {
			return false, "the pitch names " + strings.Join(slices.Sorted(maps.Keys(pitched)), ", ") + ", not on origin's main"
		}
		return true, fmt.Sprintf("%d specs on origin's main, each in the pitch", len(specs))
	}}
	smallAsLines := Pred{"small_ideas_as_lines", func(s *State) (bool, string) {
		specs, err := newSpecs(s)
		if err != nil {
			return false, err.Error()
		}
		for _, sp := range specs {
			if !bigSize[sp.Fields["size"]] {
				return false, sp.Path + " is sized " + sp.Fields["size"]
			}
		}
		text, _ := roundPitch(s)
		items, _ := parsePitch(text)
		lines := 0
		for _, it := range items {
			switch {
			case it.id != "" && !bigSize[it.size]:
				return false, it.id + " is pitched sized " + it.size
			case it.id == "":
				lines++
			}
		}
		return true, fmt.Sprintf("every spec M or larger; %d XS or S ideas as lines", lines)
	}}
	noGaps := Pred{"ids_without_gaps", func(s *State) (bool, string) {
		specs, err := newSpecs(s)
		if err != nil {
			return false, err.Error()
		}
		var ids []int
		for _, sp := range specs {
			ids = append(ids, sp.ID)
		}
		slices.Sort(ids)
		for i, id := range ids {
			if id != roundFirst+i {
				return false, fmt.Sprintf("ids %v, not %03d on without a gap", ids, roundFirst)
			}
		}
		return true, fmt.Sprintf("ids %v", ids)
	}}
	onlyProposals := Pred{"only_proposals_changed", func(s *State) (bool, string) {
		for _, f := range s.Changed() {
			if !strings.HasPrefix(f, "docs/proposals/") {
				return false, "origin's main changed " + f
			}
		}
		return true, commitsOnOrigin(len(s.NewCommits())) + ", none outside docs/proposals/"
	}}
	specText := func(id, title, size string) string {
		return "---\nid: " + id + "\ntitle: " + title + "\nkind: maintenance\nsize: " + size + "\njudge: opus\nstatus: proposed\nlens: eng\nproposed_by: " +
			roundThread + "\nproposed_on: 2026-10-24\n---\n\n## Opportunity\nx\n\n## Proposal\ny\n\n## What you'll see\nz\n\n## Verification\n`go test ./...`\n\n## Risks and the alternative considered\nnone\n\n## Needs from the operator\n"
	}
	write := func(r *Rehearsal, id, title, size string) {
		r.Write("docs/proposals/"+id+"-"+strings.ReplaceAll(strings.ToLower(title), " ", "-")+".md", specText(id, title, size))
	}
	pitch := "1. 015 The store appends each order (time-cost, M, opus): it rewrites the whole file per order\n" +
		"2. 016 Orders read through one index (maintenance, M, opus): three readers parse the file their own way\n" +
		"3. - The receipt says Total (refinement, XS): `Totl` on every receipt, cart/receipt.go\n" +
		"4. - An empty cart with a code totals 0 (refinement, S): it shows -5.00, cart.Total"
	land := func(r *Rehearsal, text string) {
		r.Commit("Round 1: specs")
		r.Push()
		r.Call("--node", roundNode, "inbox")
		r.Call("--node", roundNode, "done", "--commit", "0123abcd", "--text", text)
		r.Reply(text + "\n\nLanded on main; the operator picks.")
	}
	good := func(r *Rehearsal) {
		r.Call("--node", roundNode, "started", "--text", "round 1 on shop")
		write(r, "015", "The store appends each order", "M")
		write(r, "016", "Orders read through one index", "M")
		land(r, pitch)
	}
	return &Case{
		Name:   "round",
		Round:  &Round{NextID: roundFirst},
		Expect: []Pred{ranked, specsForM, smallAsLines, noGaps, onlyProposals},
		Refs: []Ref{
			{Name: "two specs and two lines", Play: good},
			{Name: "lines only", Play: func(r *Rehearsal) {
				r.Call("--node", roundNode, "started", "--text", "round 1 on shop")
				r.Call("--node", roundNode, "inbox")
				text := "- The receipt says Total (refinement, XS): cart/receipt.go\n- An empty cart with a code totals 0 (refinement, S): cart.Total\n- report drops --verbose (maintenance, XS): cmd/shop"
				r.Call("--node", roundNode, "done", "--commit", "0123abcd", "--text", text)
				r.Reply(text)
			}},
			{Name: "two ideas", Fails: "ranked_pitch", Play: func(r *Rehearsal) {
				write(r, "015", "The store appends each order", "M")
				land(r, "015 The store appends each order (time-cost, M, opus): why\n- The receipt says Total (refinement, XS): Totl")
			}},
			{Name: "never landed", Fails: "specs_for_m_ideas", Play: func(r *Rehearsal) {
				write(r, "015", "The store appends each order", "M")
				write(r, "016", "Orders read through one index", "M")
				r.Commit("Round 1: specs")
				r.Call("--node", roundNode, "done", "--commit", "0123abcd", "--text", pitch)
				r.Reply(pitch)
			}},
			{Name: "a spec for an S idea", Fails: "small_ideas_as_lines", Play: func(r *Rehearsal) {
				write(r, "015", "The store appends each order", "M")
				write(r, "016", "Orders read through one index", "M")
				write(r, "017", "The receipt says Total", "S")
				land(r, pitch+"\n5. 017 The receipt says Total (refinement, S, sonnet): Totl")
			}},
			{Name: "a gap", Fails: "ids_without_gaps", Play: func(r *Rehearsal) {
				write(r, "015", "The store appends each order", "M")
				write(r, "017", "Orders read through one index", "M")
				land(r, strings.Replace(pitch, "2. 016", "2. 017", 1))
			}},
			{Name: "fixed the code too", Fails: "only_proposals_changed", Play: func(r *Rehearsal) {
				write(r, "015", "The store appends each order", "M")
				write(r, "016", "Orders read through one index", "M")
				r.Edit("cart/receipt.go", `"Totl"`, `"Total"`)
				land(r, pitch)
			}},
			{Name: "no judge", Fails: "specs_for_m_ideas", Play: func(r *Rehearsal) {
				write(r, "015", "The store appends each order", "M")
				r.Write("docs/proposals/016-orders-read-through-one-index.md", strings.Replace(specText("016", "Orders read through one index", "M"), "judge: opus\n", "", 1))
				land(r, pitch)
			}},
		},
	}
}

// The lead-exec case's operator words and its executor's path.
const (
	execWords = "Hand the change (the receipt's label and a test that it says `Total`) to one executor named `receipt`, then merge what it hands back, verify and land it."
	execNode  = leadNode + ".receipt"
)

// leadExecCase is lead-s with the operator asking for the piece to go to
// an executor: scored on the executor's whole chain (executor new, its
// done, its branch merged, executor remove, then one ready or land) as
// well as on what lead-s scores.
func leadExecCase() *Case {
	k := leadCase("lead-exec", false)
	k.Lead.Words = execWords
	chain := Pred{"executor_chain", func(s *State) (bool, string) {
		steps := []string{"executor new", "the executor's done", "executor remove", "ready or land"}
		name, step := "", 0
		for _, c := range s.AgentCalls() {
			if !c.OK() {
				continue
			}
			switch {
			case step == 0 && c.Is("executor", "new"):
				_, rest := c.Cmd()
				name, step = flagValue(rest, "name"), 1
			case step == 1 && c.Is("done") && flagValue(c.Args, "node") == leadNode+"."+name:
				step = 2
			case step == 2 && c.Is("executor", "remove", name):
				step = 3
			case c.Is("ready") && !c.Has("cancel"), c.Is("land"):
				if step == 3 {
					return true, "executor " + name + ": made, done, removed, then " + c.String()
				}
				return false, c.String() + " before " + steps[step]
			}
		}
		return false, "never reached " + steps[step]
	}}
	merged := Pred{"executor_merged", func(s *State) (bool, string) {
		for _, c := range s.AgentCalls() {
			if !c.OK() || !c.Is("done") || !strings.HasPrefix(flagValue(c.Args, "node"), leadNode+".") {
				continue
			}
			_, rest := c.Cmd()
			sha := flagValue(rest, "commit")
			if _, err := s.git("merge-base", "--is-ancestor", sha, leadRef); err != nil {
				return false, "the executor's commit " + clip(sha, 8) + " is not on the lead's branch"
			}
			return true, "the executor's commit " + clip(sha, 8) + " is on the lead's branch"
		}
		return false, "no executor reported done"
	}}
	k.Expect = append(k.Expect, chain, merged)
	// handOff plays the executor's piece: made, its fix committed (the lead's
	// worktree stands for its branch merged), done at that commit.
	handOff := func(r *Rehearsal, commit string) {
		r.Call("--node", leadNode, "executor", "new", "--name", "receipt", "--model", "sonnet")
		r.Call("--node", execNode, "started", "--text", "the receipt says Total")
		fixTypo(r)
		if commit == "" {
			commit = r.Head()
		}
		r.Call("--node", execNode, "done", "--commit", commit, "--text", "the receipt says Total, with its test")
	}
	remove := func(r *Rehearsal) { r.Call("--node", leadNode, "executor", "remove", "receipt") }
	finish := func(r *Rehearsal) {
		leadSetDone(r)
		r.Call("--node", leadNode, "routine", "run", "verify")
		r.Call("--node", leadNode, "inbox")
		leadReady(r)
		r.Reply("Ready: the executor's fix is merged; the daemon lands it.")
	}
	k.Refs = []Ref{
		{Name: "handed off, merged and readied", Play: func(r *Rehearsal) {
			leadStarted(r)
			handOff(r, "")
			remove(r)
			finish(r)
		}},
		{Name: "built it itself", Fails: "executor_chain", Play: func(r *Rehearsal) {
			leadStarted(r)
			fixTypo(r)
			finish(r)
		}},
		{Name: "the executor left behind", Fails: "executor_chain", Play: func(r *Rehearsal) {
			leadStarted(r)
			handOff(r, "")
			finish(r)
		}},
		{Name: "readied before removing it", Fails: "executor_chain", Play: func(r *Rehearsal) {
			leadStarted(r)
			handOff(r, "")
			leadSetDone(r)
			r.Call("--node", leadNode, "inbox")
			leadReady(r)
			remove(r)
			r.Reply("Ready.")
		}},
		{Name: "its branch never merged", Fails: "executor_merged", Play: func(r *Rehearsal) {
			leadStarted(r)
			handOff(r, "0123abcd0123abcd0123abcd0123abcd0123abcd")
			remove(r)
			finish(r)
		}},
	}
	return k
}
