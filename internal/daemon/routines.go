package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

// Tunables; tests shorten them.
var (
	kindRescan   = time.Minute            // how often each repo's main is read for kinds
	ackWaitExtra = time.Minute            // a kind consumer's AckWait (and lease ttl) beyond the kind's timeout
	busyNakDelay = 30 * time.Second       // a job whose slot is busy is redelivered after this
	fetchWait    = time.Second            // the longest one fetch blocks, so a stop is seen
	drainWait    = 500 * time.Millisecond // after a batch, how long an instance waits for a further queued job
)

// routineSet runs every routine kind of every repo in [repos]. Each kind has
// one feed: its inbox consumer and a dispatcher that fetches the kind's
// messages and routes each job to the group of the job's repo, which runs it
// on a pooled instance of the job's model or on a fresh run of its own.
type routineSet struct {
	d      Deps
	ctx    context.Context // the loop's: running jobs and heartbeats end with it
	root   string
	claude string
	wg     sync.WaitGroup

	mu    sync.Mutex // guards feeds
	feeds map[string]*feed

	pathMu sync.Mutex // guards paths
	paths  map[string]chan struct{}

	// worktreeMu serializes adding and removing checkouts: concurrent `git
	// worktree add` and `prune` in one repo read each other's half-written
	// metadata.
	worktreeMu sync.Mutex

	fetchMu sync.Mutex // serializes fetches, which lock a repo's refs

	updateRepo string // [update] repo when the update can run, else ""
	updateSeen string // the main commit the update last acted on
	drained    bool   // an update drained the feeds; guarded by mu
}

// fetch updates repoPath's origin refs (nothing without an origin).
func (r *routineSet) fetch(repoPath string) error {
	r.fetchMu.Lock()
	defer r.fetchMu.Unlock()
	return checkout.Fetch(repoPath)
}

func (r *routineSet) ensureCheckout(repoPath, instance string) (string, error) {
	r.worktreeMu.Lock()
	defer r.worktreeMu.Unlock()
	return checkout.Ensure(repoPath, r.root, instance)
}

func (r *routineSet) removeCheckout(repoPath, dir string) error {
	r.worktreeMu.Lock()
	defer r.worktreeMu.Unlock()
	return checkout.Remove(repoPath, dir)
}

// feed is one kind's consumer, dispatcher and per-repo groups.
type feed struct {
	name     string
	cons     jetstream.Consumer
	ctx      context.Context // ends when the feed stops
	cancel   context.CancelFunc
	maxAck   int
	ackWait  atomic.Int64 // time.Duration
	inflight atomic.Int64 // jobs fetched and not yet settled
	held     atomic.Int64 // jobs handed to a pool and not yet started

	mu     sync.Mutex // guards groups and sends to their pools
	groups map[string]*group
}

// group is one repo's definition of a kind (as on its main) and the pools
// that run it. Stopping a group (ctx) lets each instance finish its job.
type group struct {
	repo, repoPath string
	k              kind.Kind
	sig            string
	ctx            context.Context
	cancel         context.CancelFunc
	pools          map[string]*pool // pooled kinds, by model ("" for a command)
}

// pool is the instances of one (repo, kind, model): they take jobs from jobs
// and are told to start a batch on gos.
type pool struct {
	model string
	paths []string
	jobs  chan *delivery
	gos   []chan struct{}
}

// routines runs the routine loop until ctx ends: on start and every
// kindRescan it reads each repo's kinds from its main and starts, restarts
// or stops the kinds' feeds, pools and instances to match.
func routines(ctx context.Context, d Deps) {
	d.defaults()
	if d.Checkouts == "" {
		d.Log.Warn("routines: no checkout root; routines are off")
		if d.Cfg.Update.Repo != "" {
			d.Log.Warn("update: no checkout root to build from; off", "repo", d.Cfg.Update.Repo)
		}
		return
	}
	r := &routineSet{d: d, ctx: ctx, root: d.Checkouts, claude: d.ClaudeBin,
		feeds: map[string]*feed{}, paths: map[string]chan struct{}{}, updateRepo: updateRepo(d)}
	if strings.TrimSpace(r.claude) == "" {
		r.claude = "claude"
	}
	every(ctx, kindRescan, r.rescan)
	r.mu.Lock()
	for name, f := range r.feeds {
		f.stop()
		delete(r.feeds, name)
	}
	r.mu.Unlock()
	r.wg.Wait()
}

