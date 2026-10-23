package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rickdiculousli/buildermesh"
	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/daemon"
	"github.com/rickdiculousli/buildermesh/internal/instance"
	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/merge"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/scaffold"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

const landUsage = "usage: bmesh land [--spec NNN] [--size XS|S|M|L] [--ttl D] [--wait D]"

// landPoll is how often land tries a held lease again and looks for a job's
// reply; landMainPoll how often it fetches again while the main checkout
// has unpushed commits; landEventEvery how often it publishes its land
// event. Tests shorten them.
var (
	landPoll       = 2 * time.Second
	landMainPoll   = 15 * time.Second
	landEventEvery = time.Minute
)

// landRepoRE is what a repo name must look like to name a lease key.
var landRepoRE = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*$`)

// errReviewAgain is land's own rebase changing the diff the branch's review
// read: land releases the lease and queues the review again.
var errReviewAgain = errors.New("the rebase changed the diff the review read")

// lander is one `bmesh land`: a lead's worktree landing its spec on main.
type lander struct {
	a        *app
	top      string    // the lead's worktree
	repoPath string    // the main checkout
	id       int       // the spec
	holder   string    // the node (or host) holding the lease and recording results
	node     mesh.Path // the landing node, whose inbox is read; nil outside a node
	repo     string    // the registry row's name, else the lease's
	branch   string
	origin   bool
	base     string // origin/main, or main without an origin
	size     string // --size: the size the change was built at

	deadline time.Time          // the end of all of land's waiting (--wait)
	mailSeq  uint64             // the node's inbox sequence when land started: operator mail after it refuses
	waited   string             // HEAD as land first read it, or land's own rebase of it
	requeued map[string]bool    // the jobs land queued once more after a reply that recorded nothing
	answered map[string]bool    // the jobs whose replies land read
	kinds    []kind.Kind        // the repo's kinds
	doing    atomic.Value       // what land is doing, for its land events
	heads    []string           // the branch's heads before land touched it: HEAD and the heads a rebase by hand rewrote
	before   map[string]*source // per kind with a result before land touched the branch: where it is
	required []string           // the kinds the landed tree must carry
	ran      map[string]string  // per inline kind, the last tree it was ok at in this run
	resolved []string           // the files a rebase in this run resolved by rule

	landed string // the commit main points at after the push
}

// source is where a required kind's result was before land touched the
// branch: HEAD as it stood, or the head a rebase by hand rewrote.
type source struct {
	head, tree string
	base       string        // head's fork point from main, set at the first fetch
	rebase     string        // the commit the rebase made of head; "" for HEAD as it stood
	result     mesh.Result   // the kind's newest result at tree, a failing one when any model's newest fails
	newest     []mesh.Result // the kind's newest result per model at tree
}

// String says where the result is, for land's required line.
func (s *source) String() string {
	if s.rebase == "" {
		return "t:" + s.tree[:8] + " at HEAD"
	}
	return "t:" + s.tree[:8] + " before the rebase at " + s.rebase[:8]
}

// cmdLand lands the worktree's branch on main under the lease land.<repo>.
// Before the lease: the inline kinds on HEAD, the review the spec's size
// calls for, the node's jobs on HEAD and a main checkout with nothing
// unpushed. Under it: fetch, rebase onto main, results for the rebased
// tree, the spec's verified line, done with merged and branch, a commit, a
// fast-forward push. A push main refuses (it moved) starts again from the
// fetch, and a rebase that changed the diff a review read queues the review
// again and starts again from the top, while --wait lasts.
func (a *app) cmdLand(args []string) error {
	fs := a.flags("land")
	id := fs.String("spec", "", "spec id (default this node's spec)")
	size := fs.String("size", "", "the size the change was built at, when it grew past the spec's (XS, S, M or L)")
	ttl := fs.Duration("ttl", defaultTTL, "how long the lease lasts unrenewed")
	wait := fs.Duration("wait", 30*time.Minute, "how long land waits in all, for jobs, main, the main checkout and the lease (exit 5 after)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, landUsage)
	}
	if *ttl < minTTL {
		return fail(exitUsage, "land: --ttl must be at least %s", minTTL)
	}
	if _, ok := reviewEffort(*size); *size != "" && !ok {
		return fail(exitUsage, "land: --size %q: want XS, S, M or L", *size)
	}
	top, err := gitRev("--show-toplevel")
	if err != nil {
		return err
	}
	l := &lander{a: a, top: top, size: *size, deadline: time.Now().Add(*wait),
		ran: map[string]string{}, requeued: map[string]bool{}, answered: map[string]bool{}}
	l.doing.Store("landing")
	if err := l.setup(id); err != nil {
		return err
	}
	stop := l.events()
	defer stop()
	// Land's waits outlast a command's minute; each mesh call bounds its own.
	ctx := context.Background()
	slot := "land." + l.repo
	for {
		if again, err := l.ready(ctx); err != nil {
			return err
		} else if again {
			continue
		}
		key, err := a.acquireWait(slot, l.holder, *ttl, l.deadline, func(by string) error {
			l.doing.Store(fmt.Sprintf("waiting for %s (held by %s)", slot, by))
			return l.check(ctx)
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "lease %s\n", slot)
		l.doing.Store("landing under " + slot)
		err = a.holdLeases([]string{key}, l.holder, *ttl, l.land)
		if !errors.Is(err, errReviewAgain) {
			if err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "released %s\nlanded %s\n", slot, l.landed[:8])
			return nil
		}
		fmt.Fprintf(a.stdout, "released %s\n", slot)
		if l.waited, err = gitIn(ctx, l.top, "rev-parse", "HEAD"); err != nil {
			return err
		}
		if _, err := l.queueReview(ctx, true); err != nil {
			return err
		}
	}
}

// setup resolves the node, the spec, the repo and the main checkout, and
// notes where the node's inbox stands.
func (l *lander) setup(id *string) error {
	a := l.a
	ctx, cancel := cmdCtx()
	defer cancel()
	p, err := a.identity(ctx, "land")
	switch {
	case errors.Is(err, errNoNode):
		p = nil
	case err != nil:
		return err
	}
	l.holder = a.cfg.HostName()
	repo := a.repoNameAt(l.top)
	if p != nil {
		l.holder = p.String()
		l.node = p
		if repo, err = a.repoName(ctx, p, l.top); err != nil {
			return err
		}
		if *id == "" {
			n, ok, err := mesh.GetNode(ctx, a.client, p.String())
			if err != nil {
				return err
			}
			if ok {
				*id = n.Spec
			}
		}
		if l.mailSeq, err = mesh.InboxSeq(ctx, a.client, p); err != nil {
			return err
		}
	}
	if *id == "" {
		return fail(exitUsage, "land needs --spec NNN (no node spec resolves)")
	}
	if l.id, err = strconv.Atoi(*id); err != nil {
		return fail(exitUsage, "--spec %q is not a number", *id)
	}
	if !landRepoRE.MatchString(repo) {
		return fail(exitUsage, "repo name %q can't name a lease", repo)
	}
	common, ok := checkout.CommonDir(l.top)
	if !ok || filepath.Base(common) != ".git" {
		return fail(exitRefused, "refused: %s is not a worktree of a repo with a main checkout", l.top)
	}
	l.repoPath = filepath.Dir(common)
	l.repo = repo
	return nil
}

// acquireWait takes the one place of slot for holder, trying again every
// landPoll while someone else holds it (at once when it was released
// between the try and the look); it names the holder once per holder, calls
// check with it before each wait (an error ends the wait) and gives up at
// deadline (exit 5).
func (a *app) acquireWait(slot, holder string, ttl time.Duration, deadline time.Time, check func(by string) error) (string, error) {
	said := ""
	for {
		ctx, cancel := cmdCtx()
		key, err := mesh.AcquireLease(ctx, a.client, slot, holder, ttl, 1, time.Now())
		if !errors.Is(err, mesh.ErrBusy) {
			cancel()
			return key, err
		}
		by := "someone"
		if leases, err := mesh.ListLeases(ctx, a.client); err == nil {
			i := slices.IndexFunc(leases, func(l mesh.Lease) bool { return l.Slot == slot })
			if i < 0 {
				// Released since the try: try again at once.
				cancel()
				continue
			}
			by = leases[i].Holder
		}
		cancel()
		if by != said {
			fmt.Fprintf(a.stdout, "waiting for %s (held by %s)\n", slot, by)
			said = by
		}
		if check != nil {
			if err := check(by); err != nil {
				return "", err
			}
		}
		if time.Now().After(deadline) {
			return "", fail(exitTimeout, "timed out waiting for %s, held by %s", slot, by)
		}
		time.Sleep(landPoll)
	}
}

// events publishes the node's land event, naming what land is doing, now
// and every landEventEvery until the returned stop: it sets no state but is
// the node's activity, so a lead waiting in land is not called silent.
// Land run outside a node publishes none.
func (l *lander) events() (stop func()) {
	if l.node == nil {
		return func() {}
	}
	done, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(landEventEvery)
		defer t.Stop()
		for {
			ctx, cancel := cmdCtx()
			text, _ := l.doing.Load().(string)
			if err := mesh.PublishEvent(ctx, l.a.client, mesh.Event{From: l.node.String(), Event: "land", Text: text}); err != nil {
				fmt.Fprintf(l.a.stderr, "bmesh: land event: %v\n", err)
			}
			cancel()
			select {
			case <-done:
				return
			case <-t.C:
			}
		}
	}()
	return func() { close(done); <-finished }
}

// check reads the node's hold and its mail again, as every wait does
// before it sleeps: a hold or the operator's mail refuses the landing.
func (l *lander) check(ctx context.Context) error {
	if err := l.gate(ctx); err != nil {
		return err
	}
	return l.mail(ctx, false)
}

// pause is one poll of a wait for what: the hold and the mail read again
// (check), then a sleep of d, cut short at the deadline; past the deadline
// it gives up (exit 5).
func (l *lander) pause(ctx context.Context, d time.Duration, what string) error {
	l.doing.Store("waiting for " + what)
	if err := l.check(ctx); err != nil {
		return err
	}
	left := time.Until(l.deadline)
	if left <= 0 {
		return fail(exitTimeout, "timed out waiting for %s; nothing is pushed", what)
	}
	select {
	case <-time.After(min(d, left)):
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// ready runs one pass of land's steps before the lease: prepare, the inline
// kinds on HEAD, the review the spec's size calls for, the node's jobs and
// a main checkout with nothing unpushed. again reports a pass that waited
// for jobs, whose results change what land requires: the next pass reads
// them.
func (l *lander) ready(ctx context.Context) (again bool, err error) {
	if err := l.prepare(ctx); err != nil {
		return false, err
	}
	tree, err := gitIn(ctx, l.top, "rev-parse", l.waited+"^{tree}")
	if err != nil {
		return false, err
	}
	l.doing.Store("running the inline kinds on t:" + tree[:8])
	if err := l.inline(ctx, l.waited, tree, tree); err != nil {
		return false, err
	}
	jobs, err := l.review(ctx)
	if err != nil {
		return false, err
	}
	onHead, err := l.headJobs(ctx)
	if err != nil {
		return false, err
	}
	for _, j := range onHead {
		if !slices.ContainsFunc(jobs, func(w waitJob) bool { return w.ID == j.ID }) {
			jobs = append(jobs, j)
		}
	}
	if len(jobs) > 0 {
		return true, l.waitJobs(ctx, jobs)
	}
	return false, l.mainPushed(ctx)
}

// prepare refuses a worktree land can't start from and reads the results
// recorded for the branch, which name the kinds the landed tree must carry
// (verify always, when the repo has it): those at HEAD's tree and, when HEAD
// is a commit a rebase made, those at the tree of the head it rewrote, as if
// land had done that rebase itself. A kind takes the nearest head with a
// result for it. It prints the kinds and where each came from. A pass after
// the first refuses a HEAD other than the one land waited on.
func (l *lander) prepare(ctx context.Context) error {
	if err := l.gate(ctx); err != nil {
		return err
	}
	if err := refuseDirty(); err != nil {
		return err
	}
	b, err := gitIn(ctx, l.top, "branch", "--show-current")
	if err != nil {
		return err
	}
	if b == "" || b == "main" {
		return fail(exitRefused, "refused: land runs on a lead's branch, not %s", dash(b))
	}
	l.branch = b
	if err := l.mail(ctx, l.waited == ""); err != nil {
		return err
	}
	l.origin = hasOrigin(ctx, l.top)
	l.base = "main"
	if l.origin {
		l.base = "origin/main"
	}
	head, err := gitIn(ctx, l.top, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if l.waited == "" {
		l.waited = head
	} else if head != l.waited {
		return l.moved(head)
	}
	heads := append([]source{{head: head}}, rebasedFrom(ctx, l.top, b, head)...)
	all, err := mesh.ListAllResults(ctx, l.a.client)
	if err != nil {
		return err
	}
	l.before, l.heads, l.required = map[string]*source{}, nil, nil
	for _, h := range heads {
		l.heads = append(l.heads, h.head)
		if h.tree, err = gitIn(ctx, l.top, "rev-parse", h.head+"^{tree}"); err != nil {
			return err
		}
		// The newest result per (kind, model), as spec verified composes
		// them; a kind is as good as its worst model.
		type group struct{ kind, model string }
		newest := map[group]mesh.Result{}
		for _, r := range all {
			g := group{r.Kind, r.Model}
			if old, ok := newest[g]; r.Tree == h.tree && (!ok || spec.Newer(r.At, old.At)) {
				newest[g] = r
			}
		}
		at := map[string]*source{}
		for g, r := range newest {
			if _, ok := l.before[g.kind]; ok {
				continue
			}
			src, ok := at[g.kind]
			if !ok {
				src = &source{head: h.head, tree: h.tree, rebase: h.rebase, result: r}
				at[g.kind] = src
			}
			src.newest = append(src.newest, r)
			if src.result.OK && !r.OK {
				src.result = r
			}
		}
		for k, src := range at {
			l.before[k] = src
		}
	}
	for k := range l.before {
		l.required = append(l.required, k)
	}
	_, kinds, err := cwdKinds()
	if err != nil {
		return err
	}
	l.kinds = kinds
	repoVerify := slices.ContainsFunc(kinds, func(k kind.Kind) bool { return k.Name == "verify" })
	if repoVerify && !slices.Contains(l.required, "verify") {
		l.required = append(l.required, "verify")
	}
	sort.Strings(l.required)
	var said []string
	for _, k := range l.required {
		switch {
		case k == "verify" && repoVerify:
			said = slices.Insert(said, 0, "verify (the repo's verify kind)")
		default:
			said = append(said, fmt.Sprintf("%s (%s)", k, l.before[k]))
		}
	}
	if len(said) == 0 {
		said = []string{"nothing"}
	}
	fmt.Fprintf(l.a.stdout, "required: %s\n", strings.Join(said, ", "))
	return nil
}

// gate refuses the landing while the node is held (bm-holds), at either
// gate: nothing is pushed, and the lead runs bmesh held and waits for the
// release. Land run outside a node has no hold to read.
func (l *lander) gate(ctx context.Context) error {
	if l.node == nil {
		return nil
	}
	h, ok, err := mesh.GetLeadHold(ctx, l.a.client, l.node.String())
	if err != nil || !ok {
		return err
	}
	return fail(exitRefused, "%s; nothing is pushed: run bmesh held and end your turn, and run bmesh land again once the release wakes you", h.Line(time.Now()))
}

// mail peeks the node's inbox, holding and acking nothing: a message the
// operator sent with bmesh send refuses the landing, since it may change
// what landing needs, when it is unread or arrived after land started, read
// or not (a lead woken mid-land can't act on it before the push); anything
// else (a report, a reply, another node's message) is named on one line
// when say, so the lead sees it before the push. Land run outside a node
// has no inbox and skips it.
func (l *lander) mail(ctx context.Context, say bool) error {
	if l.node == nil {
		return nil
	}
	pending, err := mesh.Pending(ctx, l.a.client, l.node)
	if err != nil {
		return err
	}
	operator := 0
	var others []string
	for _, m := range pending {
		if m.Kind == "message" && m.From == "operator" {
			operator++
			continue
		}
		others = append(others, daemon.WakeItem(m))
	}
	if operator > 0 {
		return fail(exitRefused, "refused: %d unread message(s) from the operator; run bmesh inbox, act on them, then run bmesh land again", operator)
	}
	since, err := mesh.InboxAfter(ctx, l.a.client, l.node, l.mailSeq)
	if err != nil {
		return err
	}
	if n := len(slices.DeleteFunc(since, func(m mesh.Message) bool { return !fromOperator(m) })); n > 0 {
		return fail(exitRefused, "refused: %d message(s) from the operator since land started; nothing is pushed: "+
			"run bmesh inbox, act on them, then run bmesh land again", n)
	}
	if say && len(others) > 0 {
		fmt.Fprintf(l.a.stdout, "unread: %s\n", strings.Join(others, "; "))
	}
	return nil
}

// fromOperator reports a message the operator sent with bmesh send.
func fromOperator(m mesh.Message) bool { return m.Kind == "message" && m.From == "operator" }

// moved refuses a HEAD other than the one land waited on.
func (l *lander) moved(head string) error {
	return fail(exitRefused, "refused: HEAD moved while land waited (%.8s, was %.8s); nothing is pushed: run bmesh land again", head, l.waited)
}

// kind is the repo's kind name, when it has one.
func (l *lander) kind(name string) (kind.Kind, bool) {
	i := slices.IndexFunc(l.kinds, func(k kind.Kind) bool { return k.Name == name })
	if i < 0 {
		return kind.Kind{}, false
	}
	return l.kinds[i], true
}

// reviewEffort is the effort a review of a change of size gets, by bmesh's
// done rule: none for XS or S, medium for M, high for L or larger. ok is
// false for a size that is none of these.
func reviewEffort(size string) (effort string, ok bool) {
	switch size {
	case "XS", "S":
		return "", true
	case "M":
		return "medium", true
	}
	if l, ok := strings.CutSuffix(size, "L"); ok && strings.Trim(l, "X") == "" {
		return "high", true
	}
	return "", false
}

// sized is the size the change was built at, --size or the spec's own, and
// the effort its review gets; a spec with neither refuses.
func (l *lander) sized() (size, effort string, err error) {
	size = l.size
	if size == "" {
		specs, err := spec.Scan(l.top, l.a.cfg.Spec.Glob)
		if err != nil {
			return "", "", landFail(err)
		}
		s, ok := spec.Find(specs, l.id)
		if !ok {
			return "", "", fail(exitRefused, "refused: no spec %03d in %s", l.id, l.top)
		}
		size = s.Fields["size"]
		if size == "" {
			return "", "", fail(exitUsage, "land needs --size: spec %03d has no size", l.id)
		}
	}
	effort, ok := reviewEffort(size)
	if !ok {
		return "", "", fail(exitUsage, "land needs --size: spec %03d's size %q is none of XS, S, M or L", l.id, size)
	}
	return size, effort, nil
}

// waitJob is a job of the node's land waits for the reply to.
type waitJob struct {
	kind string
	mesh.QueuedJob
}

// review makes sure the branch's review is on its way when the spec's size
// calls for one: with the node's review job unanswered for a commit of the
// branch, it waits for that job; with a review result on the branch (spec
// 038's hand rebase included) it queues none, so a fixed failing review
// lands on verify alone; else it queues one on HEAD at the size's effort,
// or none for XS or S. It returns the jobs to wait for. Land run outside a
// node, or in a repo with no queued review kind, does nothing here.
func (l *lander) review(ctx context.Context) ([]waitJob, error) {
	k, ok := l.kind(reviewKind)
	if l.node == nil || !ok || k.Mode == "inline" {
		return nil, nil
	}
	queued, err := mesh.KindJobs(ctx, l.a.client, reviewKind)
	if err != nil {
		return nil, err
	}
	var mine []waitJob
	for _, j := range queued {
		if j.From == l.node.String() && !l.answered[j.ID] && onBranch(ctx, l.top, l.base, l.heads, j.Commit) {
			fmt.Fprintf(l.a.stdout, "review: waiting for job %s\n", j.ID)
			mine = append(mine, waitJob{reviewKind, j})
		}
	}
	if len(mine) > 0 {
		return mine, nil
	}
	prior, ok, err := l.a.priorReview(ctx, l.top)
	if err != nil {
		return nil, err
	}
	if ok {
		fmt.Fprintf(l.a.stdout, "review: on the branch already (%s at t:%.8s)\n", okWord(prior.OK), prior.Tree)
		return nil, nil
	}
	size, effort, err := l.sized()
	if err != nil {
		return nil, err
	}
	if effort == "" {
		fmt.Fprintf(l.a.stdout, "review: none (size %s)\n", size)
		return nil, nil
	}
	j, err := l.queueReview(ctx, false)
	if err != nil {
		return nil, err
	}
	return []waitJob{j}, nil
}

// queueReview queues the review kind on HEAD at the effort the change's size
// calls for, on the kind's first model, started at once, its text the
// branch's base, after pushing the branch so a daemon on any box can fetch
// the commit; again is the second pass after land's rebase changed the
// diff.
func (l *lander) queueReview(ctx context.Context, again bool) (waitJob, error) {
	k, _ := l.kind(reviewKind)
	size, effort, err := l.sized()
	if err != nil {
		return waitJob{}, err
	}
	if _, ok := l.a.cfg.Repos[l.repo]; !ok {
		return waitJob{}, fail(exitRefused, "refused: %s is not in the registry, so no daemon takes its review; nothing is pushed", l.repo)
	}
	head, err := gitIn(ctx, l.top, "rev-parse", "HEAD")
	if err != nil {
		return waitJob{}, err
	}
	base, err := gitIn(ctx, l.top, "merge-base", l.base, head)
	if err != nil {
		return waitJob{}, err
	}
	if l.origin {
		if _, err := gitIn(ctx, l.top, "push", "--quiet", "--force-with-lease", "origin", head+":refs/heads/"+l.branch); err != nil {
			fmt.Fprintf(l.a.stderr, "bmesh: push %s: %v; a daemon on another box can't fetch the commit\n", l.branch, err)
		} else {
			fmt.Fprintf(l.a.stdout, "push origin %s %.8s\n", l.branch, head)
		}
	}
	model := ""
	if k.IsModel() {
		model = k.ModelsList()[0]
	}
	j := mesh.Job{ID: newID(), Kind: reviewKind, Repo: l.repo, Commit: head, Model: model, Effort: effort,
		Text: "base: " + base, Go: true}
	if _, err := mesh.Queue(ctx, l.a.client, l.node, j, l.top); err != nil {
		return waitJob{}, err
	}
	if again {
		fmt.Fprintf(l.a.stdout, "review: queued %s again (the rebase changed the diff)\n", j.ID)
	} else {
		fmt.Fprintf(l.a.stdout, "review: queued %s (%s, size %s)\n", j.ID, effort, size)
	}
	return waitJob{reviewKind, mesh.QueuedJob{ID: j.ID, From: l.node.String(), Commit: head, State: "waiting"}}, nil
}

// onBranch reports whether commit is one of the branch's: an ancestor of
// one of heads (HEAD and the heads a hand rebase rewrote) that main
// (mainRef) doesn't have.
func onBranch(ctx context.Context, top, mainRef string, heads []string, commit string) bool {
	if commit == "" {
		return false
	}
	if _, err := gitIn(ctx, top, "merge-base", "--is-ancestor", commit, mainRef); err == nil {
		return false
	}
	return slices.ContainsFunc(heads, func(h string) bool {
		_, err := gitIn(ctx, top, "merge-base", "--is-ancestor", commit, h)
		return err == nil
	})
}

// headJobs lists the node's unanswered jobs on HEAD of the repo's queued
// kinds. A job on an earlier commit is not waited for: its result is not
// at HEAD's tree. Land run outside a node waits for none.
func (l *lander) headJobs(ctx context.Context) ([]waitJob, error) {
	if l.node == nil {
		return nil, nil
	}
	var out []waitJob
	for _, k := range l.kinds {
		if k.Mode == "inline" {
			continue
		}
		queued, err := mesh.KindJobs(ctx, l.a.client, k.Name)
		if err != nil {
			return nil, err
		}
		for _, j := range queued {
			if j.From == l.node.String() && !l.answered[j.ID] && j.Commit == l.waited {
				out = append(out, waitJob{k.Name, j})
			}
		}
	}
	return out, nil
}

// waitJobs waits for the reply to each job, naming each once, and acks each
// reply it reads so no wake follows. A job the node's jobs list as
// unanswered is answered after land started, so the replies are read from
// there on. An ok reply is done; a failing one
// refuses with its output (a review's findings); one that recorded nothing
// because its model run never answered is queued once more, and a second
// such, or any other that recorded nothing, refuses with its output.
func (l *lander) waitJobs(ctx context.Context, jobs []waitJob) error {
	out := l.a.stdout
	say := func(j waitJob) {
		where := j.State
		switch {
		case j.Host != "":
			where += " on " + j.Host
		case j.Holder != "":
			where += " on " + j.Holder
		}
		fmt.Fprintf(out, "waiting for job %s (%s, %s)\n", j.ID, j.kind, where)
	}
	for _, j := range jobs {
		say(j)
	}
	for {
		since, err := mesh.InboxAfter(ctx, l.a.client, l.node, l.mailSeq)
		if err != nil {
			return err
		}
		replies := map[string]*mesh.Reply{}
		for _, m := range since {
			if m.Kind == "reply" && m.Reply != nil {
				replies[m.Reply.Job] = m.Reply
			}
		}
		var left []waitJob
		for _, j := range jobs {
			r, ok := replies[j.ID]
			if !ok {
				left = append(left, j)
				continue
			}
			l.answered[j.ID] = true
			isReply := func(m mesh.Message) bool { return m.Kind == "reply" && m.Reply != nil && m.Reply.Job == j.ID }
			if _, _, err := mesh.FetchMatch(ctx, l.a.client, l.node, isReply); err != nil {
				return err
			}
			tree := r.Tree
			if len(tree) > 8 {
				tree = tree[:8]
			}
			output := r.Output
			if output != "" && !strings.HasSuffix(output, "\n") {
				output += "\n"
			}
			switch {
			case r.OK:
				fmt.Fprintf(out, "job %s (%s): ok t:%s\n", j.ID, j.kind, tree)
			case r.ResultKey != "":
				fmt.Fprint(out, output)
				return fail(exitRefused, "refused: %s failed at t:%s (job %s); nothing is pushed: fix what it found, commit and run bmesh land again",
					j.kind, tree, j.ID)
			case strings.HasPrefix(r.Output, instance.UnjudgedPrefix) && !l.requeued[j.ID]:
				again, err := l.requeue(ctx, j)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "job %s (%s): %s; queued %s once more\n", j.ID, j.kind, strings.TrimSpace(r.Output), again.ID)
				say(again)
				left = append(left, again)
			default:
				fmt.Fprint(out, output)
				return fail(exitRefused, "refused: job %s (%s) recorded nothing; nothing is pushed: see its output above, then run bmesh land again",
					j.ID, j.kind)
			}
		}
		if jobs = left; len(jobs) == 0 {
			return nil
		}
		ids := make([]string, len(jobs))
		for i, j := range jobs {
			ids[i] = fmt.Sprintf("job %s (%s)", j.ID, j.kind)
		}
		if err := l.pause(ctx, landPoll, strings.Join(ids, ", ")); err != nil {
			return err
		}
	}
}

// requeue queues job j once more as it was queued, under a new id.
func (l *lander) requeue(ctx context.Context, j waitJob) (waitJob, error) {
	job, ok, err := mesh.FindJob(ctx, l.a.client, j.kind, j.ID)
	if err != nil {
		return waitJob{}, err
	}
	if !ok {
		return waitJob{}, fail(exitRefused, "refused: job %s (%s) recorded nothing and the stream no longer holds it; run bmesh land again", j.ID, j.kind)
	}
	job.ID = newID()
	if _, err := mesh.Queue(ctx, l.a.client, l.node, job, l.top); err != nil {
		return waitJob{}, err
	}
	l.requeued[job.ID] = true
	return waitJob{j.kind, mesh.QueuedJob{ID: job.ID, From: l.node.String(), Commit: job.Commit, State: "waiting"}}, nil
}

// mainPushed waits while the main checkout's main has commits origin's
// doesn't (someone's unpushed work, which land never pushes), fetching
// again every landMainPoll; it says so once per count.
func (l *lander) mainPushed(ctx context.Context) error {
	if !l.origin {
		return nil
	}
	said := ""
	for {
		if _, err := gitIn(ctx, l.top, "fetch", "--quiet", "origin", "main"); err != nil {
			return err
		}
		n, err := gitIn(ctx, l.top, "rev-list", "--count", "origin/main..refs/heads/main")
		if err != nil || n == "0" {
			return nil
		}
		what := "main in " + l.repoPath + " to be pushed"
		if n != said {
			fmt.Fprintf(l.a.stdout, "waiting for %s: it has %s commits origin/main doesn't (someone's unpushed work; land never pushes it)\n", what, n)
			said = n
		}
		if err := l.pause(ctx, landMainPoll, what); err != nil {
			return err
		}
	}
}

// rebaseRE matches a reflog entry a finished rebase wrote on the branch
// (rebase, rebase -i, pull --rebase; "rebase finished" from older git).
var rebaseRE = regexp.MustCompile(`^(rebase|pull)\b[^:]*\(finish\): |^rebase finished: `)

// rebasedFrom reads the branch's reflog, newest first, for a rebase that
// finished at head and returns the head it rewrote (the entry just before
// it), and so on back while that head was itself made by a rebase: the
// heads land would have rebased itself, nearest first. A head with commits
// on top of the rebase, or a branch with no reflog, returns none.
func rebasedFrom(ctx context.Context, dir, branch, head string) []source {
	out, err := gitIn(ctx, dir, "reflog", "show", "--format=%H%x00%gs", "refs/heads/"+branch, "--")
	if err != nil {
		return nil
	}
	type entry struct{ sha, subject string }
	var log []entry
	for _, line := range strings.Split(out, "\n") {
		if sha, subject, ok := strings.Cut(line, "\x00"); ok {
			log = append(log, entry{sha, subject})
		}
	}
	var heads []source
	for i, cur := 0, head; ; {
		k := slices.IndexFunc(log[i:], func(e entry) bool { return e.sha == cur && rebaseRE.MatchString(e.subject) })
		if k < 0 || i+k+1 >= len(log) || log[i+k+1].sha == cur {
			return heads
		}
		i += k + 1
		heads = append(heads, source{head: log[i].sha, rebase: cur})
		cur = log[i].sha
	}
}

// land runs the steps under the lease, on the worktree land waited on
// (clean, HEAD the commit it read or its own rebase of it), starting again
// from the fetch when main moved before the push, while --wait lasts.
func (l *lander) land(ctx context.Context) error {
	if st, err := gitIn(ctx, l.top, "status", "--porcelain"); err != nil {
		return err
	} else if st != "" {
		return fail(exitRefused, "refused: HEAD moved while land waited: the worktree has changes not committed; nothing is pushed: commit and run bmesh land again")
	}
	if head, err := gitIn(ctx, l.top, "rev-parse", "HEAD"); err != nil {
		return err
	} else if head != l.waited {
		return l.moved(head)
	}
	for {
		moved, err := l.once(ctx)
		if err != nil || !moved {
			return err
		}
		if time.Now().After(l.deadline) {
			return fail(exitTimeout, "timed out waiting for main to stop moving; nothing is pushed")
		}
		fmt.Fprintln(l.a.stdout, "main moved; again from the fetch")
	}
}

// once is one pass from fetch to push; moved reports a push main refused
// because it moved, with the record commit taken back off the branch.
func (l *lander) once(ctx context.Context) (moved bool, err error) {
	out := l.a.stdout
	git := func(args ...string) (string, error) { return gitIn(ctx, l.top, args...) }
	// Cleanup runs on its own context: an interrupt or a lost lease cancels
	// ctx, and the worktree must still be left as land found it.
	cleanup := func(args ...string) (string, error) {
		c, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return gitIn(c, l.top, args...)
	}
	// Read again under the lease: the operator may have held or written
	// while land waited for it.
	if err := l.gate(ctx); err != nil {
		return false, err
	}
	if err := l.mail(ctx, false); err != nil {
		return false, err
	}
	// Fetches, and waits while the main checkout has unpushed commits.
	if err := l.mainPushed(ctx); err != nil {
		return false, err
	}
	baseSHA, err := git("rev-parse", l.base)
	if err != nil {
		return false, err
	}
	fmt.Fprintf(out, "fetch %s %s\n", l.base, baseSHA[:8])
	preTree, err := git("rev-parse", "HEAD^{tree}")
	if err != nil {
		return false, err
	}
	for _, src := range l.before {
		if src.base == "" {
			if src.base, err = git("merge-base", src.head, baseSHA); err != nil {
				return false, err
			}
		}
	}

	if _, err := git("merge-base", "--is-ancestor", baseSHA, "HEAD"); err == nil {
		fmt.Fprintf(out, "rebase: on %s already\n", baseSHA[:8])
	} else if err := l.rebase(ctx, baseSHA, cleanup); err != nil {
		return false, err
	} else {
		n, _ := git("rev-list", "--count", baseSHA+"..HEAD")
		fmt.Fprintf(out, "rebase onto %s: %s commits\n", baseSHA[:8], n)
	}
	if n, err := git("rev-list", "--count", baseSHA+"..HEAD"); err != nil {
		return false, err
	} else if n == "0" {
		return false, fail(exitRefused, "refused: nothing to land: HEAD is on %s", l.base)
	}
	merged, err := git("rev-parse", "HEAD")
	if err != nil {
		return false, err
	}
	tree, err := git("rev-parse", "HEAD^{tree}")
	if err != nil {
		return false, err
	}

	specs, err := spec.Scan(l.top, l.a.cfg.Spec.Glob)
	if err != nil {
		return false, landFail(err)
	}
	s, ok := spec.Find(specs, l.id)
	if !ok {
		return false, fail(exitRefused, "refused: no spec %03d in %s", l.id, l.top)
	}
	if s.Status() != "building" {
		return false, fail(exitRefused, "refused: spec %03d reads %s, not building", l.id, s.Status())
	}

	if err := l.scaffolded(ctx, baseSHA); err != nil {
		return false, err
	}

	entries, err := l.results(ctx, merged, tree, baseSHA, preTree)
	if err != nil {
		return false, err
	}

	rel, err := filepath.Rel(l.top, s.Path)
	if err != nil {
		return false, err
	}
	restore := func() { _, _ = cleanup("checkout", "HEAD", "--", rel) }
	done := []spec.Field{{Key: "merged", Value: merged}, {Key: "branch", Value: l.branch}}
	steps := spec.KeepBoxes(s.NeedsSteps(), l.a.rosterBoxes())
	old, _ := s.Steps()
	if len(steps) > 0 || len(old) > 0 { // a spec landed again keeps its ticks, or drops its steps
		v, err := spec.EncodeSteps(spec.CarryTicks(old, steps))
		if err != nil {
			return false, err
		}
		done = append(done, spec.Field{Key: spec.StepsKey, Value: v})
	}
	if _, err := spec.SetStatus(s, "done", []string{"building"}, done...); err != nil {
		restore()
		return false, ledgerFail(err)
	}
	if err := spec.WriteVerified(s.Path, entries); err != nil {
		restore()
		return false, err
	}
	fmt.Fprintf(out, "verified: %s\n", spec.FormatVerified(entries))
	fmt.Fprintf(out, "spec %03d done: merged %s, branch %s\n", l.id, merged[:8], l.branch)
	if len(steps) > 0 {
		fmt.Fprintf(out, "operator steps: %d (bmesh spec steps %03d)\n", len(steps), l.id)
	}
	msg := fmt.Sprintf("%03d: done at %s", l.id, merged[:8])
	if _, err := git("add", "--", rel); err != nil {
		restore()
		return false, err
	}
	if _, err := git("commit", "--quiet", "-m", msg); err != nil {
		_, _ = cleanup("reset", "--quiet", "--", rel)
		restore()
		return false, err
	}
	rec, err := git("rev-parse", "HEAD")
	if err != nil {
		return false, err
	}
	fmt.Fprintf(out, "commit %s %s\n", rec[:8], msg)
	undo := func() { _, _ = cleanup("reset", "--quiet", "--keep", "HEAD~1") }

	if l.origin {
		if _, err := git("push", "--quiet", "origin", "HEAD:refs/heads/main"); err != nil {
			undo()
			if l.mainMoved(ctx, baseSHA) {
				return true, nil
			}
			return false, fmt.Errorf("push main: %v", err)
		}
		fmt.Fprintf(out, "push origin main %s\n", rec[:8])
		// origin has it; the main checkout follows when it can.
		if err := l.a.landOnMain(ctx, l.repoPath, l.branch); err != nil {
			fmt.Fprintf(l.a.stderr, "bmesh: %v; origin/main has %s\n", err, rec[:8])
		} else {
			fmt.Fprintf(out, "main %s\n", rec[:8])
		}
	} else {
		if err := l.a.landOnMain(ctx, l.repoPath, l.branch); err != nil {
			undo()
			if l.mainMoved(ctx, baseSHA) {
				return true, nil
			}
			return false, err
		}
		fmt.Fprintf(out, "main %s\n", rec[:8])
	}
	l.landed = rec
	return false, nil
}

// landDriftLines is how much of the scaffold drift a refusal prints.
const landDriftLines = 20

// ownModuleRepo reports whether the checkout at repoPath is bmesh's own
// repo: its go.mod declares the module this binary was built from.
func ownModuleRepo(repoPath string) bool {
	bi, ok := readBuildInfo()
	return ok && bi != nil && bi.Main.Path != "" && config.ModulePath(filepath.Join(repoPath, "go.mod")) == bi.Main.Path
}

// scaffolded refuses a rebased tree whose scaffolded text the branch changed
// to read otherwise than scaffold writes it from the repo's ship row (see
// scaffold.Drift): the row's verify is the source of the verify kind's
// command and of AGENTS.md's blocks, and its setup of the repo config's
// [setup]. A repo with no ship row, or never
// scaffolded, is not checked. A tree of this binary's own module renders its
// own blocks, which this binary may not, so there only the verify kind is.
func (l *lander) scaffolded(ctx context.Context, baseSHA string) error {
	row, ok := l.a.cfg.Repos[l.repo]
	if !ok || row.Mode != "ship" {
		return nil
	}
	own := ownModuleRepo(l.top)
	at := func(rel string) (string, bool, error) {
		obj := baseSHA + ":" + rel
		if exec.CommandContext(ctx, "git", "-C", l.top, "cat-file", "-e", obj).Run() != nil {
			return "", false, nil
		}
		b, err := exec.CommandContext(ctx, "git", "-C", l.top, "cat-file", "blob", obj).Output()
		if err != nil {
			return "", false, fmt.Errorf("git cat-file %s: %w", obj, err)
		}
		return string(b), true, nil
	}
	diff, done, err := scaffold.Drift(l.top, row, buildermesh.AgentsSnippet, !own, at)
	if err != nil {
		return err
	}
	switch {
	case !done:
		fmt.Fprintln(l.a.stdout, "scaffold: not scaffolded, not checked")
		return nil
	case diff != "":
		lines := strings.SplitAfter(diff, "\n")
		if len(lines) > landDriftLines {
			lines = append(lines[:landDriftLines], "...\n")
		}
		fmt.Fprint(l.a.stdout, strings.Join(lines, ""))
		return fail(exitRefused, "refused: the branch's scaffolded files differ from what bmesh scaffold writes from %s's row; "+
			"the row's verify and setup are the source: bmesh repos propose with the new verify or setup, or restore the scaffolded files", l.repo)
	case own:
		fmt.Fprintln(l.a.stdout, "scaffold: the verify kind follows the row (AGENTS.md is rendered by this tree's own bmesh, not checked)")
	default:
		fmt.Fprintln(l.a.stdout, "scaffold: follows the row")
	}
	return nil
}

// mainMoved reports whether main (origin's, fetched again, when there is
// an origin) no longer points at baseSHA.
func (l *lander) mainMoved(ctx context.Context, baseSHA string) bool {
	if l.origin {
		if _, err := gitIn(ctx, l.top, "fetch", "--quiet", "origin", "main"); err != nil {
			return false
		}
	}
	now, err := gitIn(ctx, l.top, "rev-parse", l.base)
	return err == nil && now != baseSHA
}

// results makes sure the rebased tree carries every required kind: an
// inline kind runs (inline); another must have its own result there or one
// carried to it by patch-id (spec.ComposeVerified), unless the branch had an
// ok one before the rebase (land's, or the one by hand its source names)
// that recorded no patch-id and the rebase left the diff unchanged, which is
// carried. A review the rebase changed the diff of is errReviewAgain, for
// land to queue again, when land runs in a node of a repo with a queued
// review kind. It returns the verified entries for the tree, refusing a
// failing one.
func (l *lander) results(ctx context.Context, merged, tree, baseSHA, preTree string) ([]spec.Entry, error) {
	out := l.a.stdout
	if err := l.inline(ctx, merged, tree, preTree); err != nil {
		return nil, err
	}
	byName := map[string]kind.Kind{}
	for _, k := range l.kinds {
		byName[k.Name] = k
	}
	composeCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	entries, err := spec.ComposeVerified(composeCtx, l.a.client, l.top, "HEAD")
	if err != nil {
		return nil, err
	}
	for _, name := range l.required {
		if k, ok := byName[name]; ok && k.Mode == "inline" {
			continue
		}
		var found []spec.Entry
		for _, e := range entries {
			if e.Kind == name {
				found = append(found, e)
			}
		}
		for _, e := range found {
			if e.Carried != "" {
				fmt.Fprintf(out, "%s (carried from t:%s: the diff is unchanged)\n", e, e.Carried)
			} else {
				fmt.Fprintf(out, "%s (recorded)\n", e)
			}
		}
		if len(found) > 0 {
			continue
		}
		src := l.before[name]
		if src == nil {
			return nil, fail(exitRefused, "refused: %s has no result for t:%s; run it, then run bmesh land again", name, tree[:8])
		}
		if !src.result.OK {
			return nil, fail(exitRefused, "refused: %s failed at t:%s, before the rebase; fix what it found, commit and run bmesh land again",
				name, src.tree[:8])
		}
		// A model's result that recorded the diff it read and did not
		// carry read another diff than this one, whichever model's result
		// is the kind's worst.
		same := !slices.ContainsFunc(src.newest, func(r mesh.Result) bool { return r.PatchID != "" })
		if same {
			if same, err = samePatch(ctx, l.top, src.base, src.head, baseSHA, merged); err != nil {
				return nil, err
			}
		}
		if k, ok := byName[name]; !same && name == reviewKind && ok && k.Mode != "inline" && l.node != nil {
			fmt.Fprintf(out, "%s has a result for t:%s but none for t:%s, and the rebase changed the diff\n", name, src.tree[:8], tree[:8])
			return nil, errReviewAgain
		}
		if !same {
			return nil, fail(exitRefused, "refused: %s has a result for t:%s but none for t:%s, and the rebase changed the diff: "+
				"run it again on this tree (for review: bmesh routine queue review --again --go --text \"base: %s\", then routine wait), "+
				"then run bmesh land again", name, src.tree[:8], tree[:8], baseSHA)
		}
		carried, err := l.carry(ctx, src, tree)
		if err != nil {
			return nil, err
		}
		if len(carried) == 0 {
			fmt.Fprintf(out, "%s ok t:%s carried: the rebase left the diff unchanged (not on the verified line)\n", name, src.tree[:8])
		}
		for _, e := range carried {
			fmt.Fprintf(out, "%s (carried from t:%s: the rebase left the diff unchanged)\n", e, e.Carried)
		}
		entries = append(entries, carried...)
	}
	slices.SortFunc(entries, func(a, b spec.Entry) int {
		return cmp.Or(strings.Compare(a.Kind, b.Kind), strings.Compare(a.Model, b.Model))
	})
	if len(entries) == 0 {
		return nil, fail(exitRefused, "refused: %v", spec.ErrNoResults)
	}
	for _, e := range entries {
		if !e.OK {
			at := e.Tree8
			if e.Carried != "" {
				at = e.Carried + ", the same diff"
			}
			return nil, fail(exitRefused, "refused: %s fails at t:%s; fix it, commit and run bmesh land again", e.Kind, at)
		}
	}
	return entries, nil
}

// inline runs each required inline kind on merged (tree): a deterministic
// one is answered from its record, or carried to tree by carryCode from
// preTree (the branch's tree before the rebase), the last tree it was ok at
// in this run or its source's tree, and verify then by carryVerify from the
// code run of merged and of each head before the rebase. A kind that was ok
// at tree earlier in this run is not run again. A failing one refuses (exit
// 1).
func (l *lander) inline(ctx context.Context, merged, tree, preTree string) error {
	out := l.a.stdout
	for _, name := range l.required {
		k, ok := l.kind(name)
		if !ok || k.Mode != "inline" {
			continue
		}
		model := ""
		if k.IsModel() {
			model = k.ModelsList()[0]
		}
		line := spec.Entry{Kind: name, OK: true, Tree8: tree[:8], Model: model}.String()
		if l.ran[name] == tree {
			fmt.Fprintf(out, "%s (ran before the lease)\n", line)
			continue
		}
		if k.Deterministic {
			from := []string{l.ran[name], preTree}
			if src := l.before[name]; src != nil {
				from = append(from, src.tree)
			}
			old, err := l.carryCode(ctx, k, model, merged, tree, from)
			if err == nil && old == "" {
				old, err = l.a.carryVerify(ctx, name, l.top, l.repo, merged, tree, l.holder, model, nil, append([]string{merged}, l.heads...)...)
			}
			if err != nil {
				return err
			}
			if old != "" {
				fmt.Fprintf(out, "%s (%s)\n", line, carriedHow(old))
				l.ran[name] = tree
				continue
			}
		}
		ok, output, how, err := l.a.runInline(ctx, k, l.top, merged, tree, l.holder, model, "", "", nil, false)
		if err != nil {
			return err
		}
		line = spec.Entry{Kind: name, OK: ok, Tree8: tree[:8], Model: model}.String()
		if !ok {
			if output != "" && !strings.HasSuffix(output, "\n") {
				output += "\n"
			}
			fmt.Fprintf(out, "%s%s (%s)\n", output, line, how)
			if len(l.resolved) > 0 {
				return fail(exitError, "%s failed at t:%s, after the rebase resolved %s by rule: that is land's merge, and the branch is left rebased; "+
					"fix it forward, commit, push the branch, queue the review again (--again), then run bmesh land again",
					name, tree[:8], strings.Join(slices.Compact(slices.Sorted(slices.Values(l.resolved))), ", "))
			}
			return fail(exitError, "%s failed at t:%s; fix it, commit and run bmesh land again", name, tree[:8])
		}
		fmt.Fprintf(out, "%s (%s)\n", line, how)
		l.ran[name] = tree
	}
	return nil
}

// carryCode carries deterministic kind k's ok result to tree, which has no
// result of its own, from the first tree of from that has one and differs
// from tree only outside the repo's code (selfupdate.CodeSame of codePaths):
// it is stored again for tree and merged, so done and spec verified match it
// as any recorded result. It returns the tree it came from, "" when none
// carries.
func (l *lander) carryCode(ctx context.Context, k kind.Kind, model, merged, tree string, from []string) (string, error) {
	if _, ok, err := mesh.GetResult(ctx, l.a.client, mesh.ResultKey(k.Name, tree, nil, model)); err != nil || ok {
		return "", err
	}
	code := l.a.codePaths(l.repo)
	for _, old := range from {
		if old == "" || old == tree {
			continue
		}
		r, ok, err := mesh.GetResult(ctx, l.a.client, mesh.ResultKey(k.Name, old, nil, model))
		if err != nil {
			return "", err
		}
		if !ok || !r.OK {
			continue
		}
		same, err := selfupdate.CodeSame(l.top, old, tree, code)
		if err != nil {
			return "", err
		}
		if !same {
			continue
		}
		if stored, err := l.a.storeCarried(ctx, r, tree, merged, l.holder); err != nil || !stored {
			return "", err
		}
		return old, nil
	}
	return "", nil
}

// storeCarried stores r, run at another tree whose code is tree's, for tree
// (commit's) by by, its output led by `carried from t:<old8>: nothing under
// code changed`, only when tree has no result yet; false when it has one.
func (a *app) storeCarried(ctx context.Context, r mesh.Result, tree, commit, by string) (bool, error) {
	old := r.Tree
	r.Tree, r.Commit, r.By, r.At = tree, commit, by, ""
	r.Output = mesh.Truncate(fmt.Sprintf("%s%s: nothing under code changed\n%s", mesh.CarriedPrefix, old[:8], r.Output))
	return mesh.CreateResult(ctx, a.client, r)
}

