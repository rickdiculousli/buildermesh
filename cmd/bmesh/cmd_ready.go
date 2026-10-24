package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/daemon"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

const readyUsage = `usage: bmesh ready --text "<done line>" [--size XS|S|M|L] | bmesh ready --cancel`

// readyPoll is how often ready --cancel looks for the daemon to end the
// request; readyCancelWait how long it waits in all. Tests shorten them.
var (
	readyPoll       = time.Second
	readyCancelWait = 3 * time.Minute
)

// cmdReady is a lead handing its landing to its box's daemon: once the
// worktree, the spec, the mail and the daemon allow it, it writes the
// lead landing, then bm-ready <path>, then publishes ready, and the lead
// ends its turn. The daemon lands the change and wakes the lead only when
// land needs it, or once at the end for its closing line.
func (a *app) cmdReady(args []string) error {
	fs := a.flags("ready")
	text := fs.String("text", "", "your done line, which done carries once the change has landed")
	size := fs.String("size", "", "the size the change was built at, when it grew past the spec's (XS, S, M or L), passed to land")
	cancel := fs.Bool("cancel", false, "withdraw the request: the daemon cancels a running step")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, readyUsage)
	}
	if *cancel {
		if *text != "" || *size != "" {
			return fail(exitUsage, readyUsage)
		}
		return a.readyCancel()
	}
	if strings.TrimSpace(*text) == "" {
		return fail(exitUsage, readyUsage)
	}
	if _, ok := reviewEffort(*size); *size != "" && !ok {
		return fail(exitUsage, "ready: --size %q: want XS, S, M or L", *size)
	}
	ctx, done := cmdCtx()
	defer done()
	p, err := a.identity(ctx, "ready")
	if err != nil {
		return err
	}
	path := p.String()
	n, ok, err := mesh.GetNode(ctx, a.client, path)
	if err != nil {
		return err
	}
	host := a.cfg.HostName()
	switch {
	case !ok:
		return fail(exitRefused, "refused: no node %s; run bmesh ready in your worktree, or pass --node <your path>", path)
	case n.Role != "lead" && n.Role != "":
		next := "run bmesh land"
		if n.Role == "executor" {
			next = "report to your lead with bmesh done --commit <sha>"
		}
		return fail(exitRefused, "refused: %s is a %s; only a lead's daemon lands it: %s", path, n.Role, next)
	case n.Host != host:
		return fail(exitRefused, "refused: %s runs on %s, not this box (%s); its daemon is there: run bmesh ready on %s, or bmesh land here", path, n.Host, host, n.Host)
	case n.Runner.Thread == "":
		return fail(exitRefused, "refused: %s has no thread for the daemon to wake; run bmesh land", path)
	}
	top, err := gitRev("--show-toplevel")
	if err != nil {
		return err
	}
	if n.Worktree == "" || !mesh.SameDir(n.Worktree, top) {
		return fail(exitRefused, "refused: %s's worktree is %s, not this checkout %s; run bmesh ready in %s", path, dash(n.Worktree), top, dash(n.Worktree))
	}
	if st, err := gitIn(ctx, top, "status", "--porcelain"); err != nil {
		return err
	} else if st != "" {
		return fail(exitRefused, "refused: the worktree has changes not committed; commit them, then run bmesh ready again")
	}
	id, err := strconv.Atoi(n.Spec)
	if err != nil {
		return fail(exitRefused, "refused: %s names no spec (%q); run bmesh land --spec NNN", path, n.Spec)
	}
	branch, err := gitIn(ctx, top, "branch", "--show-current")
	if err != nil {
		return err
	}
	if _, err := landsDone(top, a.cfg.Spec.Glob, id, dash(branch), "bmesh ready"); err != nil {
		return err
	}
	pending, err := mesh.Pending(ctx, a.client, p)
	if err != nil {
		return err
	}
	if k := len(slices.DeleteFunc(pending, func(m mesh.Message) bool { return !fromOperator(m) })); k > 0 {
		return fail(exitRefused, "refused: %d unread message(s) from the operator; run bmesh inbox, act on them, then run bmesh ready again", k)
	}
	if _, _, ok, err := mesh.GetReady(ctx, a.client, path); err != nil {
		return err
	} else if ok {
		return fail(exitRefused, "already ready; bmesh ready --cancel first")
	}
	if ev, ok, err := a.ownStarted(ctx); err != nil {
		return err
	} else if !ok || !slices.Contains(ev.Can, daemon.CanLand) {
		return fail(exitRefused, "this box's daemon doesn't land for leads; run bmesh land")
	}
	cur, _, err := mesh.ReadState(ctx, a.client, path)
	if err != nil {
		return err
	}
	if !mesh.Legal(cur.State, "landing") {
		return illegalMove(cur.State, "landing", "bmesh ready")
	}
	head, err := gitIn(ctx, top, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	seq, err := mesh.InboxSeq(ctx, a.client, p)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// The state first, then the request: a pass that finds the request
	// finds the lead landing, never a working lead it would withdraw.
	if _, err := mesh.WriteState(ctx, a.client, path, mesh.State{State: "landing", Text: "ready to land " + short8(head), By: "node", At: now}); err != nil {
		var ill mesh.ErrIllegalTransition
		if errors.As(err, &ill) {
			return illegalMove(ill.From, ill.To, "bmesh ready")
		}
		return err
	}
	r := mesh.Ready{Path: path, Spec: n.Spec, Head: head, Size: *size, Text: *text, At: now, Phase: mesh.PhaseReady, Seq: seq}
	if err := mesh.CreateReady(ctx, a.client, r); errors.Is(err, mesh.ErrReadyExists) {
		return fail(exitRefused, "already ready; bmesh ready --cancel first")
	} else if err != nil {
		return err
	}
	if err := mesh.PublishEvent(ctx, a.client, mesh.Event{From: path, Event: "ready", Text: *text, Commit: head, At: now}); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "ready: %s's daemon lands %s; it wakes you only if land needs you\n", host, short8(head))
	return nil
}

