package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	tokenuse "github.com/rickdiculousli/buildermesh/internal/usage"
)

const usageSession = "c0ffee00-1111-4111-8111-000000000001"

// publishUsage puts what a box's usage pass would on the stream: a lead
// with a subagent, and a coordinator session in design and exec.
func publishUsage(t *testing.T, c *mesh.Client, ctx context.Context) {
	t.Helper()
	at := func(ago time.Duration) string { return time.Now().Add(-ago).UTC().Format(time.RFC3339Nano) }
	msgs := []struct {
		subj string
		m    tokenuse.Message
	}{
		{"bmusage.node.app-1", tokenuse.Message{Agent: "app-1", Role: "lead", Host: testHost, Repo: "app", Calls: []tokenuse.Call{
			{At: at(3 * time.Hour), ID: "m1", Model: "claude-opus-5-5", In: 5, CW: 20000, Out: 800, Ctx: 20005, Cause: "message"},
			{At: at(2 * time.Hour), ID: "m2", Model: "claude-opus-5-5", In: 5, CR: 20000, CW: 18800, Out: 845, Ctx: 38805, Cause: "Read cases.go"},
		}, Marks: []tokenuse.Mark{{At: at(90 * time.Minute), Kind: "compact"}}}},
		{"bmusage.node.app-1", tokenuse.Message{Agent: "app-1", Role: "lead", Host: testHost, Repo: "app", Sub: "areview1", Desc: "Review spec 001", Calls: []tokenuse.Call{
			{At: at(150 * time.Minute), ID: "s1", Model: "claude-opus-5-5", CW: 7000, Out: 90, Ctx: 7000, Sub: "areview1", Cause: "message"},
		}}},
		{"bmusage.node.app-1", tokenuse.Message{Agent: "app-1", Role: "lead", Host: testHost, Repo: "app", Calls: []tokenuse.Call{
			{At: at(60 * time.Hour), ID: "m0", CR: 999999, Ctx: 999999}, // outside the default window
			{At: at(time.Hour), ID: "m3", Model: "claude-opus-5-5", CR: 10000, Out: 100, Ctx: 10000, Cause: "compact"},
		}}},
		{"bmusage.session." + testHost + "." + usageSession, tokenuse.Message{Agent: "session." + testHost + "." + usageSession, Role: "coordinator", Host: testHost, Repo: "other", Calls: []tokenuse.Call{
			{At: at(4 * time.Hour), ID: "c1", Model: "claude-opus-5-5", CW: 3000, Out: 50, Ctx: 3000, Phase: "design", Cause: "message"},
			{At: at(4*time.Hour - time.Minute), ID: "c2", Model: "claude-opus-5-5", CR: 3000, CW: 1000, Out: 60, Ctx: 4000, Phase: "exec", Cause: "Bash: Commit the change"},
		}}},
	}
	for i, x := range msgs {
		data, _ := json.Marshal(x.m)
		if _, err := c.JS.Publish(ctx, x.subj, data); err != nil {
			t.Fatal(i, err)
		}
	}
}

