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

// eventOf decodes a stored event.
func eventOf(t *testing.T, data []byte) mesh.Event {
	t.Helper()
	var ev mesh.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatal(err)
	}
	return ev
}

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestIdeateStartsARoundOfOneThread(t *testing.T) {
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
	if got.Path != "demo-round-1" || got.Round != 1 || got.Eng.Path != "demo-round-1.eng" || got.UX.Path != "" ||
		!strings.HasPrefix(got.Eng.Thread, "mcp:") || got.Eng.Host != testHost || strings.Contains(out, `"ux"`) {
		t.Fatalf("%+v", got)
	}
	round, ok, err := mesh.GetNode(ctx, c, "demo-round-1")
	if err != nil || !ok || round.Role != "round" || round.Repo != "demo" || round.RepoPath != repo || round.Host != testHost ||
		strings.Join(round.Children, ",") != "eng" || round.Idea {
		t.Fatalf("round %+v %v %v", round, ok, err)
	}
	if s := stateOf(t, c, ctx, "demo-round-1"); s != "working" {
		t.Fatalf("round state %s", s)
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "demo-round-1.ux"); ok {
		t.Fatal("a same-host round registered a ux node")
	}
	n, ok, err := mesh.GetNode(ctx, c, "demo-round-1.eng")
	if err != nil || !ok {
		t.Fatalf("eng: %v %v", ok, err)
	}
	if n.Role != "engineer" || n.Parent != "demo-round-1" || n.Branch != "demo-round-1-eng" || n.Repo != "demo" || n.RepoPath != repo ||
		n.Runner.Kind != "t3" || !strings.HasPrefix(n.Runner.Thread, "mcp:") || !uuidRE.MatchString(n.Runner.Ref) || n.Model != "prop-model" || n.Effort != "xhigh" || n.Host != testHost {
		t.Fatalf("eng: %+v", n)
	}
	if filepath.Base(n.Worktree) != "demo-round-1-eng" || filepath.Base(filepath.Dir(n.Worktree)) != "demo" {
		t.Fatalf("eng worktree %s", n.Worktree)
	}
	if b, err := os.ReadFile(filepath.Join(n.Worktree, "docs", "proposals", "012-thing.md")); err != nil || !strings.Contains(string(b), "status: proposed") {
		t.Fatalf("eng worktree has no ledger: %v", err)
	}
	if head := gitOut(t, "-C", n.Worktree, "rev-parse", "--abbrev-ref", "HEAD"); head != "demo-round-1-eng" {
		t.Fatalf("eng worktree on %s", head)
	}
	if s := stateOf(t, c, ctx, n.Path); s != "idle" {
		t.Fatalf("eng state %s", s)
	}
	// One thread opens, in the target repo's own T3 project.
	l := launches(fake)
	ws := `{"branch":"` + n.Branch + `","type":"existing_worktree","worktreePath":"` + n.Worktree + `"}`
	if len(l) != 1 || l[0]["projectId"] != "mcp:p2" || l[0]["title"] != "[ideate] demo round 1: engineer" || l[0]["runtimeMode"] != "auto" ||
		jsonOf(l[0]["workspaceStrategy"]) != ws ||
		jsonOf(l[0]["modelSelection"]) != `{"model":"prop-model","options":{"effort":"xhigh"},"provider":"claudeAgent"}` {
		t.Fatalf("launches %s", jsonOf(l))
	}
	// Its brief ranks both lenses in one list and screenshots the row's
	// surfaces; its block is six ids, 014 to 019.
	b, err := os.ReadFile(filepath.Join(os.Getenv("BMESH_CACHE_DIR"), "bmesh", "briefs", "demo-round-1-eng.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Engineer: proposal round 1 for demo", "number yours from 014", "`proposed_by`: `" + n.Runner.Ref + "`",
		"--node demo-round-1.eng", "cd " + n.Worktree + " &&", "`npm test`", "`buildermesh:ideate-engineer`",
		"in its code and in how its app looks and feels", "three to six ranked ideas in one list", "this round's only thread",
		"- Surfaces to screenshot, each at its stated viewport: phone / at 390x844."} {
		if !strings.Contains(string(b), want) {
			t.Errorf("brief lacks %q", want)
		}
	}
	if strings.Contains(string(b), "{{") || strings.Contains(string(b), "a gap is expected") || strings.Contains(string(b), "ideate-ux") {
		t.Fatalf("brief:\n%s", b)
	}
	if next, err := mesh.PeekSpecID(ctx, c, "demo", 13); err != nil || next != 20 {
		t.Fatalf("peek: %d %v", next, err)
	}
	// The round's started event opened it; status lists the round and its child.
	if hist, _ := mesh.History(ctx, c, "bm.demo-round-1.started", time.Time{}); len(hist) != 1 {
		t.Fatalf("round started events: %d", len(hist))
	} else if ev := eventOf(t, hist[0].Data); ev.Text != "round 1 on demo: eng on "+testHost {
		t.Fatalf("started %+v", ev)
	}
	st, _, code := runCLI(t, nil, "mesh-status")
	if code != 0 || !strings.Contains(st, "demo-round-1  ") || !strings.Contains(st, "demo-round-1.eng") ||
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

// A row with no surfaces gets one thread whose brief never mentions them,
// whatever its ux_host says.
func TestIdeateBareRowsBriefIsAboutItsCode(t *testing.T) {
	c, repo, fake := proposeSetup(t, "")
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\nverify = [\"go test ./...\"]\nux_host = \"other\"\n")
	if out, errs, code := runCLI(t, nil, "ideate", "demo"); code != 0 || strings.Contains(out, `"ux"`) {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	if l, _ := os.ReadFile(os.Getenv("FAKE_SSH_LOG")); len(l) != 0 {
		t.Fatalf("ssh log:\n%s", l)
	}
	if l := launches(fake); len(l) != 1 {
		t.Fatalf("launches %s", jsonOf(l))
	}
	if round, _, _ := mesh.GetNode(t.Context(), c, "demo-round-1"); strings.Join(round.Children, ",") != "eng" {
		t.Fatalf("round %+v", round)
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("BMESH_CACHE_DIR"), "bmesh", "briefs", "demo-round-1-eng.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "three to six ranked ideas in one list") {
		t.Fatalf("brief:\n%s", b)
	}
	for _, bad := range []string{"urface", "UI/UX", "creenshot", "looks and feels", "{{"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("brief has %q:\n%s", bad, b)
		}
	}
}

// A row whose ux_host is another box, with surfaces, gets two threads:
// the engineer here on the engineering part, the UI/UX engineer there.
func TestIdeateStartsTwoThreadsWhenUXHostIsAnotherBox(t *testing.T) {
	c, _, fake := proposeSetup(t, "ux_host = \"other\"\n")
	t.Setenv("FAKE_SSH_OUTPUT", `{"path":"demo-round-1.ux","thread":"t-remote","branch":"demo-round-1-ux","worktree":"/other/wt","host":"other"}`)
	out, errs, code := runCLI(t, nil, "ideate", "demo")
	if code != 0 || !strings.Contains(out, `"ux":{"path":"demo-round-1.ux"`) || !strings.Contains(out, `"eng":{"path":"demo-round-1.eng"`) {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	if l := launches(fake); len(l) != 1 {
		t.Fatalf("launches %s", jsonOf(l))
	}
	ctx := t.Context()
	if round, _, _ := mesh.GetNode(ctx, c, "demo-round-1"); strings.Join(round.Children, ",") != "eng,ux" {
		t.Fatalf("round %+v", round)
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("BMESH_CACHE_DIR"), "bmesh", "briefs", "demo-round-1-eng.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Engineer: proposal round 1", "improve this repo's code and bring back three to five",
		"The UI/UX engineer runs beside you on `other`", "a gap is expected"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("eng brief lacks %q", want)
		}
	}
	if strings.Contains(string(b), "Surfaces to screenshot") {
		t.Errorf("eng brief lists surfaces:\n%s", b)
	}
	// Two blocks of five: 014 for eng, 019 for ux.
	l, _ := os.ReadFile(os.Getenv("FAKE_SSH_LOG"))
	if !strings.Contains(string(l), "--child ux --round 1 --next-id 19 ") {
		t.Fatalf("ssh log:\n%s", l)
	}
	if hist, _ := mesh.History(ctx, c, "bm.demo-round-1.started", time.Time{}); len(hist) != 1 {
		t.Fatalf("round started events: %d", len(hist))
	} else if ev := eventOf(t, hist[0].Data); ev.Text != "round 1 on demo: eng on "+testHost+", ux on other" {
		t.Fatalf("started %+v", ev)
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

// Each child's brief opens with bmesh setup in its worktree when the repo
// names a [setup].
func TestIdeateBriefsStartWithSetup(t *testing.T) {
	_, repo, _ := proposeSetup(t, "")
	commitFile(t, repo, ".bmesh/config.toml", "[setup]\ncommand = \"npm ci\"\n", "Name the setup")
	if out, errs, code := runCLI(t, nil, "ideate", "demo"); code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	for _, child := range []string{"eng"} {
		b, err := os.ReadFile(filepath.Join(os.Getenv("BMESH_CACHE_DIR"), "bmesh", "briefs", "demo-round-1-"+child+".md"))
		if err != nil {
			t.Fatal(err)
		}
		i := strings.Index(string(b), "## First: bmesh setup\n\nBefore anything else, run `cd ")
		if i < 0 || i != strings.Index(string(b), "\n## ")+1 || !strings.Contains(string(b), "demo-round-1-"+child+" && bmesh setup`.") {
			t.Fatalf("%s brief:\n%s", child, b)
		}
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
	// An idea thread claims three ids: 014 to 016.
	if next, err := mesh.PeekSpecID(ctx, c, "demo", 13); err != nil || next != 17 {
		t.Fatalf("peek: %d %v", next, err)
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("BMESH_CACHE_DIR"), "bmesh", "briefs", "demo-round-1-eng.md"))
	if err != nil || !strings.Contains(string(b), "## The operator's idea\n") || !strings.Contains(string(b), "an automated fix lead") ||
		strings.Contains(string(b), "## The operator's focus") || strings.Contains(string(b), "UI/UX engineer runs beside you") ||
		strings.Contains(string(b), "bring back three to five") || strings.Contains(string(b), "{{") {
		t.Fatalf("%v %q", err, b)
	}
	if _, errs, code := runCLI(t, nil, "ideate", "demo", "--idea", "--child", "ux", "--round", "2", "--next-id", "20", "--thread-id", "x"); code != exitUsage {
		t.Fatalf("an idea's ux child: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, nil, "ideate", "demo", "--idea", "--child", "eng", "--round", "2", "--next-id", "20", "--thread-id", "x"); code != exitUsage {
		t.Fatalf("an idea child with no words: %d %q", code, errs)
	}
}

func TestIdeateIdeaOnAnotherHostCarriesIdea(t *testing.T) {
	c, _, _ := proposeSetup(t, "host = \"other\"\n")
	t.Setenv("FAKE_SSH_OUTPUT", `{"path":"demo-round-1.eng","thread":"t-remote","branch":"demo-round-1-eng","worktree":"/other/wt","host":"other"}`)
	if out, errs, code := runCLI(t, nil, "ideate", "demo", "--idea", "--context", "one thing"); code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	l, _ := os.ReadFile(os.Getenv("FAKE_SSH_LOG"))
	if !strings.Contains(string(l), "--child eng") || !strings.Contains(string(l), "--idea") || strings.Contains(string(l), "--child ux") {
		t.Fatalf("ssh log:\n%s", l)
	}
	if round, ok, err := mesh.GetNode(t.Context(), c, "demo-round-1"); err != nil || !ok || !round.Idea {
		t.Fatalf("round %+v %v %v", round, ok, err)
	}
}

func TestIdeateIdeaBlocksWhenTheEngineerDoesNotStart(t *testing.T) {
	c, _, _ := proposeSetup(t, "host = \"other\"\n")
	t.Setenv("FAKE_SSH_OUTPUT", "not json")
	if _, errs, code := runCLI(t, nil, "ideate", "demo", "--idea", "--context", "one thing"); code != 1 {
		t.Fatalf("code %d err %q", code, errs)
	}
	st, _, err := mesh.ReadState(t.Context(), c, "demo-round-1")
	if err != nil || st.State != "blocked" || !strings.HasPrefix(st.Text, "eng blocked: did not start") {
		t.Fatalf("%+v %v", st, err)
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
	if len(l) != 1 {
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
	// The ux child runs only the UI/UX part, on the row's surfaces.
	for _, want := range []string{"# UI/UX engineer: proposal round 2 for demo", "You run only the UI/UX part of the method",
		"the UI/UX engineer of round `demo-round-2`", "- Surfaces to screenshot, each at its stated viewport: phone / at 390x844.",
		"number yours from 009", "The engineer has a block of its own", "`buildermesh:ideate-engineer`"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("ux brief lacks %q", want)
		}
	}
}

func TestIdeateRefusesFiveProposedAndNonShip(t *testing.T) {
	c, repo, _ := proposeSetup(t, "")
	ctx := t.Context()
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
	if code != 3 || errs != "bmesh: refused: demo has 5 proposed specs waiting (012, 013, 014, 015, 016); build or decline them first, or pass --force\n" {
		t.Fatalf("code %d err %q", code, errs)
	}
	// A spec being built reads proposed but isn't waiting; --force passes
	// the cap.
	putNode(t, c, ctx, mesh.Node{Path: "demo-014", Role: "lead", Repo: "demo", Spec: "014"})
	if _, err := mesh.WriteState(ctx, c, "demo-014", mesh.State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	if out, errs, code := runCLI(t, nil, "ideate", "demo"); code != 0 {
		t.Fatalf("one building: %d %q %q", code, out, errs)
	}
	meshtest.ForceState(t, c, "demo-014", mesh.State{State: "done", By: "node"})
	if out, errs, code := runCLI(t, nil, "ideate", "demo", "--force"); code != 0 {
		t.Fatalf("--force: %d %q %q", code, out, errs)
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
	// Another writer claimed 014 and 015 first, so the round's blocks
	// start past them: the engineer's at 016, the UI/UX engineer's at 021.
	if first, err := mesh.ClaimSpecIDs(ctx, c, "demo", 13, 2); err != nil || first != 14 {
		t.Fatalf("claim: %d %v", first, err)
	}
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
		!strings.HasPrefix(lines[1], "ssh other bash -lc 'bmesh ideate demo --child ux --round 1 --next-id 21 --thread-id ") ||
		!strings.HasSuffix(lines[1], " --model prop-model --effort xhigh'") {
		t.Fatalf("ssh log:\n%s", l)
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "demo-round-1.eng"); !ok {
		t.Fatal("the local child was not registered")
	}
	if b, err := os.ReadFile(filepath.Join(os.Getenv("BMESH_CACHE_DIR"), "bmesh", "briefs", "demo-round-1-eng.md")); err != nil ||
		!strings.Contains(string(b), "number yours from 016") {
		t.Fatalf("eng brief: %v\n%s", err, b)
	}
	// Both blocks are claimed: the next claim is past the UI/UX engineer's.
	if next, err := mesh.PeekSpecID(ctx, c, "demo", 13); err != nil || next != 26 {
		t.Fatalf("peek: %d %v", next, err)
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
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\nhost = \"other\"\nux_host = \""+testHost+"\"\nsurfaces = [\"phone / at 390x844\"]\n")
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
