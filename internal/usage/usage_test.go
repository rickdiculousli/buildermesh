package usage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

const (
	coordID = "c0ffee00-1111-4111-8111-000000000001"
	leadID  = "1eadbeef-2222-4222-8222-000000000002"
	namedID = "5e551011-3333-4333-8333-000000000003"
	jobID   = "4007e000-4444-4444-8444-000000000004"
)

var day = time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)

// box is a fake box: a hub, a repo with a lead's worktree, a routine
// checkout and a Claude Code config dir holding the testdata transcripts.
type box struct {
	t                         *testing.T
	ctx                       context.Context
	c                         *mesh.Client
	env                       Env
	work, wt, checkouts, root string
}

func newBox(t *testing.T) *box {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	c, err := mesh.Connect(ctx, meshtest.Start(t), "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	b := &box{t: t, ctx: ctx, c: c, work: t.TempDir(), checkouts: t.TempDir(), root: t.TempDir()}
	meshtest.Git(t, b.work, "init", "-q", "-b", "main")
	meshtest.CommitFiles(t, b.work, map[string]string{".gitignore": "cache/\n", "main.go": "package main\n"})
	os.MkdirAll(filepath.Join(b.work, "cache"), 0o755)
	os.MkdirAll(filepath.Join(b.work, "docs", "proposals"), 0o755)
	b.wt = filepath.Join(t.TempDir(), "app-1")
	meshtest.Git(t, b.work, "worktree", "add", "-q", "-b", "app-1", b.wt)
	os.MkdirAll(filepath.Join(b.checkouts, "app", "routine.review.opus-5-5-1"), 0o755)
	b.env = Env{Client: c, Host: "k16", Claude: b.root, StateDir: t.TempDir(), Checkouts: b.checkouts,
		Repos: map[string]string{"app": b.work}, Now: func() time.Time { return day.Add(12 * time.Hour) }}
	b.node(mesh.Node{Path: "app-1", Role: "lead", Repo: "app", Host: "k16", Worktree: b.wt, StartedAt: "2026-10-24T10:59:00Z"})
	b.node(mesh.Node{Path: "app-1.part", Role: "executor", Repo: "app", Host: "k16", StartedAt: "2026-10-24T10:59:30Z"})
	b.node(mesh.Node{Path: "app-2", Role: "lead", Repo: "app", Host: "k16", Session: namedID, StartedAt: "2026-10-24T08:00:00Z"})
	// Another box's node with the same worktree is never this box's.
	b.node(mesh.Node{Path: "app-3", Role: "lead", Repo: "app", Host: "mac", Worktree: b.wt, StartedAt: "2026-10-24T11:00:00Z"})
	return b
}

func (b *box) node(n mesh.Node) {
	b.t.Helper()
	if err := mesh.PutNode(b.ctx, b.c, n); err != nil {
		b.t.Fatal(err)
	}
}

// install copies testdata file name to <root>/projects/<rel>, its
// placeholders filled, written an hour before the box's now.
func (b *box) install(name, rel string) string {
	b.t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		b.t.Fatal(err)
	}
	s := strings.NewReplacer("$WORK", b.work, "$WT", b.wt, "$CHECKOUTS", b.checkouts).Replace(string(data))
	p := filepath.Join(b.root, "projects", rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		b.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		b.t.Fatal(err)
	}
	b.age(p)
	if meta, err := os.ReadFile(filepath.Join("testdata", strings.TrimSuffix(name, ".jsonl")+".meta.json")); err == nil {
		os.WriteFile(strings.TrimSuffix(p, ".jsonl")+".meta.json", meta, 0o644)
	}
	return p
}

// age dates p an hour before the box's now: a file at rest.
func (b *box) age(p string) {
	at := b.env.Now().Add(-time.Hour)
	if err := os.Chtimes(p, at, at); err != nil {
		b.t.Fatal(err)
	}
}

