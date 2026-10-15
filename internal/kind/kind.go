// Package kind reads routine kind files: the TOML a repo keeps under
// .bmesh/routines/ to say how a routine runs.
package kind

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// ErrNotFound is wrapped by LoadAt when the kind file is absent at the commit.
var ErrNotFound = errors.New("kind not found")

const (
	defaultDir = ".bmesh/routines"
	configFile = ".bmesh/config.toml"
)

// NameRE is the grammar of kind and slot names.
var NameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// trailingComment matches a shell comment: a # starting a word.
var trailingComment = regexp.MustCompile(`(^|\s)#`)

// Kind is a parsed kind file; Name is the file name without .toml.
type Kind struct {
	Name           string
	Mode           string // inline | fresh | pooled
	Deterministic  bool
	Slots          []string
	Timeout        time.Duration
	Command        string
	Model          string
	Models         []string
	Prompt         string
	Allow          []string
	Pool           int
	BatchIdle      time.Duration
	RecycleIdle    time.Duration
	RecycleUses    int
	RecycleContext int
}

// ModelsList is [Model] when Model is set, else Models.
func (k Kind) ModelsList() []string {
	if k.Model != "" {
		return []string{k.Model}
	}
	return k.Models
}

// IsModel reports whether the kind runs a model rather than a command.
func (k Kind) IsModel() bool { return k.Model != "" || len(k.Models) > 0 }

type duration time.Duration

func (d *duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = duration(v)
	return nil
}

type kindFile struct {
	Mode           string
	Deterministic  bool
	Slots          []string
	Timeout        duration
	Command        string
	Model          string
	Models         []string
	Prompt         string
	Allow          []string
	Pool           int
	BatchIdle      duration `toml:"batch_idle"`
	RecycleUses    int      `toml:"recycle_uses"`
	RecycleContext int      `toml:"recycle_context"`
	RecycleIdle    duration `toml:"recycle_idle"`
}

// Parse decodes a kind file's text over the defaults and validates it.
func Parse(name string, data []byte) (Kind, error) {
	if !NameRE.MatchString(name) {
		return Kind{}, fmt.Errorf("kind name %q: want lowercase letters, digits and hyphens", name)
	}
	f := kindFile{
		Mode:           "pooled",
		Timeout:        duration(20 * time.Minute),
		Pool:           1,
		BatchIdle:      duration(30 * time.Second),
		RecycleUses:    20,
		RecycleContext: 120000,
		RecycleIdle:    duration(55 * time.Minute),
	}
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return Kind{}, fmt.Errorf("kind %s: %w", name, err)
	}
	if un := md.Undecoded(); len(un) > 0 {
		return Kind{}, fmt.Errorf("kind %s: unknown key %s", name, un[0])
	}
	switch f.Mode {
	case "inline", "fresh", "pooled":
	default:
		return Kind{}, fmt.Errorf("kind %s: mode %q: want inline, fresh or pooled", name, f.Mode)
	}
	f.Command = strings.TrimRight(f.Command, " \t\r\n;")
	if lines := strings.Split(f.Command, "\n"); trailingComment.MatchString(lines[len(lines)-1]) {
		return Kind{}, fmt.Errorf(`kind %s: command must not end in a comment: args are appended as "$@"`, name)
	}
	hasModel := f.Model != "" || len(f.Models) > 0
	switch {
	case f.Command != "" && hasModel:
		return Kind{}, fmt.Errorf("kind %s: set command or model(s), not both", name)
	case f.Model != "" && len(f.Models) > 0:
		return Kind{}, fmt.Errorf("kind %s: set model or models, not both", name)
	case f.Command == "" && !hasModel:
		return Kind{}, fmt.Errorf("kind %s: set one of command, model, models", name)
	}
	if f.Pool < 1 {
		return Kind{}, fmt.Errorf("kind %s: pool must be at least 1", name)
	}
	return Kind{
		Name: name, Mode: f.Mode, Deterministic: f.Deterministic, Slots: f.Slots,
		Timeout: time.Duration(f.Timeout), Command: f.Command, Model: f.Model, Models: f.Models,
		Prompt: f.Prompt, Allow: f.Allow, Pool: f.Pool, BatchIdle: time.Duration(f.BatchIdle),
		RecycleIdle: time.Duration(f.RecycleIdle), RecycleUses: f.RecycleUses, RecycleContext: f.RecycleContext,
	}, nil
}

