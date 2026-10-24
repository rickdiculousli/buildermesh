package brief

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

var pb = config.Repo{Name: "pb", Path: "x/pb", Mode: "ship", Remote: "example/pb", Verify: []string{"npm test"},
	Surfaces: []string{"phone / at 390x844"}}

func proposalValues(nextID string) map[string]string {
	return partValues(nextID, RoundAll, pb)
}

func partValues(nextID string, part RoundPart, r config.Repo) map[string]string {
	v := RowValues(r, "/home/x/repos/pb")
	for k, s := range map[string]string{"date": "2026-10-15", "next_id": nextID, "thread_id": "5d0c7a1e-0000-4000-8000-000000000001",
		"node": "pb-round-1.eng", "round": "pb-round-1", "round_n": "1", "worktree": "/home/x/.bmesh/worktrees/pb/pb-round-1-eng",
		"branch": "pb-round-1-eng", "operator_context": ""} {
		v[k] = s
	}
	maps.Copy(v, RoundValues(part, r))
	return v
}

func TestNames(t *testing.T) {
	if got := strings.Join(Names(), ","); got != "build,engineer,fix,half-pager" {
		t.Fatal(got)
	}
}

func TestEngineerBriefCarriesTheRepoFacts(t *testing.T) {
	out, err := Render("", "engineer", proposalValues("007"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"`npm test`", "number yours from 007", "/home/x/repos/pb", "> When it works, finish it: run the repo's verify commands (`npm test`)",
		"`proposed_by`: `5d0c7a1e-0000-4000-8000-000000000001`", "--node pb-round-1.eng started", "cd /home/x/.bmesh/worktrees/pb/pb-round-1-eng &&",
		"`buildermesh:ideate-engineer`", "bmesh spec template", "waits on the operator"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q", s)
		}
	}
	if strings.Contains(out, "{{") || strings.Contains(out, "Rick") || strings.Contains(out, "command-center") {
		t.Fatalf("%s", out)
	}
}

// The round's one thread screenshots the row's surfaces and ranks both
// lenses in one list; a row without surfaces gets a brief that never
// mentions them.
func TestRoundBriefWithAndWithoutSurfaces(t *testing.T) {
	out, err := Render("", "engineer", proposalValues("012"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"# Engineer: proposal round 1", "- Surfaces to screenshot, each at its stated viewport: phone / at 390x844.",
		"in how its app looks and feels", "three to six ranked ideas in one list", "this round's only thread", "`buildermesh:ideate-engineer`"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q", s)
		}
	}
	bare := config.Repo{Name: "shop", Verify: []string{"go test ./..."}}
	out, err = Render("", "engineer", partValues("012", RoundAll, bare))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "three to six ranked ideas in one list") {
		t.Errorf("bare brief:\n%s", out)
	}
	for _, s := range []string{"urface", "creenshot", "UI/UX", "looks and feels", "a gap is expected", "{{"} {
		if strings.Contains(out, s) {
			t.Errorf("bare brief has %q", s)
		}
	}
	none := RowValues(config.Repo{Name: "bare"}, "/r")
	if !strings.Contains(none["surfaces"], "none are listed") || !strings.Contains(none["verify"], "none are listed") {
		t.Fatal(none)
	}
}

// A round split across two boxes: the engineer leaves the surfaces to the
// UI/UX engineer on ux_host, which runs only the UI/UX part.
func TestRoundBriefsOfASplitRound(t *testing.T) {
	split := pb
	split.UXHost = "mac"
	eng, err := Render("", "engineer", partValues("012", RoundEng, split))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"# Engineer:", "improve this repo's code and bring back three to five", "The UI/UX engineer runs beside you on `mac`",
		"The UI/UX engineer has a block of its own, so a gap is expected."} {
		if !strings.Contains(eng, s) {
			t.Errorf("eng brief lacks %q", s)
		}
	}
	if strings.Contains(eng, "Surfaces to screenshot") {
		t.Errorf("eng brief lists surfaces")
	}
	ux, err := Render("", "engineer", partValues("017", RoundUX, split))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"# UI/UX engineer: proposal round 1", "You are the UI/UX engineer on `pb`", "You run only the UI/UX part of the method",
		"- Surfaces to screenshot, each at its stated viewport: phone / at 390x844.", "number yours from 017", "The engineer has a block of its own",
		"`buildermesh:ideate-engineer`"} {
		if !strings.Contains(ux, s) {
			t.Errorf("ux brief lacks %q", s)
		}
	}
}

