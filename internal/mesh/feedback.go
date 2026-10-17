package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// familyRE is what a build family matches: one subject token.
var familyRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// CheckFamily refuses a family that is not one subject token.
func CheckFamily(f string) error {
	if !familyRE.MatchString(f) {
		return fmt.Errorf("invalid family %q: want letters, digits, - or _, starting with a letter or digit", f)
	}
	return nil
}

// FeedbackSubject is bm.feedback.<family>, a build family's queue.
func FeedbackSubject(family string) string { return "bm.feedback." + family }

// Feedback is one lesson a lead leaves for its family's coordinator.
type Feedback struct {
	From   string `json:"from"`
	Family string `json:"family"`
	Repo   string `json:"repo"`
	Spec   string `json:"spec"`
	Commit string `json:"commit"`
	Where  string `json:"where"`
	Fixed  string `json:"fixed"`
	Text   string `json:"text"`
	At     string `json:"at"`
	Seq    uint64 `json:"seq,omitempty"` // as read back: its stream sequence
}

// PublishFeedback publishes f on its family's subject, filling at when
// empty, and returns its stream sequence.
func PublishFeedback(ctx context.Context, c *Client, f Feedback) (uint64, error) {
	if err := CheckFamily(f.Family); err != nil {
		return 0, err
	}
	if f.At == "" {
		f.At = time.Now().UTC().Format(time.RFC3339)
	}
	f.Seq = 0
	data, err := json.Marshal(f)
	if err != nil {
		return 0, err
	}
	subject := FeedbackSubject(f.Family)
	ack, err := c.JS.Publish(ctx, subject, data, jetstream.WithExpectStream(StreamMesh))
	if err != nil {
		return 0, fmt.Errorf("publish %s: %w", subject, err)
	}
	return ack.Sequence, nil
}

// ListFeedback returns the family's pending feedback, oldest first, each
// with its stream sequence.
func ListFeedback(ctx context.Context, c *Client, family string) ([]Feedback, error) {
	if err := CheckFamily(family); err != nil {
		return nil, err
	}
	hist, err := History(ctx, c, FeedbackSubject(family), time.Time{})
	if err != nil {
		return nil, err
	}
	out := make([]Feedback, 0, len(hist))
	for _, m := range hist {
		var f Feedback
		if err := json.Unmarshal(m.Data, &f); err != nil {
			f = Feedback{Family: family, Text: string(m.Data)}
		}
		f.Seq = m.Seq
		out = append(out, f)
	}
	return out, nil
}

// ListAllFeedback returns every family's pending feedback, in stream
// order, each with its stream sequence.
func ListAllFeedback(ctx context.Context, c *Client) ([]Feedback, error) {
	hist, err := History(ctx, c, FeedbackSubject(">"), time.Time{})
	if err != nil {
		return nil, err
	}
	out := make([]Feedback, 0, len(hist))
	for _, m := range hist {
		family := strings.TrimPrefix(m.Subject, "bm.feedback.")
		var f Feedback
		if err := json.Unmarshal(m.Data, &f); err != nil {
			f = Feedback{Text: string(m.Data)}
		}
		f.Family, f.Seq = family, m.Seq
		out = append(out, f)
	}
	return out, nil
}

// DeleteFeedback deletes the feedback messages at seqs from the stream and
// returns the ones it deleted and the ones not there (drained already). A
// sequence holding anything but feedback is refused before any is deleted.
func DeleteFeedback(ctx context.Context, c *Client, seqs []uint64) (deleted, missing []uint64, err error) {
	st, err := c.JS.Stream(ctx, StreamMesh)
	if err != nil {
		return nil, nil, err
	}
	var found []uint64
	for _, seq := range seqs {
		m, err := st.GetMsg(ctx, seq)
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			missing = append(missing, seq)
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read %d: %w", seq, err)
		}
		if !strings.HasPrefix(m.Subject, "bm.feedback.") {
			return nil, nil, fmt.Errorf("%d is %s, not feedback", seq, m.Subject)
		}
		found = append(found, seq)
	}
	for _, seq := range found {
		if err := st.DeleteMsg(ctx, seq); err != nil {
			if errors.Is(err, jetstream.ErrMsgNotFound) {
				missing = append(missing, seq)
				continue
			}
			return deleted, missing, fmt.Errorf("delete %d: %w", seq, err)
		}
		deleted = append(deleted, seq)
	}
	return deleted, missing, nil
}

// DrainFeedback deletes the family's messages from the stream up to and
// including through (0: up to the last one there now) and returns how many
// it deleted. Only the family's subject is purged; what was published after
// through stays.
func DrainFeedback(ctx context.Context, c *Client, family string, through uint64) (int, error) {
	pending, err := ListFeedback(ctx, c, family)
	if err != nil {
		return 0, err
	}
	if through == 0 {
		if len(pending) == 0 {
			return 0, nil
		}
		through = pending[len(pending)-1].Seq
	}
	n := 0
	for _, f := range pending {
		if f.Seq <= through {
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	st, err := c.JS.Stream(ctx, StreamMesh)
	if err != nil {
		return 0, err
	}
	// The purge's sequence is exclusive.
	if err := st.Purge(ctx, jetstream.WithPurgeSubject(FeedbackSubject(family)), jetstream.WithPurgeSequence(through+1)); err != nil {
		return 0, fmt.Errorf("purge %s: %w", FeedbackSubject(family), err)
	}
	return n, nil
}

// FamilyPending is how many feedback messages a family holds.
type FamilyPending struct {
	Family  string
	Pending uint64
}

// PendingFeedback lists every family with feedback in the stream, by name.
func PendingFeedback(ctx context.Context, c *Client) ([]FamilyPending, error) {
	st, err := c.JS.Stream(ctx, StreamMesh)
	if err != nil {
		return nil, err
	}
	info, err := st.Info(ctx, jetstream.WithSubjectFilter(FeedbackSubject(">")))
	if err != nil {
		return nil, err
	}
	var out []FamilyPending
	for subject, n := range info.State.Subjects {
		if f, ok := strings.CutPrefix(subject, "bm.feedback."); ok && n > 0 {
			out = append(out, FamilyPending{Family: f, Pending: n})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Family < out[j].Family })
	return out, nil
}
