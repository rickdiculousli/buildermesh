package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/proc"
)

const leaseUsage = "usage: bmesh lock-lease acquire <slot> [--ttl D] | lock-lease renew <key> [--ttl D] | " +
	"lock-lease release <key> | lock-lease run <slot> [--ttl D] -- <cmd...>"

const (
	defaultTTL = 10 * time.Minute
	minTTL     = 3 * time.Second // renewals run every ttl/3
)

// leaseSignals delivers the signals that interrupt `lock-lease run`; tests replace it.
var leaseSignals = func() (<-chan os.Signal, func()) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	return c, func() { signal.Stop(c) }
}

func (a *app) cmdLease(args []string) error {
	if len(args) == 0 {
		return fail(exitUsage, leaseUsage)
	}
	sub := args[0]
	if sub != "acquire" && sub != "renew" && sub != "release" && sub != "run" {
		return fail(exitUsage, leaseUsage)
	}
	fs := a.flags("lease " + sub)
	var ttl *time.Duration
	if sub != "release" {
		ttl = fs.Duration("ttl", defaultTTL, "how long the lease lasts unrenewed")
	}
	pos, err := parse(fs, args[1:])
	if err != nil {
		return err
	}
	if ttl != nil && *ttl < minTTL {
		return fail(exitUsage, "lease %s: --ttl must be at least %s", sub, minTTL)
	}
	switch {
	case sub == "run" && len(pos) >= 2 && kind.NameRE.MatchString(pos[0]):
		return a.leaseRun(pos[0], pos[1:], *ttl)
	case sub == "run" || len(pos) != 1:
		return fail(exitUsage, leaseUsage)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	switch sub {
	case "acquire":
		key, _, err := a.acquire(ctx, pos[0], *ttl)
		if err != nil {
			return err
		}
		fmt.Fprintln(a.stdout, key)
		return nil
	case "renew":
		holder, err := a.identityOrHost(ctx, "lock-lease")
		if err != nil {
			return err
		}
		return leaseErr(mesh.RenewLease(ctx, a.client, pos[0], holder, *ttl, time.Now()), pos[0])
	default:
		holder, err := a.identityOrHost(ctx, "lock-lease")
		if err != nil {
			return err
		}
		return leaseErr(mesh.ReleaseLease(ctx, a.client, pos[0], holder), pos[0])
	}
}

func leaseErr(err error, key string) error {
	if errors.Is(err, mesh.ErrNotHolder) {
		return fail(exitRefused, "lease %s is not held by you", key)
	}
	return err
}

// acquire takes a place in slot for this node (or host) and returns the key
// and the holder.
func (a *app) acquire(ctx context.Context, slot string, ttl time.Duration) (string, string, error) {
	if !kind.NameRE.MatchString(slot) {
		return "", "", fail(exitUsage, "invalid slot %q", slot)
	}
	capacity, err := slotCapacity(slot)
	if err != nil {
		return "", "", err
	}
	holder, err := a.identityOrHost(ctx, "lock-lease")
	if err != nil {
		return "", "", err
	}
	key, err := a.acquireAs(ctx, slot, holder, ttl, capacity)
	return key, holder, err
}

// acquireAs takes a place in slot for holder; a full slot is refused (exit 3).
func (a *app) acquireAs(ctx context.Context, slot, holder string, ttl time.Duration, capacity int) (string, error) {
	key, err := mesh.AcquireLease(ctx, a.client, slot, holder, ttl, capacity, time.Now())
	if errors.Is(err, mesh.ErrBusy) {
		return "", fail(exitRefused, "slot %s is busy", slot)
	}
	return key, err
}

// slotCapacity reads the slot's capacity from the cwd's repo config; 1 when
// the cwd is not in a repo or the repo does not list the slot.
func slotCapacity(slot string) (int, error) {
	top, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return 1, nil
	}
	rc, err := kind.LoadRepoConfig(strings.TrimSpace(string(top)))
	if err != nil {
		return 0, err
	}
	if n := rc.Slots[slot]; n > 0 {
		return n, nil
	}
	return 1, nil
}

