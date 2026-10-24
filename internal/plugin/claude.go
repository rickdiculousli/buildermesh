package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const cmdTimeout = 2 * time.Minute

// ErrNoClaude is Install's and Status's error on a box without Claude Code.
var ErrNoClaude = errors.New("claude is not on PATH")

// ClaudeCommand is the claude command, split on spaces: $BMESH_CLAUDE_COMMAND,
// else claude when it is on PATH, else nil.
func ClaudeCommand() []string {
	if c := strings.Fields(os.Getenv("BMESH_CLAUDE_COMMAND")); len(c) > 0 {
		return c
	}
	if p, err := exec.LookPath("claude"); err == nil {
		return []string{p}
	}
	return nil
}

// Run runs base plus args with a timeout and returns stdout; a failure
// names the command and its trimmed stderr.
func Run(ctx context.Context, base []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, base[0], append(append([]string{}, base[1:]...), args...)...)
	cmd.WaitDelay = 5 * time.Second
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if len(msg) > 200 {
			msg = msg[:200]
		}
		name := strings.Join(append(append([]string{}, base...), args...), " ")
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s: %s", name, msg)
	}
	return out.Bytes(), nil
}

// Marketplace is one of `claude plugin marketplace list --json`: Path for a
// directory source, Repo for a GitHub one, URL for a git one.
type Marketplace struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Path   string `json:"path"`
	Repo   string `json:"repo"`
	URL    string `json:"url"`
}

// From is what the marketplace reads, as `claude plugin marketplace add`
// takes it.
func (m Marketplace) From() string {
	switch {
	case m.Path != "":
		return m.Path
	case m.Repo != "":
		return m.Repo
	}
	return m.URL
}

// Reads reports whether m is a directory marketplace reading dir or a
// dir inside it.
func (m Marketplace) Reads(dir string) bool { return m.Path != "" && Within(m.Path, dir) }

// Is reports whether m is a directory marketplace at dir itself.
func (m Marketplace) Is(dir string) bool { return m.Path != "" && SamePath(m.Path, dir) }

// Installed is one of `claude plugin list --json`.
type Installed struct {
	ID             string `json:"id"`
	Version        string `json:"version"`
	Scope          string `json:"scope"`
	Enabled        bool   `json:"enabled"`
	ReadFromFolder string `json:"readFromFolder"`
	FolderVersion  string `json:"folderVersion"`
}

// Marketplaces lists Claude Code's marketplaces through base.
func Marketplaces(ctx context.Context, base []string) ([]Marketplace, error) {
	out, err := Run(ctx, base, "plugin", "marketplace", "list", "--json")
	if err != nil {
		return nil, err
	}
	var all []Marketplace
	if err := json.Unmarshal(out, &all); err != nil {
		return nil, fmt.Errorf("claude plugin marketplace list --json: %v", err)
	}
	return all, nil
}

// Installs lists Claude Code's installed plugins through base.
func Installs(ctx context.Context, base []string) ([]Installed, error) {
	out, err := Run(ctx, base, "plugin", "list", "--json")
	if err != nil {
		return nil, err
	}
	var all []Installed
	if err := json.Unmarshal(out, &all); err != nil {
		return nil, fmt.Errorf("claude plugin list --json: %v", err)
	}
	return all, nil
}

// State is whether Claude Code reads the plugin from dir.
type State struct {
	Marketplace *Marketplace // the buildermesh marketplace; nil = none
	Install     *Installed   // the user-scope buildermesh@buildermesh; nil = none
}

// Reads reports whether s loads the plugin from dir: the marketplace
// reads dir and the install is enabled.
func (s State) Reads(dir string) bool {
	return s.Marketplace != nil && s.Marketplace.Is(dir) && s.Install != nil && s.Install.Enabled
}

// Status reads what Claude Code has of the plugin.
func Status(ctx context.Context) (State, error) {
	base := ClaudeCommand()
	if base == nil {
		return State{}, ErrNoClaude
	}
	return status(ctx, base)
}

func status(ctx context.Context, base []string) (State, error) {
	var s State
	mks, err := Marketplaces(ctx, base)
	if err != nil {
		return s, err
	}
	for i := range mks {
		if mks[i].Name == Name {
			s.Marketplace = &mks[i]
		}
	}
	ins, err := Installs(ctx, base)
	if err != nil {
		return s, err
	}
	for i := range ins {
		if ins[i].ID == ID && ins[i].Scope == "user" {
			s.Install = &ins[i]
		}
	}
	return s, nil
}

// Install points Claude Code at dir, which holds the written plugin: it
// removes a buildermesh marketplace that reads anything else, adds dir as
// the buildermesh marketplace, installs buildermesh@buildermesh at user
// scope (or enables it) and checks the install reads dir. It returns one
// line per change, none when Claude Code already read dir. A failure after
// the old marketplace was removed says how to put it back.
func Install(ctx context.Context, dir string) ([]string, error) {
	base := ClaudeCommand()
	if base == nil {
		return nil, ErrNoClaude
	}
	s, err := status(ctx, base)
	if err != nil {
		return nil, err
	}
	var lines []string
	var old *Marketplace
	if m := s.Marketplace; m != nil && !m.Is(dir) {
		if _, err := Run(ctx, base, "plugin", "marketplace", "remove", Name); err != nil {
			return lines, err
		}
		old = m
		lines = append(lines, fmt.Sprintf("removed marketplace %s (%s)", Name, m.From()))
		s = State{}
	}
	back := func(err error) error {
		if old == nil {
			return err
		}
		return fmt.Errorf("%v; to put the old marketplace back: claude plugin marketplace add %s && claude plugin install %s --scope user",
			err, old.From(), ID)
	}
	if s.Marketplace == nil {
		if _, err := Run(ctx, base, "plugin", "marketplace", "add", dir); err != nil {
			return lines, back(err)
		}
		lines = append(lines, fmt.Sprintf("added marketplace %s (%s)", Name, dir))
	}
	switch {
	case s.Install == nil:
		if _, err := Run(ctx, base, "plugin", "install", ID, "--scope", "user"); err != nil {
			return lines, back(err)
		}
		lines = append(lines, "installed "+ID+" (user scope)")
	case !s.Install.Enabled:
		if _, err := Run(ctx, base, "plugin", "enable", ID, "--scope", "user"); err != nil {
			return lines, back(err)
		}
		lines = append(lines, "enabled "+ID+" (user scope)")
	}
	if s, err = status(ctx, base); err != nil {
		return lines, back(err)
	}
	if !s.Reads(dir) {
		return lines, back(fmt.Errorf("claude plugin list shows no enabled %s from %s", ID, dir))
	}
	return lines, nil
}

// resolve returns p with symlinks resolved, or cleaned when it can't be.
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// SamePath reports whether a and b name one dir, symlinks resolved.
func SamePath(a, b string) bool { return resolve(a) == resolve(b) }

// Within reports whether p is dir or inside it.
func Within(p, dir string) bool {
	p, dir = resolve(p), resolve(dir)
	if p == dir {
		return true
	}
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
