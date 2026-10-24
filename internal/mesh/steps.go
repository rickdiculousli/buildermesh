package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

// bm-steps holds the operator's ticks of a landed spec's steps (bmesh spec
// steps --tick and --check), one key per spec, stepsKey, its value the
// ticks as JSON. The steps themselves are the spec's, on main.

var stepsConfig = jetstream.KeyValueConfig{Bucket: BucketSteps, History: 1, Storage: jetstream.FileStorage}

// StepTick is one step ticked: the step's words, its box included (not
// its number, so the tick stays with its step when the list changes),
// `<time> <box>`, and the words the tick added.
type StepTick struct {
	Step string `json:"step"`
	Done string `json:"done"`
	Note string `json:"note,omitempty"`
}

// stepsKey is spec id's key for repo: the repo's token as bm-specids keys
// it, a dot, and the id.
func stepsKey(repo, id string) string { return specIDsKey(repo) + "." + id }

// stepsBucket opens bm-steps, creating it when create and it is missing
// (a hub provisioned before it); without create a missing bucket is nil,
// nil.
func stepsBucket(ctx context.Context, c *Client, create bool) (jetstream.KeyValue, error) {
	kv, err := bucket(ctx, c, BucketSteps)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		if !create {
			return nil, nil
		}
		return c.JS.CreateOrUpdateKeyValue(ctx, stepsConfig)
	}
	return kv, err
}

// readTicks is key's ticks and revision; nil, 0 when it has none.
func readTicks(ctx context.Context, kv jetstream.KeyValue, key string) ([]StepTick, uint64, error) {
	e, err := kv.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var ticks []StepTick
	if err := json.Unmarshal(e.Value(), &ticks); err != nil {
		return nil, 0, fmt.Errorf("%s %s: %w", BucketSteps, key, err)
	}
	return ticks, e.Revision(), nil
}

// StepTicks is the ticks recorded for repo's spec id, by step number.
func StepTicks(ctx context.Context, c *Client, repo, id string) ([]StepTick, error) {
	kv, err := stepsBucket(ctx, c, false)
	if err != nil || kv == nil {
		return nil, err
	}
	ticks, _, err := readTicks(ctx, kv, stepsKey(repo, id))
	return ticks, err
}

// TickSteps adds ticks to repo's spec id by compare-and-swap, retrying when
// another writer moved the key first; a step already ticked keeps its
// tick. It returns the ticks recorded, oldest first.
func TickSteps(ctx context.Context, c *Client, repo, id string, ticks []StepTick) ([]StepTick, error) {
	kv, err := stepsBucket(ctx, c, true)
	if err != nil {
		return nil, err
	}
	key := stepsKey(repo, id)
	for {
		cur, rev, err := readTicks(ctx, kv, key)
		if err != nil {
			return nil, err
		}
		have := map[string]bool{}
		for _, t := range cur {
			have[t.Step] = true
		}
		all := cur
		for _, t := range ticks {
			if !have[t.Step] {
				all = append(all, t)
				have[t.Step] = true
			}
		}
		data, err := json.Marshal(all)
		if err != nil {
			return nil, err
		}
		if rev == 0 {
			_, err = kv.Create(ctx, key, data)
		} else {
			_, err = kv.Update(ctx, key, data, rev)
		}
		if err == nil {
			return all, nil
		}
		if !isConflict(err) {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
}
