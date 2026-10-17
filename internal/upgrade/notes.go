// Package upgrade reads a release's upgrade notes, docs/upgrading/<tag>.toml
// in bmesh's own repo: the steps an install from the release before needs,
// distilled by the coordinator from the Upgrading sections of the specs the
// release carries. `bmesh release` checks them and renders them into the
// tag; `bmesh update` walks a box through them.
package upgrade

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

// Dir is where a release's notes live in bmesh's own repo.
const Dir = "docs/upgrading"

// Path is the notes file of release tag, repo-relative.
func Path(tag string) string { return Dir + "/" + tag + ".toml" }

// Step kinds.
const (
	Ask   = "ask"   // a config or registry key to set: a question with choices and a default
	Run   = "run"   // a command to run
	Check = "check" // a command whose output must show something
)

// Files an ask writes.
const (
	FileConfig   = "config"   // config.toml: key is `[section] key`
	FileRegistry = "registry" // repos.toml, every row: key is the row's key
)

// OSes a run step names.
var OSes = []string{"linux", "darwin", "windows"}

// Step is one step of a release's notes.
type Step struct {
	Kind string `toml:"kind" json:"kind"`
	// ask
	Key      string   `toml:"key" json:"key,omitempty"`
	File     string   `toml:"file" json:"file,omitempty"`
	Changed  string   `toml:"changed" json:"changed,omitempty"`
	Question string   `toml:"question" json:"question,omitempty"`
	Choices  []string `toml:"choices" json:"choices,omitempty"`
	Default  string   `toml:"default" json:"default,omitempty"`
	// run and check
	Command string   `toml:"command" json:"command,omitempty"`
	Why     string   `toml:"why" json:"why,omitempty"`
	OS      []string `toml:"os" json:"os,omitempty"`
	Confirm bool     `toml:"confirm" json:"confirm,omitempty"`
	Expect  string   `toml:"expect" json:"expect,omitempty"` // check: what its output must show
	// any
	When string `toml:"when" json:"when,omitempty"`
}

// Notes is one release's notes.
type Notes struct {
	Release   string `toml:"-"`
	Summary   string `toml:"summary"`    // one line: what the release asks of an install
	StartOver bool   `toml:"start_over"` // the steps uninstall and set the box up again
	Loses     string `toml:"loses"`      // what a start-over loses
	Steps     []Step `toml:"step"`
}

// Parse reads release tag's notes from text: every step a known kind with
// the fields it needs, every condition readable. Keys are checked
// separately (Unknown), against the binary that reads them.
func Parse(tag, text string) (*Notes, error) {
	n := &Notes{Release: tag}
	md, err := toml.Decode(text, n)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", Path(tag), err)
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return nil, fmt.Errorf("%s: unknown key %s", Path(tag), keys[0])
	}
	for i := range n.Steps {
		s := &n.Steps[i]
		if s.Kind == Ask && s.File == "" {
			s.File = FileConfig
		}
		if err := s.check(); err != nil {
			return nil, fmt.Errorf("%s: step %d (%s): %w", Path(tag), i+1, s.Kind, err)
		}
	}
	return n, nil
}

