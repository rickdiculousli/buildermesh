package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// conf is the config the hosts layer reads rows from: the daemon's, with
// the rows of the newest registry it took since it started.
func (h *hostsNode) conf() *config.Config {
	c, _ := h.confVersion()
	return c
}

// syncRegistry takes a newer registry from the hub (writing repos.toml,
// keeping repos.toml.prev) and puts this box's back when the hub's is
// missing, unsigned or lower. A registry this binary can't read is left
// as it is, and pushed once per version. Then it starts the rescaffold of
// any repo a registry changed that hasn't landed yet.
func (h *hostsNode) syncRegistry(ctx context.Context) error {
	if h.d.Cfg.Path() == "" {
		return nil // no config file, so nowhere for repos.toml
	}
	h.regMu.Lock()
	defer h.regMu.Unlock()
	kv, err := hosts.Bucket(ctx, h.d.Client.JS, hosts.BucketRoster)
	if err != nil {
		return err
	}
	h.mu.Lock()
	pinned := h.local.Pinned
	h.mu.Unlock()
	res, err := hosts.SyncRegistry(ctx, kv, h.d.StateDir, pinned, h.applyRegistry)
	if res.Took > 0 {
		h.d.Log.Info("hosts: registry", "version", res.Took)
	} else if reg, ok, lerr := hosts.LoadRegistry(h.d.StateDir, pinned); lerr == nil && ok && reg.Version > h.version() {
		// Saved by another process, which wrote repos.toml with it.
		if rows, perr := config.ParseRepos(reg.Rows); perr == nil {
			h.useRows(reg.Version, rows)
			h.d.Log.Info("hosts: registry", "version", reg.Version)
		}
	}
	if res.Refused > 0 && res.Refused != h.regRefused {
		h.regRefused = res.Refused
		text := fmt.Sprintf("%s: refused: %v", h.d.Host, res.RefuseErr)
		h.d.Log.Warn("hosts: "+text, "version", res.Refused)
		detail := fmt.Sprintf("%s. %s keeps the rows it has until its bmesh can read v%d. In a terminal on %s: bmesh update.",
			text, h.d.Host, res.Refused, h.d.Host)
		ev := mesh.Event{From: h.d.from(), Event: "registry", Text: text, Detail: detail, Body: fmt.Sprint(res.Refused),
			At: h.d.Now().UTC().Format(time.RFC3339)}
		if err := mesh.PublishEvent(ctx, h.d.Client, ev); err != nil {
			h.d.Log.Warn("hosts: publish registry", "err", err)
		}
	}
	h.goRescaffold(ctx)
	return err
}

// applyRegistry reads r's rows with this binary's loader and writes them
// as repos.toml; from then on the hosts layer reads those rows.
func (h *hostsNode) applyRegistry(r hosts.Registry) error {
	rows, err := config.ParseRepos(r.Rows)
	if err != nil {
		return fmt.Errorf("registry v%d needs a newer bmesh (%v)", r.Version, err)
	}
	if err := config.WriteReposText(config.ReposPath(h.d.Cfg.Path()), r.Rows); err != nil {
		return fmt.Errorf("registry v%d: %v", r.Version, err)
	}
	h.useRows(r.Version, rows)
	return nil
}

// useRows makes rows, from registry version v, the ones the hosts layer
// reads, noting each repo of this box's whose scaffold fields they change
// (noteScaffold).
func (h *hostsNode) useRows(v int64, rows map[string]config.Repo) {
	h.noteScaffold(v, h.conf().Repos, rows)
	c := *h.d.Cfg
	c.Repos = rows
	h.mu.Lock()
	h.cfg, h.cfgVersion = &c, v
	h.mu.Unlock()
}

// confVersion is conf and version read together.
func (h *hostsNode) confVersion() (*config.Config, int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg != nil {
		return h.cfg, h.cfgVersion
	}
	return h.d.Cfg, h.cfgVersion
}

// version is the registry version the rows are from (0: repos.toml's at
// start, with no registry).
func (h *hostsNode) version() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfgVersion
}

// proposeJob holds a registry proposal for the operator on the admin box:
// the rows must load here and be proposed against the registry this box
// has. The hold carries the row diff; `bmesh hosts approve` publishes it
// as the next version, and nothing changes until then.
func (h *hostsNode) proposeJob(ctx context.Context, j *hosts.Job) (bool, string) {
	r := h.roster()
	if admin, ok := r.Admin(); !ok || admin.Name != h.d.Host || !h.d.Cfg.Hosts.Admin {
		return false, fmt.Sprintf("refused: %s is not the admin box", h.d.Host)
	}
	base, text, err := hosts.ParseProposeArgs(j.Args)
	if err != nil {
		return false, "refused: " + err.Error()
	}
	rows, err := config.ParseRepos(text)
	if err != nil {
		return false, fmt.Sprintf("refused: the proposed rows don't load here: %v", err)
	}
	cur, _, err := hosts.LoadRegistry(h.d.StateDir, r.AdminKey)
	if err != nil {
		return false, "failed: " + err.Error()
	}
	if base != cur.Version {
		return false, fmt.Sprintf("refused: proposed against registry v%d; it is v%d now: propose again", base, cur.Version)
	}
	old := map[string]config.Repo{}
	if cur.Version > 0 {
		if old, err = config.ParseRepos(cur.Rows); err != nil {
			return false, "failed: registry v" + fmt.Sprint(cur.Version) + ": " + err.Error()
		}
	}
	if text == cur.Rows {
		return true, fmt.Sprintf("no change: registry v%d has these rows", cur.Version)
	}
	diff := config.DiffRepos(old, rows)
	log := strings.Join(diff, "\n")
	if len(diff) == 0 {
		log = "(comments and layout only)"
	}
	x := hosts.Held{ID: j.ID, Kind: hosts.HeldRegistry, Base: base, Job: *j, Repo: "", Commits: len(diff), Log: log,
		Created: hosts.Stamp(h.d.Now())}
	h.heldMu.Lock()
	defer h.heldMu.Unlock()
	all, _ := hosts.ListHeld(h.d.StateDir)
	for _, old := range all {
		if old.Kind != hosts.HeldRegistry || old.Job.By != j.By || old.ID == x.ID {
			continue
		}
		if err := hosts.DropHeld(h.d.StateDir, old.ID); err != nil {
			h.d.Log.Warn("hosts: drop hold", "id", old.ID, "err", err)
			continue
		}
		h.answer(ctx, &old.Job, false, "proposal superseded by job "+x.ID)
	}
	if err := hosts.SaveHeld(h.d.StateDir, x); err != nil {
		return false, "failed: " + err.Error()
	}
	h.d.Log.Info(fmt.Sprintf("hosts: %s holds: registry proposal from %s, %d row %s", h.d.Host, j.By, len(diff),
		plural(len(diff), "change")), "id", x.ID)
	h.publishHeld(ctx, x, "Approving publishes its rows as the next registry, which every box takes.")
	return true, fmt.Sprintf("held for approval as %s (%d row %s against registry v%d)", x.ID, len(diff),
		plural(len(diff), "change"), base)
}
