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

// buildSetup is proposeSetup with the ledger replaced: specs 001 (done),
// 002 and 003 (accepted, 003 builds on 002), 004 (accepted, names 010 and
// 001), 005 (accepted, no judge) and 006 (proposed), on main and pushed.
func buildSetup(t *testing.T) (*mesh.Client, string, string) {
	t.Helper()
	c, repo, log := proposeSetup(t, "")
	dir := filepath.Join(repo, "docs", "proposals")
	for _, f := range []string{"012-thing.md", "013-thing.md"} {
		os.Remove(filepath.Join(dir, f))
	}
	specs := map[string]string{
		"001-first":   "status: done\njudge: opus\nmerged: abc\n---\n\n## Proposal\nThe first.\n",
		"002-second":  "status: accepted\njudge: opus\n---\n\n## Proposal\nThe second, after 001.\n",
		"003-third":   "status: accepted\njudge: fable\n---\n\n## Proposal\nBuilds on 002; deleted in 010.\n",
		"004-fourth":  "status: accepted\njudge: sonnet\n---\n\n## Proposal\nIndependent of 001 to 001; the 120 s timeout stays; instead of 005 and 007.\n",
		"007-seventh": "status: rejected\nreason: no\n---\n\n## Proposal\nRejected; 004 replaces it.\n",
		"008-eighth":  "status: later\n---\n\n## Proposal\nLater.\n",
		"009-ninth":   "status: accepted\njudge: opus\n---\n\n## Proposal\nAfter 008.\n",
		"005-fifth":   "status: accepted\n---\n\n## Proposal\nNo judge.\n",
		"006-sixth":   "status: proposed\njudge: opus\n---\n\n## Proposal\nNot yet.\n",
	}
	for name, rest := range specs {
		id := name[:3]
		text := "---\nid: " + id + "\ntitle: Spec " + id + "\nkind: feature\nsize: S\n" + rest
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "ledger")
	meshtest.Git(t, repo, "push", "origin", "main")
	return c, repo, log
}

