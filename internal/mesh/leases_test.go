package mesh_test

import (
	"errors"
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
