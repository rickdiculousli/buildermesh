package hosts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// Sync first takes a newer roster.json (another process may have written
// it), then reads the hub's roster: one the pinned key signed with a
// higher version becomes the box's (saved to roster.json, never over a
// newer one); when the hub's copy is missing, unsigned or lower, the box's
// own is put back (compare-and-set,
// so a concurrent writer wins and the next sync looks again). accepted
// reports a newer roster taken; err is a hub that can't be read.
func Sync(ctx context.Context, kv jetstream.KeyValue, stateDir string, l *Local) (accepted bool, err error) {
	if reload(stateDir, l) {
		accepted = true
	}
	h, err := readHub(ctx, kv)
	if err != nil {
		return accepted, err
	}
	if h.parsed && l.Offer(h.roster) {
		return true, saveNewer(stateDir, l)
	}
	valid := h.parsed && h.roster.Verify(l.Pinned) == nil
	if !h.found || !valid || h.roster.Version < l.Roster.Version {
		if err := put(ctx, kv, h, l.Roster); err != nil && !isConflict(err) {
			return accepted, fmt.Errorf("re-put roster v%d: %w", l.Roster.Version, err)
		}
	}
	return accepted, nil
}

// reload takes roster.json when another process (the admin's CLI, or init
// after a fresh invite) wrote a newer roster or a new pin there since l was
// read; it reports whether l changed. roster.json is the box's own record,
// so a roster saved there and not yet on the hub is what gets put back.
func reload(stateDir string, l *Local) bool {
	disk, ok, err := LoadLocal(stateDir)
	if err != nil || !ok {
		return false
	}
	if disk.Pinned != l.Pinned {
		*l = disk
		return true
	}
	return l.Offer(disk.Roster)
}

// saveNewer writes l to roster.json unless the file already holds the same
// pin's roster at l's version or higher, which then becomes l.
func saveNewer(stateDir string, l *Local) error {
	if disk, ok, err := LoadLocal(stateDir); err == nil && ok && disk.Pinned == l.Pinned && disk.Roster.Version >= l.Roster.Version {
		*l = disk
		return nil
	}
	return SaveLocal(stateDir, *l)
}

func isConflict(err error) bool {
	return errors.Is(err, jetstream.ErrKeyExists) || errors.Is(err, jetstream.ErrKeyRevisionMismatch)
}

// Next is the roster after l's own (never the hub's) with rows, issued
// now under adminKey, unsigned in its signed form: what a request shows
// and the admin key then signs.
func (l *Local) Next(rows []Host, adminKey string, now time.Time) Roster {
	next := Roster{Version: l.Roster.Version + 1, AdminKey: adminKey, Hosts: slices.Clone(rows), IssuedAt: Stamp(now)}
	next.Prepare()
	return next
}

// Publish takes next, signed, as the version after the box's own, saves it
// to roster.json and writes it to the hub, compare-and-set on the entry's
// revision (retried when another writer gets there first). The saved copy
// is what every daemon re-puts, so a failed hub write heals.
func Publish(ctx context.Context, kv jetstream.KeyValue, stateDir string, l *Local, next Roster) (Roster, error) {
	if next.Version != l.Roster.Version+1 {
		return Roster{}, fmt.Errorf("roster v%d was signed, but this box is at v%d now: nothing published, run it again", next.Version, l.Roster.Version)
	}
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

// RosterDiff is what next changes in old, a line per row by name:
// "+ k16: secure, accept nix,ping, key <fp>", "- k16",
// "k16: accept nix,ping -> nix,ping,stop-thread"; "no row changes" when
// none does.
func RosterDiff(old, next Roster) []string {
	rows := func(r Roster) map[string]Host {
		m := map[string]Host{}
		for _, h := range r.Hosts {
			m[h.Name] = h
		}
		return m
	}
	om, nm := rows(old), rows(next)
	var names []string
	for n := range om {
		names = append(names, n)
	}
	for n := range nm {
		if _, ok := om[n]; !ok {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	secure := func(b bool) string {
		if b {
			return "secure"
		}
		return "not secure"
	}
	accept := func(h Host) string {
		if len(h.Accept) == 0 {
			return "(none)"
		}
		return strings.Join(Names(h.Accept), ",")
	}
	var out []string
	for _, n := range names {
		o, inOld := om[n]
		w, inNew := nm[n]
		switch {
		case !inOld:
			out = append(out, fmt.Sprintf("+ %s: %s, accept %s, key %s", n, secure(w.Secure), accept(w), Fingerprint(w.Key)))
		case !inNew:
			out = append(out, "- "+n)
		default:
			var ch []string
			if o.Key != w.Key {
				ch = append(ch, fmt.Sprintf("key %s -> %s", Fingerprint(o.Key), Fingerprint(w.Key)))
			}
			if o.Secure != w.Secure {
				ch = append(ch, fmt.Sprintf("%s -> %s", secure(o.Secure), secure(w.Secure)))
			}
			if o.ApproveKey != w.ApproveKey {
				ch = append(ch, fmt.Sprintf("approve key %s -> %s", orNone(o.ApproveKey, Fingerprint), orNone(w.ApproveKey, Fingerprint)))
			}
			if a, b := accept(o), accept(w); a != b {
				ch = append(ch, fmt.Sprintf("accept %s -> %s", a, b))
			}
			if len(ch) > 0 {
				out = append(out, n+": "+strings.Join(ch, "; "))
			}
		}
	}
	if len(out) == 0 {
		out = []string{"no row changes"}
	}
	return out
}

// orNone is f(s), or "(none)" for an empty s.
func orNone(s string, f func(string) string) string {
	if s == "" {
		return "(none)"
	}
	return f(s)
}
