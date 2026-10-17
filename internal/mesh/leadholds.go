package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/nats-io/nats.go/jetstream"
)

// Gates a coordinator holds a lead at: land (bmesh land refuses) and now
// (the lead's next inbox says to stop there).
const (
	GateLand = "land"
	GateNow  = "now"
)

// LeadHold is a coordinator's hold on a lead, kept in bm-holds under the
// lead's path until it is released. Path is the key, not stored.
type LeadHold struct {
	Path string `json:"-"`
	Gate string `json:"gate"`
	Text string `json:"text"`
	By   string `json:"by"`
	At   string `json:"at"`
}

// GatePhrase is where a hold stops its lead, as land and inbox say it:
// "before land" or "now".
func GatePhrase(gate string) string {
	if gate == GateLand {
		return "before land"
	}
	return gate
}

// holdsBucket opens bm-holds, creating it when create and it is missing (a
// hub provisioned before it); without create a missing bucket is nil, nil.
func holdsBucket(ctx context.Context, c *Client, create bool) (jetstream.KeyValue, error) {
	kv, err := bucket(ctx, c, BucketHolds)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		if !create {
			return nil, nil
		}
		return c.JS.CreateOrUpdateKeyValue(ctx, holdsConfig)
	}
	return kv, err
}

var holdsConfig = jetstream.KeyValueConfig{Bucket: BucketHolds, History: 1, Storage: jetstream.FileStorage}

// PutLeadHold writes h under its path, replacing any hold there.
func PutLeadHold(ctx context.Context, c *Client, h LeadHold) error {
	if h.Gate != GateLand && h.Gate != GateNow {
		return fmt.Errorf("hold %s: gate %q is not land or now", h.Path, h.Gate)
	}
	if _, err := ParsePath(h.Path); err != nil {
		return err
	}
	kv, err := holdsBucket(ctx, c, true)
	if err != nil {
		return err
	}
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	_, err = kv.Put(ctx, h.Path, data)
	return err
}

// GetLeadHold returns path's hold; false when it is not held.
func GetLeadHold(ctx context.Context, c *Client, path string) (LeadHold, bool, error) {
	kv, err := holdsBucket(ctx, c, false)
	if err != nil || kv == nil {
		return LeadHold{}, false, err
	}
	e, err := kv.Get(ctx, path)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return LeadHold{}, false, nil
	}
	if err != nil {
		return LeadHold{}, false, err
	}
	var h LeadHold
	if err := json.Unmarshal(e.Value(), &h); err != nil {
		return LeadHold{}, false, fmt.Errorf("%s %s: %w", BucketHolds, path, err)
	}
	h.Path = path
	return h, true, nil
}

// DeleteLeadHold releases path's hold; false when there was none.
func DeleteLeadHold(ctx context.Context, c *Client, path string) (bool, error) {
	_, ok, err := GetLeadHold(ctx, c, path)
	if err != nil || !ok {
		return false, err
	}
	kv, err := holdsBucket(ctx, c, false)
	if err != nil || kv == nil {
		return false, err
	}
	if err := kv.Purge(ctx, path); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
		return false, err
	}
	return true, nil
}

// ListLeadHolds returns every hold, sorted by path.
func ListLeadHolds(ctx context.Context, c *Client) ([]LeadHold, error) {
	kv, err := holdsBucket(ctx, c, false)
	if err != nil || kv == nil {
		return nil, err
	}
	keys, err := kv.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	var out []LeadHold
	for _, k := range keys {
		h, ok, err := GetLeadHold(ctx, c, k)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, h)
		}
	}
	return out, nil
}
