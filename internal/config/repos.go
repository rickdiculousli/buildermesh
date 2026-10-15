package config

import (
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

var repoName = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)

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
	out := map[string]Repo{}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	var f reposFile
	md, err := toml.DecodeFile(path, &f)
	if err != nil {
		return nil, fmt.Errorf("repos %s: %w", path, err)
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return nil, fmt.Errorf("repos %s: unknown key %s", path, keys[0])
	}
	for name, r := range f.Repos {
		if !repoName.MatchString(name) {
			return nil, fmt.Errorf("repos %s: [repos.%s]: a name matches %s", path, name, repoName)
		}
		r.Name = name
		if err := r.check(); err != nil {
			return nil, fmt.Errorf("repos %s: [repos.%s]: %w", path, name, err)
		}
		out[name] = r
	}
	return out, nil
}

// check fills a row's defaults and refuses what it can't read.
func (r *Repo) check() error {
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
	return nil
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

// WriteRepo adds or replaces one row in repos.toml, keeping the other rows.
// The file is rewritten whole, sorted by name, so a comment in it is lost.
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
	names := make([]string, 0, len(repos))
	for n := range repos {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("# bmesh's repo registry: one [repos.<name>] per repo (see README.md, Config).\n")
	for _, n := range names {
		r := repos[n]
		fmt.Fprintf(&b, "\n[repos.%s]\n", n)
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
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

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