// discover returns the kinds on repoPath's main: origin/main after a fetch
// when the repo has an origin, else the local main (R4). dir is the main
// checkout once it is at that commit, else "".
func (r *routineSet) discover(repoPath string) (dir string, kinds []kind.Kind, err error) {
	if err := r.fetch(repoPath); err != nil {
		r.d.Log.Warn("routines: fetch", "repo", repoPath, "err", err)
	}
	dir, err = r.ensureCheckout(repoPath, "main")
	if err != nil {
		return "", nil, err
	}
	ref := "main"
	if exec.Command("git", "-C", repoPath, "remote", "get-url", "origin").Run() == nil {
		ref = "origin/main"
	}
	// Under the lock a `bmesh update` on this box holds while it builds
	// from the same checkout.
	unlock, err := selfupdate.Lock(dir)
	if err != nil {
		return "", nil, err
	}
	err = checkout.MoveTo(dir, ref)
	unlock()
	if err != nil {
		return "", nil, err
	}
	kinds, err = kind.LoadDir(dir)
	return dir, kinds, err
}

// rescan reads every repo's kinds and brings the feeds in line. A repo that
// cannot be read keeps what it had. With [update] on, the update repo's main
// is then compared with this binary; an update drains the feeds for good.
func (r *routineSet) rescan() {
	if r.isDrained() {
		return
	}
	updateDir := ""
	names := make([]string, 0, len(r.d.Cfg.Repos))
	for name := range r.d.Cfg.Repos {
		names = append(names, name)
	}
	sort.Strings(names)
	want := map[string]map[string]kind.Kind{} // kind → repo → definition
	repoPaths := map[string]string{}
	failed := map[string]bool{}
	for _, name := range names {
		if r.ctx.Err() != nil {
			return
		}
		_, path, err := r.d.Cfg.RepoPath(name)
		if err != nil {
			r.d.Log.Warn("routines: repo", "repo", name, "err", err)
			continue
		}
		repoPaths[name] = path
		dir, kinds, err := r.discover(path)
		if name == r.updateRepo {
			updateDir = dir
		}
		if err != nil {
			r.d.Log.Warn("routines: read kinds", "repo", name, "err", err)
			failed[name] = true
			continue
		}
		for _, k := range kinds {
			if want[k.Name] == nil {
				want[k.Name] = map[string]kind.Kind{}
			}
			want[k.Name][name] = k
		}
	}

	if updateDir != "" && r.checkUpdate(updateDir) {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx.Err() != nil || r.drained {
		return
	}
	for name, f := range r.feeds {
		f.mu.Lock()
		for repo, g := range f.groups {
			if failed[repo] {
				if want[name] == nil {
					want[name] = map[string]kind.Kind{}
				}
				want[name][repo] = g.k
			}
		}
		f.mu.Unlock()
		if want[name] == nil {
			f.stop()
			delete(r.feeds, name)
		}
	}
	kinds := make([]string, 0, len(want))
	for name := range want {
		kinds = append(kinds, name)
	}
	sort.Strings(kinds)
	for _, name := range kinds {
		r.applyKind(name, want[name], repoPaths)
	}
	r.sweep()
}

// sweep forgets this host's routine instances that have been done for
// [status] keep_done: a pooled instance whose pool stopped, or a fresh one
// whose node outlived its run. A path an instance runs as is skipped, and
// held while it is forgotten, so an instance starting on it waits.
func (r *routineSet) sweep() {
	keep := r.d.Cfg.Status.KeepDone
	if keep <= 0 {
		return
	}
	nodes, err := mesh.ListNodes(r.ctx, r.d.Client)
	if err != nil {
		r.d.Log.Warn("routines: sweep", "err", err)
		return
	}
	cutoff := r.d.Now().Add(-keep)
	for _, n := range nodes {
		if n.Role != "routine" || n.Host != r.d.Host {
			continue
		}
		release := r.tryClaimPath(n.Path)
		if release == nil {
			continue
		}
		s, _, err := mesh.ReadState(r.ctx, r.d.Client, n.Path)
		if err == nil && doneBefore(s, cutoff) {
			err = mesh.ForgetNode(r.ctx, r.d.Client, n.Path)
		}
		release()
		if err != nil {
			r.d.Log.Warn("routines: sweep", "path", n.Path, "err", err)
		}
	}
}

// doneBefore reports a state that is done and was written before cutoff.
func doneBefore(s mesh.State, cutoff time.Time) bool {
	at, err := time.Parse(time.RFC3339, s.At)
	return s.State == "done" && err == nil && at.Before(cutoff)
}

func (r *routineSet) isDrained() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.drained
}

