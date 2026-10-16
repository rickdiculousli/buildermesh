package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/level"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// approveTimeout bounds one approval's level, fetch included.
const approveTimeout = 5 * time.Minute

// checkoutOf is repo's checkout on this box: its row, and its path when
// the path exists.
func (h *hostsNode) checkoutOf(repo string) (path string, ok bool) {
	if _, ok := h.d.Cfg.Repos[repo]; !ok {
		return "", false
	}
	_, path, err := h.d.Cfg.RepoPath(repo)
	if err != nil {
		return "", false
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) || err != nil {
		return "", false
	}
	return path, true
}

// secure: this box holds for the operator, by its [hosts] secure or its
// roster row.
func (h *hostsNode) secure() bool {
	if h.d.Cfg.Hosts.Secure {
		return true
	}
	r := h.roster()
	row, ok := r.Host(h.d.Host)
	return ok && row.Secure
}

// held reports whether a move of repo waits for the operator: on a secure
// box, a repo sessions load (a plugin marketplace, or the [update] repo).
func (h *hostsNode) held(ctx context.Context, repo, path string) bool {
	return h.secure() && (repo == h.d.Cfg.Update.Repo || level.LoadsPlugins(ctx, path))
}

// request is a level of repo at path, as this box's config says.
func (h *hostsNode) request(ctx context.Context, repo, path string) level.Request {
	row := h.d.Cfg.Repos[repo]
	return level.Request{Repo: repo, Path: path, Mode: row.Mode, Pin: row.Pin, StateDir: h.d.StateDir,
		PushModes: h.d.Cfg.Hosts.PushModes, Hold: h.held(ctx, repo, path), Now: h.d.Now}
}

// levelJob runs a received level: the box judges its own checkout against
// origin, may push, and holds a move of a repo sessions load. A refusal
// or failure the caller no longer waits for is published for the phone.
func (h *hostsNode) levelJob(ctx context.Context, j *hosts.Job) (bool, string) {
	path, ok := h.checkoutOf(j.Repo)
	if !ok {
		return false, fmt.Sprintf("refused: no checkout of %s here", cut(j.Repo, 64))
	}
	sha, branch, until, err := level.ParseJobArgs(j.Args)
	if err != nil {
		return false, "refused: " + err.Error()
	}
	req := h.request(ctx, j.Repo, path)
	req.Sha, req.Branch, req.Push = sha, branch, true
	o := level.Level(ctx, req)
	h.d.Log.Info("hosts: level", "repo", j.Repo, "by", j.By, "result", o.Text())
	if o.Result == level.Held {
		h.hold(ctx, j, false, o, j.Repo, path)
		return true, o.Text()
	}
	if !o.OK() && unwaited(until, h.d.Now()) {
		h.publishLevel(ctx, j.Repo, j.By, o.Text())
	}
	return o.OK(), o.Text()
}

// unwaited: the caller stopped waiting (until passed, or never said).
func unwaited(until string, now time.Time) bool {
	t, err := hosts.ParseStamp(until)
	return err != nil || now.After(t)
}

// selfLevel fast-forwards every registered repo this box has, never
// pushing; refusals are logged. A move of a repo sessions load on a
// secure box is held as a received one is. One runs at a time.
func (h *hostsNode) selfLevel(ctx context.Context) {
	if !h.levelling.CompareAndSwap(false, true) {
		return
	}
	defer h.levelling.Store(false)
	n := 0
	defer func() { h.d.Log.Info("hosts: self-level done", "repos", n) }()
	for _, repo := range h.d.Cfg.RepoNames() {
		if ctx.Err() != nil {
			return
		}
		path, ok := h.checkoutOf(repo)
		if !ok {
			continue
		}
		n++
		o := level.Level(ctx, h.request(ctx, repo, path))
		switch {
		case o.Result == level.Held:
			if h.heldFor(repo) {
				continue // the hold there stands for it
			}
			j := hosts.NewJob(h.key, h.d.Host, h.d.Host, "level", repo, level.JobArgs(o.Target, o.Branch, ""),
				h.roster().Version, h.d.Now(), hosts.MaxLifetime)
			h.hold(ctx, &j, true, o, repo, path)
		case !o.OK():
			h.d.Log.Info("hosts: self-level", "repo", repo, "result", o.Text())
		case o.Result != level.UpToDate:
			h.d.Log.Info("hosts: self-level", "repo", repo, "result", o.Text())
		}
	}
}