func (b *box) all() {
	b.install("coordinator.jsonl", "-work/"+coordID+".jsonl")
	b.install("coordinator-sub.jsonl", "-work/"+coordID+"/subagents/agent-aexplore1.jsonl")
	b.install("lead.jsonl", "-wt/"+leadID+".jsonl")
	b.install("executor-sub.jsonl", "-wt/"+leadID+"/subagents/agent-aexec1.jsonl")
	b.install("lead-sub.jsonl", "-wt/"+leadID+"/subagents/agent-areview1.jsonl")
	b.install("named.jsonl", "-gone/"+namedID+".jsonl")
	b.install("routine.jsonl", "-routine/"+jobID+".jsonl")
}

func (b *box) pass() {
	b.t.Helper()
	if err := Pass(b.ctx, b.env); err != nil {
		b.t.Fatal(err)
	}
}

func (b *box) read(agent string) Report {
	b.t.Helper()
	rep, err := Read(b.ctx, b.c, day, Filter{Agent: agent})
	if err != nil {
		b.t.Fatal(err)
	}
	return rep
}

func (b *box) msgs() uint64 {
	b.t.Helper()
	st, err := b.c.JS.Stream(b.ctx, mesh.StreamUsage)
	if err != nil {
		b.t.Fatal(err)
	}
	info, err := st.Info(b.ctx)
	if err != nil {
		b.t.Fatal(err)
	}
	return info.State.Msgs
}

func agentOf(t *testing.T, rep Report, name string) Agent {
	t.Helper()
	for _, a := range rep.Agents {
		if a.Agent == name {
			return a
		}
	}
	t.Fatalf("no agent %s in %+v", name, rep.Agents)
	return Agent{}
}

func TestEachMatchRule(t *testing.T) {
	b := newBox(t)
	b.all()
	b.pass()
	rep := b.read("")
	want := map[string]struct {
		role, repo string
		read       int64
	}{
		"session.k16." + coordID:        {Coordinator, "app", 1502 + 1703 + 1804 + 1855 + 1202 + 1302 + 1462 + 1572 + 1682 + 4003 + 4103},
		"app-1":                         {"lead", "app", 20005 + 20305 + 7003}, // its review subagent folded in
		"app-1.part":                    {"executor", "app", 9003},             // an executor's subagent file is the executor
		"app-2":                         {"lead", "app", 3002},                 // its started named the session
		"routine.review.k16-opus-5-5-1": {"routine", "app", 5002},              // a routine instance's checkout
	}
	if len(rep.Agents) != len(want) {
		t.Fatalf("agents %+v, want %d", rep.Agents, len(want))
	}
	for name, w := range want {
		a := agentOf(t, rep, name)
		if a.Role != w.role || a.Repo != w.repo || a.Read() != w.read || a.Host != "k16" {
			t.Errorf("%s: role %s repo %s read %d host %s, want %s %s %d", name, a.Role, a.Repo, a.Read(), a.Host, w.role, w.repo, w.read)
		}
	}
	lead := agentOf(t, rep, "app-1")
	if len(lead.Subs) != 1 || lead.Subs[0].ID != "areview1" || lead.Subs[0].Desc != "Review spec 001" || lead.Sub.Read() != 7003 {
		t.Errorf("app-1's subagents %+v", lead.Subs)
	}
	if lead.Design != nil || lead.Exec != nil {
		t.Errorf("a node carries no phase: %+v %+v", lead.Design, lead.Exec)
	}
	if rep.Agents[0].Agent != "app-1" { // most read first
		t.Errorf("first agent %s", rep.Agents[0].Agent)
	}
}

