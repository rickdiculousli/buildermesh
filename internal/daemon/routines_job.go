package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/instance"
	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// settleTimeout bounds the writes made after a job (and after the loop's
// ctx has ended): replies, acks, records, states and events.
const settleTimeout = 10 * time.Second

// delivery is a fetched job until it is settled. While unsettled it is kept
// in progress every AckWait/3, so a job held in a batch or running past the
// kind's timeout is not redelivered. A loop that stops hands it back with a
// NAK (a running job's process group killed first), so the next daemon takes
// it at once; only a daemon that dies leaves it to be redelivered after
// AckWait.
type delivery struct {
	msg  jetstream.Msg
	job  mesh.Job
	from mesh.Path
	seq  uint64 // the job's stream sequence; 0 when unknown
	f    *feed
	r    *routineSet
	stop chan struct{}
	once sync.Once

	held   bool // counted in f.held until started or settled
	unheld sync.Once
	noted  atomic.Bool // in the kind's bm-jobs entry until settled
}

// hold counts dl as waiting in a pool; call before handing it over.
func (dl *delivery) hold() {
	dl.held = true
	dl.f.held.Add(1)
}

// start ends dl's wait in a pool.
func (dl *delivery) start() {
	if dl.held {
		dl.unheld.Do(func() { dl.f.held.Add(-1) })
	}
}

func (r *routineSet) newDelivery(f *feed, m jetstream.Msg, j mesh.Job, from mesh.Path) *delivery {
	dl := &delivery{msg: m, job: j, from: from, f: f, r: r, stop: make(chan struct{})}
	if meta, err := m.Metadata(); err == nil {
		dl.seq = meta.Sequence.Stream
	}
	f.inflight.Add(1)
	every := max(time.Duration(f.ackWait.Load())/3, 100*time.Millisecond)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-dl.stop:
				return
			case <-r.ctx.Done():
				return
			case <-t.C:
				if err := m.InProgress(); err != nil {
					r.d.Log.Warn("routines: in progress", "job", j.ID, "err", err)
				}
				if dl.noted.Load() { // the kind's bm-jobs entry stays fresh, so status believes it
					r.writeHolds(f.name, func(map[uint64]mesh.Hold) {})
				}
			}
		}
	}()
	return dl
}

func (dl *delivery) settle(how string, do func() error) {
	dl.start()
	dl.once.Do(func() {
		close(dl.stop)
		dl.f.inflight.Add(-1)
		if err := do(); err != nil {
			dl.r.d.Log.Warn("routines: "+how, "job", dl.job.ID, "err", err)
		}
		if dl.noted.Load() {
			dl.r.writeHolds(dl.f.name, func(m map[uint64]mesh.Hold) { delete(m, dl.seq) })
		}
	})
}

// note records dl in its kind's bm-jobs entry as held or running by holder.
func (dl *delivery) note(holder, state string) {
	if dl.seq == 0 {
		return
	}
	dl.noted.Store(true)
	h := mesh.Hold{Seq: dl.seq, Job: dl.job.ID, From: dl.from.String(), Holder: holder, State: state,
		Since: dl.r.d.Now().UTC().Format(time.RFC3339)}
	dl.r.writeHolds(dl.f.name, func(m map[uint64]mesh.Hold) { m[dl.seq] = h })
}

// writeHolds applies change to this daemon's unsettled jobs of kindName
// and writes them to the kind's bm-jobs entry. Writes are serialized, so
// the entry ends as the last change left it.
func (r *routineSet) writeHolds(kindName string, change func(map[uint64]mesh.Hold)) {
	r.holdMu.Lock()
	defer r.holdMu.Unlock()
	m := r.holds[kindName]
	if m == nil {
		m = map[uint64]mesh.Hold{}
		r.holds[kindName] = m
	}
	change(m)
	jobs := make([]mesh.Hold, 0, len(m))
	for _, h := range m {
		jobs = append(jobs, h)
	}
	slices.SortFunc(jobs, func(a, b mesh.Hold) int { return cmp.Compare(a.Seq, b.Seq) })
	ctx, cancel := r.settleCtx()
	defer cancel()
	h := mesh.Holds{Kind: kindName, Host: r.d.Host, At: r.d.Now().UTC().Format(time.RFC3339), Jobs: jobs}
	if err := mesh.PutHolds(ctx, r.d.Client, h); err != nil {
		r.d.Log.Warn("routines: write holds", "kind", kindName, "err", err)
	}
}

