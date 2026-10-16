// Package scaffold gives a ship repo what a buildermesh build expects of it:
// the "Definition of done", "Proposals" and "Buildermesh" sections of its
// AGENTS.md between markers, docs/proposals/README.md and assets/.gitkeep,
// and the routine kinds its registry row names, seeded from the embedded
// examples. Only the text between the markers is rewritten and an existing
// kind is left alone, so a second run changes nothing.
package scaffold

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/rickdiculousli/buildermesh"
	"github.com/rickdiculousli/buildermesh/internal/brief"
	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/kind"
)

const (
	// Agents is the file the sections go in.
	Agents = "AGENTS.md"
	// Readme and Gitkeep are created when missing, never rewritten.
	Readme  = "docs/proposals/README.md"
	Gitkeep = "docs/proposals/assets/.gitkeep"
	// MarkerPrefix opens a block: <!-- bmesh:done --> ... <!-- /bmesh:done -->.
	MarkerPrefix = "bmesh:"
	// LegacyPrefix is the prefix an earlier scaffold wrote; a block under it
	// is read, and rewritten in place under MarkerPrefix.
	LegacyPrefix = "command-center:"
	// VerifyPlaceholder is what the snippet says where the verify commands go.
	VerifyPlaceholder = "[the verify commands]"
)

// Blocks are the three sections, in the order they are appended.
var Blocks = []string{"done", "proposals", "mesh"}

// ErrSnippet is a snippet with no quoted paragraph, or one that keeps a
// placeholder.
var ErrSnippet = errors.New("snippet")

// Done is the "Definition of done" section for a row, heading included: the
// row's done rule, then what to do when the rebase conflicts.
func Done(r config.Repo) string {
	return "## Definition of done\n" + brief.DoneRule(r) +
		" If the rebase conflicts, resolve it: read your spec and the specs that landed since you branched, make every intent hold, " +
		"widen your spec if a landed one changes what yours must look like, re-validate, then merge. " +
		"Stop only when the widened scope can't be met from the specs alone; then mark your spec `blocked` with the reason, push, and report. " +
		"End with one line: what's done (the commit on `main`), " + pushState(r) + ", and what, if anything, waits on the operator."
}

// pushState is what the done line says of the push, so the operator knows
// whether they can sync: fetched and checked, never assumed.
func pushState(r config.Repo) string {
	if r.Remote == "" {
		return "that it is local only (the repo has no remote)"
	}
	return "its push state from a fresh fetch (pushed, with the commit `origin/main` is at and that it contains yours, or not pushed and why)"
}

// Proposals is the "Proposals" section, the same for every repo.
func Proposals() string {
	return "## Proposals\n" +
		"Half-page specs live in `docs/proposals/NNN-slug.md` (the template is `bmesh spec template`; the lifecycle is in `docs/proposals/README.md`). " +
		"The build thread owns its spec's status: `building` when dispatched, `done` with the merge SHA when merged, `blocked` with a reason if it can't finish. " +
		"Rejected specs stay so they aren't re-proposed. Accepted specs are built by buildermesh leads (see Buildermesh below)."
}

// Mesh is the "Buildermesh" section: the snippet's quoted paragraph with
// its leading bold name dropped and the verify placeholder filled from the
// row. A snippet with no quoted paragraph, or one that keeps a bracketed
// placeholder, is an error.
func Mesh(r config.Repo, snippet string) (string, error) {
	var parts []string
	for _, line := range strings.Split(snippet, "\n") {
		if !strings.HasPrefix(line, ">") {
			continue
		}
		part := strings.TrimSpace(strings.TrimPrefix(line, ">"))
		if part != "" {
			parts = append(parts, part)
		}
	}
	body := strings.Join(parts, " ")
	if body == "" {
		return "", fmt.Errorf("%w: no quoted paragraph", ErrSnippet)
	}
	body = strings.TrimSpace(strings.TrimPrefix(body, "**buildermesh.**"))
	if i := strings.Index(strings.ReplaceAll(body, VerifyPlaceholder, ""), "["); i >= 0 {
		return "", fmt.Errorf("%w: a placeholder is left: %s", ErrSnippet, body[i:min(len(body), i+40)])
	}
	verify := brief.VerifyText(r.Verify)
	if len(r.Verify) == 0 {
		verify = "nothing yet (the registry row lists no verify commands)"
	}
	return "## Buildermesh\n" + strings.ReplaceAll(body, VerifyPlaceholder, verify), nil
}

func block(name, body string) string {
	return "<!-- " + MarkerPrefix + name + " -->\n" + body + "\n<!-- /" + MarkerPrefix + name + " -->"
}

// blockRE matches a marked block under either prefix, opening and closing
// marker under the same one.
func blockRE(name string) *regexp.Regexp {
	var alts []string
	for _, prefix := range []string{MarkerPrefix, LegacyPrefix} {
		m := regexp.QuoteMeta(prefix + name)
		alts = append(alts, `<!-- `+m+` -->.*?<!-- /`+m+` -->`)
	}
	return regexp.MustCompile(`(?s)` + strings.Join(alts, "|"))
}

