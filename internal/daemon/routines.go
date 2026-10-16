package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math/rand/v2"
	"os"
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
	"github.com/rickdiculousli/buildermesh/internal/level"
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
	deferBase    = 2 * time.Second        // a deferred job's first redelivery delay, doubled per delivery
	deferCap     = time.Minute            // the longest a deferred job waits before it is redelivered
)

// routineSet runs every routine kind of every repo in [repos]. Each kind has
// one feed: its inbox consumer, shared with every other box that runs the
// kind, and a dispatcher that fetches the kind's messages and routes each
// job to the group of the job's repo, which runs it on a pooled instance of
// the job's model or on a fresh run of its own. A job for a repo this box
// can't judge the kind for is deferred to a box that can.
type routineSet struct {
	d      Deps
	ctx    context.Context    // the loop's: running jobs and heartbeats end with it
	cancel context.CancelFunc // ends ctx: an update's drain kills running jobs with it
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

	holdMu sync.Mutex                      // serializes writes of holds
	holds  map[string]map[uint64]mesh.Hold // kind → job sequence → this daemon's unsettled job

	missing map[string]bool   // registered repos with no path on this box, logged once; rescan's own
	readErr map[string]string // repo → the main commit whose kinds failed to read, logged once per commit; rescan's own
	mainAt  map[string]string // repo → "<ref> at <sha8>", the main its kinds were last read at; rescan's own

	deferMu  sync.Mutex           // guards deferred
	deferred map[string]time.Time // job id → when it was last deferred, so each is logged once

	updateRepo string // [update] repo when the update can run, else ""
	updateSeen string // the main commit the update last acted on

	checkEvery time.Duration // how often main is checked for the notice; 0 = never (self-updating from a row, or off)
	checkNext  time.Time     // when the next check is due
	upstream   string        // the module the check installs from ([update] upstream), else "": notice only
	failedAt   string        // the main commit whose failed install was last logged
	drained    bool          // an update drained the feeds; guarded by mu
}

// fetch updates repoPath's origin refs (nothing without an origin). With
// [hosts] enabled it holds repo's lock (<state dir>/locks/<repo>.lock),
// which a level and a landing take too.
func (r *routineSet) fetch(repo, repoPath string) error {
	r.fetchMu.Lock()
	defer r.fetchMu.Unlock()
	if r.d.Cfg.Hosts.Enabled && r.d.StateDir != "" {
		unlock, err := level.Lock(r.d.StateDir, repo)
		if err != nil {
			return err
		}
		defer unlock()
	}
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
	ackWait  atomic.Int64 // time.Duration: the AckWait this box last wrote
	capacity atomic.Int64 // this box's instance count: the most jobs it fetches at once
	inflight atomic.Int64 // jobs fetched and not yet settled

	heldMu sync.Mutex
	held   map[*delivery]uint64 // jobs handed to a pool and not yet started → stream sequence

	mu       sync.Mutex // guards groups, deferred, mains and sends to the pools
	groups   map[string]*group
	deferred map[string]string // repo → why this box can't judge the kind for it
	mains    map[string]string // repo → the main its kinds were read at ("origin/main at <sha8>")
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
	names []instanceName
	jobs  chan *delivery
	gos   []chan struct{}
}

// instanceName is a pooled instance's node path, which carries this box's
// host, and the name of its checkout, which doesn't: checkouts are local to
// the box.
type instanceName struct{ path, checkout string }

