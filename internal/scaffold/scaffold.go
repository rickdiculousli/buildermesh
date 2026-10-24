// Package scaffold gives a ship repo what a buildermesh build expects of it:
// the "Definition of done", "Proposals" and "Buildermesh" sections of its
// AGENTS.md between markers, and its "Engineering rules" block (rules.go),
// docs/proposals/README.md and assets/.gitkeep,
// the routine kinds its registry row names, seeded from the embedded
// examples, and the row's setup as .bmesh/config.toml's [setup]. Only the
// text between the markers and the [setup] table are rewritten and an
// existing kind is left alone, so a second run changes nothing.
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

// Blocks are the three sections the binary enforces, in the order they are
// appended; the rules block (RulesBlock) follows them and is the repo's own.
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
		"Stop only when the widened scope can't be met from the specs alone; then report the block with `bmesh blocked` and the reason. " +
		"End with one line: what's done (the commit on `main`), " + pushState(r) + ", what wasn't checked, and what, if anything, waits on the operator. " + brief.PlainWords
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
		"The build thread owns its spec's status: it sets `done` on its own branch, so the spec lands with its code; a build in flight, or blocked, is its lead's node on the mesh, never a status in git. " +
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
	// Note is a line scaffold prints of the file besides writing it.
	Note string
}

// Changed reports whether writing the target would change the repo.
func (t Target) Changed() bool { return !t.Exists || t.Current != t.Want }

// Targets is every file scaffold would write for the row in the repo at
// repoPath: AGENTS.md (created when missing; its rules block as ApplyRules
// reads it, a customized one named in the target's Note), the proposals README and
// .gitkeep when missing, a kind file for each of the row's routines that
// the repo lacks, an existing verify kind whose command is not the
// row's verify commands, and .bmesh/config.toml when its [setup] is not
// the row's (merged in: the rest of the file is kept; a row with no setup
// leaves the file alone). A routine with no embedded example is an error; a
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
	want, customized, err := ApplyRules(Apply(agents.Current, Done(r), Proposals(), mesh))
	if err != nil {
		return nil, nil, err
	}
	agents.Want = want
	if customized {
		agents.Note = RulesNote
	}
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
	if len(r.Verify) > 0 {
		t, err := verifyTarget(repoPath, rc, r)
		if err != nil {
			return nil, nil, err
		}
		if t.Exists && t.Changed() {
			targets = append(targets, t)
		}
	}
	if r.Setup != "" {
		t, err := setupTarget(repoPath, r)
		if err != nil {
			return nil, nil, err
		}
		if t.Changed() {
			targets = append(targets, t)
		}
	}
	return targets, skipped, nil
}

// Follow is the row-fed targets of the repo at repoPath that read
// otherwise than the row: the verify kind's command (when verify, the row
// names verify commands and the kind exists) and .bmesh/config.toml's
// [setup] (when setup, the row names a setup and the file exists). A
// missing file is left to Targets, which creates it; AGENTS.md is never
// one, since its wording is this binary's. An error is FollowVerify's or
// FollowSetup's refusal.
func Follow(repoPath string, r config.Repo, verify, setup bool) ([]Target, error) {
	var out []Target
	if verify && len(r.Verify) > 0 {
		rc, err := kind.LoadRepoConfig(repoPath)
		if err != nil {
			return nil, err
		}
		t, err := verifyTarget(repoPath, rc, r)
		if err != nil {
			return nil, err
		}
		if t.Exists && t.Changed() {
			out = append(out, t)
		}
	}
	if setup && r.Setup != "" {
		t, err := setupTarget(repoPath, r)
		if err != nil {
			return nil, err
		}
		if t.Exists && t.Changed() {
			out = append(out, t)
		}
	}
	return out, nil
}

// verifyTarget is the verify kind with its command the row's; Want is
// empty when the kind doesn't exist.
func verifyTarget(repoPath string, rc kind.RepoConfig, r config.Repo) (Target, error) {
	t, err := read(repoPath, verifyRel(rc))
	if err != nil || !t.Exists {
		return t, err
	}
	t.Want, err = FollowVerify(t.Rel, t.Current, r)
	return t, err
}

// setupTarget is the repo config with its [setup] the row's.
func setupTarget(repoPath string, r config.Repo) (Target, error) {
	t, err := read(repoPath, kind.ConfigFile)
	if err != nil {
		return t, err
	}
	t.Want, err = FollowSetup(t.Current, r)
	return t, err
}

