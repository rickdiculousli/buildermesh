package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

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
// the revision.
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
		if ev, err := decodeEvent(upd.Data); err == nil && ev.Commit != d.revision() {
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
	ev := mesh.Event{From: d.from(), Event: "started", Text: d.revision(), At: d.Now().UTC().Format(time.RFC3339)}
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
	case d.Revision == "":
		d.Log.Warn("update: this binary has no vcs revision; off", "repo", repo)
	case d.Exe == "" || d.Restart == nil:
		d.Log.Warn("update: no binary path to replace; off", "repo", repo)
	default:
		return repo
	}
	return ""
}

// checkUpdate compares main's checkout dir with this binary's revision.
// When main contains the revision and is ahead it builds main over the
// binary, publishes update, drains and asks serve to restart; it reports
// whether it did. Each main commit is acted on once: a failed build or a
// main without this revision (a rollback) is logged once and left.
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
	head, err := selfupdate.Head(dir)
	if err != nil {
		r.d.Log.Warn("update: read main", "err", err)
		return false
	}
	if head == r.d.Revision || head == r.updateSeen {
		return false
	}
	r.updateSeen = head
	old, next := selfupdate.Short(r.d.Revision), selfupdate.Short(head)
	if !selfupdate.Contains(dir, r.d.Revision, head) {
		r.d.Log.Warn("update: main does not contain this revision; left alone", "main", next, "running", old)
		return false
	}
	err = selfupdate.Install(r.ctx, dir, r.d.Exe)
	unlock()
	if err != nil {
		r.d.Log.Warn("update: build failed; keeping this binary", "main", next, "err", err)
		return false
	}
	text := old + " -> " + next
	ctx, cancel := r.settleCtx()
	err = mesh.PublishEvent(ctx, r.d.Client, mesh.Event{From: r.d.from(), Event: "update", Text: text, Commit: head,
		At: r.d.Now().UTC().Format(time.RFC3339)})
	cancel()
	if err != nil {
		r.d.Log.Warn("update: publish", "err", err)
	}
	r.d.Log.Info("update: "+text, "exe", r.d.Exe, "prev", selfupdate.Prev(r.d.Exe))
	r.drain()
	r.d.Log.Info("update: drained; restarting")
	r.d.Restart()
	return true
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
