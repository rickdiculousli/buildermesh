package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
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
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "ledger")
	runGit(t, repo, "push", "origin", "main")
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
	b, err := os.ReadFile(filepath.Join(os.Getenv("XDG_CACHE_HOME"), "bmesh", "briefs", "demo-002.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Build docs/proposals/002-second.md in demo", "title: Spec 002", "The second, after 001.", "--node demo-002 started",
		"`npm test`", "`buildermesh:lead`", "pushed to `example/demo`", "bmesh spec set --repo . --id 002 --status done --from building",
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
		!strings.Contains(string(l), "--thread-id "+got.Thread+" --effort high\n") {
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
	runGit(t, repo, "commit", "-qam", "reset")
	runGit(t, repo, "push", "-q", "origin", "main")
	if _, err := mesh.WriteState(ctx, c, "demo-002", mesh.State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	head := gitOut(t, "-C", repo, "rev-parse", "main")
	if _, errs, code := runCLI(t, nil, "build", "demo", "002"); code != 3 || !strings.Contains(errs, "path demo-002 is taken (state working); use --path demo-002-2") {
		t.Fatalf("taken: %d %q", code, errs)
	}
	if gitOut(t, "-C", repo, "rev-parse", "main") != head {
		t.Fatal("a refused build recorded on main")
	}
	out, errs, code = runCLI(t, nil, "build", "demo", "002", "--path", "demo-002-2", "--model", "claude-sonnet-5-5", "--effort", "medium")
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
	// Two leads already working on demo, one done, one on another repo.
	putNode(t, c, ctx, mesh.Node{Path: "demo-020", Repo: "demo", Host: "elsewhere"})
	putNode(t, c, ctx, mesh.Node{Path: "demo-021", Repo: "demo"})
	putNode(t, c, ctx, mesh.Node{Path: "demo-022", Repo: "demo"})
	putNode(t, c, ctx, mesh.Node{Path: "other-001", Repo: "other"})
	for _, p := range []string{"demo-020", "demo-021", "demo-022", "other-001"} {
		if _, err := mesh.WriteState(ctx, c, p, mesh.State{State: "working", By: "node"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mesh.WriteState(ctx, c, "demo-022", mesh.State{State: "done", By: "node"}); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runCLI(t, nil, "build", "--accepted", "demo")
	if code != 0 {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	want := []string{
		"started 002 Spec 002: demo-002 on claude-opus-5-5 (thread ",
		"waits 003 Spec 003: on 002 (accepted)",
		"waits 004 Spec 004: 3 leads working on demo",
		"waits 005 Spec 005: 3 leads working on demo",
		"waits 009 Spec 009: 3 leads working on demo",
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
		if _, err := mesh.WriteState(ctx, c, p, mesh.State{State: "working", By: "node"}); err != nil {
			t.Fatal(err)
		}
		if _, err := mesh.WriteState(ctx, c, p, mesh.State{State: "done", By: "node"}); err != nil {
			t.Fatal(err)
		}
	}
	runCLI(t, nil, "spec", "set", "--repo", repo, "--id", "002", "--status", "done", "--field", "merged=abc")
	runGit(t, repo, "commit", "-qam", "002 done")
	runGit(t, repo, "push", "-q", "origin", "main")
	out, errs, code = runCLI(t, map[string]string{}, "--json", "build", "--accepted", "demo")
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
	out, _, _ = runCLI(t, nil, "build", "--accepted", "demo")
	if out != "waits 005 Spec 005: 3 leads working on demo\n" {
		t.Fatalf("%q", out)
	}
	if _, _, code := runCLI(t, nil, "build", "--accepted", "demo", "002"); code != 2 {
		t.Fatal("--accepted with an id")
	}
}
