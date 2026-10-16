package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/hosts"
)

func adminRegistry(version int64, rows string) hosts.Registry {
	r := hosts.Registry{Version: version, AdminKey: hostPub("admin"), Rows: rows, IssuedAt: "2026-10-16T10:00:00Z"}
	r.Sign(hostKey("admin"))
	return r
}

func (h *hostsFixture) putRegistry(t *testing.T, r hosts.Registry) {
	t.Helper()
	kv, err := hosts.Bucket(h.ctx, h.c.JS, hosts.BucketRoster)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	if _, err := kv.Put(h.ctx, hosts.RegistryKey, b); err != nil {
		t.Fatal(err)
	}
}

func waitTrue(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestDaemonTakesARegistryAndRefusesOneItCantRead(t *testing.T) {
	h := hostsSetup(t, "ping")
	repos := config.ReposPath(h.d.Cfg.Path())
	if err := os.WriteFile(repos, []byte("# hand-written\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v1 := "[repos.app]\npath = \"" + filepath.Join(t.TempDir(), "app") + "\"\n"
	h.putRegistry(t, adminRegistry(1, v1))
	waitTrue(t, "registry v1 taken", func() bool {
		b, _ := os.ReadFile(repos)
		return string(b) == v1
	})
	if b, _ := os.ReadFile(repos + ".prev"); string(b) != "# hand-written\n" {
		t.Fatalf("repos.toml.prev: %q", b)
	}
	// The hosts layer reads the new rows: presence lists app.
	waitTrue(t, "presence lists app", func() bool {
		kv, _ := hosts.Bucket(h.ctx, h.c.JS, hosts.BucketHosts)
		seen, _ := hosts.ReadPresence(h.ctx, kv, h.roster, time.Now())
		for _, s := range seen {
			if s.Host.Name == testHost && len(s.Presence.Repos) == 1 && s.Presence.Repos[0].Name == "app" {
				return true
			}
		}
		return false
	})

	// v2 has a key this binary doesn't know: kept v1, pushed once.
	h.putRegistry(t, adminRegistry(2, v1+"agent = \"opus\"\n"))
	waitTrue(t, "the refusal is published", func() bool { return len(h.claims(t, "registry")) > 0 })
	time.Sleep(500 * time.Millisecond) // several ticks
	evs := h.claims(t, "registry")
	if len(evs) != 1 || !strings.Contains(evs[0].Text, testHost+": refused: registry v2 needs a newer bmesh (unknown key repos.app.agent)") {
		t.Fatalf("registry events: %+v", evs)
	}
	if b, _ := os.ReadFile(repos); string(b) != v1 {
		t.Fatalf("repos.toml after an unreadable registry: %q", b)
	}
	if r, _, _ := hosts.LoadRegistry(h.d.StateDir, hostPub("admin")); r.Version != 1 {
		t.Fatalf("registry.json v%d", r.Version)
	}
}

func TestProposeIsHeldOnlyOnTheAdminBox(t *testing.T) {
	h := hostsSetup(t, "ping", "propose")
	j := hosts.NewJob(hostKey("mac"), "mac", testHost, "propose", "", hosts.ProposeArgs(0, "[repos.a]\npath = \"/a\"\n"),
		2, time.Now(), time.Hour)
	h.send(t, j)
	if r := h.reply(t, j, 1); r.OK || r.Text != "refused: "+testHost+" is not the admin box" {
		t.Fatalf("on a box that isn't the admin: %+v", r)
	}
}

func TestProposeHoldsTheDiffOnTheAdminBox(t *testing.T) {
	f := setup(t)
	f.d.Now = time.Now
	f.d.StateDir, f.d.ConfigDir = t.TempDir(), t.TempDir()
	f.d.HostsEvery = 100 * time.Millisecond
	f.d.Cfg.Hosts.Enabled, f.d.Cfg.Hosts.Admin = true, true
	if err := os.WriteFile(hosts.HostKeyPath(f.d.ConfigDir), []byte(hexs(hostKey(testHost).Seed())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// testhost is the admin (its approve key is the admin key); k16 proposes.
	r := hosts.Roster{Version: 2, AdminKey: hostPub("admin"), IssuedAt: "2026-10-16T08:00:00Z", Hosts: []hosts.Host{
		{Name: testHost, Key: hostPub(testHost), Secure: true, ApproveKey: hostPub("admin"), Accept: []string{"propose"}},
		{Name: "k16", Key: hostPub("k16"), Accept: []string{"ping"}},
	}}
	r.Sign(hostKey("admin"))
	if err := hosts.SaveLocal(f.d.StateDir, hosts.Local{Pinned: hostPub("admin"), Roster: r}); err != nil {
		t.Fatal(err)
	}
	v1 := "[repos.app]\npath = \"/x/app\"\n"
	if err := hosts.SaveRegistry(f.d.StateDir, adminRegistry(1, v1)); err != nil {
		t.Fatal(err)
	}
	h := &hostsFixture{fixture: f, roster: r}
	h.start(t)

	propose := func(base int64, rows string) hosts.Reply {
		j := hosts.NewJob(hostKey("k16"), "k16", testHost, "propose", "", hosts.ProposeArgs(base, rows), 2, time.Now(), time.Hour)
		h.send(t, j)
		return h.reply(t, j, 1)
	}
	if rep := propose(0, v1+"verify = [\"make\"]\n"); rep.OK || !strings.Contains(rep.Text, "proposed against registry v0; it is v1 now") {
		t.Fatalf("stale: %+v", rep)
	}
	if rep := propose(1, v1+"agent = 1\n"); rep.OK || !strings.Contains(rep.Text, "don't load here: unknown key repos.app.agent") {
		t.Fatalf("unreadable: %+v", rep)
	}
	if rep := propose(1, v1); !rep.OK || rep.Text != "no change: registry v1 has these rows" {
		t.Fatalf("no change: %+v", rep)
	}
	rep := propose(1, v1+"verify = [\"make\"]\n")
	if !rep.OK || !strings.HasPrefix(rep.Text, "held for approval as ") {
		t.Fatalf("held: %+v", rep)
	}
	held, _ := hosts.ListHeld(f.d.StateDir)
	if len(held) != 1 || held[0].Kind != hosts.HeldRegistry || held[0].Base != 1 || held[0].Log != `~ app: verify: (none) -> ["make"]` {
		t.Fatalf("hold: %+v", held)
	}
	// A newer proposal from the same box supersedes it.
	propose(1, v1+"verify = [\"make test\"]\n")
	held, _ = hosts.ListHeld(f.d.StateDir)
	if len(held) != 1 || !strings.Contains(held[0].Log, "make test") {
		t.Fatalf("superseded: %+v", held)
	}
	if r, _, _ := hosts.LoadRegistry(f.d.StateDir, hostPub("admin")); r.Version != 1 {
		t.Fatalf("a held proposal moved the registry to v%d", r.Version)
	}
	if len(h.claims(t, "held")) != 2 {
		t.Fatalf("held events: %+v", h.claims(t, "held"))
	}
}

func TestNixJobTakesThePathFromThisBoxsRow(t *testing.T) {
	h := hostsSetup(t, "ping", "nix")
	nix := func(by string, key string, repo, origin string) hosts.Reply {
		j := hosts.NewJob(hostKey(key), by, testHost, "nix", repo, []string{origin}, 2, time.Now(), time.Hour)
		h.send(t, j)
		return h.reply(t, j, 1)
	}
	// Signed with the admin box's host key, not the admin key: refused.
	if rep := nix("mac", "mac", "app", "me/app"); rep.OK || rep.Text != "refused: signature" {
		t.Fatalf("host key: %+v", rep)
	}
	// A repo this box has no row for is absent; nothing else is looked at.
	if rep := nix("mac", "admin", "app", "me/app"); !rep.OK || !strings.HasPrefix(rep.Text, "absent: app is not in this box's registry") {
		t.Fatalf("no row: %+v", rep)
	}
}
