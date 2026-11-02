package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/usage"
)

// /api/usage flags each agent by the rules against the done leads of its
// size: the band holds the flagged live agents, runaway first (a live lead
// six times its median, a live session past compact_soon, a live lead
// quiet through the window but over its median), never a done one; the
// outliers hold every lead with its flags and the run to compare with.
func TestUsageFlagsBandAndOutliers(t *testing.T) {
	ctx, c := connect(t)
	repo := filepath.Join(t.TempDir(), "app")
	meshtest.Git(t, filepath.Dir(repo), "init", "-q", "-b", "main", "app")
	files := map[string]string{}
	for id := 1; id <= 8; id++ {
		files[fmt.Sprintf("docs/proposals/%03d-s.md", id)] = fmt.Sprintf("---\nid: %d\ntitle: S\nstatus: accepted\nsize: M\n---\nbody\n", id)
	}
	meshtest.CommitFiles(t, repo, files)
	started := at(-2, 9, 0).Format(time.RFC3339)
	lead := func(id int) string { return fmt.Sprintf("app-%03d", id) }
	// read is a call reading n with a small context.
	read := func(t time.Time, n int64) usage.Call { c := call(t, n, "", ""); c.Ctx = 100_000; return c }
	for id := 1; id <= 8; id++ {
		if err := mesh.PutNode(ctx, c, mesh.Node{Path: lead(id), Role: "lead", Repo: "app", Spec: fmt.Sprintf("%03d", id), Host: "k16", StartedAt: started}); err != nil {
			t.Fatal(err)
		}
	}
	// Five done runs of 10M (the last 20M: a done overworker), each over an hour two days ago.
	for id := 1; id <= 6; id++ {
		n := int64(10_000_000)
		if id == 6 {
			n = 30_000_000
		}
		publishUsage(t, ctx, c, usage.Message{Agent: lead(id), Role: "lead", Host: "k16", Repo: "app",
			Calls: []usage.Call{read(at(-2, 9, 30), n/2), read(at(-2, 10, 0), n/2)}})
		meshtest.ForceState(t, c, lead(id), mesh.State{State: "done", By: "node", At: at(-2, 10, 0).Format(time.RFC3339)})
	}
	// app-007 live and 6x the median, reading now; app-008 live, 3x, quiet for a day.
	publishUsage(t, ctx, c, usage.Message{Agent: lead(7), Role: "lead", Host: "k16", Repo: "app",
		Calls: []usage.Call{read(at(-1, 9, 0), 30_000_000), read(at(0, 12, 0), 30_000_000)}})
	meshtest.ForceState(t, c, lead(7), mesh.State{State: "working", By: "node"})
	publishUsage(t, ctx, c, usage.Message{Agent: lead(8), Role: "lead", Host: "k16", Repo: "app",
		Calls: []usage.Call{read(at(-1, 9, 0), 30_000_000)}})
	meshtest.ForceState(t, c, lead(8), mesh.State{State: "working", By: "node"})
	coord := usage.SessionAgent("k16", coordID)
	publishUsage(t, ctx, c, usage.Message{Agent: coord, Role: usage.Coordinator, Host: "k16", Repo: "app", Session: coordID,
		Calls: []usage.Call{call(at(0, 12, 5), 537_000, "design", "")}})
	cfg := &config.Config{Repos: map[string]config.Repo{"app": {Path: repo}}, Spec: config.SpecConfig{Glob: "docs/proposals/{id}-*.md"},
		Context: config.ContextConfig{CompactSoon: 400000}}
	srv := httptest.NewServer(Handler(c, time.Hour, Options{Cfg: cfg, Host: "k16", Now: func() time.Time { return usageNow }}))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/usage?since=1h")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v UsageView
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	var band []string
	for _, b := range v.Band {
		band = append(band, b.Level+" "+b.Agent+": "+usage.Words(b.Flags))
	}
	want := []string{
		"runaway app-007: read 60.0M, 6.0× the app M median (10.0M)",
		"runaway " + coord + ": context 537k, past compact_soon (400k)",
		"overworking app-008: read 30.0M, 3.0× the app M median (10.0M)",
	}
	if fmt.Sprint(band) != fmt.Sprint(want) {
		t.Fatalf("band\n%q\nwant\n%q", band, want)
	}
	for _, a := range v.Agents {
		if a.Flags == nil {
			t.Fatalf("%s: flags null", a.Agent)
		}
	}
	got := map[string]Outlier{}
	for _, o := range v.Outliers {
		got[o.Agent] = o
	}
	if len(got) != 8 {
		t.Fatalf("outliers %+v", v.Outliers)
	}
	if o := got[lead(6)]; o.Level != usage.Overworking || o.Live || o.Wall != 3600 || o.Size != "M" || o.Vs == "" || o.Vs == lead(6) {
		t.Fatalf("done overworker %+v", o)
	}
	if o := got[lead(7)]; o.Level != usage.Runaway || !o.Live || o.Read != 60_000_000 || o.Turns != 2 {
		t.Fatalf("live runaway %+v", o)
	}
	if o := got[lead(1)]; o.Level != "" || len(o.Flags) != 0 {
		t.Fatalf("a median run flagged: %+v", o)
	}
}
