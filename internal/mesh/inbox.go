package mesh

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
)

// Message is one inbox entry. Kind is message, wake, job or reply; a job
// carries Job and a reply carries Reply.
type Message struct {
	ID    string `json:"id"`
	From  string `json:"from"`
	To    string `json:"to"`
	Kind  string `json:"kind"`
	At    string `json:"at"`
	Text  string `json:"text"`
	Job   *Job   `json:"job,omitempty"`
	Reply *Reply `json:"reply,omitempty"`
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
