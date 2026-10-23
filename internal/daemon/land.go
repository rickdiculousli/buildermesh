package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

// CanLand is the word a daemon's started event carries in can when it
// lands for its box's leads; bmesh ready refuses without it.
const CanLand = "land"

const (
	readyTTL     = 2 * time.Minute // the claim ready.<path>, renewed every third of it
	landWait     = "10m"           // land's --wait in a step
	refusedLines = 40              // how much of land's output a refusal keeps
	wakeGiveUp   = time.Hour       // how long an ending wake may fail before the request ends
	exitTimedOut = 5               // land's exit when its --wait passed
)

var (
	// stepPoll is how often a running step's watcher reads the request,
	// the lead's state, its mail and land's newest land event.
	stepPoll = 2 * time.Second
	// stepGrace is how long a cancelled step may clean up before it is
	// killed.
	stepGrace = 2 * time.Minute
)

// lands reports whether this daemon can run the land steps.
func (d *Deps) lands() bool { return d.RunStep != nil || d.Exe != "" }

// LandedText is the final wake of a lead whose change the daemon landed.
func LandedText(commit, specID, main string) string {
	return fmt.Sprintf("bmesh: landed %s, spec %s done; origin/main at %s contains it. "+
		"Write your closing line; nothing else to run.", short8(commit), specID, short8(main))
}

// RefusedText is the wake of a lead whose landing land refused: the
// refusal, land's last lines, and what to do.
func RefusedText(refusal, output string) string {
	s := "bmesh: land refused and needs you: " + refusal
	if output = strings.TrimSpace(output); output != "" {
		s += "\n\nland's last lines:\n" + output + "\n\n"
	} else {
		s += ". "
	}
	return s + "Fix it, commit, then run `bmesh ready` again."
}

// landers runs the land steps for this box's leads that asked for them
// (bmesh ready): one goroutine per request, under its claim.
type landers struct {
	d   Deps
	act *Activity

	mu      sync.Mutex
	running map[string]bool // the requests a goroutine of this daemon works
	wg      sync.WaitGroup
}

func newLanders(d Deps, act *Activity) *landers {
	return &landers{d: d, act: act, running: map[string]bool{}}
}

// landLoop runs a pass over the ready requests on every ready event, every
// message to an inbox (a release, or a message that withdraws a request)
// and every tick, until ctx ends and every step it started has ended.
func landLoop(ctx context.Context, d Deps, act *Activity, interval time.Duration) {
	l := newLanders(d, act)
	defer l.wg.Wait()
	kick := make(chan struct{}, 1)
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			ch, err := mesh.Follow(ctx, d.Client, "bm.>")
			if err != nil {
				d.Log.Warn("land: follow", "err", err)
				select {
				case <-ctx.Done():
				case <-time.After(followRetry):
				}
				continue
			}
			for s := range ch {
				if strings.HasSuffix(s.Subject, ".ready") || strings.HasPrefix(s.Subject, "bm.inbox.") {
					select {
					case kick <- struct{}{}:
					default:
					}
				}
			}
		}
	}()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		l.pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-kick:
		}
	}
}

// LandPass runs one pass of the lander and waits for the steps it started:
// what the land loop does on each tick.
func LandPass(ctx context.Context, d Deps, act *Activity) {
	d.defaults()
	l := newLanders(d, act)
	l.pass(ctx)
	l.wg.Wait()
}

// pass starts a goroutine for each request of a lead this box runs that
// none of this daemon's works. A request whose node is gone is deleted.
func (l *landers) pass(ctx context.Context) {
	d := l.d
	rs, err := mesh.ListReady(ctx, d.Client)
	if err != nil {
		d.Log.Warn("land: list ready", "err", err)
		return
	}
	for _, r := range rs {
		n, ok, err := mesh.GetNode(ctx, d.Client, r.Path)
		if err != nil {
			d.Log.Warn("land: get node", "path", r.Path, "err", err)
			continue
		}
		if !ok {
			if _, err := mesh.DeleteReady(ctx, d.Client, r.Path); err != nil {
				d.Log.Warn("land: delete ready", "path", r.Path, "err", err)
			}
			continue
		}
		if n.Host != d.Host || n.Runner.Thread == "" {
			continue
		}
		l.mu.Lock()
		if l.running[n.Path] {
			l.mu.Unlock()
			continue
		}
		l.running[n.Path] = true
		l.mu.Unlock()
		l.wg.Add(1)
		go func() {
			defer l.wg.Done()
			defer func() { l.mu.Lock(); delete(l.running, n.Path); l.mu.Unlock() }()
			l.work(ctx, n)
		}()
	}
}