// ack removes the job for good.
func (dl *delivery) ack() {
	dl.settle("ack", func() error {
		ctx, cancel := context.WithTimeout(context.Background(), settleTimeout)
		defer cancel()
		return dl.msg.DoubleAck(ctx)
	})
}

// nak returns the job to the inbox at once; nakDelay after d.
func (dl *delivery) nak() { dl.settle("nak", dl.msg.Nak) }

func (dl *delivery) nakDelay(d time.Duration) {
	dl.settle("nak", func() error { return dl.msg.NakWithDelay(d) })
}

// settleCtx is a context for writes that must happen even as the loop stops.
func (r *routineSet) settleCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.ctx), settleTimeout)
}

// reply sends rep, filled with the job's id, kind and tree, to the requester.
func (r *routineSet) reply(dl *delivery, rep mesh.Reply) {
	rep.Job, rep.Kind = dl.job.ID, dl.f.name
	if rep.Tree == "" {
		rep.Tree = dl.job.Tree
	}
	from, err := mesh.ParsePath(rep.By)
	if err != nil {
		from = mesh.RoutinePath(dl.f.name)
	}
	ctx, cancel := r.settleCtx()
	defer cancel()
	if err := mesh.SendReply(ctx, r.d.Client, from, dl.from, rep); err != nil {
		r.d.Log.Warn("routines: reply", "job", dl.job.ID, "to", dl.from.String(), "err", err)
	}
}

// worker is one instance path running jobs: a pooled instance across its
// jobs, or a fresh run for one job.
type worker struct {
	r     *routineSet
	g     *group
	path  string
	model string
	dir   string

	session    *instance.Model // model kinds: the warm session
	sessionDef kind.Kind       // the definition the session was started from
}

// instanceFor returns the instance to run a job whose commit defines kc: a
// new command each time; for a model kind the warm session, replaced when
// kc differs from the definition it was started with.
func (w *worker) instanceFor(kc kind.Kind) instance.Instance {
	if !kc.IsModel() {
		return instance.NewCommand(kc)
	}
	if w.session == nil || !reflect.DeepEqual(w.sessionDef, kc) {
		if w.session != nil {
			w.event("progress", "recycled", "")
		}
		w.session, w.sessionDef = instance.NewModel(kc, w.model, w.r.claude), kc
	}
	return w.session
}

func (w *worker) setState(state string) {
	ctx, cancel := w.r.settleCtx()
	defer cancel()
	if _, err := mesh.WriteState(ctx, w.r.d.Client, w.path, mesh.State{State: state, By: "daemon"}); err != nil {
		w.r.d.Log.Warn("routines: state", "path", w.path, "state", state, "err", err)
	}
}

// idle resets the state between jobs (R2: no transition leads to idle).
func (w *worker) idle() {
	ctx, cancel := w.r.settleCtx()
	defer cancel()
	if err := mesh.ResetState(ctx, w.r.d.Client, w.path); err != nil {
		w.r.d.Log.Warn("routines: reset state", "path", w.path, "err", err)
	}
}

// event publishes an event of the instance; to names the requester of the
// job it is about ("" for none), whose tree it keeps alive.
func (w *worker) event(event, text, to string) {
	ctx, cancel := w.r.settleCtx()
	defer cancel()
	if err := mesh.PublishEvent(ctx, w.r.d.Client, mesh.Event{From: w.path, Event: event, Text: text, To: to}); err != nil {
		w.r.d.Log.Warn("routines: event", "path", w.path, "event", event, "err", err)
	}
}

// begin registers the instance node, announces it and leaves it idle.
func (w *worker) begin() {
	ctx, cancel := w.r.settleCtx()
	defer cancel()
	n := mesh.Node{Path: w.path, Role: "routine", Repo: w.g.repo, RepoPath: w.g.repoPath, Host: w.r.d.Host,
		Model: w.model, StartedAt: w.r.d.Now().UTC().Format(time.RFC3339)}
	if err := mesh.PutNode(ctx, w.r.d.Client, n); err != nil {
		w.r.d.Log.Warn("routines: put node", "path", w.path, "err", err)
	}
	w.idle() // a path restarted after done starts over
	w.setState("working")
	w.event("started", "", "")
	w.idle()
}

