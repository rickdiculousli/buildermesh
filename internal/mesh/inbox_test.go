package mesh_test

import (
	"strings"
	"testing"

	. "github.com/rickdiculousli/buildermesh/internal/mesh"
)

func TestSendSameIDIsOneMessage(t *testing.T) {
	c, ctx := connect(t)
	m := Message{ID: "m1", From: "a", To: "b", Text: "hi"}
	if dup, err := Send(ctx, c, m); err != nil || dup {
		t.Fatal(dup, err)
	}
	if dup, err := Send(ctx, c, m); err != nil || !dup {
		t.Fatal(dup, err)
	}
	p, _ := ParsePath("b")
	got, err := Fetch(ctx, c, p, true)
	if err != nil || len(got) != 1 || got[0].ID != "m1" || got[0].Kind != "message" || got[0].At == "" {
		t.Fatal(got, err)
	}
}

func TestFetchAckRemovesUnread(t *testing.T) {
	c, ctx := connect(t)
	for _, text := range []string{"one", "two"} {
		if _, err := Send(ctx, c, Message{From: "a", To: "b", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := ParsePath("b")
	if n, err := Unread(ctx, c, p); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	got, err := Fetch(ctx, c, p, false)
	if err != nil || len(got) != 2 || got[0].Text != "one" || got[1].Text != "two" {
		t.Fatal(got, err)
	}
	if n, _ := Unread(ctx, c, p); n != 2 {
		t.Fatal(n)
	}
	if _, err := Fetch(ctx, c, p, true); err != nil {
		t.Fatal(err)
	}
	if n, _ := Unread(ctx, c, p); n != 0 {
		t.Fatal(n)
	}
	if _, err := Send(ctx, c, Message{From: "a", To: "bad path"}); err == nil {
		t.Fatal("bad to accepted")
	}
}

// Pending lists what is above the ack floor, oldest first, and takes nothing:
// Unread is the same before and after. A message acked out of order stays
// listed while an older one is unread.
func TestPendingReadsAboveAckFloorWithoutFetching(t *testing.T) {
	c, ctx := connect(t)
	p, _ := ParsePath("b")
	if got, err := Pending(ctx, c, p); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	send := func(text string) {
		t.Helper()
		if _, err := Send(ctx, c, Message{From: "a", To: "b", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	send("old")
	if _, err := Fetch(ctx, c, p, true); err != nil {
		t.Fatal(err)
	}
	send("one")
	send("two")
	send("three")
	texts := func() string {
		t.Helper()
		got, err := Pending(ctx, c, p)
		if err != nil {
			t.Fatal(err)
		}
		var s []string
		for _, m := range got {
			s = append(s, m.Text)
		}
		return strings.Join(s, ",")
	}
	if got := texts(); got != "one,two,three" {
		t.Fatal(got)
	}
	if n, err := Unread(ctx, c, p); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	if _, ok, err := FetchMatch(ctx, c, p, func(m Message) bool { return m.Text == "two" }); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if got := texts(); got != "one,two,three" {
		t.Fatal(got)
	}
	if n, err := Unread(ctx, c, p); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if _, err := Fetch(ctx, c, p, true); err != nil {
		t.Fatal(err)
	}
	if got := texts(); got != "" {
		t.Fatal(got)
	}
}
