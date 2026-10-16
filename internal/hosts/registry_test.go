package hosts

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func testRegistry(version int64, rows string) Registry {
	r := Registry{Version: version, AdminKey: pubFor("admin"), Rows: rows, IssuedAt: "2026-10-16T10:00:00Z"}
	r.Sign(keyFor("admin"))
	return r
}

func TestSyncRegistryTakesOnlyAHigherVersionTheAdminSignedAndTheBoxReads(t *testing.T) {
	ctx, c := hub(t)
	kv := rosterKV(t, ctx, c)
	dir := t.TempDir()
	pinned := pubFor("admin")
	var applied []int64
	apply := func(r Registry) error {
		if strings.Contains(r.Rows, "newkey") {
			return errors.New("registry v" + r.Rows + " needs a newer bmesh")
		}
		applied = append(applied, r.Version)
		return nil
	}
	putReg := func(r Registry) {
		b, _ := json.Marshal(r)
		if _, err := kv.Put(ctx, RegistryKey, b); err != nil {
			t.Fatal(err)
		}
	}
	hubReg := func() Registry {
		e, err := kv.Get(ctx, RegistryKey)
		if err != nil {
			t.Fatal(err)
		}
		var r Registry
		_ = json.Unmarshal(e.Value(), &r)
		return r
	}

	// No registry anywhere: nothing taken, nothing put.
	if res, err := SyncRegistry(ctx, kv, dir, pinned, apply); err != nil || res.Took != 0 {
		t.Fatalf("empty: %+v %v", res, err)
	}

	putReg(testRegistry(3, "a"))
	if res, err := SyncRegistry(ctx, kv, dir, pinned, apply); err != nil || res.Took != 3 {
		t.Fatalf("v3: %+v %v", res, err)
	}
	if r, ok, _ := LoadRegistry(dir, pinned); !ok || r.Version != 3 {
		t.Fatalf("registry.json after v3: %v %+v", ok, r)
	}

	// A lower version: kept v3, and v3 put back on the hub.
	putReg(testRegistry(2, "b"))
	if res, _ := SyncRegistry(ctx, kv, dir, pinned, apply); res.Took != 0 || hubReg().Version != 3 {
		t.Fatalf("a lower version: %+v hub v%d", res, hubReg().Version)
	}

	// A non-admin signature, at a higher version: refused, put back.
	forged := Registry{Version: 9, AdminKey: pubFor("k16"), Rows: "evil", IssuedAt: "2026-10-16T10:00:00Z"}
	forged.Sign(keyFor("k16"))
	putReg(forged)
	if res, _ := SyncRegistry(ctx, kv, dir, pinned, apply); res.Took != 0 || hubReg().Version != 3 {
		t.Fatalf("a non-admin signature: %+v hub v%d", res, hubReg().Version)
	}
	// The admin key's name on another key's signature: refused.
	forged.AdminKey = pinned
	putReg(forged)
	if res, _ := SyncRegistry(ctx, kv, dir, pinned, apply); res.Took != 0 || hubReg().Version != 3 {
		t.Fatalf("a forged signature: %+v hub v%d", res, hubReg().Version)
	}

	// A deleted key comes back.
	if err := kv.Delete(ctx, RegistryKey); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncRegistry(ctx, kv, dir, pinned, apply); err != nil || hubReg().Version != 3 {
		t.Fatalf("deleted: %v hub v%d", err, hubReg().Version)
	}

	// A registry this box can't read: kept v3, reported, and the newer one
	// left on the hub for the boxes that can.
	putReg(testRegistry(4, "newkey"))
	res, err := SyncRegistry(ctx, kv, dir, pinned, apply)
	if err != nil || res.Took != 0 || res.Refused != 4 || res.RefuseErr == nil {
		t.Fatalf("unreadable: %+v %v", res, err)
	}
	if r, _, _ := LoadRegistry(dir, pinned); r.Version != 3 || hubReg().Version != 4 {
		t.Fatalf("unreadable: box v%d hub v%d", r.Version, hubReg().Version)
	}
	if len(applied) != 1 || applied[0] != 3 {
		t.Fatalf("applied %v", applied)
	}

	// A box re-pinned to another admin starts again from none.
	if _, ok, _ := LoadRegistry(dir, pubFor("other")); ok {
		t.Fatal("registry.json signed by the old admin was read under a new pin")
	}
}

func TestPublishRegistryTakesTheNextVersionFromTheBox(t *testing.T) {
	ctx, c := hub(t)
	kv := rosterKV(t, ctx, c)
	dir := t.TempDir()
	var applied []string
	apply := func(r Registry) error { applied = append(applied, r.Rows); return nil }
	r1, err := PublishRegistry(ctx, kv, dir, "one", keyFor("admin"), time.Now(), apply)
	if err != nil || r1.Version != 1 || r1.Verify(pubFor("admin")) != nil {
		t.Fatalf("first: %+v %v", r1, err)
	}
	// The hub's copy is never the base: a higher one there doesn't move
	// the box's next version.
	b, _ := json.Marshal(testRegistry(40, "hub"))
	if _, err := kv.Put(ctx, RegistryKey, b); err != nil {
		t.Fatal(err)
	}
	r2, err := PublishRegistry(ctx, kv, dir, "two", keyFor("admin"), time.Now(), apply)
	if err != nil || r2.Version != 2 {
		t.Fatalf("second: %+v %v", r2, err)
	}
	if l, _, _ := LoadRegistry(dir, pubFor("admin")); l.Version != 2 || l.Rows != "two" {
		t.Fatalf("registry.json: %+v", l)
	}
	if strings.Join(applied, ",") != "one,two" {
		t.Fatalf("applied %v", applied)
	}
	// A refused apply publishes nothing.
	if _, err := PublishRegistry(ctx, kv, dir, "three", keyFor("admin"), time.Now(), func(Registry) error {
		return errors.New("no")
	}); err == nil {
		t.Fatal("a refused apply published")
	}
	if l, _, _ := LoadRegistry(dir, pubFor("admin")); l.Version != 2 {
		t.Fatalf("after a refused apply: v%d", l.Version)
	}
}