// carryVerify answers kind name, when it is verify, for tree (commit's in
// dir), which has no result of its own under args and model, from the code
// run (selfupdate.CodeRun) of each of tips in turn: the nearest result run
// in it (mesh.NearestResult) carries when it is ok and its tree differs
// from tree only outside repo's code, and is stored for tree
// (storeCarried). A fail there carries nothing, from any tip: a fail over
// the same code is a flake or a file code misses, and an older ok must not
// hide it. It returns the tree the result came from, "" when none carries
// (or tree has a result, stored meanwhile included).
func (a *app) carryVerify(ctx context.Context, name, dir, repo, commit, tree, by, model string, args []string, tips ...string) (string, error) {
	if name != "verify" {
		return "", nil
	}
	if _, ok, err := mesh.GetResult(ctx, a.client, mesh.ResultKey(name, tree, args, model)); err != nil || ok {
		return "", err
	}
	code := a.codePaths(repo)
	for _, tip := range tips {
		run, err := selfupdate.CodeRun(dir, tip, code)
		if err != nil {
			return "", err
		}
		r, ok, err := mesh.NearestResult(ctx, a.client, name, run, args, model)
		if err != nil {
			return "", err
		}
		if !ok {
			continue
		}
		same, err := selfupdate.CodeSame(dir, r.Tree, tree, code)
		if err != nil {
			return "", err
		}
		if !same {
			continue
		}
		if !r.OK {
			return "", nil
		}
		if stored, err := a.storeCarried(ctx, r, tree, commit, by); err != nil || !stored {
			return "", err
		}
		return r.Tree, nil
	}
	return "", nil
}