// Apply is text with each marked block replaced (done, proposals, mesh in
// that order) by a bmesh: block with the new content; the first block of a
// name under either prefix is rewritten in place, so a legacy
// command-center: block becomes the bmesh: one rather than gaining a twin.
// A block that is absent is appended at the end, after a blank line.
// Nothing outside the markers changes.
func Apply(text, done, proposals, mesh string) string {
	bodies := map[string]string{"done": done, "proposals": proposals, "mesh": mesh}
	for _, name := range Blocks {
		re := blockRE(name)
		b := block(name, bodies[name])
		if re.MatchString(text) {
			first := true
			text = re.ReplaceAllStringFunc(text, func(m string) string {
				if first {
					first = false
					return b
				}
				return m
			})
			continue
		}
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if text != "" {
			text += "\n"
		}
		text += b + "\n"
	}
	return text
}

// Target is one file scaffold writes: its repo-relative path (slashes), the
// text it should hold and the text it holds now (Exists false when it does
// not).
type Target struct {
	Rel     string
	Want    string
	Current string
	Exists  bool
}

// Changed reports whether writing the target would change the repo.
func (t Target) Changed() bool { return !t.Exists || t.Current != t.Want }

// Targets is every file scaffold would write for the row in the repo at
// repoPath: AGENTS.md (created when missing), the proposals README and
// .gitkeep when missing, and a kind file for each of the row's routines
// that the repo lacks. A routine with no embedded example is an error; a
// verify kind for a row with no verify commands is skipped and named in
// skipped.
func Targets(repoPath string, r config.Repo, snippet string) (targets []Target, skipped []string, err error) {
	mesh, err := Mesh(r, snippet)
	if err != nil {
		return nil, nil, err
	}
	agents, err := read(repoPath, Agents)
	if err != nil {
		return nil, nil, err
	}
	agents.Want = Apply(agents.Current, Done(r), Proposals(), mesh)
	targets = append(targets, agents)
	for rel, want := range map[string]string{Readme: buildermesh.ProposalsReadme, Gitkeep: ""} {
		t, err := read(repoPath, rel)
		if err != nil {
			return nil, nil, err
		}
		if !t.Exists {
			t.Want = want
			targets = append(targets, t)
		}
	}
	sort.Slice(targets[1:], func(i, j int) bool { return targets[1+i].Rel < targets[1+j].Rel })
	rc, err := kind.LoadRepoConfig(repoPath)
	if err != nil {
		return nil, nil, err
	}
	for _, name := range r.Routines {
		if !kind.NameRE.MatchString(name) {
			return nil, nil, fmt.Errorf("routine %q in the row is not a kind name", name)
		}
		t, err := read(repoPath, filepath.ToSlash(filepath.Join(rc.RoutinesDir, name+".toml")))
		if err != nil {
			return nil, nil, err
		}
		if t.Exists {
			continue
		}
		if name == "verify" && len(r.Verify) == 0 {
			skipped = append(skipped, name)
			continue
		}
		text, err := Example(name, r)
		if err != nil {
			return nil, nil, err
		}
		t.Want = text
		targets = append(targets, t)
	}
	return targets, skipped, nil
}

// Example is the embedded example kind, filled for the row: verify's
// command is the row's verify commands joined with " && ". A kind with no
// example is an error naming the file to write by hand.
func Example(name string, r config.Repo) (string, error) {
	b, err := buildermesh.Examples.ReadFile("examples/routines/" + name + ".toml")
	if err != nil {
		return "", fmt.Errorf("no example for kind %s; write .bmesh/routines/%s.toml yourself", name, name)
	}
	text := string(b)
	if name == "verify" {
		cmd := "command = " + tomlString(strings.Join(r.Verify, " && "))
		re := regexp.MustCompile(`(?m)^command = .*$`)
		if !re.MatchString(text) {
			return "", errors.New("the verify example has no command line")
		}
		text = re.ReplaceAllLiteralString(text, cmd)
	}
	return text, nil
}

// tomlString quotes s as a basic TOML string.
func tomlString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}

func read(repoPath, rel string) (Target, error) {
	t := Target{Rel: rel}
	b, err := os.ReadFile(filepath.Join(repoPath, filepath.FromSlash(rel)))
	if errors.Is(err, os.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return t, err
	}
	t.Current, t.Exists = string(b), true
	return t, nil
}

// Plan is the unified diff of what scaffold would change (a new empty file is
// one line); empty when the repo is scaffolded.
func Plan(targets []Target) string {
	var b strings.Builder
	for _, t := range targets {
		if !t.Changed() {
			continue
		}
		if !t.Exists && t.Want == "" {
			fmt.Fprintf(&b, "new empty file %s\n", t.Rel)
			continue
		}
		from := "a/" + t.Rel
		if !t.Exists {
			from = "/dev/null"
		}
		b.WriteString(Unified(from, "b/"+t.Rel, t.Current, t.Want))
	}
	return b.String()
}

// Write writes every changed target and returns their paths.
func Write(repoPath string, targets []Target) ([]string, error) {
	var written []string
	for _, t := range targets {
		if !t.Changed() {
			continue
		}
		p := filepath.Join(repoPath, filepath.FromSlash(t.Rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(p, []byte(t.Want), 0o644); err != nil {
			return written, err
		}
		written = append(written, t.Rel)
	}
	return written, nil
}
