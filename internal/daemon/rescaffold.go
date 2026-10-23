package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/gitwork"
	"github.com/rickdiculousli/buildermesh/internal/level"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/scaffold"
)

// scaffoldedFile is the rescaffold record in the state dir (074).
const scaffoldedFile = "scaffolded.json"

// rescaffoldTimeout caps one repo's attempt: level, edit, push.
const rescaffoldTimeout = 3 * time.Minute

// scaffoldFields are a row's fields scaffold writes into the repo; a
// registry that changes one of them changes the repo's scaffold.
var scaffoldFields = []string{"verify", "setup", "setup_inputs", "setup_timeout"}

// scaffolded is scaffolded.json: the registry version Rows are from,
// every row's scaffold fields at that version (what the next registry is
// compared with, so a version another process took while the daemon was
// down is still seen), and each repo's record.
type scaffolded struct {
	Version int64                  `json:"version"`
	Rows    map[string]scaffoldRow `json:"rows"`
	Repos   map[string]scaffoldRec `json:"repos"`
}

// scaffoldRow is a row's scaffold fields.
type scaffoldRow struct {
	Verify       []string `json:"verify,omitempty"`
	Setup        string   `json:"setup,omitempty"`
	SetupInputs  []string `json:"setup_inputs,omitempty"`
	SetupTimeout string   `json:"setup_timeout,omitempty"`
}

func rowsOf(rows map[string]config.Repo) map[string]scaffoldRow {
	out := make(map[string]scaffoldRow, len(rows))
	for name, r := range rows {
		out[name] = scaffoldRow{Verify: r.Verify, Setup: r.Setup, SetupInputs: r.SetupInputs, SetupTimeout: r.SetupTimeout}
	}
	return out
}

// scaffoldRec is one repo's record: the registry version that last
// changed its row's scaffold fields, the fields changed since the last
// landing, and whether it landed. Told is what was pushed for Version:
// "failed" or "nocheckout", each once.
type scaffoldRec struct {
	Version int64    `json:"version"`
	Fields  []string `json:"fields"`
	Landed  bool     `json:"landed"`
	Commit  string   `json:"commit,omitempty"`
	Told    string   `json:"told,omitempty"`
}

func scaffoldedPath(stateDir string) string { return filepath.Join(stateDir, scaffoldedFile) }

func loadScaffolded(stateDir string) (scaffolded, error) {
	s := scaffolded{Repos: map[string]scaffoldRec{}}
	b, err := os.ReadFile(scaffoldedPath(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return scaffolded{Repos: map[string]scaffoldRec{}}, fmt.Errorf("%s: %v", scaffoldedFile, err)
	}
	if s.Repos == nil {
		s.Repos = map[string]scaffoldRec{}
	}
	return s, nil
}

func saveScaffolded(stateDir string, s scaffolded) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(stateDir, "."+scaffoldedFile+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), scaffoldedPath(stateDir))
}

// changedScaffold is the scaffold fields that differ between a row's old
// and new versions.
func changedScaffold(old, new scaffoldRow) []string {
	var out []string
	if !slices.Equal(old.Verify, new.Verify) {
		out = append(out, "verify")
	}
	if old.Setup != new.Setup {
		out = append(out, "setup")
	}
	if !slices.Equal(old.SetupInputs, new.SetupInputs) {
		out = append(out, "setup_inputs")
	}
	if old.SetupTimeout != new.SetupTimeout {
		out = append(out, "setup_timeout")
	}
	return out
}

// noteScaffold records registry version v's rows in scaffolded.json and,
// as pending, each ship row this box hosts whose scaffold fields differ
// from the rows recorded there (old, the rows the daemon had, when
// nothing is recorded yet): a newer version replaces a pending one (the
// fields add up until one lands). A version already recorded changes
// nothing; a row new in rows is not a change, its first scaffold being the
// operator's.
func (h *hostsNode) noteScaffold(v int64, old, rows map[string]config.Repo) {
	h.scaffoldMu.Lock()
	defer h.scaffoldMu.Unlock()
	s, err := loadScaffolded(h.d.StateDir)
	if err != nil {
		h.d.Log.Warn("hosts: rescaffold", "err", err)
	}
	if s.Version == v && s.Rows != nil {
		return
	}
	base := s.Rows
	if base == nil {
		base = rowsOf(old)
	}
	now := rowsOf(rows)
	for name, r := range rows {
		o, ok := base[name]
		if !ok || r.Host != h.d.Host || r.Mode != "ship" {
			continue
		}
		fields := changedScaffold(o, now[name])
		if len(fields) == 0 {
			continue
		}
		if rec := s.Repos[name]; !rec.Landed && rec.Version > 0 {
			fields = unionFields(rec.Fields, fields)
		}
		s.Repos[name] = scaffoldRec{Version: v, Fields: fields}
		h.d.Log.Info("hosts: registry changes a repo's scaffold", "repo", name, "version", v, "fields", strings.Join(fields, ","))
	}
	s.Version, s.Rows = v, now
	if err := saveScaffolded(h.d.StateDir, s); err != nil {
		h.d.Log.Warn("hosts: rescaffold", "err", err)
	}
}

