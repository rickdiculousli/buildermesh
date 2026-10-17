package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

// revision is this daemon's revision as published: the full commit, or
// unknown.
func (d *Deps) revision() string {
	if d.Revision == "" {
		return selfupdate.Unknown
	}
	return d.Revision
}

// announce runs once on start: when this host's newest update has no
// started after it and named another revision than this one, that update
// never came up, which it logs and pushes; then it publishes started with
// the revision, and its commit time as the body.
func announce(ctx context.Context, d Deps) {
	upd, okU, err := mesh.Last(ctx, d.Client, mesh.DaemonSubject(d.Host, "update"))
	if err != nil {
		d.Log.Warn("update: read last update", "err", err)
	}
	st, okS, err := mesh.Last(ctx, d.Client, mesh.DaemonSubject(d.Host, "started"))
	if err != nil {
		d.Log.Warn("update: read last started", "err", err)
	}
	if okU && (!okS || upd.Seq > st.Seq) {
		if ev, err := decodeEvent(upd.Data); err == nil && !selfupdate.Same(ev.Commit, d.revision()) {
			title := "bmesh update failed on " + d.Host
			body := fmt.Sprintf("update %s did not start; this is %s", ev.Text, selfupdate.Short(d.Revision))
			d.Log.Warn("update: failed", "update", ev.Text, "running", selfupdate.Short(d.Revision))
			if d.Cfg.Notify.Ntfy != "" || d.Cfg.Notify.Command != "" {
				if err := deliver(ctx, d, Headers{Title: title, Priority: "high", Tags: "warning"}, body); err != nil {
					d.Log.Warn("update: push failed update", "err", err)
				}
			}
		}
	}
	ev := mesh.Event{From: d.from(), Event: "started", Text: d.revision(), Body: d.RevTime, At: d.Now().UTC().Format(time.RFC3339)}
	if err := mesh.PublishEvent(ctx, d.Client, ev); err != nil {
		d.Log.Warn("started: publish", "err", err)
	}
}

// updateRepo is the registry name the routine loop updates this binary
// from, or "" when [update] is off or cannot run (logged once, here).
func updateRepo(d Deps) string {
	repo := d.Cfg.Update.Repo
	switch {
	case repo == "":
		return ""
	case d.Cfg.Repos[repo].Path == "":
		d.Log.Warn("update: [update] repo is not in the registry; off", "repo", repo)
	case d.Revision == "" && d.Tag == "":
		d.Log.Warn("update: this binary has no vcs revision; off", "repo", repo)
	case d.Exe == "" || d.Restart == nil:
		d.Log.Warn("update: no binary path to replace; off", "repo", repo)
	case d.GOOS == "windows" && !d.Supervised:
		d.Log.Warn("update: [update] repo needs bmesh serve --restart on Windows, which starts the new binary (init --service sets it up); off", "repo", repo)
	default:
		return repo
	}
	return ""
}

// installed is the commit, as dir knows it, of the binary at Exe: what its
// `version` names, so a `bmesh update` that already swapped in a newer one
// is never built over, else this daemon's own revision or release tag; a
// tag resolved to its commit there (a tagged module install). "" when none
// of them names one.
func (r *routineSet) installed(dir string) string {
	own := selfupdate.Installed(r.ctx, r.d.Exe)
	if own == "" {
		own = r.d.Revision
	}
	if own == "" {
		own = r.d.Tag
	}
	return selfupdate.OwnCommit(dir, own)
}

// codePaths is the code, as git pathspecs (selfupdate.CodePaths), of the
// row main is read for: [update] repo, else the row of this binary's own
// module, as bmesh update finds it, so both read one code head. A commit
// outside it is never a code head.
func (d *Deps) codePaths() []string {
	repo := cmp.Or(d.Cfg.Update.Repo, d.Cfg.ModuleRepo(d.Module))
	return selfupdate.CodePaths(d.Cfg.Repos[repo].Code, d.Cfg.Spec.Glob)
}

