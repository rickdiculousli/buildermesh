package main

import (
	"encoding/json"
	"fmt"
	"os"
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
	Expect []Pred
	Refs   []Ref
}

// Lead makes a case a lead's: the session is lead Node building spec Spec,
// in a worktree of the sandbox on its own branch, and its one turn is the
// build brief this tree's internal/brief writes. With StopFirst the fake's
// first ready or land refuses because the spec doesn't read done.
type Lead struct {
	Node, Spec string
	StopFirst  bool
}

// worktree is the lead's worktree, relative to the cell, and branch its
// branch, as bmesh lead start names them.
func (l *Lead) worktree() string { return filepath.Join("worktrees", repoName, l.Node) }
func (l *Lead) branch() string   { return "bmesh/" + l.Node }

// brief is the build brief for the lead's spec in the sandbox, as bmesh
// build writes it for a repo with no setup command.
func (l *Lead) brief(sandbox, host string, now time.Time) (string, error) {
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
	text, err := brief.Render(sandbox, "build", values)
	if err != nil {
		return "", err
	}
	return brief.WithSetup(text, brief.SetupStep("", "")), nil
}

const (
	goAhead   = "yes, go ahead"
	maxSteers = 2
)

var cases = []*Case{bugCase(), tweakCase(), questionCase(), qaCase(), featureMCase(), featureLCase(),
	inFlightCase(), ideasCase(), pickCase(), releaseCase(), leadCase("lead-s", false), leadCase("lead-stop", true)}

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
			noSpecAdded(), nothingStarted()},
		Refs: []Ref{
			{Name: "fixed and pushed", Play: func(r *Rehearsal) {
				r.Call("repos", "show", "shop")
				fixTotal(r)
				r.Call("routine", "run", "verify")
				r.Push()
				r.Reply("Fixed: an empty cart with SAVE5 totals 0.00. Pushed; origin/main contains it.")
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
		Expect: []Pred{landed(), testsPass(), debug, gone, readme, noSpecAdded(), nothingStarted()},
		Refs: []Ref{
			{Name: "renamed", Play: func(r *Rehearsal) {
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
		Expect: []Pred{noCommit(), nothingStarted(), replyNonEmpty()},
		Refs: []Ref{
			{Name: "answered", Play: func(r *Rehearsal) {
				r.Reply("orders.json is one JSON array, and a JSON array can't be appended to in place, so store.Add reads every order, appends the new one and writes the whole file to orders.json.tmp, then renames it over the old one: a crash leaves the old file or the new, never half of one.")
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
			hidden("bug_left", "cart", "TestSkilltestHiddenEmptyCartStillNegative")},
		Refs: []Ref{
			{Name: "held, then fixed 2", Play: func(r *Rehearsal) {
				r.Reply("Holding: I'll keep a list and change nothing until you say go.")
				r.Reply("1 noted: Total subtracts the discount from a subtotal of 0; fix: never below 0 (XS).")
				r.Reply("2 noted: the receipt's label is spelled Totl; fix: Total (XS).")
				fixTypo(r)
				r.Push()
				r.Reply("Fixed 2: the receipt says Total. Pushed. 1 stays on the list.")
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
		Expect: []Pred{landed(), testsPass(), testAdded("csv_test_added", "csv"), csvExport()},
		Refs: []Ref{
			{Name: "built and pushed", Play: func(r *Rehearsal) {
				addCSV(r, "csv", true)
				r.Reply("shop report --csv prints the orders as CSV. Reviewed, verified and pushed.")
			}},
			{Name: "a flag named its own way", Play: func(r *Rehearsal) {
				addCSV(r, "spreadsheet", true)
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
		Expect: []Pred{noCommit(), nothingStarted(), planInReply("sqlite"), asksGoAhead()},
		Refs: []Ref{
			{Name: "a plan, then a question", Play: func(r *Rehearsal) {
				r.Reply("This is L: orders.json is the format the bookkeeping import reads, so changing the store changes an interface others depend on.\n\n" +
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
		Expect: []Pred{noCommit(), noBuild, names},
		Refs: []Ref{
			{Name: "said so", Play: func(r *Rehearsal) {
				r.Call("mesh-status")
				r.Reply("That's being built: spec 012, CSV export for reports, has lead shop-012 working on it now. Nothing started or changed here.")
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
		Expect: []Pred{noCommit(), roundOrList},
		Refs: []Ref{
			{Name: "a round", Play: func(r *Rehearsal) {
				r.Call("ideate", "shop")
				r.Call("watch", "--nodes", "shop-round-1", "--timeout", "30m")
				r.Reply("Round 1 on shop is proposed: 015 report --since, 016 typo hints at checkout, 017 a store file per day.")
			}},
			{Name: "a list", Play: func(r *Rehearsal) {
				r.Reply("What I'd improve:\n\n1. Discounts never take a total below zero.\n2. The receipt's Totl typo.\n3. Two checkouts at once lose an order.\n\nPick any and I'll make it.")
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
			onlyTouches("no_other_commit", "docs/proposals/013-*")},
		Refs: []Ref{
			{Name: "accepted, then built on a go", Play: func(r *Rehearsal) {
				r.Call("choose-ideas", "shop", reply)
				r.Reply("012 and 014 accepted, 013 rejected. Build the accepted two?")
				r.Call("build", "--accepted", "shop")
				r.Reply("Started shop-012 and shop-014 in family shop-build-1.")
			}},
			{Name: "built by id", Play: func(r *Rehearsal) {
				r.Call("build", "shop", "012", "014")
				r.Call("spec", "set", "--id", "013", "--status", "rejected", "--field", "reason=it duplicates the --verbose flag")
				r.Reply("Building 012 and 014; 013 is rejected: it duplicates --verbose.")
			}},
			{Name: "built by id, the decline landed by spec set", Play: func(r *Rehearsal) {
				r.Call("build", "shop", "012", "014")
				r.Call("spec", "set", "--repo", "shop", "--id", "013", "--status", "rejected", "--field", "reason=it duplicates the --verbose flag", "--land")
				r.Reply("Building 012 and 014; 013 is declined on main: it duplicates --verbose.")
			}},
			{Name: "flags, one build each", Play: func(r *Rehearsal) {
				r.Call("choose-ideas", "shop", "--accept", "012,014", "--reject", "013=it duplicates the --verbose flag")
				r.Call("build", "shop", "012")
				r.Call("build", "shop", "014", "--family", "shop-build-1")
				r.Reply("Building 012 and 014; 013 rejected.")
			}},
			{Name: "the decline landed", Play: func(r *Rehearsal) {
				r.Edit("docs/proposals/013-report-details-lists-each-orders-items.md", "status: proposed", "status: rejected\nreason: it duplicates the --verbose flag")
				r.Commit("Reject 013: it duplicates the --verbose flag")
				r.Push()
				r.Call("build", "shop", "012", "014")
				r.Reply("Building 012 and 014; 013 is rejected on main.")
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
				r.Reply("Building 012 and 014; 013 rejected.")
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
// order: ready (not --cancel), land and done.
func leadCalls(s *State) []string {
	var out []string
	for _, c := range s.AgentCalls() {
		switch {
		case c.Is("ready") && !c.Has("cancel"):
			out = append(out, "ready")
		case c.Is("land"), c.Is("done"):
			cmd, _ := c.Cmd()
			out = append(out, cmd)
		}
	}
	return out
}

// landedByFake are the commits the fake's land pushed to origin's main.
func landedByFake(s *State) []string {
	b, _ := os.ReadFile(filepath.Join(s.Dir, "state", "landed"))
	return strings.Fields(string(b))
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
		calls := leadCalls(s)
		seen := strings.Join(calls, ", ")
		if stopFirst {
			if len(calls) == 0 || calls[0] == "done" {
				return false, "no ready or land for the fake to refuse: " + seen
			}
			calls = calls[1:]
		}
		ok := slices.Equal(calls, []string{"ready"}) || slices.Equal(calls, []string{"land", "done"})
		return ok, "ready, land and done in order: [" + seen + "]"
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
		r.Call("--node", leadNode, "started", "--text", "012: the receipt says Total")
		fixTypo(r)
	}
	setDone := func(r *Rehearsal) {
		r.Call("--node", leadNode, "spec", "set", "--id", "012", "--status", "done")
		r.Edit(leadSpecFile, "status: proposed", "status: done")
		r.Commit("012: done")
	}
	ready := func(r *Rehearsal) { r.Call("--node", leadNode, "ready", "--text", "012: the receipt says Total") }
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
				r.Call("--node", leadNode, "land")
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
			r.Call("--node", leadNode, "inbox")
			ready(r)
			setDone(r)
			ready(r)
			r.Reply("Ready, after setting 012 done as the stop said.")
		}},
		{Name: "landed after the stop", Play: func(r *Rehearsal) {
			fix(r)
			setDone(r)
			r.Call("--node", leadNode, "inbox")
			r.Call("--node", leadNode, "land")
			r.Call("--node", leadNode, "spec", "set", "--id", "012", "--status", "done")
			r.Call("--node", leadNode, "land")
			r.Call("--node", leadNode, "done", "--commit", "0123abcd")
			r.Reply("Landed 0123abcd.")
		}},
		{Name: "stopped at the stop", Fails: "lands_once", Play: func(r *Rehearsal) {
			fix(r)
			setDone(r)
			r.Call("--node", leadNode, "inbox")
			ready(r)
			r.Reply("ready refused: the spec doesn't read done. It does on my branch; what should I do?")
		}},
		{Name: "asked about the stop", Fails: "no_ask_or_blocked", Play: func(r *Rehearsal) {
			fix(r)
			r.Call("--node", leadNode, "inbox")
			ready(r)
			r.Call("--node", leadNode, "ask", "--text", "ready says 012 doesn't read done; set it?")
			setDone(r)
			ready(r)
			r.Reply("Ready.")
		}},
	}
	return k
}
