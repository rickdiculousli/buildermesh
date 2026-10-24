package spec

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// Entry is one `<kind> <ok|fail> t:<tree first 8>[ m:<model>][ c:<tree
// first 8>]` of a spec's verified line. Carried is the tree a model's result
// was recorded at when it carries to Tree8: the diff it read is the one that
// lands.
type Entry struct {
	Kind    string
	OK      bool
	Tree8   string
	Model   string
	Carried string
}

func (e Entry) String() string {
	s := e.Kind + " fail t:" + e.Tree8
	if e.OK {
		s = e.Kind + " ok t:" + e.Tree8
	}
	if e.Model != "" {
		s += " m:" + e.Model
	}
	if e.Carried != "" {
		s += " c:" + e.Carried
	}
	return s
}

// ErrNoResults is the refusal of an empty entry list.
var ErrNoResults = errors.New("no results for HEAD's tree; run the routines first")

var tree8RE = regexp.MustCompile(`^[0-9a-f]{8}$`)

// ParseVerified reads a verified line's value; an empty one is no entries.
func ParseVerified(line string) ([]Entry, error) {
	if strings.TrimSpace(line) == "" {
		return nil, nil
	}
	var out []Entry
	for _, raw := range strings.Split(line, ";") {
		e, err := parseEntry(strings.TrimSpace(raw))
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func parseEntry(s string) (Entry, error) {
	bad := fmt.Errorf("verified entry %q is not `<kind> <ok|fail> t:<tree first 8>[ m:<model>][ c:<tree first 8>]`", s)
	f := strings.Fields(s)
	if len(f) < 3 || len(f) > 5 {
		return Entry{}, bad
	}
	e := Entry{Kind: f[0]}
	if !kind.NameRE.MatchString(e.Kind) {
		return Entry{}, bad
	}
	switch f[1] {
	case "ok":
		e.OK = true
	case "fail":
	default:
		return Entry{}, bad
	}
	t8, ok := strings.CutPrefix(f[2], "t:")
	if !ok || !tree8RE.MatchString(t8) {
		return Entry{}, bad
	}
	e.Tree8 = t8
	rest := f[3:]
	if len(rest) > 0 && strings.HasPrefix(rest[0], "m:") {
		if e.Model = rest[0][2:]; e.Model == "" {
			return Entry{}, bad
		}
		rest = rest[1:]
	}
	if len(rest) > 0 {
		c, ok := strings.CutPrefix(rest[0], "c:")
		if !ok || !tree8RE.MatchString(c) || len(rest) > 1 {
			return Entry{}, bad
		}
		e.Carried = c
	}
	return e, nil
}

// FormatVerified is the verified line's value for entries.
func FormatVerified(entries []Entry) string {
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = e.String()
	}
	return strings.Join(parts, "; ")
}

// Tree resolves rev's tree in the repo.
func Tree(repoPath, rev string) (string, error) {
	out, err := git(repoPath, "rev-parse", "--verify", "--end-of-options", rev+"^{tree}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ComposeVerified makes an entry per (kind, model) from the results recorded
// for the tree of rev (HEAD, or a commit) in the repo, whatever their args;
// the newest wins. A (kind, model) with none there takes the newest model
// result recorded with rev's patch-id (the diff from its merge-base with
// main to rev) at another tree, as an entry carried from that tree. Entries
// are sorted by kind, then model.
func ComposeVerified(ctx context.Context, c *mesh.Client, repoPath, rev string) ([]Entry, error) {
	tree, err := Tree(repoPath, rev)
	if err != nil {
		return nil, err
	}
	all, err := mesh.ListAllResults(ctx, c)
	if err != nil {
		return nil, err
	}
	patch, err := revPatch(ctx, repoPath, rev)
	if err != nil {
		return nil, err
	}
	type group struct{ kind, model string }
	newest := map[group]mesh.Result{}
	carried := map[group]mesh.Result{}
	for _, r := range all {
		g := group{r.Kind, r.Model}
		switch {
		case r.Tree == tree:
			if old, ok := newest[g]; !ok || Newer(r.At, old.At) {
				newest[g] = r
			}
		case patch != "" && r.Model != "" && r.PatchID == patch && len(r.Tree) >= 8:
			if old, ok := carried[g]; !ok || Newer(r.At, old.At) {
				carried[g] = r
			}
		}
	}
	out := make([]Entry, 0, len(newest)+len(carried))
	for g, r := range newest {
		out = append(out, Entry{Kind: g.kind, OK: r.OK, Tree8: tree[:8], Model: g.model})
	}
	for g, r := range carried {
		if _, ok := newest[g]; !ok {
			out = append(out, Entry{Kind: g.kind, OK: r.OK, Tree8: tree[:8], Model: g.model, Carried: r.Tree[:8]})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Model < out[j].Model
	})
	return out, nil
}

// revPatch is the patch-id of the repo's diff from rev's merge-base with
// main (origin/main when there is an origin) to rev: "" when rev is on main
// or there is no main to measure from.
func revPatch(ctx context.Context, repoPath, rev string) (string, error) {
	base := "main"
	if _, err := gitCtx(ctx, repoPath, "remote", "get-url", "origin"); err == nil {
		base = "origin/main"
	}
	mb, err := gitCtx(ctx, repoPath, "merge-base", base, rev)
	if err != nil {
		return "", nil
	}
	return checkout.PatchID(ctx, repoPath, strings.TrimSpace(mb), rev)
}

// Newer reports whether At a is later than b, comparing the strings when
// either does not parse.
func Newer(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339, a)
	tb, errB := time.Parse(time.RFC3339, b)
	if errA != nil || errB != nil {
		return a > b
	}
	return ta.After(tb)
}

// VerifiedLine is FormatVerified(entries), refused unless it reads back
// as the same entries (a model with a ";" or a space would not), so the
// trailer land writes is one done can check.
func VerifiedLine(entries []Entry) (string, error) {
	if len(entries) == 0 {
		return "", ErrNoResults
	}
	value := FormatVerified(entries)
	if back, err := ParseVerified(value); err != nil {
		return "", fmt.Errorf("verified %q: %w", value, err)
	} else if !slices.Equal(back, entries) {
		return "", fmt.Errorf("verified %q would read back as other entries", value)
	}
	return value, nil
}
