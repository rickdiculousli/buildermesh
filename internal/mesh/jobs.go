package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"

	"github.com/rickdiculousli/buildermesh/internal/kind"
)

// Job asks a kind's routine to run against a commit. Repo is the name the
// daemon resolves through [repos].
type Job struct {
	ID     string   `json:"id"`
	Kind   string   `json:"kind"`
	Repo   string   `json:"repo"`
	Commit string   `json:"commit"`
	Tree   string   `json:"tree"`
	Args   []string `json:"args"`
	Model  string   `json:"model"`
	Effort string   `json:"effort,omitempty"`
	Text   string   `json:"text"`
	Go     bool     `json:"go"`
	Rerun  bool     `json:"rerun"`
}

// Reply answers a job in the requester's inbox.
type Reply struct {
	Job       string `json:"job"`
	Kind      string `json:"kind"`
	Tree      string `json:"tree"`
	OK        bool   `json:"ok"`
	Output    string `json:"output"`
	ResultKey string `json:"result_key"`
	By        string `json:"by"`
}

// maxOutput bounds a recorded or replied output.
const maxOutput = 64 << 10

// Truncate cuts s to 64 KiB, marking the cut with a trailing [truncated] line.
func Truncate(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	return s[:maxOutput] + "\n[truncated]"
}

// Default kind consumer settings, used when a requester queues before any
// daemon has set the kind's own (timeout + 1 min). MaxAckPending is
// unlimited, as every daemon writes it: the consumer is shared by every box
// that runs the kind, and each box gates its own fetches.
const (
	defaultKindMaxAck  = -1
	defaultKindAckWait = 21 * time.Minute
)

// kindConsumerConfig is the kind's inbox consumer: at most maxAck jobs out
// at once (-1 for no limit), each redelivered when unacked after ackWait.
func kindConsumerConfig(kindName string, maxAck int, ackWait time.Duration) jetstream.ConsumerConfig {
	p := RoutinePath(kindName)
	return jetstream.ConsumerConfig{
		Durable:       InboxConsumer(p),
		FilterSubject: InboxSubject(p),
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       ackWait,
		MaxAckPending: maxAck,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	}
}

// KindConsumer creates or updates the kind's inbox consumer: at most maxAck
// jobs out at once (-1 for no limit), each redelivered when unacked after
// ackWait.
func KindConsumer(ctx context.Context, c *Client, kindName string, maxAck int, ackWait time.Duration) (jetstream.Consumer, error) {
	cons, err := c.JS.CreateOrUpdateConsumer(ctx, StreamMesh, kindConsumerConfig(kindName, maxAck, ackWait))
	if err != nil {
		return nil, fmt.Errorf("kind consumer for %s: %w", kindName, err)
	}
	return cons, nil
}

// EnsureKindInbox creates the kind's consumer with default settings when it
// does not exist. The create fails on an existing consumer with other
// settings, so a requester never overwrites a daemon's.
func EnsureKindInbox(ctx context.Context, c *Client, kindName string) error {
	_, err := c.JS.CreateConsumer(ctx, StreamMesh, kindConsumerConfig(kindName, defaultKindMaxAck, defaultKindAckWait))
	if errors.Is(err, jetstream.ErrConsumerExists) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("kind consumer for %s: %w", kindName, err)
	}
	return nil
}

// ensureAnyInbox ensures p's consumer: a kind's for routine.<kind>, the
// plain inbox for every other path.
func ensureAnyInbox(ctx context.Context, c *Client, p Path) error {
	if k, ok := KindFromPath(p); ok && len(p) == 2 {
		return EnsureKindInbox(ctx, c, k)
	}
	return EnsureInbox(ctx, c, p)
}

// Queue puts j on its kind's inbox from from. It resolves j.Commit (default
// HEAD) in repoPath to a full SHA and fills Tree, Repo (the base name of
// repoPath, when empty) and ID (when empty). dup reports a job id the stream
// already holds.
func Queue(ctx context.Context, c *Client, from Path, j Job, repoPath string) (dup bool, err error) {
	if !kind.NameRE.MatchString(j.Kind) {
		return false, fmt.Errorf("kind name %q: want lowercase letters, digits and hyphens", j.Kind)
	}
	if j.Commit == "" {
		j.Commit = "HEAD"
	}
	sha, err := revParse(ctx, repoPath, "--verify", "--end-of-options", j.Commit+"^{commit}")
	if err != nil {
		return false, fmt.Errorf("commit %q: not a commit in %s", j.Commit, repoPath)
	}
	if j.Tree, err = revParse(ctx, repoPath, sha+"^{tree}"); err != nil {
		return false, err
	}
	j.Commit = sha
	if j.Repo == "" {
		j.Repo = filepath.Base(repoPath)
	}
	if j.ID == "" {
		j.ID = nuid.Next()
	}
	if j.Args == nil {
		j.Args = []string{}
	}
	return Send(ctx, c, Message{ID: j.ID, Kind: "job", From: from.String(), To: RoutinePath(j.Kind).String(), Job: &j})
}