// leaseRun holds a lease on slot while argv runs, waiting its turn for it
// as withLeases does. The command runs in its own process group so the
// whole group can be killed when the lease is lost, yielded or the run
// interrupted; otherwise the command's exit code is returned.
func (a *app) leaseRun(slot string, argv []string, ttl time.Duration) error {
	ctx, cancel := cmdCtx()
	holder, err := a.identityOrHost(ctx, "lock-lease")
	cancel()
	if err != nil {
		return err
	}
	return a.withLeases([]string{slot}, holder, ttl, slotCapacity, a.roleWait(a.stderr), func(ctx context.Context) error {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = a.stdin, a.stdout, a.stderr
		proc.Group(cmd)
		cmd.Cancel = func() error { return proc.KillGroup(cmd) }
		cmd.WaitDelay = time.Second // a killed command's children may hold its output open
		err := cmd.Run()
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code := ee.ExitCode()
			if code < 0 {
				code = exitError
			}
			return &exitErr{code: code}
		}
		return err
	})
}

// leaseWait is how a run waits for its slots: its priority, set by role
// and never by a flag, and where it says that it waits.
type leaseWait struct {
	prio      mesh.Priority
	why       string                // an urgent wait's reason, named in the yield it asks of a best-effort holder; "" asks none
	out       io.Writer             // where each new waiting line goes
	say       func(text string)     // the run's status line, given each new waiting line and "" once it holds; nil: none
	check     func(by string) error // called with the holder before each pause; an error ends the wait
	deadline  time.Time             // given up at (exit 5); zero: none
	preempted string                // who made this run yield, while it waits again
	said      bool                  // say was given a line since the run last held its slots
}

// roleWait is an inline run's wait outside land: urgent from the operator
// node, best effort from any other.
func (a *app) roleWait(out io.Writer) leaseWait {
	if a.givenNode() == "operator" {
		return leaseWait{prio: mesh.Urgent, why: "the operator's run", out: out}
	}
	return leaseWait{prio: mesh.BestEffort, out: out}
}

// waiterTTL is how long a waiter's place outlasts its last poll, so a
// waiter that died drops out of line. Tests shorten it.
var waiterTTL = 30 * time.Second

// acquireWait takes a place in slot (capacity places) for holder, waiting
// its turn: it records holder as a waiter beside the slot's leases every
// landPoll, and takes a place once one is free for it in the waiters'
// order (urgent first, then oldest first). An urgent wait with every place
// held marks the best-effort holder that started last to yield (one per
// urgent waiter ahead of it and itself). Each new line of the wait is
// printed and said; check runs before each pause, and past the deadline
// it gives up (exit 5). The waiter's record goes when it returns.
func (a *app) acquireWait(slot, holder string, ttl time.Duration, capacity int, w *leaseWait) (string, error) {
	since, said := time.Now(), ""
	defer func() {
		ctx, cancel := cmdCtx()
		defer cancel()
		mesh.DeleteWaiter(ctx, a.client, slot, holder) // one that outlives a failure expires
	}()
	for {
		ctx, cancel := cmdCtx()
		now := time.Now()
		err := mesh.PutWaiter(ctx, a.client, slot, mesh.Waiter{Holder: holder, Priority: w.prio, Why: w.why, Since: since}, waiterTTL, now)
		var line mesh.SlotLine
		if err == nil {
			line, err = mesh.SlotQueue(ctx, a.client, slot, capacity, now)
		}
		if err != nil {
			cancel()
			return "", err
		}
		place := slices.IndexFunc(line.Waiters, func(x mesh.Waiter) bool { return x.Holder == holder })
		if place < 0 {
			place = len(line.Waiters) // expired between the write and the read: last until the next poll
		}
		text, by := waitLine(slot, line, place+1, w.preempted)
		if place < line.Free {
			key, err := mesh.AcquireRanked(ctx, a.client, slot, holder, w.prio, ttl, capacity, time.Now())
			cancel()
			if errors.Is(err, mesh.ErrBusy) {
				continue // taken since the read: read again at once
			}
			if err == nil && w.preempted != "" {
				if said == "" {
					w.tell(text)
				}
				w.preempted = ""
			}
			return key, err
		}
		if w.prio == mesh.Urgent && w.why != "" && line.Free == 0 {
			err = askYield(ctx, a.client, line, place, holder+" ("+w.why+")")
		}
		cancel()
		if err != nil {
			return "", err
		}
		if text != said {
			w.tell(text)
			said = text
		}
		if w.check != nil {
			if err := w.check(by); err != nil {
				return "", err
			}
		}
		if !w.deadline.IsZero() && time.Now().After(w.deadline) {
			return "", fail(exitTimeout, "timed out waiting for %s, held by %s; nothing is pushed: run bmesh land again", slot, by)
		}
		time.Sleep(landPoll)
	}
}

