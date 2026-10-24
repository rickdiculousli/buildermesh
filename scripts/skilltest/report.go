package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// PredResult is one expectation as a cell scored on it.
type PredResult struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	Why  string `json:"why"`
}

// Row is one cell's line in results.json.
type Row struct {
	Case          string       `json:"case"`
	Arm           string       `json:"arm"`
	Run           int          `json:"run"`
	Pass          bool         `json:"pass"`
	Predicates    []PredResult `json:"predicates"`
	Steers        int          `json:"steers"`
	OperatorTurns int          `json:"operator_turns"`
	Commits       int          `json:"commits"`     // on origin's main
	BmeshCalls    int          `json:"bmesh_calls"` // the session's, the session hook's left out
	Commands      []string     `json:"commands"`    // the session's bmesh calls, as command lines
	Blocked       []string     `json:"blocked,omitempty"`
	PluginHook    bool         `json:"plugin_hook"`      // the plugin's session hook called the fake
	Plugins       []string     `json:"plugins"`          // the plugins the session had, from its first turn's init event
	MCPServers    []string     `json:"mcp_servers"`      // the MCP servers it had: none, when isolated
	Reaching      []string     `json:"reaching_tools"`   // the tools it had that reach past the sandbox: none, when isolated
	Available     []string     `json:"skills_available"` // the buildermesh skills it had
	Skills        []string     `json:"skills"`           // the skills it invoked
	InputTokens   int64        `json:"input_tokens"`
	OutputTokens  int64        `json:"output_tokens"`
	CacheRead     int64        `json:"cache_read_tokens"`
	CacheCreate   int64        `json:"cache_creation_tokens"`
	Tokens        int64        `json:"tokens"`
	CostUSD       float64      `json:"cost_usd"`
	DurationMS    int64        `json:"duration_ms"` // the CLI's, summed over turns
	WallMS        int64        `json:"wall_ms"`
	Turns         int          `json:"turns"` // the CLI's num_turns, summed
	Error         string       `json:"error,omitempty"`
}

// scoreCell scores the cell in dir on its case's expectations.
func scoreCell(dir string, k *Case) (Row, error) {
	s, err := loadState(dir)
	if err != nil {
		return Row{}, err
	}
	defer s.Close()
	return score(s, k), nil
}

func score(s *State, k *Case) Row {
	c := s.Cell
	r := Row{Case: c.Case, Arm: c.Arm, Run: c.Run, Steers: c.Steers, OperatorTurns: len(c.Turns), Error: c.Error,
		Commits: len(s.NewCommits()), Commands: []string{}, Skills: append([]string{}, s.Skills()...)}
	r.Pass = c.Error == "" // a turn that failed proves nothing, whatever the state it left
	for _, p := range k.Expect {
		ok, why := p.Check(s)
		r.Predicates = append(r.Predicates, PredResult{p.Name, ok, why})
		r.Pass = r.Pass && ok
	}
	for _, call := range s.Calls {
		if hookCall(call) {
			r.PluginHook = true
			continue
		}
		r.BmeshCalls++
		r.Commands = append(r.Commands, call.String())
	}
	if b, err := os.ReadFile(filepath.Join(s.Dir, blockedLog)); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if f := strings.SplitN(line, "\t", 2); len(f) == 2 {
				r.Blocked = append(r.Blocked, strings.ReplaceAll(f[1], "\t", " "))
			}
		}
	}
	var spent Spend
	for i, res := range s.Results {
		var prev *Result
		if i > 0 && res.SessionID != "" && s.Results[i-1].SessionID == res.SessionID {
			prev = &s.Results[i-1]
		}
		spent = spent.plus(turnSpend(res, prev))
		r.DurationMS += res.DurationMS
		r.Turns += res.NumTurns
		r.WallMS += c.Turns[i].WallMS
	}
	r.InputTokens, r.OutputTokens, r.CacheRead, r.CacheCreate, r.CostUSD = spent.Input, spent.Output, spent.CacheRead, spent.CacheCreate, spent.CostUSD
	r.Tokens = spent.Tokens()
	r.Plugins, r.MCPServers, r.Available, r.Reaching = []string{}, []string{}, []string{}, []string{}
	for _, in := range s.Inits {
		if in == nil {
			continue
		}
		r.Plugins, r.MCPServers = names(in.Plugins), names(in.MCPServers)
		for _, tool := range names(in.Tools) {
			if slices.Contains(deniedTools, tool) {
				r.Reaching = append(r.Reaching, tool)
			}
		}
		for _, sk := range names(in.Skills) {
			if strings.HasPrefix(sk, "buildermesh:") {
				r.Available = append(r.Available, sk)
			}
		}
		break
	}
	return r
}

