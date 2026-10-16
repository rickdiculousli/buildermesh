package hosts

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

func hub(t *testing.T) (context.Context, *mesh.Client) {
	t.Helper()
	url := meshtest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	c, err := mesh.Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	return ctx, c
}

func rosterKV(t *testing.T, ctx context.Context, c *mesh.Client) jetstream.KeyValue {
	t.Helper()
	kv, err := Bucket(ctx, c.JS, BucketRoster)
	if err != nil {
		t.Fatal(err)
	}
	return kv
}

func putRaw(t *testing.T, ctx context.Context, kv jetstream.KeyValue, r Roster) {
	t.Helper()
	b, _ := json.Marshal(r)
	if _, err := kv.Put(ctx, RosterKey, b); err != nil {
		t.Fatal(err)
	}
}

func hubVersion(t *testing.T, ctx context.Context, kv jetstream.KeyValue) int64 {
	t.Helper()
	r, ok, err := HubRoster(ctx, kv)
	if err != nil || !ok {
		t.Fatalf("hub roster: ok %v err %v", ok, err)
	}
	return r.Version
}

func TestSyncTakesOnlyAHigherVersionThePinnedKeySigned(t *testing.T) {
	ctx, c := hub(t)
	kv := rosterKV(t, ctx, c)
	dir := t.TempDir()
	l := Local{Pinned: pubFor("admin"), Roster: testRoster(2)}
	if err := SaveLocal(dir, l); err != nil {
		t.Fatal(err)
	}

	putRaw(t, ctx, kv, testRoster(1)) // lower
	if ok, err := Sync(ctx, kv, dir, &l); ok || err != nil || l.Roster.Version != 2 {
		t.Fatalf("a lower version: accepted %v err %v v%d", ok, err, l.Roster.Version)
	}
	if hubVersion(t, ctx, kv) != 2 {
		t.Error("a lower hub roster was not replaced by the box's")
	}

	forged := testRoster(9)
	forged.AdminKey = pubFor("k16")
	forged.Sign(keyFor("k16")) // a non-admin signature
	putRaw(t, ctx, kv, forged)
	if ok, _ := Sync(ctx, kv, dir, &l); ok || l.Roster.Version != 2 {
		t.Fatalf("a non-admin signature: accepted %v v%d", ok, l.Roster.Version)
	}
	if hubVersion(t, ctx, kv) != 2 {
		t.Error("a forged hub roster was not replaced")
	}

	if err := kv.Delete(ctx, RosterKey); err != nil { // a deleted key
		t.Fatal(err)
	}
	if ok, _ := Sync(ctx, kv, dir, &l); ok || l.Roster.Version != 2 {
		t.Fatalf("a deleted key: accepted %v v%d", ok, l.Roster.Version)
	}
	if hubVersion(t, ctx, kv) != 2 {
		t.Error("a deleted roster was not put back")
	}

	putRaw(t, ctx, kv, testRoster(3)) // higher, by the admin
	if ok, err := Sync(ctx, kv, dir, &l); !ok || err != nil || l.Roster.Version != 3 {
		t.Fatalf("a higher version: accepted %v err %v v%d", ok, err, l.Roster.Version)
	}
	saved, _, err := LoadLocal(dir)
	if err != nil || saved.Roster.Version != 3 {
		t.Fatalf("roster.json: v%d err %v", saved.Roster.Version, err)
	}
}

