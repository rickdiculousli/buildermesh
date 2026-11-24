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

// Priority ranks a wait for a slot: an urgent waiter takes a freed place
// before a best-effort one and may make a best-effort holder yield. A lease
// with none (taken by lock-lease acquire, the daemon or an older release)
// is never asked to yield.
type Priority string

const (
	Urgent     Priority = "urgent"
	BestEffort Priority = "best-effort"
)

// Word is the priority as status lines say it; "" for none.
func (p Priority) Word() string { return strings.ReplaceAll(string(p), "-", " ") }

// Lease is one entry of bm-leases: the n-th place of a slot.
type Lease struct {
	Key       string
	Slot      string
	N         int
	Holder    string
	ExpiresAt time.Time
	Priority  Priority
	Started   time.Time // when the holder took it
	Yield     string    // who asked a best-effort holder to yield: "<holder> (<why>)"
	rev       uint64
}

type leaseValue struct {
	Holder    string   `json:"holder"`
	ExpiresAt string   `json:"expires_at"`
	Priority  Priority `json:"priority,omitempty"`
	Started   string   `json:"started,omitempty"`
	Yield     string   `json:"yield,omitempty"`
}

func (v leaseValue) data() ([]byte, error) { return json.Marshal(v) }

// waiterMark separates a slot from a waiter's id in its key,
// <slot>.w-<priority>-<holder>: no lease place, which ends in a number, reads as one.
const waiterMark = ".w-"

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
	return AcquireRanked(ctx, c, slot, holder, "", ttl, capacity, now)
}

// AcquireRanked is AcquireLease for a holder that waited at prio: the
// lease carries the priority and when it was taken.
func AcquireRanked(ctx context.Context, c *Client, slot, holder string, prio Priority, ttl time.Duration, capacity int, now time.Time) (string, error) {
	kv, err := bucket(ctx, c, BucketLeases)
	if err != nil {
		return "", err
	}
	v := leaseValue{Holder: holder, ExpiresAt: now.Add(ttl).UTC().Format(time.RFC3339), Priority: prio}
	if prio != "" {
		v.Started = now.UTC().Format(time.RFC3339Nano)
	}
	data, err := v.data()
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

// RenewLease moves key's expiry to now+ttl, keeping the rest of its value;
// ErrNotHolder unless holder holds it (whether or not it has expired, so
// long as nobody took it over). An update that loses to another write (a
// waiter asking it to yield) reads again.
func RenewLease(ctx context.Context, c *Client, key, holder string, ttl time.Duration, now time.Time) error {
	kv, err := bucket(ctx, c, BucketLeases)
	if err != nil {
		return err
	}
	for range 3 {
		v, rev, err := readLease(ctx, kv, key)
		if err != nil {
			return err
		}
		if rev == 0 || v.Holder != holder {
			return ErrNotHolder
		}
		v.ExpiresAt = now.Add(ttl).UTC().Format(time.RFC3339)
		data, err := v.data()
		if err != nil {
			return err
		}
		if _, err = kv.Update(ctx, key, data, rev); err == nil || !isConflict(err) {
			return err
		}
	}
	return ErrNotHolder
}

// LeaseYield reads who asked holder's key to yield ("" when nobody did);
// ErrNotHolder unless holder holds it.
func LeaseYield(ctx context.Context, c *Client, key, holder string) (string, error) {
	kv, err := bucket(ctx, c, BucketLeases)
	if err != nil {
		return "", err
	}
	v, rev, err := readLease(ctx, kv, key)
	if err != nil {
		return "", err
	}
	if rev == 0 || v.Holder != holder {
		return "", ErrNotHolder
	}
	return v.Yield, nil
}

// AskYield marks l, a best-effort lease as SlotQueue read it, to yield to
// by; a lease that changed since is left alone (the next poll reads it again).
func AskYield(ctx context.Context, c *Client, l Lease, by string) error {
	kv, err := bucket(ctx, c, BucketLeases)
	if err != nil {
		return err
	}
	v, rev, err := readLease(ctx, kv, l.Key)
	if err != nil || rev != l.rev || v.Priority != BestEffort || v.Yield != "" {
		return err
	}
	v.Yield = by
	data, err := v.data()
	if err != nil {
		return err
	}
	if _, err := kv.Update(ctx, l.Key, data, rev); err != nil && !isConflict(err) {
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
	keys, err := listKeys(ctx, c, kv, ">")
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	out := make([]Lease, 0, len(keys))
	for _, k := range keys {
		if isWaiterKey(k) {
			continue
		}
		v, rev, err := readLease(ctx, kv, k)
		if err != nil {
			return nil, err
		}
		if rev == 0 {
			continue
		}
		l := v.lease(k, rev)
		if i := strings.LastIndexByte(k, '.'); i >= 0 {
			if n, err := strconv.Atoi(k[i+1:]); err == nil {
				l.Slot, l.N = k[:i], n
			}
		}
		out = append(out, l)
	}
	return out, nil
}

func (v leaseValue) lease(key string, rev uint64) Lease {
	l := Lease{Key: key, Slot: key, N: -1, Holder: v.Holder, Priority: v.Priority, Yield: v.Yield, rev: rev}
	l.ExpiresAt, _ = time.Parse(time.RFC3339, v.ExpiresAt)
	l.Started, _ = time.Parse(time.RFC3339Nano, v.Started)
	return l
}

// isWaiterKey reports a waiter's key (waiterID has no dot).
func isWaiterKey(k string) bool {
	i := strings.LastIndex(k, waiterMark)
	return i > 0 && !strings.Contains(k[i+len(waiterMark):], ".")
}

// Waiter is one wait for a slot, kept beside its leases as <slot>.w-<priority>-<holder>
// while the waiter polls, so every waiter reads the same line.
type Waiter struct {
	Holder    string    `json:"holder"`
	Priority  Priority  `json:"priority"`
	Why       string    `json:"why,omitempty"` // an urgent wait's reason, as a yield names it
	Since     time.Time `json:"since"`         // when it began waiting: its place in line
	ExpiresAt time.Time `json:"expires_at"`    // a waiter that stopped polling drops out after it
}

// PutWaiter writes w for slot, its expiry moved to now+ttl. A waiter's key
// is its holder's and priority's (waiterID): one wait per holder, priority
// and slot, so the bucket keeps a key or two per holder however many waits
// end, and a lead's landing never shares a record with its own
// best-effort run.
func PutWaiter(ctx context.Context, c *Client, slot string, w Waiter, ttl time.Duration, now time.Time) error {
	w.ExpiresAt = now.Add(ttl)
	return put(ctx, c, BucketLeases, slot+waiterMark+waiterID(w.Holder, w.Priority), w)
}

// DeleteWaiter removes holder's wait at prio for slot.
func DeleteWaiter(ctx context.Context, c *Client, slot, holder string, prio Priority) error {
	kv, err := bucket(ctx, c, BucketLeases)
	if err != nil {
		return err
	}
	return kv.Delete(ctx, slot+waiterMark+waiterID(holder, prio))
}

// waiterID is holder and prio as one key token with no dot.
func waiterID(holder string, prio Priority) string {
	return string(prio) + "-" + strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return r
		}
		return '_'
	}, holder)
}