func TestCoordinatorCallsPhaseAndCause(t *testing.T) {
	b := newBox(t)
	b.all()
	b.pass()
	rep := b.read("session." + coordID[:8])
	type call struct{ id, sub, phase, cause string }
	want := []call{
		{"msg_a1", "", Design, "message"},               // an operator's turn starts in design
		{"msg_a2", "", Design, "Read main.go"},          // a spec's Write stays design
		{"msg_a3", "", Exec, "Write 001-x.md"},          // the call making a git commit is exec
		{"msg_a4", "", Exec, "Bash: Commit the change"}, // and the turn stays there
		{"msg_b1", "", Design, "message"},               // the next turn starts in design
		{"msg_s1", "aexplore1", Design, "message"},      // its subagent takes the phase it started in
		{"msg_s2", "aexplore1", Design, "Bash"},         // a Bash call with no description: never its command
		{"msg_b2", "", Exec, "Agent Explore the code"},  // an acting bmesh command moves it
		{"msg_c1", "", Exec, "wake"},                    // a background task's notice starts in exec
		{"msg_d1", "", Design, "message"},               // bmesh mesh-status and a gitignored file's Edit change nothing
		{"msg_d2", "", Design, "Bash: Show the mesh +1"},
	}
	if len(rep.Calls) != len(want) {
		t.Fatalf("calls %+v", rep.Calls)
	}
	for i, w := range want {
		c := rep.Calls[i]
		if c.ID != w.id || c.Sub != w.sub || c.Phase != w.phase || c.Cause != w.cause {
			t.Errorf("call %d: %s sub %q %s %q, want %+v", i, c.ID, c.Sub, c.Phase, c.Cause, w)
		}
		if strings.Contains(c.Cause, "SECRET") || strings.Contains(c.Cause, "secret") {
			t.Errorf("call %d's cause holds a command's text: %q", i, c.Cause)
		}
	}
	if c := rep.Calls[0]; c.In != 2 || c.CR != 1000 || c.CW != 500 || c.Out != 10 || c.Ctx != 1502 || c.Model != "claude-opus-5-5" {
		t.Errorf("first call %+v", c)
	}
	a := rep.Agents[0]
	if a.Calls != 11 || a.Peak != 1855 || a.LastCtx != 1682 || a.Compactions != 1 || len(rep.Marks) != 1 || rep.Marks[0].At != "2026-10-24T10:10:00.000Z" {
		t.Errorf("agent %+v marks %+v", a, rep.Marks)
	}
	if a.Design.Read() != 1502+1703+1202+1572+1682+4003+4103 || a.Exec.Read() != 1804+1855+1302+1462 {
		t.Errorf("design %d exec %d", a.Design.Read(), a.Exec.Read())
	}
}

func TestRestartPublishesNothingTwice(t *testing.T) {
	b := newBox(t)
	b.all()
	b.pass()
	n := b.msgs()
	b.pass()
	if got := b.msgs(); got != n {
		t.Fatalf("a second pass over nothing new published %d messages", got-n)
	}
	// A crash between the ack and the cursor's write: the pass runs again
	// from the old cursor, and the stream drops what it holds.
	cur := filepath.Join(b.env.StateDir, "usage", "cursor.json")
	old, _ := os.ReadFile(cur)
	p := filepath.Join(b.root, "projects", "-gone", namedID+".jsonl")
	appendLines(t, p, `{"type":"assistant","isSidechain":false,"uuid":"u-x1","timestamp":"2026-10-24T09:10:00.000Z","message":{"model":"claude-opus-5-5","id":"msg_o2","content":[],"usage":{"input_tokens":1,"cache_read_input_tokens":3000,"cache_creation_input_tokens":0,"output_tokens":4}}}`+"\n")
	b.age(p)
	b.pass()
	n = b.msgs()
	os.WriteFile(cur, old, 0o644)
	b.pass()
	if got := b.msgs(); got != n {
		t.Errorf("a retry from the old cursor stored %d more messages", got-n)
	}
	if a := agentOf(t, b.read("app-2"), "app-2"); a.Calls != 2 || a.Read() != 3002+3001 {
		t.Errorf("app-2 after the retry: %d calls, %d read", a.Calls, a.Read())
	}
}

func appendLines(t *testing.T, p string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		f.WriteString(l)
	}
}

func TestHalfWrittenLineWaits(t *testing.T) {
	b := newBox(t)
	p := b.install("named.jsonl", "-gone/"+namedID+".jsonl")
	line := `{"type":"assistant","isSidechain":false,"uuid":"u-x2","timestamp":"2026-10-24T09:20:00.000Z","message":{"model":"claude-opus-5-5","id":"msg_o3","content":[],"usage":{"input_tokens":1,"cache_read_input_tokens":4000,"cache_creation_input_tokens":0,"output_tokens":4}}}`
	appendLines(t, p, line[:40])
	b.age(p)
	b.pass()
	if a := agentOf(t, b.read("app-2"), "app-2"); a.Calls != 1 {
		t.Fatalf("a half-written line was read: %d calls", a.Calls)
	}
	appendLines(t, p, line[40:]+"\n")
	b.age(p)
	b.pass()
	if a := agentOf(t, b.read("app-2"), "app-2"); a.Calls != 2 || a.Read() != 3002+4001 {
		t.Errorf("after the line was finished: %d calls, %d read", a.Calls, a.Read())
	}
}

