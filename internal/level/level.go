// Package level brings one checkout level with its origin (020): it
// fetches, judges the checkout against origin/<branch> (or a pinned tag),
// fast-forwards a box that is behind, pushes one that is ahead when the
// push gate allows, and refreshes the plugins sessions load from it. It
// never merges, resets or stashes.
package level

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

// Result is what a level did.
type Result string

const (
	UpToDate      Result = "up to date"
	FastForwarded Result = "fast-forwarded"
	Pinned        Result = "pinned"  // moved to the row's pin tag
	Pushed        Result = "pushed"  // ahead, and pushed
	Ahead         Result = "ahead"   // ahead, not pushed
	Held          Result = "held"    // would move; held for the operator
	TargetMoved   Result = "moved"   // an approval's target is no longer origin's
	Refused       Result = "refused" // the checkout or the job is refused
	Failed        Result = "failed"  // git failed
)

// DefaultPushModes are the modes whose rows a box pushes when ahead.
var DefaultPushModes = []string{"ship", "fork"}

// MinPushAge is how old the newest local commit must be before a push.
const MinPushAge = 10 * time.Minute

// logMax bounds the git log --stat a hold keeps.
const logMax = 64 << 10

// Request is one level of one checkout.
type Request struct {
	Repo     string // the registry name, for the lock
	Path     string // the checkout
	Mode     string // the row's mode
	Pin      string // the row's pin tag ("" = follows its branch)
	StateDir string // <state dir>: the lock lives under it ("" = no lock)

	// Sha, from a job, must be on the target ("" = none); Branch, from a
	// job, is the branch the sender levelled ("" = any).
	Sha, Branch string
	// Push: a box that is ahead may push (a job or sync; never a
	// self-level). PushModes is [hosts] push_modes (nil = default).
	Push      bool
	PushModes []string
	// Hold: a move is held for the operator instead of made. Expect, for
	// an approved hold, is the target the operator saw: another target is
	// answered TargetMoved and nothing moves.
	Hold   bool
	Expect string
	// Now is the clock for the push gate (nil = time.Now).
	Now func() time.Time
}

// Outcome is what a level did, with the answer line.
type Outcome struct {
	Result  Result
	N       int    // commits moved, pushed or ahead
	Why     string // a refusal's reason, or why an ahead box didn't push
	Branch  string // the branch levelled ("" for a pinned row)
	Head    string // HEAD before
	Target  string // origin's commit (the tag's for a pinned row); after a push, HEAD
	Log     string // git log --stat HEAD..target, for a hold or a moved target
	Plugins []string
	Err     error // a plugin refresh that failed after the move
}

// OK is false for a refusal or a failure.
func (o Outcome) OK() bool { return o.Result != Refused && o.Result != Failed }

// Moved reports whether the checkout moved.
func (o Outcome) Moved() bool { return o.Result == FastForwarded || o.Result == Pinned }

// Text is the answer line: `fast-forwarded 3`, `refused: dirty`, `ahead 2,
// not pushed: <why>`, with any plugin lines after it.
func (o Outcome) Text() string {
	var s string
	switch o.Result {
	case UpToDate:
		s = "up to date"
	case FastForwarded, Pushed:
		s = fmt.Sprintf("%s %d", o.Result, o.N)
	case Pinned:
		s = "moved to " + o.Why
	case Ahead:
		s = fmt.Sprintf("ahead %d, not pushed: %s", o.N, o.Why)
	case Held:
		s = fmt.Sprintf("held for approval (+%d %s)", o.N, commits(o.N))
	case TargetMoved:
		s = "target moved to " + short(o.Target)
	case Refused:
		s = "refused: " + o.Why
	case Failed:
		s = "failed: " + o.Why
	}
	if len(o.Plugins) > 0 {
		s += "; " + strings.Join(o.Plugins, "; ")
	}
	if o.Err != nil {
		s += "; plugins: " + o.Err.Error()
	}
	return s
}

