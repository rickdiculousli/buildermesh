package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// stored reads a subject straight from bm-mesh.
func stored(t *testing.T, c *mesh.Client, subject string) []mesh.Stored {
	t.Helper()
	h, err := mesh.History(t.Context(), c, subject, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestFeedbackPublishesWithTheNodesFamily(t *testing.T) {
	c, ctx := setup(t)
	writeRepos(t, "[repos.demo]\npath = \""+t.TempDir()+"\"\n[repos.buildermesh]\npath = \""+t.TempDir()+"\"\n")
	putNode(t, c, ctx, mesh.Node{Path: "demo-004", Repo: "demo", Spec: "004", Family: "demo-build-2"})
	putNode(t, c, ctx, mesh.Node{Path: "demo-005", Repo: "demo", Spec: "005"})

	out, errs, code := runCLI(t, as("demo-004"), "feedback", "inbox read is slow when empty", "--where", "bmesh inbox", "--fixed", "abc123")
	if code != 0 || !strings.HasPrefix(out, "feedback ") || !strings.HasSuffix(out, " in demo-build-2\n") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	if _, errs, code := runCLI(t, as("demo-004"), "feedback", "the skill says X twice", "--repo", "buildermesh"); code != 0 {
		t.Fatal(code, errs)
	}
	h := stored(t, c, "bm.feedback.demo-build-2")
	if len(h) != 2 {
		t.Fatalf("%d messages", len(h))
	}
	var f mesh.Feedback
	if err := json.Unmarshal(h[0].Data, &f); err != nil {
		t.Fatal(err)
	}
	if f.From != "demo-004" || f.Family != "demo-build-2" || f.Repo != "demo" || f.Spec != "004" || f.Where != "bmesh inbox" ||
		f.Fixed != "abc123" || f.Text != "inbox read is slow when empty" || f.At == "" {
		t.Fatalf("%+v", f)
	}
	if err := json.Unmarshal(h[1].Data, &f); err != nil || f.Repo != "buildermesh" {
		t.Fatalf("%+v %v", f, err)
	}

	// No family, no feedback; nor for an unknown --repo or an empty text.
	if _, errs, code := runCLI(t, as("demo-005"), "feedback", "x"); code != 3 || !strings.Contains(errs, "no build family") {
		t.Fatalf("no family: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, as("demo-004"), "feedback", "x", "--repo", "nope"); code != 2 {
		t.Fatalf("bad repo: %d %q", code, errs)
	}
	if _, _, code := runCLI(t, as("demo-004"), "feedback", " "); code != 2 {
		t.Fatalf("empty: %d", code)
	}
	if n := len(stored(t, c, "bm.feedback.>")); n != 2 {
		t.Fatalf("%d messages after the refusals", n)
	}

	// list prints each with its sequence, then on stderr how to rule on
	// them and drain them.
	out, errs, code = runCLI(t, nil, "feedback", "list", "demo-build-2")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 2 ||
		lines[0] != itoa(h[0].Seq)+" demo-004 (demo 004, where bmesh inbox, fixed abc123): inbox read is slow when empty" ||
		lines[1] != itoa(h[1].Seq)+" demo-004 (buildermesh 004): the skill says X twice" {
		t.Fatalf("%d %q", code, out)
	}
	for _, want := range []string{"next: merge duplicates and drop what main already covers", "titled Quick fix from demo-build-2 #<seq>: <what it changes>",
		"from_feedback: demo-build-2 (<its leads>) #<seq>", "landed as Feedback NNN: proposed from demo-build-2",
		"next: then bmesh feedback drain --seq <the sequences you ruled on>"} {
		if !strings.Contains(errs, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errs)
		}
	}
	out, errs, code = runCLI(t, nil, "feedback", "list", "demo-build-2", "--json")
	var list []mesh.Feedback
	if err := json.Unmarshal([]byte(out), &list); code != 0 || err != nil || len(list) != 2 || list[1].Seq != h[1].Seq || errs != "" {
		t.Fatalf("%d %q %v %q", code, out, err, errs)
	}
	if out, errs, code := runCLI(t, nil, "feedback", "list", "demo-build-9"); code != 0 || out != "" || errs != "" {
		t.Fatalf("empty family: %d %q %q", code, out, errs)
	}

	// watch-done names the watched leads' families with feedback pending.
	putNode(t, c, ctx, mesh.Node{Path: "demo-006", Role: "lead", Repo: "demo", Spec: "006", Family: "demo-build-2"})
	putNode(t, c, ctx, mesh.Node{Path: "demo-007", Role: "lead", Repo: "demo", Spec: "007", Family: "demo-build-9"})
	for _, p := range []string{"demo-006", "demo-007"} {
		meshtest.ForceState(t, c, p, mesh.State{State: "done", By: "node"})
	}
	if out, errs, code := runCLI(t, nil, "watch", "--nodes", "demo-006,demo-007", "--timeout", "10s"); code != 0 ||
		out != "watch-done; next: bmesh feedback list demo-build-2\n" {
		t.Fatalf("watch: %d %q %q", code, out, errs)
	}
	if out, _, _ := runCLI(t, nil, "watch", "--nodes", "demo-007", "--timeout", "10s"); out != "watch-done\n" {
		t.Fatalf("watch with nothing pending: %q", out)
	}

	// A fixer's family waits for the operator: no drain steps, and its
	// leads' watch-done names none.
	putNode(t, c, ctx, mesh.Node{Path: "buildermesh-fix-1", Role: "fixer", Family: "buildermesh-fix-1"})
	putNode(t, c, ctx, mesh.Node{Path: "demo-008", Role: "lead", Repo: "demo", Spec: "008", Family: "buildermesh-fix-1"})
	meshtest.ForceState(t, c, "demo-008", mesh.State{State: "done", By: "node"})
	if _, errs, code := runCLI(t, as("demo-008"), "feedback", "the brief names a dead flag"); code != 0 {
		t.Fatal(code, errs)
	}
	if out, errs, code := runCLI(t, nil, "feedback", "list", "buildermesh-fix-1"); code != 0 || out == "" || errs != "" {
		t.Fatalf("fixer's family: %d %q %q", code, out, errs)
	}
	if out, _, _ := runCLI(t, nil, "watch", "--nodes", "demo-008", "--timeout", "10s"); out != "watch-done\n" {
		t.Fatalf("watch on a fixer's lead: %q", out)
	}
	if _, _, code := runCLI(t, nil, "feedback", "list", "a.b"); code != 2 {
		t.Fatalf("bad family: %d", code)
	}
}

// Feedback naming bmesh's own repo is its fixer's: the coordinator's list
// and watch-done leave it out, and it holds no build.
func TestFeedbackNamingOwnRepoSkipsTheCoordinator(t *testing.T) {
	c, _ := ownSetup(t)
	ctx := t.Context()
	putNode(t, c, ctx, mesh.Node{Path: "demo-006", Role: "lead", Repo: "demo", Spec: "006", Family: "demo-build-5"})
	meshtest.ForceState(t, c, "demo-006", mesh.State{State: "done", By: "node"})
	if _, errs, code := runCLI(t, as("demo-006"), "feedback", "the skill says X twice", "--repo", "bmesh"); code != 0 {
		t.Fatal(code, errs)
	}
	if h := stored(t, c, "bm.feedback.demo-build-5"); len(h) != 1 || !strings.Contains(string(h[0].Data), `"repo":"demo"`) {
		t.Fatalf("%+v", h)
	}
	if out, errs, code := runCLI(t, nil, "feedback", "list", "demo-build-5"); code != 0 || out != "" || errs != "" {
		t.Fatalf("list: %d %q %q", code, out, errs)
	}
	if out, _, _ := runCLI(t, nil, "watch", "--nodes", "demo-006", "--timeout", "10s"); out != "watch-done\n" {
		t.Fatalf("watch: %q", out)
	}
	if out, errs, code := runCLI(t, nil, "build", "demo", "002"); code != 0 {
		t.Fatalf("held: %d %q %q", code, out, errs)
	}
}

// On a box whose registry lacks bmesh's repo, a lead names bmesh by its
// module; the coordinator is told to offer it to the operator as a GitHub
// issue and drain it, and it holds no build meanwhile.
func TestFeedbackNamingBmeshWithNoRowGoesToGitHub(t *testing.T) {
	c, _, _ := buildSetup(t)
	ctx := t.Context()
	withRevision(t, "") // the binary's module is example.com/bm; no row declares it
	putNode(t, c, ctx, mesh.Node{Path: "demo-006", Role: "lead", Repo: "demo", Spec: "006", Family: "demo-build-5"})
	meshtest.ForceState(t, c, "demo-006", mesh.State{State: "done", By: "node"})
	for _, name := range []string{"bmesh", "bm", "example.com/bm"} {
		if _, errs, code := runCLI(t, as("demo-006"), "feedback", "via "+name, "--repo", name); code != 0 {
			t.Fatal(name, code, errs)
		}
	}
	if _, errs, code := runCLI(t, as("demo-006"), "feedback", "x", "--repo", "nope"); code != 2 {
		t.Fatalf("bad repo: %d %q", code, errs)
	}
	out, errs, code := runCLI(t, nil, "feedback", "list", "demo-build-5")
	if code != 0 || strings.Count(out, "(example.com/bm 006)") != 3 ||
		!strings.Contains(errs, "ask the operator once, listing them, which to file as GitHub issues (gh issue create --repo example.com/bm") ||
		!strings.Contains(errs, "next: of the rest, merge duplicates") {
		t.Fatalf("list: %d %q %q", code, out, errs)
	}
	if out, _, _ := runCLI(t, nil, "watch", "--nodes", "demo-006", "--timeout", "10s"); out != "watch-done; next: bmesh feedback list demo-build-5\n" {
		t.Fatalf("watch: %q", out)
	}
	if out, errs, code := runCLI(t, nil, "build", "demo", "002"); code != 0 {
		t.Fatalf("held: %d %q %q", code, out, errs)
	}
}

func itoa(n uint64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestFeedbackDrainPurgesOnlyTheFamilyThroughSeq(t *testing.T) {
	c, ctx := setup(t)
	pub := func(family, text string) uint64 {
		seq, err := mesh.PublishFeedback(ctx, c, mesh.Feedback{From: "x-001", Family: family, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		return seq
	}
	pub("a-build-1", "one")
	two := pub("a-build-1", "two")
	pub("a-build-2", "other family")
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "x-001", Event: "progress", Text: "an event"}); err != nil {
		t.Fatal(err)
	}
	three := pub("a-build-1", "three, after --through")

	out, errs, code := runCLI(t, nil, "mesh-status")
	if code != 0 || !strings.HasSuffix(out, "feedback: 3 pending in a-build-1\nfeedback: 1 pending in a-build-2\n") {
		t.Fatalf("status: %d %q %q", code, out, errs)
	}

	if out, errs, code := runCLI(t, nil, "feedback", "drain", "a-build-1", "--through", itoa(two)); code != 0 || out != "drained 2 from a-build-1\n" {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	h := stored(t, c, "bm.feedback.a-build-1")
	if len(h) != 1 || h[0].Seq != three {
		t.Fatalf("left %+v", h)
	}
	if len(stored(t, c, "bm.feedback.a-build-2")) != 1 || len(stored(t, c, "bm.x-001.progress")) != 1 {
		t.Fatal("drain touched another subject")
	}
	out, _, _ = runCLI(t, nil, "mesh-status")
	if !strings.HasSuffix(out, "feedback: 1 pending in a-build-1\nfeedback: 1 pending in a-build-2\n") {
		t.Fatalf("status: %q", out)
	}

	// Without --through: up to the last one there.
	if out, _, code := runCLI(t, nil, "feedback", "drain", "a-build-1"); code != 0 || out != "drained 1 from a-build-1\n" {
		t.Fatalf("%d %q", code, out)
	}
	if out, _, code := runCLI(t, nil, "feedback", "drain", "a-build-1"); code != 0 || out != "drained 0 from a-build-1\n" {
		t.Fatalf("%d %q", code, out)
	}
	if out, _, _ := runCLI(t, nil, "feedback", "list", "a-build-1"); out != "" {
		t.Fatalf("list after drain: %q", out)
	}
	if out, _, _ := runCLI(t, nil, "mesh-status"); strings.Contains(out, "a-build-1") || !strings.Contains(out, "feedback: 1 pending in a-build-2") {
		t.Fatalf("status: %q", out)
	}
	if _, _, code := runCLI(t, nil, "feedback", "drain", "a-build-2", "--through", "x"); code != 2 {
		t.Fatalf("bad --through: %d", code)
	}
}
