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

// PatchID is the `git patch-id --verbatim` of the diff from..to in dir
// (`--stable` with whitespace kept: a re-indented hunk is another diff),
// read with plumbing (diff-tree) so no user config (renames, prefixes,
// external diffs) changes it; "" for an empty diff.
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
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "patch-id", "--verbatim")
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

// LandedFrom reports whether some first-parent ancestor of merged (among
// its nearest maxBase) is a base from which merged's diff has patch-id
// patch: the diff a carried result read is the one that landed.
func LandedFrom(ctx context.Context, dir, merged, patch string) (bool, error) {
	if patch == "" {
		return false, nil
	}
	out, err := git(dir, "rev-list", "--first-parent", fmt.Sprintf("--max-count=%d", maxBase+1), "--end-of-options", merged)
	if err != nil {
		return false, err
	}
	revs := strings.Fields(out)
	for _, base := range revs[min(1, len(revs)):] {
		id, err := PatchID(ctx, dir, base, merged)
		if err != nil {
			return false, err
		}
		if id == patch {
			return true, nil
		}
	}
	return false, nil
}

// maxBase is how far back LandedFrom looks for the base of a landed diff:
// a lead's branch is its commits on a base, rarely more than a few.
const maxBase = 100
