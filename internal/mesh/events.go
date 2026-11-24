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
	Body   string `json:"body,omitempty"` // a digest's body
	Seq    uint64 `json:"seq,omitempty"`  // a push claim's event: its stream sequence
	// Detail is a daemon event's push body when it says more than the
	// text, the push's title: what a hold changes and how to answer it.
	Detail string `json:"detail,omitempty"`
	// Can is what a daemon's started event says it does beyond the
	// basics: "land" when it lands for its box's leads (bmesh ready).
	Can []string `json:"can,omitempty"`
	// Land is a land event's account of where its run is.
	Land *Land `json:"land,omitempty"`
	// Run is a run event's account of the command bmesh run runs.
	Run *Run `json:"run,omitempty"`
}

// Run is a command bmesh run runs under a node: the program's base name
// (never its args or output), a sha256 of its argv and cwd, when it
// started (RFC3339), the lines it printed and the CPU seconds its group
// used so far, how many seconds it has been quiet, and, on its last
// event, how it ended: ok, exit <n>, stalled, timed out or interrupted.
type Run struct {
	Prog  string `json:"prog"`
	Key   string `json:"key"`
	Since string `json:"since"`
	Lines int64  `json:"lines"`
	CPU   int64  `json:"cpu"`
	Quiet int64  `json:"quiet"`
	End   string `json:"end,omitempty"`
}

// The steps of a land run, in the order it takes them: before the lease
// (steps 1-5, 6, 7-8, 9, 10 of bmesh land), then under it (1-3, 4, 5-8).
const (
	LandChecks  = "checks"
	LandKinds   = "kinds"
	LandJobs    = "jobs"
	LandMain    = "main"
	LandLease   = "lease"
	LandRebase  = "rebase"
	LandResults = "results"
	LandPush    = "push"
)

// Land is where a land run is: when the run and its step began (RFC3339),
// the command it runs, the jobs it waits for, and, on the run's last event,
// how it ended (landed <sha8>, or its error line).
type Land struct {
	Run   string    `json:"run"`
	Step  string    `json:"step"`
	Since string    `json:"since"`
	Cmd   *LandCmd  `json:"cmd,omitempty"`
	Jobs  []LandJob `json:"jobs,omitempty"`
	End   string    `json:"end,omitempty"`
}

// LandCmd is an inline kind's command (or the worktree's setup, kind
// "setup") a land run runs: the command itself only on the first event
// after it started, its newest non-empty output line and when it was
// printed, the lines so far, and while it waits for a slot, where it waits.
type LandCmd struct {
	Kind    string    `json:"kind"`
	Command string    `json:"command,omitempty"`
	Since   string    `json:"since"`
	Last    string    `json:"last,omitempty"`
	LastAt  string    `json:"last_at,omitempty"`
	Lines   int       `json:"lines"`
	Wait    *LandWait `json:"wait,omitempty"`
}

// LandWait is the slot an inline kind waits for, who holds it, and the
// run's place in its line.
type LandWait struct {
	Slot   string `json:"slot"`
	Holder string `json:"holder"`
	Place  int    `json:"place"`
}

// LandJob is a job a land run waits for the reply to: waiting, held or
// running, and the box holding it.
type LandJob struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	State string `json:"state"`
	Host  string `json:"host,omitempty"`
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
	_, err := publishEvent(ctx, c, ev, "")
	return err
}

// PublishEventID is PublishEvent with a Nats-Msg-Id: dup reports that the
// stream already holds msgID inside its duplicate window, so nothing was
// stored.
func PublishEventID(ctx context.Context, c *Client, ev Event, msgID string) (dup bool, err error) {
	if msgID == "" {
		return false, errors.New("PublishEventID: empty msg id")
	}
	return publishEvent(ctx, c, ev, msgID)
}

func publishEvent(ctx context.Context, c *Client, ev Event, msgID string) (bool, error) {
	if !tokenRE.MatchString(ev.Event) {
		return false, fmt.Errorf("invalid event %q: want a single token of letters, digits, - or _", ev.Event)
	}
	var subject string
	if strings.HasPrefix(ev.From, "daemon.") {
		host := strings.Split(ev.From, ".")[1]
		if host == "" {
			return false, fmt.Errorf("invalid daemon sender %q", ev.From)
		}
		subject = DaemonSubject(host, ev.Event)
	} else {
		p, err := ParsePath(ev.From)
		if err != nil {
			return false, err
		}
		subject = EventSubject(p, ev.Event)
	}
	if ev.At == "" {
		ev.At = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return false, err
	}
	opts := []jetstream.PublishOpt{jetstream.WithExpectStream(StreamMesh)}
	if msgID != "" {
		opts = append(opts, jetstream.WithMsgID(msgID))
	}
	ack, err := c.JS.Publish(ctx, subject, data, opts...)
	if err != nil {
		return false, fmt.Errorf("publish %s: %w", subject, err)
	}
	return ack.Duplicate, nil
}

// History replays the stream messages matching filter (from since, when set)
// oldest first and returns once caught up. The consumer is ordered, so no
// durable state is touched.
func History(ctx context.Context, c *Client, filter string, since time.Time) ([]Stored, error) {
	cfg := jetstream.OrderedConsumerConfig{FilterSubjects: []string{filter}, DeliverPolicy: jetstream.DeliverAllPolicy}
	if !since.IsZero() {
		cfg.DeliverPolicy = jetstream.DeliverByStartTimePolicy
		cfg.OptStartTime = &since
	}
	cons, err := c.JS.OrderedConsumer(ctx, StreamMesh, cfg)
	if err != nil {
		return nil, err
	}
	// The create response counts what this consumer will deliver from since.
	total := int(cons.CachedInfo().NumPending)
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
// when ctx ends. After a lost connection the consumer resumes past the last
// message it delivered, so what was published during the gap arrives late
// as live: Stored.At tells such a backlog apart.
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

// Last is the newest stream message on subject, which may hold wildcards;
// ok is false when there is none.
func Last(ctx context.Context, c *Client, subject string) (s Stored, ok bool, err error) {
	st, err := c.JS.Stream(ctx, StreamMesh)
	if err != nil {
		return Stored{}, false, err
	}
	raw, err := st.GetLastMsgForSubject(ctx, subject)
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return Stored{}, false, nil
	}
	if err != nil {
		return Stored{}, false, err
	}
	return Stored{Subject: raw.Subject, Seq: raw.Sequence, At: raw.Time, Data: raw.Data}, true, nil
}