// SlotLine is a slot as a waiter reads it: its places held and unexpired, and
// its waiters not expired in turn order (urgent first, then oldest first).
type SlotLine struct {
	Held    []Lease
	Free    int
	Waiters []Waiter
}

// SlotQueue reads slot's places below capacity and its waiters at now.
func SlotQueue(ctx context.Context, c *Client, slot string, capacity int, now time.Time) (SlotLine, error) {
	kv, err := bucket(ctx, c, BucketLeases)
	if err != nil {
		return SlotLine{}, err
	}
	q := SlotLine{Free: capacity}
	for n := range capacity {
		key := slot + "." + strconv.Itoa(n)
		v, rev, err := readLease(ctx, kv, key)
		if err != nil {
			return SlotLine{}, err
		}
		if rev != 0 && !v.expired(now) {
			l := v.lease(key, rev)
			l.Slot, l.N = slot, n
			q.Held = append(q.Held, l)
			q.Free--
		}
	}
	keys, err := listKeys(ctx, c, kv, slot+".>")
	if err != nil && !errors.Is(err, jetstream.ErrNoKeysFound) {
		return SlotLine{}, err
	}
	for _, k := range keys {
		if !strings.HasPrefix(k, slot+waiterMark) || !isWaiterKey(k) {
			continue
		}
		e, err := kv.Get(ctx, k)
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			continue
		}
		if err != nil {
			return SlotLine{}, err
		}
		var w Waiter
		if json.Unmarshal(e.Value(), &w) != nil || !w.ExpiresAt.After(now) {
			continue
		}
		q.Waiters = append(q.Waiters, w)
	}
	sort.Slice(q.Waiters, func(i, j int) bool {
		a, b := q.Waiters[i], q.Waiters[j]
		if (a.Priority == Urgent) != (b.Priority == Urgent) {
			return a.Priority == Urgent
		}
		if !a.Since.Equal(b.Since) {
			return a.Since.Before(b.Since)
		}
		return a.Holder < b.Holder
	})
	return q, nil
}
