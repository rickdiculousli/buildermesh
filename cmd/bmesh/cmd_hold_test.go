package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// A hold at land refuses bmesh land, pushing nothing; the lead runs held
// and is held; once released, the same land lands.
func TestHoldRefusesLandUntilUnhold(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	if _, errs, code := runCLI(t, as("app-012"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	out, errs, code := runCLI(t, as("operator"), "hold", "app-012", "--text", "hub move first")
	if code != 0 || out != "holding app-012 before land: hub move first\n" {
		t.Fatalf("hold: %d %q %q", code, out, errs)
	}
	h, ok, err := mesh.GetLeadHold(ctx, c, "app-012")
	if err != nil || !ok || h.Gate != "land" || h.Text != "hub move first" || h.By != "operator" || h.At == "" {
		t.Fatalf("hold %+v %v %v", h, ok, err)
	}
	work := gitOut(t, "rev-parse", "HEAD")
	base := r.originMain(t)
	out, errs, code = runCLI(t, as("app-012"), "land")
	if code != 3 || out != "" || errs != "bmesh: held before land by operator 0m ago: hub move first; nothing is pushed: "+
		"run bmesh held and end your turn, and run bmesh land again once the release wakes you\n" {
		t.Fatalf("land: %d %q %q", code, out, errs)
	}
	if r.originMain(t) != base || gitOut(t, "rev-parse", "HEAD") != work {
		t.Fatal("held land moved origin or the branch")
	}
	assertNoLeases(t, c, ctx)

	out, errs, code = runCLI(t, as("app-012"), "held")
	if code != 0 || out != "held app-012 before land by operator: hub move first; end your turn: the release wakes you\n" {
		t.Fatalf("held: %d %q %q", code, out, errs)
	}
	if s, _, _ := mesh.ReadState(ctx, c, "app-012"); s.State != "held" || s.Text != "before land by operator: hub move first" {
		t.Fatalf("state %+v", s)
	}
	if ev, ok, err := mesh.Last(ctx, c, "bm.app-012.held"); err != nil || !ok || !strings.Contains(string(ev.Data), "hub move first") {
		t.Fatalf("held event %v %v", ok, err)
	}

	out, errs, code = runCLI(t, as("operator"), "unhold", "app-012", "--text", "hub moved")
	if code != 0 || out != "released app-012\n" {
		t.Fatalf("unhold: %d %q %q", code, out, errs)
	}
	if _, ok, _ := mesh.GetLeadHold(ctx, c, "app-012"); ok {
		t.Fatal("still held")
	}
	out, _, _ = runCLI(t, as("app-012"), "inbox")
	if !strings.Contains(out, "operator: hold from operator (land): hub move first\n") || !strings.Contains(out, "operator: released by operator: hub moved\n") {
		t.Fatalf("inbox %q", out)
	}
	// Released, held is refused: the lead carries on.
	if _, errs, code := runCLI(t, as("app-012"), "held"); code != 3 || !strings.Contains(errs, "app-012 is not held") {
		t.Fatalf("held after release: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, as("app-012"), "progress", "--text", "landing"); code != 0 || stateOf(t, c, ctx, "app-012") != "working" {
		t.Fatalf("progress: %d %q", code, errs)
	}
	if out, errs, code := runCLI(t, as("app-012"), "land"); code != 0 || !strings.Contains(out, "landed ") {
		t.Fatalf("land after release: %d\n%s\nerr %q", code, out, errs)
	}
	if r.originMain(t) == base {
		t.Fatal("nothing pushed")
	}
}

// A hold at now is the first line of every inbox read while it stands.
func TestInboxPrintsANowHoldFirst(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", Spec: "012"})
	sendTo(t, c, ctx, "app-012", mesh.Message{From: "app-012.alpha", Kind: "message", Text: "ask from app-012.alpha: which file?"})
	if _, errs, code := runCLI(t, as("operator"), "hold", "app-012", "--gate", "now", "--text", "wrong approach, stop"); code != 0 {
		t.Fatal(code, errs)
	}
	first := "held now by operator 0m ago: wrong approach, stop; stop here: run bmesh held, end your turn, and wait for the release\n"
	out, errs, code := runCLI(t, as("app-012"), "inbox")
	lines := strings.SplitAfter(out, "\n")
	if code != 0 || len(lines) < 3 || lines[0] != first || !strings.Contains(out, "app-012.alpha: ask from") ||
		!strings.Contains(out, "operator: hold from operator (now): wrong approach, stop\n") {
		t.Fatalf("inbox: %d %q %q", code, out, errs)
	}
	if out, _, _ := runCLI(t, as("app-012"), "inbox"); out != first+"inbox: empty\n" {
		t.Fatalf("second inbox %q", out)
	}
	// --json carries it as a message of kind held, first.
	out, _, code = runCLI(t, as("app-012"), "inbox", "--json")
	var m mesh.Message
	if code != 0 || json.Unmarshal([]byte(strings.SplitN(out, "\n", 2)[0]), &m) != nil || m.Kind != "held" || m.From != "operator" {
		t.Fatalf("inbox --json %q", out)
	}
	if _, errs, code := runCLI(t, as("operator"), "unhold", "app-012"); code != 0 {
		t.Fatal(code, errs)
	}
	if out, _, _ := runCLI(t, as("app-012"), "inbox"); strings.HasPrefix(out, "held ") || !strings.Contains(out, "operator: released by operator\n") {
		t.Fatalf("inbox after release %q", out)
	}
}

// --family holds every live lead of the family and no one else, and
// unhold --family releases exactly those.
func TestHoldAndUnholdAFamily(t *testing.T) {
	c, ctx := setup(t)
	for _, n := range []mesh.Node{
		{Path: "app-001", Family: "app-build-7"}, {Path: "app-002", Family: "app-build-7"},
		{Path: "app-003", Family: "app-build-7"}, {Path: "app-004", Family: "app-build-8"},
		{Path: "app-001.x", Role: "executor", Parent: "app-001", Family: "app-build-7"},
	} {
		putNode(t, c, ctx, n)
	}
	for _, p := range []string{"app-001", "app-001.x", "app-004"} {
		if _, errs, code := runCLI(t, as(p), "started"); code != 0 {
			t.Fatal(code, errs)
		}
	}
	if _, errs, code := runCLI(t, as("app-003"), "started"); code != 0 {
		t.Fatal(code, errs)
	}
	if _, errs, code := runCLI(t, as("app-003"), "blocked", "--text", "no creds"); code != 0 {
		t.Fatal(code, errs)
	}
	out, errs, code := runCLI(t, as("operator"), "hold", "--family", "app-build-7", "--text", "hub move first")
	if code != 0 || out != "holding app-001 before land: hub move first\nholding app-002 before land: hub move first\n" {
		t.Fatalf("hold: %d %q %q", code, out, errs)
	}
	if _, errs, code := runCLI(t, as("operator"), "hold", "app-004", "--text", "after 001"); code != 0 {
		t.Fatal(code, errs)
	}
	paths := func() []string {
		hs, err := mesh.ListLeadHolds(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, h := range hs {
			out = append(out, h.Path)
		}
		return out
	}
	if got := paths(); !slices.Equal(got, []string{"app-001", "app-002", "app-004"}) {
		t.Fatalf("held %v", got)
	}
	out, errs, code = runCLI(t, as("operator"), "unhold", "--family", "app-build-7", "--text", "hub moved")
	if code != 0 || out != "released app-001\nreleased app-002\n" {
		t.Fatalf("unhold: %d %q %q", code, out, errs)
	}
	if got := paths(); !slices.Equal(got, []string{"app-004"}) {
		t.Fatalf("held after unhold %v", got)
	}
	if out, _, _ := runCLI(t, as("operator"), "unhold", "--family", "app-build-7"); out != "app-build-7: nothing held\n" {
		t.Fatalf("second unhold %q", out)
	}
	if _, errs, code := runCLI(t, as("operator"), "hold", "--family", "app-build-9", "--text", "x"); code != 3 || !strings.Contains(errs, "no live lead in family app-build-9") {
		t.Fatalf("empty family: %d %q", code, errs)
	}
}

// Executors and finished leads are never held; a release of a lead not
// held says so and exits 0; held without a hold is refused.
func TestHoldRefusalsAndUnholdOfUnheld(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012"})
	putNode(t, c, ctx, mesh.Node{Path: "app-012.x", Role: "executor", Parent: "app-012"})
	if _, errs, code := runCLI(t, as("operator"), "hold", "app-012.x", "--text", "x"); code != 3 ||
		errs != "bmesh: refused: app-012.x is an executor; executors are never held: hold its lead app-012\n" {
		t.Fatalf("executor: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, as("operator"), "hold", "app-099", "--text", "x"); code != 3 || errs != "bmesh: refused: no node app-099\n" {
		t.Fatalf("no node: %d %q", code, errs)
	}
	if _, _, code := runCLI(t, as("operator"), "hold", "app-012"); code != 2 {
		t.Fatalf("no text: %d", code)
	}
	if _, _, code := runCLI(t, as("operator"), "hold", "app-012", "--gate", "later", "--text", "x"); code != 2 {
		t.Fatalf("bad gate: %d", code)
	}
	out, errs, code := runCLI(t, as("operator"), "unhold", "app-012")
	if code != 0 || out != "app-012: not held\n" || errs != "" {
		t.Fatalf("unhold unheld: %d %q %q", code, out, errs)
	}
	p, _ := mesh.ParsePath("app-012")
	if n, err := mesh.Unread(ctx, c, p); err != nil || n != 0 {
		t.Fatalf("unhold of an unheld lead sent %d (%v)", n, err)
	}
	if _, errs, code := runCLI(t, as("app-012"), "held"); code != 3 || !strings.Contains(errs, "refused: app-012 is not held") {
		t.Fatalf("held unheld: %d %q", code, errs)
	}
	// A forgotten node takes its hold with it.
	if _, errs, code := runCLI(t, as("operator"), "hold", "app-012", "--text", "x"); code != 0 {
		t.Fatal(code, errs)
	}
	meshtest.ForceState(t, c, "app-012", mesh.State{State: "done"})
	meshtest.ForceState(t, c, "app-012.x", mesh.State{State: "done"})
	if _, errs, code := runCLI(t, nil, "node", "forget", "app-012"); code != 0 {
		t.Fatal(code, errs)
	}
	if _, ok, _ := mesh.GetLeadHold(ctx, c, "app-012"); ok {
		t.Fatal("forget left the hold")
	}
}

// mesh-status names each hold with its age.
func TestMeshStatusNamesHolds(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012"})
	if _, errs, code := runCLI(t, as("operator"), "hold", "app-012", "--text", "read the diff first"); code != 0 {
		t.Fatal(code, errs)
	}
	out, errs, code := runCLI(t, nil, "mesh-status")
	if code != 0 || !strings.Contains(out, "\napp-012 held before land by operator 0m ago: read the diff first\n") {
		t.Fatalf("mesh-status: %d %q %q", code, out, errs)
	}
	out, _, _ = runCLI(t, nil, "mesh-status", "--json")
	var rows []statusRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 || rows[0].Hold == nil || rows[0].Hold.Gate != "land" {
		t.Fatalf("--json %q %v", out, err)
	}
}
