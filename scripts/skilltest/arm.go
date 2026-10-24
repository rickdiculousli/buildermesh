package main

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Arm is one version of the plugin under test: the directory claude loads
// with --plugin-dir, and the top-level bmesh commands that version's binary
// has, read from its manual.
type Arm struct {
	Name     string   `json:"name"`
	Source   string   `json:"source"`           // what --arm named: a git ref or a directory
	Commit   string   `json:"commit,omitempty"` // the ref's commit
	Dir      string   `json:"dir"`
	Commands []string `json:"commands"`
}

var armNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// pluginPaths are what an arm from a ref takes from the tree: the plugin
// and the manual its command set is read from. The first two and the manual
// are required.
var pluginPaths = []string{".claude-plugin", "skills", "docs/manual.md", "hooks", "third_party"}

func parseArm(v string) (name, source string, err error) {
	name, source, ok := strings.Cut(v, "=")
	if !ok || !armNameRE.MatchString(name) || source == "" {
		return "", "", fmt.Errorf("--arm %q: want name=<git ref or dir>, the name letters, digits, - or _", v)
	}
	return name, source, nil
}

// prepareArm makes the arm source names ready: a directory is used in
// place; anything else is a git ref of the repo at root, whose plugin files
// are extracted under outDir/arms/<name>/plugin.
func prepareArm(name, source, root, outDir string) (Arm, error) {
	a := Arm{Name: name, Source: source}
	if st, err := os.Stat(source); err == nil && st.IsDir() {
		dir, err := filepath.Abs(source)
		if err != nil {
			return a, err
		}
		a.Dir = dir
	} else {
		if root == "" {
			return a, fmt.Errorf("arm %s: %q is no directory, and there is no git repo here to read it as a ref from", name, source)
		}
		commit, err := git(root, nil, "rev-parse", "--verify", "-q", source+"^{commit}")
		if err != nil {
			return a, fmt.Errorf("arm %s: %q is neither a directory nor a commit in %s", name, source, root)
		}
		a.Commit = commit
		a.Dir = filepath.Join(outDir, "arms", name, "plugin")
		if err := extract(root, commit, a.Dir); err != nil {
			return a, fmt.Errorf("arm %s: %w", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(a.Dir, ".claude-plugin", "plugin.json")); err != nil {
		return a, fmt.Errorf("arm %s: %s has no .claude-plugin/plugin.json", name, a.Dir)
	}
	manual, err := os.ReadFile(filepath.Join(a.Dir, "docs", "manual.md"))
	if err != nil {
		return a, fmt.Errorf("arm %s: its commands are read from docs/manual.md: %w", name, err)
	}
	a.Commands = commandsFromManual(string(manual))
	return a, nil
}

// extract writes the plugin paths of commit's tree into dir.
func extract(root, commit, dir string) error {
	var paths []string
	for i, p := range pluginPaths {
		if _, err := git(root, nil, "cat-file", "-e", commit+":"+p); err != nil {
			if i < 3 {
				return fmt.Errorf("%s has no %s", commit[:8], p)
			}
			continue
		}
		paths = append(paths, p)
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	cmd := exec.Command("git", append([]string{"-C", root, "archive", "--format=tar", commit, "--"}, paths...)...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	untarErr := untar(pipe, dir)
	if untarErr != nil {
		_, _ = io.Copy(io.Discard, pipe)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("git archive %s: %v: %s", commit[:8], err, strings.TrimSpace(stderr.String()))
	}
	return untarErr
}

// untar writes a tar stream's directories, files and symlinks under dir,
// refusing a name that would land outside it.
func untar(r io.Reader, dir string) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(filepath.FromSlash(h.Name))
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			if h.Typeflag == tar.TypeXGlobalHeader || name == "." {
				continue
			}
			return fmt.Errorf("tar entry %q is outside the tree", h.Name)
		}
		p := filepath.Join(dir, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o777|0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(h.Linkname, p); err != nil {
				return err
			}
		}
	}
}

var (
	manualHeadingRE = regexp.MustCompile("^### `bmesh ([a-z][a-z0-9-]*)[^`]*`")
	manualNameRE    = regexp.MustCompile("`([a-z][a-z0-9-]*)`")
)

// commandsFromManual is the set of top-level commands a manual documents,
// from its level-3 headings that name one (the heading of bmesh hold,
// unhold and held names three), plus help, version and hosts, sorted.
func commandsFromManual(manual string) []string {
	set := map[string]bool{"help": true, "version": true, "hosts": true}
	for _, line := range strings.Split(manual, "\n") {
		m := manualHeadingRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		set[m[1]] = true
		for _, n := range manualNameRE.FindAllStringSubmatch(line[len(m[0]):], -1) {
			set[n[1]] = true
		}
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
