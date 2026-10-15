package mesh_test

import (
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
