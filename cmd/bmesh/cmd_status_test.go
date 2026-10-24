package main

import (
	"strings"
	"testing"

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
	if _, err := mesh.TickSteps(ctx, c, "demo", "015", []mesh.StepTick{{N: 1, Done: "x"}, {N: 2, Done: "x"}, {N: 3, Done: "x"}, {N: 4, Done: "x"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := mesh.TickSteps(ctx, c, "demo", "016", []mesh.StepTick{{N: 2, Done: "x"}}); err != nil {
		t.Fatal(err)
	}
	out, errs, code = runCLI(t, nil, "mesh-status")
	if code != 0 || strings.Contains(out, "operator:") {
		t.Fatalf("after the ticks: %d %q %q", code, out, errs)
	}
}
