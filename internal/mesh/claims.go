package mesh

import (
	"context"
	"time"
)

// ClaimsAge is bm-claims' age limit: a claim is gone after it, so the
// bucket holds about a week of windows.
const ClaimsAge = 8 * 24 * time.Hour

// Claim creates key in bm-claims with holder as its value: true when this
// call created it, false when the key was already there (another daemon,
// or this one earlier, has it).
func Claim(ctx context.Context, c *Client, key, holder string) (bool, error) {
	kv, err := bucket(ctx, c, BucketClaims)
	if err != nil {
		return false, err
	}
	if _, err := kv.Create(ctx, key, []byte(holder)); err != nil {
		if isConflict(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Unclaim deletes key from bm-claims, so a later Claim of it can win.
func Unclaim(ctx context.Context, c *Client, key string) error {
	kv, err := bucket(ctx, c, BucketClaims)
	if err != nil {
		return err
	}
	return kv.Delete(ctx, key)
}
