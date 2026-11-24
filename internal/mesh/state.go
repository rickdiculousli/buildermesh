package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// State is a node's reported state.
type State struct {
	State string `json:"state"`
	At    string `json:"at"`
	Text  string `json:"text"`
	By    string `json:"by"`
}

// StoppedBy is the blocked text bmesh node stop writes: "stopped by <who>",
// then ": <why>" when there is one.
func StoppedBy(who, why string) string {
	if why = strings.TrimSpace(why); why != "" {
		return "stopped by " + who + ": " + why
	}
	return "stopped by " + who
}

// StoppedIn is the blocked text the daemon writes for a thread the user
// interrupted in its runner.
func StoppedIn(runner string) string { return "stopped in " + runner + " by the operator" }

// Stopped reports a stopped node: blocked by bmesh node stop (StoppedBy)
// or by an interrupt in its runner (StoppedIn). The daemon wakes it only
// for a message sent with bmesh send.
func Stopped(s State) bool {
	return s.State == "blocked" && (strings.HasPrefix(s.Text, "stopped by ") || strings.HasPrefix(s.Text, "stopped in "))
}

// ErrIllegalTransition is returned for a move the table does not allow.
type ErrIllegalTransition struct{ From, To string }

func (e ErrIllegalTransition) Error() string {
	return fmt.Sprintf("illegal state transition %s -> %s", e.From, e.To)
}

// StateRow is one node state: its need rank (the needs-you band's and the timeline's order), whether a node in it is live (the Live agents tile), whether it needs the operator (the band), the states it may move to, the node events that set it and the daemon events that set it on their `to`.
type StateRow struct {
	Need   int      `json:"need"`
	Live   bool     `json:"live"`
	Needs  bool     `json:"needs"`
	Next   []string `json:"next"`
	Events []string `json:"events"`
	Daemon []string `json:"daemon"`
}

// States is the node-state table; the page reads it as /js/mesh-table.js.
var States = map[string]StateRow{
	"blocked": {Need: 0, Live: true, Needs: true, Next: []string{"working", "done"}, Events: []string{"blocked"}, Daemon: []string{}},
	"asking":  {Need: 1, Live: true, Needs: true, Next: []string{"working", "blocked", "asking", "done", "held"}, Events: []string{"ask"}, Daemon: []string{}},
	"silent":  {Need: 2, Live: true, Needs: true, Next: []string{"working", "blocked", "asking", "done", "held"}, Events: []string{}, Daemon: []string{"silent"}},
	"working": {Need: 3, Live: true, Next: []string{"working", "blocked", "asking", "done", "silent", "held", "landing"}, Events: []string{"started", "progress"}, Daemon: []string{"land-refused", "land-withdrawn"}},
	// landing: a lead that ran bmesh ready, whose box's daemon lands its
	// change. The daemon moves it on (held at a hold, done, working after a
	// refusal, landing again as land's text changes); the daemon never calls
	// it silent.
	"landing": {Need: 4, Live: true, Next: []string{"landing", "held", "done", "working", "blocked"}, Events: []string{"ready"}, Daemon: []string{"landing"}},
	// held: a lead waiting at a coordinator's gate (bmesh held). Only an
	// event of its own moves it on; the daemon never calls it silent.
	"held": {Need: 5, Live: true, Next: []string{"working", "blocked", "asking", "done", "held", "landing"}, Events: []string{"held"}, Daemon: []string{}},
	"idle": {Need: 6, Next: []string{"working"}, Events: []string{}, Daemon: []string{}},
	"done": {Need: 7, Next: []string{}, Events: []string{"done"}, Daemon: []string{}},
}

// Ticks gives each event the tick it draws on its lane.
var Ticks = map[string]string{"progress": "progress", "job": "job", "land-refused": "refused", "ready": "ready", "done": "done"}

func stateOf(event string, daemon bool) (string, bool) {
	for name, r := range States {
		list := r.Events
		if daemon {
			list = r.Daemon
		}
		if slices.Contains(list, event) {
			return name, true
		}
	}
	return "", false
}

// StateFor maps a node event to the state it sets.
func StateFor(event string) (string, bool) { return stateOf(event, false) }

// DaemonStateFor maps a daemon event to the state it sets on its `to`.
func DaemonStateFor(event string) (string, bool) { return stateOf(event, true) }

// Legal reports whether from -> to is allowed.
func Legal(from, to string) bool { return slices.Contains(States[from].Next, to) }

// NeedRank orders states as the needs-you band does; an unknown state ranks as idle.
func NeedRank(state string) int {
	if r, ok := States[state]; ok {
		return r.Need
	}
	return States["idle"].Need
}

