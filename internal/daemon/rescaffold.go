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

// scaffoldRec is one repo's line in scaffolded.json: the registry version
// that last changed its row's scaffold fields, the fields changed since
// the last landing, and whether it landed. Told is set once a failure or
// a missing checkout for Version was pushed.
type scaffoldRec struct {
	Version int64    `json:"version"`
	Fields  []string `json:"fields"`
	Landed  bool     `json:"landed"`
	Commit  string   `json:"commit,omitempty"`
	Told    bool     `json:"told,omitempty"`
}

func scaffoldedPath(stateDir string) string { return filepath.Join(stateDir, scaffoldedFile) }

func loadScaffolded(stateDir string) (map[string]scaffoldRec, error) {
	b, err := os.ReadFile(scaffoldedPath(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]scaffoldRec{}, nil
	}
	if err != nil {
		return nil, err
	}
	recs := map[string]scaffoldRec{}
	if err := json.Unmarshal(b, &recs); err != nil {
		return nil, fmt.Errorf("%s: %v", scaffoldedFile, err)
	}
	return recs, nil
}

func saveScaffolded(stateDir string, recs map[string]scaffoldRec) error {
	b, err := json.MarshalIndent(recs, "", "  ")
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
func changedScaffold(old, new config.Repo) []string {
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

// noteScaffold records, for registry version v, each ship row this box
// hosts whose scaffold fields differ between old and rows, as pending in
// scaffolded.json: a newer version replaces a pending one (the fields
// add up until one lands). A row new in rows is not a change: its first
// scaffold is the operator's.
func (h *hostsNode) noteScaffold(v int64, old, rows map[string]config.Repo) {
	type change struct {
		repo   string
		fields []string
	}
	var changes []change
	for name, r := range rows {
		o, ok := old[name]
		if !ok || r.Host != h.d.Host || r.Mode != "ship" {
			continue
		}
		if f := changedScaffold(o, r); len(f) > 0 {
			changes = append(changes, change{name, f})
		}
	}
	if len(changes) == 0 {
		return
	}
	h.scaffoldMu.Lock()
	defer h.scaffoldMu.Unlock()
	recs, err := loadScaffolded(h.d.StateDir)
	if err != nil {
		h.d.Log.Warn("hosts: rescaffold", "err", err)
		recs = map[string]scaffoldRec{}
	}
	for _, c := range changes {
		rec := recs[c.repo]
		fields := c.fields
		if !rec.Landed && rec.Version > 0 {
			fields = unionFields(rec.Fields, fields)
		}
		recs[c.repo] = scaffoldRec{Version: v, Fields: fields}
		h.d.Log.Info("hosts: registry changes a repo's scaffold", "repo", c.repo, "version", v, "fields", strings.Join(fields, ","))
	}
	if err := saveScaffolded(h.d.StateDir, recs); err != nil {
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
	recs, err := loadScaffolded(h.d.StateDir)
	h.scaffoldMu.Unlock()
	if err != nil {
		h.d.Log.Warn("hosts: rescaffold", "err", err)
		return
	}
	names := make([]string, 0, len(recs))
	for name, rec := range recs {
		if !rec.Landed {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if ctx.Err() != nil {
			return
		}
		was := recs[name]
		rec, keep := h.rescaffoldRepo(ctx, name, was)
		h.scaffoldMu.Lock()
		now, err := loadScaffolded(h.d.StateDir)
		if err == nil && now[name].Version == was.Version && slices.Equal(now[name].Fields, was.Fields) {
			if keep {
				now[name] = rec
			} else {
				delete(now, name)
			}
			err = saveScaffolded(h.d.StateDir, now)
		}
		h.scaffoldMu.Unlock()
		if err != nil {
			h.d.Log.Warn("hosts: rescaffold", "repo", name, "err", err)
		}
	}
}

// rescaffoldRepo makes one attempt at landing repo's pending rec: in a
// bmesh worktree from levelled main, it writes the row-fed targets of the
// fields that changed (the verify kind's command, [setup]), commits them
// as bmesh and pushes, under the repo's lock. It returns the record to
// keep, or keep false when the row is no longer this box's ship row.
func (h *hostsNode) rescaffoldRepo(ctx context.Context, repo string, rec scaffoldRec) (scaffoldRec, bool) {
	cfg := h.conf()
	r, ok := cfg.Repos[repo]
	if !ok || r.Host != h.d.Host || r.Mode != "ship" {
		h.d.Log.Info("hosts: rescaffold dropped; the row is no longer this box's ship row", "repo", repo, "version", rec.Version)
		return rec, false
	}
	path := cfg.Expand(r.Path)
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		if !rec.Told {
			h.scaffoldEvent(ctx, fmt.Sprintf("nocheckout v%d", rec.Version), fmt.Sprintf(
				"%s: registry v%d changes %s's scaffold but %s has no checkout; run bmesh scaffold %s where it has one",
				h.d.Host, rec.Version, repo, path, repo))
			rec.Told = true
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
		if rec.Told {
			body += " again"
		}
		h.scaffoldEvent(ctx, body, fmt.Sprintf("%s: scaffold %s for registry v%d failed: %s", h.d.Host, repo, rec.Version, oneLine(err.Error())))
		rec.Told = true
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