func TestBuildBriefEmbedsSpecAndNode(t *testing.T) {
	v := BuildValues(pb, "/home/x/repos/pb", "---\nid: 012\n---\n\n## Opportunity\nuses {{nope}} literally\n",
		"docs/proposals/012-a.md", "012", "pb-012", "2026-10-15")
	out, err := Render("", "build", v)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"## Opportunity", "docs/proposals/012-a.md", "Your node is `pb-012`.", "{{nope}}",
		"`bmesh --node pb-012 routine run verify`", "`bmesh --node pb-012 result record review --ok|--fail --model <m>`",
		"`bmesh --node pb-012 land`", "`bmesh spec set --id 012 --status done`", "the trailers `Spec: 012` and `Verified: <entries>`",
		"follow the `buildermesh:lead` skill", "## Landing it", "landing the branch your worktree is on (`git branch --show-current`), never the name in your brief",
		"`git push origin HEAD`", "pushed to `example/pb`",
		"`npm test`"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q", s)
		}
	}
	for _, ev := range []string{"started", "progress", "ask", "blocked", "done", "inbox"} {
		if !strings.Contains(out, "`bmesh --node pb-012 "+ev) {
			t.Errorf("missing event %s", ev)
		}
	}
	for _, bad := range []string{"proposals.py", "k16", "Rick", "{{spec", "--status blocked", "recorded the spec `building`", "accepted", "operator's words"} {
		if strings.Contains(out, bad) {
			t.Errorf("has %q", bad)
		}
	}
}

func TestMissingPlaceholderIsAnError(t *testing.T) {
	_, err := Render("", "build", RowValues(pb, "/r"))
	if err == nil || !strings.Contains(err.Error(), "{{date}}") || !strings.Contains(err.Error(), "{{spec}}") {
		t.Fatalf("%v", err)
	}
	if _, _, err := Template("", "nope"); err == nil || !strings.Contains(err.Error(), "build, engineer") {
		t.Fatal(err)
	}
}

func TestBriefsCarryTheOperatorsFocus(t *testing.T) {
	if OperatorContext("  \n") != "" {
		t.Fatal("blank words made a section")
	}
	v := proposalValues("007")
	plain, err := Render("", "engineer", v)
	if err != nil || strings.Contains(plain, "operator's focus") || !strings.Contains(plain, "\n\n## Closing") {
		t.Fatal(plain, err)
	}
	v["operator_context"] = OperatorContext("routines: reuse and retirement\nthe map's flow\n")
	for _, name := range []RoundPart{RoundAll, RoundUX} {
		maps.Copy(v, RoundValues(name, pb))
		out, err := Render("", "engineer", v)
		if err != nil {
			t.Fatal(err)
		}
		i, j := strings.Index(out, "## The operator's focus"), strings.Index(out, "## Closing")
		if i < 0 || j < i || !strings.Contains(out, "routines: reuse and retirement\nthe map's flow\n\n## Closing") {
			t.Fatalf("%s:\n%s", name, out)
		}
	}
}

// A round pitches an XS or S idea as a line with no spec, and writes a
// spec only for M or larger.
func TestRoundBriefsPitchSmallIdeasAsLines(t *testing.T) {
	for _, name := range []RoundPart{RoundAll, RoundEng, RoundUX, RoundIdea} {
		out, err := Render("", "engineer", partValues("007", name, pb))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"## What you pitch\n", "An idea sized M or larger is a spec",
			"An idea sized XS or S is one pitch line, `- <title> (<kind>, XS|S): <what changes, where, and why>`, with no spec file and no id.",
			"The pitch ranks them all together", "pick or decline in a sentence"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
		if strings.Contains(out, "accept, adjust or reject") {
			t.Errorf("%s still asks for an accept", name)
		}
	}
}