// routines runs the routine loop until ctx ends (or an update drained it):
// on start and every kindRescan it reads each repo's kinds from its main and
// starts, restarts or stops the kinds' feeds, pools and instances to match.
// When it ends, every job it held or ran has been handed back (NAK) or
// answered, and the NAKs have reached the server.
func routines(ctx context.Context, d Deps) {
	d.defaults()
	if d.Checkouts == "" {
		d.Log.Warn("routines: no checkout root; routines are off")
		if d.Cfg.Update.Repo != "" {
			d.Log.Warn("update: no checkout root to build from; off", "repo", d.Cfg.Update.Repo)
		}
		d.writeNotice("")
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	r := &routineSet{d: d, ctx: ctx, cancel: cancel, root: d.Checkouts, claude: d.ClaudeBin,
		feeds: map[string]*feed{}, paths: map[string]chan struct{}{}, missing: map[string]bool{},
		readErr: map[string]string{}, mainAt: map[string]string{}, deferred: map[string]time.Time{},
		holds: map[string]map[uint64]mesh.Hold{}, updateRepo: updateRepo(d)}
	if strings.TrimSpace(r.claude) == "" {
		r.claude = "claude"
	}
	r.initCheck()
	// What a daemon that died here held is back in the inbox after AckWait,
	// not held.
	if err := mesh.ClearHolds(ctx, d.Client, d.Host, d.Now().UTC().Format(time.RFC3339)); err != nil {
		d.Log.Warn("routines: clear holds", "err", err)
	}
	every(ctx, kindRescan, r.rescan)
	r.mu.Lock()
	for name, f := range r.feeds {
		f.stop()
		delete(r.feeds, name)
	}
	r.mu.Unlock()
	r.wg.Wait()
	// A NAK is a publish; flush so it reaches the server before serve
	// closes the connection.
	if err := d.Client.NC.FlushTimeout(settleTimeout); err != nil {
		d.Log.Warn("routines: flush", "err", err)
	}
}

// discover returns the kinds on repoPath's main: origin/main after a fetch
// when the repo has an origin, else the local main (R4). dir is the main
// checkout once it is at that commit, else ""; at names the commit the kinds
// were read at, "<ref> at <sha8>".
func (r *routineSet) discover(repo, repoPath string) (dir, at string, kinds []kind.Kind, err error) {
	if err := r.fetch(repo, repoPath); err != nil {
		r.d.Log.Warn("routines: fetch", "repo", repoPath, "err", err)
	}
	dir, err = r.ensureCheckout(repoPath, "main")
	if err != nil {
		return "", "", nil, err
	}
	ref := "main"
	if exec.Command("git", "-C", repoPath, "remote", "get-url", "origin").Run() == nil {
		ref = "origin/main"
	}
	// Under the lock a `bmesh update` on this box holds while it builds
	// from the same checkout.
	unlock, err := selfupdate.Lock(dir)
	if err != nil {
		return "", "", nil, err
	}
	err = checkout.MoveTo(dir, ref)
	var sha []byte
	if err == nil {
		sha, err = exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	}
	unlock()
	if err != nil {
		return "", "", nil, err
	}
	kinds, err = kind.LoadDir(dir)
	return dir, fmt.Sprintf("%s at %.8s", ref, strings.TrimSpace(string(sha))), kinds, err
}

// mainCommit is the commit discover reads kinds at, origin/main when the
// repo has an origin, else main; "" when that doesn't resolve.
func mainCommit(repoPath string) string {
	ref := "main"
	if exec.Command("git", "-C", repoPath, "remote", "get-url", "origin").Run() == nil {
		ref = "origin/main"
	}
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "-q", ref+"^{commit}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// rescan reads every repo's kinds and brings the feeds in line; a kind whose
// os is another OS's is left out, so this box never fetches its jobs. A repo that
// cannot be read keeps what it had; one whose path does not exist on this
// box has no kinds here and is logged once per daemon start. Each feed is
// handed, with its groups, the repos it can't judge the kind for here: no
// checkout, a main that failed to read and left no group, or the kind left
// out for another OS. Their jobs are deferred to a box that can. With
// [update] on, the update repo's main is then compared with this binary; an
// update drains the feeds for good. Without it, every [update] check_every
// main is checked for the notice (and, with upstream, installed).
func (r *routineSet) rescan() {
	if r.isDrained() {
		return
	}
	updateDir, rowDir := "", "" // the main checkouts of the update repo while it updates, and of [update] repo
	names := make([]string, 0, len(r.d.Cfg.Repos))
	for name := range r.d.Cfg.Repos {
		names = append(names, name)
	}
	sort.Strings(names)
	want := map[string]map[string]kind.Kind{} // kind → repo → definition
	repoPaths := map[string]string{}
	failed := map[string]bool{}
	cant := map[string]string{}             // repo → why no kind of it can be judged here
	osOut := map[string]map[string]string{} // kind → repo → why it is left to another OS
	for _, name := range names {
		if r.ctx.Err() != nil {
			return
		}
		_, path, err := r.d.Cfg.RepoPath(name)
		if err != nil {
			r.d.Log.Warn("routines: repo", "repo", name, "err", err)
			cant[name] = "has no usable path"
			continue
		}
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			// Registered for other boxes: nothing to read here.
			if !r.missing[name] {
				r.missing[name] = true
				r.d.Log.Info("routines: repo has no checkout on this box; skipped", "repo", name, "path", path)
			}
			cant[name] = "has no checkout"
			delete(r.mainAt, name)
			continue
		}
		repoPaths[name] = path
		dir, at, kinds, err := r.discover(name, path)
		if name == r.updateRepo {
			updateDir = dir
		}
		if name == r.d.Cfg.Update.Repo {
			rowDir = dir
		}
		if err != nil {
			// Once per repo and main commit: a repo with no main fails
			// the same way every minute.
			at := mainCommit(path)
			if seen, ok := r.readErr[name]; !ok || seen != at {
				r.readErr[name] = at
				r.d.Log.Warn("routines: read kinds", "repo", name, "err", err)
			}
			failed[name] = true
			cant[name] = "has a main that failed to read"
			continue
		}
		delete(r.readErr, name)
		r.mainAt[name] = at
		for _, k := range kinds {
			if !k.RunsOn(r.d.GOOS) { // another OS's box takes its jobs
				if osOut[k.Name] == nil {
					osOut[k.Name] = map[string]string{}
				}
				osOut[k.Name][name] = "runs " + k.Name + " on " + k.OS + ", not"
				continue
			}
			if want[k.Name] == nil {
				want[k.Name] = map[string]kind.Kind{}
			}
			want[k.Name][name] = k
		}
	}

	if updateDir != "" && r.checkUpdate(updateDir) {
		return
	}
	if r.checkEvery > 0 && !r.d.Now().Before(r.checkNext) {
		r.checkNext = r.d.Now().Add(r.checkEvery)
		if r.checkMain(rowDir) {
			return
		}
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
		deferred := maps.Clone(osOut[name])
		if deferred == nil {
			deferred = map[string]string{}
		}
		for repo, why := range cant {
			if _, kept := want[name][repo]; !kept { // a failed read keeps an earlier scan's group
				deferred[repo] = why
			}
		}
		r.applyKind(name, want[name], repoPaths, deferred, maps.Clone(r.mainAt))
	}
	r.forgetDeferred()
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

// consumerSettings: capacity is this box's instance count for the kind over
// its repos (pool × models for pooled, pool for fresh: R5), at least 1 so a
// feed whose groups can only refuse still fetches; AckWait the longest
// timeout plus ackWaitExtra.
func consumerSettings(defs map[string]kind.Kind) (int, time.Duration) {
	capacity, longest := 0, time.Duration(0)
	for _, k := range defs {
		switch k.Mode {
		case "pooled":
			capacity += k.Pool * len(modelsOf(k))
		case "fresh":
			capacity += k.Pool
		}
		longest = max(longest, k.Timeout)
	}
	return max(capacity, 1), max(longest+ackWaitExtra, time.Second)
}

// applyKind starts the kind's feed when new, brings its consumer in line
// and starts or restarts each repo's group whose definition or instance
// paths changed; deferred and mains change with the groups, under f.mu.
// Called with r.mu held.
//
// The consumer is every box's: its MaxAckPending is -1 (each box gates its
// own fetches at its capacity), and its AckWait is the last writer's. This
// box writes AckWait when its own value changes; otherwise it reads the
// consumer and writes -1 back, keeping the AckWait it read, when an old
// daemon left a count.
func (r *routineSet) applyKind(name string, defs map[string]kind.Kind, repoPaths, deferred, mains map[string]string) {
	f := r.feeds[name]
	capacity, ackWait := consumerSettings(defs)
	switch {
	case f == nil:
		cons, err := mesh.KindConsumer(r.ctx, r.d.Client, name, -1, ackWait)
		if err != nil {
			r.d.Log.Warn("routines: consumer", "kind", name, "err", err)
			return
		}
		f = r.startFeed(name, cons, capacity)
		f.ackWait.Store(int64(ackWait))
		r.feeds[name] = f
	case time.Duration(f.ackWait.Load()) != ackWait:
		if _, err := mesh.KindConsumer(r.ctx, r.d.Client, name, -1, ackWait); err != nil {
			r.d.Log.Warn("routines: consumer", "kind", name, "err", err)
		} else {
			f.ackWait.Store(int64(ackWait))
		}
	default:
		r.convergeConsumer(f)
	}
	f.capacity.Store(int64(capacity))

	repos := make([]string, 0, len(defs))
	for repo := range defs {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	host := mesh.RepoToken(r.d.Host)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deferred, f.mains = deferred, mains
	numbered := map[string]int{} // model short → instances numbered so far
	for _, repo := range repos {
		k := defs[repo]
		repoPath := repoPaths[repo]
		if old := f.groups[repo]; old != nil && repoPath == "" {
			repoPath = old.repoPath // a repo kept from an earlier scan
		}
		names := map[string][]instanceName{}
		if k.Mode == "pooled" {
			for _, m := range modelsOf(k) {
				for i := 0; i < k.Pool; i++ {
					short := shortOf(m)
					numbered[short]++
					n := strconv.Itoa(numbered[short])
					names[m] = append(names[m], instanceName{
						path:     "routine." + name + "." + host + "-" + short + "-" + n,
						checkout: "routine." + name + "." + short + "-" + n,
					})
				}
			}
		}
		sig := fmt.Sprintf("%s|%+v|%v", repoPath, k, names)
		if old := f.groups[repo]; old != nil {
			if old.sig == sig {
				continue
			}
			old.cancel()
		}
		f.groups[repo] = r.startGroup(repo, repoPath, k, sig, names)
	}
	for repo, g := range f.groups {
		if _, ok := defs[repo]; !ok {
			g.cancel()
			delete(f.groups, repo)
		}
	}
}

// convergeConsumer reads the kind's consumer and, when its MaxAckPending is
// anything but -1 (an old daemon's count), writes -1 back with the AckWait
// it read.
func (r *routineSet) convergeConsumer(f *feed) {
	ctx, cancel := context.WithTimeout(r.ctx, settleTimeout)
	defer cancel()
	info, err := f.cons.Info(ctx)
	if err != nil {
		r.d.Log.Warn("routines: consumer info", "kind", f.name, "err", err)
		return
	}
	if info.Config.MaxAckPending == -1 {
		return
	}
	if _, err := mesh.KindConsumer(ctx, r.d.Client, f.name, -1, info.Config.AckWait); err != nil {
		r.d.Log.Warn("routines: consumer", "kind", f.name, "err", err)
	}
}

func (r *routineSet) startFeed(name string, cons jetstream.Consumer, capacity int) *feed {
	ctx, cancel := context.WithCancel(r.ctx)
	f := &feed{name: name, cons: cons, ctx: ctx, cancel: cancel, groups: map[string]*group{},
		held: map[*delivery]uint64{}}
	f.capacity.Store(int64(capacity))
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.dispatch(ctx, f)
	}()
	return f
}

// stop ends the dispatcher and every group; running jobs finish (until
// the loop's ctx ends).
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
func (r *routineSet) startGroup(repo, repoPath string, k kind.Kind, sig string, names map[string][]instanceName) *group {
	ctx, cancel := context.WithCancel(r.ctx)
	g := &group{repo: repo, repoPath: repoPath, k: k, sig: sig, ctx: ctx, cancel: cancel, pools: map[string]*pool{}}
	if k.Mode != "pooled" {
		return g
	}
	var gwg sync.WaitGroup
	for _, m := range modelsOf(k) {
		p := &pool{model: m, names: names[m], jobs: make(chan *delivery, 64)}
		g.pools[m] = p
		for _, name := range p.names {
			goCh := make(chan struct{}, 1)
			p.gos = append(p.gos, goCh)
			r.wg.Add(1)
			gwg.Add(1)
			go func() {
				defer r.wg.Done()
				defer gwg.Done()
				r.pooledInstance(g, p, name, goCh)
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

// dispatch fetches the kind's messages one at a time until ctx ends, while
// fewer jobs than this box's capacity are in flight: the consumer has no
// limit of its own, since every box that runs the kind shares it. A go is
// passed to the instances; a job is routed. A go is one message that one box
// fetches, so while jobs are held this box looks in the stream for a go past
// the oldest of them (and past the last go it acted on): after each empty
// fetch, and every fetchWait while its capacity is full. A message fetched
// once ctx has ended goes back to the inbox.
func (r *routineSet) dispatch(ctx context.Context, f *feed) {
	var goSeen uint64 // stream sequence of the last go acted on
	peek := func() {
		if oldest, ok := f.oldestHeld(); ok {
			if seq, ok := r.peekGo(ctx, f.name, max(oldest, goSeen)); ok {
				goSeen = seq
				f.signalGo()
			}
		}
	}
	for ctx.Err() == nil {
		if f.inflight.Load() >= f.capacity.Load() {
			peek()
			sleepCtx(ctx, fetchWait)
			continue
		}
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
			peek()
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
				goSeen = max(goSeen, seq)
				f.signalGo()
			}
		default:
			r.d.Log.Info("routines: dropped message", "kind", f.name, "from", msg.From, "msg_kind", msg.Kind)
			m.Ack()
		}
	}
}

// oldestHeld is the stream sequence of the oldest job held in a pool and
// not yet started; false when none is.
func (f *feed) oldestHeld() (uint64, bool) {
	f.heldMu.Lock()
	defer f.heldMu.Unlock()
	if len(f.held) == 0 {
		return 0, false
	}
	oldest := uint64(0)
	first := true
	for _, seq := range f.held {
		if first || seq < oldest {
			oldest, first = seq, false
		}
	}
	return oldest, true
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

// route settles what the dispatcher can decide (a kind not on the repo's
// main, an inline kind, an unknown model: each replied and acked), defers
// what this box can't judge (a repo its registry doesn't name yet, or one
// the feed defers) and hands the rest to the repo's pool for the model or to
// a fresh run.
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
		// serve reads repos.toml only when it starts: a box that has the
		// row now runs it once restarted, and another box may have it.
		r.deferJob(dl, "is not in the registry serve read at start, so not")
		return
	}
	if text := r.handOff(f, dl); text != "" {
		refuse(text)
	}
}

// deferJob hands dl back for a box that can run it: a NAK delayed
// deferBase × 2^(n-1), at most deferCap, n its delivery count, with no
// reply. A random part of the delay, up to a quarter, is taken off: this box
// fetches again at once and then every fetchWait, so an exact delay would
// end just as its own fetch became the oldest waiting, which nats-server
// serves first, and a box that can run the job would never get it. Each job
// is logged once.
func (r *routineSet) deferJob(dl *delivery, why string) {
	n := uint64(1)
	if meta, err := dl.msg.Metadata(); err == nil {
		n = meta.NumDelivered
	}
	r.deferMu.Lock()
	_, seen := r.deferred[dl.job.ID]
	r.deferred[dl.job.ID] = r.d.Now()
	r.deferMu.Unlock()
	if !seen {
		r.d.Log.Info(fmt.Sprintf("routines: deferred job %s: %s %s here", dl.job.ID, dl.job.Repo, why))
	}
	d := deferDelay(n)
	dl.nakDelay(d - rand.N(d/4+1))
}

// deferDelay is a deferred job's redelivery delay at its nth delivery.
func deferDelay(n uint64) time.Duration {
	d := deferBase
	for i := uint64(1); i < n && d < deferCap; i++ {
		d *= 2
	}
	return min(d, deferCap)
}

// forgetDeferred drops the deferred jobs not seen for ten times deferCap:
// answered, or taken by a box that can run them.
func (r *routineSet) forgetDeferred() {
	cutoff := r.d.Now().Add(-10 * deferCap)
	r.deferMu.Lock()
	defer r.deferMu.Unlock()
	for id, at := range r.deferred {
		if at.Before(cutoff) {
			delete(r.deferred, id)
		}
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
	if g == nil {
		if why, ok := f.deferred[j.Repo]; ok {
			r.deferJob(dl, why)
			return ""
		}
		return fmt.Sprintf("kind %s is not on %s's main (%s)", f.name, j.Repo, f.mains[j.Repo])
	}
	switch {
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
		dl.note(r.d.from(), "held")
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
