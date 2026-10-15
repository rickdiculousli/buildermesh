package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Event is one thing a node or the daemon reports.
type Event struct {
	From   string `json:"from"`
	Event  string `json:"event"`
	At     string `json:"at"`
	Text   string `json:"text"`
	Commit string `json:"commit"`
	To     string `json:"to,omitempty"`
}

// Stored is a stream message as read back.
type Stored struct {
	Subject string
	Seq     uint64
	At      time.Time
	Data    []byte
}

var tokenRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// PublishEvent publishes ev on its event subject (daemon.<host> senders use
// the daemon subject), filling at when empty.
func PublishEvent(ctx context.Context, c *Client, ev Event) error {
	if !tokenRE.MatchString(ev.Event) {
		return fmt.Errorf("invalid event %q: want a single token of letters, digits, - or _", ev.Event)
	}
	var subject string
	if strings.HasPrefix(ev.From, "daemon.") {
		host := strings.Split(ev.From, ".")[1]
		if host == "" {
			return fmt.Errorf("invalid daemon sender %q", ev.From)
		}
		subject = DaemonSubject(host, ev.Event)
	} else {
		p, err := ParsePath(ev.From)
		if err != nil {
			return err
		}
		subject = EventSubject(p, ev.Event)
	}
	if ev.At == "" {
		ev.At = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := c.JS.Publish(ctx, subject, data, jetstream.WithExpectStream(StreamMesh)); err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}

// History replays the stream messages matching filter (from since, when set)
// oldest first and returns once caught up. The consumer is ordered, so no
// durable state is touched.
func History(ctx context.Context, c *Client, filter string, since time.Time) ([]Stored, error) {
	st, err := c.JS.Stream(ctx, StreamMesh)
	if err != nil {
		return nil, err
	}
	info, err := st.Info(ctx, jetstream.WithSubjectFilter(filter))
	if err != nil {
		return nil, err
	}
	total := 0
	for _, n := range info.State.Subjects {
		total += int(n)
	}
	if total == 0 || (!since.IsZero() && since.After(info.State.LastTime)) {
		return nil, nil
	}
	cfg := jetstream.OrderedConsumerConfig{FilterSubjects: []string{filter}, DeliverPolicy: jetstream.DeliverAllPolicy}
	if !since.IsZero() {
		cfg.DeliverPolicy = jetstream.DeliverByStartTimePolicy
		cfg.OptStartTime = &since
	}
	cons, err := c.JS.OrderedConsumer(ctx, StreamMesh, cfg)
	if err != nil {
		return nil, err
	}
	var out []Stored
	for len(out) < total {
		m, err := cons.Next(jetstream.FetchMaxWait(2 * time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			if errors.Is(err, jetstream.ErrNoMessages) || errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
				return out, nil
			}
			return out, err
		}
		meta, err := m.Metadata()
		if err != nil {
			return out, err
		}
		out = append(out, Stored{Subject: m.Subject(), Seq: meta.Sequence.Stream, At: meta.Timestamp, Data: m.Data()})
		if meta.NumPending == 0 {
			break
		}
	}
	return out, nil
}

// Follow delivers matching messages published from now on. The channel closes
// when ctx ends.
func Follow(ctx context.Context, c *Client, filter string) (<-chan Stored, error) {
	cons, err := c.JS.OrderedConsumer(ctx, StreamMesh, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{filter},
		DeliverPolicy:  jetstream.DeliverNewPolicy,
	})
	if err != nil {
		return nil, err
	}
	ch := make(chan Stored)
	var mu sync.RWMutex // held by a callback while it sends, so close waits for it
	closed := false
	cc, err := cons.Consume(func(m jetstream.Msg) {
		meta, err := m.Metadata()
		if err != nil {
			return
		}
		s := Stored{Subject: m.Subject(), Seq: meta.Sequence.Stream, At: meta.Timestamp, Data: m.Data()}
		mu.RLock()
		defer mu.RUnlock()
		if closed {
			return
		}
		select {
		case ch <- s:
		case <-ctx.Done():
		}
	})
	if err != nil {
		return nil, err
	}
	go func() {
		<-ctx.Done()
		cc.Stop()
		mu.Lock()
		closed = true
		close(ch)
		mu.Unlock()
	}()
	return ch, nil
}