// work takes the claim ready.<path> (a request another holds is left
// alone), renews it while it works, and derives and acts until a step
// leaves the request for a later pass. The claim is released at the end,
// on its own context, so a cancelled daemon gives it back.
func (l *landers) work(ctx context.Context, n mesh.Node) {
	d := l.d
	holder := d.from()
	key, err := mesh.AcquireLease(ctx, d.Client, "ready."+n.Path, holder, readyTTL, 1, time.Now())
	if errors.Is(err, mesh.ErrBusy) {
		return
	}
	if err != nil {
		d.Log.Warn("land: claim", "path", n.Path, "err", err)
		return
	}
	wctx, cancel := context.WithCancel(ctx)
	renewed := make(chan struct{})
	go func() {
		defer close(renewed)
		t := time.NewTicker(readyTTL / 3)
		defer t.Stop()
		for {
			select {
			case <-wctx.Done():
				return
			case <-t.C:
			}
			if err := mesh.RenewLease(wctx, d.Client, key, holder, readyTTL, time.Now()); err != nil && wctx.Err() == nil {
				d.Log.Warn("land: claim lost", "path", n.Path, "err", err)
				cancel()
				return
			}
		}
	}()
	defer func() {
		cancel()
		<-renewed
		rctx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err := mesh.ReleaseLease(rctx, d.Client, key, holder); err != nil && !errors.Is(err, mesh.ErrNotHolder) {
			d.Log.Warn("land: release claim", "path", n.Path, "err", err)
		}
	}()
	for i := 0; i < 8 && wctx.Err() == nil; i++ {
		if !l.step(wctx, n) {
			return
		}
	}
}

// step derives what the request needs from what exists, never from what
// an earlier pass meant to do (spec 059's rule), and does it: the wake an
// ended request still owes; done, for a change on origin/main; nothing, for
// a request withdrawn; held, for a lead held; else land. again reports a
// step after which the next is derived at once.
func (l *landers) step(ctx context.Context, n mesh.Node) (again bool) {
	d := l.d
	r, rev, ok, err := mesh.GetReady(ctx, d.Client, n.Path)
	if err != nil {
		d.Log.Warn("land: get ready", "path", n.Path, "err", err)
		return false
	}
	if !ok {
		return false
	}
	switch r.Phase {
	case mesh.PhaseLanded:
		l.finalWake(ctx, n, r, rev)
		return false
	case mesh.PhaseRefused:
		l.refusedWake(ctx, n, r, rev)
		return false
	}
	if landed, err := l.landedOnMain(ctx, n, r); err != nil {
		d.Log.Warn("land: read the spec on main", "path", n.Path, "err", err)
		return false
	} else if landed {
		return l.doneStep(ctx, n, r, rev)
	}
	st, srev, err := mesh.ReadState(ctx, d.Client, n.Path)
	if err != nil {
		d.Log.Warn("land: read state", "path", n.Path, "err", err)
		return false
	}
	if why, message := l.withdrawn(ctx, n, r, st); why != "" {
		if _, err := mesh.DeleteReady(ctx, d.Client, n.Path); err != nil {
			d.Log.Warn("land: delete ready", "path", n.Path, "err", err)
			return false
		}
		d.Log.Info("land: " + n.Path + " withdrawn: " + why)
		if message {
			l.wakeForMail(ctx, n)
		}
		return false
	}
	if _, held, err := mesh.GetLeadHold(ctx, d.Client, n.Path); err != nil {
		d.Log.Warn("land: read hold", "path", n.Path, "err", err)
		return false
	} else if held {
		if st.State == "landing" {
			if out, code, err := l.run(ctx, n.Worktree, "--node", n.Path, "held"); err != nil || code != 0 {
				d.Log.Warn("land: held", "path", n.Path, "code", code, "err", err, "out", strings.TrimSpace(out))
			}
		}
		return false
	}
	if st.State == "held" {
		// The release resumes the landing.
		if !l.writeLanding(ctx, n, srev, "released; landing") {
			return true
		}
	}
	return l.landStep(ctx, n, r, rev)
}

