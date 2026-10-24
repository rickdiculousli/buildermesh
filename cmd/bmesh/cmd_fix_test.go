package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// ownSetup is buildSetup with demo made bmesh's own repo: its go.mod
// declares the module the binary was built from.
func ownSetup(t *testing.T) (*mesh.Client, string) {
	t.Helper()
	c, repo, _ := buildSetup(t)
	withRevision(t, "") // the binary's module is example.com/bm
	commitFile(t, repo, "go.mod", "module example.com/bm\n", "go.mod")
	return c, repo
}

// writeSpec commits a spec file on demo's main.
func writeSpec(t *testing.T, repo, name, front string) {
	t.Helper()
	id := name[:3]
	text := "---\nid: " + id + "\ntitle: Spec " + id + "\nkind: refinement\n" + front + "---\n\n## Proposal\nA fix.\n"
	commitFile(t, repo, "docs/proposals/"+name+".md", text, "spec "+id)
}

func pubFeedback(t *testing.T, c *mesh.Client, f mesh.Feedback) uint64 {
	t.Helper()
	seq, err := mesh.PublishFeedback(t.Context(), c, f)
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

func TestFixRefusesWithNoOwnRow(t *testing.T) {
	c, _, fake := buildSetup(t)
	pubFeedback(t, c, mesh.Feedback{From: "demo-002", Family: "demo-build-1", Repo: "demo", Text: "x"})
	if _, errs, code := runCLI(t, nil, "fix"); code != 3 || errs != "bmesh: refused: no registry row holds the module bmesh was built from\n" {
		t.Fatalf("%d %q", code, errs)
	}
	if l := launches(fake); len(l) != 0 {
		t.Fatalf("launched %v", l)
	}
	// With no own row, a repo's feedback holds its builds as before.
	if _, errs, code := runCLI(t, nil, "build", "demo", "002"); code != 3 || !strings.Contains(errs, "held: demo-build-1 has 1 feedback pending") {
		t.Fatalf("hold: %d %q", code, errs)
	}
}

func TestFixStartsOneFixerWithTheRulings(t *testing.T) {
	c, repo, fake := buildSetup(t)
	ctx := t.Context()
	withRevision(t, "")
	commitFile(t, repo, "go.mod", "module example.com/bm\n", "go.mod")
	// A fix family left feedback once, and a forgotten fixer's lead is
	// still registered: the next fixer numbers past both.
	pubFeedback(t, c, mesh.Feedback{From: "demo-040", Family: "demo-fix-1", Repo: "demo", Text: "x"})
	putNode(t, c, ctx, mesh.Node{Path: "demo-041", Repo: "demo", Family: "demo-fix-2"})
	meshtest.ForceState(t, c, "demo-041", mesh.State{State: "done", By: "node"})
	out, errs, code := runCLI(t, nil, "fix")
	if code != 0 || !strings.HasPrefix(out, "fix: demo-fix-3 started (thread ") || !strings.HasSuffix(out, ", claude-opus-5-5, on "+testHost+")\n") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	n, ok, err := mesh.GetNode(ctx, c, "demo-fix-3")
	if err != nil || !ok || n.Role != "fixer" || n.Family != "demo-fix-3" || n.Repo != "demo" || n.Spec != "" ||
		n.Model != "claude-opus-5-5" || n.Effort != "high" {
		t.Fatalf("node %+v %v %v", n, ok, err)
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("BMESH_CACHE_DIR"), "bmesh", "briefs", "demo-fix-3.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Fix: rule on the feedback naming demo", "`buildermesh:fix`", "--node demo-fix-3 started", "--family demo-fix-3",
		"1. `root`:", "2. `failing-first`:", "3. `smallest`:", "4. `structure`:", "5. `premise`:", "6. `no-new-surface`:", "7. `reversible`:", "8. `drop`:",
		"`npm test`", "the operator never has to look anything up"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("brief lacks %q", want)
		}
	}
	if strings.Contains(string(b), "{{") {
		t.Fatal("placeholder left in the brief")
	}
	if l := launches(fake); len(l) != 1 || l[0]["title"] != "[fix] demo: feedback" || l[0]["message"] != string(b) {
		t.Fatalf("launches %s", jsonOf(l))
	}
	// A repo's own brief override never replaces the rulings.
	commitFile(t, repo, ".bmesh/briefs/fix.md", "ours\n", "override")

	// One writer: a second fixer is refused while the first is live.
	if _, errs, code := runCLI(t, nil, "fix"); code != 3 || errs != "bmesh: refused: fixer demo-fix-3 is idle; one fixer writes the queue\n" {
		t.Fatalf("second: %d %q", code, errs)
	}
	meshtest.ForceState(t, c, "demo-fix-3", mesh.State{State: "done", By: "node"})
	if out, errs, code := runCLI(t, nil, "fix", "--effort", "medium"); code != 0 || !strings.HasPrefix(out, "fix: demo-fix-4 started") {
		t.Fatalf("after done: %d %q %q", code, out, errs)
	}
	b, _ = os.ReadFile(filepath.Join(os.Getenv("BMESH_CACHE_DIR"), "bmesh", "briefs", "demo-fix-4.md"))
	if !strings.Contains(string(b), "1. `root`:") {
		t.Fatalf("override used:\n%s", b)
	}
	if n, _, _ := mesh.GetNode(ctx, c, "demo-fix-4"); n.Effort != "medium" {
		t.Fatalf("effort %+v", n)
	}
}