// DoneFollows reports whether AGENTS.md's text holds the row's done block
// as this binary writes it.
func DoneFollows(text string, r config.Repo) bool {
	return blockRE("done").FindString(text) == block("done", Done(r))
}

// RowSetup is the [setup] a row asks for, as the repo config reads it.
func RowSetup(r config.Repo) (kind.Setup, error) {
	rc, err := kind.ParseRepoConfig(SetupBlock(r))
	if err != nil {
		return kind.Setup{}, err
	}
	return rc.Setup, nil
}

// SetupBlock is the [setup] table scaffold writes for a row: its command,
// and its inputs and timeout when it names them.
func SetupBlock(r config.Repo) string {
	if r.Setup == "" {
		return ""
	}
	b := "[setup]\ncommand = " + tomlString(r.Setup) + "\n"
	if len(r.SetupInputs) > 0 {
		q := make([]string, len(r.SetupInputs))
		for i, p := range r.SetupInputs {
			q[i] = tomlString(p)
		}
		b += "inputs = [" + strings.Join(q, ", ") + "]\n"
	}
	if r.SetupTimeout != "" {
		b += "timeout = " + tomlString(r.SetupTimeout) + "\n"
	}
	return b
}

// FollowSetup is a repo config's text with its [setup] table the row's:
// text itself when the table already reads the same (formatting and
// comments kept), else the table replaced in place (from its header to the
// next table's) or appended, the rest of the file ([slots], [routines])
// kept. Text that doesn't parse is an error.
func FollowSetup(text string, r config.Repo) (string, error) {
	want, err := RowSetup(r)
	if err != nil {
		return "", err
	}
	rc, err := kind.ParseRepoConfig(text)
	if err != nil {
		return "", err
	}
	if sameSetup(rc.Setup, want) {
		return text, nil
	}
	lines := strings.SplitAfter(text, "\n")
	start, end := -1, len(lines)
	inString := "" // the delimiter of the multi-line string a line is in
	for i, l := range lines {
		was := inString
		for _, d := range []string{`"""`, `'''`} {
			if strings.Count(l, d)%2 == 0 {
				continue
			}
			switch inString {
			case "":
				inString = d
			case d:
				inString = ""
			}
		}
		m := tableHeaderRE.FindStringSubmatch(l)
		if was != "" || m == nil {
			continue
		}
		if start >= 0 {
			end = i
			break
		}
		if strings.TrimSpace(m[1]) == "setup" {
			start = i
		}
	}
	block := SetupBlock(r)
	var out string
	if start < 0 {
		out = text
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		if out != "" {
			out += "\n"
		}
		out += block
	} else {
		// The comment lines right above the next table lead into it.
		for end > start+1 && end < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[end-1]), "#") {
			end--
		}
		rest := strings.Join(lines[end:], "")
		if rest != "" {
			rest = "\n" + rest
		}
		out = strings.Join(lines[:start], "") + block + rest
	}
	if got, err := kind.ParseRepoConfig(out); err != nil || !sameSetup(got.Setup, want) {
		return "", fmt.Errorf("%s: its [setup] is not a table scaffold can rewrite; set it to the row's setup by hand", kind.ConfigFile)
	}
	return out, nil
}

// tableHeaderRE is a TOML table or array-of-tables header line, its name
// in the first group.
var tableHeaderRE = regexp.MustCompile(`^\s*\[\[?([A-Za-z0-9_."' -]+)\]\]?\s*(#.*)?\r?\n?$`)

func sameSetup(a, b kind.Setup) bool {
	return a.Command == b.Command && a.Timeout == b.Timeout && strings.Join(a.Inputs, "\x00") == strings.Join(b.Inputs, "\x00")
}

func verifyRel(rc kind.RepoConfig) string {
	return filepath.ToSlash(filepath.Join(rc.RoutinesDir, "verify.toml"))
}

// commandRE is a kind file's command line, when it is on one line.
var commandRE = regexp.MustCompile(`(?m)^command[ \t]*=.*$`)

