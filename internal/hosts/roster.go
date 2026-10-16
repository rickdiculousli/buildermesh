package hosts

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// The hosts layer's buckets, outside bm.> so no stream overlaps: the roster
// (key roster; 023 adds registry) and each box's presence (key <host>).
const (
	BucketRoster = "bm-roster"
	RosterKey    = "roster"
	BucketHosts  = "bm-hosts"
)

// Local is what a joined box keeps in <state dir>/roster.json: the admin
// key it pinned when it joined and its newest valid roster.
type Local struct {
	Pinned string `json:"pinned"`
	Roster Roster `json:"roster"`
}

// LocalPath is <state dir>/roster.json.
func LocalPath(stateDir string) string { return filepath.Join(stateDir, "roster.json") }

// LoadLocal reads roster.json; ok is false when the box hasn't joined. A
// file whose roster doesn't verify against its own pin is an error.
func LoadLocal(stateDir string) (l Local, ok bool, err error) {
	b, err := os.ReadFile(LocalPath(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return Local{}, false, nil
	}
	if err != nil {
		return Local{}, false, err
	}
	if err := json.Unmarshal(b, &l); err != nil {
		return Local{}, false, fmt.Errorf("%s: %w", LocalPath(stateDir), err)
	}
	if err := l.Roster.Verify(l.Pinned); err != nil {
		return Local{}, false, fmt.Errorf("%s: %w", LocalPath(stateDir), err)
	}
	return l, true, nil
}

// SaveLocal writes roster.json (mode 0600) through a rename, so a crash
// leaves the old file or the new one.
func SaveLocal(stateDir string, l Local) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(LocalPath(stateDir), append(b, '\n'))
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Offer takes r as the box's roster when the pinned key signed it and its
// version is higher; it reports whether it did.
func (l *Local) Offer(r Roster) bool {
	if r.Verify(l.Pinned) != nil || r.Version <= l.Roster.Version {
		return false
	}
	l.Roster = r
	return true
}

// Bucket opens a hosts bucket, creating it when it is missing (a token
// holder may have deleted it).
func Bucket(ctx context.Context, js jetstream.JetStream, name string) (jetstream.KeyValue, error) {
	kv, err := js.KeyValue(ctx, name)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		kv, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: name, History: 1, Storage: jetstream.FileStorage})
		if errors.Is(err, jetstream.ErrBucketExists) {
			kv, err = js.KeyValue(ctx, name)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("bucket %s: %w", name, err)
	}
	return kv, nil
}

// hubRoster is the hub's roster entry: absent, or its revision and, when
// it parses, the document.
type hubRoster struct {
	found  bool
	rev    uint64
	roster Roster
	parsed bool
}

func readHub(ctx context.Context, kv jetstream.KeyValue) (hubRoster, error) {
	e, err := kv.Get(ctx, RosterKey)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return hubRoster{}, nil
	}
	if err != nil {
		return hubRoster{}, err
	}
	h := hubRoster{found: true, rev: e.Revision()}
	h.parsed = json.Unmarshal(e.Value(), &h.roster) == nil
	return h, nil
}

// HubRoster is the hub's roster as stored, unverified; ok is false when
// the key is missing or doesn't parse.
func HubRoster(ctx context.Context, kv jetstream.KeyValue) (Roster, bool, error) {
	h, err := readHub(ctx, kv)
	return h.roster, h.found && h.parsed, err
}

// put writes r over the entry h read, compare-and-set on its revision.
func put(ctx context.Context, kv jetstream.KeyValue, h hubRoster, r Roster) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if h.found {
		_, err = kv.Update(ctx, RosterKey, b, h.rev)
	} else {
		_, err = kv.Create(ctx, RosterKey, b)
	}
	return err
}

// Sync reads the hub's roster: one the pinned key signed with a higher
// version becomes the box's (saved to roster.json); when the hub's copy is
// missing, unsigned or lower, the box's own is put back (compare-and-set,
// so a concurrent writer wins and the next sync looks again). accepted
// reports a newer roster taken; err is a hub that can't be read.
func Sync(ctx context.Context, kv jetstream.KeyValue, stateDir string, l *Local) (accepted bool, err error) {
	h, err := readHub(ctx, kv)
	if err != nil {
		return false, err
	}
	if h.parsed && l.Offer(h.roster) {
		return true, SaveLocal(stateDir, *l)
	}
	valid := h.parsed && h.roster.Verify(l.Pinned) == nil
	if !h.found || !valid || h.roster.Version < l.Roster.Version {
		if err := put(ctx, kv, h, l.Roster); err != nil && !isConflict(err) {
			return false, fmt.Errorf("re-put roster v%d: %w", l.Roster.Version, err)
		}
	}
	return false, nil
}

func isConflict(err error) bool {
	return errors.Is(err, jetstream.ErrKeyExists) || errors.Is(err, jetstream.ErrKeyRevisionMismatch)
}

// Publish signs next as the version after the box's own (never the hub's),
// saves it to roster.json and writes it to the hub, compare-and-set on the
// entry's revision (retried when another writer gets there first). The
// saved copy is what every daemon re-puts, so a failed hub write heals.
func Publish(ctx context.Context, kv jetstream.KeyValue, stateDir string, l *Local, next Roster,
	admin ed25519.PrivateKey, now time.Time) (Roster, error) {
	next.Version = l.Roster.Version + 1
	next.AdminKey = KeyHex(admin.Public().(ed25519.PublicKey))
	next.IssuedAt = Stamp(now)
	next.Sign(admin)
	if l.Pinned == "" {
		l.Pinned = next.AdminKey
	}
	if err := next.Verify(l.Pinned); err != nil {
		return Roster{}, err
	}
	l.Roster = next
	if err := SaveLocal(stateDir, *l); err != nil {
		return Roster{}, err
	}
	var err error
	for range 3 {
		var h hubRoster
		if h, err = readHub(ctx, kv); err != nil {
			break
		}
		if err = put(ctx, kv, h, next); err == nil || !isConflict(err) {
			break
		}
	}
	if err != nil {
		return next, fmt.Errorf("roster v%d saved here but not on the hub (the daemon puts it back within a minute): %w", next.Version, err)
	}
	return next, nil
}
