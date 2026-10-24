package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

func TestMeshStatusEndsWithOpenOperatorSteps(t *testing.T) {
	c, ctx, _ := stepsRepo(t)
	lines := "operator: demo 015 Steps here: 4 of 4 open (bmesh spec steps 015 --repo demo)\n" +
		"operator: demo 016 Old steps: 1 of 2 open (bmesh spec steps 016 --repo demo)\n"
	out, errs, code := runCLI(t, nil, "mesh-status")
	if code != 0 || !strings.HasSuffix(out, "\n"+lines) {
		t.Fatalf("mesh-status: %d %q %q", code, out, errs)
	}
	// Ticked from another box: the hub's ticks drop the lines.
	if _, err := mesh.TickSteps(ctx, c, "demo", "015", []mesh.StepTick{{Step: "set the key", Done: "x"}, {Step: "[" + testHost + "] restart", Done: "x"}, {Step: "[otherbox] on the other box", Done: "x"}, {Step: "by hand", Done: "x"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := mesh.TickSteps(ctx, c, "demo", "016", []mesh.StepTick{{Step: "old two", Done: "x"}}); err != nil {
		t.Fatal(err)
	}
	out, errs, code = runCLI(t, nil, "mesh-status")
	if code != 0 || strings.Contains(out, "operator:") {
		t.Fatalf("after the ticks: %d %q %q", code, out, errs)
	}
}

func TestMeshStatusAndWatchSaySettled(t *testing.T) {
	c, ctx := setup(t)
	for _, n := range []mesh.Node{
		{Path: "app-001", Role: "lead", Family: "app-build-1", Settled: "2026-10-24T09:00:00Z"},
		{Path: "app-002", Role: "lead", Family: "app-build-1", Settled: "2026-10-24T09:00:00Z"},
		{Path: "app-003", Role: "lead", Family: "app-build-2"},
	} {
		putNode(t, c, ctx, n)
		stateAt(t, c, n.Path, "done", time.Now())
	}
	out, errs, code := runCLI(t, nil, "mesh-status")
	if code != 0 || !strings.Contains(out, "\nsettled app-build-1: 2 threads\n") || strings.Contains(out, "app-build-2") {
		t.Fatalf("%d %q %q", code, out, errs)
	}

	var b bytes.Buffer
	a := &app{stdout: &b}
	a.printEvent(mesh.Stored{Subject: "bm.daemon.k16.settled",
		Data: []byte(`{"at":"2026-10-24T09:00:00Z","to":"app-001","text":"app-build-1: 2 threads"}`)}, map[string]bool{"app-001": true})
	if want := "2026-10-24T09:00:00Z daemon.k16 settled app-build-1: 2 threads\n"; b.String() != want {
		t.Fatalf("watch printed %q, want %q", b.String(), want)
	}
}
