package daemon

import (
	"context"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// A round is a node <repo>-round-<N> with two children, eng and ux, each
// an engineer thread. It settles when both are done (done, with the merged
// pitch) or one is blocked (blocked); the daemon writes the round's state
// and publishes the matching event, which the push loop delivers.

// SettleRound is the round state machine: the state and text a round
// takes from its children's states, or ok false while it is still open.
// Done needs both children done; its text is the merged ranked pitch, the
// engineer's list first, then the UI/UX engineer's. One child blocked
// blocks the round, naming it; the other's pitch follows when it is done.
func SettleRound(eng, ux mesh.State) (state, text string, ok bool) {
	for _, c := range []struct {
		name string
		st   mesh.State
	}{{"eng", eng}, {"ux", ux}} {
		if c.st.State == "blocked" {
			text = c.name + " blocked"
			if c.st.Text != "" {
				text += ": " + c.st.Text
			}
			other := ux
			if c.name == "ux" {
				other = eng
			}
			if other.State == "done" && strings.TrimSpace(other.Text) != "" {
				text += "\n\n" + strings.TrimSpace(other.Text)
			}
			return "blocked", text, true
		}
	}
	if eng.State != "done" || ux.State != "done" {
		return "", "", false
	}
	return "done", Pitch(eng.Text, ux.Text), true
}

// Pitch merges the two children's ranked lists: the engineer's lines, then
// the UI/UX engineer's, each as reported.
func Pitch(eng, ux string) string {
	var parts []string
	for _, s := range []string{eng, ux} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

// roundsOnce settles every open round. The state write is a compare-and-set
// from working (or silent), so of two daemons only one publishes. A blocked
// round is looked at again: both children done moves it to done (a blocked
// child that was unblocked and finished), and a changed blocked text (the
// other child's pitch arrived) is published again, through working, so the
// operator gets that pitch too.
func roundsOnce(ctx context.Context, d Deps) {
	nodes, err := mesh.ListNodes(ctx, d.Client)
	if err != nil {
		d.Log.Warn("rounds: list nodes", "err", err)
		return
	}
	states, err := mesh.ReadAllStates(ctx, d.Client)
	if err != nil {
		d.Log.Warn("rounds: read states", "err", err)
		return
	}
	for _, n := range nodes {
		if n.Role != "round" || ctx.Err() != nil {
			continue
		}
		cur := states[n.Path]
		if cur.State != "working" && cur.State != "silent" && cur.State != "blocked" {
			continue
		}
		state, text, ok := SettleRound(states[n.Path+".eng"], states[n.Path+".ux"])
		if !ok || (cur.State == "blocked" && state == "blocked" && text == cur.Text) {
			continue
		}
		now := d.Now().UTC().Format(time.RFC3339)
		if cur.State == "blocked" && state == "blocked" {
			if _, err := mesh.WriteState(ctx, d.Client, n.Path, mesh.State{State: "working", Text: cur.Text, By: "daemon", At: now}); err != nil {
				d.Log.Info("rounds: not resettled here", "path", n.Path, "err", err)
				continue
			}
		}
		if _, err := mesh.WriteState(ctx, d.Client, n.Path, mesh.State{State: state, Text: text, By: "daemon", At: now}); err != nil {
			d.Log.Info("rounds: not settled here", "path", n.Path, "err", err)
			continue
		}
		ev := mesh.Event{From: n.Path, Event: state, Text: text, At: now}
		if err := mesh.PublishEvent(ctx, d.Client, ev); err != nil {
			d.Log.Warn("rounds: publish", "path", n.Path, "err", err)
		}
	}
}