// heldFor reports whether repo has a hold.
func (h *hostsNode) heldFor(repo string) bool {
	h.heldMu.Lock()
	defer h.heldMu.Unlock()
	all, _ := hosts.ListHeld(h.d.StateDir)
	for _, x := range all {
		if x.Repo == repo {
			return true
		}
	}
	return false
}

// hold keeps j's level for the operator in <state dir>/held and publishes
// bm.daemon.<host>.held. A repo has one hold: a received job's replaces
// an older one (whose caller hears it was superseded).
func (h *hostsNode) hold(ctx context.Context, j *hosts.Job, self bool, o level.Outcome, repo, path string) {
	h.heldMu.Lock()
	defer h.heldMu.Unlock()
	x := hosts.Held{ID: j.ID, Job: *j, Self: self, Repo: repo, Path: path, Branch: o.Branch, Head: o.Head,
		Target: o.Target, Commits: o.N, Log: o.Log, Created: hosts.Stamp(h.d.Now())}
	all, _ := hosts.ListHeld(h.d.StateDir)
	for _, old := range all {
		if old.Repo != repo || old.ID == x.ID {
			continue
		}
		if err := hosts.DropHeld(h.d.StateDir, old.ID); err != nil {
			h.d.Log.Warn("hosts: drop hold", "id", old.ID, "err", err)
			continue
		}
		if !old.Self {
			h.answer(ctx, &old.Job, false, "hold superseded by job "+x.ID)
		}
	}
	if err := hosts.SaveHeld(h.d.StateDir, x); err != nil {
		h.d.Log.Warn("hosts: hold", "repo", repo, "err", err)
		return
	}
	text := fmt.Sprintf("%s holds: %s +%d %s (%s)", h.d.Host, repo, o.N, plural(o.N, "commit"), x.From())
	h.d.Log.Info("hosts: "+text, "id", x.ID)
	ev := mesh.Event{From: h.d.from(), Event: "held", Text: text, Body: x.ID, At: h.d.Now().UTC().Format(time.RFC3339)}
	if err := mesh.PublishEvent(ctx, h.d.Client, ev); err != nil {
		h.d.Log.Warn("hosts: publish held", "err", err)
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// publishLevel publishes bm.daemon.<host>.level for a received level's
// refusal or failure no caller waits for, which the push loop pushes once.
func (h *hostsNode) publishLevel(ctx context.Context, repo, by, text string) {
	ev := mesh.Event{From: h.d.from(), Event: "level", Text: fmt.Sprintf("%s: %s %s (job from %s)", h.d.Host, repo, text, by),
		Body: repo, At: h.d.Now().UTC().Format(time.RFC3339)}
	if err := mesh.PublishEvent(ctx, h.d.Client, ev); err != nil {
		h.d.Log.Warn("hosts: publish level", "err", err)
	}
}

// pruneHolds drops the holds whose job has expired.
func (h *hostsNode) pruneHolds(now time.Time) {
	h.heldMu.Lock()
	defer h.heldMu.Unlock()
	all, err := hosts.ListHeld(h.d.StateDir)
	if err != nil {
		h.d.Log.Warn("hosts: holds", "err", err)
		return
	}
	for _, x := range all {
		if x.Expired(now) {
			h.d.Log.Info("hosts: hold expired", "id", x.ID, "repo", x.Repo, "from", x.From())
			_ = hosts.DropHeld(h.d.StateDir, x.ID)
		}
	}
}

// serveApprovals answers approvals on bmhosts.approve.<host> until ctx
// ends.
func (h *hostsNode) serveApprovals(ctx context.Context) {
	sub, err := h.d.Client.NC.Subscribe(hosts.ApproveSubject(h.d.Host), func(m *nats.Msg) {
		var a hosts.Approve
		ans := hosts.ApproveAnswer{Text: "approval: not an approve/v1 document"}
		if json.Unmarshal(m.Data, &a) == nil {
			actx, cancel := context.WithTimeout(ctx, approveTimeout)
			ans = h.approve(actx, a)
			cancel()
		}
		b, _ := json.Marshal(ans)
		_ = m.Respond(b)
	})
	if err != nil {
		h.d.Log.Warn("hosts: approvals", "err", err)
		return
	}
	<-ctx.Done()
	_ = sub.Unsubscribe()
}

// approve checks an approval of a hold and levels it: the approval is
// signed by this box's approve key (its roster row's) over the hold's job
// and the target the operator saw; the job's signature still verifies
// against its sender, who is still in the roster; and origin's target
// hasn't moved (else the hold takes the new target and log, and the
// answer carries it for the operator to approve again).
func (h *hostsNode) approve(ctx context.Context, a hosts.Approve) hosts.ApproveAnswer {
	no := func(format string, args ...any) hosts.ApproveAnswer {
		return hosts.ApproveAnswer{Text: fmt.Sprintf(format, args...)}
	}
	h.heldMu.Lock()
	defer h.heldMu.Unlock()
	x, ok, err := hosts.LoadHeld(h.d.StateDir, a.JobID)
	if err != nil {
		return no("hold %s: %v", cut(a.JobID, 32), err)
	}
	if !ok {
		return no("no hold %s", cut(a.JobID, 32))
	}
	now := h.d.Now()
	if x.Expired(now) {
		_ = hosts.DropHeld(h.d.StateDir, x.ID)
		return no("hold %s expired", x.ID)
	}
	if err := h.syncRoster(ctx); err != nil {
		h.d.Log.Warn("hosts: roster", "err", err)
	}
	r := h.roster()
	me, ok := r.Host(h.d.Host)
	if !ok || me.ApproveKey == "" {
		return no("refused: %s has no approve key in roster v%d", h.d.Host, r.Version)
	}
	if a.By != h.d.Host || a.Verify(me.ApproveKey) != nil {
		return no("refused: the approval isn't signed by %s's approve key", h.d.Host)
	}
	if a.JobID != x.ID || a.JobSalt != x.Job.Salt || a.JobSHA256 != x.Job.Digest() {
		return no("refused: the approval names another job")
	}
	if a.Decision != x.Decision() {
		return no("refused: the approval is for %q; the hold is at %s", cut(a.Decision, 80), x.Target)
	}
	if ts, err := hosts.ParseStamp(a.TS); err != nil || ts.Sub(now).Abs() > hosts.MaxClockAhead {
		return no("refused: the approval's ts %s is not within %s of this box's clock", cut(a.TS, 32), hosts.MaxClockAhead)
	}
	sender, ok := r.Host(x.Job.By)
	if !ok {
		_ = hosts.DropHeld(h.d.StateDir, x.ID)
		return no("refused: %s is no longer in the roster; hold dropped", x.Job.By)
	}
	if x.Job.Verify(sender.Key) != nil || x.Job.To != h.d.Host {
		_ = hosts.DropHeld(h.d.StateDir, x.ID)
		return no("refused: the held job's signature doesn't verify; hold dropped")
	}
	sha, branch, _, err := level.ParseJobArgs(x.Job.Args)
	if err != nil {
		_ = hosts.DropHeld(h.d.StateDir, x.ID)
		return no("refused: the held job's args: %v; hold dropped", err)
	}
	req := h.request(ctx, x.Repo, x.Path)
	req.Sha, req.Branch, req.Push, req.Hold, req.Expect = sha, branch, !x.Self, false, x.Target
	o := level.Level(ctx, req)
	if o.Result == level.TargetMoved {
		x.Head, x.Target, x.Commits, x.Log = o.Head, o.Target, o.N, o.Log
		if err := hosts.SaveHeld(h.d.StateDir, x); err != nil {
			return no("hold %s: %v", x.ID, err)
		}
		return hosts.ApproveAnswer{Text: "the target moved to " + o.Target, Moved: &x}
	}
	_ = hosts.DropHeld(h.d.StateDir, x.ID)
	text := o.Text() + " (approved)"
	h.d.Log.Info("hosts: approved", "id", x.ID, "repo", x.Repo, "result", text)
	if !x.Self {
		h.answer(ctx, &x.Job, o.OK(), text)
	}
	return hosts.ApproveAnswer{OK: o.OK(), Text: text}
}
