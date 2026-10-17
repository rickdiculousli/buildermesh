package selfupdate

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// What a box follows ([update] track): the newest release tag, or the
// code head of main.
const (
	TrackRelease = "release"
	TrackLatest  = "latest"
)

// releaseTag is a release tag: vX.Y.Z, no pre-release or build suffix.
var releaseTag = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)

// IsRelease reports whether s is a release tag, vX.Y.Z.
func IsRelease(s string) bool { return releaseTag.MatchString(s) }

// releaseNumbers is a release tag's three numbers.
func releaseNumbers(tag string) ([3]int, bool) {
	m := releaseTag.FindStringSubmatch(tag)
	if m == nil {
		return [3]int{}, false
	}
	var r [3]int
	for i := range r {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, false
		}
		r[i] = n
	}
	return r, true
}

// NewestRelease is the highest release tag in ref's history, "" when it
// has none.
func NewestRelease(dir, ref string) (string, error) {
	tags, err := git(dir, "tag", "--list", "v*", "--merged", ref)
	if err != nil {
		return "", err
	}
	var last string
	var top [3]int
	for _, t := range strings.Fields(tags) {
		if r, ok := releaseNumbers(t); ok && (last == "" || slices.Compare(r[:], top[:]) > 0) {
			last, top = t, r
		}
	}
	return last, nil
}

// CodeHead is the newest commit of ref that changes anything outside
// specDir (ref itself when only specs ever changed), so ledger commits
// don't count as code.
func CodeHead(dir, ref, specDir string) (string, error) {
	sha, err := git(dir, "rev-list", "-1", ref, "--", ".", ":(exclude)"+specDir)
	if err != nil || sha != "" {
		return sha, err
	}
	return git(dir, "rev-parse", "--verify", ref+"^{commit}")
}

// Target is the commit a box follows.
type Target struct {
	Commit string // the full commit
	Tag    string // the release tag it is; "" for the code head
	NoTag  bool   // track release, but ref has no release tag yet: the code head stands in
}

// Follow is what track (TrackRelease, or "") or TrackLatest follows on ref
// in dir: the newest release tag in ref's history, or ref's code head
// (CodeHead), which a release track with no tag yet follows too.
func Follow(dir, ref, track, specDir string) (Target, error) {
	if track != TrackLatest {
		tag, err := NewestRelease(dir, ref)
		if err != nil {
			return Target{}, err
		}
		if tag != "" {
			sha, err := git(dir, "rev-parse", "--verify", "--end-of-options", tag+"^{commit}")
			if err != nil {
				return Target{}, err
			}
			return Target{Commit: sha, Tag: tag}, nil
		}
	}
	head, err := CodeHead(dir, ref, specDir)
	if err != nil {
		return Target{}, err
	}
	return Target{Commit: head, NoTag: track != TrackLatest}, nil
}

// Ref is what go install names the target by: its tag, else its commit.
func (t Target) Ref() string {
	if t.Tag != "" {
		return t.Tag
	}
	return t.Commit
}

// Standing is where a binary stands against its target.
type Standing int

const (
	Behind  Standing = iota // the target has the binary's commit in its history
	Current                 // the binary is the target
	Ahead                   // the binary has the target in its history: never moved back
	Apart                   // neither: unknown, or another line (a rollback)
)

// Stand is where own (a commit, OwnCommit's) stands against target in dir.
func Stand(dir, own, target string) Standing {
	switch {
	case Same(own, target):
		return Current
	case Contains(dir, target, own):
		return Ahead
	case Contains(dir, own, target):
		return Behind
	}
	return Apart
}

// OwnCommit is own, a binary's revision or, for a tagged module install
// that names no commit, its release tag, as a commit in dir: the tag
// resolved there, "" when dir has no such tag.
func OwnCommit(dir, own string) string {
	if !IsRelease(own) {
		return own
	}
	sha, err := git(dir, "rev-parse", "--verify", "-q", "--end-of-options", "refs/tags/"+own+"^{commit}")
	if err != nil {
		return ""
	}
	return sha
}

// Label names commit for a person: its release (`v0.2.0`), the release
// it follows with its commit (`v0.2.0+2 (<rev8>)`), or `<rev8>` when no
// release is in its history (Describe in dir).
func Label(dir, commit string) string {
	if commit == "" || commit == Unknown {
		return Unknown
	}
	rel := Describe(dir, commit)
	switch {
	case rel == "":
		return Short(commit)
	case strings.Contains(rel, "+"):
		return fmt.Sprintf("%s (%s)", rel, Short(commit))
	}
	return rel
}

// Base is the release a label names or follows: v0.2.0 for v0.2.0 and
// v0.2.0+3, "" for anything else.
func Base(label string) string {
	base, _, _ := strings.Cut(label, "+")
	if !IsRelease(base) {
		return ""
	}
	return base
}

// Later reports whether release tag a comes after b; a tag is later than
// "" or anything that isn't one.
func Later(a, b string) bool {
	ra, ok := releaseNumbers(a)
	if !ok {
		return false
	}
	rb, ok := releaseNumbers(b)
	return !ok || slices.Compare(ra[:], rb[:]) > 0
}