// tell prints a line of the wait and says it.
func (w *leaseWait) tell(text string) {
	fmt.Fprintln(w.out, text)
	if w.say != nil {
		w.say(text)
		w.said = true
	}
}

// waitLine is a wait's line at place in line, and who holds the slot
// ("someone" when no place is held): "waiting for <slot> (held by
// <holder>, <priority>); #N", or, waiting again after a yield,
// "preempted by <holder> (<why>); waiting again at #N".
func waitLine(slot string, line mesh.SlotLine, place int, preempted string) (text, by string) {
	by, held := "someone", ""
	if len(line.Held) > 0 {
		// shortcut: names the first place's holder only; name each when a slot of capacity > 1 needs it.
		h := line.Held[0]
		by, held = h.Holder, " (held by "+h.Holder
		if p := h.Priority.Word(); p != "" {
			held += ", " + p
		}
		held += ")"
	}
	if preempted != "" {
		return fmt.Sprintf("preempted by %s; waiting again at #%d", preempted, place), by
	}
	return fmt.Sprintf("waiting for %s%s; #%d", slot, held, place), by
}

// askYield marks the best-effort holder of line that started last to yield
// to by, the urgent waiter at place, unless the places already marked
// cover every urgent waiter up to it.
func askYield(ctx context.Context, c *mesh.Client, line mesh.SlotLine, place int, by string) error {
	ahead := 0
	for _, w := range line.Waiters[:min(place, len(line.Waiters))] {
		if w.Priority == mesh.Urgent {
			ahead++
		}
	}
	var pick *mesh.Lease
	marked := 0
	for i, l := range line.Held {
		switch {
		case l.Yield != "":
			marked++
		case l.Priority == mesh.BestEffort && (pick == nil || l.Started.After(pick.Started)):
			pick = &line.Held[i]
		}
	}
	if pick == nil || marked > ahead {
		return nil
	}
	return mesh.AskYield(ctx, c, *pick, by)
}

// withLeases waits for a place in each slot for holder, in turn (capacity
// per slot from capacity; see acquireWait), then runs body, renewing every
// lease each ttl/3. A failed wait releases the places already taken. A
// failed renew cancels body's context and exits 1 "lease lost"; SIGINT or
// SIGTERM cancels it, releases the leases and exits 1 "interrupted". A
// best-effort run asked to yield has body's context cancelled (its
// child's process group killed), releases its leases and waits again as
// urgent, so it yields at most once, then runs body again; otherwise the
// leases are released and body's error is returned.
func (a *app) withLeases(slots []string, holder string, ttl time.Duration, capacity func(slot string) (int, error),
	w leaseWait, body func(ctx context.Context) error) error {
	for {
		var keys []string
		for _, slot := range slots {
			n, err := capacity(slot)
			if err == nil {
				var key string
				if key, err = a.acquireWait(slot, holder, ttl, n, &w); err == nil {
					keys = append(keys, key)
					continue
				}
			}
			a.releaseLeases(keys, holder)
			return err
		}
		if w.said {
			w.say("")
			w.said = false
		}
		err := a.holdLeases(keys, holder, ttl, w.prio == mesh.BestEffort, body)
		var y *yielded
		if !errors.As(err, &y) {
			return err
		}
		w.prio, w.why, w.preempted = mesh.Urgent, "", y.by
	}
}

