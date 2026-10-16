package daemon

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

const (
	defaultHostsEvery = time.Minute     // the roster, presence and wake tick
	wakeGap           = 3 * time.Minute // a gap between ticks this long is a wake
	jobProgressEvery  = time.Minute     // InProgress while a job runs
)

// hostsNak is how long a job waits after a nak; tests shorten it.
var hostsNak = time.Minute

// wakeClock tells a wake from the gap between ticks on the wall clock:
// monotonic time stops while a box sleeps, so the reading is stripped of it.
type wakeClock struct{ last time.Time }

// observe records a tick at now and reports the gap since the last one and
// whether it is a wake (wakeGap or more).
func (w *wakeClock) observe(now time.Time) (gap time.Duration, woke bool) {
	now = now.Round(0)
	if !w.last.IsZero() {
		gap = now.Sub(w.last)
	}
	w.last = now
	return gap, gap >= wakeGap
}

// hostsNode is this box on the hosts layer: its host key, its newest valid
// roster and its salt records.
type hostsNode struct {
	d     Deps
	key   ed25519.PrivateKey
	salts *hosts.Salts

	mu    sync.Mutex
	local hosts.Local
	wake  wakeClock

	notesCut bool // notes.md was over 16 KB at the last read: logged once

	heldMu    sync.Mutex  // the held store
	levelling atomic.Bool // a self-level is running
	wg        sync.WaitGroup
}

// hostsRun runs the hosts layer until ctx ends: each minute it notes a
// wake, syncs the roster (taking a newer one, putting its own back when the
// hub's is missing, unsigned or lower), drops expired holds, writes signed
// presence and the session line (this-box.txt); it takes this box's jobs
// one at a time, answers approvals of its holds, and self-levels its repos
// on start and on each wake. A box that hasn't joined logs so and does
// nothing.
func hostsRun(ctx context.Context, d Deps) {
	key, err := hosts.LoadHostKey(hosts.HostKeyPath(d.ConfigDir))
	if err != nil {
		d.Log.Warn("hosts: enabled but no host key; run bmesh init --hosts", "err", err)
		return
	}
	local, ok, err := hosts.LoadLocal(d.StateDir)
	if err != nil || !ok {
		d.Log.Warn("hosts: enabled but this box hasn't joined; run bmesh init --hosts", "err", err)
		return
	}
	h := &hostsNode{d: d, key: key, local: local, salts: hosts.OpenSalts(d.StateDir)}
	if row, ok := local.Roster.Host(d.Host); !ok {
		d.Log.Warn("hosts: this box isn't in its roster", "host", d.Host, "roster", local.Roster.Version)
	} else if row.Key != hosts.KeyHex(key.Public().(ed25519.PublicKey)) {
		d.Log.Warn("hosts: the roster has another key for this box; ask the admin for a new invite", "host", d.Host)
	}
	tick := d.HostsEvery
	if tick <= 0 {
		tick = defaultHostsEvery
	}
	h.wg.Add(4)
	go func() { defer h.wg.Done(); h.jobLoop(ctx) }()
	go func() { defer h.wg.Done(); h.serveApprovals(ctx) }()
	go func() { defer h.wg.Done(); h.selfLevel(ctx) }()
	go func() { defer h.wg.Done(); every(ctx, tick, func() { h.tick(ctx) }) }()
	h.wg.Wait()
}

// tick is the minute's work: a wake check, the roster, old salt records and
// presence.
func (h *hostsNode) tick(ctx context.Context) {
	now := h.d.Now()
	h.mu.Lock()
	gap, woke := h.wake.observe(now)
	h.mu.Unlock()
	if woke {
		h.d.Log.Info("hosts: woke; re-reading the roster and self-levelling", "gap", gap.Round(time.Second))
	}
	if err := h.syncRoster(ctx); err != nil {
		h.d.Log.Warn("hosts: roster", "err", err)
	}
	if woke && ctx.Err() == nil {
		h.wg.Add(1)
		go func() { defer h.wg.Done(); h.selfLevel(ctx) }()
	}
	if err := h.salts.Prune(now); err != nil {
		h.d.Log.Warn("hosts: salts", "err", err)
	}
	h.pruneHolds(now)
	if err := h.putPresence(ctx, now); err != nil {
		h.d.Log.Warn("hosts: presence", "err", err)
	}
	if err := h.writeHere(ctx, now); err != nil {
		h.d.Log.Warn("hosts: this-box.txt", "err", err)
	}
}

