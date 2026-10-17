package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

// First is the first release, tagged before notes existed: its notes,
// written after it, start a box over. A box with no record walks from it.
const First = "v0.1.0"

func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Between is the release tags a box walks from from to to, oldest first:
// the vX.Y.Z tags in to's history that from's history lacks. A from of ""
// (a box with no record) starts after First, so its start-over is never
// walked; a from First isn't an ancestor of (a box from before it) walks
// First's notes first.
func Between(dir, from, to string) ([]string, error) {
	out, err := git(dir, "tag", "--list", "v*", "--merged", to)
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, t := range strings.Fields(out) {
		if !selfupdate.IsRelease(t) {
			continue
		}
		if from == "" {
			if selfupdate.Later(t, First) {
				tags = append(tags, t)
			}
			continue
		}
		if !selfupdate.Contains(dir, t, from) {
			tags = append(tags, t)
		}
	}
	slices.SortFunc(tags, func(a, b string) int {
		switch {
		case selfupdate.Later(a, b):
			return 1
		case selfupdate.Later(b, a):
			return -1
		}
		return 0
	})
	return tags, nil
}

// Read is tag's notes as ref (main, not the tag: notes written after a tag
// still apply) has them in dir; false when ref has none.
func Read(dir, ref, tag string) (*Notes, bool, error) {
	obj := ref + ":" + Path(tag)
	if _, err := git(dir, "cat-file", "-e", obj); err != nil {
		return nil, false, nil
	}
	text, err := git(dir, "show", obj)
	if err != nil {
		return nil, false, err
	}
	n, err := Parse(tag, text)
	if err != nil {
		return nil, false, err
	}
	return n, true, nil
}

// MainRef is the ref of main dir's notes are read from: origin's main when
// dir has one (a clone, or a checkout of a repo with an origin), else main.
func MainRef(dir string) string {
	if _, err := git(dir, "rev-parse", "--verify", "-q", "refs/remotes/origin/main"); err == nil {
		return "refs/remotes/origin/main"
	}
	return "refs/heads/main"
}

// Planned is a step as a walk sees it.
type Planned struct {
	ID      string `json:"id"` // <release>#<n>, n from 1 in the release's notes
	Release string `json:"release"`
	Step
	Done   bool   `json:"done"`
	Answer string `json:"answer,omitempty"` // an ask's answer, once given
}

// Plan is the steps of notes that apply on goos, in order, marked done as
// st records.
func Plan(notes []*Notes, goos string, st *State) []Planned {
	var out []Planned
	for _, n := range notes {
		for i, s := range n.Steps {
			if !s.Applies(goos) {
				continue
			}
			p := Planned{ID: fmt.Sprintf("%s#%d", n.Release, i+1), Release: n.Release, Step: s}
			if st != nil {
				p.Done = slices.Contains(st.Done, p.ID)
				p.Answer = st.Answers[p.ID]
			}
			out = append(out, p)
		}
	}
	return out
}

// State is a walk under way, <state dir>/upgrade.json: where it goes from
// and to, and the steps done, so a walk stopped by a failing step resumes
// after the last one done.
type State struct {
	From    string            `json:"from"`              // the revision walked from ("" = no record)
	FromAs  string            `json:"from_label"`        // how it was named (v0.1.0, <rev8>)
	To      string            `json:"to"`                // the revision walked to
	ToAs    string            `json:"to_label"`          // its label (v0.2.0)
	Shown   bool              `json:"shown"`             // the walk's header was printed
	Done    []string          `json:"done"`              // step ids done
	Answers map[string]string `json:"answers,omitempty"` // ask answers by step id
}

// StatePath is the walk's file.
func StatePath(stateDir string) string { return filepath.Join(stateDir, "upgrade.json") }

// LoadState is the walk under way, nil when there is none.
func LoadState(stateDir string) (*State, error) {
	b, err := os.ReadFile(StatePath(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", StatePath(stateDir), err)
	}
	return &st, nil
}

// Save writes st.
func (st *State) Save(stateDir string) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(StatePath(stateDir), append(b, '\n'))
}

// ClearState removes the walk's file.
func ClearState(stateDir string) error {
	if err := os.Remove(StatePath(stateDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// writeFile writes b to p through a temporary file and a rename, making
// p's directory (a start-over removes the state dir mid-walk).
func writeFile(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+"-*")
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), p); err != nil {
		os.Remove(f.Name())
		return err
	}
	return nil
}

// Record is <state dir>/installed: the version this box was set up at or
// last walked to, a record, never the source of truth (the binary's
// revision is).
type Record struct {
	Version  string // its label: v0.2.0, v0.2.0+3, or <rev8>
	Revision string // the full commit, or "" when unknown
	At       string // RFC 3339
}

// InstalledPath is the record's file.
func InstalledPath(stateDir string) string { return filepath.Join(stateDir, "installed") }

// PreviousPath is where a swap that walks nothing keeps the revision of the
// binary it replaced, for the walk the new binary asks for.
func PreviousPath(stateDir string) string { return filepath.Join(stateDir, "previous") }

// WriteInstalled writes the record: `<version> <revision> <time>`.
func WriteInstalled(stateDir string, r Record) error {
	rev := r.Revision
	if rev == "" {
		rev = selfupdate.Unknown
	}
	return writeFile(InstalledPath(stateDir), []byte(fmt.Sprintf("%s %s %s\n", r.Version, rev, r.At)))
}

// ReadInstalled is the record; false when there is none (or it doesn't
// read).
func ReadInstalled(stateDir string) (Record, bool) {
	b, err := os.ReadFile(InstalledPath(stateDir))
	if err != nil {
		return Record{}, false
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return Record{}, false
	}
	r := Record{Version: f[0], Revision: f[1]}
	if r.Revision == selfupdate.Unknown {
		r.Revision = ""
	}
	if len(f) > 2 {
		r.At = f[2]
	}
	return r, true
}

// WritePrevious keeps rev, the revision (or release tag) of the binary a
// swap replaced.
func WritePrevious(stateDir, rev string) error {
	return writeFile(PreviousPath(stateDir), []byte(rev+"\n"))
}

// ReadPrevious is what WritePrevious kept, "" when nothing.
func ReadPrevious(stateDir string) string {
	b, err := os.ReadFile(PreviousPath(stateDir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Pending is the line a binary whose release is own (a Describe label,
// v0.2.0 or v0.2.0+3) prints when the notes of a release after the one
// this box last walked to (the record, else First) are still to walk:
// `upgrade notes pending since v0.1.0: run bmesh update`. "" when own
// names no release, or none is pending.
func Pending(stateDir, own string) string {
	base := selfupdate.Base(own)
	if base == "" {
		return ""
	}
	since := First
	if r, ok := ReadInstalled(stateDir); ok {
		since = r.Version
	}
	sinceBase := selfupdate.Base(since)
	if sinceBase == "" {
		sinceBase = First
	}
	if !selfupdate.Later(base, sinceBase) {
		return ""
	}
	return fmt.Sprintf("upgrade notes pending since %s: run bmesh update", since)
}
