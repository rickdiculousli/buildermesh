package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp"
	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp/t3fake"
)

// leadSetup is t3Setup with an auto sign-in, models for leads and
// proposers, and a repos.toml demo entry whose repo is a T3 project.
func leadSetup(t *testing.T) (*mesh.Client, *t3fake.Server) {
	t.Helper()
	c, fake := t3Setup(t)
	exp := time.Now().Add(20 * 24 * time.Hour)
	if err := t3mcp.WriteToken(tokenPath(), t3mcp.Token{Server: fake.URL, Token: fake.MintToken("auto", exp), ExpiresAt: exp, Access: "auto"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(os.Getenv("BMESH_CONFIG"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("[models]\nlead = \"lead-model\"\nproposer = \"prop-model\"\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	repo := t.TempDir()
	addT3Project(fake, repo)
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\n")
	return c, fake
}

// addT3Project registers repo as a project in the fake T3.
func addT3Project(fake *t3fake.Server, repo string) {
	fake.Do(func(s *t3fake.Server) {
		s.Projects = append(s.Projects, t3fake.Project{ID: fmt.Sprintf("mcp:p%d", len(s.Projects)+1), WorkspaceRoot: repo})
	})
}

// launches is the arguments of every t3_thread_launch so far, oldest first.
func launches(fake *t3fake.Server) []map[string]any {
	var out []map[string]any
	fake.Do(func(s *t3fake.Server) {
		for _, c := range s.Calls {
			if c.Tool == "t3_thread_launch" {
				out = append(out, c.Args)
			}
		}
	})
	return out
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestLeadStartRegistersNode(t *testing.T) {
	c, fake := leadSetup(t)
	ctx := t.Context()
	out, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	var got struct{ Path, Thread, Branch, Worktree string }
	if err := json.Unmarshal([]byte(out), &got); err != nil || strings.Count(out, "\n") != 1 {
		t.Fatalf("output %q: %v", out, err)
	}
	n, ok, err := mesh.GetNode(ctx, c, "demo-012")
	if err != nil || !ok {
		t.Fatalf("node %v %v", ok, err)
	}
	if got.Path != "demo-012" || n.Runner.Thread != got.Thread || n.Runner.Kind != "t3" || !strings.HasPrefix(got.Thread, "mcp:") ||
		!uuidRE.MatchString(n.Runner.Ref) || n.Branch != "bmesh/"+n.Runner.Ref || got.Branch != n.Branch ||
		n.Worktree != filepath.Join("/fake-t3/worktrees", filepath.Base(n.RepoPath), n.Branch) || got.Worktree != n.Worktree {
		t.Fatalf("output %+v node %+v", got, n)
	}
	if n.Role != "lead" || n.Repo != "demo" || n.Spec != "012" || n.Host != testHost || n.Model != "lead-model" || n.Effort != "high" ||
		n.StartedAt == "" || n.RepoPath == "" {
		t.Fatalf("node %+v", n)
	}
	if s := stateOf(t, c, ctx, "demo-012"); s != "idle" {
		t.Fatalf("state %s", s)
	}
	if l := launches(fake); len(l) != 1 || l[0]["title"] != "[build] demo 012" || l[0]["runtimeMode"] != "auto" ||
		jsonOf(l[0]["modelSelection"]) != `{"model":"lead-model","options":{"effort":"high"},"provider":"claudeAgent"}` {
		t.Fatalf("launches %s", jsonOf(l))
	}
}

func TestLeadStartRefusesTakenPath(t *testing.T) {
	c, fake := leadSetup(t)
	ctx := t.Context()
	if out, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null"); code != 0 {
		t.Fatalf("first: %d %q %q", code, out, errs)
	}
	if _, err := mesh.WriteState(ctx, c, "demo-012", mesh.State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	before := len(launches(fake))
	out, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null")
	if code != 3 || out != "" || errs != "bmesh: refused: path demo-012 is taken (state working); use --path demo-012-2\n" {
		t.Fatalf("second: %d %q %q", code, out, errs)
	}
	if after := len(launches(fake)); after != before {
		t.Fatalf("runner called on refusal: %d launches", after)
	}
	// --path picks a free one.
	out, errs, code = runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null",
		"--path", "demo-012-2")
	if code != 0 || !strings.Contains(out, `"path":"demo-012-2"`) {
		t.Fatalf("--path: %d %q %q", code, out, errs)
	}
	// The routine pool's paths are not for leads.
	if _, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null",
		"--path", "routine.verify"); code != 3 || !strings.Contains(errs, "routine") {
		t.Fatalf("routine path: %d %q", code, errs)
	}
	// A done path can be started again, and reads idle afterwards.
	if _, err := mesh.WriteState(ctx, c, "demo-012", mesh.State{State: "done", By: "node"}); err != nil {
		t.Fatal(err)
	}
	if out, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null"); code != 0 {
		t.Fatalf("restart done: %d %q %q", code, out, errs)
	}
	if s := stateOf(t, c, ctx, "demo-012"); s != "idle" {
		t.Fatalf("state after restart %s", s)
	}
	if _, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012"); code != 2 {
		t.Fatalf("no --brief: %d %q", code, errs)
	}
}

func TestLeadStartTimeoutNamesThread(t *testing.T) {
	c, fake := leadSetup(t)
	t.Setenv("BMESH_T3_TIMEOUT", "300ms")
	fake.Do(func(s *t3fake.Server) { s.LoseLaunch = true })
	out, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "012", "--brief", "/dev/null")
	m := regexp.MustCompile(`^bmesh: start timed out; a thread on branch (bmesh/[0-9a-f-]{36}) may exist\n$`).FindStringSubmatch(errs)
	if code != 1 || out != "" || m == nil {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	if l := launches(fake); len(l) != 1 || jsonOf(l[0]["workspaceStrategy"].(map[string]any)["branch"]) != `"`+m[1]+`"` {
		t.Fatalf("branch not the one launched: %s", jsonOf(l))
	}
	if _, ok, err := mesh.GetNode(t.Context(), c, "demo-012"); ok || err != nil {
		t.Fatalf("node registered after timeout: %v %v", ok, err)
	}
}

func TestLeadStartRefusesOverMax(t *testing.T) {
	c, fake := leadSetup(t)
	ctx := t.Context()
	cfg := os.Getenv("BMESH_CONFIG")
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(strings.Replace(string(b), "[runner]\n", "[runner]\nmax_leads = 2\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	// Another host's lead and a done lead here don't count.
	putNode(t, c, ctx, mesh.Node{Path: "elsewhere-001", Host: "otherbox"})
	putNode(t, c, ctx, mesh.Node{Path: "old-001"})
	meshtest.ForceState(t, c, "old-001", mesh.State{State: "done", By: "node"})
	for _, id := range []string{"001", "002"} {
		if out, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", id, "--brief", "/dev/null"); code != 0 {
			t.Fatalf("start %s: %d %q %q", id, code, out, errs)
		}
	}
	before := len(launches(fake))
	out, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "003", "--brief", "/dev/null")
	if code != 3 || out != "" || errs != "bmesh: 2 leads already working on this host\n" {
		t.Fatalf("third: %d %q %q", code, out, errs)
	}
	if after := len(launches(fake)); after != before {
		t.Fatalf("runner called on refusal: %d launches", after)
	}
	if _, ok, _ := mesh.GetNode(ctx, c, "demo-003"); ok {
		t.Fatal("refused lead registered")
	}
	// One finishing frees a place.
	meshtest.ForceState(t, c, "demo-001", mesh.State{State: "done", By: "node"})
	if _, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "003", "--brief", "/dev/null"); code != 0 {
		t.Fatalf("after done: %d %q", code, errs)
	}
}

// A lead started through the claude runner gets a worktree on a branch named
// for its thread, from the repo's main, and a headless claude turn whose
// session id is the thread id, with the brief on stdin.
func TestLeadStartClaudeRunner(t *testing.T) {
	c, _ := setup(t)
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-claude.sh"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMESH_CLAUDE_COMMAND", "sh "+fake)
	root := t.TempDir()
	log := filepath.Join(root, "claude.log")
	t.Setenv("FAKE_LOG", log)
	t.Setenv("BMESH_CACHE_DIR", filepath.Join(root, "cache")) // the runner's state dir
	repo := filepath.Join(root, "demo")
	meshtest.Git(t, root, "init", "-b", "main", repo)
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "one")
	brief := filepath.Join(root, "brief.md")
	os.WriteFile(brief, []byte("build it"), 0o644)
	f, err := os.OpenFile(os.Getenv("BMESH_CONFIG"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("[runner]\nkind = \"claude\"\nworktrees = \"" + filepath.Join(root, "wt") + "\"\n[models]\nlead = \"m\"\n")
	f.Close()
	writeRepos(t, "[repos.demo]\npath = \""+repo+"\"\n")
	out, errs, code := runCLI(t, nil, "lead", "start", "--repo", "demo", "--spec", "001", "--brief", brief)
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	var got struct{ Path, Thread, Branch, Worktree string }
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(root, "wt", "demo", got.Thread)
	if got.Branch != got.Thread || got.Worktree != wt || len(got.Thread) != 36 {
		t.Fatalf("started %+v", got)
	}
	n, ok, err := mesh.GetNode(t.Context(), c, "demo-001")
	if err != nil || !ok || n.Runner.Kind != "claude" || n.Runner.Thread != got.Thread || n.Worktree != wt {
		t.Fatalf("node %+v %v %v", n, ok, err)
	}
	// The fake writes argv, then stdin: wait on the stdin, the later write.
	waitFor(t, "the brief on claude's stdin", func() bool {
		in, _ := os.ReadFile(log + ".stdin")
		return string(in) == "build it"
	})
	if l, _ := os.ReadFile(log); string(l) != "-p --output-format json --session-id "+got.Thread+" --model m --effort high --dangerously-skip-permissions\n" {
		t.Fatalf("argv %q", l)
	}
}