// writeHere writes the session line, <state dir>/this-box.txt, from the
// roster and every box's presence.
func (h *hostsNode) writeHere(ctx context.Context, now time.Time) error {
	kv, err := hosts.Bucket(ctx, h.d.Client.JS, hosts.BucketHosts)
	if err != nil {
		return err
	}
	r := h.roster()
	seen, err := hosts.ReadPresence(ctx, kv, r, now)
	if err != nil {
		return err
	}
	return hosts.WriteHere(h.d.StateDir, hosts.HereLine(h.d.Host, r, seen, now))
}

// syncRoster reads the hub's roster key (kv.Get), taking a newer valid
// roster and putting this box's back when the hub's is missing, unsigned
// or lower.
func (h *hostsNode) syncRoster(ctx context.Context) error {
	kv, err := hosts.Bucket(ctx, h.d.Client.JS, hosts.BucketRoster)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	took, err := hosts.Sync(ctx, kv, h.d.StateDir, &h.local)
	if took {
		h.d.Log.Info("hosts: roster", "version", h.local.Roster.Version)
	}
	return err
}

func (h *hostsNode) roster() hosts.Roster {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.local.Roster
}

func (h *hostsNode) revision() string { return selfupdate.Short(h.d.Revision) }

// putPresence writes this box's signed presence: revision, OS, whether it
// runs routines, its registered repos with their origins and its notes
// (<config dir>/notes.md, cut to 16 KB).
func (h *hostsNode) putPresence(ctx context.Context, now time.Time) error {
	kv, err := hosts.Bucket(ctx, h.d.Client.JS, hosts.BucketHosts)
	if err != nil {
		return err
	}
	p := hosts.Presence{Host: h.d.Host, Rev: h.revision(), OS: runtime.GOOS, Routines: h.d.Checkouts != "",
		Repos: []hosts.PresenceRepo{}, TS: hosts.Stamp(now)}
	for _, name := range h.d.Cfg.RepoNames() {
		_, path, err := h.d.Cfg.RepoPath(name)
		if err != nil {
			continue
		}
		p.Repos = append(p.Repos, hosts.PresenceRepo{Name: name, Path: path, Origin: origin(ctx, path, h.d.Cfg.Repos[name].Remote)})
	}
	notes, cut, err := hosts.ReadNotes(h.d.ConfigDir)
	if err != nil {
		h.d.Log.Warn("hosts: notes", "err", err)
	}
	if cut && !h.notesCut {
		h.d.Log.Warn("hosts: notes.md is over 16 KB; the rest is cut", "path", hosts.NotesPath(h.d.ConfigDir))
	}
	h.notesCut = cut
	p.Notes = notes
	p.Sign(h.key)
	return hosts.PutPresence(ctx, kv, p)
}

// origin is the checkout's origin URL, else the row's remote.
func origin(ctx context.Context, path, remote string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", path, "remote", "get-url", "origin").Output()
	if err != nil {
		return remote
	}
	return strings.TrimSpace(string(out))
}

// jobLoop takes this box's jobs from inbox-host--<host>, one at a time,
// re-making the consumer after a failure.
func (h *hostsNode) jobLoop(ctx context.Context) {
	for ctx.Err() == nil {
		err := h.consume(ctx)
		if ctx.Err() != nil {
			return
		}
		h.d.Log.Warn("hosts: jobs", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(followRetry):
		}
	}
}

func (h *hostsNode) consume(ctx context.Context) error {
	cons, err := hosts.HostInbox(ctx, h.d.Client.JS, h.d.Host)
	if err != nil {
		return err
	}
	it, err := cons.Messages(jetstream.PullMaxMessages(1))
	if err != nil {
		return err
	}
	defer it.Stop()
	stop := context.AfterFunc(ctx, it.Stop)
	defer stop()
	for {
		m, err := it.Next()
		if err != nil {
			return err
		}
		h.handle(ctx, m)
	}
}

