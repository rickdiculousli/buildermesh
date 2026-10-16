package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// runInit runs the CLI with stdin lines, config at cfg, from dir.
func runInit(t *testing.T, cfg, dir, stdin string, args ...string) (string, string, int) {
	t.Helper()
	t.Setenv("BMESH_CONFIG", cfg)
	t.Setenv("BMESH_NODE", "")
	t.Chdir(dir)
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
	return out.String(), errb.String(), code
}

func TestInitSingleBoxWritesConfigRepoAndFlag(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "cfg", "config.toml")
	repo := filepath.Join(root, "app")
	meshtest.Git(t, root, "init", "-b", "main", repo)
	meshtest.Git(t, repo, "remote", "add", "origin", "git@github.com:owner/app.git")
	// Every answer is the default: single box, register the repo.
	out, errs, code := runInit(t, cfg, repo, "", "init", "--runner", "claude")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	c, err := config.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c.Mesh.NATS != "" || c.Runner.Kind != "claude" {
		t.Fatalf("config %+v", c.Mesh)
	}
	r := c.Repos["app"]
	if r.Path != repo || r.Remote != "owner/app" || r.Mode != "ship" {
		t.Fatalf("row %+v", r)
	}
	if _, err := os.Stat(config.SetupFlag(cfg)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Single box", "wrote " + cfg, "wrote " + config.ReposPath(cfg), "bmesh serve", "bmesh repos"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// A second init edits nothing without --force, and says so.
	os.WriteFile(cfg, []byte("[runner]\nkind = \"tmux\"\n"), 0o600)
	out, _, code = runInit(t, cfg, repo, "", "init")
	if code != 0 || !strings.Contains(out, "exists; leaving it") || !strings.Contains(out, "app is already in") {
		t.Fatalf("second init: %d %q", code, out)
	}
	if b, _ := os.ReadFile(cfg); !strings.Contains(string(b), "tmux") {
		t.Fatalf("config rewritten: %s", b)
	}
	// --force rewrites both.
	out, _, code = runInit(t, cfg, repo, "", "init", "--force", "--runner", "t3")
	if code != 0 {
		t.Fatalf("force: %d %q", code, out)
	}
	if c, _ := config.Load(cfg); c.Runner.Kind != "t3" {
		t.Fatalf("force kept the old config: %+v", c.Runner)
	}
}

// A checkout whose directory name is not a registry name is skipped with a
// note; init still completes.
func TestInitSkipsUnregisterableDirectory(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config.toml")
	repo := filepath.Join(root, "my repo")
	meshtest.Git(t, root, "init", "-b", "main", repo)
	out, errs, code := runInit(t, cfg, repo, "", "init", "--runner", "claude")
	if code != 0 || !strings.Contains(out, "not registering "+repo) {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	if _, err := os.Stat(config.SetupFlag(cfg)); err != nil {
		t.Fatal(err)
	}
	if c, _ := config.Load(cfg); len(c.Repos) != 0 {
		t.Fatalf("%v", c.Repos)
	}
}

func TestInitMultiBoxFromStdinAndFlags(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config.toml")
	// Answers typed: multi, the hub, the token file, then no to registering.
	out, _, code := runInit(t, cfg, root, "Multi\nnats://User:S3cret@hub:4222\n/home/Rick/Hub.token\n", "init", "--runner", "tmux", "--no-repo")
	if code != 0 {
		t.Fatalf("%d %q", code, out)
	}
	c, err := config.Load(cfg)
	if err != nil || c.Mesh.NATS != "nats://User:S3cret@hub:4222" || c.Mesh.TokenFile != "/home/Rick/Hub.token" || c.Runner.Kind != "tmux" || len(c.Repos) != 0 {
		t.Fatalf("%v %+v %v", err, c.Mesh, c.Repos)
	}
	// The same through flags, nothing asked about the hub.
	cfg2 := filepath.Join(root, "two", "config.toml")
	out, _, code = runInit(t, cfg2, root, "", "init", "--hub", "nats://h:1", "--token-file", "/t", "--runner", "claude", "--no-repo")
	if code != 0 || strings.Contains(out, "Hub URL") || strings.Contains(out, "Token file") {
		t.Fatalf("%d %q", code, out)
	}
	if c, _ := config.Load(cfg2); c.Mesh.NATS != "nats://h:1" || c.Mesh.TokenFile != "/t" {
		t.Fatalf("%+v", c.Mesh)
	}
	// A token file given by flag survives the multi answer at the prompt.
	cfg3 := filepath.Join(root, "three", "config.toml")
	if _, _, code := runInit(t, cfg3, root, "multi\nnats://h:2\n", "init", "--token-file", "/tok3", "--runner", "claude", "--no-repo"); code != 0 {
		t.Fatalf("code %d", code)
	}
	if c, _ := config.Load(cfg3); c.Mesh.NATS != "nats://h:2" || c.Mesh.TokenFile != "/tok3" {
		t.Fatalf("%+v", c.Mesh)
	}
	// An unknown runner is a usage error.
	if _, _, code := runInit(t, filepath.Join(root, "three.toml"), root, "", "init", "--runner", "codex"); code != exitUsage {
		t.Fatalf("code %d", code)
	}
}

func TestInitServiceWritesUnit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd")
	}
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg-cache"))
	cfg := filepath.Join(root, "config.toml")
	out, _, code := runInit(t, cfg, root, "", "init", "--service", "--no-repo", "--runner", "claude")
	if code != 0 {
		t.Fatalf("%d %q", code, out)
	}
	unit := filepath.Join(root, "xdg", "systemd", "user", "bmesh.service")
	b, err := os.ReadFile(unit)
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	if !strings.Contains(string(b), "ExecStart="+exe+" serve") || !strings.Contains(string(b), "BMESH_CONFIG="+cfg) ||
		!strings.Contains(string(b), "KillMode=process\n") ||
		!strings.Contains(string(b), "Environment=XDG_CACHE_HOME="+filepath.Join(root, "xdg-cache")+"\n") {
		t.Fatalf("unit:\n%s", b)
	}
	if !strings.Contains(out, "systemctl --user enable --now bmesh") || !strings.Contains(out, "wrote "+unit) {
		t.Fatalf("out %q", out)
	}
}

func TestCommandsRefuseUntilSetUp(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	// No flag, no config, stdin not a terminal: refused with the init hint.
	_, errs, code := runInit(t, cfg, t.TempDir(), "", "repos")
	if code != exitRefused || !strings.Contains(errs, "run `bmesh init`") {
		t.Fatalf("%d %q", code, errs)
	}
	// --config given after the command counts too.
	t.Setenv("BMESH_CONFIG", "")
	var out, errb bytes.Buffer
	if code := run([]string{"repos", "--config", cfg}, strings.NewReader(""), &out, &errb); code != exitRefused {
		t.Fatalf("%d %q", code, errb.String())
	}
	// A config installed by hand counts as set up.
	os.WriteFile(cfg, []byte(""), 0o600)
	if _, errs, code := runInit(t, cfg, t.TempDir(), "", "repos"); code != 0 {
		t.Fatalf("%d %q", code, errs)
	}
	// help never needs it.
	if _, _, code := runInit(t, filepath.Join(t.TempDir(), "none.toml"), t.TempDir(), "", "help"); code != 0 {
		t.Fatalf("help %d", code)
	}
}
