package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp/t3fake"
)

// proposeSetup is leadSetup with a real repo (a bare origin, main pushed,
// specs 012 and 013 proposed) registered as demo with surfaces, the fake
// ssh, bmesh worktrees and the cache dir under the test's temp dir. It
// returns the client, the repo path and the fake T3, where the repo is a
// project.
func proposeSetup(t *testing.T, row string) (*mesh.Client, string, *t3fake.Server) {
	t.Helper()
	c, fake := leadSetup(t)
	repo, _, _ := landedRepo(t)
	addT3Project(fake, repo)
	dir := filepath.Join(repo, "docs", "proposals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"012", "013"} {
		text := "---\nid: " + id + "\ntitle: Thing " + id + "\nkind: feature\nsize: S\njudge: opus\nstatus: proposed\nlens: eng\n---\n\n## Opportunity\nx\n\n## Proposal\nThe change.\n"
		if err := os.WriteFile(filepath.Join(dir, id+"-thing.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "specs")
	meshtest.Git(t, repo, "push", "origin", "main")
	addRunnerKeys(t, "worktrees = \""+filepath.Join(t.TempDir(), "wt")+"\"\n")
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\nremote = \"example/demo\"\nverify = [\"npm test\"]\nsurfaces = [\"phone / at 390x844\"]\n"+row)
	ssh, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-ssh.sh"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMESH_SSH_COMMAND", "sh "+ssh)
	t.Setenv("FAKE_SSH_LOG", filepath.Join(t.TempDir(), "ssh.log"))
	t.Setenv("FAKE_SSH_EXIT", "0")
	t.Setenv("FAKE_SSH_OUTPUT", "")
	t.Setenv("BMESH_CACHE_DIR", t.TempDir())
	return c, repo, fake
}

// addRunnerKeys adds keys to the test config's [runner] table (leadSetup
// wrote it with kind = "t3").
func addRunnerKeys(t *testing.T, keys string) {
	t.Helper()
	cfg := os.Getenv("BMESH_CONFIG")
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Replace(string(b), "[runner]\nkind = \"t3\"\n", "[runner]\nkind = \"t3\"\n"+keys, 1)
	if s == string(b) {
		t.Fatal("no [runner] table to extend")
	}
	if err := os.WriteFile(cfg, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestIdeateStartsARoundWithTwoChildren(t *testing.T) {
	c, repo, fake := proposeSetup(t, "")
	ctx := t.Context()
	out, errs, code := runCLI(t, nil, "ideate", "demo")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	var got struct {
		Path    string
		Round   int
		Eng, UX childStarted
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%q: %v", out, err)
	}
	if got.Path != "demo-round-1" || got.Round != 1 || got.Eng.Path != "demo-round-1.eng" || got.UX.Path != "demo-round-1.ux" ||
		!strings.HasPrefix(got.Eng.Thread, "mcp:") || !strings.HasPrefix(got.UX.Thread, "mcp:") || got.Eng.Thread == got.UX.Thread ||
		got.Eng.Host != testHost || got.UX.Host != testHost {
		t.Fatalf("%+v", got)
	}
	round, ok, err := mesh.GetNode(ctx, c, "demo-round-1")
	if err != nil || !ok || round.Role != "round" || round.Repo != "demo" || round.RepoPath != repo || round.Host != testHost {
		t.Fatalf("round %+v %v %v", round, ok, err)
	}
	if s := stateOf(t, c, ctx, "demo-round-1"); s != "working" {
		t.Fatalf("round state %s", s)
	}
	for _, child := range []string{"eng", "ux"} {
		n, ok, err := mesh.GetNode(ctx, c, "demo-round-1."+child)
		if err != nil || !ok {
			t.Fatalf("%s: %v %v", child, ok, err)
		}
		wantBranch := "demo-round-1-" + child
		if n.Role != "engineer" || n.Parent != "demo-round-1" || n.Branch != wantBranch || n.Repo != "demo" || n.RepoPath != repo ||
			n.Runner.Kind != "t3" || !strings.HasPrefix(n.Runner.Thread, "mcp:") || !uuidRE.MatchString(n.Runner.Ref) || n.Model != "prop-model" || n.Effort != "xhigh" || n.Host != testHost {
			t.Fatalf("%s: %+v", child, n)
		}
		if filepath.Base(n.Worktree) != wantBranch || filepath.Base(filepath.Dir(n.Worktree)) != "demo" {
			t.Fatalf("%s worktree %s", child, n.Worktree)
		}
		if b, err := os.ReadFile(filepath.Join(n.Worktree, "docs", "proposals", "012-thing.md")); err != nil || !strings.Contains(string(b), "status: proposed") {
			t.Fatalf("%s worktree has no ledger: %v", child, err)
		}
		if head := gitOut(t, "-C", n.Worktree, "rev-parse", "--abbrev-ref", "HEAD"); head != wantBranch {
			t.Fatalf("%s worktree on %s", child, head)
		}
		if s := stateOf(t, c, ctx, n.Path); s != "idle" {
			t.Fatalf("%s state %s", child, s)
		}
	}
	eng, _, _ := mesh.GetNode(ctx, c, "demo-round-1.eng")
	ux, _, _ := mesh.GetNode(ctx, c, "demo-round-1.ux")
	// Both threads open in the target repo's own T3 project.
	l := launches(fake)
	if len(l) != 2 {
		t.Fatalf("launches %s", jsonOf(l))
	}
	for i, n := range []mesh.Node{eng, ux} {
		title := map[string]string{"demo-round-1.eng": "[ideate] demo round 1: engineer", "demo-round-1.ux": "[ideate] demo round 1: UI/UX"}[n.Path]
		ws := `{"branch":"` + n.Branch + `","type":"existing_worktree","worktreePath":"` + n.Worktree + `"}`
		if l[i]["projectId"] != "mcp:p2" || l[i]["title"] != title || l[i]["runtimeMode"] != "auto" || jsonOf(l[i]["workspaceStrategy"]) != ws ||
			jsonOf(l[i]["modelSelection"]) != `{"model":"prop-model","options":{"effort":"xhigh"},"provider":"claudeAgent"}` {
			t.Errorf("launch %d: %s", i, jsonOf(l[i]))
		}
	}
	briefs := filepath.Join(os.Getenv("BMESH_CACHE_DIR"), "bmesh", "briefs")
	engBrief, err := os.ReadFile(filepath.Join(briefs, "demo-round-1-eng.md"))
	if err != nil {
		t.Fatal(err)
	}
	uxBrief, err := os.ReadFile(filepath.Join(briefs, "demo-round-1-ux.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"number yours from 014", "`proposed_by`: `" + eng.Runner.Ref + "`", "--node demo-round-1.eng",
		"cd " + eng.Worktree + " &&", "`npm test`", "`buildermesh:ideate-engineer`", "# Engineer: proposal round 1 for demo"} {
		if !strings.Contains(string(engBrief), want) {
			t.Errorf("eng brief lacks %q", want)
		}
	}
	for _, want := range []string{"number yours from 019", "`proposed_by`: `" + ux.Runner.Ref + "`", "--node demo-round-1.ux",
		"phone / at 390x844", "`buildermesh:ideate-ux`"} {
		if !strings.Contains(string(uxBrief), want) {
			t.Errorf("ux brief lacks %q", want)
		}
	}
	if strings.Contains(string(engBrief), "{{") || strings.Contains(string(uxBrief), "{{") {
		t.Fatal("a placeholder was left in a brief")
	}
	// The round's started event opened it; status lists the round and its children.
	if hist, _ := mesh.History(ctx, c, "bm.demo-round-1.started", time.Time{}); len(hist) != 1 {
		t.Fatalf("round started events: %d", len(hist))
	}
	st, _, code := runCLI(t, nil, "mesh-status")
	if code != 0 || !strings.Contains(st, "demo-round-1  ") || !strings.Contains(st, "demo-round-1.eng") || !strings.Contains(st, "demo-round-1.ux") ||
		!strings.Contains(st, "round") || !strings.Contains(st, "engineer") {
		t.Fatalf("status:\n%s", st)
	}
	// A second round counts on from the nodes bucket (round 1 is still
	// open, so it takes --force).
	out, errs, code = runCLI(t, nil, "ideate", "demo", "--force")
	if code != 0 || !strings.Contains(out, `"path":"demo-round-2"`) {
		t.Fatalf("second round: %d %q %q", code, out, errs)
	}
}

func TestIdeateCarriesTheOperatorsContext(t *testing.T) {
	_, repo, _ := proposeSetup(t, "ux_host = \"other\"\n")
	t.Setenv("FAKE_SSH_OUTPUT", `{"path":"demo-round-1.ux","thread":"t-remote","branch":"demo-round-1-ux","worktree":"/other/wt","host":"other"}`)
	// A repo's own engineer template without the placeholder still gets the words.
	dir := filepath.Join(repo, ".bmesh", "briefs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "engineer.md"), []byte("# mine for {{repo}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, errs, code := runCLI(t, nil, "ideate", "demo", "--context", "  the map's flow  "); code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("BMESH_CACHE_DIR"), "bmesh", "briefs", "demo-round-1-eng.md"))
	if err != nil || !strings.HasPrefix(string(b), "# mine for demo\n\n## The operator's focus\n") || !strings.HasSuffix(string(b), "\n\nthe map's flow\n") {
		t.Fatalf("%v %q", err, b)
	}
	l, _ := os.ReadFile(os.Getenv("FAKE_SSH_LOG"))
	if !strings.Contains(string(l), " --model prop-model --effort xhigh --context ") || !strings.Contains(string(l), "the map") {
		t.Fatalf("ssh log:\n%s", l)
	}
}

func TestIdeateIdeaStartsOnlyTheEngineer(t *testing.T) {
	c, _, fake := proposeSetup(t, "ux_host = \"other\"\n")
	ctx := t.Context()
	if _, errs, code := runCLI(t, nil, "ideate", "demo", "--idea"); code != exitUsage || !strings.Contains(errs, "--context") {
		t.Fatalf("an idea with no words: %d %q", code, errs)
	}
	out, errs, code := runCLI(t, nil, "ideate", "demo", "--idea", "--context", "an automated fix lead")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	if !strings.Contains(out, `"eng":{"path":"demo-round-1.eng"`) || strings.Contains(out, `"ux"`) {
		t.Fatalf("%s", out)
	}
	// The ux host is never asked, and only one thread is launched.
	if l, _ := os.ReadFile(os.Getenv("FAKE_SSH_LOG")); len(l) != 0 {
		t.Fatalf("ssh log:\n%s", l)
	}
	if l := launches(fake); len(l) != 1 {
		t.Fatalf("launches %s", jsonOf(l))
	}
	round, ok, err := mesh.GetNode(ctx, c, "demo-round-1")
	if err != nil || !ok || !round.Idea {
		t.Fatalf("round %+v %v %v", round, ok, err)
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "demo-round-1.ux"); ok {
		t.Fatal("an idea round registered a ux node")
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("BMESH_CACHE_DIR"), "bmesh", "briefs", "demo-round-1-eng.md"))
	if err != nil || !strings.Contains(string(b), "## The operator's idea\n") || !strings.Contains(string(b), "an automated fix lead") ||
		strings.Contains(string(b), "## The operator's focus") {
		t.Fatalf("%v %q", err, b)
	}
	if _, errs, code := runCLI(t, nil, "ideate", "demo", "--idea", "--child", "ux", "--round", "2", "--next-id", "20", "--thread-id", "x"); code != exitUsage {
		t.Fatalf("an idea's ux child: %d %q", code, errs)
	}
}

func TestIdeateEffortFlag(t *testing.T) {
	_, _, fake := proposeSetup(t, "")
	if _, errs, code := runCLI(t, nil, "ideate", "demo", "--effort", "huge"); code != 2 || !strings.Contains(errs, `effort "huge"`) {
		t.Fatalf("code %d err %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "ideate", "demo", "--model", "claude-fable-5-1", "--effort", "max"); code != 0 {
		t.Fatalf("code %d err %q", code, errs)
	}
	l := launches(fake)
	for _, a := range l {
		if jsonOf(a["modelSelection"]) != `{"model":"claude-fable-5-1","options":{"effort":"max"},"provider":"claudeAgent"}` {
			t.Fatalf("launch %s", jsonOf(a))
		}
	}
	if len(l) != 2 {
		t.Fatalf("launches %s", jsonOf(l))
	}
}

func TestIdeateChildFormReadsContextFromStdin(t *testing.T) {
	proposeSetup(t, "")
	t.Setenv("BMESH_NODE", "")
	var out, errb bytes.Buffer
	code := run([]string{"ideate", "demo", "--child", "ux", "--round", "2", "--next-id", "9",
		"--thread-id", "5d0c7a1e-0000-4000-8000-000000000002", "--context", "-"}, strings.NewReader("routines first\n"), &out, &errb)
	if code != 0 {
		t.Fatalf("code %d err %q", code, errb.String())
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("BMESH_CACHE_DIR"), "bmesh", "briefs", "demo-round-2-ux.md"))
	if err != nil || !strings.Contains(string(b), "## The operator's focus") || !strings.Contains(string(b), "routines first\n\n## Closing") {
		t.Fatalf("%v %s", err, b)
	}
}

func TestIdeateRefusesFiveProposedAndNonShip(t *testing.T) {
	_, repo, _ := proposeSetup(t, "")
	dir := filepath.Join(repo, "docs", "proposals")
	for _, id := range []string{"014", "015", "016"} {
		text := "---\nid: " + id + "\nstatus: proposed\n---\nx\n"
		if err := os.WriteFile(filepath.Join(dir, id+"-more.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "more")
	meshtest.Git(t, repo, "push", "origin", "main")
	_, errs, code := runCLI(t, nil, "ideate", "demo")
	if code != 3 || !strings.Contains(errs, "5 proposed specs") || !strings.Contains(errs, "012, 013, 014, 015, 016") {
		t.Fatalf("code %d err %q", code, errs)
	}
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\nmode = \"companion\"\n")
	if _, errs, code := runCLI(t, nil, "ideate", "demo"); code != 3 || !strings.Contains(errs, "companion") {
		t.Fatalf("code %d err %q", code, errs)
	}
	if _, _, code := runCLI(t, nil, "ideate", "nope"); code != 2 {
		t.Fatalf("unknown repo: code %d", code)
	}
	if _, errs, code := runCLI(t, nil, "ideate", t.TempDir()); code != 2 || !strings.Contains(errs, "repos.toml") {
		t.Fatalf("a path without a row: %d %q", code, errs)
	}
	// A row's path is that row.
	if _, errs, code := runCLI(t, nil, "ideate", repo); code != 3 || !strings.Contains(errs, "demo is companion") {
		t.Fatalf("a row's path: %d %q", code, errs)
	}
}

func TestIdeateRemoteChildGoesThroughSSH(t *testing.T) {
	c, repo, fake := proposeSetup(t, "ux_host = \"other\"\n")
	ctx := t.Context()
	t.Setenv("FAKE_SSH_OUTPUT", `{"path":"demo-round-1.ux","thread":"t-remote","branch":"demo-round-1-ux","worktree":"/other/wt","host":"other"}`)
	out, errs, code := runCLI(t, nil, "ideate", "demo")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	var got struct{ Eng, UX childStarted }
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.UX.Host != "other" || got.UX.Thread != "t-remote" || got.Eng.Host != testHost {
		t.Fatalf("%+v", got)
	}
	l, _ := os.ReadFile(os.Getenv("FAKE_SSH_LOG"))
	lines := strings.Split(strings.TrimSpace(string(l)), "\n")
	if len(lines) != 2 || lines[0] != "ssh other bash -lc 'bmesh help >/dev/null'" ||
		!strings.HasPrefix(lines[1], "ssh other bash -lc 'bmesh ideate demo --child ux --round 1 --next-id 19 --thread-id ") ||
		!strings.HasSuffix(lines[1], " --model prop-model --effort xhigh'") {
		t.Fatalf("ssh log:\n%s", l)
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "demo-round-1.eng"); !ok {
		t.Fatal("the local child was not registered")
	}
	// Only the local child ran through this box's runner.
	if l := launches(fake); len(l) != 1 {
		t.Fatalf("launches %s", jsonOf(l))
	}
	_ = repo
}

func TestIdeateBlocksTheRoundWhenAChildDoesNotStart(t *testing.T) {
	c, _, _ := proposeSetup(t, "ux_host = \"other\"\n")
	ctx := t.Context()
	t.Setenv("FAKE_SSH_OUTPUT", "not json")
	_, errs, code := runCLI(t, nil, "ideate", "demo")
	if code != 1 || !strings.Contains(errs, "round demo-round-1: the ux thread did not start: on other: bmesh ideate printed") {
		t.Fatalf("code %d err %q", code, errs)
	}
	st, _, _ := mesh.ReadState(ctx, c, "demo-round-1")
	if st.State != "blocked" || !strings.HasPrefix(st.Text, "ux blocked: did not start: on other: bmesh ideate printed") {
		t.Fatalf("%+v", st)
	}
	if ux, _, _ := mesh.ReadState(ctx, c, "demo-round-1.ux"); ux.State != "blocked" {
		t.Fatalf("ux %+v", ux)
	}
	if hist, _ := mesh.History(ctx, c, "bm.demo-round-1.blocked", time.Time{}); len(hist) != 1 {
		t.Fatalf("blocked events: %d", len(hist))
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "demo-round-1.eng"); !ok {
		t.Fatal("the eng thread that started lost its node")
	}
}

func TestIdeateStartsTheOtherChildWhenTheFirstFails(t *testing.T) {
	c, repo, fake := proposeSetup(t, "")
	ctx := t.Context()
	// The engineer's host is unreachable only when its child starts: ssh
	// answers the reachability check and then prints no JSON.
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\nhost = \"other\"\nux_host = \""+testHost+"\"\n")
	t.Setenv("FAKE_SSH_OUTPUT", "not json")
	_, errs, code := runCLI(t, nil, "ideate", "demo")
	if code != 1 || !strings.Contains(errs, "round demo-round-1: the eng thread did not start") {
		t.Fatalf("code %d err %q", code, errs)
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "demo-round-1.ux"); !ok {
		t.Fatal("the ux thread was not started after the eng failure")
	}
	if l := launches(fake); len(l) != 1 {
		t.Fatalf("launches %s", jsonOf(l))
	}
	st, _, _ := mesh.ReadState(ctx, c, "demo-round-1")
	if st.State != "blocked" || !strings.HasPrefix(st.Text, "eng blocked: did not start") {
		t.Fatalf("%+v", st)
	}
}

func TestIdeateRefusesAnExistingWorktreePathUntouched(t *testing.T) {
	proposeSetup(t, "")
	cfgB, _ := os.ReadFile(os.Getenv("BMESH_CONFIG"))
	root := strings.TrimSuffix(strings.TrimPrefix(regexp.MustCompile(`worktrees = "[^"]*"`).FindString(string(cfgB)), `worktrees = "`), `"`)
	wt := filepath.Join(root, "demo", "demo-round-1-eng")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "keep.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errs, code := runCLI(t, nil, "ideate", "demo", "--child", "eng", "--round", "1", "--next-id", "1", "--thread-id", "5d0c7a1e-0000-4000-8000-000000000002")
	if code != 3 || !strings.Contains(errs, wt+" already exists") {
		t.Fatalf("code %d err %q", code, errs)
	}
	if b, err := os.ReadFile(filepath.Join(wt, "keep.txt")); err != nil || string(b) != "mine" {
		t.Fatalf("the existing path was touched: %v %q", err, b)
	}
}

func TestIdeateRefusesAnUnreachableHostBeforeStartingAnything(t *testing.T) {
	c, repo, fake := proposeSetup(t, "host = \"far\"\n")
	t.Setenv("FAKE_SSH_EXIT", "255")
	_, errs, code := runCLI(t, nil, "ideate", "demo")
	if code != 3 || !strings.Contains(errs, "host far (the eng thread's) is unreachable from "+testHost) || !strings.Contains(errs, "no route") {
		t.Fatalf("code %d err %q", code, errs)
	}
	if nodes, _ := mesh.ListNodes(t.Context(), c); len(nodes) != 0 {
		t.Fatalf("nodes registered: %+v", nodes)
	}
	if calls := fake.ToolCalls(); len(calls) != 0 {
		t.Fatalf("the runner was called: %v", calls)
	}
	if out := gitOut(t, "-C", repo, "worktree", "list"); strings.Count(out, "\n") != 0 {
		t.Fatalf("worktrees made: %s", out)
	}
}

func TestIdeateChildForm(t *testing.T) {
	c, _, _ := proposeSetup(t, "")
	ctx := t.Context()
	thread := "5d0c7a1e-0000-4000-8000-000000000001"
	out, errs, code := runCLI(t, nil, "ideate", "demo", "--child", "eng", "--round", "3", "--next-id", "7", "--thread-id", thread, "--model", "m-1")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	var got childStarted
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Path != "demo-round-3.eng" || !strings.HasPrefix(got.Thread, "mcp:") || got.Branch != "demo-round-3-eng" {
		t.Fatalf("%q %v", out, err)
	}
	n, ok, _ := mesh.GetNode(ctx, c, "demo-round-3.eng")
	if !ok || n.Model != "m-1" || n.Parent != "demo-round-3" || n.Runner.Thread != got.Thread || n.Runner.Ref != thread {
		t.Fatalf("%+v", n)
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("BMESH_CACHE_DIR"), "bmesh", "briefs", "demo-round-3-eng.md"))
	if err != nil || !strings.Contains(string(b), "number yours from 007") {
		t.Fatalf("%v %s", err, b)
	}
	// The same child again is refused while its node is live and its branch exists.
	if _, errs, code := runCLI(t, nil, "ideate", "demo", "--child", "eng", "--round", "3", "--next-id", "7", "--thread-id", thread); code != 3 {
		t.Fatalf("code %d err %q", code, errs)
	}
	if _, _, code := runCLI(t, nil, "ideate", "demo", "--child", "both", "--round", "3", "--next-id", "7", "--thread-id", thread); code != 2 {
		t.Fatalf("code %d", code)
	}
}

func TestIdeateRefusesWhileARoundIsOpen(t *testing.T) {
	c, _, _ := proposeSetup(t, "")
	ctx := t.Context()
	if _, errs, code := runCLI(t, nil, "ideate", "demo"); code != 0 {
		t.Fatalf("round 1: %d %q", code, errs)
	}
	if _, err := mesh.WriteState(ctx, c, "demo-round-1.eng", mesh.State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	_, errs, code := runCLI(t, nil, "ideate", "demo")
	if code != 3 || errs != "bmesh: refused: demo-round-1 is still open (eng working); wait for it, or pass --force to start another\n" {
		t.Fatalf("second round: %d %q", code, errs)
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "demo-round-2"); ok {
		t.Fatal("a refused round was registered")
	}
	if _, err := mesh.WriteState(ctx, c, "demo-round-1", mesh.State{State: "done", Text: "012 a", By: "node"}); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := runCLI(t, nil, "ideate", "demo"); code != 0 {
		t.Fatalf("after round 1 settled: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "ideate", "demo"); code != 3 || !strings.Contains(errs, "demo-round-2 is still open") {
		t.Fatalf("round 2 open: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "ideate", "demo", "--force"); code != 0 {
		t.Fatalf("--force: %d %q", code, errs)
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "demo-round-3"); !ok {
		t.Fatal("--force started no round 3")
	}
}
