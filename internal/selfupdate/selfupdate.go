// Package selfupdate builds bmesh from a checkout of its repo's main and
// swaps the result in for a binary, keeping the one it replaces beside it.
// The daemon's [update] and `bmesh update` share it.
package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/checkout"
)

// Unknown stands for the revision of a binary built without VCS stamping.
const Unknown = "unknown"

// Version is what a binary's build info says of the commit it was built from.
type Version struct {
	Revision string // the full commit; "" when built without VCS
	Time     string // the commit's time (RFC 3339)
	Modified bool   // built from a tree with uncommitted changes
}

// FromBuildInfo reads the vcs.* settings of bi (nil or !ok: none).
func FromBuildInfo(bi *debug.BuildInfo, ok bool) Version {
	var v Version
	if !ok || bi == nil {
		return v
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			v.Revision = s.Value
		case "vcs.time":
			v.Time = s.Value
		case "vcs.modified":
			v.Modified = s.Value == "true"
		}
	}
	return v
}

// String is `<rev8> <time> [modified]`, or `unknown`.
func (v Version) String() string {
	if v.Revision == "" {
		return Unknown
	}
	s := Short(v.Revision)
	if v.Time != "" {
		s += " " + v.Time
	}
	if v.Modified {
		s += " modified"
	}
	return s
}

// Short is a revision's first eight characters, or `unknown` for "".
func Short(rev string) string {
	if rev == "" || rev == Unknown {
		return Unknown
	}
	if len(rev) > 8 {
		return rev[:8]
	}
	return rev
}

const (
	gitTimeout = time.Minute
	// BuildTimeout bounds one build of ./cmd/bmesh.
	BuildTimeout = 10 * time.Minute
)

func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, s)
	}
	return s, nil
}

// Head is the commit dir is at.
func Head(dir string) (string, error) { return git(dir, "rev-parse", "--verify", "HEAD^{commit}") }

// Contains reports whether head has rev in its history (rev itself
// included). A rev the checkout does not know is not contained.
func Contains(dir, rev, head string) bool {
	if rev == "" || rev == Unknown {
		return false
	}
	_, err := git(dir, "merge-base", "--is-ancestor", "--end-of-options", rev, head)
	return err == nil
}

// MainCheckout leaves a bmesh-owned checkout of repoPath's main at its
// newest and returns it, never touching repoPath's own working tree: with
// checkouts (the [runner] checkouts root) it is the daemon's own
// <checkouts>/<repo dir>/main, moved to origin/main after a fetch (main
// without an origin); else a clone at clones/<name> of repoPath's origin
// (of repoPath when it has none), moved to origin/main after a fetch. The
// checkout is returned under Lock, which the caller releases once it has
// built from it.
func MainCheckout(repoPath, checkouts, clones, name string) (dir string, unlock func(), err error) {
	if checkouts != "" {
		if err := checkout.Fetch(repoPath); err != nil {
			return "", nil, err
		}
		if dir, err = checkout.Ensure(repoPath, checkouts, "main"); err != nil {
			return "", nil, err
		}
		ref := "main"
		if _, err := git(repoPath, "remote", "get-url", "origin"); err == nil {
			ref = "origin/main"
		}
		return locked(dir, func() error { return checkout.MoveTo(dir, ref) })
	}
	dir = filepath.Join(clones, name)
	if err := os.MkdirAll(clones, 0o755); err != nil {
		return "", nil, err
	}
	return locked(dir, func() error { return cloneMain(repoPath, clones, dir) })
}

// locked runs move under dir's Lock and returns dir still locked, or
// unlocks when move fails.
func locked(dir string, move func() error) (string, func(), error) {
	unlock, err := Lock(dir)
	if err != nil {
		return "", nil, err
	}
	if err := move(); err != nil {
		unlock()
		return "", nil, err
	}
	return dir, unlock, nil
}

// cloneMain leaves the clone dir at origin/main, cloning it first from
// repoPath's origin (repoPath itself without one) when it is missing.
func cloneMain(repoPath, clones, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		src, err := git(repoPath, "remote", "get-url", "origin")
		if err != nil {
			src = repoPath
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if _, err := git(clones, "clone", "--quiet", "--no-checkout", "--end-of-options", src, dir); err != nil {
			return err
		}
	}
	if _, err := git(dir, "fetch", "--quiet", "origin", "+refs/heads/main:refs/remotes/origin/main"); err != nil {
		return err
	}
	return checkout.MoveTo(dir, "origin/main")
}

// Prev is where Install keeps the binary it replaces: bmesh.prev beside it.
func Prev(exe string) string {
	return filepath.Join(filepath.Dir(exe), strings.TrimSuffix(filepath.Base(exe), ".exe")+".prev")
}

// goCommand is $BMESH_GO_COMMAND split on spaces (a test hook), else go.
func goCommand() []string {
	if f := strings.Fields(os.Getenv("BMESH_GO_COMMAND")); len(f) > 0 {
		return f
	}
	return []string{"go"}
}

// Scoped reports whether a build runs in a transient systemd scope of its
// own: when this process runs in a systemd unit ($INVOCATION_ID), whose
// memory limit the build would otherwise share, and systemd-run is on PATH.
func Scoped() bool {
	if os.Getenv("INVOCATION_ID") == "" {
		return false
	}
	_, err := exec.LookPath("systemd-run")
	return err == nil
}

// Install builds ./cmd/bmesh in dir to a temporary file beside exe, then
// moves exe to Prev(exe) and the build to exe. A failed build leaves exe as
// it was; so does a failed swap, as far as the file system allows.
func Install(ctx context.Context, dir, exe string) error {
	ctx, cancel := context.WithTimeout(ctx, BuildTimeout)
	defer cancel()
	f, err := os.CreateTemp(filepath.Dir(exe), ".bmesh-update-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	f.Close()
	os.Remove(tmp) // go build writes it whole
	defer os.Remove(tmp)
	argv := append(goCommand(), "build", "-o", tmp, "./cmd/bmesh")
	if Scoped() {
		argv = append([]string{"systemd-run", "--user", "--scope", "--quiet", "--collect", "--"}, argv...)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.WaitDelay = 5 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", strings.Join(argv, " "), err, tail(string(out)))
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	prev := Prev(exe)
	if err := os.Remove(prev); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(exe, prev); err != nil {
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		if rerr := os.Rename(prev, exe); rerr != nil {
			return fmt.Errorf("%w; and restoring %s: %v", err, prev, rerr)
		}
		return err
	}
	return nil
}

// tail is the last few lines of a build's output, for a log line.
func tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}
	return strings.Join(lines, "\n")
}
