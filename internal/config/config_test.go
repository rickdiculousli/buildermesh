package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		c.Spec.Glob != "docs/proposals/{id}-*.md" || c.Models != (ModelsConfig{Lead: "claude-opus-5-5", LeadEffort: "high", Proposer: "claude-opus-5-5", ProposerEffort: "xhigh"}) ||
		c.Runner.TmuxSession != "bmesh" || c.Runner.T3Server != "http://127.0.0.1:3773" ||
		c.Runner.Checkouts != "~/.bmesh/checkouts" || c.Runner.Claude != "claude" {
		t.Fatalf("defaults wrong: %+v", c)
	}
	if c.Quota.Command != "" || c.Update.Repo != "" || c.Console.ThreadURL != "" || c.Console.Hosts == nil || len(c.Console.Hosts) != 0 {
		t.Fatalf("defaults wrong: %+v %+v", c.Quota, c.Console)
	}
	if fmt.Sprint(c.Notify.Push) != "[ask blocked done]" || c.Notify.MapURL != "" || c.Notify.CatchUp != time.Hour {
		t.Fatalf("notify defaults wrong: %+v", c.Notify)
	}
}

func TestLoadPushList(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	// [notify] without push keeps the default list.
	os.WriteFile(p, []byte("[notify]\nntfy = \"http://n/x\"\n"), 0o644)
	c, err := Load(p)
	if err != nil || fmt.Sprint(c.Notify.Push) != "[ask blocked done]" || c.Notify.CatchUp != time.Hour {
		t.Fatalf("%v %+v", err, c.Notify)
	}
	// An empty list turns pushes off rather than meaning the default.
	os.WriteFile(p, []byte("[notify]\npush = []\n"), 0o644)
	c, err = Load(p)
	if err != nil || c.Notify.Push == nil || len(c.Notify.Push) != 0 {
		t.Fatalf("push = []: %v %#v", err, c.Notify.Push)
	}
	// An event outside ask, blocked and done is an error, not a silent no-op.
	os.WriteFile(p, []byte("[notify]\npush = [\"asks\", \"done\"]\n"), 0o644)
	_, err = Load(p)
	if want := "config " + p + `: [notify] push: unknown event "asks"`; err == nil || err.Error() != want {
		t.Fatalf("push = [asks]: %v, want %s", err, want)
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
		"t3_server":   {c.Runner.T3Server, "http://box-one:3773"},
		"tmux":        {c.Runner.TmuxSession, "mesh"},
		"worktrees":   {c.Runner.Worktrees, "~/wt"},
		"checkouts":   {c.Runner.Checkouts, "~/co"},
		"claude":      {c.Runner.Claude, "claude --debug"},
		"max_leads":   {fmt.Sprint(c.Runner.MaxLeads), "4"},
		"ntfy":        {c.Notify.Ntfy, "http://ntfy.example/bm"},
		"command":     {c.Notify.Command, "cat"},
		"push":        {fmt.Sprint(c.Notify.Push), "[ask done]"},
		"map_url":     {c.Notify.MapURL, "http://map.example/"},
		"glob":        {c.Spec.Glob, "specs/{id}.md"},
		"lead":        {c.Models.Lead, "lead-model"},
		"repos.demo":  {c.Repos["demo"].Path, "demo"},
		"hostname":    {c.HostName(), "box-one"},
		"quota":       {c.Quota.Command, "cat quota.json"},
		"thread_url":  {c.Console.ThreadURL, "http://box-one:3773/t/{thread}"},
		"hosts.other": {c.Console.Hosts["other"], "http://other:3773/t/{thread}"},
		"update":      {c.Update.Repo, "demo"},
	}
	for k, v := range checks {
		if v[0] != v[1] {
			t.Errorf("%s = %q, want %q", k, v[0], v[1])
		}
	}
	if c.Notify.DigestEvery != 30*time.Minute || c.Notify.CatchUp != 2*time.Hour || c.Liveness.Grace != 5*time.Minute ||
		c.Liveness.PollEvery != 15*time.Second || c.Liveness.WakeEvery != 2*time.Minute {
		t.Errorf("durations wrong: %+v %+v", c.Notify, c.Liveness)
	}
}

