package web

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// paths lists the paths of ns in order.
func paths(ns []TreeNode) []string {
	out := []string{}
	for _, n := range ns {
		out = append(out, n.Node.Path)
	}
	return out
}

// count counts ns and their descendants.
func count(ns []TreeNode) int {
	c := 0
	for _, n := range ns {
		c += 1 + count(n.Children)
	}
	return c
}

func TestTreeGroupsExecutorsUnderLeads(t *testing.T) {
	nodes := []mesh.Node{
		{Path: "b", Role: "lead"},
		{Path: "a", Role: "lead"},
		{Path: "a.x", Role: "executor", Parent: "a"},
		{Path: "a.x.y", Role: "executor", Parent: "a.x"},
		{Path: "a.w", Role: "executor", Parent: "a"},
		{Path: "routine.verify.cmd-1", Role: "routine", Repo: "app"},
		{Path: "routine.review.m-1", Role: "routine", Repo: "app"},
		{Path: "routine.review.m-2", Role: "routine", Repo: "app"},
		{Path: "routine.review.m-1", Role: "routine", Repo: "other"},
	}
	states := map[string]mesh.State{"a": {State: "working"}, "routine.review.m-1": {State: "working"}}
	x := Extras{
		Runners: map[string]mesh.RunnerSample{"a": {Running: true, Known: true}},
		Unread:  map[string]int{"a.x": 3},
		Usage:   map[string]Usage{"routine.review.m-1": {Uses: 2, Context: 5000}},
		Depth:   map[string]int{"review": 4},
	}
	v := Tree(nodes, states, x)
	if got := paths(v.Leads); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("leads %v", got)
	}
	a, b := v.Leads[0], v.Leads[1]
	if a.State.State != "working" || a.Runner == nil || !a.Runner.Running || b.State.State != "idle" || b.Runner != nil {
		t.Fatalf("lead rows %+v %+v", a, b)
	}
	if got := paths(a.Children); !reflect.DeepEqual(got, []string{"a.w", "a.x"}) {
		t.Fatalf("a's children %v", got)
	}
	if ax := a.Children[1]; ax.Unread != 3 || !reflect.DeepEqual(paths(ax.Children), []string{"a.x.y"}) {
		t.Fatalf("a.x %+v", ax)
	}
	if len(v.Orphans) != 0 {
		t.Fatalf("orphans %v", paths(v.Orphans))
	}
	type group struct {
		kind, repo string
		insts      []string
		depth      int
	}
	var got []group
	for _, g := range v.Routines {
		var ps []string
		for _, in := range g.Instances {
			ps = append(ps, in.Node.Path)
		}
		got = append(got, group{g.Kind, g.Repo, ps, g.Depth})
	}
	want := []group{
		{"review", "app", []string{"routine.review.m-1", "routine.review.m-2"}, 4},
		{"review", "other", []string{"routine.review.m-1"}, 4},
		{"verify", "app", []string{"routine.verify.cmd-1"}, 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routines %+v, want %+v", got, want)
	}
	if in := v.Routines[0].Instances[0]; in.Uses != 2 || in.Context != 5000 || in.State.State != "working" {
		t.Fatalf("instance %+v", in)
	}
	if in := v.Routines[0].Instances[1]; in.Uses != 0 || in.State.State != "idle" {
		t.Fatalf("instance %+v", in)
	}
	// Empty lists encode as [], not null.
	data, _ := json.Marshal(Tree(nil, nil, Extras{}))
	if string(data) != `{"leads":[],"routines":[],"orphans":[]}` {
		t.Fatalf("empty tree %s", data)
	}
}