// modelsOf is the models a kind's pools run: its models, or "" for a command.
func modelsOf(k kind.Kind) []string {
	if k.IsModel() {
		return k.ModelsList()
	}
	return []string{""}
}

// shortOf names a model in instance paths: its ModelShort, cmd for a command.
func shortOf(model string) string {
	if model == "" {
		return "cmd"
	}
	return mesh.ModelShort(model)
}

// consumerSettings: MaxAckPending is the kind's instance count over every
// repo (pool × models for pooled, pool for fresh: R5); AckWait the longest
// timeout plus ackWaitExtra.
func consumerSettings(defs map[string]kind.Kind) (int, time.Duration) {
	maxAck, longest := 0, time.Duration(0)
	for _, k := range defs {
		switch k.Mode {
		case "pooled":
			maxAck += k.Pool * len(modelsOf(k))
		case "fresh":
			maxAck += k.Pool
		}
		longest = max(longest, k.Timeout)
	}
	return max(maxAck, 1), max(longest+ackWaitExtra, time.Second)
}

// applyKind starts the kind's feed when new, updates its consumer when its
// settings changed, and starts or restarts each repo's group whose
// definition or instance paths changed. Called with r.mu held.
func (r *routineSet) applyKind(name string, defs map[string]kind.Kind, repoPaths map[string]string) {
	f := r.feeds[name]
	maxAck, ackWait := consumerSettings(defs)
	if f == nil || f.maxAck != maxAck || time.Duration(f.ackWait.Load()) != ackWait {
		cons, err := mesh.KindConsumer(r.ctx, r.d.Client, name, maxAck, ackWait)
		if err != nil {
			r.d.Log.Warn("routines: consumer", "kind", name, "err", err)
			if f == nil {
				return
			}
		} else if f == nil {
			f = r.startFeed(name, cons)
			r.feeds[name] = f
		}
		if err == nil {
			f.maxAck = maxAck
			f.ackWait.Store(int64(ackWait))
		}
	}

	repos := make([]string, 0, len(defs))
	for repo := range defs {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	f.mu.Lock()
	defer f.mu.Unlock()
	numbered := map[string]int{} // model short → instances numbered so far
	for _, repo := range repos {
		k := defs[repo]
		repoPath := repoPaths[repo]
		if old := f.groups[repo]; old != nil && repoPath == "" {
			repoPath = old.repoPath // a repo kept from an earlier scan
		}
		paths := map[string][]string{}
		if k.Mode == "pooled" {
			for _, m := range modelsOf(k) {
				for i := 0; i < k.Pool; i++ {
					numbered[shortOf(m)]++
					paths[m] = append(paths[m], "routine."+name+"."+shortOf(m)+"-"+strconv.Itoa(numbered[shortOf(m)]))
				}
			}
		}
		sig := fmt.Sprintf("%s|%+v|%v", repoPath, k, paths)
		if old := f.groups[repo]; old != nil {
			if old.sig == sig {
				continue
			}
			old.cancel()
		}
		f.groups[repo] = r.startGroup(repo, repoPath, k, sig, paths)
	}
	for repo, g := range f.groups {
		if _, ok := defs[repo]; !ok {
			g.cancel()
			delete(f.groups, repo)
		}
	}
}

func (r *routineSet) startFeed(name string, cons jetstream.Consumer) *feed {
	ctx, cancel := context.WithCancel(r.ctx)
	f := &feed{name: name, cons: cons, ctx: ctx, cancel: cancel, groups: map[string]*group{}}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.dispatch(ctx, f)
	}()
	return f
}

// stop ends the dispatcher and every group; running jobs finish.
func (f *feed) stop() {
	f.cancel()
	f.mu.Lock()
	defer f.mu.Unlock()
	for repo, g := range f.groups {
		g.cancel()
		delete(f.groups, repo)
	}
}