// carriedHow is how a carried result came to be, after its verdict line.
func carriedHow(from string) string {
	return "carried from t:" + from[:8] + ": nothing under code changed"
}

// carry puts a kind's model results at its source's tree, which read the
// diff the rebase left unchanged and recorded no patch-id, on the line for
// tree as carried entries: each is stored again with the patch-id of the
// diff it read (the fork point to the source's head), so done matches it as
// it matches any carried entry. A result with no model has nothing for done
// to match and stays off the line.
func (l *lander) carry(ctx context.Context, src *source, tree string) ([]spec.Entry, error) {
	var out []spec.Entry
	patch := ""
	for _, r := range src.newest {
		if r.Model == "" || !r.OK || r.PatchID != "" {
			continue
		}
		if patch == "" {
			var err error
			if patch, err = checkout.PatchID(ctx, l.top, src.base, src.head); err != nil {
				return nil, err
			}
			if patch == "" {
				return nil, nil
			}
		}
		r.PatchID = patch
		if err := mesh.PutResult(ctx, l.a.client, r); err != nil {
			return nil, err
		}
		out = append(out, spec.Entry{Kind: r.Kind, OK: true, Tree8: tree[:8], Model: r.Model, Carried: src.tree[:8]})
	}
	return out, nil
}

// samePatch reports whether the diff a..b has the patch-id of c..d.
func samePatch(ctx context.Context, dir, a, b, c, d string) (bool, error) {
	x, err := checkout.PatchID(ctx, dir, a, b)
	if err != nil {
		return false, err
	}
	y, err := checkout.PatchID(ctx, dir, c, d)
	if err != nil {
		return false, err
	}
	return x == y, nil
}