// The V1 keys load, each with a line saying to delete it; an empty
// t3_server, which meant V1 without asking, doesn't.
func TestLoadDroppedT3Keys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[runner]\nt3_dispatch = \"~/x/t3_dispatch.py\"\nt3_project = \"bm\"\n"), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"bmesh: [runner] t3_dispatch is ignored (T3 0.1.0 needs no script); delete it from " + p,
		"bmesh: [runner] t3_project is ignored (T3 0.1.0 needs no script); delete it from " + p,
	}
	if fmt.Sprint(c.Ignored()) != fmt.Sprint(want) || c.Runner.T3Server != "http://127.0.0.1:3773" {
		t.Fatalf("ignored %q server %q", c.Ignored(), c.Runner.T3Server)
	}
	if c, err := Load("../../testdata/config.toml"); err != nil || len(c.Ignored()) != 0 {
		t.Fatalf("fixture: %v %q", err, c.Ignored())
	}
	os.WriteFile(p, []byte("[runner]\nt3_server = \"\"\n"), 0o644)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "[runner] t3_server is empty") {
		t.Fatalf("empty t3_server: %v", err)
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
	os.WriteFile(p, []byte("[models]\nlead = \"m\"\nproposer_effort = \"huge\"\n"), 0o644)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), `[models] proposer_effort: effort "huge": want one of low, medium, high, xhigh, max`) {
		t.Fatalf("bad effort: %v", err)
	}
	os.WriteFile(p, []byte("[models]\nlead = \"m\"\nlead_effort = \"\"\n"), 0o644)
	if c, err := Load(p); err != nil || c.Models.Lead != "m" || c.Models.LeadEffort != "" || c.Models.ProposerEffort != "xhigh" {
		t.Fatalf("%v %+v", err, c.Models)
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
	// A directory that is no row's path gets its base name.
	other := t.TempDir()
	if name, path, err := c.RepoPath(other); err != nil || name != filepath.Base(other) || path != other {
		t.Fatalf("other dir: %q %q %v", name, path, err)
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

func TestLoadReposFixture(t *testing.T) {
	c, err := Load("../../testdata/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	d := c.Repos["demo"]
	if d.Name != "demo" || d.Path != "demo" || d.Mode != "ship" || d.Remote != "example/demo" || d.Host != "box-one" ||
		d.UXHost != "other" || fmt.Sprint(d.Verify) != "[npm test npm run lint]" || fmt.Sprint(d.Surfaces) != "[desk / at 1280x800]" ||
		d.Pin != "v1.2" || fmt.Sprint(d.Routines) != "[verify]" {
		t.Fatalf("demo row: %+v", d)
	}
	// A row with only a path gets the defaults: mode ship, routines verify and review, empty lists.
	b := c.Repos["bare"]
	if b.Name != "bare" || b.Mode != "ship" || fmt.Sprint(b.Routines) != "[verify review]" || b.Verify == nil || len(b.Verify) != 0 ||
		b.Surfaces == nil || len(b.Surfaces) != 0 || b.Code == nil || len(b.Code) != 0 || b.Host != "" {
		t.Fatalf("bare row: %+v", b)
	}
	if fmt.Sprint(c.RepoNames()) != "[bare demo]" {
		t.Fatalf("names %v", c.RepoNames())
	}
	// A config without a repos.toml beside it has an empty registry.
	c, err = Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil || len(c.Repos) != 0 {
		t.Fatalf("%v %v", err, c.Repos)
	}
}

func TestLoadReposRefusals(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	repos := filepath.Join(dir, "repos.toml")
	// [repos] in config.toml is refused, naming repos.toml.
	os.WriteFile(cfg, []byte("[repos]\ndemo = \"demo\"\n"), 0o644)
	_, err := Load(cfg)
	if err == nil || !strings.Contains(err.Error(), "[repos] has moved to "+repos) {
		t.Fatalf("config [repos]: %v", err)
	}
	os.Remove(cfg)
	for body, want := range map[string]string{
		"[repos.x]\nmode = \"ship\"\n":                                            "[repos.x]: path is required",
		"[repos.x]\npath = \"p\"\nmode = \"shipped\"\n":                           `[repos.x]: mode "shipped" is not one of ship, companion, publish, fork, dormant`,
		"[repos.x]\npath = \"p\"\nhots = \"k\"\n":                                 "unknown key repos.x.hots",
		"[repos.\"a/b\"]\npath = \"p\"\n":                                         `[repos.a/b]: name "a/b": a name matches`,
		"[repos.x]\npath = \"p\"\ncode = [\"*.go\", \" \"]\n":                     "[repos.x]: code: an empty pathspec",
		"[repos.x]\npath = \"p\"\nsetup_inputs = [\"a\"]\n":                       "[repos.x]: setup_inputs and setup_timeout need setup",
		"[repos.x]\npath = \"p\"\nsetup = \"npm ci\"\nsetup_timeout = \"soon\"\n": `[repos.x]: setup_timeout "soon": want a positive duration`,
		"[repos.x]\npath = \"p\"\nsetup = \"npm ci\"\nsetup_inputs = [\"\"]\n":    "[repos.x]: setup_inputs: an empty pathspec",
	} {
		os.WriteFile(repos, []byte(body), 0o644)
		_, err := Load(cfg)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %s", body, err, want)
		}
	}
}

func TestWriteRepoRoundTrips(t *testing.T) {
	p := filepath.Join(t.TempDir(), "repos.toml")
	r := Repo{Name: "app", Path: "~/repos/app", Mode: "fork", Remote: "o/app", Host: "k16", UXHost: "mac",
		Verify: []string{`test -z "$(gofmt -l .)"`, "go test ./..."}, Surfaces: []string{"desk at 1280x800"}, Pin: "v1",
		Routines: []string{"verify"}, Code: []string{"*.go", "cmd/**"},
		Setup: "npm ci", SetupInputs: []string{":(glob)**/package-lock.json"}, SetupTimeout: "5m"}
	if err := WriteRepo(p, r); err != nil {
		t.Fatal(err)
	}
	if err := WriteRepo(p, Repo{Name: "bare", Path: "/b"}); err != nil {
		t.Fatal(err)
	}
	// A dotted name is a quoted key, not a nested table; a bad name is refused.
	if err := WriteRepo(p, Repo{Name: "socket.io", Path: "/s"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteRepo(p, Repo{Name: "my repo", Path: "/m"}); err == nil || !strings.Contains(err.Error(), `name "my repo"`) {
		t.Fatalf("bad name: %v", err)
	}
	got, err := LoadRepos(p)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%+v", got["app"]) != fmt.Sprintf("%+v", r) {
		t.Fatalf("got %+v\nwant %+v", got["app"], r)
	}
	if b := got["bare"]; b.Path != "/b" || b.Mode != "ship" || fmt.Sprint(b.Routines) != "[verify review]" {
		t.Fatalf("bare %+v", b)
	}
	if got["socket.io"].Path != "/s" || len(got) != 3 {
		t.Fatalf("dotted %+v", got)
	}
	// Replacing a row keeps the others.
	if err := WriteRepo(p, Repo{Name: "app", Path: "/moved"}); err != nil {
		t.Fatal(err)
	}
	got, _ = LoadRepos(p)
	if got["app"].Path != "/moved" || got["app"].Mode != "ship" || got["bare"].Path != "/b" || got["socket.io"].Path != "/s" {
		t.Fatalf("after replace %+v", got)
	}
	if _, err := os.Stat(SetupFlag(p)); !os.IsNotExist(err) || SetupFlag(p) != filepath.Join(filepath.Dir(p), ".setup-complete") {
		t.Fatalf("flag %s %v", SetupFlag(p), err)
	}
}

func TestLoadKeepDurations(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	c, err := Load(p)
	if err != nil || c.Status.KeepDone != 24*time.Hour || c.Routines.KeepResults != 30*24*time.Hour {
		t.Fatalf("defaults: %v %+v %+v", err, c.Status, c.Routines)
	}
	os.WriteFile(p, []byte("[status]\nkeep_done = \"36h\"\n[routines]\nkeep_results = \"7d\"\n"), 0o644)
	if c, err = Load(p); err != nil || c.Status.KeepDone != 36*time.Hour || c.Routines.KeepResults != 7*24*time.Hour {
		t.Fatalf("%v %+v %+v", err, c.Status, c.Routines)
	}
	for _, bad := range []string{"[status]\nkeep_done = \"-1h\"\n", "[routines]\nkeep_results = \"xd\"\n", "[routines]\nkeep_results = \"-2d\"\n"} {
		os.WriteFile(p, []byte(bad), 0o644)
		if _, err := Load(p); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	for d, want := range map[time.Duration]string{24 * time.Hour: "24h", 30 * 24 * time.Hour: "30d", 90 * time.Minute: "1h30m", 45 * time.Second: "45s", 0: "0s"} {
		if got := FormatDuration(d); got != want {
			t.Fatalf("FormatDuration(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestUpdateTrack(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	if c, err := Load(p); err != nil || c.Update.Track != "release" {
		t.Fatalf("default: %v %+v", err, c)
	}
	os.WriteFile(p, []byte("[update]\nrepo = \"bm\"\n"), 0o644)
	if c, err := Load(p); err != nil || c.Update.Track != "release" {
		t.Fatalf("unset: %v %+v", err, c.Update)
	}
	os.WriteFile(p, []byte("[update]\ntrack = \"latest\"\n"), 0o644)
	if c, err := Load(p); err != nil || c.Update.Track != "latest" {
		t.Fatalf("latest: %v %+v", err, c.Update)
	}
	os.WriteFile(p, []byte("[update]\ntrack = \"main\"\n"), 0o644)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), `[update] track "main": want release or latest`) {
		t.Fatalf("bad track: %v", err)
	}
}

func TestQuotaRemindTopic(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	ok := "[notify]\nntfy = \"http://k16:2586/bmesh\"\n[quota]\ncommand = \"cat q.json\"\nremind_topic = \"quota-reset\"\n"
	os.WriteFile(p, []byte(ok), 0o644)
	if c, err := Load(p); err != nil || c.Quota.RemindTopic != "quota-reset" {
		t.Fatalf("%v %+v", err, c)
	}
	for body, want := range map[string]string{
		"[notify]\nntfy = \"http://k16:2586/bmesh\"\n[quota]\ncommand = \"x\"\nremind_topic = \"a/b\"\n": "an ntfy topic is",
		"[notify]\nntfy = \"http://k16:2586/bmesh\"\n[quota]\nremind_topic = \"q\"\n":                    "needs [quota] command",
		"[notify]\ncommand = \"cat\"\n[quota]\ncommand = \"x\"\nremind_topic = \"q\"\n":                  "needs [notify] ntfy",
	} {
		os.WriteFile(p, []byte(body), 0o644)
		if _, err := Load(p); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
}

func TestStateDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	saved := goos
	t.Cleanup(func() { goos = saved })
	for _, tc := range []struct{ os, xdg, want string }{
		{"linux", "", filepath.Join(home, ".local", "state", "bmesh")},
		{"darwin", "", filepath.Join(home, "Library", "Application Support", "bmesh", "state")},
		{"linux", "/x/state", "/x/state/bmesh"},
		{"darwin", "/x/state", "/x/state/bmesh"},
	} {
		goos = tc.os
		t.Setenv("XDG_STATE_HOME", tc.xdg)
		if got := StateDir(); got != tc.want {
			t.Errorf("%s xdg %q: %q, want %q", tc.os, tc.xdg, got, tc.want)
		}
	}
}

func TestHubURLIsTheEmbeddedHubWhenNATSIsEmpty(t *testing.T) {
	for _, tc := range []struct {
		nats, listen, want string
		embedded           bool
	}{
		{"nats://hub:4222", "127.0.0.1:4222", "nats://hub:4222", false},
		{"", "127.0.0.1:4222", "nats://127.0.0.1:4222", true},
		{"", "0.0.0.0:5000", "nats://127.0.0.1:5000", true},
		{"", "[::]:5000", "nats://127.0.0.1:5000", true},
		{"", ":5000", "nats://127.0.0.1:5000", true},
	} {
		c := &Config{Mesh: MeshConfig{NATS: tc.nats, Listen: tc.listen}}
		if got, emb := c.HubURL(); got != tc.want || emb != tc.embedded {
			t.Errorf("%q %q: %q %v, want %q %v", tc.nats, tc.listen, got, emb, tc.want, tc.embedded)
		}
	}
}

func TestAddons(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	c, err := Load(p)
	if err != nil || len(c.Addons.Enable) != 0 || c.Addons.T3Done.Every != 5*time.Second || c.Addons.T3Done.MinBusy != 5*time.Minute {
		t.Fatalf("defaults: %v %+v", err, c.Addons)
	}
	os.WriteFile(p, []byte("[addons]\nenable = [\"t3-done\"]\n[addons.t3-done]\nntfy = \"http://n:2586/t3\"\nevery = \"2s\"\nmin_busy = \"0s\"\n"), 0o644)
	c, err = Load(p)
	if err != nil || fmt.Sprint(c.Addons.Enable) != "[t3-done]" || c.Addons.T3Done != (T3DoneConfig{Ntfy: "http://n:2586/t3", Every: 2 * time.Second}) {
		t.Fatalf("t3-done: %v %+v", err, c.Addons)
	}
	// The settings alone run nothing, and need no ntfy.
	os.WriteFile(p, []byte("[addons.t3-done]\nevery = \"1m\"\n"), 0o644)
	if c, err = Load(p); err != nil || len(c.Addons.Enable) != 0 {
		t.Fatalf("settings only: %v %+v", err, c.Addons)
	}
	for body, want := range map[string]string{
		"[addons]\nenable = [\"t3-dnoe\"]\n": `[addons] enable: unknown add-on "t3-dnoe" (want one of t3-done)`,
		"[addons]\nenable = [\"t3-done\", \"t3-done\"]\n[addons.t3-done]\nntfy = \"http://n/t\"\n": `[addons] enable: "t3-done" is listed twice`,
		"[addons]\nenable = [\"t3-done\"]\n": "[addons.t3-done] ntfy is empty: t3-done needs the ntfy URL with its topic it posts to",
		"[addons]\nenable = [\"t3-done\"]\n[addons.t3-done]\nntfy = \"http://n/t\"\nevery = \"0s\"\n":     "[addons.t3-done] every: want a positive duration",
		"[addons]\nenable = [\"t3-done\"]\n[addons.t3-done]\nntfy = \"http://n/t\"\nmin_busy = \"-1m\"\n": "[addons.t3-done] min_busy: want a duration of 0 or more",
	} {
		os.WriteFile(p, []byte(body), 0o644)
		if _, err := Load(p); err == nil || err.Error() != "config "+p+": "+want {
			t.Errorf("%q: %v, want %s", body, err, want)
		}
	}
}