// FollowVerify is an existing verify kind's text (rel is its path, for the
// error) with its command the row's verify commands joined with " && ", the
// rest of the file kept; text itself when the command already reads so, or
// when the kind runs a model rather than a command. A command not on one
// line it can rewrite is an error naming the file.
func FollowVerify(rel, text string, r config.Repo) (string, error) {
	want := strings.Join(r.Verify, " && ")
	if cmd, err := verifyCommand(text); err != nil {
		return "", fmt.Errorf("%s: %w", rel, err)
	} else if cmd == "" || cmd == trimCommand(want) {
		return text, nil
	}
	if n := len(commandRE.FindAllStringIndex(text, -1)); n == 1 {
		out := commandRE.ReplaceAllLiteralString(text, "command = "+tomlString(want))
		if cmd, err := verifyCommand(out); err == nil && cmd == trimCommand(want) {
			return out, nil
		}
	}
	return "", fmt.Errorf("%s: its command is not on one line scaffold can rewrite; set it to the row's verify (%s) by hand", rel, want)
}

// verifyCommand is the command a verify kind's text runs, as the kind
// loader reads it.
func verifyCommand(text string) (string, error) {
	k, err := kind.Parse("verify", []byte(text))
	if err != nil {
		return "", err
	}
	return k.Command, nil
}

// trimCommand trims a command as the kind loader does.
func trimCommand(s string) string { return strings.TrimRight(s, " \t\r\n;") }

// Drift is the unified diff of the scaffolded text a branch changed that the
// row doesn't write: AGENTS.md's marked blocks (when agents; the rules block
// is the repo's own, so an edited or re-picked one is never drift), the verify
// kind's command and the repo config's [setup] (when the row names a
// setup), each only when it differs from the base's (at reads a
// repo-relative file there: its text and whether it exists). So a row or a
// snippet that moved under an untouched file is not the branch's drift, and
// a file scaffold would create is not drift at all. A repo whose AGENTS.md
// has no bmesh: marker was never scaffolded: scaffolded is false and
// nothing is compared.
func Drift(repoPath string, r config.Repo, snippet string, agents bool, at func(rel string) (string, bool, error)) (diff string, scaffolded bool, err error) {
	cur, err := read(repoPath, Agents)
	if err != nil {
		return "", false, err
	}
	if !cur.Exists || !strings.Contains(cur.Current, "<!-- "+MarkerPrefix) {
		return "", false, nil
	}
	var drift []Target
	if agents {
		base, _, err := at(Agents)
		if err != nil {
			return "", true, err
		}
		if blocks(base) != blocks(cur.Current) {
			mesh, err := Mesh(r, snippet)
			if err != nil {
				return "", true, err
			}
			if cur.Want = Apply(cur.Current, Done(r), Proposals(), mesh); cur.Changed() {
				drift = append(drift, cur)
			}
		}
	}
	rc, err := kind.LoadRepoConfig(repoPath)
	if err != nil {
		return "", true, err
	}
	t, err := read(repoPath, verifyRel(rc))
	if err != nil {
		return "", true, err
	}
	if t.Exists && len(r.Verify) > 0 {
		base, ok, err := at(t.Rel)
		if err != nil {
			return "", true, err
		}
		was := ""
		if ok {
			was, _ = verifyCommand(base)
		}
		now, err := verifyCommand(t.Current)
		if err != nil {
			return "", true, fmt.Errorf("%s: %w", t.Rel, err)
		}
		if now != was {
			if t.Want, err = FollowVerify(t.Rel, t.Current, r); err != nil {
				return "", true, err
			}
			if t.Changed() {
				drift = append(drift, t)
			}
		}
	}
	if r.Setup != "" {
		t, err := read(repoPath, kind.ConfigFile)
		if err != nil {
			return "", true, err
		}
		base, _, err := at(t.Rel)
		if err != nil {
			return "", true, err
		}
		was, err := kind.ParseRepoConfig(base)
		if err != nil {
			was = kind.RepoConfig{}
		}
		now, err := kind.ParseRepoConfig(t.Current)
		if err != nil {
			return "", true, err
		}
		if !sameSetup(now.Setup, was.Setup) {
			if t.Want, err = FollowSetup(t.Current, r); err != nil {
				return "", true, err
			}
			if t.Changed() {
				drift = append(drift, t)
			}
		}
	}
	return Plan(drift), true, nil
}

// blocks is the text of text's marked blocks, the first of each name under
// either prefix, in Blocks order.
func blocks(text string) string {
	var b strings.Builder
	for _, name := range Blocks {
		b.WriteString(blockRE(name).FindString(text))
		b.WriteByte(0)
	}
	return b.String()
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