func commits(n int) string {
	if n == 1 {
		return "commit"
	}
	return "commits"
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// Lock takes <stateDir>/locks/<repo>.lock (a flock, waiting for another
// holder), so a level, the daemon's routine fetch and a landing never move
// one repo's refs at once.
func Lock(stateDir, repo string) (unlock func(), err error) {
	if repo == "" || strings.ContainsAny(repo, `/\`) || repo == "." || repo == ".." {
		return nil, fmt.Errorf("lock: repo name %q", repo)
	}
	dir := filepath.Join(stateDir, "locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return selfupdate.Lock(filepath.Join(dir, repo))
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
		return s, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, cut(s, 300))
	}
	return s, nil
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var shaRE = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// ValidSha: a hex commit name, as a job carries it.
func ValidSha(s string) bool { return shaRE.MatchString(s) }

var branchRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// ValidBranch: a branch name a job may carry.
func ValidBranch(s string) bool {
	return branchRE.MatchString(s) && !strings.Contains(s, "..") && !strings.HasSuffix(s, ".lock") &&
		!strings.HasSuffix(s, "/") && !strings.Contains(s, "//")
}

// Level levels req.Path, under the repo's lock: a plain fetch, the target
// (origin/<current branch>, or the pin tag), the job's sha on it, the
// checkout check, then a fast-forward, a push or nothing. A move is
// followed by a plugin refresh.
func Level(ctx context.Context, req Request) Outcome {
	if req.StateDir != "" {
		unlock, err := Lock(req.StateDir, req.Repo)
		if err != nil {
			return Outcome{Result: Failed, Why: err.Error()}
		}
		defer unlock()
	}
	o := level(ctx, req)
	if o.Moved() {
		o.Plugins, o.Err = Refresh(ctx, req.Path)
	}
	return o
}

func level(ctx context.Context, req Request) Outcome {
	refuse := func(why string) Outcome { return Outcome{Result: Refused, Why: why} }
	failed := func(err error) Outcome { return Outcome{Result: Failed, Why: err.Error()} }
	dir := req.Path
	if _, err := git(ctx, dir, "rev-parse", "--git-dir"); err != nil {
		return refuse("not a git checkout")
	}
	if _, err := git(ctx, dir, "remote", "get-url", "origin"); err != nil {
		return refuse("no origin")
	}
	if _, err := git(ctx, dir, "fetch", "--quiet", "origin"); err != nil {
		return failed(err)
	}
	if req.Pin != "" {
		ref := "refs/tags/" + req.Pin
		// The pin tag itself, never forced: a tag that moved on origin is
		// a failure, not a silent move.
		if _, err := git(ctx, dir, "fetch", "--quiet", "origin", ref+":"+ref); err != nil {
			if _, lerr := git(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); lerr != nil {
				return refuse("no tag " + req.Pin)
			}
			return failed(err)
		}
	}
	gitDir, err := git(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return failed(err)
	}
	head, err := git(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return failed(err)
	}
	// The target: the pin tag, else origin/<the job's branch>, else
	// origin/<the current branch>.
	o := Outcome{Head: head}
	current, _ := git(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	var target, onto string
	if req.Pin != "" {
		onto = req.Pin
		if target, err = git(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/tags/"+req.Pin+"^{commit}"); err != nil {
			return refuse("no tag " + req.Pin)
		}
	} else {
		o.Branch = req.Branch
		if o.Branch == "" {
			o.Branch = current
		}
		if o.Branch == "" {
			return refuse("other branch")
		}
		onto = "origin/" + o.Branch
		if target, err = git(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+o.Branch+"^{commit}"); err != nil {
			return refuse("no " + onto)
		}
	}
	o.Target = target
	if req.Sha != "" && !ancestor(ctx, dir, req.Sha, target) {
		return refuse(fmt.Sprintf("%s not on %s", short(req.Sha), onto))
	}
	if inProgress(gitDir) {
		return refuse("in progress")
	}
	status, err := git(ctx, dir, "status", "--porcelain")
	if err != nil {
		return failed(err)
	}
	if status != "" {
		return refuse("dirty")
	}
	if req.Pin == "" && current != o.Branch {
		return refuse("other branch")
	}
	if req.Pin != "" {
		return pinned(ctx, req, o)
	}
	ahead, behind, err := counts(ctx, dir, head, target)
	if err != nil {
		return failed(err)
	}
	switch {
	case ahead > 0 && behind > 0:
		o.Result, o.Why = Refused, "diverged"
		return o
	case behind > 0:
		o.N = behind
		if held, ok := hold(ctx, req, &o); ok {
			return held
		}
		if _, err := git(ctx, dir, "merge", "--ff-only", "--quiet", target); err != nil {
			return failed(err)
		}
		o.Result = FastForwarded
		return o
	case ahead > 0:
		o.N = ahead
		return push(ctx, req, o)
	}
	o.Result = UpToDate
	return o
}

// pinned moves a pinned row's checkout to its tag, unless HEAD carries
// commits no remote branch or tag has; a pinned row is never pushed.
func pinned(ctx context.Context, req Request, o Outcome) Outcome {
	dir := req.Path
	if o.Head == o.Target {
		o.Result = UpToDate
		return o
	}
	n, err := git(ctx, dir, "rev-list", "--count", "HEAD", "--not", "--remotes", "--tags")
	if err != nil {
		return Outcome{Result: Failed, Why: err.Error()}
	}
	if c, _ := strconv.Atoi(n); c > 0 {
		o.Result, o.N, o.Why = Ahead, c, "pinned to "+req.Pin
		return o
	}
	if n, err := git(ctx, dir, "rev-list", "--count", o.Head+".."+o.Target); err == nil {
		o.N, _ = strconv.Atoi(n)
	}
	if held, ok := hold(ctx, req, &o); ok {
		return held
	}
	if _, err := git(ctx, dir, "-c", "advice.detachedHead=false", "checkout", "--quiet", "--detach", o.Target); err != nil {
		return Outcome{Result: Failed, Why: err.Error()}
	}
	o.Result, o.Why = Pinned, req.Pin
	return o
}

// hold decides a move that waits: an approval whose target moved, or a
// hold. ok is false when the move goes ahead.
func hold(ctx context.Context, req Request, o *Outcome) (Outcome, bool) {
	switch {
	case req.Expect != "" && req.Expect != o.Target:
		o.Result = TargetMoved
	case req.Expect == "" && req.Hold:
		o.Result = Held
	default:
		return Outcome{}, false
	}
	o.Log = Log(ctx, req.Path, o.Head, o.Target)
	return *o, true
}

// Log is `git log --stat head..target`, bounded.
func Log(ctx context.Context, dir, head, target string) string {
	s, err := git(ctx, dir, "log", "--stat", "--no-color", head+".."+target)
	if err != nil {
		return err.Error()
	}
	return cut(s, logMax)
}

// push pushes the branch and the tags on the pushed commits when the gate
// allows: the caller may push, the row's mode is in push_modes and the
// newest local commit is at least MinPushAge old.
func push(ctx context.Context, req Request, o Outcome) Outcome {
	dir := req.Path
	modes := req.PushModes
	if modes == nil {
		modes = DefaultPushModes
	}
	mode := req.Mode
	if mode == "" {
		mode = DefaultPushModes[0]
	}
	now := time.Now
	if req.Now != nil {
		now = req.Now
	}
	o.Result = Ahead
	switch {
	case !req.Push:
		o.Why = "a self-level never pushes"
		return o
	case !slices.Contains(modes, mode):
		o.Why = fmt.Sprintf("mode %s is not in push_modes", mode)
		return o
	}
	ct, err := git(ctx, dir, "log", "-1", "--format=%ct", "HEAD")
	if err != nil {
		return Outcome{Result: Failed, Why: err.Error()}
	}
	sec, _ := strconv.ParseInt(ct, 10, 64)
	if age := now().Sub(time.Unix(sec, 0)); age < MinPushAge {
		o.Why = fmt.Sprintf("the newest commit is %s old (%s before a push)", age.Round(time.Minute), MinPushAge)
		return o
	}
	tags, err := tagsOn(ctx, dir, o.Target)
	if err != nil {
		return Outcome{Result: Failed, Why: err.Error()}
	}
	ref := "refs/heads/" + o.Branch
	args := append([]string{"push", "--quiet", "origin", ref + ":" + ref}, tags...)
	if _, err := git(ctx, dir, args...); err != nil {
		// Another box pushed first: origin's branch is no longer under ours.
		if _, ferr := git(ctx, dir, "fetch", "--quiet", "origin"); ferr == nil {
			if t, terr := git(ctx, dir, "rev-parse", "--verify", "refs/remotes/origin/"+o.Branch); terr == nil && !ancestor(ctx, dir, t, "HEAD") {
				o.Result, o.Why, o.Target = Refused, "diverged", t
				return o
			}
		}
		return Outcome{Result: Failed, Why: err.Error()}
	}
	o.Result, o.Target = Pushed, o.Head // origin is at HEAD now
	return o
}

// tagsOn is the tags (lightweight or annotated) pointing at a commit in
// target..HEAD, as refs/tags/<t>:refs/tags/<t> refspecs.
func tagsOn(ctx context.Context, dir, target string) ([]string, error) {
	list, err := git(ctx, dir, "rev-list", target+"..HEAD")
	if err != nil {
		return nil, err
	}
	pushed := map[string]bool{}
	for _, c := range strings.Fields(list) {
		pushed[c] = true
	}
	refs, err := git(ctx, dir, "for-each-ref", "refs/tags", "--format=%(refname) %(objectname) %(*objectname)")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(refs, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		commit := f[1]
		if len(f) == 3 {
			commit = f[2] // annotated: the commit it peels to
		}
		if pushed[commit] {
			out = append(out, f[0]+":"+f[0])
		}
	}
	return out, nil
}

// counts is how many commits HEAD has that target lacks, and the reverse.
func counts(ctx context.Context, dir, head, target string) (ahead, behind int, err error) {
	s, err := git(ctx, dir, "rev-list", "--left-right", "--count", head+"..."+target)
	if err != nil {
		return 0, 0, err
	}
	f := strings.Fields(s)
	if len(f) != 2 {
		return 0, 0, fmt.Errorf("rev-list --count: %q", s)
	}
	ahead, _ = strconv.Atoi(f[0])
	behind, _ = strconv.Atoi(f[1])
	return ahead, behind, nil
}

// ancestor: a is a commit in b's history (b itself included).
func ancestor(ctx context.Context, dir, a, b string) bool {
	_, err := git(ctx, dir, "merge-base", "--is-ancestor", "--end-of-options", a, b)
	return err == nil
}

// inProgress: a merge, rebase, cherry-pick or revert is under way, or
// another git holds the index.
func inProgress(gitDir string) bool {
	for _, name := range []string{"MERGE_HEAD", "rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD", "REVERT_HEAD", "index.lock"} {
		if _, err := os.Stat(filepath.Join(gitDir, name)); err == nil || !errors.Is(err, os.ErrNotExist) {
			return true
		}
	}
	return false
}

// JobArgs is a level job's args: the sha (origin's head after the sender
// levelled), the sender's branch ("" for a pinned row) and until, when the
// sender stops waiting for the reply (RFC 3339 UTC to the second).
func JobArgs(sha, branch, until string) []string { return []string{sha, branch, until} }

// ParseJobArgs reads JobArgs, refusing a sha or branch that isn't one.
func ParseJobArgs(args []string) (sha, branch, until string, err error) {
	if len(args) != 3 {
		return "", "", "", fmt.Errorf("want sha, branch and until, got %d args", len(args))
	}
	sha, branch, until = args[0], args[1], args[2]
	if !ValidSha(sha) {
		return "", "", "", fmt.Errorf("sha %q", cut(sha, 64))
	}
	if branch != "" && !ValidBranch(branch) {
		return "", "", "", fmt.Errorf("branch %q", cut(branch, 64))
	}
	return sha, branch, until, nil
}