// withdrawn says why a request is withdrawn, "" when it is not: cancelled
// (bmesh ready --cancel), the node stopped, the lead acted itself (its
// state is neither landing nor held), or a message came after the request;
// message reports the last, which wakes the lead once the request is gone.
func (l *landers) withdrawn(ctx context.Context, n mesh.Node, r mesh.Ready, st mesh.State) (why string, message bool) {
	switch {
	case r.Phase == mesh.PhaseCancelled:
		return "cancelled", false
	case mesh.Stopped(st):
		return "stopped", false
	case st.State != "landing" && st.State != "held":
		return "the lead is " + st.State, false
	}
	if l.messageSince(ctx, n, r.Seq) {
		return "a message came", true
	}
	return "", false
}

// messageSince reports a message (bmesh send) in n's inbox after seq, read
// or not.
func (l *landers) messageSince(ctx context.Context, n mesh.Node, seq uint64) bool {
	p, err := mesh.ParsePath(n.Path)
	if err != nil {
		return false
	}
	ms, err := mesh.InboxAfter(ctx, l.d.Client, p, seq)
	if err != nil {
		l.d.Log.Warn("land: inbox", "path", n.Path, "err", err)
		return false
	}
	for _, m := range ms {
		if m.Kind == "message" {
			return true
		}
	}
	return false
}

// wakeForMail wakes a lead whose request a message withdrew, as the wake
// pass would.
func (l *landers) wakeForMail(ctx context.Context, n mesh.Node) {
	p, err := mesh.ParsePath(n.Path)
	if err != nil {
		return
	}
	unread, err := mesh.Unread(ctx, l.d.Client, p)
	if err != nil || unread == 0 {
		return
	}
	wake(ctx, l.d, l.act, n, unread, "")
}

// landedOnMain reports a request whose change is on main: the spec on
// origin/main reads done with a merged commit that is an ancestor of the
// worktree's HEAD (a daemon that died after the push finds it here).
func (l *landers) landedOnMain(ctx context.Context, n mesh.Node, r mesh.Ready) (bool, error) {
	if n.RepoPath == "" || !dirExists(n.Worktree) {
		return false, nil
	}
	done, err := spec.CheckDone(n.RepoPath, l.d.Cfg.Spec.Glob, r.Spec)
	if err != nil || !done.OK {
		return false, err
	}
	return gitOK(ctx, n.Worktree, "merge-base", "--is-ancestor", done.Merged, "HEAD"), nil
}

// doneStep records the landed change with bmesh done, its text the lead's
// done line and what the daemon read of origin/main after a fetch; done is
// idempotent, so a daemon that restarts after it runs it again harmlessly.
// The request turns landed, and the final wake follows.
func (l *landers) doneStep(ctx context.Context, n mesh.Node, r mesh.Ready, rev uint64) bool {
	d := l.d
	head, err := gitOut(ctx, n.Worktree, "rev-parse", "HEAD")
	if err != nil {
		d.Log.Warn("land: read HEAD", "path", n.Path, "err", err)
		return false
	}
	main, where := head, "on main"
	if gitOK(ctx, n.Worktree, "remote", "get-url", "origin") {
		if _, err := gitOut(ctx, n.Worktree, "fetch", "--quiet", "origin"); err != nil {
			d.Log.Warn("land: fetch", "path", n.Path, "err", err)
			return false
		}
		if main, err = gitOut(ctx, n.Worktree, "rev-parse", "origin/main"); err != nil {
			d.Log.Warn("land: read origin/main", "path", n.Path, "err", err)
			return false
		}
		where = "origin/main at " + short8(main)
		if gitOK(ctx, n.Worktree, "merge-base", "--is-ancestor", head, main) {
			where += " contains it"
		} else {
			where += " does not contain it"
		}
	}
	text := r.Text + "; landed " + short8(head) + ", " + where
	out, code, err := l.run(ctx, n.Worktree, "--node", n.Path, "done", "--commit", head, "--text", text)
	switch {
	case ctx.Err() != nil:
		return false
	case err != nil:
		d.Log.Warn("land: done", "path", n.Path, "err", err)
		return false
	case code == 3:
		return l.refuse(ctx, n, r, rev, "landed "+short8(head)+", but bmesh done refused: "+refusalLine(out), lastLines(out, refusedLines))
	case code != 0:
		d.Log.Warn("land: done", "path", n.Path, "code", code, "out", strings.TrimSpace(out))
		return false
	}
	d.Log.Info("land: " + n.Path + " landed " + short8(head) + ", " + where)
	r.Phase, r.Commit, r.Main = mesh.PhaseLanded, head, main
	nrev, err := mesh.UpdateReady(ctx, d.Client, r, rev)
	if errors.Is(err, mesh.ErrReadyMoved) {
		// Cancelled meanwhile: the change is on main all the same, so the
		// next derive finds it landed and records it.
		cur, crev, ok, gerr := mesh.GetReady(ctx, d.Client, n.Path)
		if gerr != nil || !ok {
			return false
		}
		cur.Phase, cur.Commit, cur.Main = mesh.PhaseLanded, head, main
		if nrev, err = mesh.UpdateReady(ctx, d.Client, cur, crev); err != nil {
			return errors.Is(err, mesh.ErrReadyMoved)
		}
		r = cur
	} else if err != nil {
		d.Log.Warn("land: update ready", "path", n.Path, "err", err)
		return false
	}
	l.finalWake(ctx, n, r, nrev)
	return false
}