func revParse(ctx context.Context, repo string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", repo, "rev-parse"}, args...)...).Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s in %s: %w", strings.Join(args, " "), repo, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Depth counts the kind's waiting plus in-flight jobs; 0 when its consumer
// does not exist.
func Depth(ctx context.Context, c *Client, kindName string) (int, error) {
	cons, err := c.JS.Consumer(ctx, StreamMesh, InboxConsumer(RoutinePath(kindName)))
	if errors.Is(err, jetstream.ErrConsumerNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	info, err := caughtUp(ctx, c, cons, RoutinePath(kindName))
	if err != nil {
		return 0, err
	}
	return int(info.NumPending) + info.NumAckPending, nil
}

// SendReply delivers r to to's inbox; the message text is the output's
// first line and the output is truncated to 64 KiB.
func SendReply(ctx context.Context, c *Client, from, to Path, r Reply) error {
	r.Output = Truncate(r.Output)
	first, _, _ := strings.Cut(r.Output, "\n")
	_, err := Send(ctx, c, Message{Kind: "reply", From: from.String(), To: to.String(), Text: first, Reply: &r})
	return err
}

// FetchMatch pulls what p's inbox holds now (up to 100 messages), acks the
// first message match accepts and returns the rest to the inbox. ok is false
// when none matched.
func FetchMatch(ctx context.Context, c *Client, p Path, match func(Message) bool) (Message, bool, error) {
	if err := ensureAnyInbox(ctx, c, p); err != nil {
		return Message{}, false, err
	}
	cons, err := c.JS.Consumer(ctx, StreamMesh, InboxConsumer(p))
	if err != nil {
		return Message{}, false, err
	}
	if _, err := caughtUp(ctx, c, cons, p); err != nil {
		return Message{}, false, err
	}
	batch, err := cons.FetchNoWait(100)
	if err != nil {
		return Message{}, false, err
	}
	// Settle after the batch ends: a Nak inside it would redeliver into it.
	var msgs []jetstream.Msg
	var found Message
	hit := -1
	for m := range batch.Messages() {
		var msg Message
		if hit < 0 && json.Unmarshal(m.Data(), &msg) == nil && match(msg) {
			hit, found = len(msgs), msg
		}
		msgs = append(msgs, m)
	}
	if err := batch.Error(); err != nil {
		return Message{}, false, err
	}
	for i, m := range msgs {
		var err error
		if i == hit {
			err = m.DoubleAck(ctx) // the caller acts on it, so be sure it is gone
		} else {
			err = m.Nak()
		}
		if err != nil {
			return Message{}, false, err
		}
	}
	return found, hit >= 0, nil
}

// WaitReply returns the reply to job from p's inbox subject, read from the
// stream rather than the consumer: the subject keeps every message for the
// stream's age, acked or not, so a reply `inbox` already read is found too.
// It scans the history, then follows the subject until the reply arrives or
// ctx ends.
func WaitReply(ctx context.Context, c *Client, p Path, job string) (Message, error) {
	cons, err := c.JS.OrderedConsumer(ctx, StreamMesh, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{InboxSubject(p)},
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return Message{}, err
	}
	it, err := cons.Messages()
	if err != nil {
		return Message{}, err
	}
	defer it.Stop()
	for {
		m, err := it.Next(jetstream.NextContext(ctx))
		if err != nil {
			return Message{}, err
		}
		var msg Message
		if json.Unmarshal(m.Data(), &msg) == nil && msg.Kind == "reply" && msg.Reply != nil && msg.Reply.Job == job {
			return msg, nil
		}
	}
}

// JobKind finds the kind a job id was queued to, scanning the headers of
// every kind's inbox; ok is false when no kind's inbox holds it.
func JobKind(ctx context.Context, c *Client, id string) (string, bool, error) {
	filter := InboxSubject(RoutinePath("*"))
	st, err := c.JS.Stream(ctx, StreamMesh)
	if err != nil {
		return "", false, err
	}
	info, err := st.Info(ctx, jetstream.WithSubjectFilter(filter))
	if err != nil {
		return "", false, err
	}
	if len(info.State.Subjects) == 0 {
		return "", false, nil
	}
	cons, err := c.JS.OrderedConsumer(ctx, StreamMesh, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{filter},
		DeliverPolicy:  jetstream.DeliverAllPolicy,
		HeadersOnly:    true,
	})
	if err != nil {
		return "", false, err
	}
	it, err := cons.Messages()
	if err != nil {
		return "", false, err
	}
	defer it.Stop()
	prefix := InboxSubject(RoutinePath(""))
	for {
		m, err := it.Next(jetstream.NextContext(ctx))
		if err != nil {
			return "", false, err
		}
		if m.Headers().Get(jetstream.MsgIDHeader) == id {
			return strings.TrimPrefix(m.Subject(), prefix), true, nil
		}
		meta, err := m.Metadata()
		if err != nil {
			return "", false, err
		}
		if meta.NumPending == 0 {
			return "", false, nil
		}
	}
}

// FindJob reads the job id queued to kindName from the kind's inbox
// history; ok is false when the stream no longer holds it.
func FindJob(ctx context.Context, c *Client, kindName, id string) (Job, bool, error) {
	st, err := c.JS.Stream(ctx, StreamMesh)
	if err != nil {
		return Job{}, false, err
	}
	var found Job
	ok := false
	err = scanSubject(ctx, c, st, InboxSubject(RoutinePath(kindName)), 1, func(_ uint64, m Message) (bool, error) {
		ok = m.Kind == "job" && m.Job != nil && m.Job.ID == id
		if ok {
			found = *m.Job
		}
		return !ok, nil
	})
	return found, ok, err
}