func (s *Step) check() error {
	if _, err := ParseWhen(s.When); err != nil {
		return err
	}
	switch s.Kind {
	case Ask:
		switch {
		case s.File != FileConfig && s.File != FileRegistry:
			return fmt.Errorf("file %q: want config or registry", s.File)
		case s.Key == "":
			return errors.New("no key")
		case s.Question == "":
			return errors.New("no question")
		case s.Default == "":
			return errors.New("no default: every ask has one, which --defaults takes")
		case len(s.Choices) > 0 && !slices.Contains(s.Choices, s.Default):
			return fmt.Errorf("default %q is not one of the choices", s.Default)
		case s.Command != "" || s.Confirm || s.Expect != "" || len(s.OS) > 0:
			return errors.New("an ask takes no command, os, confirm or expect")
		}
		if s.File == FileConfig {
			if _, _, ok := ParseKey(s.Key); !ok {
				return fmt.Errorf("key %q: want [section] key", s.Key)
			}
		}
	case Run, Check:
		if s.Command == "" {
			return errors.New("no command")
		}
		if s.Kind == Run && s.Why == "" {
			return errors.New("no why")
		}
		if s.Kind == Run && s.Expect != "" {
			return errors.New("expect is a check's")
		}
		if s.Key != "" || s.Question != "" || len(s.Choices) > 0 || s.Default != "" || s.Changed != "" {
			return errors.New("only an ask takes key, question, choices, default or changed")
		}
		for _, o := range s.OS {
			if !slices.Contains(OSes, o) {
				return fmt.Errorf("os %q: want %s", o, strings.Join(OSes, ", "))
			}
		}
	default:
		return fmt.Errorf("kind %q: want ask, run or check", s.Kind)
	}
	return nil
}

// keyRE is `[section] key`.
var keyRE = regexp.MustCompile(`^\[\s*([A-Za-z0-9_]+)\s*\]\s*([A-Za-z0-9_]+)$`)

// ParseKey splits `[section] key`.
func ParseKey(s string) (section, key string, ok bool) {
	m := keyRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// Unknown is the keys n's asks and conditions name that this binary's
// config.toml or repos.toml doesn't have, each as written.
func (n *Notes) Unknown() []string {
	var out []string
	add := func(k string) {
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	for _, s := range n.Steps {
		if s.Kind == Ask {
			if s.File == FileRegistry {
				if !config.KnownRepoKey(s.Key) {
					add(s.Key)
				}
			} else if sec, k, _ := ParseKey(s.Key); !config.Known(sec, k) {
				add(s.Key)
			}
		}
		w, _ := ParseWhen(s.When)
		for _, c := range w {
			if !config.Known(c.Section, c.Key) {
				add("[" + c.Section + "] " + c.Key)
			}
		}
	}
	return out
}

// Applies reports whether s runs on goos: an ask always, a run or check
// when it names no OS or names goos.
func (s Step) Applies(goos string) bool { return len(s.OS) == 0 || slices.Contains(s.OS, goos) }

// Label is the ask's short name: the key without its section.
func (s Step) Label() string {
	if _, k, ok := ParseKey(s.Key); ok {
		return k
	}
	return s.Key
}

// Text renders n for a person, as the tag message carries it: the summary,
// what a start-over loses, then one line per step.
func (n *Notes) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Upgrading to %s (%s):\n", n.Release, Path(n.Release))
	if n.Summary != "" {
		fmt.Fprintf(&b, "%s\n", n.Summary)
	}
	if n.Loses != "" {
		fmt.Fprintf(&b, "It loses: %s\n", n.Loses)
	}
	for i, s := range n.Steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, s.Line())
	}
	return strings.TrimRight(b.String(), "\n")
}

// Line is s on one line.
func (s Step) Line() string {
	var line string
	switch s.Kind {
	case Ask:
		choices := s.Default
		if len(s.Choices) > 0 {
			choices = strings.Join(s.Choices, "/") + ", default " + s.Default
		}
		where := s.Key
		if s.File == FileRegistry {
			where = "repos.toml " + s.Key
		}
		line = fmt.Sprintf("ask %s: %s (%s)", where, strings.TrimSpace(s.Changed+" "+s.Question), choices)
	case Run:
		line = fmt.Sprintf("%s: `%s`", s.Why, s.Command)
		if s.Confirm {
			line += " (asks first)"
		}
	case Check:
		line = fmt.Sprintf("check `%s`", s.Command)
		if s.Expect != "" {
			line += fmt.Sprintf(" shows %q", s.Expect)
		}
		if s.Why != "" {
			line += ": " + s.Why
		}
	}
	if len(s.OS) > 0 {
		line += " [" + strings.Join(s.OS, ", ") + "]"
	}
	if s.When != "" {
		line += " when " + s.When
	}
	return line
}
