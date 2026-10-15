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

// Closing is the standard closing for a thread on this repo, a blockquote:
// finish the work by the definition of done rather than leaving it on a
// branch.
func Closing(verify []string) string {
	return "> When it works, finish it: run the repo's verify commands (" + VerifyText(verify) +
		") until they pass; have a validation agent that didn't write the change review it, and fix what it finds; " +
		"rebase onto `main` and re-run verify; merge fast-forward to `main`; and push where the repo has a remote. " +
		"It isn't done while it sits on a branch. End with one line: what's on `main` (the commit) and what, if anything, waits on the operator."
}

// DoneRule is the repo's definition of done, from its row: what must hold
// before a change is done.
func DoneRule(r config.Repo) string {
	s := "A change is done when all of these hold: the repo's verify commands pass (" + VerifyText(r.Verify) +
		"); a validation agent that didn't write the change reviewed it against its spec and the findings were fixed; " +
		"the branch is rebased onto `main` with verify re-run; it is merged fast-forward to `main`"
	if r.Remote != "" {
		s += " and pushed to `" + r.Remote + "`"
	}
	return s + "."
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
