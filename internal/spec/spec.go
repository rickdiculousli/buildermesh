// Package spec finds a spec on the project's main branch and reads its
// frontmatter, with the same rules as tools/proposals.py.
package spec

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/gitfetch"
)

// Frontmatter parses the `---` fenced block at the top of text.
func Frontmatter(text string) (map[string]string, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return nil, errors.New("malformed: no frontmatter")
	}
	m := map[string]string{}
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		t := strings.TrimSpace(line)
		if t == "---" {
			return m, nil
		}
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if !strings.Contains(line, ": ") && !strings.HasSuffix(t, ":") {
			return nil, fmt.Errorf("malformed: line %d: %s", i+1, line)
		}
		k, v, _ := strings.Cut(line, ":")
		m[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return nil, errors.New("malformed: no closing ---")
}

func git(repo string, args ...string) (string, error) {
	return gitCtx(context.Background(), repo, args...)
}

// gitCtx runs git in repo under ctx, for at most 30 s.
func gitCtx(ctx context.Context, repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

type notFoundError struct{ id, ref string }

func (e *notFoundError) Error() string { return fmt.Sprintf("no spec %s on %s", e.id, e.ref) }

// IsNotFound reports whether err says the spec is not on the ref read.
func IsNotFound(err error) bool {
	var nf *notFoundError
	return errors.As(err, &nf)
}

// FindOnMain returns the path and text of spec id on origin/main (after a
// fetch) when the repo has an origin, else on main.
func FindOnMain(repoPath, glob, id string) (string, string, error) {
	p, text, _, err := findOnMain(context.Background(), repoPath, glob, id, true)
	return p, text, err
}

// FindOnMainLocal is FindOnMain without the fetch: it reads the local
// origin/main ref as it stands, under ctx.
func FindOnMainLocal(ctx context.Context, repoPath, glob, id string) (string, string, error) {
	p, text, _, err := findOnMain(ctx, repoPath, glob, id, false)
	return p, text, err
}

// FetchMain fetches origin's main into the local origin/main when the repo
// has an origin; without one it does nothing.
func FetchMain(ctx context.Context, repoPath string) error {
	if _, err := gitCtx(ctx, repoPath, "remote", "get-url", "origin"); err != nil {
		return nil
	}
	_, err := gitCtx(ctx, repoPath, gitfetch.Args("--quiet", "origin", "main")...)
	return err
}

// findOnMain is FindOnMain (FindOnMainLocal when fetch is false) also
// naming the ref it read.
func findOnMain(ctx context.Context, repoPath, glob, id string, fetch bool) (p, text, ref string, err error) {
	if ref, err = mainRef(ctx, repoPath, fetch); err != nil {
		return "", "", ref, err
	}
	pattern := strings.ReplaceAll(glob, "{id}", id)
	dir := path.Dir(glob)
	out, err := gitCtx(ctx, repoPath, "ls-tree", "--name-only", "-r", ref, "--", dir)
	if err != nil {
		return "", "", ref, err
	}
	var hits []string
	for _, e := range strings.Split(out, "\n") {
		if e == "" {
			continue
		}
		ok, err := path.Match(pattern, e)
		if err != nil {
			return "", "", ref, fmt.Errorf("bad spec glob %q: %w", glob, err)
		}
		if ok {
			hits = append(hits, e)
		}
	}
	switch len(hits) {
	case 0:
		return "", "", ref, &notFoundError{id, ref}
	case 1:
	default:
		return "", "", ref, fmt.Errorf("spec %s matches %d files on %s: %s", id, len(hits), ref, strings.Join(hits, ", "))
	}
	if text, err = gitCtx(ctx, repoPath, "show", ref+":"+hits[0]); err != nil {
		return "", "", ref, err
	}
	return hits[0], text, ref, nil
}

// mainRef is origin/main when the repo has an origin (fetched first when
// fetch is set), else main.
func mainRef(ctx context.Context, repoPath string, fetch bool) (string, error) {
	if _, err := gitCtx(ctx, repoPath, "remote", "get-url", "origin"); err != nil {
		return "main", nil
	}
	if fetch {
		if _, err := gitCtx(ctx, repoPath, gitfetch.Args("--quiet", "origin", "main")...); err != nil {
			return "main", err
		}
	}
	return "origin/main", nil
}

// ScanMainLocal reads the ledger as it stands on the local origin/main
// (main without an origin), without a fetch, under ctx, sorted by id. A
// file that does not parse is left out rather than failing the read.
func ScanMainLocal(ctx context.Context, repoPath, glob string) ([]*Spec, error) {
	ref, err := mainRef(ctx, repoPath, false)
	if err != nil {
		return nil, err
	}
	dir := path.Dir(glob)
	out, err := gitCtx(ctx, repoPath, "ls-tree", "--name-only", ref, "--", dir+"/")
	if err != nil {
		return nil, err
	}
	var specs []*Spec
	for _, p := range strings.Split(out, "\n") {
		if p == "" || !strings.HasSuffix(p, ".md") || path.Base(p) == "README.md" {
			continue
		}
		text, err := gitCtx(ctx, repoPath, "show", ref+":"+p)
		if err != nil {
			return nil, err
		}
		if s, err := Parse(text, p); err == nil {
			specs = append(specs, s)
		}
	}
	sort.SliceStable(specs, func(i, j int) bool { return specs[i].ID < specs[j].ID })
	return specs, nil
}

// Done is the verdict of CheckDone. Verified is the spec's verified line,
// parsed; OK and Why do not depend on it.
type Done struct {
	Path, Status, Merged string
	OK                   bool
	Why                  string
	Verified             []Entry
}

// CheckDone reports whether spec id has landed as done with a merged value
// that is on main (origin/main when the repo has an origin). A missing spec
// is a verdict (OK false), a malformed one (frontmatter or verified line) an
// error.
func CheckDone(repoPath, glob, id string) (Done, error) {
	p, text, ref, err := findOnMain(context.Background(), repoPath, glob, id, true)
	var nf *notFoundError
	if errors.As(err, &nf) {
		return Done{Why: nf.Error()}, nil
	}
	if err != nil {
		return Done{}, err
	}
	fm, err := Frontmatter(text)
	if err != nil {
		return Done{Path: p}, fmt.Errorf("%s: %w", p, err)
	}
	verified, err := ParseVerified(fm["verified"])
	if err != nil {
		return Done{Path: p}, fmt.Errorf("%s: %w", p, err)
	}
	d := Done{Path: p, Status: fm["status"], Merged: fm["merged"], Verified: verified}
	switch {
	case d.Status != "done":
		d.Why = "status is " + d.Status
	case d.Merged == "":
		d.Why = "merged is empty"
	case !onRef(repoPath, d.Merged, ref):
		d.Why = "merged " + d.Merged + " is not on " + ref
	default:
		d.OK = true
	}
	return d, nil
}

// onRef reports whether rev names a commit that ref contains.
func onRef(repoPath, rev, ref string) bool {
	sha, err := git(repoPath, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return false
	}
	_, err = git(repoPath, "merge-base", "--is-ancestor", strings.TrimSpace(sha), ref)
	return err == nil
}