// RepoConfig is the part of a repo's .bmesh/config.toml that routines read.
type RepoConfig struct {
	Slots       map[string]int
	RoutinesDir string
}

func parseRepoConfig(data []byte) (RepoConfig, error) {
	rc := RepoConfig{Slots: map[string]int{}, RoutinesDir: defaultDir}
	if data == nil {
		return rc, nil
	}
	var f struct {
		Slots    map[string]int
		Routines struct{ Dir string }
	}
	if _, err := toml.Decode(string(data), &f); err != nil {
		return RepoConfig{}, fmt.Errorf("%s: %w", configFile, err)
	}
	if f.Slots != nil {
		rc.Slots = f.Slots
	}
	if f.Routines.Dir != "" {
		rc.RoutinesDir = f.Routines.Dir
	}
	return rc, nil
}

// LoadRepoConfig reads <repoPath>/.bmesh/config.toml; a missing file yields the defaults.
func LoadRepoConfig(repoPath string) (RepoConfig, error) {
	b, err := os.ReadFile(filepath.Join(repoPath, filepath.FromSlash(configFile)))
	if errors.Is(err, os.ErrNotExist) {
		return parseRepoConfig(nil)
	}
	if err != nil {
		return RepoConfig{}, err
	}
	return parseRepoConfig(b)
}

// LoadDir reads every kind file in the repo's routines directory, sorted by name.
// A missing or empty directory is not an error.
func LoadDir(repoPath string) ([]Kind, error) {
	rc, err := LoadRepoConfig(repoPath)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(repoPath, filepath.FromSlash(rc.RoutinesDir))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Kind
	for _, e := range entries {
		n, ok := strings.CutSuffix(e.Name(), ".toml")
		if !ok || e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		k, err := Parse(n, b)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// LoadAt reads kind name as committed at commit, never from the working copy.
// A kind file absent at the commit yields an error wrapping ErrNotFound.
func LoadAt(repoPath, commit, name string) (Kind, error) {
	if !NameRE.MatchString(name) {
		return Kind{}, fmt.Errorf("kind name %q: want lowercase letters, digits and hyphens", name)
	}
	sha, err := resolveCommit(repoPath, commit)
	if err != nil {
		return Kind{}, err
	}
	var cfg []byte
	switch b, err := gitShow(repoPath, sha, configFile); {
	case err == nil:
		cfg = []byte(b)
	case !errors.Is(err, ErrNotFound):
		return Kind{}, err
	}
	rc, err := parseRepoConfig(cfg)
	if err != nil {
		return Kind{}, err
	}
	data, err := gitShow(repoPath, sha, path.Join(rc.RoutinesDir, name+".toml"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Kind{}, fmt.Errorf("kind %s at %s: %w", name, commit, ErrNotFound)
		}
		return Kind{}, err
	}
	return Parse(name, []byte(data))
}

// resolveCommit turns a caller-supplied commit into a SHA, so a value that
// looks like an option can never reach git show.
func resolveCommit(repo, commit string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", repo, "rev-parse", "--verify", "--end-of-options", commit+"^{commit}").Output()
	if err != nil {
		return "", fmt.Errorf("commit %q: not a commit in %s", commit, repo)
	}
	return strings.TrimSpace(string(out)), nil
}

func gitShow(repo, commit, file string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "show", commit+":"+file)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err == nil {
		return string(out), nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		stderr := strings.TrimSpace(string(ee.Stderr))
		if strings.Contains(stderr, "does not exist in") || strings.Contains(stderr, "exists on disk, but not in") {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("git show %s:%s: %w: %s", commit, file, err, stderr)
	}
	return "", fmt.Errorf("git show %s:%s: %w", commit, file, err)
}
