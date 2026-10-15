package spec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// Entry is one `<kind> <ok|fail> t:<tree first 8>[ m:<model>]` of a spec's
// verified line.
type Entry struct {
	Kind  string
	OK    bool
	Tree8 string
	Model string
}

func (e Entry) String() string {
	s := e.Kind + " fail t:" + e.Tree8
	if e.OK {
		s = e.Kind + " ok t:" + e.Tree8
	}
	if e.Model != "" {
		s += " m:" + e.Model
	}
	return s
}

// ErrNoResults is WriteVerified's refusal of an empty entry list.
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
	bad := fmt.Errorf("verified entry %q is not `<kind> <ok|fail> t:<tree first 8>[ m:<model>]`", s)
	f := strings.Fields(s)
	if len(f) != 3 && len(f) != 4 {
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
	if len(f) == 4 {
		m, ok := strings.CutPrefix(f[3], "m:")
		if !ok || m == "" {
			return Entry{}, bad
		}
		e.Model = m
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
// for the tree of the repo's HEAD, whatever their args; the newest wins.
// Entries are sorted by kind, then model.
func ComposeVerified(ctx context.Context, c *mesh.Client, repoPath string) ([]Entry, error) {
	tree, err := Tree(repoPath, "HEAD")
	if err != nil {
		return nil, err
	}
	all, err := mesh.ListAllResults(ctx, c)
	if err != nil {
		return nil, err
	}
	type group struct{ kind, model string }
	newest := map[group]mesh.Result{}
	for _, r := range all {
		if r.Tree != tree {
			continue
		}
		g := group{r.Kind, r.Model}
		if old, ok := newest[g]; !ok || newer(r.At, old.At) {
			newest[g] = r
		}
	}
	out := make([]Entry, 0, len(newest))
	for g, r := range newest {
		out = append(out, Entry{Kind: g.kind, OK: r.OK, Tree8: tree[:8], Model: g.model})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Model < out[j].Model
	})
	return out, nil
}

// newer reports whether At a is later than b, comparing the strings when
// either does not parse.
func newer(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339, a)
	tb, errB := time.Parse(time.RFC3339, b)
	if errA != nil || errB != nil {
		return a > b
	}
	return ta.After(tb)
}

// WriteVerified sets the spec's verified key to entries the way
// tools/proposals.py's update sets a key: the first `verified:` line is
// rewritten in place, else one is added before the closing fence. Every
// other byte is kept.
func WriteVerified(specPath string, entries []Entry) error {
	if len(entries) == 0 {
		return ErrNoResults
	}
	b, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	text := string(b)
	if _, err := Frontmatter(text); err != nil {
		return fmt.Errorf("%s: %w", specPath, err)
	}
	value := FormatVerified(entries)
	lines := strings.SplitAfter(text, "\n")
	closing := -1
	set := false
	for i := 1; i < len(lines); i++ {
		body := strings.TrimRight(lines[i], "\r\n")
		if strings.TrimSpace(body) == "---" {
			closing = i
			break
		}
		if set || strings.HasPrefix(strings.TrimLeft(body, " \t"), "#") {
			continue
		}
		if k, _, ok := strings.Cut(body, ":"); ok && strings.TrimSpace(k) == "verified" {
			lines[i] = "verified: " + value + lines[i][len(body):]
			set = true
		}
	}
	if !set {
		lines = append(lines[:closing], append([]string{"verified: " + value + "\n"}, lines[closing:]...)...)
	}
	out := strings.Join(lines, "")
	fm, err := Frontmatter(out)
	if err != nil {
		return fmt.Errorf("%s: %w", specPath, err)
	}
	if fm["verified"] != value {
		return fmt.Errorf("%s: writing verified would read back as %q; left unchanged", specPath, fm["verified"])
	}
	return os.WriteFile(specPath, []byte(out), 0o644)
}
