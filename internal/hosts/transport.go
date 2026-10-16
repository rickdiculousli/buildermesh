package hosts

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
)

// The stream jobs and replies travel in (mesh.StreamMesh: its subjects are
// bm.>), named here so this package needs no mesh import.
const stream = "bm-mesh"

// JobAckWait is how long a box holds a job before the hub redelivers it;
// a running job reports progress every minute.
const JobAckWait = 10 * time.Minute

// JobSubject is where jobs for host go: bm.inbox.host.<host>.
func JobSubject(host string) string { return "bm.inbox.host." + host }

// ReplySubject is where a reply to caller's job goes:
// bm.hostreply.<caller>.<job id>.
func ReplySubject(caller, jobID string) string { return "bm.hostreply." + caller + "." + jobID }

// HostConsumer is host's durable job consumer, inbox-host--<host>.
func HostConsumer(host string) string { return "inbox-host--" + host }

// HostInbox creates or updates host's job consumer: one job out at a time,
// redelivered after JobAckWait unacked, resuming where it stopped.
func HostInbox(ctx context.Context, js jetstream.JetStream, host string) (jetstream.Consumer, error) {
	if !ValidHostName(host) {
		return nil, fmt.Errorf("host %q: not a valid host name", host)
	}
	cons, err := js.CreateOrUpdateConsumer(ctx, stream, jetstream.ConsumerConfig{
		Durable:       HostConsumer(host),
		FilterSubject: JobSubject(host),
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       JobAckWait,
		MaxAckPending: 1,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("job consumer for %s: %w", host, err)
	}
	return cons, nil
}

// NewSalt is 16 random bytes as hex.
func NewSalt() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// NewJob is a signed job from by to to, living ttl (at most MaxLifetime).
func NewJob(key ed25519.PrivateKey, by, to, op, repo string, args []string, rosterVersion int64,
	now time.Time, ttl time.Duration) Job {
	if args == nil {
		args = []string{}
	}
	j := Job{ID: nuid.Next(), By: by, To: to, Op: op, Repo: repo, Args: args, RosterVersion: rosterVersion,
		TS: Stamp(now), Expires: Stamp(now.Add(min(ttl, MaxLifetime))), Salt: NewSalt()}
	j.Sign(key)
	return j
}

// SendJob puts j on its box's job subject.
func SendJob(ctx context.Context, js jetstream.JetStream, j Job) error {
	if !ValidHostName(j.To) {
		return fmt.Errorf("host %q: not a valid host name", j.To)
	}
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	if _, err := js.Publish(ctx, JobSubject(j.To), b, jetstream.WithMsgID("hostjob-"+j.ID),
		jetstream.WithExpectStream(stream)); err != nil {
		return fmt.Errorf("send %s to %s: %w", j.Op, j.To, err)
	}
	return nil
}

// NewReply is self's signed answer to j.
func NewReply(key ed25519.PrivateKey, j *Job, self string, ok bool, text string, now time.Time) Reply {
	r := Reply{JobID: j.ID, JobSalt: j.Salt, By: self, To: j.By, OK: ok, Text: text, TS: Stamp(now), Salt: NewSalt()}
	r.Sign(key)
	return r
}

// SendReply publishes r on its caller's reply subject. A reply to a
// sender whose name isn't a valid host name has nowhere to go and is
// dropped.
func SendReply(ctx context.Context, js jetstream.JetStream, r Reply) error {
	if !ValidHostName(r.To) || !tokenOK(r.JobID) {
		return nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, err := js.Publish(ctx, ReplySubject(r.To, r.JobID), b, jetstream.WithExpectStream(stream)); err != nil {
		return fmt.Errorf("reply to %s: %w", r.To, err)
	}
	return nil
}

// tokenOK: s can stand as one subject token.
func tokenOK(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// CheckReply checks a reply the way a box checks a job: signed by the
// job's to (by its roster key), from the job's to to its by, naming the
// job's id and salt.
func CheckReply(r *Reply, j *Job, roster *Roster) error {
	h, ok := roster.Host(j.To)
	if !ok {
		return fmt.Errorf("reply to %s: %s is not in the roster", j.ID, j.To)
	}
	if r.By != j.To || r.To != j.By || r.JobID != j.ID || r.JobSalt != j.Salt {
		return fmt.Errorf("reply to %s: names another job or box", j.ID)
	}
	if err := r.Verify(h.Key); err != nil {
		return fmt.Errorf("reply to %s: %w", j.ID, err)
	}
	return nil
}

// errNoJob is SentJobs' answer for an id it didn't find.
var errNoJob = errors.New("no such job")

// SentJobs reads the jobs by sent, from every box's job subject in the
// stream (kept 30 days), by id.
func SentJobs(ctx context.Context, js jetstream.JetStream, by string) (map[string]Job, error) {
	out := map[string]Job{}
	err := scan(ctx, js, JobSubject("*"), func(_ string, data []byte) {
		var j Job
		if json.Unmarshal(data, &j) == nil && j.By == by {
			out[j.ID] = j
		}
	})
	return out, err
}

// Replies reads every reply addressed to caller, oldest first, with the
// subject's job id; none is checked.
func Replies(ctx context.Context, js jetstream.JetStream, caller string) ([]Reply, error) {
	var out []Reply
	err := scan(ctx, js, ReplySubject(caller, "*"), func(_ string, data []byte) {
		var r Reply
		if json.Unmarshal(data, &r) == nil {
			out = append(out, r)
		}
	})
	return out, err
}

// scan replays what the stream holds on filter, oldest first.
func scan(ctx context.Context, js jetstream.JetStream, filter string, f func(subject string, data []byte)) error {
	st, err := js.Stream(ctx, stream)
	if err != nil {
		return err
	}
	info, err := st.Info(ctx, jetstream.WithSubjectFilter(filter))
	if err != nil {
		return err
	}
	if len(info.State.Subjects) == 0 {
		return nil
	}
	cons, err := js.OrderedConsumer(ctx, stream, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{filter}, DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return err
	}
	for {
		m, err := cons.Next(jetstream.FetchMaxWait(2 * time.Second))
		if err != nil {
			return err
		}
		f(m.Subject(), m.Data())
		meta, err := m.Metadata()
		if err != nil {
			return err
		}
		if meta.NumPending == 0 {
			return nil
		}
	}
}

// WaitReplies follows caller's reply subject for the jobs in want until
// each has a reply that checks or ctx ends, calling got for each such
// reply as it arrives (replies that fail the check are dropped).
func WaitReplies(ctx context.Context, js jetstream.JetStream, caller string, want map[string]Job, roster *Roster,
	got func(Job, Reply)) error {
	cons, err := js.OrderedConsumer(ctx, stream, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{ReplySubject(caller, "*")}, DeliverPolicy: jetstream.DeliverNewPolicy,
	})
	if err != nil {
		return err
	}
	it, err := cons.Messages()
	if err != nil {
		return err
	}
	defer it.Stop()
	stop := context.AfterFunc(ctx, it.Stop)
	defer stop()
	left := len(want)
	for left > 0 {
		m, err := it.Next()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		var r Reply
		if json.Unmarshal(m.Data(), &r) != nil {
			continue
		}
		j, ok := want[r.JobID]
		if !ok || CheckReply(&r, &j, roster) != nil {
			continue
		}
		delete(want, r.JobID)
		left--
		got(j, r)
	}
	return nil
}
