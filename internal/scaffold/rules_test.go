package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

func TestApplyRulesAppendsCoreWhenAbsent(t *testing.T) {
	out, customized, err := ApplyRules("# A\n\n<!-- bmesh:mesh -->\nM\n<!-- /bmesh:mesh -->\n")
	if err != nil || customized {
		t.Fatal(err, customized)
	}
	body := RulesBody([]string{"core"})
	want := "# A\n\n<!-- bmesh:mesh -->\nM\n<!-- /bmesh:mesh -->\n\n<!-- bmesh:rules core " + RulesHash(body) + " -->\n" + body + "\n<!-- /bmesh:rules -->\n"
	if out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	if !strings.HasPrefix(body, "## Engineering rules\nFrom bmesh's rule set `core`. Where the rest of this file says otherwise, the rest wins.") ||
		!strings.Contains(body, "\n### core\n- Make the smallest change") {
		t.Fatal(body)
	}
	if again, _, _ := ApplyRules(out); again != out {
		t.Fatalf("a second run changed it:\n%s", again)
	}
}

func TestApplyRulesRendersThePickInOrder(t *testing.T) {
	src := "intro\n<!-- bmesh:rules tests core design -->\n<!-- /bmesh:rules -->\ntail\n"
	out, customized, err := ApplyRules(src)
	if err != nil || customized {
		t.Fatal(err, customized)
	}
	body := RulesBody([]string{"tests", "core", "design"})
	if !strings.HasPrefix(out, "intro\n<!-- bmesh:rules tests core design "+RulesHash(body)+" -->\n"+body+"\n<!-- /bmesh:rules -->\ntail\n") {
		t.Fatal(out)
	}
	if !strings.Contains(body, "From bmesh's rule sets `tests`, `core`, `design`.") {
		t.Fatal(body)
	}
	if i, j, k := strings.Index(body, "### tests"), strings.Index(body, "### core"), strings.Index(body, "### design"); i < 0 || i > j || j > k {
		t.Fatal(body)
	}
	if again, _, _ := ApplyRules(out); again != out {
		t.Fatal("a second run changed it")
	}
	// A re-pick on the marker is rendered on the next run.
	repick := strings.Replace(out, "bmesh:rules tests core design", "bmesh:rules core tests tdd", 1)
	got, _, _ := ApplyRules(repick)
	if !strings.Contains(got, "### tdd\n- Write the failing test first") || strings.Contains(got, "### design") {
		t.Fatal(got)
	}
}

func TestApplyRulesKeepsAnEdit(t *testing.T) {
	once, _, _ := ApplyRules("# A\n")
	edited := strings.Replace(once, "Make the smallest change that solves the problem.", "Keep diffs small.", 1)
	out, customized, err := ApplyRules(edited)
	if err != nil || !customized {
		t.Fatal(err, customized)
	}
	if !strings.Contains(out, "<!-- bmesh:rules customized -->\n## Engineering rules") || !strings.Contains(out, "- Keep diffs small.") ||
		strings.Contains(out, "smallest change") {
		t.Fatal(out)
	}
	// A customized block is never rewritten, whatever it holds.
	again, customized, err := ApplyRules(out)
	if err != nil || !customized || again != out {
		t.Fatalf("%v %v\n%s", err, customized, again)
	}
	odd := "<!-- bmesh:rules customized -->\nmine\n<!-- /bmesh:rules -->\n"
	if got, _, _ := ApplyRules(odd); got != odd {
		t.Fatal(got)
	}
	// Set names back on the marker return it to bmesh's text.
	back, customized, _ := ApplyRules(strings.Replace(out, "bmesh:rules customized", "bmesh:rules core", 1))
	if customized || back != once {
		t.Fatalf("got\n%s\nwant\n%s", back, once)
	}
	// So does deleting the block.
	if gone, _, _ := ApplyRules("# A\n"); gone != once {
		t.Fatal(gone)
	}
}

func TestApplyRulesReadsCRLFAsUnedited(t *testing.T) {
	once, _, _ := ApplyRules("# A\n")
	if _, customized, _ := ApplyRules(strings.ReplaceAll(once, "\n", "\r\n")); customized {
		t.Fatal("a CRLF checkout reads as an edit")
	}
}

func TestApplyRulesEmptyMarkerStaysOff(t *testing.T) {
	src := "# A\n\n<!-- bmesh:rules -->\n<!-- /bmesh:rules -->\n"
	out, customized, err := ApplyRules(src)
	if err != nil || customized || out != src {
		t.Fatalf("%v %v\n%s", err, customized, out)
	}
	// A body left under an empty marker is dropped.
	if out, _, _ := ApplyRules("<!-- bmesh:rules -->\n## Engineering rules\nx\n<!-- /bmesh:rules -->\n"); out != "<!-- bmesh:rules -->\n<!-- /bmesh:rules -->\n" {
		t.Fatal(out)
	}
}

