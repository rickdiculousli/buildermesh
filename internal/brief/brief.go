// Package brief renders the briefs the mesh hands its threads: the engineer
// and UI/UX engineer briefs of a proposal round, the build brief of a lead
// and the half-pager a spec is written as. The templates embed in the
// binary; a repo overrides one with .bmesh/briefs/<name>.md. `{{name}}`
// placeholders are filled from the registry row, the spec and the thread,
// in one pass (braces inside a value stay as written); a placeholder with
// no value is an error, never left in a brief.
package brief

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

//go:embed templates/*.md
var templates embed.FS

// OverrideDir is where a repo keeps its own templates, under its root.
const OverrideDir = ".bmesh/briefs"

var placeholder = regexp.MustCompile(`\{\{(\w+)\}\}`)

// Names lists the embedded templates.
func Names() []string {
	ents, _ := templates.ReadDir("templates")
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		out = append(out, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(out)
	return out
}

// Template returns the template text: the repo's override
// (<repoPath>/.bmesh/briefs/<name>.md) when it exists, else the embedded
// one; source says which ("embedded", or the override's path). An unknown
// name with no override is an error.
func Template(repoPath, name string) (text, source string, err error) {
	if repoPath != "" {
		p := filepath.Join(repoPath, filepath.FromSlash(OverrideDir), name+".md")
		if b, err := os.ReadFile(p); err == nil {
			return string(b), p, nil
		} else if !os.IsNotExist(err) {
			return "", "", err
		}
	}
	b, err := templates.ReadFile("templates/" + name + ".md")
	if err != nil {
		return "", "", fmt.Errorf("no template %q; have %s", name, strings.Join(Names(), ", "))
	}
	return string(b), "embedded", nil
}

// Render fills the named template (the repo's override, else the embedded
// one) from values. A placeholder without a value is an error naming it.
func Render(repoPath, name string, values map[string]string) (string, error) {
	text, source, err := Template(repoPath, name)
	if err != nil {
		return "", err
	}
	var missing []string
	out := placeholder.ReplaceAllStringFunc(text, func(m string) string {
		key := m[2 : len(m)-2]
		v, ok := values[key]
		if !ok {
			missing = append(missing, key)
			return m
		}
		return v
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("template %s (%s) uses {{%s}}, which has no value here", name, source, strings.Join(missing, "}}, {{"))
	}
	return out, nil
}

// VerifyText is a row's verify commands as prose: each in backticks, comma
// separated; a row with none says so.
func VerifyText(verify []string) string {
	if len(verify) == 0 {
		return "none are listed in repos.toml; run what the repo's AGENTS.md or README names as its tests"
	}
	q := make([]string, len(verify))
	for i, v := range verify {
		q[i] = "`" + v + "`"
	}
	return strings.Join(q, ", ")
}

// SurfacesText is a row's surfaces as prose, or what to do when it lists none.
func SurfacesText(surfaces []string) string {
	if len(surfaces) == 0 {
		return "none are listed in repos.toml; find the screens its users see, and name each one you shot"
	}
	return strings.Join(surfaces, "; ")
}

// sizedText sizes a change's review by its spec's size: XS and S land on
// verify alone; M and up get one Opus review, harder the larger the change.
const sizedText = "its spec's `size` as built: a change whose scope grew is sized by what it grew to, and work with no spec is sized as a spec would be " +
	"(XS one file, S one module, M crosses modules, L changes an interface others depend on); at medium effort for M, high for L or larger"

// PlainWords is how a thread's report names things, so the operator can
// read it without looking anything up; the done rule and every closing end
// with it.
const PlainWords = "Say what each thing is: the first time a message names a spec, issue, PR, commit, job or numbered item, it says in plain words what that is, with the number after it in brackets if at all, so the operator never has to look anything up. The last line is final: it reads on its own, names nothing not explained above it, and says in plain words what waits on the operator and what they'd do about it."

// Closing is the standard closing for a thread on this repo, a blockquote:
// finish the work by the definition of done rather than leaving it on a
// branch.
func Closing(verify []string) string {
	return "> When it works, finish it: run the repo's verify commands (" + VerifyText(verify) +
		") until they pass; when the change is sized M or larger (" + sizedText + "), have Opus in a session that didn't write it review it once and fix what it finds, while an XS or S change lands on verify alone; " +
		"rebase onto `main` and re-run verify; merge fast-forward to `main`, landing the branch your worktree is on (`git branch --show-current`), never the name in your brief; and push where the repo has a remote. " +
		"It isn't done while it sits on a branch. End with one line: what's on `main` (the commit), its push state from a fresh fetch (pushed, with the commit `origin/main` is at and that it contains yours; not pushed and why; or local only, for a repo with no remote), and what, if anything, waits on the operator. " + PlainWords
}

// DoneRule is the repo's definition of done, from its row: what must hold
// before a change is done.
func DoneRule(r config.Repo) string {
	s := "A change is done when all of these hold: the repo's verify commands pass (" + VerifyText(r.Verify) +
		"); a change sized M or larger (" + sizedText + ") was reviewed once against its spec by Opus in a session that didn't write it and the findings were fixed, while an XS or S change is done on verify alone; " +
		"the branch is rebased onto `main` with verify re-run; it is merged fast-forward to `main`"
	if r.Remote != "" {
		s += " and pushed to `" + r.Remote + "`"
	}
	return s + "."
}

// OperatorContext is the section a round's briefs carry for what the
// operator asked of the round, their words as given; no words, no section.
func OperatorContext(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return "## The operator's focus\n\nThe operator started this round with these words. Weigh them first; they do not rule out what else you find:\n\n" +
		text + "\n\n"
}

// OperatorIdea is the section an idea round's engineer gets instead of
// OperatorContext: the round is the operator's one idea, so the engineer
// writes that idea's spec, not a ranked list of its own.
func OperatorIdea(text string) string {
	return "## The operator's idea\n\nThis round is one idea, the operator's own, in their words below; there is no UI/UX thread. " +
		"It replaces the three to five specs of your own: read the repo and its ledger, measure, widen and attack this idea, " +
		"then write its spec, one half-pager, or two or three in build order when it is too big for one lead, each naming the one before. " +
		"Propose nothing else. Your ranked list is those specs.\n\n" + strings.TrimSpace(text) + "\n\n"
}

// RoundValues are the engineer brief's lines that differ for an idea
// round: what to bring back, who runs beside it and how the ids are
// split.
func RoundValues(idea bool) map[string]string {
	if idea {
		return map[string]string{
			"round_ask":     "Write up the operator's one idea, in the section below, as the half-page spec (or two or three in build order) the operator can accept, adjust or reject in a sentence.",
			"round_partner": "There is no UI/UX engineer on this round: it is the operator's one idea, and yours alone.",
			"ux_ids":        "",
		}
	}
	return map[string]string{
		"round_ask":     "Find what would most improve this repo and bring back three to five ranked half-page specs that the operator can accept, adjust or reject in a sentence.",
		"round_partner": "The UI/UX engineer runs beside you on the same round and lands on the same `main`.",
		"ux_ids":        " The UI/UX engineer numbers from five past that, so a gap is expected.",
	}
}

// RowValues are the placeholders every brief gets from a registry row.
func RowValues(r config.Repo, repoPath string) map[string]string {
	return map[string]string{
		"repo":             r.Name,
		"repo_path":        repoPath,
		"verify":           VerifyText(r.Verify),
		"surfaces":         SurfacesText(r.Surfaces),
		"standard_closing": Closing(r.Verify),
		"done_rule":        DoneRule(r),
	}
}

// SetupStep is the section a brief opens with when the repo names a setup
// command (its .bmesh/config.toml [setup]): the thread's first command is
// bmesh setup in its worktree, from inside it when worktree is given (a
// thread whose shell doesn't start there). "" for no command.
func SetupStep(command, worktree string) string {
	if command == "" {
		return ""
	}
	run := "`bmesh setup`"
	if worktree != "" {
		run = "`cd " + worktree + " && bmesh setup`"
	}
	return "## First: bmesh setup\n\nBefore anything else, run " + run + ". This repo names a setup command (`" + command +
		"`) that installs what its verify needs and a fresh worktree lacks; `bmesh setup` runs it when the installs aren't current. " +
		"If it fails, read its output and fix what it names before going on. Run it again after you change dependencies; a verify runs it too.\n\n"
}

// WithSetup is text with section (SetupStep's) before its first `## `
// heading, or at its end when it has none; text itself for no section or
// one it already holds, so a repo's own template gets it too.
func WithSetup(text, section string) string {
	if section == "" || strings.Contains(text, section) {
		return text
	}
	if strings.HasPrefix(text, "## ") {
		return section + text
	}
	if i := strings.Index(text, "\n## "); i >= 0 {
		return text[:i+1] + section + text[i+1:]
	}
	return strings.TrimRight(text, "\n") + "\n\n" + strings.TrimRight(section, "\n") + "\n"
}

// UpgradingRule is what an `## Upgrading` section holds, shared by the
// half-pager's section and the build brief's bullet in bmesh's own repo:
// the steps an install from the previous release needs, for any fleet.
const UpgradingRule = "The steps an install from the previous release needs to run this change, written for any fleet (never one fleet's paths or box names), or `nothing`. " +
	"A new config key is written as an ask: one line saying what changed, the question, the choices and the default. " +
	"A section that is empty or whose first word is `nothing` has no steps; any other text is input to the release's upgrade notes."

// UpgradingSection is the half-pager's `## Upgrading` section, which
// `bmesh spec template` adds in bmesh's own repo.
const UpgradingSection = "## Upgrading\n" + UpgradingRule + "\n"

// UpgradingBullet is the build brief's `{{upgrading}}` in bmesh's own
// repo: a bullet that follows the Build line.
const UpgradingBullet = "\n- This is bmesh's own repo: fill the spec's `## Upgrading` section as built (add it before `## Needs from the operator` when the spec has none). " + UpgradingRule

// WithUpgrading is a half-pager with UpgradingSection before its
// `## Needs from the operator`, or at its end when it has none; a text
// that already has an `## Upgrading` section is returned as is.
func WithUpgrading(text string) string {
	if HasSection(text, "Upgrading") {
		return text
	}
	const needs = "## Needs from the operator"
	for i := 0; ; {
		j := strings.Index(text[i:], needs)
		if j < 0 {
			break
		}
		at := i + j
		if at == 0 || text[at-1] == '\n' {
			return text[:at] + UpgradingSection + "\n" + text[at:]
		}
		i = at + len(needs)
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + "\n" + UpgradingSection
}

// HasSection reports whether text has a `## <name>` heading line.
func HasSection(text, name string) bool {
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) == "## "+name {
			return true
		}
	}
	return false
}
