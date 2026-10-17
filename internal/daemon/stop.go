package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

// StopNode stops n (bmesh node stop): the node blocked with
// mesh.StoppedBy(who, why) and that blocked published, then its thread
// interrupted through r when it has one (a thread the runner no longer
// knows is stopped already). Marked first, the daemon's poll finds it
// blocked and never writes its own stop over the reason; a thread that
// reported between the two is marked again. A done or blocked node is
// left as it is (already names its state), but a stopped one's thread is
// interrupted again: a stop-thread job that reaches a box after its sender
// marked the node, or a stop whose interrupt failed.
func StopNode(ctx context.Context, c *mesh.Client, r runner.Runner, n mesh.Node, who, why string) (already string, err error) {
	st, _, err := mesh.ReadState(ctx, c, n.Path)
	if err != nil {
		return "", err
	}
	if st.State == "done" || st.State == "blocked" && !mesh.Stopped(st) {
		return st.State, nil
	}
	if st.State != "blocked" {
		if already, err = markBlocked(ctx, c, n.Path, mesh.StoppedBy(who, why)); err != nil {
			return "", err
		}
	} else {
		already = st.State
	}
	if n.Runner.Thread == "" {
		return already, nil
	}
	if err := r.Interrupt(ctx, n.Runner.Thread); err != nil && !errors.Is(err, runner.ErrNoThread) {
		return "", fmt.Errorf("stop %s: %w (it is marked stopped; run node stop again to stop its thread)", n.Path, err)
	}
	if already == "" {
		// The thread reported (blocked -> working) before the interrupt
		// reached it: mark it again.
		if cur, _, err := mesh.ReadState(ctx, c, n.Path); err == nil && cur.State != "done" && cur.State != "blocked" {
			if _, err := markBlocked(ctx, c, n.Path, mesh.StoppedBy(who, why)); err != nil {
				return "", err
			}
		}
	}
	return already, nil
}

// MarkStopped marks path blocked with mesh.StoppedBy(who, why), touching
// no thread: a node with none (an executor), or one whose box hasn't
// answered its stop-thread yet. already names a done or blocked state it
// left as it was.
func MarkStopped(ctx context.Context, c *mesh.Client, path, who, why string) (already string, err error) {
	return markBlocked(ctx, c, path, mesh.StoppedBy(who, why))
}

// markBlocked writes path blocked with text and publishes that blocked; an
// idle path passes through working, the only state idle moves to. A path
// another writer finished meanwhile is left as it is (already).
func markBlocked(ctx context.Context, c *mesh.Client, path, text string) (already string, err error) {
	now := time.Now().UTC().Format(time.RFC3339)
	st, _, err := mesh.ReadState(ctx, c, path)
	if err != nil {
		return "", err
	}
	if st.State == "idle" {
		if _, err := mesh.WriteState(ctx, c, path, mesh.State{State: "working", By: "node", At: now}); err != nil {
			return "", err
		}
	}
	if _, err := mesh.WriteState(ctx, c, path, mesh.State{State: "blocked", Text: text, By: "node", At: now}); err != nil {
		var ill mesh.ErrIllegalTransition
		if errors.As(err, &ill) && (ill.From == "done" || ill.From == "blocked") {
			return ill.From, nil
		}
		return "", err
	}
	return "", mesh.PublishEvent(ctx, c, mesh.Event{From: path, Event: "blocked", Text: text, At: now})
}

// StopRound marks a round stopped once nothing under it is left to stop:
// settled from its children as the daemon would (SettleRound), or, with
// no children left, blocked with mesh.StoppedBy(who, why). It returns the
// round's state afterwards; ok is false while the children leave it open
// (a child on an asleep box not stopped yet).
func StopRound(ctx context.Context, c *mesh.Client, n mesh.Node, children bool, who, why string) (state string, ok bool, err error) {
	if !children {
		already, err := markBlocked(ctx, c, n.Path, mesh.StoppedBy(who, why))
		if already != "" {
			return already, true, err
		}
		return "blocked", err == nil, err
	}
	states, err := mesh.ReadAllStates(ctx, c)
	if err != nil {
		return "", false, err
	}
	cur := states[n.Path].State
	if cur == "done" || cur == "blocked" {
		return cur, true, nil
	}
	state, text, ok := SettleRound(RoundStates(n, states))
	if !ok {
		return cur, false, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if cur == "" || cur == "idle" {
		if _, err := mesh.WriteState(ctx, c, n.Path, mesh.State{State: "working", By: "node", At: now}); err != nil {
			return "", false, err
		}
	}
	if _, err := mesh.WriteState(ctx, c, n.Path, mesh.State{State: state, Text: text, By: "node", At: now}); err != nil {
		var ill mesh.ErrIllegalTransition
		if errors.As(err, &ill) && (ill.From == "done" || ill.From == "blocked") {
			return ill.From, true, nil // the daemon settled it first
		}
		return "", false, err
	}
	ev := mesh.Event{From: n.Path, Event: state, Text: text, At: now}
	if _, err := mesh.PublishEventID(ctx, c, ev, RoundEventID(n.Path, state, text)); err != nil {
		return "", false, err
	}
	return state, true, nil
}

// stopThreadJob runs a received stop-thread: j.Args is the node's path,
// who stops it and why. A node of this box's is stopped here (StopNode,
// through this box's runner), answering `stopped <path>` or `already
// <state>`.
func (h *hostsNode) stopThreadJob(ctx context.Context, j *hosts.Job) (bool, string) {
	if len(j.Args) != 3 {
		return false, "refused: stop-thread takes a node path, who and why"
	}
	p, err := mesh.ParsePath(j.Args[0])
	if err != nil {
		return false, "refused: " + cut(err.Error(), 128)
	}
	path := p.String()
	n, ok, err := mesh.GetNode(ctx, h.d.Client, path)
	if err != nil {
		return false, err.Error()
	}
	if !ok {
		return false, "refused: no node " + path
	}
	if n.Host != h.d.Host {
		return false, fmt.Sprintf("refused: %s is on %s, not %s", path, n.Host, h.d.Host)
	}
	if n.Runner.Thread != "" && n.Runner.Kind != h.d.Cfg.Runner.Kind {
		return false, fmt.Sprintf("refused: %s's runner is %q, not this box's %s", path, n.Runner.Kind, h.d.Cfg.Runner.Kind)
	}
	already, err := StopNode(ctx, h.d.Client, h.d.Runner, n, cut(j.Args[1], 128), cut(j.Args[2], 512))
	if err != nil {
		return false, cut(err.Error(), 512)
	}
	h.d.Log.Info("hosts: stop-thread", "node", path, "by", j.By)
	if already != "" {
		return true, "already " + already
	}
	return true, "stopped " + path
}
