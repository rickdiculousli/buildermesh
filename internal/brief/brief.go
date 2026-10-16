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
