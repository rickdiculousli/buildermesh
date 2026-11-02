package mesh_test

import (
	"testing"
	"time"

	. "github.com/rickdiculousli/buildermesh/internal/mesh"
)

// A since-read whose filter has nothing at or after since returns at once,
// even when other subjects were published later.
func TestHistorySinceReturnsAtOnce(t *testing.T) {
	c, ctx := connect(t)
	for _, ev := range []Event{
		{From: "a", Event: "started"},
		{From: "a", Event: "progress"},
	} {
		if err := PublishEvent(ctx, c, ev); err != nil {
			t.Fatal(err)
		}
	}
	got, err := History(ctx, c, "bm.a.>", time.Time{})
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := PublishEvent(ctx, c, Event{From: "b", Event: "started"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		since time.Time
		want  int
	}{
		{got[0].At, 2},
		{got[1].At, 1},
		{got[1].At.Add(time.Millisecond), 0},
	} {
		start := time.Now()
		h, err := History(ctx, c, "bm.a.>", tc.since)
		if err != nil || len(h) != tc.want {
			t.Fatal(tc.since, h, err)
		}
		if d := time.Since(start); d > 500*time.Millisecond {
			t.Fatalf("since %v: History took %v for %d messages", tc.since, d, tc.want)
		}
	}
}