// The fixer is not a lead: [runner] max_leads doesn't count it, nor stop it.
func TestFixerIsNotCountedAsALead(t *testing.T) {
	_, _ = ownSetup(t)
	addRunnerKeys(t, "max_leads = 1\n")
	if out, errs, code := runCLI(t, nil, "fix"); code != 0 {
		t.Fatalf("fix: %d %q %q", code, out, errs)
	}
	if out, errs, code := runCLI(t, nil, "build", "demo", "002"); code != 0 {
		t.Fatalf("build beside the fixer: %d %q %q", code, out, errs)
	}
	if _, errs, code := runCLI(t, nil, "build", "demo", "004", "--family", "demo-build-1"); code != 3 || !strings.Contains(errs, "1 leads already working") {
		t.Fatalf("cap: %d %q", code, errs)
	}
}

func TestFeedbackListByRepoSpansFamiliesAndHidesNamedSeqs(t *testing.T) {
	c, repo := ownSetup(t)
	one := pubFeedback(t, c, mesh.Feedback{From: "demo-002", Family: "demo-build-1", Repo: "demo", Spec: "002", Text: "first"})
	pubFeedback(t, c, mesh.Feedback{From: "demo-002", Family: "demo-build-1", Repo: "other", Spec: "002", Text: "not demo's"})
	two := pubFeedback(t, c, mesh.Feedback{From: "other-007", Family: "other-build-3", Repo: "demo", Spec: "007", Where: "bmesh land", Text: "second"})
	three := pubFeedback(t, c, mesh.Feedback{From: "demo-020", Family: "demo-fix-1", Repo: "demo", Spec: "020", Text: "third"})

	out, errs, code := runCLI(t, nil, "feedback", "list", "--repo", "demo")
	want := itoa(one) + " demo-002 (demo 002, family demo-build-1): first\n" +
		itoa(two) + " other-007 (demo 007, family other-build-3, where bmesh land): second\n" +
		itoa(three) + " demo-020 (demo 020, family demo-fix-1): third\n"
	if code != 0 || out != want {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	// A spec on main naming #<seq> hides that issue, whatever its status.
	writeSpec(t, repo, "020-fix", "status: proposed\nfrom_feedback: other-build-3 (other-007) #"+itoa(two)+"\n")
	out, _, _ = runCLI(t, nil, "feedback", "list", "--repo", "demo", "--json")
	var list []mesh.Feedback
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list) != 2 || list[0].Seq != one || list[1].Seq != three || list[1].Family != "demo-fix-1" {
		t.Fatalf("%q %v", out, err)
	}
	// So does a fix commit on main naming it, the fixer's or a
	// coordinator's; a commit that only mentions the number doesn't.
	commitFile(t, repo, "a.txt", "1", "Note #"+itoa(one)+": not a fix")
	commitFile(t, repo, "a.txt", "2", "Fix #"+itoa(three)+": the land wait")
	if out, _, _ := runCLI(t, nil, "feedback", "list", "--repo", "demo"); out != itoa(one)+" demo-002 (demo 002, family demo-build-1): first\n" {
		t.Fatalf("after Fix #: %q", out)
	}
	commitFile(t, repo, "a.txt", "3", "Quick fix from demo-build-1 #"+itoa(two)+", #"+itoa(one)+": both")
	if out, errs, code := runCLI(t, nil, "feedback", "list", "--repo", "demo"); code != 0 || out != "" {
		t.Fatalf("after Quick fix: %d %q %q", code, out, errs)
	}
	if _, _, code := runCLI(t, nil, "feedback", "list", "demo-build-1", "--repo", "demo"); code != 2 {
		t.Fatalf("both: %d", code)
	}
	if _, _, code := runCLI(t, nil, "feedback", "list", "--repo", "nope"); code != 2 {
		t.Fatalf("unknown repo: %d", code)
	}

	// drain --seq deletes exactly those, across families, and says which
	// were gone already; a sequence that isn't feedback deletes nothing.
	ev := mesh.Event{From: "demo-002", Event: "progress", Text: "x"}
	if err := mesh.PublishEvent(t.Context(), c, ev); err != nil {
		t.Fatal(err)
	}
	evSeq := stored(t, c, "bm.demo-002.progress")[0].Seq
	if _, errs, code := runCLI(t, nil, "feedback", "drain", "--seq", itoa(one)+","+itoa(evSeq)); code != 3 || !strings.Contains(errs, "not feedback") {
		t.Fatalf("event: %d %q", code, errs)
	}
	if len(stored(t, c, "bm.feedback.>")) != 4 {
		t.Fatal("a refused drain deleted")
	}
	if out, errs, code := runCLI(t, nil, "feedback", "drain", "--seq", itoa(one)+","+itoa(three)); code != 0 || out != "drained 2\n" {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	left := stored(t, c, "bm.feedback.>")
	if len(left) != 2 || left[0].Subject != "bm.feedback.demo-build-1" || left[1].Seq != two {
		t.Fatalf("left %+v", left)
	}
	if out, _, code := runCLI(t, nil, "feedback", "drain", "--seq", itoa(one)); code != 0 || out != "drained 0\nnot pending: "+itoa(one)+"\n" {
		t.Fatalf("again: %d %q", code, out)
	}
	if _, _, code := runCLI(t, nil, "feedback", "drain", "demo-build-1", "--seq", itoa(two)); code != 2 {
		t.Fatalf("family and --seq: %d", code)
	}
	if _, _, code := runCLI(t, nil, "feedback", "drain", "--seq", "x"); code != 2 {
		t.Fatalf("bad --seq: %d", code)
	}
}

func TestBuildRefusesRuledLAndAThirdFix(t *testing.T) {
	_, repo, _ := buildSetup(t)
	writeSpec(t, repo, "020-big", "size: L\njudge: opus\nstatus: accepted\nruled: [root]\n")
	writeSpec(t, repo, "021-fix", "size: S\njudge: opus\nstatus: done\nfix_of: 001\n")
	writeSpec(t, repo, "022-fix", "size: S\njudge: opus\nstatus: done\nfix_of: 021\n")
	writeSpec(t, repo, "023-fix", "size: S\njudge: opus\nstatus: accepted\nruled: [root]\nfix_of: 022\n")
	writeSpec(t, repo, "024-fix", "size: S\njudge: opus\nstatus: accepted\nruled: [root, failing-first]\nfix_of: 021\n")
	writeSpec(t, repo, "025-big", "size: L\njudge: opus\nstatus: accepted\n")
	head := gitOut(t, "-C", repo, "rev-parse", "main")
	if _, errs, code := runCLI(t, nil, "build", "demo", "020"); code != 3 ||
		errs != "bmesh: refused: spec 020 is ruled and sized L (reversible: anything sized L is proposed, never built); set it proposed\n" {
		t.Fatalf("ruled L: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "build", "demo", "023"); code != 3 ||
		errs != "bmesh: refused: spec 023 is fix 3 on one premise (023 fixes 022 fixes 021 fixes 001) (premise: no third fix on one premise); set it proposed with the belief the fixes assumed\n" {
		t.Fatalf("third fix: %d %q", code, errs)
	}
	if gitOut(t, "-C", repo, "rev-parse", "main") != head {
		t.Fatal("a refused build recorded on main")
	}
	// Several say the same per spec.
	out, errs, code := runCLI(t, nil, "build", "demo", "020", "023")
	if code != 0 || !strings.HasPrefix(out, "not started 020 Spec 020: refused: spec 020 is ruled and sized L") ||
		!strings.Contains(out, "\nnot started 023 Spec 023: refused: spec 023 is fix 3 on one premise") {
		t.Fatalf("several: %d %q %q", code, out, errs)
	}
	// Two deep builds, and an L the operator accepted (no ruled) builds.
	for _, id := range []string{"024", "025"} {
		if out, errs, code := runCLI(t, nil, "build", "demo", id, "--force"); code != 0 {
			t.Fatalf("%s: %d %q %q", id, code, out, errs)
		}
	}
}

func TestFixAndOperatorPipelinesDontHoldEachOther(t *testing.T) {
	c, repo := ownSetup(t)
	ctx := t.Context()
	// A fix lead is running in the fixer's family.
	putNode(t, c, ctx, mesh.Node{Path: "demo-030", Repo: "demo", Family: "demo-fix-1"})
	if _, err := mesh.WriteState(ctx, c, "demo-030", mesh.State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	// The operator's build goes beside it.
	out, errs, code := runCLI(t, nil, "build", "demo", "002")
	if code != 0 {
		t.Fatalf("operator build: %d %q %q", code, out, errs)
	}
	var op buildStarted
	if err := json.Unmarshal([]byte(out), &op); err != nil {
		t.Fatal(err)
	}
	// And a fix build goes beside the operator's running lead.
	writeSpec(t, repo, "020-fix", "size: S\njudge: opus\nstatus: accepted\nruled: [root]\nfix_of: 002\n")
	if out, errs, code := runCLI(t, nil, "build", "demo", "020", "--family", "demo-fix-1"); code != 0 {
		t.Fatalf("fix build: %d %q %q", code, out, errs)
	}
	// Release holds while a lead of either pipeline runs.
	meshtest.ForceState(t, c, "demo-002", mesh.State{State: "done", By: "node"})
	if _, errs, code := runCLI(t, nil, "release", "demo"); code != 3 || !strings.Contains(errs, "held: demo-fix-1 is still running (demo-020 idle, demo-030 working)") &&
		!strings.Contains(errs, "held: demo-fix-1 is still running (demo-030 working, demo-020 idle)") {
		t.Fatalf("release: %d %q", code, errs)
	}
	for _, p := range []string{"demo-020", "demo-030"} {
		meshtest.ForceState(t, c, p, mesh.State{State: "done", By: "node"})
	}

	// Feedback naming bmesh's own repo holds no build; another repo's,
	// left in demo's family, holds as before.
	pubFeedback(t, c, mesh.Feedback{From: "demo-002", Family: op.Family, Repo: "demo", Text: "the fixer's"})
	pubFeedback(t, c, mesh.Feedback{From: "other-001", Family: "other-build-2", Repo: "demo", Text: "the fixer's too"})
	if out, errs, code := runCLI(t, nil, "build", "demo", "004"); code != 0 {
		t.Fatalf("own feedback held: %d %q %q", code, out, errs)
	}
	meshtest.ForceState(t, c, "demo-004", mesh.State{State: "done", By: "node"})
	pubFeedback(t, c, mesh.Feedback{From: "demo-002", Family: op.Family, Repo: "other", Text: "other's"})
	if _, errs, code := runCLI(t, nil, "build", "demo", "009"); code != 3 || errs != "bmesh: held: "+op.Family+" has 1 feedback pending; drain it with bmesh feedback list/drain "+op.Family+"\n" {
		t.Fatalf("other's feedback: %d %q", code, errs)
	}
	// The fix pipeline is not held by the operator's family's feedback.
	writeSpec(t, repo, "021-fix", "size: S\njudge: opus\nstatus: accepted\nruled: [root]\n")
	if out, errs, code := runCLI(t, nil, "build", "demo", "021", "--family", "demo-fix-1"); code != 0 {
		t.Fatalf("fix build past op feedback: %d %q %q", code, out, errs)
	}
}

// A fix lead's ask, blocked and done go to its fixer's inbox: the fixer,
// not the operator, hears them.
func TestFixLeadReportsToItsFixer(t *testing.T) {
	c, _ := ownSetup(t)
	ctx := t.Context()
	putNode(t, c, ctx, mesh.Node{Path: "demo-fix-1", Role: "fixer", Repo: "demo", Family: "demo-fix-1"})
	fixer, _ := mesh.ParsePath("demo-fix-1")
	if err := mesh.EnsureInbox(ctx, c, fixer); err != nil {
		t.Fatal(err)
	}
	putNode(t, c, ctx, mesh.Node{Path: "demo-030", Repo: "demo", Family: "demo-fix-1"})
	putNode(t, c, ctx, mesh.Node{Path: "demo-031", Repo: "demo", Family: "demo-build-1"})
	for _, p := range []string{"demo-030", "demo-031"} {
		for _, ev := range [][]string{{"started"}, {"ask", "--text", "which db?"}, {"blocked", "--text", "no db"}} {
			if _, errs, code := runCLI(t, as(p), ev...); code != 0 {
				t.Fatalf("%s %s: %d %q", p, ev[0], code, errs)
			}
		}
	}
	msgs, err := mesh.Fetch(ctx, c, fixer, true)
	if err != nil || len(msgs) != 2 || msgs[0].Text != "ask from demo-030: which db?" || msgs[1].Text != "blocked from demo-030: no db" ||
		msgs[1].Report == nil || msgs[1].Report.Event != "blocked" {
		t.Fatalf("fixer inbox %+v %v", msgs, err)
	}
}

// mesh-status shows bmesh's own open issues as one fixes line, naming who
// has them; other repos' feedback keeps its line per family.
func TestMeshStatusFixesLine(t *testing.T) {
	c, _ := ownSetup(t)
	ctx := t.Context()
	pubFeedback(t, c, mesh.Feedback{From: "demo-002", Family: "demo-build-1", Repo: "demo", Text: "mine", At: "2026-10-17T10:00:00Z"})
	pubFeedback(t, c, mesh.Feedback{From: "other-007", Family: "other-build-3", Repo: "demo", Text: "mine too", At: "2026-10-17T10:00:00Z"})
	pubFeedback(t, c, mesh.Feedback{From: "demo-002", Family: "demo-build-1", Repo: "other", Text: "not mine"})
	tail := func(want string) {
		t.Helper()
		out, errs, code := runCLI(t, nil, "mesh-status")
		if code != 0 || !strings.Contains(out, want) || strings.Contains(out, "other-build-3") {
			t.Fatalf("%d %q %q\nwant %q", code, out, errs, want)
		}
	}
	tail("fixes: 2 pending for demo (starts within a minute)\nfeedback: 1 pending in demo-build-1\n")
	putNode(t, c, ctx, mesh.Node{Path: "demo-fix-1", Role: "fixer", Repo: "demo", Family: "demo-fix-1", StartedAt: "2026-10-17T11:00:00Z"})
	meshtest.ForceState(t, c, "demo-fix-1", mesh.State{State: "working", By: "node"})
	tail("fixes: 2 pending for demo (demo-fix-1 working)\n")
	meshtest.ForceState(t, c, "demo-fix-1", mesh.State{State: "blocked", By: "node"})
	tail("fixes: 2 pending for demo (demo-fix-1 blocked)\n")
	meshtest.ForceState(t, c, "demo-fix-1", mesh.State{State: "done", By: "node"})
	tail("fixes: 2 pending for demo (demo-fix-1 done; they wait on you)\n")
}