// rebase rebases the branch onto baseSHA. At each stop on a conflict it
// merges every conflicted file's three sides again in diff3 style and
// resolves their regions by rule (internal/merge); when every region of
// every file resolves, it adds the files and continues, printing a
// resolved line per file. Anything left aborts the rebase, refused with
// the files a rule resolves and those that need the lead.
func (l *lander) rebase(ctx context.Context, baseSHA string, cleanup func(...string) (string, error)) error {
	n, err := gitIn(ctx, l.top, "rev-list", "--count", baseSHA+"..HEAD")
	if err != nil {
		return err
	}
	stops, _ := strconv.Atoi(n) // a commit stops the rebase once at most
	rerr := rebaseGit(ctx, l.top, "rebase", "--quiet", baseSHA)
	for ; rerr != nil && stops > 0; stops-- {
		files, left, err := l.resolveStop(ctx)
		if err != nil {
			_, _ = cleanup("rebase", "--abort")
			return err
		}
		names := slices.Sorted(maps.Keys(files))
		if len(names) == 0 || len(left) > 0 {
			l.resolved = append(l.resolved, names...)
			return l.abortRebase(cleanup, baseSHA, rerr, left)
		}
		for _, f := range names {
			fmt.Fprintf(l.a.stdout, "rebase: resolved %s (%s)\n", f, joinRules(files[f]))
		}
		l.resolved = append(l.resolved, names...)
		if _, err := gitIn(ctx, l.top, append([]string{"add", "--"}, names...)...); err != nil {
			return l.abortRebase(cleanup, baseSHA, err, nil)
		}
		rerr = rebaseGit(ctx, l.top, "rebase", "--continue")
	}
	if rerr != nil {
		return l.abortRebase(cleanup, baseSHA, rerr, nil)
	}
	return nil
}