// Results is results.json: the run and one row per cell.
type Results struct {
	Date     string   `json:"date"`
	Model    string   `json:"model"`
	Claude   string   `json:"claude"`
	Host     string   `json:"host"`
	Arms     []Arm    `json:"arms"`
	Cases    []string `json:"cases"`
	Runs     int      `json:"runs"`
	Rescored bool     `json:"rescored,omitempty"`
	Rows     []Row    `json:"rows"`
}

func sortRows(rows []Row) {
	order := map[string]int{}
	for i, k := range cases {
		order[k.Name] = i
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Case != b.Case {
			return order[a.Case] < order[b.Case]
		}
		if a.Arm != b.Arm {
			return a.Arm < b.Arm
		}
		return a.Run < b.Run
	})
}

func writeResults(dir string, res Results) error {
	sortRows(res.Rows)
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "results.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "summary.md"), []byte(summary(res)), 0o644)
}

type tally struct {
	n, pass, steers, commits, turns int
	cost                            float64
	ms, tokens                      int64
}

func (t *tally) add(r Row) {
	t.n++
	if r.Pass {
		t.pass++
	}
	t.steers += r.Steers
	t.commits += r.Commits
	t.turns += r.Turns
	t.cost += r.CostUSD
	t.ms += r.WallMS
	t.tokens += r.Tokens
}

// summary is summary.md: a row per case and arm, the totals per arm, then
// every expectation a cell missed.
func summary(res Results) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# skilltest %s\n\n", res.Date)
	var arms []string
	for _, a := range res.Arms {
		src := a.Source
		if a.Commit != "" {
			src += " at " + a.Commit[:min(12, len(a.Commit))]
		}
		arms = append(arms, fmt.Sprintf("%s (%s)", a.Name, src))
	}
	fmt.Fprintf(&b, "Model %s, %s, on %s. Arms: %s.", res.Model, res.Claude, res.Host, strings.Join(arms, ", "))
	if res.Rescored {
		b.WriteString(" Rescored from the kept cells.")
	}
	b.WriteString("\n\nPass is every expectation held; steers are the go-aheads the scripted operator had to send; commits are on the sandbox origin's main; cost and tokens are what the CLI stated for each turn; time is the turns' wall clock.\n\n")
	b.WriteString(flips(res))
	b.WriteString("| case | arm | runs | pass | steers | commits | cost | time | tokens | turns |\n|---|---|--:|--:|--:|--:|--:|--:|--:|--:|\n")
	type key struct{ k, a string }
	groups := map[key]*tally{}
	var keys []key
	perArm := map[string]*tally{}
	var armOrder []string
	for _, r := range res.Rows {
		kk := key{r.Case, r.Arm}
		if groups[kk] == nil {
			groups[kk] = &tally{}
			keys = append(keys, kk)
		}
		groups[kk].add(r)
		if perArm[r.Arm] == nil {
			perArm[r.Arm] = &tally{}
			armOrder = append(armOrder, r.Arm)
		}
		perArm[r.Arm].add(r)
	}
	for _, kk := range keys {
		t := groups[kk]
		fmt.Fprintf(&b, "| %s | %s | %d | %d/%d | %d | %d | $%.2f | %.0fs | %s | %d |\n", kk.k, kk.a, t.n, t.pass, t.n, t.steers, t.commits, t.cost, float64(t.ms)/1000, kilo(t.tokens), t.turns)
	}
	sort.Strings(armOrder)
	b.WriteString("\n## Per arm\n\n| arm | cells | pass rate | steers | commits | cost | time |\n|---|--:|--:|--:|--:|--:|--:|\n")
	for _, a := range armOrder {
		t := perArm[a]
		fmt.Fprintf(&b, "| %s | %d | %.0f%% | %d | %d | $%.2f | %.0fs |\n", a, t.n, 100*float64(t.pass)/float64(max(1, t.n)), t.steers, t.commits, t.cost, float64(t.ms)/1000)
	}
	if named := plantedLines(res.Rows); len(named) > 0 {
		b.WriteString("\n## Planted defects\n\n" + strings.Join(named, "\n") + "\n")
	}
	var missed []string
	for _, r := range res.Rows {
		for _, p := range r.Predicates {
			if !p.OK {
				missed = append(missed, fmt.Sprintf("- %s / %s #%d: %s: %s", r.Case, r.Arm, r.Run, p.Name, p.Why))
			}
		}
		if r.Error != "" {
			missed = append(missed, fmt.Sprintf("- %s / %s #%d: error: %s", r.Case, r.Arm, r.Run, r.Error))
		}
	}
	if len(missed) > 0 {
		b.WriteString("\n## Missed\n\n" + strings.Join(missed, "\n") + "\n")
	}
	return b.String()
}