// readyCancel withdraws the node's request: it marks it cancelled and
// waits until the daemon has ended it (cancelling a running step), or
// ends it itself when no daemon holds its claim. A step that had already
// pushed still gets done and the final wake. A request withdrawn moves
// the lead to working again.
func (a *app) readyCancel() error {
	ctx, done := context.WithTimeout(context.Background(), readyCancelWait+time.Minute)
	defer done()
	p, err := a.identity(ctx, "ready")
	if err != nil {
		return err
	}
	path := p.String()
	r, rev, ok, err := mesh.GetReady(ctx, a.client, path)
	if err != nil {
		return err
	}
	if !ok {
		return fail(exitRefused, "refused: %s has no ready request; there is nothing to cancel", path)
	}
	switch r.Phase {
	case mesh.PhaseLanded:
		return a.readyTooLate(r)
	case mesh.PhaseRefused:
		// land has exited; its wake is all that is left.
		if _, err := mesh.DeleteReady(ctx, a.client, path); err != nil {
			return err
		}
		return a.readyCancelled(ctx, path, "land had refused: "+r.Refusal)
	case mesh.PhaseReady:
		r.Phase = mesh.PhaseCancelled
		if _, err := mesh.UpdateReady(ctx, a.client, r, rev); errors.Is(err, mesh.ErrReadyMoved) {
			return a.readyCancel() // the daemon moved it meanwhile: read it again
		} else if err != nil {
			return err
		}
	}
	slot := "ready." + path
	deadline := time.Now().Add(readyCancelWait)
	for {
		cur, _, ok, err := mesh.GetReady(ctx, a.client, path)
		if err != nil {
			return err
		}
		if !ok {
			return a.readyCancelled(ctx, path, "nothing is pushed")
		}
		if cur.Phase == mesh.PhaseLanded {
			return a.readyTooLate(cur)
		}
		leases, err := mesh.ListLeases(ctx, a.client)
		if err != nil {
			return err
		}
		claimed := slices.ContainsFunc(leases, func(l mesh.Lease) bool { return l.Slot == slot && l.ExpiresAt.After(time.Now()) })
		if !claimed {
			// No daemon works it, so no step runs: end it here. A daemon
			// that takes the claim after this reads the cancel first.
			if _, err := mesh.DeleteReady(ctx, a.client, path); err != nil {
				return err
			}
			return a.readyCancelled(ctx, path, "nothing is pushed")
		}
		if time.Now().After(deadline) {
			return fail(exitTimeout, "timed out waiting for the daemon to cancel the step; the request reads cancelled, and the daemon ends it")
		}
		time.Sleep(readyPoll)
	}
}

// readyCancelled moves a lead still landing (or held) to working and says
// the request is withdrawn.
func (a *app) readyCancelled(ctx context.Context, path, how string) error {
	text := "ready cancelled: " + how
	if cur, _, err := mesh.ReadState(ctx, a.client, path); err == nil && (cur.State == "landing" || cur.State == "held") {
		if err := mesh.PublishEvent(ctx, a.client, mesh.Event{From: path, Event: "progress", Text: text}); err != nil {
			return err
		}
		if _, err := mesh.WriteState(ctx, a.client, path, mesh.State{State: "working", Text: text, By: "node"}); err != nil {
			return err
		}
	}
	fmt.Fprintf(a.stdout, "cancelled: %s; land in your own turn with bmesh land, or bmesh ready again\n", how)
	return nil
}

// readyTooLate says a cancel came after the push.
func (a *app) readyTooLate(r mesh.Ready) error {
	fmt.Fprintf(a.stdout, "too late: landed %s; the daemon has recorded done and wakes you for your closing line\n", short8(r.Commit))
	return nil
}
