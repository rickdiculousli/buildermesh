package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// min0 is t0 plus m minutes.
func min0(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }

func ev(m int, from, event, text, to string) mesh.Stored {
	e := mesh.Event{From: from, Event: event, Text: text, To: to, At: min0(m).Format(time.RFC3339)}
	b, _ := json.Marshal(e)
	subject := "bm." + from + "." + event
	return mesh.Stored{Subject: subject, At: min0(m), Data: b}
}

func job(m int, from, to string) mesh.Stored {
	b, _ := json.Marshal(mesh.Message{From: from, To: to, Kind: "job", Text: "review api", At: min0(m).Format(time.RFC3339)})
	return mesh.Stored{Subject: "bm.inbox." + to, At: min0(m), Data: b}
}

func node(path, role, parent string, startedMin int) mesh.Node {
	return mesh.Node{Path: path, Role: role, Parent: parent, Repo: "app", StartedAt: min0(startedMin).Format(time.RFC3339)}
}

func seg(from, to int, state string) Seg { return Seg{min0(from).Unix(), min0(to).Unix(), state} }

func lane(t *testing.T, v TimelineView, path string) Lane {
	t.Helper()
	for _, l := range v.Lanes {
		if l.Path == path {
			return l
		}
	}
	t.Fatalf("no lane %s in %+v", path, v.Lanes)
	return Lane{}
}

func lanePaths(v TimelineView) []string {
	var out []string
	for _, l := range v.Lanes {
		out = append(out, l.Path)
	}
	return out
}

func TestTimelineStateTransitions(t *testing.T) {
	nodes := []mesh.Node{node("app-001", "lead", "", 0)}
	hist := []mesh.Stored{
		ev(10, "app-001", "started", "", ""),
		ev(20, "app-001", "progress", "half", ""),
		ev(30, "app-001", "ask", "which?", ""),
		ev(40, "app-001", "progress", "answered", ""),
		ev(50, "app-001", "blocked", "stuck", ""),
		ev(60, "app-001", "progress", "unstuck", ""),
		ev(70, "app-001", "held", "by operator", ""),
		ev(80, "app-001", "progress", "released", ""),
		ev(90, "daemon.k16", "silent", "quiet for 25m", "app-001"),
		ev(100, "app-001", "ready", "ready to land", ""),
		ev(105, "daemon.k16", "landing", "review running", "app-001"),
		ev(110, "daemon.k16", "land-refused", "conflicts", "app-001"),
		ev(115, "app-001", "ready", "again", ""),
		ev(118, "daemon.k16", "land-withdrawn", "operator", "app-001"),
		ev(120, "app-001", "ready", "third", ""),
		ev(130, "app-001", "done", "landed", ""),
	}
	l := lane(t, Timeline(nodes, nil, hist, min0(0), min0(200), 0), "app-001")
	want := []Seg{seg(0, 10, "idle"), seg(10, 30, "working"), seg(30, 40, "asking"), seg(40, 50, "working"),
		seg(50, 60, "blocked"), seg(60, 70, "working"), seg(70, 80, "held"), seg(80, 90, "working"),
		seg(90, 100, "silent"), seg(100, 110, "landing"), seg(110, 115, "working"), seg(115, 118, "landing"),
		seg(118, 120, "working"), seg(120, 130, "landing")}
	if !reflect.DeepEqual(l.Segs, want) {
		t.Fatalf("segs\n got %v\nwant %v", l.Segs, want)
	}
	if l.State != "done" {
		t.Fatalf("state %q, want done", l.State)
	}
	var kinds []string
	for _, k := range l.Ticks {
		kinds = append(kinds, k.Kind)
	}
	wantK := []string{"progress", "progress", "progress", "progress", "ready", "refused", "ready", "ready", "done"}
	if !reflect.DeepEqual(kinds, wantK) {
		t.Fatalf("ticks %v, want %v", kinds, wantK)
	}
	if l.Ticks[5].Text != "conflicts" || l.Ticks[5].At != min0(110).Unix() {
		t.Fatalf("refused tick %+v", l.Ticks[5])
	}
}

