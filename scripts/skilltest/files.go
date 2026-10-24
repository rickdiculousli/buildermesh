package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// git runs git in dir with env added to this process's, and returns its
// trimmed stdout; a failure carries git's stderr.
func git(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// copyTree copies src's directories, regular files and symlinks into dst.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		to := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(to, info.Mode().Perm()|0o700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, to)
		case info.Mode().IsRegular():
			return copyFile(p, to, info.Mode().Perm())
		}
		return nil
	})
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// move renames src to dst, copying and removing it when they are on
// different filesystems.
func move(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyTree(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

// archive writes the named entries of dir (directories or files) into one
// gzipped tar at file, then removes them. A kept cell holds its sandbox this
// way: a tree of Go files left in the repo's cache/ would be walked by the
// repo's own `gofmt -l .`.
func archive(dir string, names []string, file string) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(f)
	tw := tar.NewWriter(zw)
	for _, name := range names {
		err := filepath.WalkDir(filepath.Join(dir, name), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			link := ""
			if info.Mode()&os.ModeSymlink != 0 {
				if link, err = os.Readlink(p); err != nil {
					return err
				}
			}
			h, err := tar.FileInfoHeader(info, link)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			h.Name = filepath.ToSlash(rel)
			if d.IsDir() {
				h.Name += "/"
			}
			if err := tw.WriteHeader(h); err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer in.Close()
			_, err = io.Copy(tw, in)
			return err
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			f.Close()
			return err
		}
	}
	if err := tw.Close(); err != nil {
		f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	for _, name := range names {
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// thisBox is this box's name as the skills compare it with a row's host:
// `hostname -s` in lower case.
func thisBox() string {
	h, err := os.Hostname()
	if err != nil {
		return "localhost"
	}
	h, _, _ = strings.Cut(h, ".")
	return strings.ToLower(h)
}

func writeFile(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// layOver writes the files under root in fsys over dst, each at its path
// below root.
func layOver(fsys fs.FS, root, dst string) error {
	return fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(dst, filepath.FromSlash(strings.TrimPrefix(p, root+"/"))), string(b))
	})
}
