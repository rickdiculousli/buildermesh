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
		"asking":  {"working", "blocked", "asking", "done"},
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

// The table answers as the maps it replaced did, key for key.
func TestStateTableMatchesTheOldMaps(t *testing.T) {
	transitions := map[string][]string{
		"idle":    {"working"},
		"working": {"working", "blocked", "asking", "done", "silent", "held", "landing"},
		"blocked": {"working", "done"},
		"asking":  {"working", "blocked", "asking", "done", "held"},
		"silent":  {"working", "blocked", "asking", "done", "held"},
		"held":    {"working", "blocked", "asking", "done", "held", "landing"},
		"landing": {"landing", "held", "done", "working", "blocked"},
	}
	nodeState := map[string]string{"started": "working", "progress": "working", "ask": "asking", "blocked": "blocked", "held": "held", "ready": "landing", "done": "done"}
	daemonState := map[string]string{"silent": "silent", "landing": "landing", "land-refused": "working", "land-withdrawn": "working"}
	tickKind := map[string]string{"progress": "progress", "job": "job", "land-refused": "refused", "ready": "ready", "done": "done"}
	needRank := map[string]int{"blocked": 0, "asking": 1, "silent": 2, "working": 3, "landing": 4, "held": 5, "idle": 6, "done": 7}
	live := map[string]bool{"working": true, "asking": true, "blocked": true, "silent": true, "landing": true, "held": true}
	needs := map[string]bool{"blocked": true, "asking": true, "silent": true}

	for from := range States {
		for to := range States {
			want := false
			for _, x := range transitions[from] {
				want = want || x == to
			}
			if Legal(from, to) != want {
				t.Errorf("Legal(%s,%s) = %v", from, to, !want)
			}
		}
		if Live(from) != live[from] || Needs(from) != needs[from] {
			t.Errorf("%s: Live %v Needs %v", from, Live(from), Needs(from))
		}
		if r, ok := needRank[from]; !ok || NeedRank(from) != r {
			t.Errorf("NeedRank(%s) = %d", from, NeedRank(from))
		}
		if States[from].Next == nil || States[from].Events == nil || States[from].Daemon == nil {
			t.Errorf("%s has a nil list", from)
		}
	}
	if NeedRank("nope") != needRank["idle"] || Live("nope") || Needs("nope") || Legal("nope", "working") {
		t.Error("an unknown state is not idle-like")
	}
	events := []string{"started", "progress", "ask", "blocked", "held", "ready", "done", "silent", "landing", "land-refused", "land-withdrawn", "job", "wake"}
	for _, ev := range events {
		ws, wok := nodeState[ev]
		if gs, gok := StateFor(ev); gs != ws || gok != wok {
			t.Errorf("StateFor(%s) = %q %v", ev, gs, gok)
		}
		ds, dok := daemonState[ev]
		if gs, gok := DaemonStateFor(ev); gs != ds || gok != dok {
			t.Errorf("DaemonStateFor(%s) = %q %v", ev, gs, gok)
		}
		wk, kok := tickKind[ev]
		if gk, gok := TickFor(ev); gk != wk || gok != kok {
			t.Errorf("TickFor(%s) = %q %v", ev, gk, gok)
		}
	}
	if len(Ticks) != len(tickKind) {
		t.Error("Ticks has extra keys")
	}
}

// TestStateTableEventsSetOneState holds the table to one row per event: an
// event in two rows' Events (or two rows' Daemon) would make StateFor
// (DaemonStateFor) answer by map order.
func TestStateTableEventsSetOneState(t *testing.T) {
	for _, list := range []func(StateRow) []string{func(r StateRow) []string { return r.Events }, func(r StateRow) []string { return r.Daemon }} {
		seen := map[string]string{}
		for s, r := range States {
			for _, e := range list(r) {
				if was, ok := seen[e]; ok {
					t.Fatalf("event %s sets both %s and %s", e, was, s)
				}
				seen[e] = s
			}
		}
	}
}