func TestTimelineLaneWorkingAtWindowStart(t *testing.T) {
	nodes := []mesh.Node{node("app-001", "lead", "", 0)}
	hist := []mesh.Stored{ev(1, "app-001", "started", "", ""), ev(2, "app-001", "progress", "early", ""),
		ev(100, "app-001", "ask", "late", "")}
	v := Timeline(nodes, nil, hist, min0(60), min0(120), 0)
	l := lane(t, v, "app-001")
	if want := []Seg{seg(60, 100, "working"), seg(100, 120, "asking")}; !reflect.DeepEqual(l.Segs, want) {
		t.Fatalf("segs %v, want %v", l.Segs, want)
	}
	if len(l.Ticks) != 0 {
		t.Fatalf("ticks before the window drawn: %v", l.Ticks)
	}
	if l.State != "asking" || v.From != min0(60).Unix() || v.To != min0(120).Unix() {
		t.Fatalf("state %q window %d..%d", l.State, v.From, v.To)
	}
}

func TestTimelineDaemonEventSetsItsTo(t *testing.T) {
	nodes := []mesh.Node{node("app-001", "lead", "", 0), node("app-002", "lead", "", 0)}
	hist := []mesh.Stored{ev(1, "app-001", "started", "", ""), ev(1, "app-002", "started", "", ""),
		ev(5, "daemon.k16", "silent", "quiet", "app-002"), ev(6, "daemon.k16", "wake", "nudged", "app-001")}
	v := Timeline(nodes, nil, hist, min0(0), min0(10), 0)
	if l := lane(t, v, "app-002"); l.State != "silent" || l.Segs[len(l.Segs)-1] != seg(5, 10, "silent") {
		t.Fatalf("app-002 %+v", l)
	}
	if l := lane(t, v, "app-001"); l.State != "working" || len(l.Segs) != 2 {
		t.Fatalf("app-001 moved by a wake or another's silent: %+v", l)
	}
	for _, l := range v.Lanes {
		if l.Path == "daemon.k16" {
			t.Fatal("the daemon got a lane")
		}
	}
}

func TestTimelineDoneEndsLaneAndHidesOlderDone(t *testing.T) {
	nodes := []mesh.Node{node("app-001", "lead", "", 0), node("app-002", "lead", "", 0)}
	hist := []mesh.Stored{ev(1, "app-001", "started", "", ""), ev(5, "app-001", "done", "landed", ""),
		ev(1, "app-002", "started", "", ""), ev(50, "app-002", "done", "landed", "")}
	v := Timeline(nodes, nil, hist, min0(30), min0(60), 0)
	if got := lanePaths(v); !reflect.DeepEqual(got, []string{"app-002"}) {
		t.Fatalf("lanes %v, want only app-002 (app-001 was done before the window)", got)
	}
	if l := v.Lanes[0]; !reflect.DeepEqual(l.Segs, []Seg{seg(30, 50, "working")}) || l.State != "done" {
		t.Fatalf("app-002 %+v", l)
	}
}

func TestTimelineNodeWhoseEventsAgedOutStartsFromItsStoredState(t *testing.T) {
	// app-020 and app-021 started and their events left the stream: only
	// their records and stored states remain; app-022 never started.
	nodes := []mesh.Node{node("app-020", "lead", "", -60*24*31), node("app-021", "lead", "", -60*24*40),
		node("app-022", "lead", "", -60*24*40), node("app-001", "lead", "", 0)}
	states := map[string]mesh.State{
		"app-020": {State: "done", At: min0(-60 * 24 * 30).Format(time.RFC3339)},
		"app-021": {State: "blocked", At: min0(-60 * 24 * 39).Format(time.RFC3339)},
		"app-001": {State: "working", At: min0(1).Format(time.RFC3339)},
	}
	hist := []mesh.Stored{ev(1, "app-001", "started", "", "")}
	v := Timeline(nodes, states, hist, min0(0), min0(60), 0)
	if got := lanePaths(v); !reflect.DeepEqual(got, []string{"app-021", "app-001", "app-022"}) {
		t.Fatalf("lanes %v, want no lane for the done app-020", got)
	}
	if l := lane(t, v, "app-021"); !reflect.DeepEqual(l.Segs, []Seg{seg(0, 60, "blocked")}) {
		t.Fatalf("app-021 %+v", l)
	}
	if l := lane(t, v, "app-001"); !reflect.DeepEqual(l.Segs, []Seg{seg(0, 1, "idle"), seg(1, 60, "working")}) {
		t.Fatalf("a node with events keeps its replay: %+v", l)
	}
}

