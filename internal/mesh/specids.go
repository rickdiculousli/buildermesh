package mesh

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/nats-io/nats.go/jetstream"
)

// bm-specids holds one key per repo, specIDsKey, whose value is the
// highest spec id claimed there: spec next-id and ideate claim through it,
// so two writers never take the same number. It has no age limit; a claim
// that never lands is a gap.

var specIDsConfig = jetstream.KeyValueConfig{Bucket: BucketSpecIDs, History: 1, Storage: jetstream.FileStorage}

// specIDsKey is repo's key: its RepoToken, with any other character a KV
// key can't hold (an unregistered checkout's directory name may have a
// space or a +) as -.
func specIDsKey(repo string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, RepoToken(repo))
}

// specIDsBucket opens bm-specids, creating it when it is missing (a hub
// provisioned before it).
func specIDsBucket(ctx context.Context, c *Client) (jetstream.KeyValue, error) {
	kv, err := bucket(ctx, c, BucketSpecIDs)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		return c.JS.CreateOrUpdateKeyValue(ctx, specIDsConfig)
	}
	return kv, err
}

// readSpecIDs is repo's counter and its revision; 0, 0 when it has none.
func readSpecIDs(ctx context.Context, kv jetstream.KeyValue, key string) (int, uint64, error) {
	e, err := kv.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	n, err := strconv.Atoi(string(e.Value()))
	if err != nil {
		return 0, 0, fmt.Errorf("%s %s: %q is not a number", BucketSpecIDs, key, e.Value())
	}
	return n, e.Revision(), nil
}

// ClaimSpecIDs claims n ids for repo: it sets the counter to max(counter,
// highest) + n by compare-and-swap, retrying when another writer moved it
// first, and returns the first id claimed. highest is the highest id on
// the repo's main, so a counter behind main catches up.
func ClaimSpecIDs(ctx context.Context, c *Client, repo string, highest, n int) (int, error) {
	if n < 1 {
		return 0, fmt.Errorf("claim %d spec ids: want at least one", n)
	}
	kv, err := specIDsBucket(ctx, c)
	if err != nil {
		return 0, err
	}
	key := specIDsKey(repo)
	for {
		cur, rev, err := readSpecIDs(ctx, kv, key)
		if err != nil {
			return 0, err
		}
		top := max(cur, highest) + n
		val := []byte(strconv.Itoa(top))
		if rev == 0 {
			_, err = kv.Create(ctx, key, val)
		} else {
			_, err = kv.Update(ctx, key, val, rev)
		}
		if err == nil {
			return top - n + 1, nil
		}
		if !isConflict(err) {
			return 0, err
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
	}
}

// PeekSpecID is the id the next claim for repo would get, claiming nothing.
func PeekSpecID(ctx context.Context, c *Client, repo string, highest int) (int, error) {
	kv, err := bucket(ctx, c, BucketSpecIDs)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		return highest + 1, nil
	}
	if err != nil {
		return 0, err
	}
	cur, _, err := readSpecIDs(ctx, kv, specIDsKey(repo))
	if err != nil {
		return 0, err
	}
	return max(cur, highest) + 1, nil
}