func TestBuildRecordsBuildingThenStartsTheLead(t *testing.T) {
	c, repo, log := buildSetup(t)
	ctx := t.Context()
	before := gitOut(t, "-C", repo, "rev-parse", "HEAD")
	out, errs, code := runCLI(t, nil, "build", "demo", "002")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	var got buildStarted
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%q: %v", out, err)
	}
	if got.Path != "demo-002" || !uuidRE.MatchString(got.Thread) || got.Branch != "t3code/"+got.Thread || got.Model != "claude-opus-5-5" ||
		got.Spec != "002" || len(got.Building) != 40 {
		t.Fatalf("%+v", got)
	}
	// Two commits on main: building (thread, host) before the start, the
	// branch after it (the t3 runner names its own); both pushed.
	hist := gitOut(t, "-C", repo, "log", "--format=%s", before+"..main")
	if hist != "Dispatch 002: branch t3code/"+got.Thread+"\nDispatch 002: building on "+testHost {
		t.Fatalf("history:\n%s", hist)
	}
	if gitOut(t, "-C", repo, "rev-parse", "main") != got.Building || gitOut(t, "-C", repo, "rev-parse", "origin/main") != got.Building {
		t.Fatal("building is not main's head, pushed")
	}
	text := gitOut(t, "-C", repo, "show", "main:docs/proposals/002-second.md")
	for _, want := range []string{"status: building\n", "built_by: " + got.Thread + "\n", "host: " + testHost + "\n", "branch: t3code/" + got.Thread + "\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("spec lacks %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "status:") != 1 {
		t.Fatal(text)
	}
	// The node is a lead for the spec, with the thread the brief was started with.
	n, ok, err := mesh.GetNode(ctx, c, "demo-002")
	if err != nil || !ok || n.Role != "lead" || n.Spec != "002" || n.Runner.Thread != got.Thread || n.Model != "claude-opus-5-5" || n.Effort != "high" {
		t.Fatalf("node %+v %v %v", n, ok, err)
	}
	// The brief came from the binary, with the spec, the node and the row's facts.
	b, err := os.ReadFile(filepath.Join(os.Getenv("BMESH_CACHE_DIR"), "bmesh", "briefs", "demo-002.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Build docs/proposals/002-second.md in demo", "title: Spec 002", "The second, after 001.", "--node demo-002 started",
		"`npm test`", "`buildermesh:lead`", "pushed to `example/demo`", "`bmesh --node demo-002 land`", "sets spec 002 `done` with `merged`",
		"--- end spec -----", "second pass only after a rebase"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("brief lacks %q", want)
		}
	}
	if strings.Contains(string(b), "{{") || strings.Contains(string(b), "Rick") {
		t.Fatal("placeholder or operator name in the brief")
	}
	l, _ := os.ReadFile(log)
	if !strings.Contains(string(l), "--model claude-opus-5-5 --title [build] demo 002: Spec 002 --prompt-file ") ||
		!strings.Contains(string(l), "--thread-id "+got.Thread+" --effort high --runtime-mode auto\n") {
		t.Fatalf("log:\n%s", l)
	}
	// Main's worktrees and branches are left as they were.
	if wts := gitOut(t, "-C", repo, "worktree", "list"); strings.Count(wts, "\n") != 0 {
		t.Fatalf("worktrees left: %s", wts)
	}
	if br := gitOut(t, "-C", repo, "branch", "--list", "demo-build-*"); br != "" {
		t.Fatalf("branches left: %s", br)
	}
	// Built, the spec is no longer accepted; a second build is refused, and
	// so is the taken path with the -2 rule once it is accepted again.
	if _, errs, code := runCLI(t, nil, "build", "demo", "002"); code != 3 || !strings.Contains(errs, "spec 002 is building, not accepted") {
		t.Fatalf("again: %d %q", code, errs)
	}
	runCLI(t, nil, "spec", "set", "--repo", repo, "--id", "002", "--status", "accepted")
	meshtest.Git(t, repo, "commit", "-qam", "reset")
	meshtest.Git(t, repo, "push", "-q", "origin", "main")
	if _, err := mesh.WriteState(ctx, c, "demo-002", mesh.State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	head := gitOut(t, "-C", repo, "rev-parse", "main")
	if _, errs, code := runCLI(t, nil, "build", "demo", "002", "--family", got.Family); code != 3 || !strings.Contains(errs, "path demo-002 is taken (state working); use --path demo-002-2") {
		t.Fatalf("taken: %d %q", code, errs)
	}
	if gitOut(t, "-C", repo, "rev-parse", "main") != head {
		t.Fatal("a refused build recorded on main")
	}
	out, errs, code = runCLI(t, nil, "build", "demo", "002", "--path", "demo-002-2", "--model", "claude-sonnet-5-5", "--effort", "medium", "--family", got.Family)
	if code != 0 || !strings.Contains(out, `"path":"demo-002-2"`) || !strings.Contains(out, `"model":"claude-sonnet-5-5"`) {
		t.Fatalf("--path: %d %q %q", code, out, errs)
	}
	if n, _, _ := mesh.GetNode(ctx, c, "demo-002-2"); n.Effort != "medium" {
		t.Fatalf("node %+v", n)
	}
	if _, errs, code := runCLI(t, nil, "build", "demo", "002", "--effort", "lots"); code != 2 || !strings.Contains(errs, `effort "lots"`) {
		t.Fatalf("bad effort: %d %q", code, errs)
	}
}

func TestBuildRefusals(t *testing.T) {
	_, repo, log := buildSetup(t)
	head := gitOut(t, "-C", repo, "rev-parse", "main")
	for _, c := range []struct {
		args []string
		code int
		err  string
	}{
		{[]string{"build", "demo", "006"}, 3, "spec 006 is proposed, not accepted"},
		{[]string{"build", "demo", "005"}, 3, "spec 005 names no judge"},
		{[]string{"build", "demo", "011"}, 1, "no spec 011 on main"},
		{[]string{"build", "demo", "x"}, 2, "not a number"},
		{[]string{"build", "demo"}, 2, "usage"},
		{[]string{"build", "nope", "001"}, 2, "unknown repo"},
		{[]string{"build", t.TempDir(), "001"}, 2, "repos.toml"},
	} {
		_, errs, code := runCLI(t, nil, c.args...)
		if code != c.code || !strings.Contains(errs, c.err) {
			t.Errorf("%v: code %d err %q", c.args, code, errs)
		}
	}
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\nmode = \"companion\"\n")
	if _, errs, code := runCLI(t, nil, "build", "demo", "002"); code != 3 || !strings.Contains(errs, "demo is companion") {
		t.Fatalf("companion: %d %q", code, errs)
	}
	if gitOut(t, "-C", repo, "rev-parse", "main") != head {
		t.Fatal("a refused build recorded on main")
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatal("the runner was called")
	}
}