func TestTimelineChildrenFollowTheirLeadInNeedOrder(t *testing.T) {
	nodes := []mesh.Node{node("app-001", "lead", "", 0), node("app-001.api", "executor", "app-001", 2),
		node("app-001.web", "executor", "app-001", 3), node("app-002", "lead", "", 1), node("app-003", "lead", "", 5),
		node("old-9.fix", "executor", "old-9", 4)}
	hist := []mesh.Stored{ev(1, "app-001", "started", "", ""), ev(3, "app-001.api", "started", "", ""),
		ev(4, "app-001.web", "started", "", ""), ev(6, "app-001.web", "ask", "?", ""),
		ev(2, "app-002", "started", "", ""), ev(8, "app-002", "blocked", "stuck", ""),
		ev(5, "old-9.fix", "started", "", ""),
		// app-003 is registered and never started: idle.
	}
	v := Timeline(nodes, nil, hist, min0(0), min0(10), 0)
	want := []string{"app-002", "app-001", "app-001.web", "app-001.api", "old-9.fix", "app-003"}
	if got := lanePaths(v); !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	// Blocked for longer than stale_after: after held, ahead of idle.
	if got := lanePaths(Timeline(nodes, nil, hist, min0(0), min0(10), time.Minute)); !reflect.DeepEqual(got,
		[]string{"app-001", "app-001.api", "app-001.web", "old-9.fix", "app-002", "app-003"}) {
		t.Fatalf("stale order %v", got)
	}
	if l := lane(t, v, "app-001.web"); l.Parent != "app-001" || l.Role != "executor" {
		t.Fatalf("executor lane %+v", l)
	}
	if l := lane(t, v, "app-003"); l.State != "idle" || !reflect.DeepEqual(l.Segs, []Seg{seg(5, 10, "idle")}) {
		t.Fatalf("idle lane %+v", l)
	}
}

func TestTimelineRoutineJobsAndJobTicks(t *testing.T) {
	nodes := []mesh.Node{node("app-001", "lead", "", 0), {Path: "routine.review.a", Role: "routine", Repo: "app"}}
	hist := []mesh.Stored{ev(1, "app-001", "started", "", ""),
		job(2, "app-001", "routine.review"),
		ev(3, "routine.review.a", "started", "", ""),
		ev(4, "routine.review.a", "progress", "job j1 started", ""),
		ev(9, "routine.review.a", "progress", "job j1 ok 300000 ctx 51000", ""),
		ev(12, "routine.review.a", "progress", "job j2 started", ""),
		ev(13, "routine.review.b", "progress", "job j3 started", ""),
		ev(14, "routine.review.b", "progress", "job j3 fail 1000", ""),
	}
	v := Timeline(nodes, nil, hist, min0(0), min0(20), 0)
	if got := lanePaths(v); !reflect.DeepEqual(got, []string{"app-001", "routine.review.a", "routine.review.b"}) {
		t.Fatalf("lanes %v", got)
	}
	a := lane(t, v, "routine.review.a")
	if want := []Seg{seg(4, 9, "working"), seg(12, 20, "working")}; !reflect.DeepEqual(a.Segs, want) || a.Role != "routine" || a.State != "working" {
		t.Fatalf("routine lane %+v, want segs %v", a, want)
	}
	if b := lane(t, v, "routine.review.b"); b.State != "idle" || len(b.Segs) != 1 {
		t.Fatalf("an instance with no node record %+v", b)
	}
	l := lane(t, v, "app-001")
	if len(l.Ticks) != 1 || l.Ticks[0] != (Tick{min0(2).Unix(), "job", "review api"}) {
		t.Fatalf("job tick %+v", l.Ticks)
	}
	// Window bounds: a job is clipped to the window, one wholly outside draws nothing.
	if v := Timeline(nodes, nil, hist, min0(5), min0(6), 0); len(lane(t, v, "routine.review.a").Segs) != 1 {
		t.Fatalf("clipped %+v", v.Lanes)
	}
	if v := Timeline(nodes, nil, hist, min0(15), min0(16), 0); len(v.Lanes) != 2 {
		t.Fatalf("lanes %v, want app-001 and the open job only", lanePaths(v))
	}
}

