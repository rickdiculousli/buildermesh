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

// RegistryKey is the registry's key in BucketRoster.
const RegistryKey = "registry"

// Registry is repos.toml as the admin published it (023): its text, rows
// and comments as written, signed by the admin key. A box reads the rows
// with its own loader before it takes one.
type Registry struct {
	Type     string `json:"type"`
	Version  int64  `json:"version"`
	AdminKey string `json:"admin_key"`
	Rows     string `json:"rows"`
	IssuedAt string `json:"issued_at"`
	Sig      string `json:"sig"`
}

// Bytes is the registry's signed bytes: type, version, admin_key, rows,
// issued_at.
func (r *Registry) Bytes() []byte {
	var e enc
	e.str(r.Type)
	e.int(r.Version)
	e.str(r.AdminKey)
	e.str(r.Rows)
	e.str(r.IssuedAt)
	return e.b
}

func (r *Registry) Sign(admin ed25519.PrivateKey) {
	r.Type = TagRegistry
	r.Sig = sign(admin, r.Bytes())
}

// Verify checks the type, that the registry names pinned as its admin
// key, and the signature.
func (r *Registry) Verify(pinned string) error {
	if r.Type != TagRegistry {
		return fmt.Errorf("type %q: want %s", r.Type, TagRegistry)
	}
	if r.AdminKey != pinned {
		return fmt.Errorf("registry v%d is signed by %s, not the pinned admin %s", r.Version,
			Fingerprint(r.AdminKey), Fingerprint(pinned))
	}
	return verify(pinned, r.Sig, r.Bytes())
}

// RegistryPath is <state dir>/registry.json: the box's newest accepted
// registry, which its daemon puts back on the hub.
func RegistryPath(stateDir string) string { return filepath.Join(stateDir, "registry.json") }

// LoadRegistry reads registry.json; ok is false when the box has none, or
// when the one it has isn't signed by pinned (a box re-pinned to a new
// admin starts again from none).
func LoadRegistry(stateDir, pinned string) (r Registry, ok bool, err error) {
	b, err := os.ReadFile(RegistryPath(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return Registry{}, false, nil
	}
	if err != nil {
		return Registry{}, false, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return Registry{}, false, fmt.Errorf("%s: %w", RegistryPath(stateDir), err)
	}
	if r.Verify(pinned) != nil {
		return Registry{}, false, nil
	}
	return r, true, nil
}

// SaveRegistry writes registry.json (mode 0600) through a rename.
func SaveRegistry(stateDir string, r Registry) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(RegistryPath(stateDir), append(b, '\n'))
}

// hubRegistry is the hub's registry entry, as hubRoster is the roster's.
type hubRegistry struct {
	found  bool
	rev    uint64
	reg    Registry
	parsed bool
}

func readHubRegistry(ctx context.Context, kv jetstream.KeyValue) (hubRegistry, error) {
	e, err := kv.Get(ctx, RegistryKey)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return hubRegistry{}, nil
	}
	if err != nil {
		return hubRegistry{}, err
	}
	h := hubRegistry{found: true, rev: e.Revision()}
	h.parsed = json.Unmarshal(e.Value(), &h.reg) == nil
	return h, nil
}

func putRegistry(ctx context.Context, kv jetstream.KeyValue, h hubRegistry, r Registry) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if h.found {
		_, err = kv.Update(ctx, RegistryKey, b, h.rev)
	} else {
		_, err = kv.Create(ctx, RegistryKey, b)
	}
	return err
}

// ApplyRegistry reads a registry's rows with this box's own loader and,
// when it can, writes them as its repos.toml; an error refuses the
// registry and leaves the box's file as it was.
type ApplyRegistry func(Registry) error

// RegistrySync is what SyncRegistry did: the version it took (0 for
// none), or the newer registry it refused and why.
type RegistrySync struct {
	Took      int64
	Refused   int64
	RefuseErr error
}

// SyncRegistry reads the hub's registry. One the pinned key signed with a
// version above the box's own is offered to apply, and on success saved
// as registry.json; one apply refuses is left on the hub (it is valid,
// only newer than this binary) and reported. When the hub's copy is
// missing, unsigned or lower, the box's own is put back (compare-and-set,
// so a concurrent writer wins). err is a hub that can't be read or
// written.
func SyncRegistry(ctx context.Context, kv jetstream.KeyValue, stateDir, pinned string, apply ApplyRegistry) (RegistrySync, error) {
	var out RegistrySync
	local, have, err := LoadRegistry(stateDir, pinned)
	if err != nil {
		return out, err
	}
	h, err := readHubRegistry(ctx, kv)
	if err != nil {
		return out, err
	}
	valid := h.parsed && h.reg.Verify(pinned) == nil
	if valid && h.reg.Version > local.Version {
		if err := apply(h.reg); err != nil {
			out.Refused, out.RefuseErr = h.reg.Version, err
			return out, nil
		}
		if err := SaveRegistry(stateDir, h.reg); err != nil {
			return out, err
		}
		out.Took = h.reg.Version
		return out, nil
	}
	if have && (!h.found || !valid || h.reg.Version < local.Version) {
		if err := putRegistry(ctx, kv, h, local); err != nil && !isConflict(err) {
			return out, fmt.Errorf("re-put registry v%d: %w", local.Version, err)
		}
	}
	return out, nil
}

// PublishRegistry signs rows as the version after the box's own
// registry.json (never the hub's), applies it here, saves it and writes
// it to the hub, compare-and-set on the entry's revision (retried when
// another writer gets there first). The saved copy is what every daemon
// re-puts, so a failed hub write heals.
func PublishRegistry(ctx context.Context, kv jetstream.KeyValue, stateDir string, rows string,
	admin ed25519.PrivateKey, now time.Time, apply ApplyRegistry) (Registry, error) {
	pinned := KeyHex(admin.Public().(ed25519.PublicKey))
	local, _, err := LoadRegistry(stateDir, pinned)
	if err != nil {
		return Registry{}, err
	}
	next := Registry{Version: local.Version + 1, AdminKey: pinned, Rows: rows, IssuedAt: Stamp(now)}
	next.Sign(admin)
	if err := apply(next); err != nil {
		return Registry{}, err
	}
	if err := SaveRegistry(stateDir, next); err != nil {
		return Registry{}, err
	}
	for range 3 {
		var h hubRegistry
		if h, err = readHubRegistry(ctx, kv); err != nil {
			break
		}
		if err = putRegistry(ctx, kv, h, next); err == nil || !isConflict(err) {
			break
		}
	}
	if err != nil {
		return next, fmt.Errorf("registry v%d saved here but not on the hub (the daemon puts it back within a minute): %w", next.Version, err)
	}
	return next, nil
}
