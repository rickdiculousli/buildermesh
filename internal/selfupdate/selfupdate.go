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
	"reflect"
	"regexp"
	"runtime/debug"
	"strconv"
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
	Release  string // the release the commit is (v0.1.0) or follows (v0.1.0+3); "" when none is stamped
}

// stampRevision and stampTime are the commit and its time Install stamps
// into a build with -ldflags -X, since go writes no VCS stamp in a git
// worktree such as the daemon's main checkout; stampRelease is the release
// Describe named for that commit.
var stampRevision, stampTime, stampRelease string

// FromBuildInfo reads the vcs.* settings of bi (nil or !ok: none); a build
// without vcs.revision falls back to Install's stamp, then to the main
// module's pseudo-version (a `go install <module>@<commit>` build: its
// 12-hex suffix is the revision, its timestamp the time), and one with none
// of them is unknown. The stamped release is the version's when the stamp
// names the same commit.
func FromBuildInfo(bi *debug.BuildInfo, ok bool) Version {
	var v Version
	if ok && bi != nil {
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
	}
	if v.Revision == "" {
		v = Version{Revision: stampRevision, Time: stampTime}
	}
	if v.Revision == "" && ok && bi != nil {
		v = fromPseudo(bi.Main.Version)
	}
	if Same(v.Revision, stampRevision) {
		v.Release = stampRelease
	}
	return v
}

// pseudo matches a module pseudo-version (vX.0.0-<time>-<rev>,
// vX.Y.Z-pre.0.<time>-<rev> or vX.Y.Z-0.<time>-<rev>, optionally
// +incompatible): the 14-digit UTC time and the 12-hex revision.
var pseudo = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+-(?:[0-9A-Za-z.-]*\.)?([0-9]{14})-([0-9a-f]{12})(?:\+incompatible)?$`)

// fromPseudo is the revision and time a pseudo-version names; a tagged
// version, (devel) or "" names none.
func fromPseudo(version string) Version {
	m := pseudo.FindStringSubmatch(version)
	if m == nil {
		return Version{}
	}
	t, err := time.Parse("20060102150405", m[1])
	if err != nil {
		return Version{}
	}
	return Version{Revision: m[2], Time: t.UTC().Format(time.RFC3339)}
}

// Same reports whether two revisions name one commit: both known, one a
// prefix of the other (a pseudo-version's 12 hex against a full commit).
func Same(a, b string) bool {
	if a == "" || b == "" || a == Unknown || b == Unknown {
		return false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	return strings.HasPrefix(b, a)
}

// String is `<rev8> <time> [modified]`, or `unknown`; with a release it
// is `<release> (<rev8> <time> [modified])`.
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
	if v.Release != "" {
		s = v.Release + " (" + s + ")"
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

// Refused is Install's error when the build does not report the commit it
// was built from; exe is left as it was.
type Refused struct{ Reported, Head string }

func (e *Refused) Error() string {
	return fmt.Sprintf("built binary reports %s, not %s; keeping this binary", e.Reported, Short(e.Head))
}

// describeRE reads `git describe --long`: the release tag, the commits
// past it and the abbreviated commit.
var describeRE = regexp.MustCompile(`^(v[0-9]+\.[0-9]+\.[0-9]+)-([0-9]+)-g[0-9a-f]+$`)

// Describe is the release head is, `vX.Y.Z`, or follows, `vX.Y.Z+N` with N
// the commits past it, from the nearest vX.Y.Z tag in head's history (a
// pre-release such as v0.2.0-rc1 is passed over); "" when there is none.
func Describe(dir, head string) string {
	out, err := git(dir, "describe", "--tags", "--long", "--match", "v[0-9]*.[0-9]*.[0-9]*",
		"--exclude", "*-*", "--exclude", "*+*", "--end-of-options", head)
	m := describeRE.FindStringSubmatch(out)
	if err != nil || m == nil {
		return ""
	}
	if m[2] == "0" {
		return m[1]
	}
	return m[1] + "+" + m[2]
}

// stampFlags is the -ldflags value that stamps head, its commit time (UTC,
// as go writes vcs.time) and its release (Describe) into the build.
func stampFlags(dir, head string) (string, error) {
	ct, err := git(dir, "show", "-s", "--format=%ct", "--end-of-options", head)
	if err != nil {
		return "", err
	}
	sec, err := strconv.ParseInt(ct, 10, 64)
	if err != nil {
		return "", fmt.Errorf("commit time of %s: %q", Short(head), ct)
	}
	pkg := reflect.TypeOf(Version{}).PkgPath()
	flags := fmt.Sprintf("-X %s.stampRevision=%s -X %s.stampTime=%s",
		pkg, head, pkg, time.Unix(sec, 0).UTC().Format(time.RFC3339))
	if rel := Describe(dir, head); rel != "" {
		flags += fmt.Sprintf(" -X %s.stampRelease=%s", pkg, rel)
	}
	return flags, nil
}

// versionTimeout bounds the built binary's `version`.
const versionTimeout = 30 * time.Second

// reports is the revision bin's `version` prints: `<rev8>`, `unknown`, or
// its output (or error) when that is not `bmesh <rev8> ...` or `bmesh
// <release> (<rev8> ...)`.
func reports(ctx context.Context, bin string) string {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "version")
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	f := strings.Fields(string(out))
	switch {
	case err != nil:
		return fmt.Sprintf("%q (%v)", tail(string(out)), err)
	case len(f) < 2 || f[0] != "bmesh":
		return strconv.Quote(tail(string(out)))
	case len(f) > 2 && strings.HasPrefix(f[2], "("):
		return strings.TrimRight(strings.TrimPrefix(f[2], "("), ")")
	}
	return f[1]
}

// Install builds ./cmd/bmesh in dir, which is at head, to a temporary file
// beside exe, stamped with head and its commit time; it refuses the build
// (Refused) unless its `version` names head, then moves exe to Prev(exe)
// and the build to exe. A failed or refused build leaves exe as it was; so
// does a failed swap, as far as the file system allows.
func Install(ctx context.Context, dir, exe, head string) error {
	ctx, cancel := context.WithTimeout(ctx, BuildTimeout)
	defer cancel()
	ldflags, err := stampFlags(dir, head)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(exe), ".bmesh-update-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	f.Close()
	os.Remove(tmp) // go build writes it whole
	defer os.Remove(tmp)
	argv := append(goCommand(), "build", "-ldflags", ldflags, "-o", tmp, "./cmd/bmesh")
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
	return swap(ctx, tmp, exe, head)
}

// swap refuses built (Refused) unless its `version` names head, then moves
// exe to Prev(exe) and built to exe; a failed swap leaves exe as it was, as
// far as the file system allows. A Prev(exe) that cannot be removed is
// moved to Prev(exe)+".old" instead: on Windows, where a running binary can
// be renamed but not removed, it is still the image of `serve --restart`,
// which the update before this one moved there.
func swap(ctx context.Context, built, exe, head string) error {
	if got := reports(ctx, built); got != Short(head) {
		return &Refused{Reported: got, Head: head}
	}
	prev := Prev(exe)
	if err := os.Remove(prev); err != nil && !errors.Is(err, os.ErrNotExist) {
		if rerr := os.Rename(prev, prev+".old"); rerr != nil {
			return fmt.Errorf("%w; and moving it aside: %v", err, rerr)
		}
	}
	if err := os.Rename(exe, prev); err != nil {
		return err
	}
	if err := os.Rename(built, exe); err != nil {
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