// handle runs one job through the checks: the roster is read first (a hub
// it can't reach naks), then checks 1 to 7, then the salt.
func (h *hostsNode) handle(ctx context.Context, m jetstream.Msg) {
	var j hosts.Job
	if err := json.Unmarshal(m.Data(), &j); err != nil {
		h.refused(ctx, &j, hosts.CheckSignature)
		_ = m.Ack()
		return
	}
	if err := h.syncRoster(ctx); err != nil {
		h.d.Log.Warn("hosts: can't read the roster; job waits", "job", j.ID, "err", err)
		_ = m.NakWithDelay(hostsNak)
		return
	}
	r := h.roster()
	now := h.d.Now()
	v := hosts.Check(&j, &r, h.d.Host, now)
	if v.Action == hosts.Run {
		var err error
		if v, err = h.salts.Admit(&j); err != nil {
			h.d.Log.Warn("hosts: salts", "job", j.ID, "err", err)
			_ = m.NakWithDelay(hostsNak)
			return
		}
	}
	switch v.Action {
	case hosts.Nak:
		h.d.Log.Info("hosts: job waits", "job", j.ID, "by", j.By, "check", v.Check)
		_ = m.NakWithDelay(hostsNak)
		return
	case hosts.Refuse, hosts.Expired:
		h.answer(ctx, &j, false, "refused: "+v.Check)
		if hosts.Pushed(v.Check) {
			h.refused(ctx, &j, v.Check)
		} else {
			h.d.Log.Info("hosts: refused", "job", j.ID, "by", j.By, "op", j.Op, "check", v.Check)
		}
	case hosts.Replay:
		// The job ran: a redelivery (the ack was lost to a crash or a
		// dropped connection) or a replay of it gets the stored reply and
		// runs nothing, so it is logged, not pushed. A salt another job
		// used is refused above as a replay.
		h.d.Log.Info("hosts: ran already; the stored reply again", "job", j.ID, "by", j.By, "op", j.Op)
		if v.Record.Reply != nil {
			if err := hosts.SendReply(ctx, h.d.Client.JS, *v.Record.Reply); err != nil {
				h.d.Log.Warn("hosts: reply", "job", j.ID, "err", err)
			}
		}
	case hosts.Interrupted:
		h.finish(ctx, &j, false, "interrupted")
	case hosts.Run, hosts.Resume:
		ok, text := h.run(ctx, m, &j)
		h.finish(ctx, &j, ok, text)
	}
	_ = m.Ack()
}

// run runs the job's op, telling the hub it is in progress each minute.
func (h *hostsNode) run(ctx context.Context, m jetstream.Msg, j *hosts.Job) (bool, string) {
	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(jobProgressEvery)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				_ = m.InProgress()
			}
		}
	}()
	switch j.Op {
	case "ping":
		return true, fmt.Sprintf("%s, roster v%d", h.revision(), h.roster().Version)
	case "level":
		return h.levelJob(ctx, j)
	}
	return false, fmt.Sprintf("unknown op %s (this bmesh is %s)", cut(j.Op, 32), h.revision())
}

// answer sends a signed reply to j's sender and returns it.
func (h *hostsNode) answer(ctx context.Context, j *hosts.Job, ok bool, text string) hosts.Reply {
	r := hosts.NewReply(h.key, j, h.d.Host, ok, text, h.d.Now())
	if err := hosts.SendReply(ctx, h.d.Client.JS, r); err != nil {
		h.d.Log.Warn("hosts: reply", "job", j.ID, "err", err)
	}
	return r
}

// finish answers j and records its salt done with the reply, so a replay
// gets the same answer and never runs again.
func (h *hostsNode) finish(ctx context.Context, j *hosts.Job, ok bool, text string) {
	r := h.answer(ctx, j, ok, text)
	if err := h.salts.Put(j.Salt, hosts.SaltRecord{State: hosts.SaltDone, JobID: j.ID, Op: j.Op, Expires: j.Expires, Reply: &r}); err != nil {
		h.d.Log.Warn("hosts: salts", "job", j.ID, "err", err)
	}
}

// refused publishes bm.daemon.<host>.refused for the phone: Body is
// "<sender> <check>", which the push loop rate-limits by.
func (h *hostsNode) refused(ctx context.Context, j *hosts.Job, check string) {
	by := j.By
	if !hosts.ValidHostName(by) {
		by = "?"
	}
	h.d.Log.Warn("hosts: refused", "by", by, "check", check, "job", cut(j.ID, 32), "op", cut(j.Op, 32))
	ev := mesh.Event{From: h.d.from(), Event: "refused", Body: by + " " + check,
		Text: fmt.Sprintf("refused %s from %s: %s job %s", check, by, cut(j.Op, 32), cut(j.ID, 32)),
		At:   h.d.Now().UTC().Format(time.RFC3339)}
	if err := mesh.PublishEvent(ctx, h.d.Client, ev); err != nil {
		h.d.Log.Warn("hosts: publish refused", "err", err)
	}
}

// cut is s cut to n bytes, for text a sender chose.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return cutUTF8(s, n) + "…"
}
