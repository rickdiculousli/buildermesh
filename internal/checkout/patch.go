package checkout

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// baseRE finds the `base: <sha>` a review job's text carries.
var baseRE = regexp.MustCompile(`(?:^|\s)base:\s*([0-9a-fA-F]{7,64})\b`)

// BaseFromText is the commit a job text's `base: <sha>` names, "" when it
// names none.
func BaseFromText(text string) string {
	m := baseRE.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return m[1]
}

// PatchID is the `git patch-id --stable` of the diff from..to in dir, read
// with plumbing (diff-tree) so no user config (renames, prefixes, external
// diffs) changes it; "" for an empty diff.
func PatchID(ctx context.Context, dir, from, to string) (string, error) {
	diff := exec.CommandContext(ctx, "git", "-C", dir, "diff-tree", "-r", "-p", "--binary", "--full-index",
		"--no-renames", "--no-color", "--no-ext-diff", "--no-textconv", "--end-of-options", from, to)
	diff.Env = append(os.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	diff.Stderr = &stderr
	patch, err := diff.Output()
	if err != nil {
		return "", fmt.Errorf("git diff-tree %s %s: %w: %s", from, to, err, strings.TrimSpace(stderr.String()))
	}
	if len(patch) == 0 {
		return "", nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "patch-id", "--stable")
	cmd.Stdin = bytes.NewReader(patch)
	id, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git patch-id: %w", err)
	}
	f := strings.Fields(string(id))
	if len(f) == 0 {
		return "", nil
	}
	return f[0], nil
}

// ReviewedPatch is the patch-id of base..commit in dir, base the commit the
// job text's `base: <sha>` names: the diff a review was asked to read. It is
// "" when the text names no base.
func ReviewedPatch(ctx context.Context, dir, text, commit string) (string, error) {
	base := BaseFromText(text)
	if base == "" {
		return "", nil
	}
	sha, err := git(dir, "rev-parse", "--verify", "--end-of-options", base+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("base %s: not a commit in %s", base, dir)
	}
	return PatchID(ctx, dir, sha, commit)
}
