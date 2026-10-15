package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileIsDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Mesh.Domain != "hub" || c.Liveness.Grace != 20*time.Minute ||
		c.Serve.Listen != "127.0.0.1:3774" || c.Runner.Kind != "t3" {
		t.Fatalf("defaults wrong: %+v", c)
	}
	if c.Mesh.Listen != "127.0.0.1:4222" || c.Liveness.PollEvery != 60*time.Second ||
		c.Liveness.WakeEvery != 10*time.Minute || c.Notify.DigestEvery != time.Hour ||
		c.Spec.Glob != "docs/proposals/{id}-*.md" || c.Models.Lead != "claude-fable-5-1" ||
		c.Runner.TmuxSession != "bmesh" || c.Runner.T3Dispatch != "t3_dispatch.py" ||
		c.Runner.Checkouts != "~/.bmesh/checkouts" || c.Runner.Claude != "claude" {
		t.Fatalf("defaults wrong: %+v", c)
	}
}

func TestLoadFixture(t *testing.T) {
	c, err := Load("../../testdata/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string][2]string{
		"nats":        {c.Mesh.NATS, "nats://hub.example:4222"},
		"domain":      {c.Mesh.Domain, "lab"},
		"token_file":  {c.Mesh.TokenFile, "~/secrets/bm.token"},
		"store":       {c.Mesh.Store, "~/state/bm/js"},
		"mesh.listen": {c.Mesh.Listen, "127.0.0.1:4333"},
		"host":        {c.Host.Name, "Box-One"},
		"serve":       {c.Serve.Listen, "127.0.0.1:3999"},
		"kind":        {c.Runner.Kind, "tmux"},
		"t3":          {c.Runner.T3Dispatch, "~/bin/t3_dispatch.py"},
		"tmux":        {c.Runner.TmuxSession, "mesh"},
		"worktrees":   {c.Runner.Worktrees, "~/wt"},
		"checkouts":   {c.Runner.Checkouts, "~/co"},
		"claude":      {c.Runner.Claude, "claude --debug"},
		"ntfy":        {c.Notify.Ntfy, "http://ntfy.example/bm"},
		"command":     {c.Notify.Command, "cat"},
		"glob":        {c.Spec.Glob, "specs/{id}.md"},
		"lead":        {c.Models.Lead, "lead-model"},
		"repos.demo":  {c.Repos["demo"], "demo"},
		"hostname":    {c.HostName(), "box-one"},
	}
	for k, v := range checks {
		if v[0] != v[1] {
			t.Errorf("%s = %q, want %q", k, v[0], v[1])
		}
	}
	if c.Notify.DigestEvery != 30*time.Minute || c.Liveness.Grace != 5*time.Minute ||
		c.Liveness.PollEvery != 15*time.Second || c.Liveness.WakeEvery != 2*time.Minute {
		t.Errorf("durations wrong: %+v %+v", c.Notify, c.Liveness)
	}
}

func TestLoadUnknownKeysAndBadDuration(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[future]\nx = 1\n[liveness]\ngrace = \"1h\"\n"), 0o644)
	c, err := Load(p)
	if err != nil || c.Liveness.Grace != time.Hour {
		t.Fatalf("%v %+v", err, c)
	}
	os.WriteFile(p, []byte("[liveness]\ngrace = \"soon\"\n"), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("bad duration accepted")
	}
}

func TestRepoPathByNameAndByPath(t *testing.T) {
	c, err := Load("../../testdata/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs("../../testdata/demo")
	name, path, err := c.RepoPath("demo")
	if err != nil || name != "demo" || path != abs {
		t.Fatalf("by name: %q %q %v", name, path, err)
	}
	name, path, err = c.RepoPath(abs)
	if err != nil || name != "demo" || path != abs {
		t.Fatalf("by path: %q %q %v", name, path, err)
	}
	if _, _, err := c.RepoPath(filepath.Join(abs, "nope")); err == nil {
		t.Fatal("missing dir accepted")
	}
}

func TestExpandAndToken(t *testing.T) {
	c := &Config{}
	home, _ := os.UserHomeDir()
	if got := c.Expand("~/x"); got != filepath.Join(home, "x") {
		t.Fatalf("got %q", got)
	}
	if got := c.Expand("/a/b"); got != "/a/b" {
		t.Fatalf("got %q", got)
	}
	if tok, err := c.Token(); tok != "" || err != nil {
		t.Fatalf("%q %v", tok, err)
	}
	p := filepath.Join(t.TempDir(), "tok")
	os.WriteFile(p, []byte("secret\n"), 0o600)
	c.Mesh.TokenFile = p
	if tok, err := c.Token(); tok != "secret" || err != nil {
		t.Fatalf("%q %v", tok, err)
	}
}