// abortRebase aborts a stopped rebase and refuses the landing, naming the
// files that conflict and need the lead (left, or every unmerged file when
// nil) and those a rule resolved.
func (l *lander) abortRebase(cleanup func(...string) (string, error), baseSHA string, err error, left []string) error {
	if left == nil {
		files, _ := cleanup("diff", "--name-only", "--diff-filter=U")
		left = strings.Fields(files)
	}
	if _, aerr := cleanup("rebase", "--abort"); aerr != nil {
		return fmt.Errorf("rebase onto %s failed (%v) and did not abort: %v", baseSHA[:8], err, aerr)
	}
	what := "fails"
	if len(left) > 0 {
		what = "conflicts in " + strings.Join(left, ", ")
		if len(l.resolved) > 0 {
			slices.Sort(l.resolved)
			what += fmt.Sprintf(", which no rule resolves (a rule resolves %s)", strings.Join(slices.Compact(l.resolved), ", "))
		}
	}
	l.resolved = nil
	return fail(exitRefused, "refused: rebase onto %s %s; it is aborted and nothing is pushed. "+
		"Rebase yourself, resolve it, re-validate, then run bmesh land again", l.base, what)
}

// rebaseGit runs a git rebase step that may write a commit message, which
// keeps the commit's own.
func rebaseGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_EDITOR=true")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}

