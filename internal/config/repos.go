package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// ReposFile is the registry's file name, beside config.toml.
const ReposFile = "repos.toml"

// Modes are the ways a repo is worked on; the first is the default.
var Modes = []string{"ship", "companion", "publish", "fork", "dormant"}

// DefaultRoutines are the routine kinds a row runs when it names none.
var DefaultRoutines = []string{"verify", "review"}

// NamePattern is what a registry name matches: a directory name.
const NamePattern = `^[A-Za-z0-9][A-Za-z0-9_.-]*$`

var repoName = regexp.MustCompile(NamePattern)

// ValidName reports whether name matches NamePattern.
func ValidName(name string) bool { return repoName.MatchString(name) }

// Repo is one row of repos.toml: everything the mesh knows about a repo that
// is the same on every box. Path is as written (callers Expand it).
type Repo struct {
	Name     string   `json:"name" toml:"-"`
	Path     string   `json:"path" toml:"path"`
	Mode     string   `json:"mode" toml:"mode"`
	Remote   string   `json:"remote" toml:"remote"`
	Host     string   `json:"host" toml:"host"`
	UXHost   string   `json:"ux_host" toml:"ux_host"`
	Verify   []string `json:"verify" toml:"verify"`
	Surfaces []string `json:"surfaces" toml:"surfaces"`
	Pin      string   `json:"pin" toml:"pin"`
	Routines []string `json:"routines" toml:"routines"`
	Code     []string `json:"code" toml:"code"`
}

type reposFile struct {
	Repos map[string]Repo `toml:"repos"`
}

// ReposPath is the registry beside the config file.
func ReposPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), ReposFile)
}

// LoadRepos reads repos.toml. A missing file is an empty registry. Every
// row needs a path; mode defaults to ship and must be one of Modes;
// routines default to DefaultRoutines; an unknown key is an error.
func LoadRepos(path string) (map[string]Repo, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Repo{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("repos %s: %w", path, err)
	}
	out, err := ParseRepos(string(b))
	if err != nil {
		return nil, fmt.Errorf("repos %s: %w", path, err)
	}
	return out, nil
}

// ParseRepos reads a registry's text as LoadRepos reads repos.toml: the
// same defaults and refusals, an unknown key among them. It is how a box
// reads the rows of a registry the admin published (023), so a row this
// binary can't read is refused, never half-read.
func ParseRepos(text string) (map[string]Repo, error) {
	out := map[string]Repo{}
	var f reposFile
	md, err := toml.Decode(text, &f)
	if err != nil {
		return nil, err
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return nil, fmt.Errorf("unknown key %s", keys[0])
	}
	for name, r := range f.Repos {
		r.Name = name
		if err := r.check(); err != nil {
			return nil, fmt.Errorf("[repos.%s]: %w", name, err)
		}
		out[name] = r
	}
	return out, nil
}

