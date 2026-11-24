package mesh_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	. "github.com/rickdiculousli/buildermesh/internal/mesh"
)

func TestLeaseCapacityAndExpiry(t *testing.T) {
	c, ctx := connect(t)
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	ttl := 10 * time.Minute

	k1, err := AcquireLease(ctx, c, "game", "a", ttl, 2, now)
	if err != nil || k1 != "game.0" {
		t.Fatalf("first: %q %v", k1, err)
	}
	k2, err := AcquireLease(ctx, c, "game", "b", ttl, 2, now)
	if err != nil || k2 != "game.1" {
		t.Fatalf("second: %q %v", k2, err)
	}
	if _, err := AcquireLease(ctx, c, "game", "c", ttl, 2, now); !errors.Is(err, ErrBusy) {
		t.Fatalf("third: %v, want ErrBusy", err)
	}

	// Renewal needs the holder; a renewed lease outlives the other.
	if err := RenewLease(ctx, c, k1, "b", ttl, now); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("renew by other: %v", err)
	}
	if err := RenewLease(ctx, c, "game.9", "a", ttl, now); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("renew missing: %v", err)
	}
	if err := RenewLease(ctx, c, k1, "a", ttl, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// At game.1's expiry it is claimable again; game.0 was renewed and is not.
	later := now.Add(ttl)
	k3, err := AcquireLease(ctx, c, "game", "c", ttl, 2, later)
	if err != nil || k3 != "game.1" {
		t.Fatalf("after expiry: %q %v", k3, err)
	}
	if err := ReleaseLease(ctx, c, k3, "b"); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("release by expired holder: %v", err)
	}
	leases, err := ListLeases(ctx, c)
	if err != nil || len(leases) != 2 || leases[0].Key != "game.0" || leases[0].Holder != "a" ||
		leases[1].Slot != "game" || leases[1].N != 1 || leases[1].Holder != "c" ||
		!leases[1].ExpiresAt.Equal(later.Add(ttl)) {
		t.Fatalf("list: %+v %v", leases, err)
	}

	if err := ReleaseLease(ctx, c, k1, "a"); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseLease(ctx, c, k1, "a"); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("second release: %v", err)
	}
	k4, err := AcquireLease(ctx, c, "game", "d", ttl, 2, later)
	if err != nil || k4 != "game.0" {
		t.Fatalf("after release: %q %v", k4, err)
	}
}

// Waiters read in turn order (urgent first, then oldest), beside the
// leases but never listed as one; a renew keeps a lease's priority and the
// yield asked of it, and a yield is asked only of a best-effort lease.
func TestSlotLineOrderAndYield(t *testing.T) {
	c, ctx := connect(t)
	now := time.Now()
	ttl := time.Minute
	k, err := AcquireRanked(ctx, c, "land.app.w-x", "h", BestEffort, ttl, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	for i, w := range []Waiter{
		{Holder: "a.one", Priority: BestEffort, Since: now},
		{Holder: "b", Priority: BestEffort, Since: now.Add(time.Second)},
		{Holder: "c", Priority: Urgent, Since: now.Add(2 * time.Second), Why: "land of /wt"},
		{Holder: "gone", Priority: Urgent, Since: now.Add(-time.Hour)},
	} {
		wttl := ttl
		if w.Holder == "gone" {
			wttl = -time.Second
		}
		if err := PutWaiter(ctx, c, "land.app", w, wttl, now); err != nil {
			t.Fatal(i, err)
		}
	}
	line, err := SlotQueue(ctx, c, "land.app", 1, now)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, w := range line.Waiters {
		order = append(order, w.Holder)
	}
	if got := strings.Join(order, " "); got != "c a.one b" || line.Free != 1 || len(line.Held) != 0 {
		t.Fatalf("line: %s %+v", got, line)
	}
	other, err := SlotQueue(ctx, c, "land.app.w-x", 1, now)
	if err != nil || len(other.Held) != 1 || other.Held[0].Priority != BestEffort || len(other.Waiters) != 0 {
		t.Fatalf("the other slot: %+v %v", other, err)
	}
	if err := AskYield(ctx, c, other.Held[0], "c (land of /wt)"); err != nil {
		t.Fatal(err)
	}
	if err := RenewLease(ctx, c, k, "h", ttl, now); err != nil {
		t.Fatal(err)
	}
	if by, err := LeaseYield(ctx, c, k, "h"); err != nil || by != "c (land of /wt)" {
		t.Fatalf("yield after renew: %q %v", by, err)
	}
	leases, err := ListLeases(ctx, c)
	if err != nil || len(leases) != 1 || leases[0].Key != k || leases[0].Priority != BestEffort {
		t.Fatalf("leases: %+v %v", leases, err)
	}
	if err := DeleteWaiter(ctx, c, "land.app", "a.one", BestEffort); err != nil {
		t.Fatal(err)
	}
	// An unranked lease is never asked to yield.
	u, err := AcquireLease(ctx, c, "solo", "h", ttl, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	solo, _ := SlotQueue(ctx, c, "solo", 1, now)
	if err := AskYield(ctx, c, solo.Held[0], "c"); err != nil {
		t.Fatal(err)
	}
	if by, err := LeaseYield(ctx, c, u, "h"); err != nil || by != "" {
		t.Fatalf("unranked yield: %q %v", by, err)
	}
}