// startGroup starts a group's pooled instances (none for fresh and inline
// kinds). Once they have all stopped, jobs left in the pools are returned
// to the inbox.
func (r *routineSet) startGroup(repo, repoPath string, k kind.Kind, sig string, paths map[string][]string) *group {
	ctx, cancel := context.WithCancel(r.ctx)
	g := &group{repo: repo, repoPath: repoPath, k: k, sig: sig, ctx: ctx, cancel: cancel, pools: map[string]*pool{}}
	if k.Mode != "pooled" {
		return g
	}
	var gwg sync.WaitGroup
	for _, m := range modelsOf(k) {
		p := &pool{model: m, paths: paths[m], jobs: make(chan *delivery, 64)}
		g.pools[m] = p
		for _, path := range p.paths {
			goCh := make(chan struct{}, 1)
			p.gos = append(p.gos, goCh)
			r.wg.Add(1)
			gwg.Add(1)
			go func() {
				defer r.wg.Done()
				defer gwg.Done()
				r.pooledInstance(g, p, path, goCh)
			}()
		}
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		gwg.Wait()
		for _, p := range g.pools {
		drain:
			for {
				select {
				case dl := <-p.jobs:
					dl.nak()
				default:
					break drain
				}
			}
		}
	}()
	return g
}

// signalGo tells every instance of the kind to start on what it holds.
func (f *feed) signalGo() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.groups {
		for _, p := range g.pools {
			for _, ch := range p.gos {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}
	}
}

func isTimeout(err error) bool {
	return errors.Is(err, nats.ErrTimeout) || errors.Is(err, jetstream.ErrNoMessages) || errors.Is(err, context.DeadlineExceeded)
}

// isGo reports a message that starts a batch: a go, or a job with go.
func isGo(m mesh.Message) bool {
	return m.Kind == "message" && strings.TrimSpace(m.Text) == "go" || m.Kind == "job" && m.Job != nil && m.Job.Go
}

// dispatch fetches the kind's messages one at a time until ctx ends. A go
// is passed to the instances; a job is routed. Held jobs count against
// MaxAckPending, so a go queued behind them cannot be fetched: while jobs
// are held and not yet running, each empty fetch looks ahead in the stream
// for one. A message fetched once ctx has ended goes back to the inbox.
func (r *routineSet) dispatch(ctx context.Context, f *feed) {
	var last, goSeen uint64 // highest stream sequence fetched; of a go acted on
	for ctx.Err() == nil {
		m, err := f.cons.Next(jetstream.FetchMaxWait(fetchWait))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !isTimeout(err) {
				r.d.Log.Warn("routines: fetch", "kind", f.name, "err", err)
				sleepCtx(ctx, fetchWait)
				continue
			}
			if f.held.Load() > 0 {
				if seq, ok := r.peekGo(ctx, f.name, max(last, goSeen)); ok {
					goSeen = seq
					f.signalGo()
				}
			}
			continue
		}
		if ctx.Err() != nil { // Next takes no ctx: the feed stopped meanwhile
			if err := m.Nak(); err != nil {
				r.d.Log.Warn("routines: nak", "kind", f.name, "err", err)
			}
			return
		}
		var seq uint64
		if meta, err := m.Metadata(); err == nil {
			seq = meta.Sequence.Stream
			last = max(last, seq)
		}
		var msg mesh.Message
		if err := json.Unmarshal(m.Data(), &msg); err != nil {
			r.d.Log.Warn("routines: malformed message", "kind", f.name, "err", err)
			m.Term()
			continue
		}
		switch {
		case msg.Kind == "job" && msg.Job != nil:
			r.route(f, m, msg)
		case isGo(msg):
			if err := m.Ack(); err != nil {
				r.d.Log.Warn("routines: ack go", "kind", f.name, "err", err)
			}
			if seq == 0 || seq > goSeen {
				f.signalGo()
			}
		default:
			r.d.Log.Info("routines: dropped message", "kind", f.name, "from", msg.From, "msg_kind", msg.Kind)
			m.Ack()
		}
	}
}