// TestSyncTakesRosterJSONFirst: the admin's CLI saved v4 to roster.json but
// the hub write failed, while a daemon holds v2 in memory and the hub has
// v3. The daemon's next sync takes v4 from the file, puts it on the hub,
// and never writes the hub's v3 over it.
func TestSyncTakesRosterJSONFirst(t *testing.T) {
	ctx, c := hub(t)
	kv := rosterKV(t, ctx, c)
	dir := t.TempDir()
	daemon := Local{Pinned: pubFor("admin"), Roster: testRoster(2)}
	putRaw(t, ctx, kv, testRoster(3))
	if err := SaveLocal(dir, Local{Pinned: pubFor("admin"), Roster: testRoster(4)}); err != nil {
		t.Fatal(err)
	}
	if ok, err := Sync(ctx, kv, dir, &daemon); !ok || err != nil || daemon.Roster.Version != 4 {
		t.Fatalf("sync: accepted %v err %v v%d", ok, err, daemon.Roster.Version)
	}
	if hubVersion(t, ctx, kv) != 4 {
		t.Error("the saved roster wasn't put on the hub")
	}
	if saved, _, _ := LoadLocal(dir); saved.Roster.Version != 4 {
		t.Errorf("roster.json went back to v%d", saved.Roster.Version)
	}
	// A save never goes over a newer file.
	stale := Local{Pinned: pubFor("admin"), Roster: testRoster(3)}
	if err := saveNewer(dir, &stale); err != nil || stale.Roster.Version != 4 {
		t.Fatalf("saveNewer: v%d %v", stale.Roster.Version, err)
	}
	if saved, _, _ := LoadLocal(dir); saved.Roster.Version != 4 {
		t.Errorf("saveNewer wrote v%d over v4", saved.Roster.Version)
	}
}

func TestPublishTakesTheVersionFromTheBoxNotTheHub(t *testing.T) {
	ctx, c := hub(t)
	kv := rosterKV(t, ctx, c)
	dir := t.TempDir()
	l := Local{Pinned: pubFor("admin"), Roster: testRoster(2)}
	junk := testRoster(50)
	junk.AdminKey = pubFor("k16")
	junk.Sign(keyFor("k16"))
	putRaw(t, ctx, kv, junk)
	next := l.Roster
	next.Hosts = next.Hosts[1:] // k16 revoked
	got, err := Publish(ctx, kv, dir, &l, next, keyFor("admin"), t0)
	if err != nil || got.Version != 3 {
		t.Fatalf("publish: v%d err %v", got.Version, err)
	}
	if hubVersion(t, ctx, kv) != 3 {
		t.Error("hub not at v3")
	}
	saved, _, err := LoadLocal(dir)
	if err != nil || saved.Roster.Version != 3 || len(saved.Roster.Hosts) != 1 {
		t.Fatalf("roster.json: %+v err %v", saved.Roster, err)
	}
}

func TestPresence(t *testing.T) {
	ctx, c := hub(t)
	kv, err := Bucket(ctx, c.JS, BucketHosts)
	if err != nil {
		t.Fatal(err)
	}
	r := testRoster(2)
	p := Presence{Host: "k16", Rev: "abc", OS: "linux", Notes: "hello", TS: Stamp(time.Now())}
	p.Sign(keyFor("k16"))
	if err := PutPresence(ctx, kv, p); err != nil {
		t.Fatal(err)
	}
	forged := Presence{Host: "mac", Rev: "x", OS: "darwin", Notes: "trust me", TS: Stamp(time.Now())}
	forged.Sign(keyFor("k16"))
	if err := PutPresence(ctx, kv, forged); err != nil {
		t.Fatal(err)
	}
	states := func(now time.Time) map[string]Seen {
		seen, err := ReadPresence(ctx, kv, r, now)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]Seen{}
		for _, s := range seen {
			out[s.Host.Name] = s
		}
		return out
	}
	s := states(time.Now())
	if s["k16"].State != Awake || s["k16"].Presence.Notes != "hello" {
		t.Errorf("k16: %+v", s["k16"])
	}
	if s["mac"].State != Unverified || s["mac"].Presence.Notes != "" {
		t.Errorf("mac (signed by k16): %+v", s["mac"])
	}
	if got := states(time.Now().Add(4 * time.Minute))["k16"].State; got != Asleep {
		t.Errorf("k16 four minutes on: %s", got)
	}
}