// finalWake sends a landed lead its last wake, which asks for its closing
// line, and ends the request. A running thread is tried again next pass;
// a send that fails otherwise for wakeGiveUp is logged and dropped, since
// done is the record.
func (l *landers) finalWake(ctx context.Context, n mesh.Node, r mesh.Ready, rev uint64) {
	err := l.sendWake(ctx, n, LandedText(r.Commit, r.Spec, r.Main))
	if err != nil {
		if errors.Is(err, runner.ErrRunning) || !l.failing(ctx, n, r, rev, err) {
			return
		}
		l.d.Log.Warn("land: the final wake failed for an hour; dropped", "path", n.Path, "err", err)
	}
	if _, err := mesh.DeleteReady(ctx, l.d.Client, n.Path); err != nil {
		l.d.Log.Warn("land: delete ready", "path", n.Path, "err", err)
	}
}

// landStep runs land in the worktree, while HEAD is the request's head or
// land's own rebase of it, under a watcher that cancels it when the
// request is withdrawn and keeps the lead's landing text level with land's
// newest land event. Its exit decides: landed (derive again, which finds
// it on main), timed out (the next pass runs it again), held (derive
// again, which runs held), or refused (the refused wake).
func (l *landers) landStep(ctx context.Context, n mesh.Node, r mesh.Ready, rev uint64) bool {
	d := l.d
	if !dirExists(n.Worktree) {
		return l.refuse(ctx, n, r, rev, "the worktree "+n.Worktree+" is gone", "")
	}
	head, err := gitOut(ctx, n.Worktree, "rev-parse", "HEAD")
	if err != nil {
		d.Log.Warn("land: read HEAD", "path", n.Path, "err", err)
		return false
	}
	if head != r.Head && !l.rebaseOf(ctx, n.Worktree, head, r.Head) {
		return l.refuse(ctx, n, r, rev, fmt.Sprintf("HEAD moved since bmesh ready (%s, was %s); nothing is pushed", short8(head), short8(r.Head)), "")
	}
	args := []string{"--node", n.Path, "land", "--wait", landWait}
	if r.Size != "" {
		args = append(args, "--size", r.Size)
	}
	sctx, stop := context.WithCancel(ctx)
	defer stop()
	var why string
	watched := make(chan struct{})
	ended := make(chan struct{})
	since := time.Now()
	go func() {
		defer close(watched)
		t := time.NewTicker(stepPoll)
		defer t.Stop()
		for {
			select {
			case <-ended:
				return
			case <-t.C:
			}
			if w := l.stepWithdrawn(sctx, n, r, since); w != "" {
				why = w
				stop()
				return
			}
		}
	}()
	d.Log.Info("land: " + n.Path + " landing " + short8(head))
	out, code, err := l.run(sctx, n.Worktree, args...)
	close(ended)
	<-watched
	switch {
	case ctx.Err() != nil:
		return false
	case why != "":
		d.Log.Info("land: " + n.Path + " step cancelled: " + why)
		return true
	case err != nil:
		d.Log.Warn("land: land", "path", n.Path, "err", err)
		return false
	case code == 0:
		return true
	case code == exitTimedOut:
		return false
	}
	if _, held, err := mesh.GetLeadHold(ctx, d.Client, n.Path); err == nil && held {
		return true
	}
	if st, _, err := mesh.ReadState(ctx, d.Client, n.Path); err == nil {
		if w, _ := l.withdrawn(ctx, n, r, st); w != "" {
			return true
		}
	}
	return l.refuse(ctx, n, r, rev, refusalLine(out), lastLines(out, refusedLines))
}

