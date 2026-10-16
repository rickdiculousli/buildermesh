package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Hold is a job a daemon has fetched and not settled: held (in a pool
// waiting for an instance, or in an instance's batch) or running. Holder
// is the instance path, or daemon.<host> while the job waits for one.
type Hold struct {
	Seq    uint64 `json:"seq"` // the job message's stream sequence
	Job    string `json:"job"`
	From   string `json:"from"`
	Holder string `json:"holder"`
	State  string `json:"state"` // held or running
	Since  string `json:"since"`
}

// Holds is one daemon's unsettled jobs of one kind, as it writes them to
// bm-jobs under <kind>.<host> on every change.
type Holds struct {
	Kind string `json:"kind"`
	Host string `json:"host"`
	At   string `json:"at"`
	Jobs []Hold `json:"jobs"`
}

// PutHolds writes h under <kind>.<host>.
func PutHolds(ctx context.Context, c *Client, h Holds) error {
	if h.Jobs == nil {
		h.Jobs = []Hold{}
	}
	return put(ctx, c, BucketJobs, h.Kind+"."+h.Host, h)
}

// ListHolds returns every daemon's holds of kindName ("" = every kind);
// none when bm-jobs does not exist (a hub provisioned before it).
func ListHolds(ctx context.Context, c *Client, kindName string) ([]Holds, error) {
	kv, err := bucket(ctx, c, BucketJobs)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	filter := ">"
	if kindName != "" {
		filter = kindName + ".>"
	}
	keys, err := kv.ListKeysFiltered(ctx, filter)
	if err != nil {
		return nil, err
	}
	var out []Holds
	for key := range keys.Keys() {
		e, err := kv.Get(ctx, key)
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var h Holds
		if json.Unmarshal(e.Value(), &h) == nil {
			out = append(out, h)
		}
	}
	return out, nil
}

// QueuedJob is a job of a kind not yet answered: waiting in the kind's
// inbox, or held or running on a daemon.
type QueuedJob struct {
	ID     string `json:"id"`
	From   string `json:"from"`
	State  string `json:"state"` // waiting, held or running
	Holder string `json:"holder,omitempty"`
	Seq    uint64 `json:"seq"`
}

// KindJobs lists kindName's unanswered jobs in stream order: every job on
// its inbox past the consumer's ack floor that has no reply in its
// requester's inbox, held or running as a live daemon's bm-jobs entry says,
// else waiting (never delivered, handed back, or held by a daemon that
// died). An entry not rewritten within the kind's AckWait is a dead
// daemon's: a live one rewrites it with every in-progress heartbeat.
func KindJobs(ctx context.Context, c *Client, kindName string) ([]QueuedJob, error) {
	cons, err := c.JS.Consumer(ctx, StreamMesh, InboxConsumer(RoutinePath(kindName)))
	if errors.Is(err, jetstream.ErrConsumerNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := cons.Info(ctx)
	if err != nil {
		return nil, err
	}
	floor, delivered := info.AckFloor.Stream, info.Delivered.Stream
	all, err := ListHolds(ctx, c, kindName)
	if err != nil {
		return nil, err
	}
	live := time.Now().Add(-info.Config.AckWait)
	held := map[uint64]Hold{}
	newest := map[uint64]string{} // seq → the At of the Holds it came from
	for _, hs := range all {
		at, err := time.Parse(time.RFC3339, hs.At)
		if err != nil || at.Before(live) {
			continue
		}
		for _, h := range hs.Jobs {
			if h.Seq > floor && hs.At >= newest[h.Seq] {
				held[h.Seq], newest[h.Seq] = h, hs.At
			}
		}
	}
	st, err := c.JS.Stream(ctx, StreamMesh)
	if err != nil {
		return nil, err
	}
	var out []QueuedJob
	err = scanSubject(ctx, c, st, InboxSubject(RoutinePath(kindName)), floor+1, func(seq uint64, m Message) (bool, error) {
		if m.Kind != "job" || m.Job == nil {
			return true, nil
		}
		if seq <= delivered {
			replied, err := hasReply(ctx, c, st, m.From, m.Job.ID, seq)
			if err != nil || replied {
				return err == nil, err
			}
		}
		q := QueuedJob{ID: m.Job.ID, From: m.From, State: "waiting", Seq: seq}
		if h, ok := held[seq]; ok {
			q.State, q.Holder = h.State, h.Holder
		}
		out = append(out, q)
		return true, nil
	})
	return out, err
}

// hasReply reports a reply to job in from's inbox after sequence after.
func hasReply(ctx context.Context, c *Client, st jetstream.Stream, from, job string, after uint64) (bool, error) {
	p, err := ParsePath(from)
	if err != nil {
		return false, nil
	}
	found := false
	err = scanSubject(ctx, c, st, InboxSubject(p), after+1, func(_ uint64, m Message) (bool, error) {
		found = m.Kind == "reply" && m.Reply != nil && m.Reply.Job == job
		return !found, nil
	})
	return found, err
}

// scanSubject calls f with each message on subject from sequence from on,
// in order, until f returns false or the subject ends.
func scanSubject(ctx context.Context, c *Client, st jetstream.Stream, subject string, from uint64, f func(uint64, Message) (bool, error)) error {
	// An ordered consumer on an empty range would wait for the next message.
	first, err := st.GetMsg(ctx, from, jetstream.WithGetMsgSubject(subject))
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	cons, err := c.JS.OrderedConsumer(ctx, StreamMesh, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{subject},
		DeliverPolicy:  jetstream.DeliverByStartSequencePolicy,
		OptStartSeq:    first.Sequence,
	})
	if err != nil {
		return err
	}
	it, err := cons.Messages()
	if err != nil {
		return err
	}
	defer it.Stop()
	for {
		raw, err := it.Next(jetstream.NextContext(ctx))
		if err != nil {
			return err
		}
		meta, err := raw.Metadata()
		if err != nil {
			return err
		}
		var m Message
		if json.Unmarshal(raw.Data(), &m) == nil {
			if more, err := f(meta.Sequence.Stream, m); err != nil || !more {
				return err
			}
		}
		if meta.NumPending == 0 {
			return nil
		}
	}
}

// ClearHolds empties every bm-jobs entry host wrote, so what a daemon that
// died there held is not shown as held; at stamps the emptied entries.
func ClearHolds(ctx context.Context, c *Client, host, at string) error {
	all, err := ListHolds(ctx, c, "")
	if err != nil {
		return err
	}
	for _, h := range all {
		if h.Host == host && len(h.Jobs) > 0 {
			if err := PutHolds(ctx, c, Holds{Kind: h.Kind, Host: host, At: at}); err != nil {
				return err
			}
		}
	}
	return nil
}
