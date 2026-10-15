package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// proposeSetup is leadSetup with a real repo (a bare origin, main pushed,
// specs 012 and 013 proposed) registered as demo with surfaces, the fake
// ssh, bmesh worktrees and the cache dir under the test's temp dir. It
// returns the client, the repo path and the fake t3's log.
func proposeSetup(t *testing.T, row string) (*mesh.Client, string, string) {
	t.Helper()
	c, log := leadSetup(t)
	repo, _, _ := landedRepo(t)
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
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "specs")
	runGit(t, repo, "push", "origin", "main")
	addRunnerKeys(t, "worktrees = \""+filepath.Join(t.TempDir(), "wt")+"\"\n")
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\nremote = \"example/demo\"\nverify = [\"npm test\"]\nsurfaces = [\"phone / at 390x844\"]\n"+row)
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-ssh.sh"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMESH_SSH_COMMAND", "sh "+fake)
	t.Setenv("FAKE_SSH_LOG", filepath.Join(t.TempDir(), "ssh.log"))
	t.Setenv("FAKE_SSH_EXIT", "0")
	t.Setenv("FAKE_SSH_OUTPUT", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return c, repo, log
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

func TestProposeStartsARoundWithTwoChildren(t *testing.T) {
	c, repo, log := proposeSetup(t, "")
	ctx := t.Context()
	out, errs, code := runCLI(t, nil, "propose", "demo")
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
		!uuidRE.MatchString(got.Eng.Thread) || !uuidRE.MatchString(got.UX.Thread) || got.Eng.Thread == got.UX.Thread ||
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
			n.Runner.Kind != "t3" || !uuidRE.MatchString(n.Runner.Thread) || n.Model != "lead-model" || n.Host != testHost {
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
	l, _ := os.ReadFile(log)
	eng, _, _ := mesh.GetNode(ctx, c, "demo-round-1.eng")
	ux, _, _ := mesh.GetNode(ctx, c, "demo-round-1.ux")
	for _, want := range []string{
		"argv start --repo " + repo + " --provider claude --model lead-model --title [propose] demo round 1: engineer --prompt-file ",
		"--thread-id " + eng.Runner.Thread + " --worktree " + eng.Worktree + " --branch demo-round-1-eng",
		"--title [propose] demo round 1: UI/UX --prompt-file ",
		"--thread-id " + ux.Runner.Thread + " --worktree " + ux.Worktree + " --branch demo-round-1-ux",
	} {
		if !strings.Contains(string(l), want) {
			t.Errorf("log lacks %q:\n%s", want, l)
		}
	}
	briefs := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "bmesh", "briefs")
	engBrief, err := os.ReadFile(filepath.Join(briefs, "demo-round-1-eng.md"))
	if err != nil {
		t.Fatal(err)
	}
	uxBrief, err := os.ReadFile(filepath.Join(briefs, "demo-round-1-ux.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"number yours from 014", "`proposed_by`: `" + eng.Runner.Thread + "`", "--node demo-round-1.eng",
		"cd " + eng.Worktree + " &&", "`npm test`", "`buildermesh:propose-engineer`", "# Engineer: proposal round 1 for demo"} {
		if !strings.Contains(string(engBrief), want) {
			t.Errorf("eng brief lacks %q", want)
		}
	}
	for _, want := range []string{"number yours from 019", "`proposed_by`: `" + ux.Runner.Thread + "`", "--node demo-round-1.ux",
		"phone / at 390x844", "`buildermesh:propose-ux`"} {
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
	st, _, code := runCLI(t, nil, "status")
	if code != 0 || !strings.Contains(st, "demo-round-1  ") || !strings.Contains(st, "demo-round-1.eng") || !strings.Contains(st, "demo-round-1.ux") ||
		!strings.Contains(st, "round") || !strings.Contains(st, "engineer") {
		t.Fatalf("status:\n%s", st)
	}
	// A second round counts on from the nodes bucket.
	out, errs, code = runCLI(t, nil, "propose", "demo")
	if code != 0 || !strings.Contains(out, `"path":"demo-round-2"`) {
		t.Fatalf("second round: %d %q %q", code, out, errs)
	}
}

func TestProposeRefusesFiveProposedAndNonShip(t *testing.T) {
	_, repo, _ := proposeSetup(t, "")
	dir := filepath.Join(repo, "docs", "proposals")
	for _, id := range []string{"014", "015", "016"} {
		text := "---\nid: " + id + "\nstatus: proposed\n---\nx\n"
		if err := os.WriteFile(filepath.Join(dir, id+"-more.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "more")
	runGit(t, repo, "push", "origin", "main")
	_, errs, code := runCLI(t, nil, "propose", "demo")
	if code != 3 || !strings.Contains(errs, "5 proposed specs") || !strings.Contains(errs, "012, 013, 014, 015, 016") {
		t.Fatalf("code %d err %q", code, errs)
	}
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\nmode = \"companion\"\n")
	if _, errs, code := runCLI(t, nil, "propose", "demo"); code != 3 || !strings.Contains(errs, "companion") {
		t.Fatalf("code %d err %q", code, errs)
	}
	if _, _, code := runCLI(t, nil, "propose", "nope"); code != 2 {
		t.Fatalf("unknown repo: code %d", code)
	}
	if _, errs, code := runCLI(t, nil, "propose", repo); code != 2 || !strings.Contains(errs, "repos.toml") {
		t.Fatalf("a path without a row: %d %q", code, errs)
	}
}

func TestProposeRemoteChildGoesThroughSSH(t *testing.T) {
	c, repo, log := proposeSetup(t, "ux_host = \"other\"\n")
	ctx := t.Context()
	t.Setenv("FAKE_SSH_OUTPUT", `{"path":"demo-round-1.ux","thread":"t-remote","branch":"demo-round-1-ux","worktree":"/other/wt","host":"other"}`)
	out, errs, code := runCLI(t, nil, "propose", "demo")
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
		!strings.HasPrefix(lines[1], "ssh other bash -lc 'bmesh propose demo --child ux --round 1 --next-id 19 --thread-id ") ||
		!strings.HasSuffix(lines[1], " --model lead-model'") {
		t.Fatalf("ssh log:\n%s", l)
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "demo-round-1.eng"); !ok {
		t.Fatal("the local child was not registered")
	}
	// Only the local child ran through this box's runner.
	if tl, _ := os.ReadFile(log); strings.Count(string(tl), "argv start") != 1 {
		t.Fatalf("t3 log:\n%s", tl)
	}
	_ = repo
}

func TestProposeBlocksTheRoundWhenAChildDoesNotStart(t *testing.T) {
	c, _, _ := proposeSetup(t, "ux_host = \"other\"\n")
	ctx := t.Context()
	t.Setenv("FAKE_SSH_OUTPUT", "not json")
	_, errs, code := runCLI(t, nil, "propose", "demo")
	if code != 1 || !strings.Contains(errs, "round demo-round-1: the ux thread did not start: on other: bmesh propose printed") {
		t.Fatalf("code %d err %q", code, errs)
	}
	st, _, _ := mesh.ReadState(ctx, c, "demo-round-1")
	if st.State != "blocked" || !strings.HasPrefix(st.Text, "the ux thread did not start") {
		t.Fatalf("%+v", st)
	}
	if hist, _ := mesh.History(ctx, c, "bm.demo-round-1.blocked", time.Time{}); len(hist) != 1 {
		t.Fatalf("blocked events: %d", len(hist))
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "demo-round-1.eng"); !ok {
		t.Fatal("the eng thread that started lost its node")
	}
}

func TestProposeRefusesAnExistingWorktreePathUntouched(t *testing.T) {
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
	_, errs, code := runCLI(t, nil, "propose", "demo", "--child", "eng", "--round", "1", "--next-id", "1", "--thread-id", "5d0c7a1e-0000-4000-8000-000000000002")
	if code != 3 || !strings.Contains(errs, wt+" already exists") {
		t.Fatalf("code %d err %q", code, errs)
	}
	if b, err := os.ReadFile(filepath.Join(wt, "keep.txt")); err != nil || string(b) != "mine" {
		t.Fatalf("the existing path was touched: %v %q", err, b)
	}
}

func TestProposeRefusesAnUnreachableHostBeforeStartingAnything(t *testing.T) {
	c, repo, log := proposeSetup(t, "host = \"far\"\n")
	t.Setenv("FAKE_SSH_EXIT", "255")
	_, errs, code := runCLI(t, nil, "propose", "demo")
	if code != 3 || !strings.Contains(errs, "host far (the eng thread's) is unreachable from "+testHost) || !strings.Contains(errs, "no route") {
		t.Fatalf("code %d err %q", code, errs)
	}
	if nodes, _ := mesh.ListNodes(t.Context(), c); len(nodes) != 0 {
		t.Fatalf("nodes registered: %+v", nodes)
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatal("the runner was called")
	}
	if out := gitOut(t, "-C", repo, "worktree", "list"); strings.Count(out, "\n") != 0 {
		t.Fatalf("worktrees made: %s", out)
	}
}

func TestProposeChildForm(t *testing.T) {
	c, _, _ := proposeSetup(t, "")
	ctx := t.Context()
	thread := "5d0c7a1e-0000-4000-8000-000000000001"
	out, errs, code := runCLI(t, nil, "propose", "demo", "--child", "eng", "--round", "3", "--next-id", "7", "--thread-id", thread, "--model", "m-1")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	var got childStarted
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Path != "demo-round-3.eng" || got.Thread != thread || got.Branch != "demo-round-3-eng" {
		t.Fatalf("%q %v", out, err)
	}
	n, ok, _ := mesh.GetNode(ctx, c, "demo-round-3.eng")
	if !ok || n.Model != "m-1" || n.Parent != "demo-round-3" {
		t.Fatalf("%+v", n)
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("XDG_CACHE_HOME"), "bmesh", "briefs", "demo-round-3-eng.md"))
	if err != nil || !strings.Contains(string(b), "number yours from 007") {
		t.Fatalf("%v %s", err, b)
	}
	// The same child again is refused while its node is live and its branch exists.
	if _, errs, code := runCLI(t, nil, "propose", "demo", "--child", "eng", "--round", "3", "--next-id", "7", "--thread-id", thread); code != 3 {
		t.Fatalf("code %d err %q", code, errs)
	}
	if _, _, code := runCLI(t, nil, "propose", "demo", "--child", "both", "--round", "3", "--next-id", "7", "--thread-id", thread); code != 2 {
		t.Fatalf("code %d", code)
	}
}

func TestProposeUsesT3ProjectForTheThread(t *testing.T) {
	_, repo, log := proposeSetup(t, "")
	bm := t.TempDir()
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\n[repos.bm]\npath = \""+bm+"\"\n")
	addRunnerKeys(t, "t3_project = \"bm\"\n")
	if _, errs, code := runCLI(t, nil, "propose", "demo"); code != 0 {
		t.Fatal(code, errs)
	}
	l, _ := os.ReadFile(log)
	if !strings.Contains(string(l), "argv start --repo "+bm+" --provider") || strings.Contains(string(l), "argv start --repo "+repo+" ") {
		t.Fatalf("log:\n%s", l)
	}
}
