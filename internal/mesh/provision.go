package mesh

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Provision creates the stream and buckets; it is safe to run repeatedly.
func Provision(ctx context.Context, c *Client) error {
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
	}{{BucketNodes, 1}, {BucketState, 16}, {BucketRunner, 1}, {BucketResults, 1}, {BucketLeases, 1}} {
		if _, err := c.JS.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket: b.name, History: b.history, Storage: jetstream.FileStorage,
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