func TestBuildAFailedStartMovesTheSpecBack(t *testing.T) {
	c, repo, _ := buildSetup(t)
	t.Setenv("FAKE_T3_START_EXIT", "1")
	before := gitOut(t, "-C", repo, "rev-parse", "HEAD")
	_, errs, code := runCLI(t, nil, "build", "demo", "002")
	if code != 1 || !strings.Contains(errs, "runner start") || !strings.Contains(errs, "the spec reads accepted again") {
		t.Fatalf("code %d err %q", code, errs)
	}
	hist := gitOut(t, "-C", repo, "log", "--format=%s", before+"..main")
	if hist != "Dispatch 002: did not start\nDispatch 002: building on "+testHost {
		t.Fatalf("history:\n%s", hist)
	}
	text := gitOut(t, "-C", repo, "show", "main:docs/proposals/002-second.md")
	if !strings.Contains(text, "status: accepted\n") {
		t.Fatal(text)
	}
	if _, ok, _ := mesh.GetNode(t.Context(), c, "demo-002"); ok {
		t.Fatal("a node was registered")
	}
	// Then it can be built.
	t.Setenv("FAKE_T3_START_EXIT", "0")
	if _, errs, code := runCLI(t, nil, "build", "demo", "002"); code != 0 {
		t.Fatalf("after: %d %q", code, errs)
	}
}

func TestBuildAcceptedOrdersAndCapsLeads(t *testing.T) {
	c, repo, _ := buildSetup(t)
	ctx := t.Context()
	// Two leads of the family already working on demo, one done, one on
	// another repo.
	putNode(t, c, ctx, mesh.Node{Path: "demo-020", Repo: "demo", Host: "elsewhere", Family: "demo-build-1"})
	putNode(t, c, ctx, mesh.Node{Path: "demo-021", Repo: "demo", Family: "demo-build-1"})
	putNode(t, c, ctx, mesh.Node{Path: "demo-022", Repo: "demo"})
	putNode(t, c, ctx, mesh.Node{Path: "other-001", Repo: "other"})
	for _, p := range []string{"demo-020", "demo-021", "other-001"} {
		if _, err := mesh.WriteState(ctx, c, p, mesh.State{State: "working", By: "node"}); err != nil {
			t.Fatal(err)
		}
	}
	meshtest.ForceState(t, c, "demo-022", mesh.State{State: "done", By: "node"})
	out, errs, code := runCLI(t, nil, "build", "--accepted", "demo", "--family", "demo-build-1")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	want := []string{
		"verify: demo defines none",
		"started 002 Spec 002: demo-002 on claude-opus-5-5 (thread ",
		"waits 003 Spec 003: on 002 (accepted)",
		"waits 004 Spec 004: 3 leads working on demo",
		"waits 005 Spec 005: 3 leads working on demo",
		"waits 009 Spec 009: 3 leads working on demo",
		"family demo-build-1",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines:\n%s", out)
	}
	for i, w := range want {
		if !strings.HasPrefix(lines[i], w) {
			t.Errorf("line %d %q, want prefix %q", i, lines[i], w)
		}
	}
	// 002 and two leads done: 003 starts, 004 starts (001 is done, 120 s is
	// no id, 005 is above it, 007 is rejected), 005 is refused for its judge,
	// 009 starts (008, later, is not waited on).
	for _, p := range []string{"demo-002", "demo-021", "demo-020"} {
		meshtest.ForceState(t, c, p, mesh.State{State: "done", By: "node"})
	}
	runCLI(t, nil, "spec", "set", "--repo", repo, "--id", "002", "--status", "done", "--field", "merged=abc")
	meshtest.Git(t, repo, "commit", "-qam", "002 done")
	meshtest.Git(t, repo, "push", "-q", "origin", "main")
	out, errs, code = runCLI(t, map[string]string{}, "--json", "build", "--accepted", "demo", "--family", "demo-build-1")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	var got []struct{ ID, Title, State, Reason, Path, Model string }
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%q: %v", out, err)
	}
	if len(got) != 4 || got[0].ID != "003" || got[0].State != "started" || got[0].Path != "demo-003" || got[0].Model != "claude-fable-5-1" ||
		got[1].ID != "004" || got[1].State != "started" || got[1].Model != "claude-sonnet-5-5" ||
		got[2].ID != "005" || got[2].State != "not started" || !strings.Contains(got[2].Reason, "names no judge") ||
		got[3].ID != "009" || got[3].State != "started" || got[3].Path != "demo-009" {
		t.Fatalf("%+v", got)
	}
	// Nothing accepted is left but 005, which waits for a slot before its
	// judge is looked at.
	out, _, _ = runCLI(t, nil, "build", "--accepted", "demo", "--family", "demo-build-1")
	if out != "verify: demo defines none\nwaits 005 Spec 005: 3 leads working on demo\n" {
		t.Fatalf("%q", out)
	}
	if _, _, code := runCLI(t, nil, "build", "--accepted", "demo", "002"); code != 2 {
		t.Fatal("--accepted with an id")
	}
}