func TestNewestCallWaitsWhileItsFileIsFresh(t *testing.T) {
	b := newBox(t)
	p := b.install("named.jsonl", "-gone/"+namedID+".jsonl")
	now := b.env.Now()
	os.Chtimes(p, now, now)
	b.pass()
	if rep := b.read("app-2"); len(rep.Agents) != 0 {
		t.Fatalf("the newest call of a file written just now was read: %+v", rep.Agents)
	}
	b.age(p)
	b.pass()
	if a := agentOf(t, b.read("app-2"), "app-2"); a.Calls != 1 {
		t.Errorf("once the file was quiet: %d calls", a.Calls)
	}
}

func TestOneMessageIDOneCall(t *testing.T) {
	// msg_a1 and msg_b2 are written as two lines each, one per block.
	b := newBox(t)
	b.install("coordinator.jsonl", "-work/"+coordID+".jsonl")
	b.pass()
	ids := map[string]int{}
	for _, c := range b.read("session." + coordID).Calls {
		ids[c.ID]++
	}
	if ids["msg_a1"] != 1 || ids["msg_b2"] != 1 || len(ids) != 9 {
		t.Errorf("calls by id %v", ids)
	}
}

func TestReadCountsADuplicatedMessageOnce(t *testing.T) {
	b := newBox(t)
	m := Message{Agent: "app-2", Role: "lead", Host: "k16", Repo: "app", File: "-gone/x.jsonl",
		Calls: []Call{{At: "2026-10-24T09:00:02.000Z", ID: "msg_o1", CR: 100, Ctx: 100}}}
	data, _ := json.Marshal(m)
	for _, id := range []string{"k16/-gone/x.jsonl/10", "k16/-gone/x.jsonl/10-again"} {
		if _, err := b.c.JS.Publish(b.ctx, "bmusage.node.app-2", data, jetstream.WithMsgID(id)); err != nil {
			t.Fatal(err)
		}
	}
	if a := agentOf(t, b.read(""), "app-2"); a.Calls != 1 || a.Read() != 100 {
		t.Errorf("%d calls, %d read", a.Calls, a.Read())
	}
}

func TestWorktreeFileWaitsForItsNode(t *testing.T) {
	b := newBox(t)
	wt2 := filepath.Join(t.TempDir(), "app-4")
	meshtest.Git(t, b.work, "worktree", "add", "-q", "-b", "app-4", wt2)
	b.wt = wt2
	b.install("lead.jsonl", "-wt2/"+leadID+".jsonl")
	b.env.Now = func() time.Time { return day.Add(11*time.Hour + 2*time.Minute) } // two minutes past its first line
	b.age(filepath.Join(b.root, "projects", "-wt2", leadID+".jsonl"))
	b.pass()
	if rep := b.read(""); len(rep.Agents) != 0 {
		t.Fatalf("a worktree's file was read before its node: %+v", rep.Agents)
	}
	b.node(mesh.Node{Path: "app-4", Role: "lead", Repo: "app", Host: "k16", Worktree: wt2, StartedAt: "2026-10-24T10:58:00Z"})
	b.pass()
	if a := agentOf(t, b.read(""), "app-4"); a.Calls != 2 {
		t.Errorf("app-4: %d calls", a.Calls)
	}
}

func TestUnnamedWorktreeFileIsASessionAfterTenMinutes(t *testing.T) {
	b := newBox(t)
	wt2 := filepath.Join(t.TempDir(), "scratch")
	meshtest.Git(t, b.work, "worktree", "add", "-q", "-b", "scratch", wt2)
	b.wt = wt2
	b.install("lead.jsonl", "-wt2/"+leadID+".jsonl")
	b.pass() // now is noon: an hour past its first line
	a := agentOf(t, b.read(""), "session.k16."+leadID)
	if a.Role != Coordinator || a.Repo != "app" || a.Design == nil {
		t.Errorf("%+v", a)
	}
}

