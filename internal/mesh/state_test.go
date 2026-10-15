package mesh_test

import (
	"errors"
	. "github.com/rickdiculousli/buildermesh/internal/mesh"
	"sync"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
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
	var wg sync.WaitGroup
	errs := make([]error, 2)
	texts := []string{"one", "two"}
	for i := range texts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = WriteState(ctx, c, "p", State{State: "working", Text: texts[i]})
		}()
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	s, _, _ := ReadState(ctx, c, "p")
	if s.Text != "one" && s.Text != "two" {
		t.Fatal(s)
	}

	rev, err := WriteState(ctx, c, "q", State{State: "working"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteState(ctx, c, "q", State{State: "blocked"}); err != nil {
		t.Fatal(err)
	}
	kv, _ := c.JS.KeyValue(ctx, BucketState)
	if _, err := kv.Update(ctx, "q", []byte(`{"state":"done"}`), rev); err == nil || !errors.Is(err, jetstream.ErrKeyExists) {
		t.Fatalf("stale update: %v", err)
	}
	if s, _, _ := ReadState(ctx, c, "q"); s.State != "blocked" {
		t.Fatal(s)
	}
}
