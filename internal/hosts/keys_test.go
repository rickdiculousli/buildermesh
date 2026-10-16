package hosts

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureHostKey(t *testing.T) {
	path := HostKeyPath(filepath.Join(t.TempDir(), "cfg"))
	key, created, err := EnsureHostKey(path)
	if err != nil || !created {
		t.Fatalf("EnsureHostKey = %v, %v", created, err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("host.key mode: %v %v", fi.Mode(), err)
	}
	if di, _ := os.Stat(filepath.Dir(path)); di.Mode().Perm() != 0o700 {
		t.Fatalf("config dir mode %v", di.Mode())
	}
	b, _ := os.ReadFile(path)
	if len(b) != 65 || b[64] != '\n' {
		t.Fatalf("host.key = %q", b)
	}
	loaded, err := LoadHostKey(path)
	if err != nil || !bytes.Equal(loaded, key) {
		t.Fatalf("LoadHostKey = %v", err)
	}
	again, created, err := EnsureHostKey(path)
	if err != nil || created || !bytes.Equal(again, key) {
		t.Fatalf("second EnsureHostKey = %v, %v", created, err)
	}
}

func TestLoadHostKeyMalformed(t *testing.T) {
	dir := t.TempDir()
	for _, s := range []string{"", "zz\n", "ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789\n", "0011\n"} {
		path := filepath.Join(dir, "host.key")
		os.WriteFile(path, []byte(s), 0o600)
		if _, err := LoadHostKey(path); err == nil {
			t.Errorf("LoadHostKey(%q) accepted", s)
		}
		if _, _, err := EnsureHostKey(path); err == nil {
			t.Errorf("EnsureHostKey(%q) accepted", s)
		}
	}
}