func TestNodesUnreadDecideNothing(t *testing.T) {
	b := newBox(t)
	b.all()
	if err := b.c.JS.DeleteKeyValue(b.ctx, mesh.BucketNodes); err != nil {
		t.Fatal(err)
	}
	if err := Pass(b.ctx, b.env); err == nil {
		t.Fatal("a pass that can't read the nodes went on")
	}
	if b.msgs() != 0 {
		t.Fatal("a pass that can't read the nodes published")
	}
	if err := mesh.Provision(b.ctx, b.c, 0); err != nil {
		t.Fatal(err)
	}
	b.node(mesh.Node{Path: "app-1", Role: "lead", Repo: "app", Host: "k16", Worktree: b.wt, StartedAt: "2026-10-24T10:59:00Z"})
	b.node(mesh.Node{Path: "app-1.part", Role: "executor", Repo: "app", Host: "k16", StartedAt: "2026-10-24T10:59:30Z"})
	b.pass()
	if a := agentOf(t, b.read(""), "app-1"); a.Role != "lead" || a.Calls != 3 {
		t.Errorf("once the nodes were read: %+v", a)
	}
}

// longSession is a coordinator's turn of n calls, each reading the file
// the next call's cause names.
func longSession(n int) string {
	var s strings.Builder
	s.WriteString(`{"type":"user","isSidechain":false,"uuid":"u0","timestamp":"2026-10-24T10:00:00.000Z","cwd":"/gone","message":{"role":"user","content":"read them all"}}` + "\n")
	for i := 1; i <= n; i++ {
		ts := day.Add(10*time.Hour + time.Duration(i)*time.Second).Format("2006-01-02T15:04:05.000Z")
		s.WriteString(`{"type":"assistant","isSidechain":false,"uuid":"a` + strconv.Itoa(i) + `","timestamp":"` + ts + `","message":{"model":"claude-opus-5-5","id":"msg_` + strconv.Itoa(i) +
			`","content":[{"type":"tool_use","id":"t` + strconv.Itoa(i) + `","name":"Read","input":{"file_path":"/gone/f` + strconv.Itoa(i) + `.go"}}],"usage":{"input_tokens":1,"cache_read_input_tokens":100,"cache_creation_input_tokens":0,"output_tokens":1}}}` + "\n")
		s.WriteString(`{"type":"user","isSidechain":false,"uuid":"r` + strconv.Itoa(i) + `","timestamp":"` + ts + `","message":{"role":"user","content":[{"tool_use_id":"t` + strconv.Itoa(i) + `","type":"tool_result","content":"x"}]}}` + "\n")
	}
	return s.String()
}

func TestCallsSplitAcrossMessagesAndPasses(t *testing.T) {
	defer func(c int, p int64) { maxCalls, passBytes = c, p }(maxCalls, passBytes)
	maxCalls = 2
	for _, budget := range []int64{64 << 20, 300} { // one pass; a pass per line, each longer than the budget
		passBytes = budget
		b := newBox(t)
		p := filepath.Join(b.root, "projects", "-gone", coordID+".jsonl")
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(longSession(5)), 0o644)
		b.age(p)
		for i := 0; i < 20; i++ {
			b.pass()
		}
		if budget > 300 && b.msgs() != 3 {
			t.Errorf("budget %d: %d messages for 5 calls at 2 a message", budget, b.msgs())
		}
		calls := b.read("session." + coordID).Calls
		if len(calls) != 5 {
			t.Fatalf("budget %d: %d calls", budget, len(calls))
		}
		for i, c := range calls {
			cause := "message"
			if i > 0 {
				cause = "Read f" + strconv.Itoa(i) + ".go"
			}
			if c.ID != "msg_"+strconv.Itoa(i+1) || c.Cause != cause || c.Phase != Design {
				t.Errorf("budget %d, call %d: %+v, want cause %q", budget, i, c, cause)
			}
		}
	}
}