func joinRules(rs []merge.Rule) string {
	s := make([]string, len(rs))
	for i, r := range rs {
		s[i] = string(r)
	}
	return strings.Join(s, ", ")
}

// resolveStop resolves the conflicted files of a stopped rebase by rule:
// those whose every region resolves (files, with their rules) and those a
// rule can't resolve (left). A file is written only when nothing is left.
// A file is left when it lacks a side (added or deleted on one), isn't a
// regular file on all three, or is binary.
func (l *lander) resolveStop(ctx context.Context) (files map[string][]merge.Rule, left []string, err error) {
	out, err := exec.CommandContext(ctx, "git", "-C", l.top, "ls-files", "-u", "-z").Output()
	if err != nil {
		return nil, nil, fmt.Errorf("git ls-files -u: %v", err)
	}
	type stages struct {
		blob [4]string
		ok   bool
	}
	byPath := map[string]*stages{}
	var order []string
	for _, e := range strings.Split(string(out), "\x00") {
		meta, path, found := strings.Cut(e, "\t")
		f := strings.Fields(meta)
		if !found || len(f) != 3 {
			continue
		}
		st := byPath[path]
		if st == nil {
			st = &stages{ok: true}
			byPath[path] = st
			order = append(order, path)
		}
		n, _ := strconv.Atoi(f[2])
		if n < 1 || n > 3 || (f[0] != "100644" && f[0] != "100755") {
			st.ok = false
			continue
		}
		st.blob[n] = f[1]
	}
	dir, err := os.MkdirTemp("", "bmesh-land-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)
	files = map[string][]merge.Rule{}
	texts := map[string]string{}
	for _, path := range order {
		st := byPath[path]
		text, rules, ok := "", []merge.Rule(nil), st.ok && st.blob[1] != "" && st.blob[2] != "" && st.blob[3] != ""
		if ok {
			text, rules, ok, err = mergeStages(ctx, l.top, dir, path, st.blob[2], st.blob[1], st.blob[3])
			if err != nil {
				return nil, nil, err
			}
		}
		if !ok {
			left = append(left, path)
			continue
		}
		files[path], texts[path] = rules, text
	}
	if len(left) > 0 {
		return files, left, nil
	}
	for path, text := range texts {
		full := filepath.Join(l.top, path)
		mode := os.FileMode(0o644)
		if fi, err := os.Stat(full); err == nil {
			mode = fi.Mode().Perm()
		}
		if err := os.WriteFile(full, []byte(text), mode); err != nil {
			return nil, nil, err
		}
	}
	return files, nil, nil
}

// mergeStages merges a file's three sides (blobs) in diff3 style, with
// markers longer than any line of them starts with, and resolves the
// regions by rule. A side with a NUL byte (binary), or a merge git finds
// clean (what made the rebase stop is not in the text), is not resolved.
func mergeStages(ctx context.Context, top, dir, path, ours, base, theirs string) (string, []merge.Rule, bool, error) {
	var sides [3]string
	var names [3]string
	for i, b := range []string{ours, base, theirs} {
		text, err := exec.CommandContext(ctx, "git", "-C", top, "cat-file", "blob", b).Output()
		if err != nil {
			return "", nil, false, fmt.Errorf("git cat-file blob %s: %v", b, err)
		}
		if strings.IndexByte(string(text), 0) >= 0 {
			return "", nil, false, nil
		}
		sides[i] = string(text)
		names[i] = filepath.Join(dir, []string{"ours", "base", "theirs"}[i])
		if err := os.WriteFile(names[i], text, 0o600); err != nil {
			return "", nil, false, err
		}
	}
	marker := merge.MarkerSize(sides[:]...)
	cmd := exec.CommandContext(ctx, "git", "merge-file", "-p", "--diff3", "--marker-size="+strconv.Itoa(marker),
		"-L", "ours", "-L", "base", "-L", "theirs", names[0], names[1], names[2])
	text, err := cmd.Output()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return "", nil, false, nil
	case !errors.As(err, &ee) || ee.ExitCode() < 1 || ee.ExitCode() > 127:
		return "", nil, false, fmt.Errorf("git merge-file %s: %v", path, err)
	}
	out, rules, ok := merge.File(path, string(text), marker)
	// merge-file ends a region's last side with a newline the sides may not
	// have had: a file both sides end without one keeps it so.
	if ok && !strings.HasSuffix(sides[0], "\n") && !strings.HasSuffix(sides[2], "\n") {
		out = strings.TrimSuffix(out, "\n")
	}
	return out, rules, ok, nil
}