// target is what [update] track follows on ref in dir (selfupdate.Follow);
// a release track with no release tag yet follows the code head, which it
// logs once.
func (r *routineSet) target(dir, ref string) (selfupdate.Target, error) {
	t, err := selfupdate.Follow(dir, ref, r.d.Cfg.Update.Track, r.d.codePaths())
	if err == nil && t.NoTag && !r.noTagSaid {
		r.noTagSaid = true
		r.d.Log.Info("update: no release tag on main yet; following its code head", "head", selfupdate.Short(t.Commit))
	}
	return t, err
}

// checkUpdate compares the target [update] track follows in main's
// checkout dir (the newest release, or the code head) with the binary at
// Exe (installed). When the target has the revision in its history and is ahead
// it builds the target over the binary, publishes update, drains and asks
// serve to restart; it reports whether it did. A binary that is the target
// or has it in its history is current: it never goes back. Each target is
// acted on once: a failed build or a target without this revision (a
// rollback) is logged once and left.
func (r *routineSet) checkUpdate(dir string) bool {
	// Locked from reading main to the end of the build, so a `bmesh update`
	// on this box cannot move the checkout in between.
	lockRelease, err := selfupdate.Lock(dir)
	if err != nil {
		r.d.Log.Warn("update: lock main", "err", err)
		return false
	}
	var once sync.Once
	unlock := func() { once.Do(lockRelease) }
	defer unlock()
	t, err := r.target(dir, "HEAD")
	if err != nil {
		r.d.Log.Warn("update: read main", "err", err)
		return false
	}
	if selfupdate.Same(t.Commit, r.d.Revision) || t.Commit == r.updateSeen {
		return false
	}
	r.updateSeen = t.Commit
	own := r.installed(dir)
	old, next := selfupdate.Label(dir, own), selfupdate.Label(dir, t.Commit)
	switch selfupdate.Stand(dir, own, t.Commit) {
	case selfupdate.Current, selfupdate.Ahead:
		return false
	case selfupdate.Apart:
		if own == "" {
			r.d.Log.Warn("update: this binary's release is not on main; left alone", "main", next, "running", r.d.Tag)
		} else {
			r.d.Log.Warn("update: main does not contain this revision; left alone", "main", next, "running", old)
		}
		return false
	}
	var steps bool
	err = selfupdate.Install(r.ctx, dir, r.d.Exe, t.Commit, r.gate(dir, own, t, &steps))
	unlock()
	var refused *selfupdate.Refused
	var h *held
	switch {
	case errors.As(err, &refused):
		r.d.Log.Warn("update: "+err.Error(), "main", next)
		return false
	case errors.As(err, &h):
		r.hold(h)
		return false
	case err != nil:
		r.d.Log.Warn("update: build failed; keeping this binary", "main", next, "err", err)
		return false
	}
	r.swapped(dir, own, t, steps)
	r.restartInto(t.Commit, old+" -> "+next)
	return true
}

// restartInto publishes update, text `<old> -> <new>`, for the binary just
// swapped in at head, drains and asks serve to restart.
func (r *routineSet) restartInto(head, text string) {
	ctx, cancel := r.settleCtx()
	err := mesh.PublishEvent(ctx, r.d.Client, mesh.Event{From: r.d.from(), Event: "update", Text: text, Commit: head,
		At: r.d.Now().UTC().Format(time.RFC3339)})
	cancel()
	if err != nil {
		r.d.Log.Warn("update: publish", "err", err)
	}
	r.d.Log.Info("update: "+text, "exe", r.d.Exe, "prev", selfupdate.Prev(r.d.Exe))
	r.drain()
	r.d.Log.Info("update: drained; restarting")
	r.d.Restart()
}

// writeNotice writes the notice's line to the state dir, or removes it for
// "" (nothing without a state dir).
func (d *Deps) writeNotice(line string) {
	if d.StateDir == "" {
		return
	}
	if err := selfupdate.WriteNotice(d.StateDir, line); err != nil {
		d.Log.Warn("update: write notice", "err", err)
	}
}