// yielded is holdLeases giving its leases up to an urgent waiter, by.
type yielded struct{ by string }

func (y *yielded) Error() string { return "preempted by " + y.by }

// releaseLeases gives back keys held by holder; the first failure is
// returned after every key was tried.
func (a *app) releaseLeases(keys []string, holder string) error {
	ctx, cancel := cmdCtx()
	defer cancel()
	var first error
	for _, key := range keys {
		if err := mesh.ReleaseLease(ctx, a.client, key, holder); err != nil && first == nil {
			first = leaseErr(err, key)
		}
	}
	return first
}

// holdLeases runs body while holder holds keys, as withLeases describes,
// and releases them after. A preemptable holder reads its leases every
// landPoll for a yield asked of it; asked, it returns *yielded.
func (a *app) holdLeases(keys []string, holder string, ttl time.Duration, preemptable bool, body func(ctx context.Context) error) error {
	release := func() error { return a.releaseLeases(keys, holder) }
	sigs, stopSigs := leaseSignals()
	defer stopSigs()
	runCtx, kill := context.WithCancel(context.Background())
	defer kill()
	var lost, interrupted atomic.Bool
	yieldTo := "" // written before watched closes
	stop := make(chan struct{})
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		t := time.NewTicker(ttl / 3)
		defer t.Stop()
		var polls <-chan time.Time
		if preemptable {
			p := time.NewTicker(landPoll)
			defer p.Stop()
			polls = p.C
		}
		for {
			select {
			case <-stop:
				return
			case <-polls:
				ctx, cancel := cmdCtx()
				for _, key := range keys {
					if by, err := mesh.LeaseYield(ctx, a.client, key, holder); err == nil && by != "" {
						yieldTo = by
						break
					}
				}
				cancel()
				if yieldTo != "" {
					kill()
					return
				}
			case <-sigs:
				interrupted.Store(true)
				kill()
				return
			case <-t.C:
				ctx, cancel := cmdCtx()
				for _, key := range keys {
					if err := mesh.RenewLease(ctx, a.client, key, holder, ttl, time.Now()); err != nil {
						lost.Store(true)
						break
					}
				}
				cancel()
				if lost.Load() {
					kill()
					return
				}
			}
		}
	}()
	runErr := body(runCtx)
	close(stop)
	<-watched
	if lost.Load() {
		release() // the places still held; the lost one is someone else's now
		return fail(exitError, "lease lost")
	}
	if err := release(); err != nil {
		return err
	}
	if yieldTo != "" && !interrupted.Load() {
		return &yielded{by: yieldTo}
	}
	if interrupted.Load() {
		return fail(exitError, "interrupted")
	}
	return runErr
}

// stateSay puts a run's wait on its node's status line, as mesh-status
// shows it: while the node is working, its text is the wait's newest line,
// and goes back to what it was once the run holds its slots, unless
// someone wrote the state since. A path with no working state is left
// alone.
func (a *app) stateSay(path string) func(text string) {
	var orig *mesh.State
	var rev uint64
	return func(text string) {
		ctx, cancel := cmdCtx()
		defer cancel()
		st, now, err := mesh.ReadState(ctx, a.client, path)
		if err != nil || st.State != "working" || orig != nil && now != rev || orig == nil && text == "" {
			return
		}
		if orig == nil {
			orig = &st
		}
		to := mesh.State{State: st.State, Text: text, By: st.By}
		if text == "" {
			to = *orig
		}
		if rev, err = mesh.WriteStateAt(ctx, a.client, path, now, to); err != nil || text == "" {
			orig = nil
		}
	}
}