// stepWithdrawn is one look of a running step's watcher: why the step is
// cancelled (the request gone or cancelled, the node stopped, the lead
// acting itself, a message), "" when it runs on; meanwhile it writes land's
// newest land event since the step began as the lead's landing text.
func (l *landers) stepWithdrawn(ctx context.Context, n mesh.Node, r mesh.Ready, since time.Time) string {
	d := l.d
	cur, _, ok, err := mesh.GetReady(ctx, d.Client, n.Path)
	if err != nil {
		return ""
	}
	if !ok {
		return "the request is gone"
	}
	st, srev, err := mesh.ReadState(ctx, d.Client, n.Path)
	if err != nil {
		return ""
	}
	if why, _ := l.withdrawn(ctx, n, cur, st); why != "" {
		return why
	}
	if st.State != "landing" {
		return ""
	}
	p, err := mesh.ParsePath(n.Path)
	if err != nil {
		return ""
	}
	s, ok, err := mesh.Last(ctx, d.Client, mesh.EventSubject(p, "land"))
	if err != nil || !ok || s.At.Before(since) {
		return ""
	}
	if ev, err := decodeEvent(s.Data); err == nil && ev.Text != "" && ev.Text != st.Text {
		l.writeLanding(ctx, n, srev, ev.Text)
	}
	return ""
}

// writeLanding writes the lead's state landing with text at srev, the
// revision the daemon read, and publishes the daemon's landing event; a
// state that moved meanwhile (the lead's own event) is left alone. It
// reports whether it wrote.
func (l *landers) writeLanding(ctx context.Context, n mesh.Node, srev uint64, text string) bool {
	d := l.d
	now := d.Now().UTC().Format(time.RFC3339)
	if _, err := mesh.WriteStateAt(ctx, d.Client, n.Path, srev, mesh.State{State: "landing", Text: text, By: "daemon", At: now}); err != nil {
		if !errors.Is(err, mesh.ErrStateMoved) {
			d.Log.Warn("land: write state", "path", n.Path, "err", err)
		}
		return false
	}
	ev := mesh.Event{From: d.from(), Event: "landing", To: n.Path, Text: text, At: now}
	if err := mesh.PublishEvent(ctx, d.Client, ev); err != nil {
		d.Log.Warn("land: publish", "path", n.Path, "err", err)
	}
	return true
}

// refuse turns the request refused, with land's refusal and last lines,
// and sends the wake that carries them.
func (l *landers) refuse(ctx context.Context, n mesh.Node, r mesh.Ready, rev uint64, refusal, output string) bool {
	r.Phase, r.Refusal, r.Output = mesh.PhaseRefused, refusal, output
	nrev, err := mesh.UpdateReady(ctx, l.d.Client, r, rev)
	if errors.Is(err, mesh.ErrReadyMoved) {
		return true
	}
	if err != nil {
		l.d.Log.Warn("land: update ready", "path", n.Path, "err", err)
		return false
	}
	l.d.Log.Info("land: " + n.Path + " refused: " + refusal)
	l.refusedWake(ctx, n, r, nrev)
	return false
}

