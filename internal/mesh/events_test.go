package mesh_test

import (
	"context"
	"testing"
	"time"

	. "github.com/rickdiculousli/buildermesh/internal/mesh"
)

func TestPublishAndHistory(t *testing.T) {
	c, ctx := connect(t)
	for _, ev := range []Event{
		{From: "a.x", Event: "started", Text: "1"},
		{From: "b", Event: "started", Text: "2"},
		{From: "a.y", Event: "done", Text: "3"},
	} {
		if err := PublishEvent(ctx, c, ev); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1100 * time.Millisecond)
	}
	got, err := History(ctx, c, "bm.a.>", time.Time{})
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	if got[0].Subject != "bm.a.x.started" || got[1].Subject != "bm.a.y.done" || got[0].Seq >= got[1].Seq {
		t.Fatal(got)
	}
	since := got[0].At.Add(500 * time.Millisecond)
	got, err = History(ctx, c, "bm.a.>", since)
	if err != nil || len(got) != 1 || got[0].Subject != "bm.a.y.done" {
		t.Fatal(got, err)
	}
	if got, err := History(ctx, c, "bm.nobody.>", time.Time{}); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if got, err := History(ctx, c, "bm.a.>", time.Now().Add(time.Hour)); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if got, err := History(ctx, c, "bm.b.>", got0At(t, c, ctx).Add(-time.Millisecond)); err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	if err := PublishEvent(ctx, c, Event{From: "a", Event: "bad.token"}); err == nil {
		t.Fatal("dotted event accepted")
	}
	if err := PublishEvent(ctx, c, Event{From: "daemon.k16", Event: "wake", To: "a"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := History(ctx, c, "bm.daemon.k16.wake", time.Time{}); len(got) != 1 {
		t.Fatal(got)
	}
}

func TestFollowDeliversLive(t *testing.T) {
	c, ctx := connect(t)
	ch, err := Follow(ctx, c, "bm.>")
	if err != nil {
		t.Fatal(err)
	}
	if err := PublishEvent(ctx, c, Event{From: "a", Event: "progress", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-ch:
		if s.Subject != "bm.a.progress" {
			t.Fatal(s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
	}
}

// got0At returns the time of the first stored message.
func got0At(t *testing.T, c *Client, ctx context.Context) time.Time {
	t.Helper()
	all, err := History(ctx, c, "bm.>", time.Time{})
	if err != nil || len(all) == 0 {
		t.Fatal(all, err)
	}
	return all[0].At
}

func TestPublishEventIDDedupes(t *testing.T) {
	c, ctx := connect(t)
	ev := Event{From: "daemon.k16", Event: "push", To: "a", Text: "a asks", Seq: 7}
	if dup, err := PublishEventID(ctx, c, ev, "push-7"); err != nil || dup {
		t.Fatal(dup, err)
	}
	if dup, err := PublishEventID(ctx, c, Event{From: "daemon.k17", Event: "push", To: "a", Seq: 7}, "push-7"); err != nil || !dup {
		t.Fatal("second claim:", dup, err)
	}
	got, err := History(ctx, c, "bm.daemon.*.push", time.Time{})
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	if _, err := PublishEventID(ctx, c, ev, ""); err == nil {
		t.Fatal("empty msg id accepted")
	}
}

func TestLastTakesWildcards(t *testing.T) {
	c, ctx := connect(t)
	if _, ok, err := Last(ctx, c, "bm.daemon.*.started"); err != nil || ok {
		t.Fatal(ok, err)
	}
	for _, host := range []string{"k16", "mac", "k16"} {
		if err := PublishEvent(ctx, c, Event{From: "daemon." + host, Event: "started", Text: "rev-" + host}); err != nil {
			t.Fatal(err)
		}
	}
	if err := PublishEvent(ctx, c, Event{From: "daemon.mac", Event: "digest"}); err != nil {
		t.Fatal(err)
	}
	s, ok, err := Last(ctx, c, "bm.daemon.*.started")
	if err != nil || !ok || s.Subject != "bm.daemon.k16.started" || s.Seq != 3 {
		t.Fatal(s, ok, err)
	}
	if s, ok, err := Last(ctx, c, "bm.daemon.mac.started"); err != nil || !ok || s.Seq != 2 {
		t.Fatal(s, ok, err)
	}
}