func TestTimelineWindowBoundsClipSegmentsAndTicks(t *testing.T) {
	nodes := []mesh.Node{node("app-001", "lead", "", 0)}
	hist := []mesh.Stored{ev(1, "app-001", "started", "", ""), ev(5, "app-001", "progress", "in", ""),
		ev(15, "app-001", "progress", "after", "")}
	v := Timeline(nodes, nil, hist, min0(2), min0(10), 0)
	l := lane(t, v, "app-001")
	if !reflect.DeepEqual(l.Segs, []Seg{seg(2, 10, "working")}) {
		t.Fatalf("segs %v", l.Segs)
	}
	if len(l.Ticks) != 1 || l.Ticks[0].Text != "in" {
		t.Fatalf("ticks %v", l.Ticks)
	}
	// A node registered after the window has no lane.
	if v := Timeline([]mesh.Node{node("app-009", "lead", "", 30)}, nil, nil, min0(0), min0(10), 0); len(v.Lanes) != 0 {
		t.Fatalf("lanes %v", lanePaths(v))
	}
}

func TestLeadTimelineSpansItsLife(t *testing.T) {
	nodes := []mesh.Node{node("app-001", "lead", "", 0), node("app-001.api", "executor", "app-001", 2), node("app-002", "lead", "", 0)}
	hist := []mesh.Stored{ev(1, "app-001", "started", "", ""), ev(3, "app-001.api", "started", "", ""),
		ev(1, "app-002", "started", "", ""), ev(300, "app-001", "done", "", "")}
	v := LeadTimeline(nodes, nil, hist, "app-001", min0(600))
	if got := lanePaths(v); !reflect.DeepEqual(got, []string{"app-001", "app-001.api"}) {
		t.Fatalf("lanes %v", got)
	}
	if v.From != min0(0).Unix() || v.To != min0(600).Unix() {
		t.Fatalf("window %d..%d, want to now while the executor works", v.From, v.To)
	}
	// Once all are done, the life ends at the last event.
	hist = append(hist, ev(200, "app-001.api", "done", "", ""))
	if v := LeadTimeline(nodes, nil, hist, "app-001", min0(600)); v.To != min0(300).Unix() {
		t.Fatalf("done lead's window ends %d, want %d", v.To, min0(300).Unix())
	}
}

func TestTimelineEndpoint(t *testing.T) {
	url := meshtest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := mesh.Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := mesh.PutNode(ctx, c, mesh.Node{Path: "app-001", Role: "lead", Repo: "app", Spec: "001",
		StartedAt: now.Add(-3 * time.Hour).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	for _, e := range []mesh.Event{
		{From: "app-001", Event: "started", At: now.Add(-2 * time.Hour).Format(time.RFC3339)},
		{From: "daemon.k16", Event: "land-refused", To: "app-001", Text: "conflicts", At: now.Add(-30 * time.Minute).Format(time.RFC3339)},
	} {
		if err := mesh.PublishEvent(ctx, c, e); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(Handler(c, time.Hour, Options{}))
	defer srv.Close()
	get := func(q string) (int, TimelineView, string) {
		resp, err := http.Get(srv.URL + "/api/timeline" + q)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var raw json.RawMessage
		_ = json.NewDecoder(resp.Body).Decode(&raw)
		var v struct {
			From, To int64
			Lanes    []struct {
				Path, Spec, State string
				Segs              [][]any
				Ticks             [][]any
			}
		}
		_ = json.Unmarshal(raw, &v)
		out := TimelineView{From: v.From, To: v.To}
		for _, l := range v.Lanes {
			out.Lanes = append(out.Lanes, Lane{Path: l.Path, Spec: l.Spec, State: l.State,
				Segs: make([]Seg, len(l.Segs)), Ticks: make([]Tick, len(l.Ticks))})
		}
		return resp.StatusCode, out, string(raw)
	}
	code, v, raw := get("?since=1h")
	if code != 200 || len(v.Lanes) != 1 || v.Lanes[0].Spec != "001" || v.Lanes[0].State != "working" ||
		len(v.Lanes[0].Segs) != 1 || len(v.Lanes[0].Ticks) != 1 || v.To-v.From != 3600 {
		t.Fatalf("1h: %d %s", code, raw)
	}
	if _, v, raw := get(""); len(v.Lanes[0].Segs) != 2 || v.To-v.From != 6*3600 {
		t.Fatalf("default 6h: %s", raw)
	}
	if _, v, raw := get("?lead=app-001"); len(v.Lanes) != 1 || v.From != now.Add(-3*time.Hour).Unix() {
		t.Fatalf("lead: %s", raw)
	}
	if code, _, _ := get("?since=2h"); code != http.StatusBadRequest {
		t.Fatalf("since=2h: %d, want 400", code)
	}
}
