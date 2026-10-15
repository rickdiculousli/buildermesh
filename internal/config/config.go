// Package config reads bmesh's config.toml: schema, discovery and defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type MeshConfig struct {
	NATS      string
	Domain    string
	TokenFile string `toml:"token_file"`
	Store     string
	Listen    string
}

type HostConfig struct{ Name string }

type ServeConfig struct{ Listen string }

type RunnerConfig struct {
	Kind        string
	T3Dispatch  string `toml:"t3_dispatch"`
	TmuxSession string `toml:"tmux_session"`
	Worktrees   string
}

type NotifyConfig struct {
	Ntfy        string
	Command     string
	DigestEvery time.Duration
}

type LivenessConfig struct {
	Grace     time.Duration
	PollEvery time.Duration
	WakeEvery time.Duration
}

type SpecConfig struct{ Glob string }

type ModelsConfig struct{ Lead string }

// Config is the parsed config.toml. Path-valued fields are stored as written;
// callers pass them through Expand.
type Config struct {
	Mesh     MeshConfig
	Host     HostConfig
	Serve    ServeConfig
	Runner   RunnerConfig
	Notify   NotifyConfig
	Liveness LivenessConfig
	Repos    map[string]string
	Spec     SpecConfig
	Models   ModelsConfig

	dir string // directory of the config file; "" when none was read
}

// duration decodes strings like "20m" from TOML.
type duration time.Duration

func (d *duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = duration(v)
	return nil
}

// file mirrors the TOML layout; durations use the text-decoding type.
type file struct {
	Mesh   MeshConfig `toml:"mesh"`
	Host   HostConfig `toml:"host"`
	Serve  ServeConfig
	Runner RunnerConfig
	Notify struct {
		Ntfy        string
		Command     string
		DigestEvery duration `toml:"digest_every"`
	}
	Liveness struct {
		Grace     duration
		PollEvery duration `toml:"poll_every"`
		WakeEvery duration `toml:"wake_every"`
	}
	Repos  map[string]string
	Spec   SpecConfig
	Models ModelsConfig
}

func defaults() *Config {
	return &Config{
		Mesh:  MeshConfig{Domain: "hub", Store: "~/.local/state/bmesh/jetstream", Listen: "127.0.0.1:4222"},
		Serve: ServeConfig{Listen: "127.0.0.1:3774"},
		Runner: RunnerConfig{Kind: "t3", T3Dispatch: "t3_dispatch.py", TmuxSession: "bmesh",
			Worktrees: "~/.bmesh/worktrees"},
		Notify:   NotifyConfig{DigestEvery: time.Hour},
		Liveness: LivenessConfig{Grace: 20 * time.Minute, PollEvery: 60 * time.Second, WakeEvery: 10 * time.Minute},
		Repos:    map[string]string{},
		Spec:     SpecConfig{Glob: "docs/proposals/{id}-*.md"},
		Models:   ModelsConfig{Lead: "claude-fable-5-1"},
	}
}

// Path is $BMESH_CONFIG, else ~/.config/bmesh/config.toml.
func Path() string {
	if p := os.Getenv("BMESH_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "bmesh", "config.toml")
}

// Load reads path over the defaults. A missing file yields the defaults;
// unknown keys are ignored.
func Load(path string) (*Config, error) {
	c := defaults()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return c, nil
	}
	f := file{
		Mesh: c.Mesh, Host: c.Host, Serve: c.Serve, Runner: c.Runner, Spec: c.Spec, Models: c.Models,
	}
	f.Notify.DigestEvery = duration(c.Notify.DigestEvery)
	f.Liveness.Grace = duration(c.Liveness.Grace)
	f.Liveness.PollEvery = duration(c.Liveness.PollEvery)
	f.Liveness.WakeEvery = duration(c.Liveness.WakeEvery)
	if _, err := toml.DecodeFile(path, &f); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	c.Mesh, c.Host, c.Serve, c.Runner, c.Spec, c.Models = f.Mesh, f.Host, f.Serve, f.Runner, f.Spec, f.Models
	c.Notify = NotifyConfig{Ntfy: f.Notify.Ntfy, Command: f.Notify.Command, DigestEvery: time.Duration(f.Notify.DigestEvery)}
	c.Liveness = LivenessConfig{Grace: time.Duration(f.Liveness.Grace), PollEvery: time.Duration(f.Liveness.PollEvery),
		WakeEvery: time.Duration(f.Liveness.WakeEvery)}
	if f.Repos != nil {
		c.Repos = f.Repos
	}
	if abs, err := filepath.Abs(path); err == nil {
		c.dir = filepath.Dir(abs)
	}
	return c, nil
}

// HostName is [host] name, else the short host name, lowercased.
func (c *Config) HostName() string {
	n := c.Host.Name
	if n == "" {
		n, _ = os.Hostname()
		n, _, _ = strings.Cut(n, ".")
	}
	return strings.ToLower(n)
}

// Token reads [mesh] token_file; "" when none is configured.
func (c *Config) Token() (string, error) {
	if c.Mesh.TokenFile == "" {
		return "", nil
	}
	b, err := os.ReadFile(c.Expand(c.Mesh.TokenFile))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// Expand replaces a leading ~ with the home directory.
func (c *Config) Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// RepoPath resolves a [repos] key, else an existing directory, to (name, abs path).
// A relative [repos] path resolves against the directory of the config file
// (the current directory when no file was read); a relative nameOrPath
// resolves against the current directory.
func (c *Config) RepoPath(nameOrPath string) (name, path string, err error) {
	if p, ok := c.Repos[nameOrPath]; ok {
		p = c.Expand(p)
		if !filepath.IsAbs(p) && c.dir != "" {
			p = filepath.Join(c.dir, p)
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", "", err
		}
		return nameOrPath, abs, nil
	}
	p := c.Expand(nameOrPath)
	if st, serr := os.Stat(p); serr == nil && st.IsDir() {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", "", err
		}
		return filepath.Base(abs), abs, nil
	}
	return "", "", fmt.Errorf("unknown repo %q: not a [repos] name or an existing directory", nameOrPath)
}