// end marks the instance done (through working: idle cannot reach done).
func (w *worker) end() {
	w.setState("working")
	w.setState("done")
	w.event("done", "", "")
}

// handle runs one job to its end: reply, record and ack, or a NAK when its
// slots are busy, or when the loop stops (mid-run, its process group
// killed), so the next daemon runs it at once.
func (w *worker) handle(dl *delivery) {
	dl.start()
	r, j := w.r, dl.job
	ctx := r.ctx
	fail := func(text string) {
		if ctx.Err() != nil { // the step failed because the loop is stopping
			dl.nak()
			return
		}
		r.reply(dl, mesh.Reply{Output: text, By: w.path})
		dl.ack()
	}
	// The tree is the daemon's own reading of the commit, not the
	// requester's. A commit the daemon's clone lacks (queued from another
	// box, pushed since) is fetched once before the job is refused.
	tree, err := gitTree(w.g.repoPath, j.Commit)
	if err != nil {
		if ferr := r.fetch(w.g.repo, w.g.repoPath); ferr != nil {
			r.d.Log.Warn("routines: fetch", "repo", w.g.repoPath, "err", ferr)
		}
		tree, err = gitTree(w.g.repoPath, j.Commit)
	}
	if err != nil {
		fail(err.Error())
		return
	}
	kc, err := kind.LoadAt(w.g.repoPath, j.Commit, w.g.k.Name)
	switch {
	case errors.Is(err, kind.ErrNotFound):
		kc = w.g.k
	case err != nil:
		fail(err.Error())
		return
	}
	// The commit may move a kind main runs anywhere to another OS: answered,
	// not NAKed, since a NAK would keep holding one of the kind's places.
	if !kc.RunsOn(r.d.GOOS) {
		fail(fmt.Sprintf("kind %s at %.8s runs on %s; this box is %s", kc.Name, j.Commit, kc.OS, r.d.GOOS))
		return
	}
	if kc.IsModel() && !slices.Contains(kc.ModelsList(), w.model) || !kc.IsModel() && w.model != "" {
		fail("unknown model " + w.model)
		return
	}
	key := mesh.ResultKey(kc.Name, tree, j.Args, w.model)
	if kc.Deterministic && !j.Rerun {
		rec, ok, err := mesh.GetResult(ctx, r.d.Client, key)
		if err != nil {
			r.d.Log.Warn("routines: get result", "key", key, "err", err)
		}
		if ok {
			r.reply(dl, mesh.Reply{Tree: rec.Tree, OK: rec.OK, Output: rec.Output, ResultKey: key, By: rec.By})
			dl.ack()
			return
		}
	}
	if err := checkout.MoveTo(w.dir, j.Commit); err != nil {
		fail(err.Error())
		return
	}
	ttl := max(kc.Timeout+ackWaitExtra, 3*time.Second)
	keys, err := w.acquire(kc, ttl)
	if errors.Is(err, mesh.ErrBusy) {
		r.d.Log.Info("routines: slot busy; job returned", "path", w.path, "job", j.ID)
		dl.nakDelay(busyNakDelay)
		return
	}
	if err != nil {
		fail(err.Error())
		return
	}

	// A model reads the diff the text's base names; its patch-id lets the
	// verdict carry to a tree a rebase made from the same diff.
	patch := ""
	if w.model != "" {
		pctx, cancel := r.settleCtx()
		if patch, err = checkout.ReviewedPatch(pctx, w.g.repoPath, j.Text, j.Commit); err != nil {
			r.d.Log.Warn("routines: patch-id", "job", j.ID, "err", err)
		}
		cancel()
	}

	dl.note(w.path, "running")
	w.setState("working")
	w.event("progress", fmt.Sprintf("job %s started", j.ID), dl.from.String())
	start := time.Now()
	ij := instance.Job{ID: j.ID, Kind: kc.Name, Commit: j.Commit, Tree: tree, From: w.path, Reply: dl.from.String(),
		Args: j.Args, Text: j.Text, Dir: w.dir}
	rep, lost := w.run(w.instanceFor(kc), ij, keys, ttl)
	held := slices.DeleteFunc(slices.Clone(keys), func(k string) bool { return k == lost })
	w.release(held)       // never the lost one: it may be someone else's now
	if ctx.Err() != nil { // killed by the stop: handed back, not answered
		dl.nak()
		return
	}
	ok := rep.OK && lost == ""
	if lost != "" {
		fail("lease lost")
	} else {
		pctx, cancel := r.settleCtx()
		rec := mesh.Result{Kind: kc.Name, Tree: tree, Commit: j.Commit, Args: j.Args, OK: rep.OK,
			Output: mesh.Truncate(rep.Output), By: w.path, Model: w.model, PatchID: patch}
		if err := mesh.PutResult(pctx, r.d.Client, rec); err != nil {
			r.d.Log.Warn("routines: put result", "key", key, "err", err)
			key = ""
		}
		cancel()
		r.reply(dl, mesh.Reply{Tree: tree, OK: rep.OK, Output: rep.Output, ResultKey: key, By: w.path})
		dl.ack()
	}
	verdict := "ok"
	if !ok {
		verdict = "fail"
	}
	// A model instance adds its context tokens, so the map can read them
	// from the stream: "job <id> ok|fail <ms> ctx <tokens>".
	text := fmt.Sprintf("job %s %s %d", j.ID, verdict, time.Since(start).Milliseconds())
	if kc.IsModel() {
		text += fmt.Sprintf(" ctx %d", rep.Context)
	}
	w.event("progress", text, dl.from.String())
	w.idle()
	if w.g.k.Mode != "fresh" && w.session != nil && w.session.ShouldRecycle(r.d.Now()) { // a fresh run's session ends with it
		w.session.Recycle()
		w.event("progress", "recycled", "")
	}
}

