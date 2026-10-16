package hosts

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// HostKeyPath is <config dir>/host.key.
func HostKeyPath(configDir string) string { return filepath.Join(configDir, "host.key") }

// LoadHostKey reads host.key: the 32-byte Ed25519 seed as lowercase hex and
// a newline.
func LoadHostKey(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := parseSeed(strings.TrimSuffix(string(b), "\n"))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// EnsureHostKey loads host.key, creating it (mode 0600, parent dir 0700 if
// missing) when absent; created reports a new key.
func EnsureHostKey(path string) (key ed25519.PrivateKey, created bool, err error) {
	key, err = LoadHostKey(path)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return key, false, err
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, false, err
	}
	if err := writeNew(path, []byte(hex.EncodeToString(seed)+"\n")); err != nil {
		if errors.Is(err, fs.ErrExist) { // another process made it first
			key, err = LoadHostKey(path)
			return key, false, err
		}
		return nil, false, err
	}
	return ed25519.NewKeyFromSeed(seed), true, nil
}

// parseSeed reads a 32-byte seed in lowercase hex.
func parseSeed(s string) ([]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != ed25519.SeedSize || hex.EncodeToString(b) != s {
		return nil, fmt.Errorf("want a %d-byte key seed in lowercase hex", ed25519.SeedSize)
	}
	return b, nil
}

// writeNew creates path with mode 0600 (its parent 0700 if missing) and
// writes b; it fails with fs.ErrExist when path is already there.
func writeNew(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}