func TestApplyRulesRefusesAnUnknownSet(t *testing.T) {
	_, _, err := ApplyRules("<!-- bmesh:rules core style -->\n<!-- /bmesh:rules -->\n")
	if err == nil || err.Error() != "AGENTS.md: unknown rule set style; bmesh has core, design, tests, tdd, prose, ponytail" {
		t.Fatal(err)
	}
}

// Every set is embedded, and none holds a heading: one would end its
// "### <name>" section in AGENTS.md early.
func TestEveryRuleSetIsEmbedded(t *testing.T) {
	for _, n := range RuleSets {
		text, ok := RuleSet(n)
		if !ok || text == "" {
			t.Errorf("%s: %v %q", n, ok, text)
		}
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "#") {
				t.Errorf("%s holds a heading: %q", n, line)
			}
		}
	}
	if _, ok := RuleSet("rules"); ok {
		t.Fatal("an unshipped name is a set")
	}
}

func TestTargetsNoteACustomizedBlock(t *testing.T) {
	repo := t.TempDir()
	row := config.Repo{Name: "pb", Mode: "ship", Verify: []string{"npm test"}}
	targets, _, err := Targets(repo, row, snippet)
	if err != nil || targets[0].Note != "" {
		t.Fatal(err, targets[0].Note)
	}
	if !strings.HasSuffix(targets[0].Want, "<!-- /bmesh:mesh -->\n\n"+rulesBlock(DefaultRules)+"\n") {
		t.Fatal(targets[0].Want)
	}
	if _, err := Write(repo, targets); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(repo, Agents)
	b, _ := os.ReadFile(p)
	writeTest(t, p, strings.Replace(string(b), "Delete what's dead before adding.", "", 1))
	targets, _, err = Targets(repo, row, snippet)
	if err != nil || targets[0].Note != RulesNote || !strings.Contains(targets[0].Want, "<!-- bmesh:rules customized -->") {
		t.Fatal(err, targets[0].Note)
	}
	writeTest(t, p, string(b)+"\n<!-- bmesh:rules nope -->\n<!-- /bmesh:rules -->\n")
	if _, _, err := Targets(repo, row, snippet); err != nil {
		t.Fatal("only the first rules block is read:", err)
	}
	writeTest(t, p, strings.Replace(string(b), "bmesh:rules core", "bmesh:rules nope", 1))
	if _, _, err := Targets(repo, row, snippet); err == nil || !strings.Contains(err.Error(), "unknown rule set nope") {
		t.Fatal(err)
	}
}

func TestDriftLeavesTheRulesBlockOut(t *testing.T) {
	repo := t.TempDir()
	row := config.Repo{Name: "pb", Mode: "ship", Verify: []string{"npm test"}}
	targets, _, err := Targets(repo, row, snippet)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(repo, targets); err != nil {
		t.Fatal(err)
	}
	agents, _ := os.ReadFile(filepath.Join(repo, Agents))
	at := func(rel string) (string, bool, error) {
		if rel == Agents {
			return string(agents), true, nil
		}
		return "", false, nil
	}
	p := filepath.Join(repo, Agents)
	for name, text := range map[string]string{
		"edited":    strings.Replace(string(agents), "Delete what's dead before adding.", "Keep diffs small.", 1),
		"re-picked": strings.Replace(string(agents), "bmesh:rules core", "bmesh:rules core tests", 1),
		"emptied":   rulesRE.ReplaceAllLiteralString(string(agents), "<!-- bmesh:rules -->\n<!-- /bmesh:rules -->"),
		"dropped":   rulesRE.ReplaceAllLiteralString(string(agents), ""),
	} {
		writeTest(t, p, text)
		if d, ok, err := Drift(repo, row, snippet, true, at); d != "" || !ok || err != nil {
			t.Errorf("%s rules: %q %v %v", name, d, ok, err)
		}
	}
	// A changed done block is still drift.
	writeTest(t, p, strings.Replace(string(agents), "`npm test`", "`npm test`, `npm run lint`", 1))
	if d, _, _ := Drift(repo, row, snippet, true, at); !strings.Contains(d, "--- a/AGENTS.md") {
		t.Fatalf("a changed done block: %q", d)
	}
}

// The manual's Rule sets section is where the operator reads the sets: it
// holds each one's exact text.
func TestTheManualHoldsEveryRuleSet(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "manual.md"))
	if err != nil {
		t.Fatal(err)
	}
	manual := string(b)
	for _, n := range RuleSets {
		text, _ := RuleSet(n)
		if !strings.Contains(manual, "**`"+n+"`**") || !strings.Contains(manual, "\n"+text+"\n") {
			t.Errorf("docs/manual.md lacks the %s set as embedded", n)
		}
	}
}