func kilo(n int64) string {
	if n >= 1000 {
		return fmt.Sprintf("%.0fk", float64(n)/1000)
	}
	return fmt.Sprint(n)
}

// armNames are the arms in the order the run named them, then any a row
// names that the run's list lacks (a rescore with no results.json).
func armNames(res Results) []string {
	var out []string
	for _, a := range res.Arms {
		out = append(out, a.Name)
	}
	var extra []string
	for _, r := range res.Rows {
		if !slices.Contains(out, r.Arm) && !slices.Contains(extra, r.Arm) {
			extra = append(extra, r.Arm)
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}

// flips is summary.md's opening section when the run has two arms or
// more: each case whose pass differs between the arms on the same run, run
// by run, so a comparison is read by which cases flipped before its totals.
func flips(res Results) string {
	arms := armNames(res)
	if len(arms) < 2 {
		return ""
	}
	type key struct {
		k   string
		run int
	}
	rows := map[key]map[string]Row{}
	var keys []key
	for _, r := range res.Rows {
		kk := key{r.Case, r.Run}
		if rows[kk] == nil {
			rows[kk] = map[string]Row{}
			keys = append(keys, kk)
		}
		rows[kk][r.Arm] = r
	}
	var lines []string
	for _, kk := range keys {
		var words []string
		seen := map[bool]bool{}
		for _, a := range arms {
			// A cell that never ran or errored proves nothing either way.
			r, ok := rows[kk][a]
			switch {
			case !ok:
				words = append(words, a+" not run")
			case r.Error != "":
				words = append(words, a+" error")
			case r.Pass:
				words = append(words, a+" pass")
			default:
				words = append(words, a+" fail")
			}
			if ok && r.Error == "" {
				seen[r.Pass] = true
			}
		}
		if len(seen) == 2 {
			lines = append(lines, fmt.Sprintf("- %s run %d: %s", kk.k, kk.run, strings.Join(words, ", ")))
		}
	}
	if len(lines) == 0 {
		return "## Flipped\n\nNo case flipped between the arms.\n\n"
	}
	return "## Flipped\n\n" + strings.Join(lines, "\n") + "\n\n"
}

// plantedPrefix names a review case's expectations, one per planted defect.
const plantedPrefix = "caught_"

// planted are the planted defects a row's reply named and missed, by name.
func planted(r Row) (named, missed []string) {
	for _, p := range r.Predicates {
		id, ok := strings.CutPrefix(p.Name, plantedPrefix)
		switch {
		case !ok:
		case p.OK:
			named = append(named, id)
		default:
			missed = append(missed, id)
		}
	}
	return named, missed
}

// plantedLines are a line per row of a case with planted defects: which
// ones its reply named and which it missed.
func plantedLines(rows []Row) []string {
	var out []string
	for _, r := range rows {
		named, missed := planted(r)
		if len(named)+len(missed) == 0 {
			continue
		}
		out = append(out, fmt.Sprintf("- %s / %s #%d: named %d of %d [%s]; missed [%s]", r.Case, r.Arm, r.Run,
			len(named), len(named)+len(missed), strings.Join(named, " "), strings.Join(missed, " ")))
	}
	return out
}