func TestRepoOverridesATemplate(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, ".bmesh", "briefs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "engineer.md"), []byte("mine for {{repo}} {{extra}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := proposalValues("001")
	if _, err := Render(repo, "engineer", v); err == nil || !strings.Contains(err.Error(), "{{extra}}") || !strings.Contains(err.Error(), dir) {
		t.Fatalf("%v", err)
	}
	v["extra"] = "x"
	out, err := Render(repo, "engineer", v)
	if err != nil || out != "mine for pb x\n" {
		t.Fatal(out, err)
	}
	if other, err := Render(repo, "build", BuildValues(pb, repo, "spec", "docs/proposals/001-a.md", "001", "pb-001", "2026-10-24")); err != nil ||
		!strings.HasPrefix(other, "# Build") {
		t.Fatal(other, err)
	}
}

func TestHalfPagerHasPlaceholdersAndHeadings(t *testing.T) {
	text, src, err := Template("", "half-pager")
	if err != nil || src != "embedded" {
		t.Fatal(src, err)
	}
	for _, ph := range []string{"id", "title", "kind", "size", "judge", "lens", "thread", "date"} {
		if !strings.Contains(text, "{{"+ph+"}}") {
			t.Errorf("missing {{%s}}", ph)
		}
	}
	for _, h := range []string{"Opportunity", "Proposal", "What you'll see", "Verification", "Risks and the alternative considered", "Needs from the operator"} {
		if !strings.Contains(text, "## "+h+"\n") {
			t.Errorf("missing heading %s", h)
		}
	}
}

func TestDoneRule(t *testing.T) {
	if got := DoneRule(pb); !strings.Contains(got, "(`npm test`)") || !strings.HasSuffix(got, "pushed to `example/pb`.") {
		t.Fatal(got)
	}
	if got := DoneRule(config.Repo{Name: "local", Verify: []string{"make"}}); !strings.HasSuffix(got, "fast-forward to `main`.") {
		t.Fatal(got)
	}
}

func TestWithUpgrading(t *testing.T) {
	half, _, err := Template("", "half-pager")
	if err != nil {
		t.Fatal(err)
	}
	if HasSection(half, "Upgrading") {
		t.Fatal("the embedded half-pager has an Upgrading section")
	}
	got := WithUpgrading(half)
	if !strings.Contains(got, "\n## Upgrading\n"+UpgradingRule+"\n\n## Needs from the operator\n") || strings.Count(got, "## Upgrading") != 1 {
		t.Fatalf("%s", got)
	}
	if WithUpgrading(got) != got {
		t.Fatal("added twice")
	}
	if got := WithUpgrading("## Proposal\nx"); got != "## Proposal\nx\n\n"+UpgradingSection {
		t.Fatalf("no Needs section: %q", got)
	}
	for _, bad := range []string{"k16", "Rick", "Mac"} {
		if strings.Contains(UpgradingBullet, bad) {
			t.Errorf("bullet names %q", bad)
		}
	}
}

func TestWithSetupOpensTheBrief(t *testing.T) {
	if SetupStep("", "/wt") != "" || WithSetup("# T\n\n## A\n", "") != "# T\n\n## A\n" {
		t.Fatal("no command, no section")
	}
	step := SetupStep("npm ci", "")
	if !strings.HasPrefix(step, "## First: bmesh setup\n\nBefore anything else, run `bmesh setup`. This repo names a setup command (`npm ci`)") {
		t.Fatalf("step %q", step)
	}
	if s := SetupStep("npm ci", "/wt"); !strings.Contains(s, "run `cd /wt && bmesh setup`.") {
		t.Fatalf("with a worktree %q", s)
	}
	got := WithSetup("# Title\n\nIntro.\n\n## A\n\nbody\n\n## B\n", step)
	if got != "# Title\n\nIntro.\n\n"+step+"## A\n\nbody\n\n## B\n" {
		t.Fatalf("got %q", got)
	}
	if WithSetup(got, step) != got {
		t.Fatal("a second pass added it again")
	}
	if got := WithSetup("# mine\n", step); got != "# mine\n\n"+strings.TrimRight(step, "\n")+"\n" {
		t.Fatalf("no heading: %q", got)
	}
}