// WriteReposText writes text as the registry at path, keeping the file it
// replaces as <path>.prev; a file that already holds text is left alone.
// The write goes through a rename, so a reader sees the old file or the
// new one.
func WriteReposText(path, text string) error {
	old, err := os.ReadFile(path)
	if err == nil && string(old) == text {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err == nil {
		if err := writeFileAtomic(path+".prev", old); err != nil {
			return err
		}
	}
	return writeFileAtomic(path, []byte(text))
}

func writeFileAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// DiffRepos is the row diff from old to new, sorted by name: `+ <name>`
// with the new row's fields for an added row, `- <name>` for a removed
// one, and `~ <name>: <field>: <old> -> <new>` for each changed field.
func DiffRepos(old, new map[string]Repo) []string {
	names := map[string]bool{}
	for n := range old {
		names[n] = true
	}
	for n := range new {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	var out []string
	for _, n := range sorted {
		o, inOld := old[n]
		w, inNew := new[n]
		switch {
		case !inOld:
			var fs []string
			for _, f := range repoFields(w) {
				if f[1] != "" {
					fs = append(fs, f[0]+" "+f[1])
				}
			}
			out = append(out, "+ "+n+": "+strings.Join(fs, ", "))
		case !inNew:
			out = append(out, "- "+n)
		default:
			of, nf := repoFields(o), repoFields(w)
			for i := range of {
				if of[i][1] != nf[i][1] {
					out = append(out, fmt.Sprintf("~ %s: %s: %s -> %s", n, of[i][0], orNone(of[i][1]), orNone(nf[i][1])))
				}
			}
		}
	}
	return out
}

// repoFields is a row's fields as name and printed value, in file order.
func repoFields(r Repo) [][2]string {
	return [][2]string{{"path", r.Path}, {"mode", r.Mode}, {"remote", r.Remote}, {"host", r.Host},
		{"ux_host", r.UXHost}, {"verify", listText(r.Verify)}, {"surfaces", listText(r.Surfaces)}, {"pin", r.Pin},
		{"routines", listText(r.Routines)}, {"code", listText(r.Code)}}
}

func listText(l []string) string {
	if len(l) == 0 {
		return ""
	}
	return tomlList(l)
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// check fills a row's defaults and refuses what it can't read.
func (r *Repo) check() error {
	if !repoName.MatchString(r.Name) {
		return fmt.Errorf("name %q: a name matches %s", r.Name, repoName)
	}
	if r.Path == "" {
		return errors.New("path is required")
	}
	if r.Mode == "" {
		r.Mode = Modes[0]
	}
	ok := false
	for _, m := range Modes {
		ok = ok || m == r.Mode
	}
	if !ok {
		return fmt.Errorf("mode %q is not one of %s", r.Mode, strings.Join(Modes, ", "))
	}
	if r.Routines == nil {
		r.Routines = append([]string{}, DefaultRoutines...)
	}
	if r.Verify == nil {
		r.Verify = []string{}
	}
	if r.Surfaces == nil {
		r.Surfaces = []string{}
	}
	if r.Code == nil {
		r.Code = []string{}
	}
	for _, p := range r.Code {
		if strings.TrimSpace(p) == "" {
			return errors.New("code: an empty pathspec")
		}
	}
	return nil
}

// ModuleRepo is the row whose checkout's go.mod declares module (bmesh's own
// row, for the module this binary was built from), "" for none.
func (c *Config) ModuleRepo(module string) string {
	if module == "" {
		return ""
	}
	for _, n := range c.RepoNames() {
		if _, p, err := c.RepoPath(n); err == nil && ModulePath(filepath.Join(p, "go.mod")) == module {
			return n
		}
	}
	return ""
}

// ModulePath is the module a go.mod declares, "" when unreadable.
func ModulePath(goMod string) string {
	f, err := os.Open(goMod)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

// RepoNames is the registry's names, sorted.
func (c *Config) RepoNames() []string {
	names := make([]string, 0, len(c.Repos))
	for n := range c.Repos {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// WriteRepo adds or replaces one row in repos.toml, keeping the other rows;
// a row that would not load (a bad name, no path) is refused before anything
// is written. The file is rewritten whole, sorted by name, so a comment in
// it is lost.
func WriteRepo(path string, r Repo) error {
	repos, err := LoadRepos(path)
	if err != nil {
		return err
	}
	if err := r.check(); err != nil {
		return err
	}
	repos[r.Name] = r
	return WriteRepos(path, repos)
}

// WriteRepos writes a whole registry.
func WriteRepos(path string, repos map[string]Repo) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(renderRepos(repos)), 0o644)
}

// renderRepos is a registry's text, sorted by name, each row's defaults
// left out.
func renderRepos(repos map[string]Repo) string {
	names := make([]string, 0, len(repos))
	for n := range repos {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("# bmesh's repo registry: one [repos.<name>] per repo (see docs/manual.md, Config).\n")
	for _, n := range names {
		r := repos[n]
		fmt.Fprintf(&b, "\n[repos.%s]\n", tomlKey(n))
		fmt.Fprintf(&b, "path = %s\n", tomlString(r.Path))
		if r.Mode != "" && r.Mode != Modes[0] {
			fmt.Fprintf(&b, "mode = %s\n", tomlString(r.Mode))
		}
		if r.Remote != "" {
			fmt.Fprintf(&b, "remote = %s\n", tomlString(r.Remote))
		}
		if r.Host != "" {
			fmt.Fprintf(&b, "host = %s\n", tomlString(r.Host))
		}
		if r.UXHost != "" {
			fmt.Fprintf(&b, "ux_host = %s\n", tomlString(r.UXHost))
		}
		if len(r.Verify) > 0 {
			fmt.Fprintf(&b, "verify = %s\n", tomlList(r.Verify))
		}
		if len(r.Surfaces) > 0 {
			fmt.Fprintf(&b, "surfaces = %s\n", tomlList(r.Surfaces))
		}
		if r.Pin != "" {
			fmt.Fprintf(&b, "pin = %s\n", tomlString(r.Pin))
		}
		if r.Routines != nil && strings.Join(r.Routines, ",") != strings.Join(DefaultRoutines, ",") {
			fmt.Fprintf(&b, "routines = %s\n", tomlList(r.Routines))
		}
		if len(r.Code) > 0 {
			fmt.Fprintf(&b, "code = %s\n", tomlList(r.Code))
		}
	}
	return b.String()
}

// tomlKey is a bare key when TOML allows it, else a quoted one (a name with
// a dot would read as a nested table).
func tomlKey(s string) string {
	if bareKey.MatchString(s) {
		return s
	}
	return tomlString(s)
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func tomlString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}

func tomlList(l []string) string {
	q := make([]string, len(l))
	for i, s := range l {
		q[i] = tomlString(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// SameRemote reports whether two remotes name the same repo: compared
// without a trailing / or .git, and a GitHub URL (https, ssh or scp form)
// as its owner/name, which is how a row's remote is usually written.
func SameRemote(a, b string) bool {
	na, nb := normRemote(a), normRemote(b)
	return na != "" && na == nb
}

func normRemote(u string) string {
	s := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(u), "/"), ".git")
	for _, p := range []string{"git@github.com:", "ssh://git@github.com/", "https://github.com/", "http://github.com/"} {
		if rest, ok := strings.CutPrefix(s, p); ok {
			return strings.ToLower(rest)
		}
	}
	return s
}

// RemoveRepoText is a registry's text without row name: its table, from
// the comment lines right above its header (unless they open the file)
// to the next table's header, keeping the comment lines that lead into the
// next table. The result is checked to read as the same rows
// less that one; when it doesn't, the rows are written afresh (losing the
// comments) by the same rules as WriteRepos.
func RemoveRepoText(text, name string) (string, error) {
	rows, err := ParseRepos(text)
	if err != nil {
		return "", err
	}
	if _, ok := rows[name]; !ok {
		return "", fmt.Errorf("no row %s", name)
	}
	delete(rows, name)
	lines := strings.SplitAfter(text, "\n")
	start, end := -1, len(lines)
	for i, l := range lines {
		h, ok := tableHeader(l)
		if !ok {
			continue
		}
		if start >= 0 {
			end = i
			break
		}
		if h == "repos."+name || h == "repos."+tomlString(name) {
			start = i
		}
	}
	if start >= 0 {
		for end > start+1 && isLead(lines[end-1]) {
			end--
		}
		lead := start
		for lead > 0 && isLead(lines[lead-1]) {
			lead--
		}
		if lead > 0 {
			start = lead // the row's own comment, right above its header; not the file's, at its top
		}
		out := strings.Join(append(append([]string{}, lines[:start]...), lines[end:]...), "")
		if got, err := ParseRepos(out); err == nil && sameRows(got, rows) {
			return out, nil
		}
	}
	return renderRepos(rows), nil
}

// tableHeader is a line's [table] name, spaces trimmed, when it is one.
func tableHeader(l string) (string, bool) {
	t := strings.TrimSpace(l)
	if !strings.HasPrefix(t, "[") || strings.HasPrefix(t, "[[") {
		return "", false
	}
	h, _, ok := strings.Cut(t[1:], "]")
	return strings.TrimSpace(h), ok
}

// isLead: a comment line, which leads into the table after it.
func isLead(l string) bool { return strings.HasPrefix(strings.TrimSpace(l), "#") }

func sameRows(a, b map[string]Repo) bool { return len(a) == len(b) && len(DiffRepos(a, b)) == 0 }