func TestBuildFamilies(t *testing.T) {
	c, repo, _ := buildSetup(t)
	ctx := t.Context()
	build := func(args ...string) buildStarted {
		t.Helper()
		out, errs, code := runCLI(t, nil, append([]string{"build", "demo"}, args...)...)
		if code != 0 {
			t.Fatalf("%v: %d %q %q", args, code, out, errs)
		}
		var got buildStarted
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("%q: %v", out, err)
		}
		return got
	}
	// Each invocation mints the next family; --family joins one.
	if got := build("002"); got.Family != "demo-build-1" {
		t.Fatalf("%+v", got)
	}
	if got := build("004", "--force"); got.Family != "demo-build-2" {
		t.Fatalf("%+v", got)
	}
	if got := build("009", "--family", "demo-build-1", "--force"); got.Family != "demo-build-1" {
		t.Fatalf("%+v", got)
	}
	for p, want := range map[string]string{"demo-002": "demo-build-1", "demo-004": "demo-build-2", "demo-009": "demo-build-1"} {
		if n, _, _ := mesh.GetNode(ctx, c, p); n.Family != want {
			t.Errorf("node %s family %q, want %q", p, n.Family, want)
		}
	}
	for f, want := range map[string]string{"002-second": "demo-build-1", "004-fourth": "demo-build-2", "009-ninth": "demo-build-1"} {
		if text := gitOut(t, "-C", repo, "show", "main:docs/proposals/"+f+".md"); !strings.Contains(text, "family: "+want+"\n") {
			t.Errorf("%s lacks family %s:\n%s", f, want, text)
		}
	}
	if _, errs, code := runCLI(t, nil, "build", "demo", "003", "--family", "a.b"); code != 2 || !strings.Contains(errs, "--family") {
		t.Fatalf("bad family: %d %q", code, errs)
	}
	// A family still holding feedback counts though its nodes are gone.
	if _, err := mesh.PublishFeedback(ctx, c, mesh.Feedback{From: "demo-099", Family: "demo-build-7", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"demo-002", "demo-004", "demo-009"} {
		meshtest.ForceState(t, c, p, mesh.State{State: "done", By: "node"})
	}
	runCLI(t, nil, "spec", "set", "--repo", repo, "--id", "002", "--status", "done", "--field", "merged=abc")
	meshtest.Git(t, repo, "commit", "-qam", "002 done")
	meshtest.Git(t, repo, "push", "-q", "origin", "main")
	out, errs, code := runCLI(t, nil, "build", "--accepted", "demo", "--force")
	if code != 0 || !strings.Contains(out, "started 003 Spec 003: demo-003 on claude-fable-5-1 (") ||
		!strings.Contains(out, ", family demo-build-8)\n") || !strings.HasSuffix(out, "\nfamily demo-build-8\n") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
}