// peekGo looks at the kind's inbox past sequence after for a go.
func (r *routineSet) peekGo(ctx context.Context, kindName string, after uint64) (uint64, bool) {
	st, err := r.d.Client.JS.Stream(ctx, mesh.StreamMesh)
	if err != nil {
		return 0, false
	}
	subject := mesh.InboxSubject(mesh.RoutinePath(kindName))
	seq := after + 1
	for range 100 {
		raw, err := st.GetMsg(ctx, seq, jetstream.WithGetMsgSubject(subject))
		if err != nil {
			return 0, false // ErrMsgNotFound: nothing further
		}
		var m mesh.Message
		if json.Unmarshal(raw.Data, &m) == nil && isGo(m) {
			return raw.Sequence, true
		}
		seq = raw.Sequence + 1
	}
	return 0, false
}

// route settles what the dispatcher can decide (unknown repo, a kind not on
// the repo's main, an inline kind, an unknown model: each replied and acked)
// and hands the rest to the repo's pool for the model or to a fresh run.
func (r *routineSet) route(f *feed, m jetstream.Msg, msg mesh.Message) {
	j := *msg.Job
	from, err := mesh.ParsePath(msg.From)
	if err != nil {
		r.d.Log.Warn("routines: job from an invalid path", "kind", f.name, "job", j.ID, "from", msg.From)
		m.Ack()
		return
	}
	dl := r.newDelivery(f, m, j, from)
	if f.ctx.Err() != nil {
		dl.nak()
		return
	}
	refuse := func(text string) {
		r.reply(dl, mesh.Reply{Output: text, By: mesh.RoutinePath(f.name).String()})
		dl.ack()
	}
	if _, ok := r.d.Cfg.Repos[j.Repo]; !ok {
		refuse("unknown repo " + j.Repo)
		return
	}
	if text := r.handOff(f, dl); text != "" {
		refuse(text)
	}
}

// handOff passes dl to its repo's pool for its model, or starts a fresh run;
// otherwise it returns why the job cannot run.
func (r *routineSet) handOff(f *feed, dl *delivery) string {
	j := dl.job
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ctx.Err() != nil { // stopping: its groups are gone, not undefined
		dl.nak()
		return ""
	}
	g := f.groups[j.Repo]
	switch {
	case g == nil:
		return fmt.Sprintf("kind %s is not on %s's main", f.name, j.Repo)
	case g.k.Mode == "inline":
		return fmt.Sprintf("kind %s is inline; run it with bmesh routine run", f.name)
	}
	model := j.Model
	if model == "" && g.k.IsModel() {
		model = g.k.ModelsList()[0]
	}
	if g.k.IsModel() && !slices.Contains(g.k.ModelsList(), model) || !g.k.IsModel() && model != "" {
		return "unknown model " + model
	}
	switch {
	case g.ctx.Err() != nil:
		dl.nak()
	case g.k.Mode == "fresh":
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.freshRun(g, dl, model)
		}()
	default:
		dl.hold()
		select {
		case g.pools[model].jobs <- dl:
		default:
			dl.nak()
		}
	}
	return ""
}

// sleepCtx waits d or until ctx ends; false when ctx ended.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// tryClaimPath holds path as claimPath does when no instance runs as it;
// nil when one does.
func (r *routineSet) tryClaimPath(path string) (release func()) {
	r.pathMu.Lock()
	defer r.pathMu.Unlock()
	if _, busy := r.paths[path]; busy {
		return nil
	}
	ch := make(chan struct{})
	r.paths[path] = ch
	return func() {
		r.pathMu.Lock()
		delete(r.paths, path)
		r.pathMu.Unlock()
		close(ch)
	}
}

// claimPath waits until no other instance runs as path, then holds it until
// the returned release is called; nil when ctx ends first.
func (r *routineSet) claimPath(ctx context.Context, path string) (release func()) {
	for {
		r.pathMu.Lock()
		prev, busy := r.paths[path]
		if !busy {
			ch := make(chan struct{})
			r.paths[path] = ch
			r.pathMu.Unlock()
			return func() {
				r.pathMu.Lock()
				delete(r.paths, path)
				r.pathMu.Unlock()
				close(ch)
			}
		}
		r.pathMu.Unlock()
		select {
		case <-prev:
		case <-ctx.Done():
			return nil
		}
	}
}
