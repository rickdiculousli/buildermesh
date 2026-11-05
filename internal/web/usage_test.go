package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/usage"
)

const coordID = "c0ffee00-1111-4111-8111-000000000001"

var usageNow = time.Date(2026, 10, 24, 12, 10, 0, 0, time.UTC)

// at is 10-24 at hh:mm, or a day offset from it.
func at(day, h, m int) time.Time { return time.Date(2026, 10, 24+day, h, m, 0, 0, time.UTC) }

func call(t time.Time, ctx int64, phase, cause string) usage.Call {
	return usage.Call{At: t.Format(time.RFC3339Nano), ID: fmt.Sprintf("m%d", t.UnixNano()), CR: ctx, Ctx: ctx, Out: ctx / 100, Phase: phase, Cause: cause}
}

// publishUsage puts m on bm-usage where the daemon's pass would.
func publishUsage(t *testing.T, ctx context.Context, c *mesh.Client, m usage.Message) {
	t.Helper()
	subj := "bmusage.node." + m.Agent
	if rest, ok := strings.CutPrefix(m.Agent, "session."); ok {
		subj = "bmusage.session." + rest
	}
	data, _ := json.Marshal(m)
	if _, err := c.JS.Publish(ctx, subj, data, jetstream.WithExpectStream(mesh.StreamUsage)); err != nil {
		t.Fatal(err)
	}
}

