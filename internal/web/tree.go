package web

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// TreeNode is a lead, an executor or an orphan with the nodes under it.
type TreeNode struct {
	Node     mesh.Node          `json:"node"`
	State    mesh.State         `json:"state"`
	Runner   *mesh.RunnerSample `json:"runner"`
	Unread   int                `json:"unread"`
	Children []TreeNode         `json:"children"`
}

// Instance is a routine instance with its use since it last started or
// recycled.
type Instance struct {
	Node    mesh.Node  `json:"node"`
	State   mesh.State `json:"state"`
	Uses    int        `json:"uses"`
	Context int        `json:"context"`
}

// RoutineGroup is the instances of one kind in one repo; Depth is the kind's
// queue depth.
type RoutineGroup struct {
	Kind      string     `json:"kind"`
	Repo      string     `json:"repo"`
	Instances []Instance `json:"instances"`
	Depth     int        `json:"depth"`
}

// TreeView is /api/tree. Orphans are the non-routine nodes whose parent
// chain never reaches a registered lead, so no node goes undrawn.
type TreeView struct {
	Leads    []TreeNode     `json:"leads"`
	Routines []RoutineGroup `json:"routines"`
	Orphans  []TreeNode     `json:"orphans"`
}

// Usage is an instance's finished jobs since its last start or recycle and
// the context tokens of the newest of them.
type Usage struct{ Uses, Context int }

// Extras is what Tree adds to nodes and states, keyed by path (Depth by kind).
type Extras struct {
	Runners map[string]mesh.RunnerSample
	Unread  map[string]int
	Usage   map[string]Usage
	Depth   map[string]int
}

func stateOf(states map[string]mesh.State, path string) mesh.State {
	if s, ok := states[path]; ok {
		return s
	}
	return mesh.State{State: "idle"}
}

// Tree groups nodes into leads with their executors (nested by Parent),
// routine instances by kind and repo, and orphans; every list is sorted by
// path.
func Tree(nodes []mesh.Node, states map[string]mesh.State, x Extras) TreeView {
	v := TreeView{Leads: []TreeNode{}, Routines: []RoutineGroup{}, Orphans: []TreeNode{}}
	sorted := append([]mesh.Node(nil), nodes...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	byPath := map[string]mesh.Node{}
	for _, n := range sorted {
		if n.Role != "routine" {
			byPath[n.Path] = n
		}
	}
	children := map[string][]mesh.Node{}
	for _, n := range sorted {
		if n.Role == "routine" || n.Role == "lead" {
			continue
		}
		if _, ok := byPath[n.Parent]; ok && n.Parent != n.Path {
			children[n.Parent] = append(children[n.Parent], n)
		}
	}
	drawn := map[string]bool{}
	var build func(n mesh.Node) TreeNode
	build = func(n mesh.Node) TreeNode {
		drawn[n.Path] = true
		t := TreeNode{Node: n, State: stateOf(states, n.Path), Unread: x.Unread[n.Path], Children: []TreeNode{}}
		if s, ok := x.Runners[n.Path]; ok {
			t.Runner = &s
		}
		for _, c := range children[n.Path] {
			if !drawn[c.Path] {
				t.Children = append(t.Children, build(c))
			}
		}
		return t
	}
	for _, n := range sorted {
		if n.Role == "lead" && !drawn[n.Path] {
			v.Leads = append(v.Leads, build(n))
		}
	}
	// Orphan roots: undrawn nodes whose parent is not an undrawn node, so an
	// orphan's own executors nest under it; then whatever a cycle left.
	for pass := 0; pass < 2; pass++ {
		for _, n := range sorted {
			if n.Role == "routine" || drawn[n.Path] {
				continue
			}
			if p, ok := byPath[n.Parent]; pass == 0 && ok && !drawn[p.Path] && p.Role != "lead" {
				continue
			}
			v.Orphans = append(v.Orphans, build(n))
		}
	}
	sort.SliceStable(v.Orphans, func(i, j int) bool { return v.Orphans[i].Node.Path < v.Orphans[j].Node.Path })

	groups := map[[2]string]*RoutineGroup{}
	for _, n := range sorted {
		if n.Role != "routine" {
			continue
		}
		p, err := mesh.ParsePath(n.Path)
		if err != nil {
			continue
		}
		kind, ok := mesh.KindFromPath(p)
		if !ok {
			continue
		}
		key := [2]string{kind, n.Repo}
		g, ok := groups[key]
		if !ok {
			g = &RoutineGroup{Kind: kind, Repo: n.Repo, Instances: []Instance{}, Depth: x.Depth[kind]}
			groups[key] = g
		}
		u := x.Usage[n.Path]
		g.Instances = append(g.Instances, Instance{Node: n, State: stateOf(states, n.Path), Uses: u.Uses, Context: u.Context})
	}
	for _, g := range groups {
		v.Routines = append(v.Routines, *g)
	}
	sort.Slice(v.Routines, func(i, j int) bool {
		a, b := v.Routines[i], v.Routines[j]
		return a.Kind < b.Kind || a.Kind == b.Kind && a.Repo < b.Repo
	})
	return v
}

var jobDoneRE = regexp.MustCompile(`^job \S+ (?:ok|fail) \d+(?: ctx (\d+))?$`)

// InstanceUsage reads routine instances' events (oldest first): uses counts
// the "job <id> ok|fail <ms>[ ctx <tokens>]" progress lines since the
// instance's last started or recycled event, context is the ctx of the
// newest of them (0 when it has none).
func InstanceUsage(hist []mesh.Stored) map[string]Usage {
	out := map[string]Usage{}
	for _, m := range hist {
		var ev mesh.Event
		if json.Unmarshal(m.Data, &ev) != nil || !strings.HasPrefix(ev.From, "routine.") {
			continue
		}
		switch {
		case ev.Event == "started" || ev.Event == "progress" && ev.Text == "recycled":
			out[ev.From] = Usage{}
		case ev.Event == "progress":
			sm := jobDoneRE.FindStringSubmatch(ev.Text)
			if sm == nil {
				continue
			}
			ctx, _ := strconv.Atoi(sm[1])
			out[ev.From] = Usage{Uses: out[ev.From].Uses + 1, Context: ctx}
		}
	}
	for p, u := range out {
		if u.Uses == 0 {
			delete(out, p)
		}
	}
	return out
}

// ConsoleURL is a lead's thread link: [console] thread_url when it runs on
// thisHost and that is set, else the [console.hosts] template for its host,
// with {thread} replaced by its runner thread; "" without a template or a
// thread.
func ConsoleURL(cfg config.ConsoleConfig, thisHost string, n mesh.Node) string {
	tmpl := cfg.Hosts[n.Host]
	if n.Host == thisHost && cfg.ThreadURL != "" {
		tmpl = cfg.ThreadURL
	}
	if tmpl == "" || n.Runner.Thread == "" {
		return ""
	}
	return strings.ReplaceAll(tmpl, "{thread}", n.Runner.Thread)
}