func TestTreeKeepsOrphans(t *testing.T) {
	nodes := []mesh.Node{
		{Path: "a", Role: "lead"},
		{Path: "a.x", Role: "executor", Parent: "a"},
		{Path: "c.x", Role: "executor", Parent: "c"},           // registered before its lead
		{Path: "c.x.y", Role: "executor", Parent: "c.x"},       // under an orphan
		{Path: "d.x", Role: "executor"},                        // no parent at all
		{Path: "e.y", Role: "executor", Parent: "routine.v.1"}, // a parent that is no lead's
		{Path: "p.q", Role: "executor", Parent: "p.r"},         // a cycle
		{Path: "p.r", Role: "executor", Parent: "p.q"},
		{Path: "routine.v.1", Role: "routine"},
	}
	v := Tree(nodes, nil, Extras{})
	if got := paths(v.Leads); !reflect.DeepEqual(got, []string{"a"}) || !reflect.DeepEqual(paths(v.Leads[0].Children), []string{"a.x"}) {
		t.Fatalf("leads %+v", v.Leads)
	}
	if got := paths(v.Orphans); !reflect.DeepEqual(got, []string{"c.x", "d.x", "e.y", "p.q"}) {
		t.Fatalf("orphans %v", got)
	}
	if got := paths(v.Orphans[0].Children); !reflect.DeepEqual(got, []string{"c.x.y"}) {
		t.Fatalf("c.x children %v", got)
	}
	if got := paths(v.Orphans[3].Children); !reflect.DeepEqual(got, []string{"p.r"}) {
		t.Fatalf("p.q children %v", got)
	}
	if n := count(v.Leads) + count(v.Orphans) + len(v.Routines[0].Instances); n != len(nodes) {
		t.Fatalf("drawn %d of %d nodes", n, len(nodes))
	}
}

func TestInstanceUsageCountsSinceStartOrRecycle(t *testing.T) {
	at := time.Now()
	ev := func(from, event, text string) mesh.Stored {
		data, _ := json.Marshal(mesh.Event{From: from, Event: event, Text: text})
		return mesh.Stored{Subject: "bm." + from + "." + event, At: at, Data: data}
	}
	const p, q = "routine.review.m-1", "routine.verify.cmd-1"
	hist := []mesh.Stored{
		ev(p, "started", ""),
		ev(p, "progress", "job j1 started"),
		ev(p, "progress", "job j1 ok 10 ctx 900"),
		ev(p, "progress", "recycled"),
		ev(p, "progress", "job j2 started"),
		ev(p, "progress", "job j2 fail 20 ctx 1200"),
		ev(p, "progress", "job j3 ok 20 ctx 3400"),
		ev(q, "progress", "job j4 ok 5"),
		ev(q, "started", ""),
		ev(q, "progress", "job j5 ok 5"),
		ev("a", "progress", "job j6 ok 5 ctx 1"),
	}
	got := InstanceUsage(hist)
	want := map[string]Usage{p: {Uses: 2, Context: 3400}, q: {Uses: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("usage %+v, want %+v", got, want)
	}
}

func TestConsoleURL(t *testing.T) {
	cfg := config.ConsoleConfig{ThreadURL: "http://here/t/{thread}", Hosts: map[string]string{"k16": "http://k16/t/{thread}"}}
	lead := func(host, thread string) mesh.Node {
		return mesh.Node{Path: "a", Role: "lead", Host: host, Runner: mesh.NodeRunner{Thread: thread}}
	}
	for _, c := range []struct {
		cfg  config.ConsoleConfig
		n    mesh.Node
		want string
	}{
		{cfg, lead("box", "th-1"), "http://here/t/th-1"},
		{cfg, lead("k16", "th-2"), "http://k16/t/th-2"},
		{cfg, lead("k99", "th-3"), ""}, // another host with no template
		{cfg, lead("box", ""), ""},     // no thread
		{config.ConsoleConfig{}, lead("box", "th-1"), ""},
	} {
		if got := ConsoleURL(c.cfg, "box", c.n); got != c.want {
			t.Errorf("ConsoleURL(%+v, %s/%s) = %q, want %q", c.cfg, c.n.Host, c.n.Runner.Thread, got, c.want)
		}
	}
}
