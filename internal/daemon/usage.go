package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/spec"
	"github.com/rickdiculousli/buildermesh/internal/usage"
)

// usageEvery is how often the usage loop reads what grew in this box's
// transcripts (105), whatever [routines] says.
const usageEvery = 30 * time.Second

// baselineEvery is how often the flag check reads the stream's 30 days
// again for the baselines (108).
const baselineEvery = time.Hour

// usageOnce runs one usage pass and then the flag check on this box's
// nodes; a failure is logged and the next tick starts from the cursor.
func usageOnce(ctx context.Context, d Deps, f *flagger) {
	repos := map[string]string{}
	for _, name := range d.Cfg.RepoNames() {
		if _, p, err := d.Cfg.RepoPath(name); err == nil {
			repos[name] = p
		}
	}
	err := usage.Pass(ctx, usage.Env{Client: d.Client, Host: d.Host, Claude: d.ClaudeDir, StateDir: d.StateDir,
		Checkouts: d.Checkouts, Repos: repos, Log: d.Log, Now: d.Now})
	if err != nil && ctx.Err() == nil {
		d.Log.Warn("usage: pass", "err", err)
	}
	if err := f.check(ctx, d); err != nil && ctx.Err() == nil && !errors.Is(err, usage.ErrNoStream) {
		d.Log.Warn("usage: flags", "err", err)
	}
}

// flagger is the flag check's memory between passes: the baselines, read
// every baselineEvery, and the spec sizes read with them.
type flagger struct {
	at    time.Time
	base  usage.Baselines
	sizes map[string]map[int]string // repo → spec id → size, from main
}

// announced is <state dir>/usage/flags.json: per node, the "<rule>
// <level>" pairs already published, so a restart announces nothing twice.
type announced map[string][]string

func flagsPath(d Deps) string { return filepath.Join(d.StateDir, "usage", "flags.json") }

func (f *flagger) sizeOf(n mesh.Node) string {
	id, err := strconv.Atoi(n.Spec)
	if err != nil {
		return ""
	}
	return f.sizes[n.Repo][id]
}

// readSizes reads every configured repo's spec sizes from its local main.
func (f *flagger) readSizes(ctx context.Context, d Deps) {
	f.sizes = map[string]map[int]string{}
	for _, name := range d.Cfg.RepoNames() {
		_, p, err := d.Cfg.RepoPath(name)
		if err != nil {
			continue
		}
		specs, err := spec.ScanMainLocal(ctx, p, d.Cfg.Spec.Glob)
		if err != nil {
			continue // a repo with no ledger has no sizes: its leads raise no size flag
		}
		m := map[int]string{}
		for _, s := range specs {
			m[s.ID] = s.Fields["size"]
		}
		f.sizes[name] = m
	}
}

// check judges this box's nodes that are not done by the rules
// (usage.Flags) and publishes bm.daemon.<host>.runaway once per node,
// rule and level: to the node, text "<level> <path>: <words>".
func (f *flagger) check(ctx context.Context, d Deps) error {
	if d.Client == nil {
		return nil
	}
	now := d.Now()
	if f.at.IsZero() || now.Sub(f.at) >= baselineEvery {
		f.readSizes(ctx, d)
		br, err := usage.LoadBaselines(ctx, d.Client, now, f.sizeOf)
		if err != nil {
			return err
		}
		f.at, f.base = now, br.Baselines
	}
	nodes, err := mesh.ListNodes(ctx, d.Client)
	if err != nil {
		return err
	}
	states, err := mesh.ReadAllStates(ctx, d.Client)
	if err != nil {
		return err
	}
	var live []mesh.Node
	oldest := now
	for _, n := range nodes {
		if n.Host != d.Host || states[n.Path].State == "done" {
			continue
		}
		live = append(live, n)
		if t := startOf(n, now); t.Before(oldest) {
			oldest = t
		}
	}
	ann := announced{}
	if b, err := os.ReadFile(flagsPath(d)); err == nil {
		_ = json.Unmarshal(b, &ann) // an unreadable file announces again rather than never
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	keep, changed := announced{}, false
	for _, n := range live {
		keep[n.Path] = ann[n.Path]
	}
	for path := range ann {
		if _, ok := keep[path]; !ok {
			changed = true // a node done or gone: its flags are forgotten
		}
	}
	if len(live) > 0 {
		hist, err := mesh.History(ctx, d.Client, "bm.daemon.*.land-refused", oldest)
		if err != nil {
			return err
		}
		refused := usage.Refusals(hist)
		for _, n := range live {
			rep, err := usage.Read(ctx, d.Client, startOf(n, now), usage.Filter{Agent: n.Path})
			if err != nil {
				return err
			}
			if len(rep.Agents) == 0 {
				continue
			}
			s := usage.SubjectOf(rep.Agents[0], rep.Calls, f.sizeOf(n), refused[n.Path], now)
			s.Role, s.Repo = n.Role, n.Repo
			for _, fl := range usage.Flags(s, f.base, d.Cfg.Context.CompactSoon) {
				key := fl.Rule + " " + fl.Level
				if slices.Contains(keep[n.Path], key) {
					continue
				}
				ev := mesh.Event{From: d.from(), Event: "runaway", To: n.Path,
					Text: fl.Level + " " + n.Path + ": " + fl.Words, At: now.UTC().Format(time.RFC3339)}
				if _, err := mesh.PublishEventID(ctx, d.Client, ev, d.Host+"/runaway/"+n.Path+"/"+fl.Rule+"/"+fl.Level); err != nil {
					return err
				}
				keep[n.Path] = append(keep[n.Path], key)
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	return writeAnnounced(flagsPath(d), keep)
}

// startOf is when n's use is read from: its started_at, else usage.Keep ago.
func startOf(n mesh.Node, now time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339, n.StartedAt); err == nil && t.After(now.Add(-usage.Keep)) {
		return t
	}
	return now.Add(-usage.Keep)
}

// writeAnnounced writes a through a temp file and a rename.
func writeAnnounced(path string, a announced) error {
	for k, v := range a {
		if len(v) == 0 {
			delete(a, k)
		}
	}
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".flags-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
