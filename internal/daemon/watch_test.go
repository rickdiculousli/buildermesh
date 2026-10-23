package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// watchOpens receives each watch the hosts loop opens from now on. Call
// it before the loop starts.
func watchOpens(t *testing.T) <-chan jetstream.KeyWatcher {
	ch := make(chan jetstream.KeyWatcher, 16)
	watchOpened = func(w jetstream.KeyWatcher) { ch <- w }
	t.Cleanup(func() { watchOpened = func(jetstream.KeyWatcher) {} })
	return ch
}

func opened(t *testing.T, opens <-chan jetstream.KeyWatcher) jetstream.KeyWatcher {
	t.Helper()
	select {
	case w := <-opens:
		return w
	case <-time.After(10 * time.Second):
		t.Fatal("no watch opened")
		return nil
	}
}

// ticked waits for the start-up tick: it ends with this box's presence.
func (h *hostsFixture) ticked(t *testing.T) {
	t.Helper()
	kv, err := hosts.Bucket(h.ctx, h.c.JS, hosts.BucketHosts)
	if err != nil {
		t.Fatal(err)
	}
	waitTrue(t, "the start-up tick", func() bool {
		_, err := kv.Get(h.ctx, h.d.Host)
		return err == nil
	})
}

// hubRegistry is the version on the hub, 0 when the key is missing.
func (f *fixture) hubRegistry(t *testing.T) int64 {
	t.Helper()
	kv, err := hosts.Bucket(f.ctx, f.c.JS, hosts.BucketRoster)
	if err != nil {
		t.Fatal(err)
	}
	e, err := kv.Get(f.ctx, hosts.RegistryKey)
	if err != nil {
		return 0
	}
	var r hosts.Registry
	_ = json.Unmarshal(e.Value(), &r)
	return r.Version
}

func reposRows(t *testing.T, n int) string {
	return fmt.Sprintf("[repos.app%d]\npath = %q\n", n, filepath.Join(t.TempDir(), "app"))
}

// readsAs waits for the file at path to hold text.
func readsAs(t *testing.T, path, text string) {
	t.Helper()
	waitTrue(t, path+" written", func() bool {
		b, _ := os.ReadFile(path)
		return string(b) == text
	})
}

// With an hour's tick, only the watch can take what is put after the
// start-up tick: a registry, a roster, and a deleted registry key healed.
func TestWatchTakesARegistryAndARosterWithoutATick(t *testing.T) {
	opens := watchOpens(t)
	h := hostsOn(t, setup(t), time.Hour, "ping")
	h.start(t)
	opened(t, opens)
	h.ticked(t)

	repos := config.ReposPath(h.d.Cfg.Path())
	rows := reposRows(t, 1)
	h.putRegistry(t, adminRegistry(1, rows))
	readsAs(t, repos, rows)

	kv, err := hosts.Bucket(h.ctx, h.c.JS, hosts.BucketRoster)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(testRoster(3, "ping"))
	if _, err := kv.Put(h.ctx, hosts.RosterKey, b); err != nil {
		t.Fatal(err)
	}
	waitTrue(t, "roster v3 taken", func() bool {
		l, ok, _ := hosts.LoadLocal(h.d.StateDir)
		return ok && l.Roster.Version == 3
	})

	if err := kv.Delete(h.ctx, hosts.RegistryKey); err != nil {
		t.Fatal(err)
	}
	waitTrue(t, "registry v1 put back", func() bool { return h.hubRegistry(t) == 1 })
}

