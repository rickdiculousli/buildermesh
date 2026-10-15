// Package spec finds a spec on the project's main branch and reads its
// frontmatter, with the same rules as tools/proposals.py.
package spec

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strings"
	"time"
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

// FindOnMain returns the path and text of spec id on origin/main (after a
// fetch) when the repo has an origin, else on main.
func FindOnMain(repoPath, glob, id string) (string, string, error) {
	ref := "main"
	if _, err := git(repoPath, "remote", "get-url", "origin"); err == nil {
		if _, err := git(repoPath, "fetch", "--quiet", "origin", "main"); err != nil {
			return "", "", err
		}
		ref = "origin/main"
	}
	pattern := strings.ReplaceAll(glob, "{id}", id)
	dir := path.Dir(glob)
	out, err := git(repoPath, "ls-tree", "--name-only", "-r", ref, "--", dir)
	if err != nil {
		return "", "", err
	}
	var hits []string
	for _, e := range strings.Split(out, "\n") {
		if e == "" {
			continue
		}
		ok, err := path.Match(pattern, e)
		if err != nil {
			return "", "", fmt.Errorf("bad spec glob %q: %w", glob, err)
		}
		if ok {
			hits = append(hits, e)
		}
	}
	switch len(hits) {
	case 0:
		return "", "", &notFoundError{id, ref}
	case 1:
	default:
		return "", "", fmt.Errorf("spec %s matches %d files on %s: %s", id, len(hits), ref, strings.Join(hits, ", "))
	}
	text, err := git(repoPath, "show", ref+":"+hits[0])
	if err != nil {
		return "", "", err
	}
	return hits[0], text, nil
}

// Done is the verdict of CheckDone.
type Done struct {
	Path, Status, Merged string
	OK                   bool
	Why                  string
}

// CheckDone reports whether spec id has landed as done with a merged value.
// A missing spec is a verdict (OK false), a malformed one an error.
func CheckDone(repoPath, glob, id string) (Done, error) {
	p, text, err := FindOnMain(repoPath, glob, id)
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
	d := Done{Path: p, Status: fm["status"], Merged: fm["merged"]}
	switch {
	case d.Status != "done":
		d.Why = "status is " + d.Status
	case d.Merged == "":
		d.Why = "merged is empty"
	default:
		d.OK = true
	}
	return d, nil
}
