package mesh_test

import (
	"context"
	"errors"
	. "github.com/rickdiculousli/buildermesh/internal/mesh"
	"testing"
)

func TestStateTransitions(t *testing.T) {
	ok := map[string][]string{
		"idle":    {"working"},
		"working": {"working", "blocked", "asking", "done", "silent"},
		"blocked": {"working", "done"},
		"asking":  {"working", "blocked", "done"},
		"silent":  {"working", "blocked", "asking", "done"},
		"done":    {},
	}
	all := []string{"idle", "working", "blocked", "asking", "done", "silent"}
	for _, from := range all {
		for _, to := range all {
			want := false
			for _, x := range ok[from] {
				want = want || x == to
			}
			if Legal(from, to) != want {
				t.Errorf("Legal(%s,%s) = %v", from, to, !want)
			}
		}
	}
	for ev, want := range map[string]string{"started": "working", "progress": "working", "blocked": "blocked", "ask": "asking", "done": "done"} {
		if got, ok := StateFor(ev); !ok || got != want {
			t.Errorf("StateFor(%s) = %s %v", ev, got, ok)
		}
	}
	if _, ok := StateFor("wake"); ok {
		t.Error("wake maps to a state")
	}

	c, ctx := connect(t)
	var ill ErrIllegalTransition
	if _, err := WriteState(ctx, c, "x", State{State: "done"}); !errors.As(err, &ill) || ill.From != "idle" || ill.To != "done" {
		t.Fatal(err)
	}
	if _, err := WriteState(ctx, c, "x", State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteState(ctx, c, "x", State{State: "done"}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteState(ctx, c, "x", State{State: "working"}); !errors.As(err, &ill) || ill.From != "done" {
		t.Fatal(err)
	}
	s, rev, err := ReadState(ctx, c, "x")
	if err != nil || s.State != "done" || s.At == "" || rev == 0 {
		t.Fatal(s, rev, err)
	}
	if s, rev, err := ReadState(ctx, c, "missing"); err != nil || s.State != "idle" || rev != 0 {
		t.Fatal(s, rev, err)
	}
	all2, err := ReadAllStates(ctx, c)
	if err != nil || len(all2) != 1 || all2["x"].State != "done" {
		t.Fatal(all2, err)
	}
}

func TestWriteStateCAS(t *testing.T) {
	c, ctx := connect(t)
	kv, _ := c.JS.KeyValue(ctx, BucketState)

	// behind swaps the stored value after WriteState's first read, so its
	// first write uses a stale revision and must conflict.
	behind := func(path, state, text string) func(int) {
		return func(attempt int) {
			if attempt != 0 {
				return
			}
			_, rev, _ := ReadState(ctx, c, path)
			if _, err := kv.Update(ctx, path, []byte(`{"state":"`+state+`","text":"`+text+`"}`), rev); err != nil {
				t.Error(err)
			}
		}
	}

	// Conflict, then the re-read shows done: working is illegal, nothing written.
	if _, err := WriteState(ctx, c, "p", State{State: "working"}); err != nil {
		t.Fatal(err)
	}
	_, err := WriteStateHook(ctx, c, "p", State{State: "working", Text: "mine"}, behind("p", "done", "theirs"))
	var ill ErrIllegalTransition
	if !errors.As(err, &ill) || ill.From != "done" || ill.To != "working" {
		t.Fatalf("want done->working illegal, got %v", err)
	}
	s, rev, _ := ReadState(ctx, c, "p")
	if s.State != "done" || s.Text != "theirs" {
		t.Fatal(s)
	}
	e, _ := kv.Get(ctx, "p")
	if e.Revision() != rev {
		t.Fatal("revision moved")
	}
	if h, _ := kv.History(ctx, "p"); len(h) != 2 {
		t.Fatalf("want 2 revisions (ours + theirs), got %d", len(h))
	}

	// Conflict, then a legal move succeeds exactly once.
	if _, err := WriteState(ctx, c, "q", State{State: "working"}); err != nil {
		t.Fatal(err)
	}
	newRev, err := WriteStateHook(ctx, c, "q", State{State: "blocked", Text: "mine"}, behind("q", "working", "x"))
	if err != nil {
		t.Fatal(err)
	}
	s, rev, _ = ReadState(ctx, c, "q")
	if s.State != "blocked" || s.Text != "mine" || rev != newRev {
		t.Fatal(s, rev, newRev)
	}
	if h, _ := kv.History(ctx, "q"); len(h) != 3 {
		t.Fatalf("want 3 revisions, got %d", len(h))
	}

	// A non-conflict error is returned at once, not retried.
	attempts := 0
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := WriteStateHook(cctx, c, "r", State{State: "working"}, func(int) { attempts++ }); err == nil || attempts > 1 {
		t.Fatal(err, attempts)
	}
}