// Live reports whether a node in state is a live agent.
func Live(state string) bool { return States[state].Live }

// Needs reports whether a node in state needs the operator.
func Needs(state string) bool { return States[state].Needs }

// TickFor is the tick an event draws on its lane.
func TickFor(event string) (string, bool) {
	k, ok := Ticks[event]
	return k, ok
}

func decodeState(data []byte) (State, error) {
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, err
	}
	return s, nil
}

// ReadState returns the path's state and revision; absent is {idle}, 0.
func ReadState(ctx context.Context, c *Client, path string) (State, uint64, error) {
	kv, err := bucket(ctx, c, BucketState)
	if err != nil {
		return State{}, 0, err
	}
	e, err := kv.Get(ctx, path)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return State{State: "idle"}, 0, nil
	}
	if err != nil {
		return State{}, 0, err
	}
	s, err := decodeState(e.Value())
	if err != nil {
		return State{}, 0, fmt.Errorf("state %s: %w", path, err)
	}
	return s, e.Revision(), nil
}

// ReadAllStates returns every path's state.
func ReadAllStates(ctx context.Context, c *Client) (map[string]State, error) {
	vals, err := allValues(ctx, c, BucketState)
	if err != nil {
		return nil, err
	}
	out := make(map[string]State, len(vals))
	for k, v := range vals {
		s, err := decodeState(v)
		if err != nil {
			return nil, fmt.Errorf("state %s: %w", k, err)
		}
		out[k] = s
	}
	return out, nil
}

// WriteState moves path to the new state: read, check Legal, then Create
// (absent) or Update at the read revision. A failed write re-reads once.
func WriteState(ctx context.Context, c *Client, path string, to State) (uint64, error) {
	return writeState(ctx, c, path, to, nil)
}

// ErrStateMoved is WriteStateAt's refusal: the state is no longer at the
// revision the writer read.
var ErrStateMoved = errors.New("state moved since it was read")

// WriteStateAt moves path to the new state only while it is still at rev,
// the revision its writer read (0: still absent): the daemon's writes for a
// node, so a node's own event written meanwhile is never overwritten. A
// state that moved is ErrStateMoved.
func WriteStateAt(ctx context.Context, c *Client, path string, rev uint64, to State) (uint64, error) {
	kv, err := bucket(ctx, c, BucketState)
	if err != nil {
		return 0, err
	}
	cur, now, err := ReadState(ctx, c, path)
	if err != nil {
		return 0, err
	}
	if now != rev {
		return 0, ErrStateMoved
	}
	if !Legal(cur.State, to.State) {
		return 0, ErrIllegalTransition{From: cur.State, To: to.State}
	}
	if to.At == "" {
		to.At = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(to)
	if err != nil {
		return 0, err
	}
	if rev == 0 {
		now, err = kv.Create(ctx, path, data)
	} else {
		now, err = kv.Update(ctx, path, data, rev)
	}
	if isConflict(err) {
		return 0, ErrStateMoved
	}
	return now, err
}

// isConflict reports a lost revision race (Create on an existing key, or
// Update at a stale revision).
func isConflict(err error) bool {
	if errors.Is(err, jetstream.ErrKeyExists) {
		return true
	}
	var api *jetstream.APIError
	return errors.As(err, &api) && api.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence
}

// writeState is WriteState with a test hook that runs after each read.
func writeState(ctx context.Context, c *Client, path string, to State, afterRead func(attempt int)) (uint64, error) {
	kv, err := bucket(ctx, c, BucketState)
	if err != nil {
		return 0, err
	}
	if to.At == "" {
		to.At = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(to)
	if err != nil {
		return 0, err
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		cur, rev, err := ReadState(ctx, c, path)
		if err != nil {
			return 0, err
		}
		if afterRead != nil {
			afterRead(attempt)
		}
		if !Legal(cur.State, to.State) {
			return 0, ErrIllegalTransition{From: cur.State, To: to.State}
		}
		var newRev uint64
		if rev == 0 {
			newRev, err = kv.Create(ctx, path, data)
		} else {
			newRev, err = kv.Update(ctx, path, data, rev)
		}
		if err == nil {
			return newRev, nil
		}
		if !isConflict(err) {
			return 0, err
		}
		lastErr = err
	}
	return 0, lastErr
}

// ResetState deletes path's state so it reads idle again (rev 0); a node
// restarted on a done or blocked path starts from idle.
func ResetState(ctx context.Context, c *Client, path string) error {
	kv, err := bucket(ctx, c, BucketState)
	if err != nil {
		return err
	}
	return kv.Delete(ctx, path)
}