// unionFields is a and b's fields, in scaffoldFields order.
func unionFields(a, b []string) []string {
	var out []string
	for _, f := range scaffoldFields {
		if slices.Contains(a, f) || slices.Contains(b, f) {
			out = append(out, f)
		}
	}
	return out
}

// goRescaffold starts rescaffold unless one runs.
func (h *hostsNode) goRescaffold(ctx context.Context) {
	if !h.rescaffolding.CompareAndSwap(false, true) {
		return
	}
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer h.rescaffolding.Store(false)
		h.rescaffold(ctx)
	}()
}

// rescaffold tries each pending repo in scaffolded.json once, in name
// order, and records the outcome; a version noted while it tried is kept
// for the next run.
func (h *hostsNode) rescaffold(ctx context.Context) {
	h.scaffoldMu.Lock()
	s, err := loadScaffolded(h.d.StateDir)
	h.scaffoldMu.Unlock()
	if err != nil {
		h.d.Log.Warn("hosts: rescaffold", "err", err)
		return
	}
	names := make([]string, 0, len(s.Repos))
	for name, rec := range s.Repos {
		if !rec.Landed {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if ctx.Err() != nil {
			return
		}
		// The rows, then the record: a version is noted before its rows
		// are taken, so a record newer than the rows waits for them.
		cfg, v := h.confVersion()
		h.scaffoldMu.Lock()
		s, err := loadScaffolded(h.d.StateDir)
		h.scaffoldMu.Unlock()
		was, ok := s.Repos[name]
		if err != nil || !ok || was.Landed || was.Version > v {
			continue
		}
		rec, keep := h.rescaffoldRepo(ctx, cfg, name, was)
		h.scaffoldMu.Lock()
		s, err = loadScaffolded(h.d.StateDir)
		if now := s.Repos[name]; err == nil && now.Version == was.Version && slices.Equal(now.Fields, was.Fields) {
			if keep {
				s.Repos[name] = rec
			} else {
				delete(s.Repos, name)
			}
			err = saveScaffolded(h.d.StateDir, s)
		}
		h.scaffoldMu.Unlock()
		if err != nil {
			h.d.Log.Warn("hosts: rescaffold", "repo", name, "err", err)
		}
	}
}

// rescaffoldRepo makes one attempt at landing repo's pending rec with
// cfg's row: in a bmesh worktree from levelled main, it writes the
// row-fed targets of the fields that changed (the verify kind's command,
// [setup]), commits them as bmesh and pushes, under the repo's lock. It
// returns the record to keep, or keep false when the row is no longer
// this box's ship row.
func (h *hostsNode) rescaffoldRepo(ctx context.Context, cfg *config.Config, repo string, rec scaffoldRec) (scaffoldRec, bool) {
	r, ok := cfg.Repos[repo]
	if !ok || r.Host != h.d.Host || r.Mode != "ship" {
		h.d.Log.Info("hosts: rescaffold dropped; the row is no longer this box's ship row", "repo", repo, "version", rec.Version)
		return rec, false
	}
	path := cfg.Expand(r.Path)
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		if rec.Told != "nocheckout" {
			h.scaffoldEvent(ctx, fmt.Sprintf("nocheckout v%d", rec.Version), fmt.Sprintf(
				"%s: registry v%d changes %s's scaffold but %s has no checkout; run bmesh scaffold %s where it has one",
				h.d.Host, rec.Version, repo, path, repo))
			rec.Told = "nocheckout"
		}
		return rec, true
	}
	verify := slices.Contains(rec.Fields, "verify")
	setup := slices.ContainsFunc(rec.Fields, func(f string) bool { return strings.HasPrefix(f, "setup") })
	var files []string
	unscaffolded, stale := false, false
	ctx, cancel := context.WithTimeout(ctx, rescaffoldTimeout)
	defer cancel()
	sha, err := gitwork.EditOnMain(ctx, gitwork.Edit{
		Repo: repo, RepoPath: path, What: "scaffold",
		Worktrees: cfg.Expand(cfg.Runner.Worktrees),
		Lock:      func() (func(), error) { return level.Lock(h.d.StateDir, repo) },
		Discard:   true,
		Warn:      func(err error) { h.d.Log.Warn("hosts: rescaffold", "repo", repo, "err", err) },
	}, func(wt string) (string, []string, error) {
		agents, err := os.ReadFile(filepath.Join(wt, scaffold.Agents))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", nil, err
		}
		if !strings.Contains(string(agents), "<!-- "+scaffold.MarkerPrefix) {
			unscaffolded = true
			return "", nil, nil
		}
		targets, err := scaffold.Follow(wt, r, verify, setup)
		if err != nil || len(targets) == 0 {
			return "", nil, err
		}
		if files, err = scaffold.Write(wt, targets); err != nil {
			return "", nil, err
		}
		stale = verify && !scaffold.DoneFollows(string(agents), r)
		msg := fmt.Sprintf("Scaffold: %s follows registry v%d\n\n%s", repo, rec.Version, rowFields(r, rec.Fields))
		return msg, files, nil
	})
	switch {
	case err != nil:
		body := fmt.Sprintf("failed v%d", rec.Version)
		if rec.Told == "failed" {
			body += " again"
		}
		h.scaffoldEvent(ctx, body, fmt.Sprintf("%s: scaffold %s for registry v%d failed: %s", h.d.Host, repo, rec.Version, oneLine(err.Error())))
		rec.Told = "failed"
	case unscaffolded:
		h.d.Log.Info("hosts: rescaffold skipped; AGENTS.md has no bmesh: marker", "repo", repo, "version", rec.Version)
		rec.Landed = true
	case sha == "":
		h.d.Log.Info("hosts: rescaffold: main already follows the row", "repo", repo, "version", rec.Version)
		rec.Landed = true
	default:
		text := fmt.Sprintf("%s: scaffolded %s for registry v%d at %s (%s)", h.d.Host, repo, rec.Version, sha[:8], strings.Join(files, ", "))
		if stale {
			text += fmt.Sprintf("; AGENTS.md's done block still names the old verify; run bmesh scaffold %s on a box with the current bmesh", repo)
		}
		h.scaffoldEvent(ctx, fmt.Sprintf("ok v%d", rec.Version), text)
		rec.Landed, rec.Commit = true, sha
	}
	return rec, true
}