// acquire takes a place in each of kc's slots for this instance, capacity
// from the checkout's repo config; on failure it releases what it took.
func (w *worker) acquire(kc kind.Kind, ttl time.Duration) ([]string, error) {
	if len(kc.Slots) == 0 {
		return nil, nil
	}
	rc, err := kind.LoadRepoConfig(w.dir)
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, slot := range kc.Slots {
		key, err := mesh.AcquireLease(w.r.ctx, w.r.d.Client, slot, w.path, ttl, max(rc.Slots[slot], 1), w.r.d.Now())
		if err != nil {
			w.release(keys)
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func (w *worker) release(keys []string) {
	ctx, cancel := w.r.settleCtx()
	defer cancel()
	for _, key := range keys {
		if err := mesh.ReleaseLease(ctx, w.r.d.Client, key, w.path); err != nil {
			w.r.d.Log.Warn("routines: release lease", "key", key, "path", w.path, "err", err)
		}
	}
}

// run runs j on inst, renewing keys every ttl/3. A failed renew kills the
// run; lost names that key.
func (w *worker) run(inst instance.Instance, j instance.Job, keys []string, ttl time.Duration) (rep instance.Reply, lost string) {
	runCtx, kill := context.WithCancel(w.r.ctx)
	defer kill()
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		if len(keys) == 0 {
			return
		}
		t := time.NewTicker(ttl / 3)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-runCtx.Done():
				return
			case <-t.C:
				for _, key := range keys {
					if err := mesh.RenewLease(runCtx, w.r.d.Client, key, w.path, ttl, w.r.d.Now()); err != nil {
						if runCtx.Err() != nil {
							return
						}
						w.r.d.Log.Warn("routines: lease lost", "key", key, "path", w.path, "err", err)
						lost = key
						kill()
						return
					}
				}
			}
		}
	}()
	rep = inst.Run(runCtx, j)
	close(stop)
	<-done
	return rep, lost
}

// gitTree resolves commit's tree in repo.
func gitTree(repo, commit string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", repo, "rev-parse", "--verify", "--end-of-options", commit+"^{tree}").Output()
	if err != nil {
		return "", fmt.Errorf("commit %q: not a commit in %s", commit, repo)
	}
	return strings.TrimSpace(string(out)), nil
}
