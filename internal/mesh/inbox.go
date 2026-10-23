package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
)

// Message is one inbox entry. Kind is message, wake, job, reply or report; a
// job carries Job, a reply carries Reply and a report carries Report.
type Message struct {
	ID     string  `json:"id"`
	From   string  `json:"from"`
	To     string  `json:"to"`
	Kind   string  `json:"kind"`
	At     string  `json:"at"`
	Text   string  `json:"text"`
	Job    *Job    `json:"job,omitempty"`
	Reply  *Reply  `json:"reply,omitempty"`
	Report *Report `json:"report,omitempty"`
}

// Report is an executor's done or blocked as its parent's inbox receives it;
// Branch and Commit are set for done.
type Report struct {
	Event  string `json:"event"`
	From   string `json:"from"`
	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`
}

// Send delivers m to its recipient's inbox. dup reports a repeat of an id the
// stream already holds, which is not stored twice.
func Send(ctx context.Context, c *Client, m Message) (dup bool, err error) {
	to, err := ParsePath(m.To)
	if err != nil {
		return false, err
	}
	if m.ID == "" {
		m.ID = nuid.Next()
	}
	if m.At == "" {
		m.At = time.Now().UTC().Format(time.RFC3339)
	}
	if m.Kind == "" {
		m.Kind = "message"
	}
	if err := ensureAnyInbox(ctx, c, to); err != nil {
		return false, err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return false, err
	}
	ack, err := c.JS.Publish(ctx, InboxSubject(to), data, jetstream.WithMsgID(m.ID), jetstream.WithExpectStream(StreamMesh))
	if err != nil {
		return false, fmt.Errorf("send to %s: %w", to, err)
	}
	return ack.Duplicate, nil
}

// Fetch pulls up to 100 messages from p's inbox, what it holds now without
// waiting for more, acking them when ack and returning them to the inbox
// otherwise.
func Fetch(ctx context.Context, c *Client, p Path, ack bool) ([]Message, error) {
	if err := ensureAnyInbox(ctx, c, p); err != nil {
		return nil, err
	}
	cons, err := c.JS.Consumer(ctx, StreamMesh, InboxConsumer(p))
	if err != nil {
		return nil, err
	}
	batch, err := cons.FetchNoWait(100)
	if err != nil {
		return nil, err
	}
	// Settle after the batch ends: a Nak inside it would redeliver into it.
	var msgs []jetstream.Msg
	var out []Message
	for m := range batch.Messages() {
		var msg Message
		if err := json.Unmarshal(m.Data(), &msg); err != nil {
			msg = Message{Kind: "malformed", Text: string(m.Data())}
		}
		msgs = append(msgs, m)
		out = append(out, msg)
	}
	if err := batch.Error(); err != nil {
		return nil, err
	}
	for _, m := range msgs {
		var err error
		if ack {
			err = m.Ack()
		} else {
			err = m.Nak()
		}
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// Unread counts p's undelivered plus unacked messages.
func Unread(ctx context.Context, c *Client, p Path) (int, error) {
	if err := ensureAnyInbox(ctx, c, p); err != nil {
		return 0, err
	}
	cons, err := c.JS.Consumer(ctx, StreamMesh, InboxConsumer(p))
	if err != nil {
		return 0, err
	}
	info, err := cons.Info(ctx)
	if err != nil {
		return 0, err
	}
	return int(info.NumPending) + info.NumAckPending, nil
}

// Pending reads p's inbox subject from the stream after its consumer's ack
// floor, oldest first, without fetching: no message is held in flight or
// acked. A message acked out of order (a reply `routine wait` took) can sit
// above the floor, so this may list more than Unread counts.
func Pending(ctx context.Context, c *Client, p Path) ([]Message, error) {
	if err := ensureAnyInbox(ctx, c, p); err != nil {
		return nil, err
	}
	cons, err := c.JS.Consumer(ctx, StreamMesh, InboxConsumer(p))
	if err != nil {
		return nil, err
	}
	info, err := cons.Info(ctx)
	if err != nil {
		return nil, err
	}
	if info.NumPending == 0 && info.NumAckPending == 0 {
		return nil, nil
	}
	ord, err := c.JS.OrderedConsumer(ctx, StreamMesh, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{InboxSubject(p)},
		DeliverPolicy:  jetstream.DeliverByStartSequencePolicy,
		OptStartSeq:    info.AckFloor.Stream + 1,
	})
	if err != nil {
		return nil, err
	}
	var out []Message
	for {
		m, err := ord.Next(jetstream.FetchMaxWait(2 * time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			if errors.Is(err, jetstream.ErrNoMessages) || errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
				return out, nil
			}
			return out, err
		}
		var msg Message
		if err := json.Unmarshal(m.Data(), &msg); err != nil {
			msg = Message{Kind: "malformed", Text: string(m.Data())}
		}
		out = append(out, msg)
		meta, err := m.Metadata()
		if err != nil {
			return out, err
		}
		if meta.NumPending == 0 {
			return out, nil
		}
	}
}

// InboxSeq is the stream sequence of the newest message on p's inbox
// subject, read or not; 0 when it has none.
func InboxSeq(ctx context.Context, c *Client, p Path) (uint64, error) {
	st, err := c.JS.Stream(ctx, StreamMesh)
	if err != nil {
		return 0, err
	}
	m, err := st.GetLastMsgForSubject(ctx, InboxSubject(p))
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return m.Sequence, nil
}

// InboxAfter lists the messages on p's inbox subject after stream sequence
// after, read or not, oldest first.
func InboxAfter(ctx context.Context, c *Client, p Path, after uint64) ([]Message, error) {
	st, err := c.JS.Stream(ctx, StreamMesh)
	if err != nil {
		return nil, err
	}
	var out []Message
	err = scanSubject(ctx, c, st, InboxSubject(p), after+1, func(_ uint64, m Message) (bool, error) {
		out = append(out, m)
		return true, nil
	})
	return out, err
}
