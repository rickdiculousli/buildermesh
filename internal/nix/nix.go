// Package nix removes a repo's checkout from a box (023): the admin's
// signed nix job names a repo and the origin its registry row has, the
// box takes the path from its own registry row and refuses, touching
// nothing, anything whose loss couldn't be undone from a remote.
package nix

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

// Request is one nix on this box.
type Request struct {
	Repo      string // the registry name
	Path      string // the row's path, expanded and absolute (this box's registry)
	RowPath   string // the row's path as written
	RowRemote string // this box's row's remote ("" = none)
	Origin    string // the origin the job carries, from the admin's registry
	Checkouts string // [runner] checkouts, expanded ("" = routines off)
	IgnoredOK bool   // the job says ignored-ok: ignored files go too
}

// Outcome is what a nix did: Removed or Absent (both count as done for
// `repos remove`), or a refusal or failure, with its text.
type Outcome struct {
	Result string
	Text   string
}

const (
	Removed = "removed"
	Absent  = "absent"
	Refused = "refused"
	Failed  = "failed"
)

// OK: removed or absent.
func (o Outcome) OK() bool { return o.Result == Removed || o.Result == Absent }

func refused(format string, args ...any) Outcome {
	return Outcome{Result: Refused, Text: "refused: " + fmt.Sprintf(format, args...)}
}

// Nix checks the checkout and, when every check passes, removes it, its
// linked worktrees and <checkouts>/<name>; nothing is touched on a
// refusal. A checkout already gone is absent (its routine checkouts are
// still removed).
func Nix(ctx context.Context, req Request) Outcome {
	routines := ""
	if req.Checkouts != "" {
		routines = filepath.Join(req.Checkouts, filepath.Base(req.Path))
	}
	if _, err := os.Lstat(req.Path); errors.Is(err, fs.ErrNotExist) {
		text := "absent: no checkout at " + req.Path
		if removed, err := removeDir(routines); err != nil {
			return Outcome{Result: Failed, Text: "failed: " + err.Error()}
		} else if removed {
			text += "; removed " + routines
		}
		return Outcome{Result: Absent, Text: text}
	}
	dir, links, o := check(ctx, req, routines)
	if o.Result != "" {
		return o
	}
	var gone []string
	for _, wt := range links {
		if _, err := git(ctx, dir, "worktree", "remove", wt); err != nil {
			return Outcome{Result: Failed, Text: fmt.Sprintf("failed: %v (removed: %s)", err, listOrNone(gone))}
		}
		gone = append(gone, wt)
	}
	if _, err := removeDir(routines); err != nil {
		return Outcome{Result: Failed, Text: fmt.Sprintf("failed: %v (removed: %s)", err, listOrNone(gone))}
	}
	if err := os.RemoveAll(dir); err != nil {
		return Outcome{Result: Failed, Text: fmt.Sprintf("failed: %v (removed: %s)", err, listOrNone(gone))}
	}
	if dir != req.Path {
		_ = os.Remove(req.Path) // the symlink that led to it
	}
	text := "removed " + req.Path
	if routines != "" {
		text += " and " + routines
	}
	if len(gone) > 0 {
		text += fmt.Sprintf(", with %d linked %s", len(gone), plural(len(gone), "worktree"))
	}
	return Outcome{Result: Removed, Text: text}
}