// oneLine is s's non-empty lines, trimmed and joined with "; " (git's
// stderr, for an event's text).
func oneLine(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "; ")
}

// rowFields is the commit body: each changed field with its new value, as
// the row reads.
func rowFields(r config.Repo, fields []string) string {
	quote := func(ss []string) string {
		q := make([]string, len(ss))
		for i, s := range ss {
			q[i] = fmt.Sprintf("%q", s)
		}
		return "[" + strings.Join(q, ", ") + "]"
	}
	var lines []string
	for _, f := range fields {
		switch f {
		case "verify":
			lines = append(lines, "verify = "+quote(r.Verify))
		case "setup":
			lines = append(lines, fmt.Sprintf("setup = %q", r.Setup))
		case "setup_inputs":
			lines = append(lines, "setup_inputs = "+quote(r.SetupInputs))
		case "setup_timeout":
			lines = append(lines, fmt.Sprintf("setup_timeout = %q", r.SetupTimeout))
		}
	}
	return strings.Join(lines, "\n")
}

// scaffoldEvent publishes one rescaffold attempt as the daemon's scaffold
// event; body is `ok|failed|nocheckout v<N>`, with ` again` on a failure
// already pushed, which the push loop leaves quiet.
func (h *hostsNode) scaffoldEvent(ctx context.Context, body, text string) {
	if strings.HasPrefix(body, "ok") {
		h.d.Log.Info("hosts: " + text)
	} else {
		h.d.Log.Warn("hosts: " + text)
	}
	ev := mesh.Event{From: h.d.from(), Event: "scaffold", Text: text, Body: body, At: h.d.Now().UTC().Format(time.RFC3339)}
	if err := mesh.PublishEvent(context.WithoutCancel(ctx), h.d.Client, ev); err != nil {
		h.d.Log.Warn("hosts: publish scaffold", "err", err)
	}
}
