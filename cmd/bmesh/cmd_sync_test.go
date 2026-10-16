package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// serveBox runs box's daemon (serve) until the returned stop.
func serveBox(t *testing.T, b box) (logs *meshtest.SyncBuffer, stop func()) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", b.state)
	cfg, err := config.Load(b.cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	logs = &meshtest.SyncBuffer{}
	res := make(chan error, 1)
	go func() { res <- serve(ctx, cfg, "127.0.0.1:0", logs, nil, "") }()
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case <-res:
		case <-time.After(20 * time.Second):
			t.Error("serve did not stop")
		}
	}
	t.Cleanup(stop)
	// The daemon reads XDG_STATE_HOME as it starts: wait until it has,
	// before another box's run changes it.
	waitFor(t, b.name+"'s daemon starts", func() bool {
		return strings.Contains(logs.String(), "hosts: self-level done")
	})
	return logs, stop
}

// waitFor polls f until it is true.
func waitFor(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestSyncLevelsEveryBoxAndHoldsOnASecureOne(t *testing.T) {
	atScreenForTest(t)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	cl, _ := filepath.Abs("../../testdata/fake-claude-plugin.sh")
	cx, _ := filepath.Abs("../../testdata/fake-codex.sh")
	t.Setenv("BMESH_CLAUDE_COMMAND", "/bin/sh "+cl)
	t.Setenv("BMESH_CODEX_COMMAND", "/bin/sh "+cx)
	url := meshtest.Start(t)
	mac, k16 := newBox(t, "mac", url), newBox(t, "k16", url)

	// mac is the admin, secure, routines off; k16 joins, not secure, and
	// both accept level.
	if out, errs, code := mac.run(t, "y\nn\npw\npw\n", "init", "--hosts", "--admin"); code != 0 {
		t.Fatalf("admin init: %d %s %s", code, out, errs)
	}
	codeStr, wait := invite(t, mac, "pw\n", "k16", "--accept", "ping,level")
	if out, errs, code := k16.run(t, "\nn\nn\n"+codeStr+"\n", "init", "--hosts"); code != 0 {
		t.Fatalf("join: %d %s %s", code, out, errs)
	}
	if res, c := wait(); c != 0 {
		t.Fatalf("invite: %d %s", c, res)
	}
	if out, errs, code := mac.run(t, "pw\n", "hosts", "set", "mac", "--accept", "ping,level"); code != 0 {
		t.Fatalf("set: %d %s %s", code, out, errs)
	}

	// Two repos on origin, checked out on both boxes: tools (a plugin
	// marketplace, so mac holds it) and app (not).
	root := t.TempDir()
	clone := func(name, dir string) {
		meshtest.Git(t, root, "clone", "-q", filepath.Join(root, name+".git"), dir)
	}
	for _, name := range []string{"tools", "app"} {
		meshtest.Git(t, root, "init", "-q", "--bare", "-b", "main", filepath.Join(root, name+".git"))
		seed := filepath.Join(root, name+"-seed")
		clone(name, seed)
		files := map[string]string{"a.txt": "one\n"}
		if name == "tools" {
			files[".claude-plugin/marketplace.json"] = `{"name":"tools"}`
		}
		meshtest.CommitFiles(t, seed, files)
		meshtest.Git(t, seed, "push", "-q", "origin", "HEAD:main")
	}
	for _, b := range []box{mac, k16} {
		var reg strings.Builder
		for _, name := range []string{"tools", "app"} {
			dir := filepath.Join(root, b.name, name)
			clone(name, dir)
			reg.WriteString("[repos." + name + "]\npath = \"" + dir + "\"\n")
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(b.cfg), "repos.toml"), []byte(reg.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	macDir := func(name string) string { return filepath.Join(root, "mac", name) }
	k16Dir := func(name string) string { return filepath.Join(root, "k16", name) }

	// k16 commits to both, an hour ago: old enough to push.
	t.Setenv("GIT_COMMITTER_DATE", time.Now().Add(-time.Hour).Format(time.RFC3339))
	appTop := meshtest.CommitFiles(t, k16Dir("app"), map[string]string{"b.txt": "two\n"})
	toolsTop := meshtest.CommitFiles(t, k16Dir("tools"), map[string]string{"skill.md": "new\n"})
	os.Unsetenv("GIT_COMMITTER_DATE")

	// mac's daemon runs: its presence lists both repos.
	logs, stopMac := serveBox(t, mac)
	waitFor(t, "mac's presence", func() bool {
		out, _, _ := k16.run(t, "", "hosts", "--json")
		return strings.Contains(out, `"name": "tools"`) || strings.Contains(out, `"name":"tools"`)
	})

	out, errs, code := k16.run(t, "", "sync", "--all", "--wait", "20s")
	if code != 0 {
		t.Fatalf("sync: %d %s %s", code, out, errs)
	}
	for _, want := range []string{"app: k16: pushed 1", "app: mac: fast-forwarded 1", "tools: k16: pushed 1",
		"tools: mac: held for approval (+1 commit)"} {
		if !strings.Contains(out, want) {
			t.Errorf("sync output lacks %q:\n%s\n%s", want, out, logs.String())
		}
	}
	if got := meshtest.Git(t, macDir("app"), "rev-parse", "HEAD"); got != appTop {
		t.Fatalf("mac app at %s, want %s", got, appTop)
	}

	// At the Mac: the hold is listed, approved with the admin key, and
	// tools moves.
	out, _, _ = mac.run(t, "", "hosts", "held", "--json")
	var held []hosts.Held
	if err := json.Unmarshal([]byte(out), &held); err != nil || len(held) != 1 || held[0].Repo != "tools" || held[0].From() != "k16" {
		t.Fatalf("held: %v %s", err, out)
	}
	if got := meshtest.Git(t, macDir("tools"), "rev-parse", "HEAD"); got == toolsTop {
		t.Fatal("a held level moved tools")
	}
	out, errs, code = mac.run(t, "pw\n", "hosts", "approve", held[0].ID)
	if code != 0 || !strings.Contains(out, "skill.md") || !strings.Contains(out, "tools: fast-forwarded 1 (approved)") {
		t.Fatalf("approve: %d %s %s", code, out, errs)
	}
	if got := meshtest.Git(t, macDir("tools"), "rev-parse", "HEAD"); got != toolsTop {
		t.Fatalf("mac tools at %s, want %s", got, toolsTop)
	}
	out, _, _ = k16.run(t, "", "hosts", "replies")
	if !strings.Contains(out, "fast-forwarded 1 (approved)") {
		t.Fatalf("k16 never heard the approval: %s", out)
	}

	// Another tools commit, held again, denied: the sender hears it.
	t.Setenv("GIT_COMMITTER_DATE", time.Now().Add(-time.Hour).Format(time.RFC3339))
	meshtest.CommitFiles(t, k16Dir("tools"), map[string]string{"skill2.md": "x\n"})
	os.Unsetenv("GIT_COMMITTER_DATE")
	out, _, _ = k16.run(t, "", "sync", "--repo", "tools", "--wait", "20s")
	if !strings.Contains(out, "mac: held for approval") {
		t.Fatalf("second sync: %s", out)
	}
	out, _, _ = mac.run(t, "", "hosts", "held")
	id := strings.Fields(out)[0]
	if out, errs, code := mac.run(t, "", "hosts", "deny", id); code != 0 || !strings.Contains(out, "denied "+id) {
		t.Fatalf("deny: %d %s %s", code, out, errs)
	}
	waitFor(t, "the denial reaches k16", func() bool {
		out, _, _ := k16.run(t, "", "hosts", "replies")
		return strings.Contains(out, "denied by the operator")
	})
	if out, _, _ := mac.run(t, "", "hosts", "held"); !strings.Contains(out, "held: none") {
		t.Fatalf("held after deny: %s", out)
	}

	// mac's daemon stops: its level waits in its inbox.
	stopMac()
	t.Setenv("GIT_COMMITTER_DATE", time.Now().Add(-time.Hour).Format(time.RFC3339))
	appTop = meshtest.CommitFiles(t, k16Dir("app"), map[string]string{"c.txt": "three\n"})
	os.Unsetenv("GIT_COMMITTER_DATE")
	out, _, _ = k16.run(t, "", "sync", "--repo", "app", "--wait", "1s")
	if !strings.Contains(out, "k16: pushed 1") || !regexp.MustCompile(`mac: (asleep|no answer in 1s), queued until `).MatchString(out) {
		t.Fatalf("sync to a stopped box: %s", out)
	}
	serveBox(t, mac)
	waitFor(t, "mac takes the queued level", func() bool {
		return meshtest.Git(t, macDir("app"), "rev-parse", "HEAD") == appTop
	})
}

func TestSyncWithHostsOffIsRefused(t *testing.T) {
	setup(t)
	_, errs, code := runCLI(t, nil, "sync", "--all")
	if code != exitRefused || !strings.Contains(errs, "hosts are off") {
		t.Fatalf("%d %s", code, errs)
	}
}