func TestBuildHoldsWhileAnotherFamilyRuns(t *testing.T) {
	c, repo, log := buildSetup(t)
	ctx := t.Context()
	// Family demo-build-4 has a lead working and one done; another repo's
	// running family is no concern of demo's.
	putNode(t, c, ctx, mesh.Node{Path: "demo-030", Repo: "demo", Host: "elsewhere", Family: "demo-build-4"})
	putNode(t, c, ctx, mesh.Node{Path: "demo-031", Repo: "demo", Family: "demo-build-4"})
	putNode(t, c, ctx, mesh.Node{Path: "other-001", Repo: "other", Family: "other-build-1"})
	for _, p := range []string{"demo-030", "other-001"} {
		if _, err := mesh.WriteState(ctx, c, p, mesh.State{State: "working", By: "node"}); err != nil {
			t.Fatal(err)
		}
	}
	meshtest.ForceState(t, c, "demo-031", mesh.State{State: "done", By: "node"})
	head := gitOut(t, "-C", repo, "rev-parse", "main")
	running := "demo-build-4 is still running (demo-030 working); watch it with bmesh watch --nodes demo-030"
	if _, errs, code := runCLI(t, nil, "build", "demo", "002"); code != 3 || errs != "bmesh: held: "+running+"\n" {
		t.Fatalf("held: %d %q", code, errs)
	}
	// --accepted says it once and starts nothing.
	if out, errs, code := runCLI(t, nil, "build", "--accepted", "demo"); code != 0 || out != "held demo-build-4: still running (demo-030 working); watch it with bmesh watch --nodes demo-030\n" {
		t.Fatalf("--accepted: %d %q %q", code, out, errs)
	}
	if gitOut(t, "-C", repo, "rev-parse", "main") != head {
		t.Fatal("a held build recorded on main")
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatal("the runner was called")
	}
	// The family's own coordinator is never held by it.
	if out, errs, code := runCLI(t, nil, "build", "demo", "002", "--family", "demo-build-4"); code != 0 || !strings.Contains(out, `"family":"demo-build-4"`) {
		t.Fatalf("own family: %d %q %q", code, out, errs)
	}

	// Done, the family still holds builds while its feedback is pending; a
	// family of another repo's does not.
	for _, p := range []string{"demo-030", "demo-002"} {
		meshtest.ForceState(t, c, p, mesh.State{State: "done", By: "node"})
	}
	for _, f := range []string{"demo-build-4", "demo-build-4", "other-build-1"} {
		if _, err := mesh.PublishFeedback(ctx, c, mesh.Feedback{From: "demo-030", Family: f, Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	meshtest.ForceState(t, c, "other-001", mesh.State{State: "done", By: "node"})
	if _, errs, code := runCLI(t, nil, "build", "demo", "004"); code != 3 ||
		errs != "bmesh: held: demo-build-4 has 2 feedback pending; drain it with bmesh feedback list/drain demo-build-4\n" {
		t.Fatalf("feedback: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "feedback", "drain", "demo-build-4"); code != 0 {
		t.Fatalf("drain: %d %q", code, errs)
	}
	if out, errs, code := runCLI(t, nil, "build", "demo", "004"); code != 0 || !strings.Contains(out, `"family":"demo-build-5"`) {
		t.Fatalf("drained: %d %q %q", code, out, errs)
	}

	// --force dispatches beside a running family and says what it passed.
	out, errs, code := runCLI(t, nil, "build", "demo", "009", "--force")
	if code != 0 || !strings.Contains(out, `"path":"demo-009"`) ||
		!strings.Contains(errs, "--force: past held: demo-build-5 is still running (demo-004 idle); watch it with bmesh watch --nodes demo-004\n") {
		t.Fatalf("--force: %d %q %q", code, out, errs)
	}
}

// verifyKind gives the demo repo a verify kind that runs check.sh, which
// logs each run to $VERIFY_LOG and then does body, committed as msg and
// pushed. It returns the commit.
func verifyKind(t *testing.T, repo, body, msg string) string {
	t.Helper()
	dir := filepath.Join(repo, ".bmesh", "routines")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "verify.toml"), []byte("mode = \"inline\"\ndeterministic = true\ncommand = \"sh check.sh\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "check.sh"), []byte("echo ran >> \"$VERIFY_LOG\"\n"+body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-qm", msg)
	meshtest.Git(t, repo, "push", "-q", "origin", "main")
	return gitOut(t, "-C", repo, "rev-parse", "main")
}

func TestBuildVerifiesMainsCodeHead(t *testing.T) {
	c, repo, _ := buildSetup(t)
	ctx := t.Context()
	vlog := filepath.Join(t.TempDir(), "verify.log")
	t.Setenv("VERIFY_LOG", vlog)
	runs := func() int {
		b, _ := os.ReadFile(vlog)
		return strings.Count(string(b), "ran\n")
	}
	green := verifyKind(t, repo, "echo all good", "Add the verify kind")
	tree := gitOut(t, "-C", repo, "rev-parse", "main^{tree}")
	out, errs, code := runCLI(t, nil, "build", "demo", "002")
	if code != 0 || !strings.Contains(errs, "verify main "+green[:8]+": ok t:"+tree[:8]+" (recorded)\n") || runs() != 1 {
		t.Fatalf("green: %d %q %q runs %d", code, out, errs, runs())
	}
	var got buildStarted
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%q: %v", out, err)
	}
	if r, ok, err := mesh.GetResult(ctx, c, mesh.ResultKey("verify", tree, nil, "")); err != nil || !ok || !r.OK || r.Commit != green {
		t.Fatalf("result %+v %v %v", r, ok, err)
	}
	if wts := gitOut(t, "-C", repo, "worktree", "list"); strings.Count(wts, "\n") != 0 {
		t.Fatalf("worktrees left: %s", wts)
	}
	// The dispatch's ledger commits on top leave the code head where it was,
	// and its record answers.
	_, errs, code = runCLI(t, nil, "build", "demo", "004", "--family", got.Family)
	if code != 0 || !strings.Contains(errs, "verify main "+green[:8]+": ok t:"+tree[:8]+" (answered from the record)\n") || runs() != 1 {
		t.Fatalf("ledger on top: %d %q runs %d", code, errs, runs())
	}

	// A red main refuses with the tail of the verify's output and records
	// nothing on main.
	red := verifyKind(t, repo, "echo '--- FAIL: TestRed'\nexit 1", "Break it")
	redTree := gitOut(t, "-C", repo, "rev-parse", "main^{tree}")
	_, errs, code = runCLI(t, nil, "build", "demo", "009", "--family", got.Family)
	if code != 3 || !strings.Contains(errs, "bmesh: main is red at "+red[:8]+" Break it: verify fail t:"+redTree[:8]+" (recorded)") ||
		!strings.HasSuffix(errs, "; build its fix with --force\n--- FAIL: TestRed\n") || runs() != 2 {
		t.Fatalf("red: %d %q runs %d", code, errs, runs())
	}
	if gitOut(t, "-C", repo, "rev-parse", "main") != red {
		t.Fatal("a refused build recorded on main")
	}

	// --force dispatches past a red main and a running family both, and
	// runs nothing.
	out, errs, code = runCLI(t, nil, "build", "demo", "009", "--force")
	if code != 0 || !strings.Contains(out, `"path":"demo-009"`) || runs() != 2 ||
		!strings.Contains(errs, "--force: past held: "+got.Family+" is still running (demo-002 idle, demo-004 idle)") ||
		!strings.Contains(errs, "--force: past main is red at "+red[:8]+" Break it: verify fail t:"+redTree[:8]+" (answered from the record)\n") {
		t.Fatalf("--force: %d %q %q runs %d", code, out, errs, runs())
	}

	// Past a code head with no result, --force says so and runs nothing;
	// without it, --accepted verifies once for the batch.
	fixed := verifyKind(t, repo, "echo fixed", "Fix it")
	fixedTree := gitOut(t, "-C", repo, "rev-parse", "main^{tree}")
	out, errs, code = runCLI(t, nil, "build", "--accepted", "demo", "--force")
	if code != 0 || !strings.Contains(out, "--force: past verify main "+fixed[:8]+": no result for t:"+fixedTree[:8]+"\n") || runs() != 2 {
		t.Fatalf("--accepted --force: %d %q %q runs %d", code, out, errs, runs())
	}
	meshtest.ForceState(t, c, "demo-009", mesh.State{State: "done", By: "node"})
	out, errs, code = runCLI(t, nil, "build", "--accepted", "demo", "--family", got.Family)
	if code != 0 || strings.Count(out, "verify main ") != 2 || !strings.HasPrefix(out, "verify main "+fixed[:8]+": running at t:"+fixedTree[:8]+"\n"+
		"verify main "+fixed[:8]+": ok t:"+fixedTree[:8]+" (recorded)\nwaits 003") || runs() != 3 {
		t.Fatalf("--accepted: %d %q %q runs %d", code, out, errs, runs())
	}
}
