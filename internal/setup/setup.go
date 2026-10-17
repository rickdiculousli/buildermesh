// Package setup runs a repo's setup command (its .bmesh/config.toml
// [setup]) in a worktree: what installs the dependencies the repo
// gitignores, which a fresh `git worktree add` lacks. One rule says when a
// worktree's install is current, wherever it runs: a successful run writes
// MarkerName into the worktree's own git dir (outside the tree), holding a
// sha256 of the command and of every tracked file the inputs match, and the
// install is current while that hash matches.
package setup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/proc"
	"github.com/rickdiculousli/buildermesh/internal/shell"
)

// MarkerName is the file a successful run writes in the worktree's git dir.
const MarkerName = "bmesh-setup"

// Error is a setup that failed: it exited non-zero, timed out or left the
// tree dirty. Output is what it printed.
type Error struct {
	Msg    string
	Output string
}

func (e *Error) Error() string { return e.Msg }

// Hash is the sha256 of s's command and of every tracked file of dir that
// s's inputs match (path and content, in path order; a tracked file missing
// from the tree counts as missing), hex-encoded.
func Hash(ctx context.Context, dir string, s kind.Setup) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "command %s\x00", s.Command)
	if len(s.Inputs) > 0 {
		out, err := git(ctx, dir, append([]string{"ls-files", "-z", "--"}, s.Inputs...)...)
		if err != nil {
			return "", err
		}
		paths := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
		sort.Strings(paths)
		for _, p := range paths {
			if p == "" {
				continue
			}
			sum := "missing"
			if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p))); err == nil {
				d := sha256.Sum256(b)
				sum = hex.EncodeToString(d[:])
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
			fmt.Fprintf(h, "file %s %s\x00", p, sum)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Marker is the path of dir's marker file: MarkerName in the worktree's
// own git dir (`git rev-parse --git-dir`), never in the tree.
func Marker(ctx context.Context, dir string) (string, error) {
	gd, err := git(ctx, dir, "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return "", err
	}
	return filepath.Join(strings.TrimSpace(gd), MarkerName), nil
}

// Current reports whether dir's install is current: its marker holds the
// hash s has now.
func Current(ctx context.Context, dir string, s kind.Setup) (bool, error) {
	m, err := Marker(ctx, dir)
	if err != nil {
		return false, err
	}
	b, err := os.ReadFile(m)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	want, err := Hash(ctx, dir, s)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(b)) == want, nil
}

// Options says how Ensure runs. Always runs even when the install is
// current; Clean fails a run that leaves tracked changes or untracked,
// unignored files (the tree a result is recorded for must be the
// commit's); Out, when set, gets the output as it is printed.
type Options struct {
	Always bool
	Clean  bool
	Out    io.Writer
}

// Ensure runs s in dir unless its install is current (or o.Always): it
// drops the marker, runs the command through the platform shell in its own
// process group, killed at s.Timeout, and writes the marker after a
// successful run. ran says whether it ran; a failed run is an *Error with
// its output, and leaves no marker.
func Ensure(ctx context.Context, dir string, s kind.Setup, o Options) (ran bool, output string, err error) {
	if s.Command == "" {
		return false, "", nil
	}
	m, err := Marker(ctx, dir)
	if err != nil {
		return false, "", err
	}
	if !o.Always {
		if cur, err := Current(ctx, dir, s); err != nil || cur {
			return false, "", err
		}
	}
	if err := os.Remove(m); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, "", err
	}
	out, err := run(ctx, dir, s, o.Out)
	if err != nil {
		return true, out, &Error{Msg: "setup failed: " + err.Error(), Output: out}
	}
	if o.Clean {
		st, err := git(ctx, dir, "status", "--porcelain", "-z", "--untracked-files=all")
		if err != nil {
			return true, out, err
		}
		if paths := porcelainPaths(st); len(paths) > 0 {
			return true, out, &Error{Msg: "setup dirtied the tree: " + strings.Join(paths, ", "), Output: out}
		}
	}
	// The hash is read after the run: a run that rewrote a lockfile
	// installed what the new one says.
	hash, err := Hash(ctx, dir, s)
	if err != nil {
		return true, out, err
	}
	if err := os.WriteFile(m, []byte(hash+"\n"), 0o644); err != nil {
		return true, out, err
	}
	return true, out, nil
}

// run runs s.Command in dir, the output captured (and copied to w).
func run(ctx context.Context, dir string, s kind.Setup, w io.Writer) (string, error) {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = kind.DefaultSetupTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var buf bytes.Buffer
	var out io.Writer = &buf
	if w != nil {
		out = io.MultiWriter(&buf, w)
	}
	cmd := shell.Command(runCtx, s.Command)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, out, out
	cmd.Env = append(os.Environ(), "BMESH_SETUP=1")
	proc.Group(cmd)
	timedOut := false
	cmd.Cancel = func() error {
		timedOut = ctx.Err() == nil
		return proc.KillGroup(cmd)
	}
	cmd.WaitDelay = time.Second // a leftover child may hold the output open
	err := cmd.Run()
	proc.KillGroup(cmd) // nothing of the run outlives it
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState.Success() {
		err = nil
	}
	switch {
	case timedOut:
		return buf.String(), fmt.Errorf("timed out after %s", timeout)
	case ctx.Err() != nil:
		return buf.String(), ctx.Err()
	}
	return buf.String(), err
}

// porcelainPaths are the paths of `git status --porcelain -z` output (a
// rename's second path, its source, is skipped).
func porcelainPaths(st string) []string {
	var paths []string
	fields := strings.Split(st, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		paths = append(paths, f[3:])
		if f[0] == 'R' || f[0] == 'C' {
			i++
		}
	}
	return paths
}

// Tail is the last n lines of s, without a trailing newline.
func Tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return strings.Join(lines[max(len(lines)-n, 0):], "\n")
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}
