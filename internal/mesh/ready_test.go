package mesh_test

import (
	"errors"
	"testing"

	. "github.com/rickdiculousli/buildermesh/internal/mesh"
)

// bm-ready is made by the first request; a second is refused, an update
// at a stale revision is refused, and forget deletes the request.
func TestReadyRequests(t *testing.T) {
	c, ctx := connect(t)
	if r, err := ListReady(ctx, c); err != nil || len(r) != 0 {
		t.Fatalf("empty %+v %v", r, err)
	}
	r := Ready{Path: "app-080", Spec: "080", Head: "abc", Text: "built it", Phase: PhaseReady, Seq: 7}
	if err := CreateReady(ctx, c, r); err != nil {
		t.Fatal(err)
	}
	if err := CreateReady(ctx, c, r); !errors.Is(err, ErrReadyExists) {
		t.Fatalf("second create %v", err)
	}
	got, rev, ok, err := GetReady(ctx, c, "app-080")
	if err != nil || !ok || got.Path != "app-080" || got.Seq != 7 || got.Phase != PhaseReady {
		t.Fatalf("get %+v %v %v", got, ok, err)
	}
	got.Phase = PhaseCancelled
	if _, err := UpdateReady(ctx, c, got, rev); err != nil {
		t.Fatal(err)
	}
	got.Phase = PhaseLanded
	if _, err := UpdateReady(ctx, c, got, rev); !errors.Is(err, ErrReadyMoved) {
		t.Fatalf("stale update %v", err)
	}
	if err := PutNode(ctx, c, Node{Path: "app-080", Role: "lead"}); err != nil {
		t.Fatal(err)
	}
	if err := ForgetNode(ctx, c, "app-080"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := GetReady(ctx, c, "app-080"); err != nil || ok {
		t.Fatalf("after forget %v %v", ok, err)
	}
	if _, err := UpdateReady(ctx, c, got, rev); !errors.Is(err, ErrReadyMoved) {
		t.Fatalf("update of a deleted request %v", err)
	}
}

// WriteStateAt writes only at the revision read: a node's own event
// written meanwhile is never overwritten.
func TestWriteStateAtRefusesAMovedState(t *testing.T) {
	c, ctx := connect(t)
	if _, err := WriteState(ctx, c, "app-080", State{State: "working"}); err != nil {
		t.Fatal(err)
	}
	rev, err := WriteState(ctx, c, "app-080", State{State: "landing", Text: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteState(ctx, c, "app-080", State{State: "working", Text: "the lead's own", By: "node"}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteStateAt(ctx, c, "app-080", rev, State{State: "landing", Text: "daemon", By: "daemon"}); !errors.Is(err, ErrStateMoved) {
		t.Fatalf("stale write %v", err)
	}
	cur, now, _ := ReadState(ctx, c, "app-080")
	if cur.Text != "the lead's own" {
		t.Fatalf("state %+v", cur)
	}
	if _, err := WriteStateAt(ctx, c, "app-080", now, State{State: "landing", Text: "daemon", By: "daemon"}); err != nil {
		t.Fatal(err)
	}
	// landing never goes to silent or asking.
	for _, to := range []string{"silent", "asking", "idle"} {
		if Legal("landing", to) {
			t.Fatalf("landing -> %s is legal", to)
		}
	}
	for _, to := range []string{"landing", "held", "done", "working", "blocked"} {
		if !Legal("landing", to) {
			t.Fatalf("landing -> %s is illegal", to)
		}
	}
	if !Legal("held", "landing") || !Legal("working", "landing") {
		t.Fatal("working or held can't reach landing")
	}
	if s, ok := StateFor("ready"); !ok || s != "landing" {
		t.Fatalf("ready sets %q", s)
	}
}
