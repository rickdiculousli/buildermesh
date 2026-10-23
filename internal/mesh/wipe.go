package mesh

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nats-io/nats.go/jetstream"
)

// Wiped is what a wipe removes of one node from the mesh: its events, its
// keys with any history (in bm-nodes, bm-state, bm-runner, bm-holds and
// bm-ready)
// and, when it had messages or a consumer, its inbox.
type Wiped struct {
	Events int
	Keys   int
	Inbox  bool
}

// wipeBuckets are the buckets keyed by node path, bm-nodes first: a
// daemon that finds no node writes nothing for it.
var wipeBuckets = []string{BucketNodes, BucketState, BucketRunner, BucketHolds, BucketReady}

// WipeTree counts what the mesh holds of root and the nodes under it
// (paths, root among them) and, with apply, purges it: the events on
// bm.<root>.>, the inboxes on bm.inbox.<root> and bm.inbox.<root>.>,
// every message of the keys <root> and <root>.> in each path-keyed bucket
// (no history and no purge marker stays) and each path's inbox consumer.
// Every filter names root as whole tokens, so a sibling whose path merely
// starts with root's text is never matched. What belongs to a path no
// longer registered (a child forgotten before) counts toward its nearest
// listed ancestor. A routine's path is refused: its inbox is its kind's
// queue.
func WipeTree(ctx context.Context, c *Client, root string, paths []string, apply bool) (map[string]*Wiped, error) {
	p, err := ParsePath(root)
	if err != nil {
		return nil, err
	}
	if _, ok := KindFromPath(p); ok {
		return nil, fmt.Errorf("%s is a routine's; its inbox is its kind's queue", root)
	}
	out := map[string]*Wiped{}
	for _, q := range paths {
		if q != root && !strings.HasPrefix(q, root+".") {
			return nil, fmt.Errorf("%s is not under %s", q, root)
		}
		out[q] = &Wiped{}
	}
	if out[root] == nil {
		out[root] = &Wiped{}
	}
	owner := func(q string) *Wiped {
		for {
			if w, ok := out[q]; ok {
				return w
			}
			i := strings.LastIndex(q, ".")
			if i < 0 {
				return out[root]
			}
			q = q[:i]
		}
	}
	under := func(prefix string) []string { return []string{prefix + root, prefix + root + ".>"} }

	// Count first: a purge that fails halfway is run again, and counts
	// what is left.
	type purge struct{ stream, filter string }
	var purges []purge
	for _, b := range wipeBuckets {
		stream, prefix := "KV_"+b, "$KV."+b+"."
		counts, err := subjectCounts(ctx, c, stream, under(prefix)...)
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			continue // bm-holds and bm-ready are made on demand
		}
		if err != nil {
			return nil, err
		}
		for subj := range counts {
			owner(strings.TrimPrefix(subj, prefix)).Keys++
		}
		for _, f := range under(prefix) {
			purges = append(purges, purge{stream, f})
		}
	}
	events, err := subjectCounts(ctx, c, StreamMesh, "bm."+root+".>")
	if err != nil {
		return nil, err
	}
	for subj, n := range events {
		rest := strings.TrimPrefix(subj, "bm.")
		owner(rest[:strings.LastIndex(rest, ".")]).Events += int(n)
	}
	inboxes, err := subjectCounts(ctx, c, StreamMesh, under("bm.inbox.")...)
	if err != nil {
		return nil, err
	}
	for subj := range inboxes {
		owner(strings.TrimPrefix(subj, "bm.inbox.")).Inbox = true
	}
	for q, w := range out {
		qp, err := ParsePath(q)
		if err != nil {
			return nil, err
		}
		_, err = c.JS.Consumer(ctx, StreamMesh, InboxConsumer(qp))
		switch {
		case err == nil:
			w.Inbox = true
		case !errors.Is(err, jetstream.ErrConsumerNotFound):
			return nil, err
		}
	}
	if !apply {
		return out, nil
	}

	purges = append(purges, purge{StreamMesh, "bm." + root + ".>"})
	for _, f := range under("bm.inbox.") {
		purges = append(purges, purge{StreamMesh, f})
	}
	for _, pg := range purges {
		st, err := c.JS.Stream(ctx, pg.stream)
		if err != nil {
			return nil, err
		}
		if err := st.Purge(ctx, jetstream.WithPurgeSubject(pg.filter)); err != nil {
			return nil, fmt.Errorf("purge %s: %w", pg.filter, err)
		}
	}
	for q := range out {
		qp, _ := ParsePath(q)
		if err := c.JS.DeleteConsumer(ctx, StreamMesh, InboxConsumer(qp)); err != nil && !errors.Is(err, jetstream.ErrConsumerNotFound) {
			return nil, fmt.Errorf("inbox consumer for %s: %w", q, err)
		}
	}
	return out, nil
}

// WipeFeedback counts the family's pending feedback and, with apply,
// purges it.
func WipeFeedback(ctx context.Context, c *Client, family string, apply bool) (int, error) {
	counts, err := subjectCounts(ctx, c, StreamMesh, FeedbackSubject(family))
	if err != nil {
		return 0, err
	}
	n := int(counts[FeedbackSubject(family)])
	if !apply || n == 0 {
		return n, nil
	}
	st, err := c.JS.Stream(ctx, StreamMesh)
	if err != nil {
		return 0, err
	}
	if err := st.Purge(ctx, jetstream.WithPurgeSubject(FeedbackSubject(family))); err != nil {
		return 0, fmt.Errorf("purge %s: %w", FeedbackSubject(family), err)
	}
	return n, nil
}

// subjectCounts is the stream's message count per subject matching any of
// filters.
func subjectCounts(ctx context.Context, c *Client, stream string, filters ...string) (map[string]uint64, error) {
	st, err := c.JS.Stream(ctx, stream)
	if err != nil {
		return nil, err
	}
	out := map[string]uint64{}
	for _, f := range filters {
		info, err := st.Info(ctx, jetstream.WithSubjectFilter(f))
		if err != nil {
			return nil, err
		}
		for s, n := range info.State.Subjects {
			out[s] += n
		}
	}
	return out, nil
}
