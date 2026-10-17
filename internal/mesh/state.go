package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

var transitions = map[string][]string{
	"idle":    {"working"},
	"working": {"working", "blocked", "asking", "done", "silent", "held"},
	"blocked": {"working", "done"},
	"asking":  {"working", "blocked", "asking", "done", "held"},
	"silent":  {"working", "blocked", "asking", "done", "held"},
	// held: a lead waiting at a coordinator's gate (bmesh held). Only an
	// event of its own moves it on; the daemon never calls it silent.
	"held": {"working", "blocked", "asking", "done", "held"},
}

var eventStates = map[string]string{
	"started": "working", "progress": "working", "blocked": "blocked", "ask": "asking", "done": "done",
	"held": "held",
}

// StateFor maps a node event to the state it sets.
func StateFor(event string) (string, bool) {
	s, ok := eventStates[event]
	return s, ok
}

// Legal reports whether from -> to is allowed.
func Legal(from, to string) bool {
	for _, t := range transitions[from] {
		if t == to {
			return true
		}
	}
	return false
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
	kv, err := bucket(ctx, c, BucketState)
	if err != nil {
		return nil, err
	}
	keys, err := kv.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return map[string]State{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(map[string]State, len(keys))
	for _, k := range keys {
		e, err := kv.Get(ctx, k)
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		s, err := decodeState(e.Value())
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