func TestUsageTable(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-1", Repo: "app"})
	publishUsage(t, c, ctx)
	out, errs, code := runCLI(t, nil, "usage")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 4 {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	want := [][]string{
		{"AGENT", "ROLE", "HOST", "READ", "OUT", "SUB", "PEAK", "DESIGN", "EXEC"},
		{"app-1", "lead", testHost, "75.8k", "1.8k", "7.0k", "38.8k", "-", "-"},
		{"session." + testHost + ".c0ffee00", "coordinator", testHost, "7.0k", "110", "0", "4.0k", "43%", "57%"},
	}
	for i, w := range want {
		if got := strings.Fields(lines[i]); strings.Join(got, " ") != strings.Join(w, " ") {
			t.Errorf("line %d: %q, want %q", i, lines[i], strings.Join(w, " "))
		}
	}
	if !strings.HasPrefix(lines[3], "total 82.8k read, 1.9k out, 2 agents since ") {
		t.Errorf("total: %q", lines[3])
	}
	out, _, code = runCLI(t, nil, "usage", "--repo", "other")
	if code != 0 || strings.Contains(out, "app-1") || !strings.Contains(out, "1 agents") {
		t.Errorf("--repo: %d %q", code, out)
	}
	out, _, code = runCLI(t, nil, "usage", "--since", "3d")
	if code != 0 || !strings.Contains(out, "1.1M") {
		t.Errorf("--since 3d holds the older call: %d %q", code, out)
	}
}

func TestUsageAgentAndCalls(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-1", Repo: "app"})
	publishUsage(t, c, ctx)
	out, errs, code := runCLI(t, nil, "usage", "app-1")
	if code != 0 {
		t.Fatal(code, errs)
	}
	for _, w := range []string{
		"app-1  lead on " + testHost + ", repo app, claude-opus-5-5, ",
		"read     75.8k: input 10, cache read 30.0k, cache write 45.8k\n",
		"out      1.8k\n",
		"main     68.8k read, 1.7k out, 3 calls\n",
		"sub      7.0k read, 90 out, 1 calls in 1 subagents\n",
		"  areview1 Review spec 001: 7.0k read, 90 out, 1 calls\n",
		"context  peak 38.8k, last 10.0k, 1 compactions\n",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("no %q in\n%s", w, out)
		}
	}
	if strings.Contains(out, "design") {
		t.Errorf("a lead has no phase split:\n%s", out)
	}
	out, _, _ = runCLI(t, nil, "usage", "app-1", "--calls")
	calls := strings.Split(out[strings.Index(out, "\n\n")+2:], "\n")
	want := []string{"ctx 20.0k  +20.0k  out 800  message", "areview1  ", "ctx 38.8k  +18.8k  out 845  Read cases.go", "compact", "ctx 10.0k  -28.8k  out 100  compact"}
	for i, w := range want {
		if !strings.Contains(calls[i], w) {
			t.Errorf("call line %d: %q, want it to hold %q", i, calls[i], w)
		}
	}
	out, _, code = runCLI(t, nil, "usage", "--session", usageSession[:8])
	if code != 0 || !strings.Contains(out, "session."+testHost+".c0ffee00  coordinator on "+testHost+", repo other") ||
		!strings.Contains(out, "design   43%: 3.0k read, 50 out\n") || !strings.Contains(out, "exec     57%: 4.0k read, 60 out\n") {
		t.Errorf("--session: %d\n%s", code, out)
	}
	out, _, _ = runCLI(t, nil, "usage", "session."+testHost+"."+usageSession, "--calls")
	if !strings.Contains(out, "exec  Bash: Commit the change") {
		t.Errorf("a coordinator's call carries its phase:\n%s", out)
	}
}

func TestUsageJSON(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-1", Repo: "app"})
	publishUsage(t, c, ctx)
	out, _, code := runCLI(t, nil, "usage", "--json")
	var rep tokenuse.Report
	if err := json.Unmarshal([]byte(out), &rep); code != 0 || err != nil || len(rep.Agents) != 2 || rep.Calls != nil {
		t.Fatalf("%d %v %q", code, err, out)
	}
	if a := rep.Agents[0]; a.Agent != "app-1" || a.Main.CR != 30000 || a.Sub.CW != 7000 || a.Peak != 38805 || a.Subs[0].Desc != "Review spec 001" {
		t.Errorf("%+v", a)
	}
	if a := rep.Agents[1]; a.Design == nil || a.Design.CW != 3000 || a.Exec.CR != 3000 {
		t.Errorf("%+v", a)
	}
	out, _, _ = runCLI(t, nil, "usage", "app-1", "--calls", "--json")
	if err := json.Unmarshal([]byte(out), &rep); err != nil || len(rep.Calls) != 4 || len(rep.Marks) != 1 || rep.Calls[1].Sub != "areview1" {
		t.Errorf("%v %q", err, out)
	}
}

func TestUsageRefusals(t *testing.T) {
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-2", Repo: "app"})
	publishUsage(t, c, ctx)
	if _, errs, code := runCLI(t, nil, "usage", "nope"); code != 3 || errs != "bmesh: refused: no node nope\n" {
		t.Errorf("no node: %d %q", code, errs)
	}
	if out, _, code := runCLI(t, nil, "usage", "app-2"); code != 0 || !strings.HasPrefix(out, "no usage for app-2 since ") {
		t.Errorf("no calls: %d %q", code, out)
	}
	if _, errs, code := runCLI(t, nil, "usage", "--since", "721h"); code != 2 || !strings.Contains(errs, "exceeds 720h") {
		t.Errorf("--since: %d %q", code, errs)
	}
	if _, _, code := runCLI(t, nil, "usage", "--calls"); code != 2 {
		t.Errorf("--calls with no agent: %d", code)
	}
	cfg := filepath.Join(t.TempDir(), "down.toml")
	if err := os.WriteFile(cfg, []byte("[mesh]\nnats = \"nats://127.0.0.1:1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errs, code := runCLI(t, map[string]string{"BMESH_CONFIG": cfg}, "usage")
	if code != 1 || !strings.HasPrefix(errs, "bmesh: can't reach NATS at nats://127.0.0.1:1: ") {
		t.Errorf("no hub: %d %q", code, errs)
	}
}
