// Package plugin writes the buildermesh plugin the binary carries to a dir
// of bmesh's own, and points Claude Code at it, so the skills a box's
// sessions load are always its binary's.
package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	buildermesh "github.com/rickdiculousli/buildermesh"
)

// Name is the plugin's name and its marketplace's; ID is its install.
const (
	Name = "buildermesh"
	ID   = Name + "@" + Name
)

// stampFile, at the dir's root, holds the stamp of the binary that wrote it.
const stampFile = ".bmesh-stamp"

// Dir is the plugin dir under the state dir.
func Dir(stateDir string) string { return filepath.Join(stateDir, "plugin") }

// Stamp is what a dir written from files by the binary labelled label
// carries: a digest of the files, then the label (`bmesh version`'s).
func Stamp(files fs.FS, label string) (string, error) {
	var paths []string
	err := fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			paths = append(paths, p)
		}
		return err
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		b, err := fs.ReadFile(files, p)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", p, len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:12] + " " + label, nil
}

// Written is the stamp of dir and the label in it; ok is false when dir
// has none (never written, or not a plugin dir).
func Written(dir string) (stamp, label string, ok bool) {
	b, err := os.ReadFile(filepath.Join(dir, stampFile))
	if err != nil {
		return "", "", false
	}
	stamp = strings.TrimSpace(string(b))
	_, label, _ = strings.Cut(stamp, " ")
	return stamp, label, stamp != ""
}

// Ensure writes the embedded plugin to dir unless dir already carries its
// stamp for label; wrote says whether it wrote.
func Ensure(dir, label string) (wrote bool, err error) {
	return Write(dir, buildermesh.Plugin, label)
}

// Write writes files to dir whole, unless dir's stamp is already theirs
// for label: into a new dir beside it, which then replaces it, so a
// session reads the old dir or the new one, never half of one. A write
// that fails leaves the old dir as it was.
func Write(dir string, files fs.FS, label string) (wrote bool, err error) {
	stamp, err := Stamp(files, label)
	if err != nil {
		return false, err
	}
	if cur, _, ok := Written(dir); ok && cur == stamp {
		return false, nil
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return false, err
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dir)+"-new-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(tmp)
	err = fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		to := filepath.Join(tmp, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(to, 0o755)
		}
		b, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}
		return os.WriteFile(to, b, 0o644)
	})
	if err != nil {
		return false, fmt.Errorf("write plugin: %w", err)
	}
	// Last, so a dir with this stamp has every file.
	if err := os.WriteFile(filepath.Join(tmp, stampFile), []byte(stamp+"\n"), 0o644); err != nil {
		return false, err
	}
	if err := swap(tmp, dir); err != nil {
		return false, fmt.Errorf("write plugin: %w", err)
	}
	return true, nil
}

// swap moves tmp to dir. A dir can't be renamed over a non-empty one, so
// the old dir moves aside first and is put back when the second rename
// fails.
func swap(tmp, dir string) error {
	old := ""
	if _, err := os.Lstat(dir); err == nil {
		old = filepath.Join(filepath.Dir(dir), fmt.Sprintf(".%s-old-%d", filepath.Base(dir), time.Now().UnixNano()))
		if err := os.Rename(dir, old); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		if old != "" {
			_ = os.Rename(old, dir)
		}
		return err
	}
	if old != "" {
		_ = os.RemoveAll(old)
	}
	return nil
}
