package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// A daemon's runaway event prints in watch for the node it watches, and
// mesh-status lists each live node's open flags: the newest of each rule, a
// runaway hiding the same rule's overworking, nothing for a node done.
func TestWatchAndMeshStatusPrintFlags(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-096", Role: "lead"})
	putNode(t, c, ctx, mesh.Node{Path: "app-097", Role: "lead"})
	if _, err := mesh.WriteState(ctx, c, "app-096", mesh.State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	meshtest.ForceState(t, c, "app-097", mesh.State{State: "done", By: "node"})
	at := time.Now().UTC().Format(time.RFC3339)
	for _, ev := range []mesh.Event{
		{To: "app-096", Text: "overworking app-096: read 30.0M, 3.0× the app M median (10.0M)"},
		{To: "app-096", Text: "overworking app-096: land refused 3× (3 rebase conflicts)"},
		{To: "app-096", Text: "runaway app-096: read 60.0M, 6.0× the app M median (10.0M)"},
		{To: "app-097", Text: "runaway app-097: read 154M in the last hour"},
	} {
		ev.From, ev.Event, ev.At = "daemon.k16", "runaway", at
		if err := mesh.PublishEvent(ctx, c, ev); err != nil {
			t.Fatal(err)
		}
	}
	out, errs, code := runCLI(t, nil, "mesh-status")
	want := "app-096 ▲ runaway 0m ago: read 60.0M, 6.0× the app M median (10.0M)\n" +
		"app-096 ▲ overworking 0m ago: land refused 3× (3 rebase conflicts)\n"
	if code != 0 || !strings.Contains(out, "\n"+want) || strings.Contains(out, "app-097 ▲") || strings.Contains(out, "3.0×") {
		t.Fatalf("mesh-status: %d %q %q", code, out, errs)
	}

	var b bytes.Buffer
	a := &app{stdout: &b}
	a.printEvent(mesh.Stored{Subject: "bm.daemon.k16.runaway",
		Data: []byte(`{"at":"` + at + `","to":"app-096","text":"runaway app-096: read 154M in the last hour"}`)}, map[string]bool{"app-096": true})
	if got := b.String(); got != at+" daemon.k16 runaway app-096 runaway app-096: read 154M in the last hour\n" {
		t.Fatalf("watch printed %q", got)
	}
}