// check runs every refusal in order and returns the checkout's resolved
// directory and the linked worktrees that go with it (routine checkouts,
// which go with <checkouts>/<name>, aside).
func check(ctx context.Context, req Request, routines string) (dir string, links []string, o Outcome) {
	dir, err := filepath.EvalSymlinks(req.Path)
	if err != nil {
		return "", nil, Outcome{Result: Failed, Text: "failed: " + err.Error()}
	}
	home, _ := os.UserHomeDir()
	want := filepath.Base(filepath.Clean(req.RowPath))
	if filepath.Base(dir) != want || dir == "/" || dir == filepath.Clean(home) {
		return "", nil, refused("%s resolves to %s, whose name isn't the row's %s", req.Path, dir, want)
	}
	top, err := git(ctx, dir, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-dir", "--git-common-dir")
	f := strings.Split(top, "\n")
	if err != nil || len(f) != 3 || resolved(f[0]) != dir || resolved(f[1]) != resolved(f[2]) {
		return "", nil, refused("%s is not a checkout's top level", req.Path)
	}
	url, err := git(ctx, dir, "remote", "get-url", "origin")
	if err != nil || url == "" {
		return "", nil, refused("%s has no origin", req.Path)
	}
	if !config.SameRemote(url, req.Origin) {
		return "", nil, refused("%s's origin is %s, not the registry's %s", req.Path, url, orNone(req.Origin))
	}
	if req.RowRemote != "" && !config.SameRemote(req.RowRemote, req.Origin) {
		return "", nil, refused("this box's registry row says remote %s, not %s", req.RowRemote, req.Origin)
	}
	if out, err := git(ctx, dir, "status", "--porcelain"); err != nil || out != "" {
		return "", nil, refused("%s has uncommitted or untracked changes", req.Path)
	}
	wts, err := worktrees(ctx, dir)
	if err != nil {
		return "", nil, Outcome{Result: Failed, Text: "failed: " + err.Error()}
	}
	for _, wt := range wts[1:] {
		if routines != "" && within(wt.path, routines) {
			continue
		}
		if _, err := os.Stat(wt.path); errors.Is(err, fs.ErrNotExist) {
			continue // gone from disk: nothing in it to lose
		}
		if out, err := git(ctx, wt.path, "status", "--porcelain"); err != nil || out != "" {
			return "", nil, refused("worktree %s has uncommitted or untracked changes", wt.path)
		}
		if !merged(ctx, dir, wt.head) {
			what := "detached at " + short(wt.head)
			if wt.branch != "" {
				what = "on " + wt.branch
			}
			return "", nil, refused("worktree %s is %s, not merged into main", wt.path, what)
		}
		if !req.IgnoredOK {
			if ig := ignored(ctx, wt.path); ig != "" {
				return "", nil, refused("worktree %s has ignored files (%s): nix with ignored-ok to remove them too", wt.path, ig)
			}
		}
		links = append(links, wt.path)
	}
	if out, err := git(ctx, dir, "log", "--branches", "--tags", "--not", "--remotes", "--format=%h %s", "-n", "1"); err != nil || out != "" {
		return "", nil, refused("%s has commits on no remote (%s)", req.Path, cutText(out, 80))
	}
	if _, err := git(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/stash"); err == nil {
		return "", nil, refused("%s has a stash", req.Path)
	}
	if !req.IgnoredOK {
		if ig := ignored(ctx, dir); ig != "" {
			return "", nil, refused("%s has ignored files (%s): nix with ignored-ok to remove them too", req.Path, ig)
		}
	}
	return dir, links, Outcome{}
}

type worktree struct{ path, head, branch string }

// worktrees is `git worktree list --porcelain`, the main checkout first.
func worktrees(ctx context.Context, dir string) ([]worktree, error) {
	out, err := git(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var wts []worktree
	for _, block := range strings.Split(out, "\n\n") {
		var w worktree
		for _, l := range strings.Split(block, "\n") {
			k, v, _ := strings.Cut(l, " ")
			switch k {
			case "worktree":
				w.path = v
			case "HEAD":
				w.head = v
			case "branch":
				w.branch = strings.TrimPrefix(v, "refs/heads/")
			}
		}
		if w.path != "" {
			wts = append(wts, w)
		}
	}
	if len(wts) == 0 {
		return nil, errors.New("git worktree list: no main checkout")
	}
	return wts, nil
}

// merged: head is in main or origin/main.
func merged(ctx context.Context, dir, head string) bool {
	if head == "" {
		return false
	}
	for _, ref := range []string{"refs/heads/main", "refs/remotes/origin/main"} {
		if _, err := git(ctx, dir, "merge-base", "--is-ancestor", head, ref); err == nil {
			return true
		}
	}
	return false
}

// ignored names the first ignored paths in dir, "" when there are none.
func ignored(ctx context.Context, dir string) string {
	out, err := git(ctx, dir, "status", "--porcelain", "--ignored")
	if err != nil {
		return "unreadable"
	}
	var paths []string
	for _, l := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(l, "!! "); ok {
			paths = append(paths, p)
		}
	}
	if len(paths) > 3 {
		paths = append(paths[:3], fmt.Sprintf("and %d more", len(paths)-3))
	}
	return strings.Join(paths, ", ")
}

// removeDir removes dir when it exists, reporting whether it did.
func removeDir(dir string) (bool, error) {
	if dir == "" {
		return false, nil
	}
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return true, os.RemoveAll(dir)
}

func within(p, dir string) bool {
	rel, err := filepath.Rel(resolved(dir), resolved(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolved(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

const gitTimeout = 2 * time.Minute

func git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		return s, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, cutText(s, 300))
	}
	return s, nil
}

func cutText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func listOrNone(l []string) string {
	if len(l) == 0 {
		return "nothing"
	}
	return strings.Join(l, ", ")
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
