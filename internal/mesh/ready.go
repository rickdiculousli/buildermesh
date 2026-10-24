package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/nats-io/nats.go/jetstream"
)

// The phases of a ready request: waiting for the daemon to land it (ready),
// withdrawn by bmesh ready --cancel (cancelled), landed and done with the
// final wake still to deliver (landed), and refused by land with the wake
// that carries the refusal still to deliver (refused).
const (
	PhaseReady     = "ready"
	PhaseCancelled = "cancelled"
	PhaseLanded    = "landed"
	PhaseRefused   = "refused"
)

// Ready is a lead's request that its box's daemon land its change (bmesh
// ready), kept in bm-ready under the lead's path until the daemon has
// delivered the wake that ends it, or the request is withdrawn. Path is the
// key, not stored.
type Ready struct {
	Path  string `json:"-"`
	Spec  string `json:"spec"`
	Head  string `json:"head"`           // HEAD when the lead ran ready: land starts only from it, or land's own rebase of it
	Size  string `json:"size,omitempty"` // --size, passed to land
	Text  string `json:"text"`           // the lead's done line
	At    string `json:"at"`
	Phase string `json:"phase"`
	Seq   uint64 `json:"seq"` // the lead's inbox sequence at ready: a message after it withdraws the request

	Commit string `json:"commit,omitempty"` // landed: the commit done records
	Main   string `json:"main,omitempty"`   // landed: origin/main after the push, which contains Commit

	Refusal string `json:"refusal,omitempty"` // refused: land's refusal line
	Output  string `json:"output,omitempty"`  // refused: land's last lines

	// FailingSince is when the ending wake first failed for a reason other
	// than a running thread; an hour of that ends the request.
	FailingSince string `json:"failing_since,omitempty"`
}

// ErrReadyExists is CreateReady's refusal: the path has a request already.
var ErrReadyExists = errors.New("a ready request exists")

// ErrReadyMoved is UpdateReady's refusal: the request is gone or no longer
// at the revision the writer read.
var ErrReadyMoved = errors.New("the ready request moved since it was read")

var readyConfig = jetstream.KeyValueConfig{Bucket: BucketReady, History: 1, Storage: jetstream.FileStorage}

// readyBucket opens bm-ready, creating it when create and it is missing (a
// hub provisioned before it); without create a missing bucket is nil, nil.
func readyBucket(ctx context.Context, c *Client, create bool) (jetstream.KeyValue, error) {
	kv, err := bucket(ctx, c, BucketReady)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		if !create {
			return nil, nil
		}
		return c.JS.CreateOrUpdateKeyValue(ctx, readyConfig)
	}
	return kv, err
}

// CreateReady writes r under its path; ErrReadyExists when one is there.
func CreateReady(ctx context.Context, c *Client, r Ready) error {
	if _, err := ParsePath(r.Path); err != nil {
		return err
	}
	kv, err := readyBucket(ctx, c, true)
	if err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, err := kv.Create(ctx, r.Path, data); err != nil {
		if isConflict(err) {
			return ErrReadyExists
		}
		return err
	}
	return nil
}

// GetReady returns path's request and its revision; false when it has none.
func GetReady(ctx context.Context, c *Client, path string) (Ready, uint64, bool, error) {
	kv, err := readyBucket(ctx, c, false)
	if err != nil || kv == nil {
		return Ready{}, 0, false, err
	}
	e, err := kv.Get(ctx, path)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return Ready{}, 0, false, nil
	}
	if err != nil {
		return Ready{}, 0, false, err
	}
	var r Ready
	if err := json.Unmarshal(e.Value(), &r); err != nil {
		return Ready{}, 0, false, fmt.Errorf("%s %s: %w", BucketReady, path, err)
	}
	r.Path = path
	return r, e.Revision(), true, nil
}

// UpdateReady writes r over the request at rev; ErrReadyMoved when it is
// gone or moved.
func UpdateReady(ctx context.Context, c *Client, r Ready, rev uint64) (uint64, error) {
	kv, err := readyBucket(ctx, c, false)
	if err != nil {
		return 0, err
	}
	if kv == nil {
		return 0, ErrReadyMoved
	}
	data, err := json.Marshal(r)
	if err != nil {
		return 0, err
	}
	n, err := kv.Update(ctx, r.Path, data, rev)
	if isConflict(err) || errors.Is(err, jetstream.ErrKeyNotFound) {
		return 0, ErrReadyMoved
	}
	return n, err
}

// DeleteReady ends path's request; false when there was none.
func DeleteReady(ctx context.Context, c *Client, path string) (bool, error) {
	_, _, ok, err := GetReady(ctx, c, path)
	if err != nil || !ok {
		return false, err
	}
	kv, err := readyBucket(ctx, c, false)
	if err != nil || kv == nil {
		return false, err
	}
	if err := kv.Purge(ctx, path); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
		return false, err
	}
	return true, nil
}

// ListReady returns every request, sorted by path.
func ListReady(ctx context.Context, c *Client) ([]Ready, error) {
	kv, err := readyBucket(ctx, c, false)
	if err != nil || kv == nil {
		return nil, err
	}
	keys, err := listKeys(ctx, c, kv, ">")
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	var out []Ready
	for _, k := range keys {
		r, _, ok, err := GetReady(ctx, c, k)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, r)
		}
	}
	return out, nil
}