// Two boxes heal a deleted registry key at once: one holds v5 (an older
// bmesh, which can't read v6's rows), the other v6. Whichever put lands
// last, the v6 box's watch puts v6 over v5, and neither box takes v5.
func TestWatchTwoBoxesHealToTheHigherRegistry(t *testing.T) {
	opens := watchOpens(t)
	h := hostsOn(t, setup(t), time.Hour, "ping")
	v5 := reposRows(t, 1)
	v6 := v5 + "agent = \"opus\"\n" // a key this bmesh doesn't know
	if err := hosts.SaveRegistry(h.d.StateDir, adminRegistry(5, v5)); err != nil {
		t.Fatal(err)
	}
	mac := h.d
	cfg, err := config.Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Hosts.Enabled = true
	mac.Cfg = cfg
	joinHost(t, &mac, "mac", h.roster)
	if err := hosts.SaveRegistry(mac.StateDir, adminRegistry(6, v6)); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	runHosts(t, mac)
	opened(t, opens)
	opened(t, opens)
	waitTrue(t, "the hub at v6", func() bool { return h.hubRegistry(t) == 6 })

	settles := func(what string) {
		t.Helper()
		waitTrue(t, what, func() bool { return h.hubRegistry(t) == 6 })
		time.Sleep(300 * time.Millisecond)
		if v := h.hubRegistry(t); v != 6 {
			t.Fatalf("%s: the hub moved to v%d", what, v)
		}
	}
	kv, err := hosts.Bucket(h.ctx, h.c.JS, hosts.BucketRoster)
	if err != nil {
		t.Fatal(err)
	}
	if err := kv.Delete(h.ctx, hosts.RegistryKey); err != nil {
		t.Fatal(err)
	}
	settles("the deleted key healed at v6")
	// The v5 box's put landing last.
	h.putRegistry(t, adminRegistry(5, v5))
	settles("v5 overwritten with v6")

	for _, d := range []Deps{h.d, mac} {
		if r, _, _ := hosts.LoadRegistry(d.StateDir, hostPub("admin")); r.Version != map[string]int64{testHost: 5, "mac": 6}[d.Host] {
			t.Errorf("%s holds registry v%d", d.Host, r.Version)
		}
	}
}

// With the tick every 10ms, watch events and ticks meet: each registry
// version is still applied once.
func TestWatchAndTickApplyARegistryOnce(t *testing.T) {
	opens := watchOpens(t)
	h := hostsOn(t, setup(t), 10*time.Millisecond, "ping")
	logs := &meshtest.SyncBuffer{}
	h.d.Log = slog.New(slog.NewTextHandler(logs, nil))
	h.start(t)
	opened(t, opens)
	repos := config.ReposPath(h.d.Cfg.Path())
	for v := int64(1); v <= 8; v++ {
		rows := reposRows(t, int(v))
		h.putRegistry(t, adminRegistry(v, rows))
		readsAs(t, repos, rows)
	}
	for v := 1; v <= 8; v++ {
		if n := strings.Count(logs.String(), fmt.Sprintf("msg=\"hosts: registry\" version=%d\n", v)); n != 1 {
			t.Errorf("registry v%d applied %d times", v, n)
		}
	}
}

// A watch that closes is opened again after the tick interval, and takes
// what is put after.
func TestWatchReopensAfterItCloses(t *testing.T) {
	const tick = 500 * time.Millisecond
	opens := watchOpens(t)
	h := hostsOn(t, setup(t), tick, "ping")
	h.start(t)
	w := opened(t, opens)
	h.ticked(t)
	stopped := time.Now()
	if err := w.Stop(); err != nil {
		t.Fatal(err)
	}
	opened(t, opens)
	if gap := time.Since(stopped); gap < tick*9/10 {
		t.Fatalf("reopened after %v, before the tick interval", gap)
	}
	rows := reposRows(t, 1)
	h.putRegistry(t, adminRegistry(1, rows))
	readsAs(t, config.ReposPath(h.d.Cfg.Path()), rows)
}

// After the hub restarts on its store, a put is taken: by the rebuilt
// watch or, at the latest, by the next tick.
func TestWatchAfterTheHubRestarts(t *testing.T) {
	store := t.TempDir()
	e, err := mesh.StartEmbedded(store, "127.0.0.1:-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Shutdown() })
	u, err := url.Parse(e.URL())
	if err != nil {
		t.Fatal(err)
	}
	opens := watchOpens(t)
	h := hostsOn(t, setupOn(t, e.URL(), true), 2*time.Second, "ping")
	h.start(t)
	opened(t, opens)
	h.ticked(t)

	e.Shutdown()
	if e, err = mesh.StartEmbedded(store, u.Host); err != nil {
		t.Fatal(err)
	}
	rows := reposRows(t, 1)
	b, _ := json.Marshal(adminRegistry(1, rows))
	waitTrue(t, "a put after the restart", func() bool {
		kv, err := hosts.Bucket(h.ctx, h.c.JS, hosts.BucketRoster)
		if err != nil {
			return false
		}
		_, err = kv.Put(h.ctx, hosts.RegistryKey, b)
		return err == nil
	})
	readsAs(t, config.ReposPath(h.d.Cfg.Path()), rows)
}