// usageMesh is a hub with a coordinator session, a lead with a subagent and
// a compaction, its executor, a routine and a done lead of last week, the
// lead's events, and a server reading them at usageNow.
func usageMesh(t *testing.T) (*httptest.Server, context.Context, *mesh.Client) {
	t.Helper()
	ctx, c := connect(t)
	repo := filepath.Join(t.TempDir(), "buildermesh")
	meshtest.Git(t, filepath.Dir(repo), "init", "-q", "-b", "main", "buildermesh")
	meshtest.CommitFiles(t, repo, map[string]string{
		"docs/proposals/096-cases.md": "---\nid: 96\ntitle: Cases\nstatus: accepted\nsize: M\n---\nbody\n",
		"docs/proposals/098-other.md": "---\nid: 98\ntitle: Other\nstatus: done\nsize: S\n---\nbody\n",
	})
	for _, n := range []mesh.Node{
		{Path: "buildermesh-096", Role: "lead", Repo: "buildermesh", Spec: "096", Host: "k16"},
		{Path: "buildermesh-096.api", Role: "executor", Parent: "buildermesh-096", Repo: "buildermesh", Host: "k16"},
		{Path: "buildermesh-098", Role: "lead", Repo: "buildermesh", Spec: "098", Host: "k16"},
	} {
		if err := mesh.PutNode(ctx, c, n); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mesh.WriteState(ctx, c, "buildermesh-096", mesh.State{State: "working", At: at(0, 11, 0).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	coord := usage.SessionAgent("k16", coordID)
	for _, m := range []usage.Message{
		{Agent: coord, Role: usage.Coordinator, Host: "k16", Repo: "buildermesh", Session: coordID, Calls: []usage.Call{
			call(at(0, 10, 0), 1000, "design", ""), call(at(0, 10, 2), 2000, "design", ""), call(at(0, 10, 3), 3000, "exec", ""),
			call(at(0, 10, 20), 4000, "exec", ""), call(at(0, 10, 25), 5000, "exec", ""), call(at(0, 10, 29), 6000, "exec", "")}},
		{Agent: coord, Role: usage.Coordinator, Host: "k16", Repo: "buildermesh", Session: coordID, Sub: "s1", Desc: "survey",
			Calls: []usage.Call{call(at(0, 10, 1), 500, "design", "")}},
		{Agent: "buildermesh-096", Role: "lead", Host: "k16", Repo: "buildermesh", Calls: []usage.Call{
			call(at(0, 11, 0), 100000, "", "message"), call(at(0, 11, 10), 150000, "", "Read cases.go"), call(at(0, 11, 50), 120000, "", "compact")},
			Marks: []usage.Mark{{At: at(0, 11, 30).Format(time.RFC3339Nano), Kind: "compact"}}},
		{Agent: "buildermesh-096", Role: "lead", Host: "k16", Repo: "buildermesh", Sub: "x", Desc: "find",
			Calls: []usage.Call{call(at(0, 11, 5), 10000, "", "")}},
		{Agent: "buildermesh-096.api", Role: "executor", Host: "k16", Repo: "buildermesh", Calls: []usage.Call{call(at(0, 11, 40), 7000, "", "")}},
		{Agent: "routine.verify.k16-1", Role: "routine", Host: "k16", Repo: "buildermesh", Calls: []usage.Call{call(at(0, 11, 45), 9, "", "")}},
		{Agent: "buildermesh-098", Role: "lead", Host: "k16", Repo: "buildermesh", Calls: []usage.Call{
			call(at(-4, 9, 0), 50000, "", ""), call(at(-4, 9, 30), 80000, "", "")}},
	} {
		publishUsage(t, ctx, c, m)
	}
	for _, ev := range []mesh.Event{
		{From: "buildermesh-096", Event: "started", At: at(0, 10, 59).Format(time.RFC3339)},
		{From: "buildermesh-096", Event: "progress", Text: "cases read", At: at(0, 11, 15).Format(time.RFC3339)},
		{From: "daemon.k16", Event: "land-refused", To: "buildermesh-096", Text: "verify failed", At: at(0, 11, 55).Format(time.RFC3339)},
		{From: "buildermesh-096", Event: "ready", Text: "again", At: at(0, 12, 0).Format(time.RFC3339)},
	} {
		if err := mesh.PublishEvent(ctx, c, ev); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Repos: map[string]config.Repo{"buildermesh": {Path: repo}}, Spec: config.SpecConfig{Glob: "docs/proposals/{id}-*.md"},
		Context: config.ContextConfig{NudgeFrom: 250000, CompactFrom: 300000, CompactSoon: 400000}}
	srv := httptest.NewServer(Handler(c, time.Hour, Options{Cfg: cfg, Host: "k16", Now: func() time.Time { return usageNow }}))
	t.Cleanup(srv.Close)
	return srv, ctx, c
}

func TestUsageRoleBucketsPerWindow(t *testing.T) {
	srv, _, _ := usageMesh(t)
	type bucket struct {
		series string
		i      int
		want   int64
	}
	for _, tc := range []struct {
		since      string
		step, from int64
		n          int
		read       int64
		buckets    []bucket
	}{
		{"1h", 60, at(0, 11, 10).Unix(), 60, 150000 + 120000 + 7000 + 9,
			[]bucket{{"lead", 0, 150000}, {"lead", 40, 120000}, {"sub", 30, 7000}, {"routine", 35, 9}, {"sub", 0, 0}}},
		{"6h", 600, at(0, 6, 10).Unix(), 36, 0,
			[]bucket{{"design", 23, 3500}, {"exec", 23, 3000}, {"exec", 25, 15000}, {"lead", 29, 100000}, {"sub", 29, 10000}}},
		{"24h", 1800, at(-1, 12, 0).Unix(), 49, 0,
			[]bucket{{"design", 44, 3500}, {"exec", 44, 18000}, {"lead", 46, 250000}, {"lead", 47, 120000}}},
		{"7d", 10800, at(-7, 12, 0).Unix(), 57, 0,
			[]bucket{{"lead", 23, 130000}, {"design", 55, 3500}, {"exec", 55, 18000}}},
	} {
		var v UsageView
		getJSON(t, srv, "/api/usage?since="+tc.since, 200, &v)
		if v.Roles.Step != tc.step || v.Roles.From != tc.from || v.To != usageNow.Unix() || len(v.Roles.Series) != 5 {
			t.Fatalf("%s: roles step %d from %d to %d", tc.since, v.Roles.Step, v.Roles.From, v.To)
		}
		for name, s := range v.Roles.Series {
			if len(s) != tc.n {
				t.Fatalf("%s: %s has %d buckets, want %d", tc.since, name, len(s), tc.n)
			}
		}
		for _, b := range tc.buckets {
			if got := v.Roles.Series[b.series][b.i]; got != b.want {
				t.Errorf("%s: %s[%d] = %d, want %d", tc.since, b.series, b.i, got, b.want)
			}
		}
		if tc.read != 0 && (v.Read != tc.read || v.Hour != tc.read) {
			t.Errorf("%s: read %d hour %d, want %d", tc.since, v.Read, v.Hour, tc.read)
		}
	}
}

func TestUsageAgentsAndSessions(t *testing.T) {
	srv, _, _ := usageMesh(t)
	var v UsageView
	getJSON(t, srv, "/api/usage", 200, &v) // 6h
	var order []string
	for _, a := range v.Agents {
		order = append(order, a.Agent)
	}
	coord := usage.SessionAgent("k16", coordID)
	if want := []string{"buildermesh-096", coord, "buildermesh-096.api", "routine.verify.k16-1"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("agents %v, want %v", order, want)
	}
	lead, sess, exe := v.Agents[0], v.Agents[1], v.Agents[2]
	if lead.Role != "lead" || lead.Size != "M" || lead.State != "working" || !lead.Live || lead.Read != 380000 ||
		lead.Main.CR != 370000 || lead.Sub.CR != 10000 || lead.Peak != 150000 || lead.LastCtx != 120000 ||
		lead.Calls != 4 || lead.Compactions != 1 || lead.First != at(0, 11, 0).Unix() || lead.Design != nil {
		t.Errorf("lead row %+v", lead)
	}
	if exe.State != "idle" || exe.Live || exe.Size != "" {
		t.Errorf("executor row %+v", exe)
	}
	if sess.Role != usage.Coordinator || sess.State != "" || sess.Live || sess.Design == nil || *sess.Design != 3500 || *sess.Exec != 18000 {
		t.Errorf("session row %+v", sess)
	}
	if v.Context != (Thresholds{250000, 300000, 400000}) || len(v.Quota) != 0 {
		t.Errorf("context %+v quota %+v", v.Context, v.Quota)
	}
	if len(v.Sessions) != 1 {
		t.Fatalf("sessions %+v", v.Sessions)
	}
	s := v.Sessions[0]
	// Split at the phase change at 10:03, broken at the 17-minute gap and at
	// the gap of exactly 5 minutes; 10:29 is 4 minutes on.
	want := []Seg{{at(0, 10, 0).Unix(), at(0, 10, 3).Unix(), "design"}, {at(0, 10, 3).Unix(), at(0, 10, 3).Unix(), "exec"},
		{at(0, 10, 20).Unix(), at(0, 10, 20).Unix(), "exec"}, {at(0, 10, 25).Unix(), at(0, 10, 29).Unix(), "exec"}}
	if s.Agent != coord || s.Host != "k16" || s.ID != coordID || s.Repo != "buildermesh" || !reflect.DeepEqual(s.Spans, want) ||
		s.Design != 3500 || s.Exec != 18000 || s.Peak != 6000 || s.First != at(0, 10, 0).Unix() || s.Last != at(0, 10, 29).Unix() {
		t.Errorf("session %+v", s)
	}
}

// UnmarshalJSON reads a span back from [from, to, phase].
func (s *Seg) UnmarshalJSON(b []byte) error {
	var a [3]any
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	f, _ := a[0].(float64)
	to, _ := a[1].(float64)
	st, _ := a[2].(string)
	*s = Seg{int64(f), int64(to), st}
	return nil
}

func TestUsageAgentNodeWithVs(t *testing.T) {
	srv, _, _ := usageMesh(t)
	var v AgentView
	getJSON(t, srv, "/api/usage/agent?agent=buildermesh-096&vs=buildermesh-098", 200, &v)
	if v.Agent.Agent != "buildermesh-096" || v.Agent.Size != "M" || v.Spans != nil || v.Context.CompactSoon != 400000 {
		t.Fatalf("agent %+v spans %v", v.Agent, v.Spans)
	}
	var ms []float64
	for _, c := range v.Calls {
		ms = append(ms, c.M)
	}
	if !reflect.DeepEqual(ms, []float64{0, 5, 10, 50}) || v.Calls[1].Sub != "x" || v.Calls[2].Cause != "Read cases.go" || v.Calls[2].Ctx != 150000 {
		t.Errorf("calls %+v", v.Calls)
	}
	if !reflect.DeepEqual(v.Marks, []AgentMark{{At: at(0, 11, 30).Unix()}}) {
		t.Errorf("marks %+v", v.Marks)
	}
	wantEv := []AgentEvent{{at(0, 11, 15).Unix(), "progress", "cases read"}, {at(0, 11, 55).Unix(), "refused", "verify failed"},
		{at(0, 12, 0).Unix(), "ready", "again"}}
	if !reflect.DeepEqual(v.Events, wantEv) {
		t.Errorf("events %+v", v.Events)
	}
	if !reflect.DeepEqual(v.Subs, []AgentSub{{ID: "x", Desc: "find", Calls: 1, Read: 10000, Out: 100}}) {
		t.Errorf("subs %+v", v.Subs)
	}
	if v.Vs == nil || v.Vs.Agent.Agent != "buildermesh-098" || v.Vs.Agent.Size != "S" || len(v.Vs.Calls) != 2 ||
		v.Vs.Calls[0].M != 0 || v.Vs.Calls[1].M != 30 || len(v.Vs.Events) != 0 || len(v.Vs.Marks) != 0 {
		t.Errorf("vs %+v", v.Vs)
	}
	// A vs with no calls is left out; a window that misses 098 is one.
	body := getJSON(t, srv, "/api/usage/agent?agent=buildermesh-096&since=24h&vs=buildermesh-098", 200, &v)
	if strings.Contains(body, `"vs"`) || len(v.Calls) != 4 {
		t.Errorf("vs outside the window: %s", body)
	}
}

func TestUsageAgentSession(t *testing.T) {
	srv, _, _ := usageMesh(t)
	var v AgentView
	body := getJSON(t, srv, "/api/usage/agent?agent="+usage.SessionAgent("k16", coordID)+"&since=6h", 200, &v)
	if len(v.Calls) != 7 || len(v.Spans) != 4 || v.Spans[0] != (Seg{at(0, 10, 0).Unix(), at(0, 10, 3).Unix(), "design"}) ||
		!strings.Contains(body, `"events":[]`) {
		t.Fatalf("session view %s", body)
	}
	if !reflect.DeepEqual(v.Subs, []AgentSub{{ID: "s1", Desc: "survey", Phase: "design", Calls: 1, Read: 500, Out: 5}}) {
		t.Errorf("subs %+v", v.Subs)
	}
}

func TestUsageRefusals(t *testing.T) {
	srv, _, _ := usageMesh(t)
	getJSON(t, srv, "/api/usage?since=2h", 400, nil)
	getJSON(t, srv, "/api/usage/agent?agent=buildermesh-096&since=2h", 400, nil)
	getJSON(t, srv, "/api/usage/agent", 400, nil)
	if body := getJSON(t, srv, "/api/usage/agent?agent=nobody&since=1h", 404, nil); !strings.Contains(body, "no usage for nobody since 2026-10-24T11:10:00Z") {
		t.Errorf("404 body %q", body)
	}
}

func TestUsageQuotaNewestPerSlotAcrossBoxes(t *testing.T) {
	srv, ctx, c := usageMesh(t)
	r := func(provider string, five float64, t time.Time) usage.QuotaReading {
		return usage.QuotaReading{Provider: provider, FiveHour: &five, Week: 20, ReadAt: t.Format(time.RFC3339)}
	}
	for host, rs := range map[string][]usage.QuotaReading{
		"k16": {r("claude", 10, at(0, 10, 0)), r("codex", 3, at(0, 10, 1)), r("claude", 13, at(0, 10, 5)), r("claude", 1, at(0, 5, 0))},
		"mac": {r("claude", 11, at(0, 10, 2))},
	} {
		if err := usage.PublishQuota(ctx, c, host, rs); err != nil {
			t.Fatal(err)
		}
	}
	var v UsageView
	getJSON(t, srv, "/api/usage?since=6h", 200, &v)
	want := []usage.QuotaReading{r("codex", 3, at(0, 10, 1)), r("claude", 11, at(0, 10, 2)), r("claude", 13, at(0, 10, 5))}
	if !reflect.DeepEqual(v.Quota, want) {
		t.Errorf("quota %+v, want %+v", v.Quota, want)
	}
}
