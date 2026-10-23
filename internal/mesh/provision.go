package mesh

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Provision creates the stream and buckets; it is safe to run repeatedly.
// keepResults is bm-results' age limit (0 = none): an older result is gone.
func Provision(ctx context.Context, c *Client, keepResults time.Duration) error {
	if _, err := c.JS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       StreamMesh,
		Subjects:   []string{"bm.>"},
		Retention:  jetstream.LimitsPolicy,
		Storage:    jetstream.FileStorage,
		MaxAge:     30 * 24 * time.Hour,
		Duplicates: 2 * time.Minute,
	}); err != nil {
		return fmt.Errorf("stream %s: %w", StreamMesh, err)
	}
	for _, b := range []struct {
		name    string
		history uint8
		ttl     time.Duration
	}{{BucketNodes, 1, 0}, {BucketState, 16, 0}, {BucketRunner, 1, 0}, {BucketResults, 1, keepResults}, {BucketLeases, 1, 0}, {BucketClaims, 1, ClaimsAge}, {BucketJobs, 1, 0}, {BucketHolds, 1, 0}, {BucketSpecIDs, 1, 0}} {
		if _, err := c.JS.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket: b.name, History: b.history, TTL: b.ttl, Storage: jetstream.FileStorage,
		}); err != nil {
			return fmt.Errorf("bucket %s: %w", b.name, err)
		}
	}
	return nil
}

// EnsureInbox creates p's durable inbox consumer.
func EnsureInbox(ctx context.Context, c *Client, p Path) error {
	if _, err := c.JS.CreateOrUpdateConsumer(ctx, StreamMesh, jetstream.ConsumerConfig{
		Durable:       InboxConsumer(p),
		FilterSubject: InboxSubject(p),
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       2 * time.Minute,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	}); err != nil {
		return fmt.Errorf("inbox consumer for %s: %w", p, err)
	}
	return nil
}
