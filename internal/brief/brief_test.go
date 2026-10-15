package brief

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

var pb = config.Repo{Name: "pb", Path: "x/pb", Mode: "ship", Remote: "example/pb", Verify: []string{"npm test"},
	Surfaces: []string{"phone / at 390x844"}}

func proposalValues(nextID string) map[string]string {
	v := RowValues(pb, "/home/x/repos/pb")
	for k, s := range map[string]string{"date": "2026-10-15", "next_id": nextID, "thread_id": "5d0c7a1e-0000-4000-8000-000000000001",
		"node": "pb-round-1.eng", "round": "pb-round-1", "round_n": "1", "worktree": "/home/x/.bmesh/worktrees/pb/pb-round-1-eng",
		"branch": "pb-round-1-eng", "operator_context": ""} {
		v[k] = s
	}
	return v
}

func TestNames(t *testing.T) {
	if got := strings.Join(Names(), ","); got != "build,engineer,half-pager,ux-engineer" {
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

func TestUXBriefNamesSurfacesAndSkill(t *testing.T) {
	out, err := Render("", "ux-engineer", proposalValues("012"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"phone / at 390x844", "`buildermesh:ideate-ux`", "number yours from 012", "`lens: ux`", "screenshot"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q", s)
		}
	}
	none := RowValues(config.Repo{Name: "bare"}, "/r")
	if !strings.Contains(none["surfaces"], "none are listed") || !strings.Contains(none["verify"], "none are listed") {
		t.Fatal(none)
	}
}

func TestBuildBriefEmbedsSpecAndNode(t *testing.T) {
	v := RowValues(pb, "/home/x/repos/pb")
	v["date"], v["node"], v["spec_id"], v["spec_path"] = "2026-10-15", "pb-012", "012", "docs/proposals/012-a.md"
	v["spec"] = "---\nid: 012\n---\n\n## Opportunity\nuses {{nope}} literally\n"
	out, err := Render("", "build", v)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"## Opportunity", "docs/proposals/012-a.md", "Your node is `pb-012`.", "{{nope}}",
		"`bmesh --node pb-012 routine run verify`", "`bmesh --node pb-012 result record review --ok|--fail --model <m>`",
		"`bmesh --node pb-012 spec verified --write`", "bmesh spec set --repo . --id 012 --status done --from building",
		"follow the `buildermesh:lead` skill", "## Landing it", "landing the branch your worktree is on (`git branch --show-current`), never the name in your brief",
		"adding `--field branch=<that name>` when `git branch --show-current` differs", "`git push origin HEAD`", "pushed to `example/pb`",
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
	for _, bad := range []string{"proposals.py", "k16", "Rick", "{{spec"} {
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
	for _, name := range []string{"engineer", "ux-engineer"} {
		out, err := Render("", name, v)
		if err != nil {
			t.Fatal(err)
		}
		i, j := strings.Index(out, "## The operator's focus"), strings.Index(out, "## Closing")
		if i < 0 || j < i || !strings.Contains(out, "routines: reuse and retirement\nthe map's flow\n\n## Closing") {
			t.Fatalf("%s:\n%s", name, out)
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
	if other, err := Render(repo, "ux-engineer", v); err != nil || !strings.HasPrefix(other, "# UI/UX engineer") {
		t.Fatal(err)
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

func TestLeadSkillLandsTheWorktreesBranch(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "skills", "lead", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, sec, ok := strings.Cut(string(b), "## 7. Land")
	if !ok {
		t.Fatal("no section 7")
	}
	sec, _, _ = strings.Cut(sec, "\n## ")
	for _, s := range []string{"`git branch --show-current`", "never the name in your brief", "--field branch=<that name>"} {
		if !strings.Contains(sec, s) {
			t.Errorf("lead skill §7 missing %q", s)
		}
	}
}
