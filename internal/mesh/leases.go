package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

var (
	// ErrBusy means every lease of a slot is held and unexpired.
	ErrBusy = errors.New("slot is busy")
	// ErrNotHolder means the lease is absent or held by someone else.
	ErrNotHolder = errors.New("not the lease holder")
)

// Lease is one entry of bm-leases: the n-th place of a slot.
type Lease struct {
	Key       string
	Slot      string
	N         int
	Holder    string
	ExpiresAt time.Time
}

type leaseValue struct {
	Holder    string `json:"holder"`
	ExpiresAt string `json:"expires_at"`
}

func leaseData(holder string, expires time.Time) ([]byte, error) {
	return json.Marshal(leaseValue{Holder: holder, ExpiresAt: expires.UTC().Format(time.RFC3339)})
}

// readLease returns the entry's value and revision; absent is rev 0.
func readLease(ctx context.Context, kv jetstream.KeyValue, key string) (leaseValue, uint64, error) {
	e, err := kv.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return leaseValue{}, 0, nil
	}
	if err != nil {
		return leaseValue{}, 0, err
	}
	var v leaseValue
	if err := json.Unmarshal(e.Value(), &v); err != nil {
		return leaseValue{}, 0, fmt.Errorf("lease %s: %w", key, err)
	}
	return v, e.Revision(), nil
}

// expired reports whether v has run out at now; an unreadable expiry counts
// as run out so a corrupt entry cannot block a slot forever.
func (v leaseValue) expired(now time.Time) bool {
	t, err := time.Parse(time.RFC3339, v.ExpiresAt)
	return err != nil || !t.After(now)
}

// AcquireLease claims the first free place <slot>.<n>, n < capacity, for
// holder until now+ttl. A place is free when absent or expired; it is claimed
// with Create or an Update at the read revision, so of two racing acquirers
// one wins and the other moves on. ErrBusy when every place is held.
func AcquireLease(ctx context.Context, c *Client, slot, holder string, ttl time.Duration, capacity int, now time.Time) (string, error) {
	kv, err := bucket(ctx, c, BucketLeases)
	if err != nil {
		return "", err
	}
	data, err := leaseData(holder, now.Add(ttl))
	if err != nil {
		return "", err
	}
	for n := 0; n < capacity; n++ {
		key := slot + "." + strconv.Itoa(n)
		v, rev, err := readLease(ctx, kv, key)
		if err != nil {
			return "", err
		}
		if rev != 0 && !v.expired(now) {
			continue
		}
		if rev == 0 {
			_, err = kv.Create(ctx, key, data)
		} else {
			_, err = kv.Update(ctx, key, data, rev)
		}
		if err == nil {
			return key, nil
		}
		if !isConflict(err) {
			return "", err
		}
	}
	return "", ErrBusy
}

// RenewLease moves key's expiry to now+ttl; ErrNotHolder unless holder
// holds it (whether or not it has expired, so long as nobody took it over).
func RenewLease(ctx context.Context, c *Client, key, holder string, ttl time.Duration, now time.Time) error {
	kv, err := bucket(ctx, c, BucketLeases)
	if err != nil {
		return err
	}
	v, rev, err := readLease(ctx, kv, key)
	if err != nil {
		return err
	}
	if rev == 0 || v.Holder != holder {
		return ErrNotHolder
	}
	data, err := leaseData(holder, now.Add(ttl))
	if err != nil {
		return err
	}
	if _, err := kv.Update(ctx, key, data, rev); err != nil {
		if isConflict(err) {
			return ErrNotHolder
		}
		return err
	}
	return nil
}

// ReleaseLease deletes key; ErrNotHolder unless holder holds it.
func ReleaseLease(ctx context.Context, c *Client, key, holder string) error {
	kv, err := bucket(ctx, c, BucketLeases)
	if err != nil {
		return err
	}
	v, rev, err := readLease(ctx, kv, key)
	if err != nil {
		return err
	}
	if rev == 0 || v.Holder != holder {
		return ErrNotHolder
	}
	if err := kv.Delete(ctx, key, jetstream.LastRevision(rev)); err != nil {
		if isConflict(err) {
			return ErrNotHolder
		}
		return err
	}
	return nil
}

// ListLeases returns every entry, expired or not, sorted by key.
func ListLeases(ctx context.Context, c *Client) ([]Lease, error) {
	kv, err := bucket(ctx, c, BucketLeases)
	if err != nil {
		return nil, err
	}
	keys, err := kv.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	out := make([]Lease, 0, len(keys))
	for _, k := range keys {
		v, rev, err := readLease(ctx, kv, k)
		if err != nil {
			return nil, err
		}
		if rev == 0 {
			continue
		}
		l := Lease{Key: k, Slot: k, N: -1, Holder: v.Holder}
		if i := strings.LastIndexByte(k, '.'); i >= 0 {
			if n, err := strconv.Atoi(k[i+1:]); err == nil {
				l.Slot, l.N = k[:i], n
			}
		}
		l.ExpiresAt, _ = time.Parse(time.RFC3339, v.ExpiresAt)
		out = append(out, l)
	}
	return out, nil
}
