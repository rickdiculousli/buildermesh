package mesh_test

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	. "github.com/rickdiculousli/buildermesh/internal/mesh"
)

// KindJobs: an acked job is gone; a replied one past the ack floor is gone;
// a held one names its holder; a delivered one with no reply and no hold
// (handed back, or on a dead daemon) and an undelivered one are waiting.
func TestKindJobsWaitingHeldAndAnswered(t *testing.T) {
	c, ctx := connect(t)
	repo := gitRepo(t)
	cons, err := KindConsumer(ctx, c, "review", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	from, _ := ParsePath("app-001")
	if err := EnsureInbox(ctx, c, from); err != nil {
		t.Fatal(err)
	}
	ids := []string{"j-acked", "j-held", "j-replied", "j-orphan", "j-new"}
	for _, id := range ids {
		if _, err := Queue(ctx, c, from, Job{ID: id, Kind: "review"}, repo); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Send(ctx, c, Message{Kind: "message", From: "app-001", To: "routine.review", Text: "go"}); err != nil {
		t.Fatal(err)
	}
	if got, err := KindJobs(ctx, c, "review"); err != nil || len(got) != 5 || got[0].State != "waiting" || got[0].ID != "j-acked" {
		t.Fatalf("before delivery %+v %v", got, err)
	}
	batch, err := cons.Fetch(4, jetstream.FetchMaxWait(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var msgs []jetstream.Msg
	for m := range batch.Messages() {
		msgs = append(msgs, m)
	}
	if len(msgs) != 4 {
		t.Fatalf("fetched %d", len(msgs))
	}
	seq := func(m jetstream.Msg) uint64 {
		meta, err := m.Metadata()
		if err != nil {
			t.Fatal(err)
		}
		return meta.Sequence.Stream
	}
	if err := msgs[0].DoubleAck(ctx); err != nil {
		t.Fatal(err)
	}
	if err := msgs[2].DoubleAck(ctx); err != nil {
		t.Fatal(err)
	}
	for range 1100 { // a busy requester: the reply is found past them all
		if _, err := Send(ctx, c, Message{From: "x", To: "app-001", Text: "chatter"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := SendReply(ctx, c, RoutinePath("review"), from, Reply{Job: "j-replied", Kind: "review", OK: true}); err != nil {
		t.Fatal(err)
	}
	// The ack floor stays below j-replied while j-held waits unacked. A hold
	// of an answered job is not believed; of two live entries the newest
	// wins; an entry older than AckWait (1m) is a dead daemon's.
	at := func(d time.Duration) string { return time.Now().Add(-d).UTC().Format(time.RFC3339) }
	for _, h := range []Holds{
		{Kind: "review", Host: "dead", At: at(2 * time.Minute), Jobs: []Hold{{Seq: seq(msgs[3]), Job: "j-orphan", Holder: "routine.review.x-9", State: "running"}}},
		{Kind: "review", Host: "old", At: at(30 * time.Second), Jobs: []Hold{{Seq: seq(msgs[1]), Job: "j-held", Holder: "routine.review.x-1", State: "held"}}},
		{Kind: "review", Host: "k16", At: at(0), Jobs: []Hold{
			{Seq: seq(msgs[1]), Job: "j-held", From: "app-001", Holder: "routine.review.opus-5-5-1", State: "running"},
			{Seq: seq(msgs[2]), Job: "j-replied", From: "app-001", Holder: "routine.review.opus-5-5-1", State: "held"},
		}},
	} {
		if err := PutHolds(ctx, c, h); err != nil {
			t.Fatal(err)
		}
	}

	got, err := KindJobs(ctx, c, "review")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(string(out))
	for i := range got {
		got[i].Seq = 0
	}
	want := []QueuedJob{
		{ID: "j-held", From: "app-001", Commit: head, State: "running", Holder: "routine.review.opus-5-5-1", Host: "k16"},
		{ID: "j-orphan", From: "app-001", Commit: head, State: "waiting"},
		{ID: "j-new", From: "app-001", Commit: head, State: "waiting"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("jobs\n got %+v\nwant %+v", got, want)
	}

	// Clearing a host's holds leaves the others.
	if err := ClearHolds(ctx, c, "k16", at(0)); err != nil {
		t.Fatal(err)
	}
	all, err := ListHolds(ctx, c, "review")
	if err != nil || len(all) != 3 {
		t.Fatalf("holds %+v %v", all, err)
	}
	for _, h := range all {
		if (h.Host == "k16") != (len(h.Jobs) == 0) {
			t.Fatalf("after clear %+v", all)
		}
	}
	if got, err := KindJobs(ctx, c, "review"); err != nil || got[0].Holder != "routine.review.x-1" {
		t.Fatalf("after clear %+v %v", got, err)
	}
	if got, err := KindJobs(ctx, c, "absent"); err != nil || got != nil {
		t.Fatalf("no consumer %+v %v", got, err)
	}
}

// Every daemon's entry is listed while the daemons rewrite theirs, as each
// does on every in-progress beat: a list read through a KV watcher missed a
// key rewritten mid-read, so a running job read as waiting.
func TestListHoldsWhileEntriesAreRewritten(t *testing.T) {
	c, ctx := connect(t)
	hosts := []string{"box-a", "box-b", "box-c"}
	for _, h := range hosts {
		if err := PutHolds(ctx, c, Holds{Kind: "slow", Host: h, At: "1"}); err != nil {
			t.Fatal(err)
		}
	}
	wctx, stop := context.WithCancel(ctx)
	var wg sync.WaitGroup
	for _, h := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for wctx.Err() == nil {
				PutHolds(wctx, c, Holds{Kind: "slow", Host: h, At: "1"})
			}
		}()
	}
	defer func() { stop(); wg.Wait() }()
	for i := range 1000 {
		all, err := ListHolds(ctx, c, "slow")
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != len(hosts) {
			t.Fatalf("list %d: %d entries, want %d: %+v", i, len(all), len(hosts), all)
		}
	}
}