// initCheck sets up the check of main the routine loop runs every [update]
// check_every when it isn't self-updating from a row: the notice, and with
// [update] upstream (no [update] repo) the install. Upstream that cannot run
// is logged once and leaves the notice; with no check, a notice left from
// before is removed.
func (r *routineSet) initCheck() {
	d, u := r.d, r.d.Cfg.Update
	if r.updateRepo != "" || u.CheckEvery <= 0 {
		if u.Upstream && r.updateRepo == "" {
			d.Log.Warn("update: [update] upstream needs check_every; off")
		}
		d.writeNotice("")
		return
	}
	if u.Repo == "" && d.Module == "" {
		d.Log.Info("update: this binary does not say its module, so no main to check; off")
		d.writeNotice("")
		return
	}
	r.checkEvery = u.CheckEvery
	if !u.Upstream || u.Repo != "" {
		return
	}
	switch {
	case d.GOOS == "windows" && !d.Supervised:
		d.Log.Warn("update: [update] upstream needs bmesh serve --restart on Windows, which starts the new binary (init --service sets it up); notice only")
	case d.Revision == "" && d.Tag == "":
		d.Log.Warn("update: this binary has no revision; notice only")
	case d.Exe == "" || d.Restart == nil:
		d.Log.Warn("update: no binary path to replace; notice only")
	default:
		r.upstream = d.Module
	}
}

// checkMain reads main, from rowDir ([update] repo's main checkout) when
// set, else from a fetch of the module's main and its tags into the bare
// repo under the cache dir, and compares the target [update] track follows
// there with the binary at Exe (this daemon's own revision, or release
// tag, when that says none, so a `bmesh update` that already swapped it in
// reads as current; a tag resolved to its commit there). When the binary
// is behind it writes the notice, or with upstream installs the target,
// publishes update, drains and asks serve to restart, reporting whether it
// did; a failed install is logged once per target and tried again on the
// next check. A current or newer binary removes the notice.
func (r *routineSet) checkMain(rowDir string) bool {
	d := r.d
	var (
		dir, ref string
		release  func()
		err      error
	)
	if rowDir != "" {
		dir, ref = rowDir, "HEAD"
		release, err = selfupdate.Lock(dir)
	} else {
		var cache string
		if cache, err = config.CacheDir(); err == nil {
			dir, ref = selfupdate.UpstreamDir(cache), "refs/heads/main"
			_, release, err = selfupdate.FetchUpstream(dir, d.Module)
		}
	}
	if err != nil {
		d.Log.Warn("update: read main", "err", err)
		return false
	}
	var once sync.Once
	unlock := func() { once.Do(release) }
	defer unlock()
	t, err := r.target(dir, ref)
	if err != nil {
		d.Log.Warn("update: read main", "err", err)
		return false
	}
	own := r.installed(dir)
	line, err := selfupdate.Notice(dir, own, ref, t, d.codePaths())
	if err != nil {
		d.Log.Warn("update: read main's history", "err", err)
		return false
	}
	if line != "" && r.upstream != "" && rowDir == "" {
		text := selfupdate.Label(dir, own) + " -> " + selfupdate.Label(dir, t.Commit)
		var steps bool
		err := selfupdate.InstallModule(r.ctx, dir, r.upstream, d.Exe, t, r.gate(dir, own, t, &steps))
		unlock()
		if err == nil {
			d.writeNotice("")
			r.swapped(dir, own, t, steps)
			r.restartInto(t.Commit, text)
			return true
		}
		var h *held
		if t.Commit != r.failedAt {
			r.failedAt = t.Commit
			if errors.As(err, &h) {
				r.hold(h)
				return false
			}
			d.Log.Warn("update: install failed; keeping this binary", "main", selfupdate.Label(dir, t.Commit), "err", err)
		} else if errors.As(err, &h) {
			return false // the notice it wrote stands
		}
	}
	d.writeNotice(line)
	return false
}

// drain stops every kind's feed, so no further job is taken, then ends the
// loop's ctx, killing every running job's process group, and waits until
// every job it held or ran has gone back to the inbox (NAK), for the
// restarted daemon to take at once.
func (r *routineSet) drain() {
	r.mu.Lock()
	for name, f := range r.feeds {
		f.stop()
		delete(r.feeds, name)
	}
	r.drained = true
	r.mu.Unlock()
	r.cancel()
	r.wg.Wait()
}