// refusedWake wakes the lead at once, past WakeEvery, with land's
// refusal; once delivered it ends the request and writes the lead working
// with the refusal, so silence applies as to any working lead. A running
// thread is tried again next pass; a send that fails otherwise for
// wakeGiveUp ends the request and blocks the lead.
func (l *landers) refusedWake(ctx context.Context, n mesh.Node, r mesh.Ready, rev uint64) {
	d := l.d
	err := l.sendWake(ctx, n, RefusedText(r.Refusal, r.Output))
	if errors.Is(err, runner.ErrRunning) {
		d.Log.Info("land: refused wake not delivered, thread is running", "path", n.Path)
		return
	}
	if err != nil && !l.failing(ctx, n, r, rev, err) {
		return
	}
	if _, derr := mesh.DeleteReady(ctx, d.Client, n.Path); derr != nil {
		d.Log.Warn("land: delete ready", "path", n.Path, "err", derr)
		return
	}
	st, srev, rerr := mesh.ReadState(ctx, d.Client, n.Path)
	if rerr != nil || st.State != "landing" {
		return
	}
	now := d.Now().UTC().Format(time.RFC3339)
	if err != nil {
		text := "land refused, and the wake carrying it failed for an hour: " + r.Refusal
		if _, werr := mesh.WriteStateAt(ctx, d.Client, n.Path, srev, mesh.State{State: "blocked", Text: text, By: "daemon", At: now}); werr != nil {
			return
		}
		if perr := mesh.PublishEvent(ctx, d.Client, mesh.Event{From: n.Path, Event: "blocked", Text: text, At: now}); perr != nil {
			d.Log.Warn("land: publish", "path", n.Path, "err", perr)
		}
		return
	}
	text := "land refused: " + r.Refusal
	if _, werr := mesh.WriteStateAt(ctx, d.Client, n.Path, srev, mesh.State{State: "working", Text: text, By: "daemon", At: now}); werr != nil {
		return
	}
	if perr := mesh.PublishEvent(ctx, d.Client, mesh.Event{From: d.from(), Event: "land-refused", To: n.Path, Text: r.Refusal, At: now}); perr != nil {
		d.Log.Warn("land: publish", "path", n.Path, "err", perr)
	}
}

// failing records the first failure of an ending wake on the request and
// reports whether failures have lasted wakeGiveUp.
func (l *landers) failing(ctx context.Context, n mesh.Node, r mesh.Ready, rev uint64, err error) bool {
	d := l.d
	d.Log.Warn("land: wake", "path", n.Path, "thread", n.Runner.Thread, "err", err)
	if since, perr := time.Parse(time.RFC3339, r.FailingSince); perr == nil {
		return d.Now().Sub(since) >= wakeGiveUp
	}
	r.FailingSince = d.Now().UTC().Format(time.RFC3339)
	if _, err := mesh.UpdateReady(ctx, d.Client, r, rev); err != nil && !errors.Is(err, mesh.ErrReadyMoved) {
		d.Log.Warn("land: update ready", "path", n.Path, "err", err)
	}
	return false
}

// sendWake sends text to n's thread, past WakeEvery, and on delivery
// records it as wake does.
func (l *landers) sendWake(ctx context.Context, n mesh.Node, text string) error {
	wakeMu.Lock()
	defer wakeMu.Unlock()
	return deliverWake(ctx, l.d, l.act, n, text)
}

// rebaseOf reports whether head is a rebase (land's own, or a chain of
// them) of want on the worktree's branch.
func (l *landers) rebaseOf(ctx context.Context, dir, head, want string) bool {
	branch, err := gitOut(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "HEAD" {
		return false
	}
	for _, r := range checkout.RebasedFrom(ctx, dir, branch, head) {
		if r.Head == want {
			return true
		}
	}
	return false
}

// run runs `bmesh <args>` in dir with this binary on the daemon's config
// (the test hook RunStep when set): its output, exit code, and an error
// only when it could not run.
func (l *landers) run(ctx context.Context, dir string, args ...string) (string, int, error) {
	if l.d.RunStep != nil {
		return l.d.RunStep(ctx, dir, args...)
	}
	return execStep(ctx, l.d.Exe, l.d.Cfg.Path(), dir, args)
}

// execStep runs exe in dir as execFix does. Cancelled, the step is sent
// an interrupt, so land cleans up on its own context, and killed after
// stepGrace.
func execStep(ctx context.Context, exe, config, dir string, args []string) (string, int, error) {
	if exe == "" {
		return "", -1, fmt.Errorf("no bmesh binary path")
	}
	if config != "" {
		args = append([]string{"--config", config}, args...)
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	cmd.Cancel = func() error {
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = stepGrace
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(out), ee.ExitCode(), nil
	}
	if err != nil {
		return string(out), -1, err
	}
	return string(out), 0, nil
}

// refusalLine is a command's refusal: its last `bmesh: ` line without the
// prefix, else its last line.
func refusalLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), "bmesh: "); ok {
			return s
		}
	}
	if s := strings.TrimSpace(lines[len(lines)-1]); s != "" {
		return s
	}
	return "land exited without saying why"
}

// lastLines is out's last n lines.
func lastLines(out string, n int) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func short8(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func dirExists(dir string) bool {
	if dir == "" {
		return false
	}
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

func gitOK(ctx context.Context, dir string, args ...string) bool {
	return exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Run() == nil
}
