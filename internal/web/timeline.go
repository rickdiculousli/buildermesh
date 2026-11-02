package web

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// histFor is how long one read of bm-mesh serves /api/timeline.
const histFor = 30 * time.Second

// windows are the spans /api/timeline?since= takes.
var windows = map[string]time.Duration{"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour}

// nodeState is the state a node's own event sets; done ends the lane.
// daemonState is the state a daemon event sets on the node it is addressed
// to (to). Both are the map's live state table (map.js EVENT_STATE and
// DAEMON_STATE).
var (
	nodeState   = map[string]string{"started": "working", "progress": "working", "ask": "asking", "blocked": "blocked", "held": "held", "ready": "landing", "done": "done"}
	daemonState = map[string]string{"silent": "silent", "landing": "landing", "land-refused": "working", "land-withdrawn": "working"}
	// tickKind is the tick an event draws on its lane.
	tickKind = map[string]string{"progress": "progress", "job": "job", "land-refused": "refused", "ready": "ready", "done": "done"}
	// needRank orders lanes as the needs-you band orders states.
	needRank = map[string]int{"blocked": 0, "asking": 1, "silent": 2, "working": 3, "landing": 4, "held": 5, "idle": 6, "done": 7}
)

var (
	jobStartRE = regexp.MustCompile(`^job (\S+) started$`)
	jobEndRE   = regexp.MustCompile(`^job (\S+) (?:ok|fail) \d+`)
)

// Seg is a span of one state, in unix seconds; it encodes as [from, to, state].
type Seg struct {
	From, To int64
	State    string
}

func (s Seg) MarshalJSON() ([]byte, error) { return json.Marshal([]any{s.From, s.To, s.State}) }

// Tick is an event drawn on a lane: [at, kind, text], at in unix seconds.
type Tick struct {
	At   int64
	Kind string
	Text string
}

func (t Tick) MarshalJSON() ([]byte, error) { return json.Marshal([]any{t.At, t.Kind, t.Text}) }

// Lane is one node's states over a window; State is its state at the
// window's end ("done" once done).
type Lane struct {
	Path   string `json:"path"`
	Role   string `json:"role"`
	Parent string `json:"parent"`
	Repo   string `json:"repo"`
	Spec   string `json:"spec"`
	State  string `json:"state"`
	Segs   []Seg  `json:"segs"`
	Ticks  []Tick `json:"ticks"`
	start  int64  // the lane's first transition, unclipped
	since  int64  // when State began
}

// TimelineView is /api/timeline: the window in unix seconds and its lanes.
type TimelineView struct {
	From  int64  `json:"from"`
	To    int64  `json:"to"`
	Lanes []Lane `json:"lanes"`
}

// laneEvent is one bm-mesh message as it bears on a lane: the path it moves
// and the event's name (a job sent from path is "job").
type laneEvent struct {
	at         time.Time
	path, name string
	text       string
}

// laneEvents reads hist into the events that move or mark a lane, oldest
// first by each event's own at (a message's time when it has none).
func laneEvents(hist []mesh.Stored) []laneEvent {
	var out []laneEvent
	for _, m := range hist {
		rest, ok := strings.CutPrefix(m.Subject, "bm.")
		if !ok || mesh.IsHostTraffic(m.Subject) {
			continue
		}
		var ev mesh.Event
		_ = json.Unmarshal(m.Data, &ev)
		at := m.At
		if t, err := time.Parse(time.RFC3339, ev.At); err == nil {
			at = t
		}
		switch first, _, _ := strings.Cut(rest, "."); first {
		case "inbox":
			var msg mesh.Message
			if json.Unmarshal(m.Data, &msg) != nil || msg.Kind != "job" || msg.From == "" {
				continue
			}
			text := msg.Text
			if text == "" && msg.Job != nil {
				text = msg.Job.Kind
			}
			if t, err := time.Parse(time.RFC3339, msg.At); err == nil {
				at = t
			}
			out = append(out, laneEvent{at: at, path: msg.From, name: "job", text: text})
		case "daemon":
			name := rest[strings.LastIndex(rest, ".")+1:]
			if _, ok := daemonState[name]; ok && ev.To != "" {
				out = append(out, laneEvent{at: at, path: ev.To, name: name, text: ev.Text})
			}
		default:
			if mesh.IsMeshInternal(m.Subject) {
				continue
			}
			i := strings.LastIndex(rest, ".")
			if i <= 0 {
				continue
			}
			out = append(out, laneEvent{at: at, path: rest[:i], name: rest[i+1:], text: ev.Text})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

type transition struct {
	at    int64
	state string
}

// Timeline replays hist (bm-mesh, oldest first) into a lane per node with a
// segment in [from, to]: leads, rounds, fixers, executors and engineers by
// the state table, from idle at registration (started_at). A node with no
// events in hist (they aged out of the stream) starts from its stored state
// in states at that state's at instead, so one done long ago has no lane;
// routine
// instances one working segment per job, from its "job <id> started" to its
// "job <id> ok|fail". A path with events but no node record (a forgotten
// node) gets a lane ending at its last event, its parent the path less its
// last segment. Lanes come in need
// order of their state at to (a blocked, asking or silent state older than
// stale, when stale is set, after held and ahead of idle, as the band folds
// it), then by start, then path, each followed by its children in the same
// order; routine lanes last, by path.
func Timeline(nodes []mesh.Node, states map[string]mesh.State, hist []mesh.Stored, from, to time.Time, stale time.Duration) TimelineView {
	f, t := from.Unix(), to.Unix()
	byPath := map[string]mesh.Node{}
	for _, n := range nodes {
		byPath[n.Path] = n
	}
	trans := map[string][]transition{}
	ticks := map[string][]Tick{}
	jobs := map[string][]Seg{}
	open := map[string]map[string]int64{} // a routine instance's jobs started and not ended
	for _, n := range nodes {
		if st, err := time.Parse(time.RFC3339, n.StartedAt); err == nil && n.Role != "routine" {
			trans[n.Path] = append(trans[n.Path], transition{st.Unix(), "idle"})
		}
	}
	for _, e := range laneEvents(hist) {
		at := e.at.Unix()
		if strings.HasPrefix(e.path, "routine.") {
			if e.name != "progress" {
				continue
			}
			if sm := jobStartRE.FindStringSubmatch(e.text); sm != nil {
				if open[e.path] == nil {
					open[e.path] = map[string]int64{}
				}
				open[e.path][sm[1]] = at
			} else if sm := jobEndRE.FindStringSubmatch(e.text); sm != nil {
				if s, ok := open[e.path][sm[1]]; ok {
					jobs[e.path] = append(jobs[e.path], Seg{s, at, "working"})
					delete(open[e.path], sm[1])
				}
			}
			continue
		}
		state := nodeState[e.name]
		if d, ok := daemonState[e.name]; ok {
			state = d
		}
		if state != "" {
			tr := trans[e.path]
			// Registration after the first event (a re-put node) adds no idle before it.
			if len(tr) == 1 && tr[0].state == "idle" && tr[0].at > at {
				tr = tr[:0]
			}
			trans[e.path] = append(tr, transition{at, state})
		}
		if k := tickKind[e.name]; k != "" && at >= f && at <= t {
			ticks[e.path] = append(ticks[e.path], Tick{at, k, e.text})
		}
	}
	for _, n := range nodes {
		tr, st := trans[n.Path], states[n.Path]
		if len(tr) != 1 || tr[0].state != "idle" || st.State == "" || st.State == "idle" {
			continue
		}
		at := tr[0].at
		if t, err := time.Parse(time.RFC3339, st.At); err == nil {
			at = t.Unix()
		}
		trans[n.Path] = []transition{{at, st.State}}
	}
	for path, js := range open {
		for _, s := range js {
			jobs[path] = append(jobs[path], Seg{s, t, "working"})
		}
	}

	var lanes, routines []Lane
	for path, tr := range trans {
		if _, ok := byPath[path]; !ok {
			// No record: the node was forgotten, which leaves its events but
			// marks nothing, so its lane ends at its last event.
			tr = append(tr, transition{tr[len(tr)-1].at, "done"})
		}
		l := newLane(path, byPath)
		l.start = tr[0].at
		for i, x := range tr {
			end := t
			if i+1 < len(tr) {
				end = tr[i+1].at
			}
			if x.at <= t {
				l.State, l.since = x.state, x.at
			}
			if x.state == "done" {
				continue
			}
			if s, e := max(x.at, f), min(end, t); s < e {
				if n := len(l.Segs); n > 0 && l.Segs[n-1].State == x.state && l.Segs[n-1].To == s {
					l.Segs[n-1].To = e
				} else {
					l.Segs = append(l.Segs, Seg{s, e, x.state})
				}
			}
		}
		if len(l.Segs) == 0 {
			continue
		}
		l.Ticks = ticks[path]
		if l.Ticks == nil {
			l.Ticks = []Tick{}
		}
		lanes = append(lanes, l)
	}
	for path, js := range jobs {
		l := newLane(path, byPath)
		l.Role, l.State, l.Ticks = "routine", "idle", []Tick{}
		sort.Slice(js, func(i, j int) bool { return js[i].From < js[j].From })
		for _, s := range js {
			if a, b := max(s.From, f), min(s.To, t); a < b {
				l.Segs = append(l.Segs, Seg{a, b, "working"})
				if s.To >= t {
					l.State = "working"
				}
			}
		}
		if len(l.Segs) > 0 {
			routines = append(routines, l)
		}
	}
	sort.Slice(routines, func(i, j int) bool { return routines[i].Path < routines[j].Path })
	staleBefore := int64(0)
	if stale > 0 {
		staleBefore = t - int64(stale/time.Second)
	}
	return TimelineView{From: f, To: t, Lanes: append(ordered(lanes, staleBefore), routines...)}
}

// newLane is path's lane with its node's fields, or for a path with no
// node a lead (one segment) or a child of the path less its last segment.
func newLane(path string, byPath map[string]mesh.Node) Lane {
	n, ok := byPath[path]
	if !ok {
		n = mesh.Node{Path: path, Role: "lead"}
		if i := strings.LastIndex(path, "."); i > 0 {
			n.Role, n.Parent = "executor", path[:i]
		}
	}
	return Lane{Path: path, Role: n.Role, Parent: n.Parent, Repo: n.Repo, Spec: n.Spec}
}

// ordered puts lanes in need order (a need state begun before staleBefore
// between held and idle), then start, then path, each followed by its
// children (lanes whose parent is it) in the same order; a lane whose
// parent has no lane is a top lane.
func ordered(lanes []Lane, staleBefore int64) []Lane {
	rank := func(l Lane) int {
		r, ok := needRank[l.State]
		if !ok {
			r = needRank["idle"]
		}
		if r <= needRank["silent"] && l.since < staleBefore {
			return 2*needRank["held"] + 1
		}
		return 2 * r
	}
	sort.Slice(lanes, func(i, j int) bool {
		a, b := lanes[i], lanes[j]
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra < rb
		}
		if a.start != b.start {
			return a.start < b.start
		}
		return a.Path < b.Path
	})
	has := map[string]bool{}
	kids := map[string][]Lane{}
	for _, l := range lanes {
		has[l.Path] = true
	}
	var tops []Lane
	for _, l := range lanes {
		if has[l.Parent] && l.Parent != l.Path {
			kids[l.Parent] = append(kids[l.Parent], l)
		} else {
			tops = append(tops, l)
		}
	}
	out := make([]Lane, 0, len(lanes))
	seen := map[string]bool{}
	var walk func(l Lane)
	walk = func(l Lane) {
		if seen[l.Path] {
			return
		}
		seen[l.Path] = true
		out = append(out, l)
		for _, c := range kids[l.Path] {
			walk(c)
		}
	}
	for _, l := range tops {
		walk(l)
	}
	for _, l := range lanes { // a parent cycle: drawn all the same
		walk(l)
	}
	return out
}

// LeadTimeline is the lanes of path and the nodes under it over path's
// whole life: from the earliest start among them to to, or, once all of
// them are done, to the last of their segments and ticks.
func LeadTimeline(nodes []mesh.Node, states map[string]mesh.State, hist []mesh.Stored, path string, to time.Time) TimelineView {
	under := func(p string) bool { return p == path || strings.HasPrefix(p, path+".") }
	all := Timeline(nodes, states, hist, time.Unix(0, 0), to, 0)
	v := TimelineView{From: to.Unix(), To: to.Unix(), Lanes: []Lane{}}
	end, done := int64(0), true
	for _, l := range all.Lanes {
		if !under(l.Path) {
			continue
		}
		v.Lanes = append(v.Lanes, l)
		v.From = min(v.From, l.Segs[0].From)
		end = max(end, l.Segs[len(l.Segs)-1].To)
		if n := len(l.Ticks); n > 0 {
			end = max(end, l.Ticks[n-1].At)
		}
		done = done && l.State == "done"
	}
	if done && len(v.Lanes) > 0 {
		v.To = end
	}
	return v
}

// history is bm-mesh read whole with the nodes and their states, at most
// once per histFor; histMu, held through the read, lets one read serve
// every request that missed the cache together.
func (s *server) history(r *http.Request) (histEntry, error) {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	now := s.now()
	if s.hist.msgs != nil && now.Sub(s.hist.at) < histFor {
		return s.hist, nil
	}
	ctx := r.Context()
	nodes, err := mesh.ListNodes(ctx, s.c)
	if err != nil {
		return histEntry{}, err
	}
	states, err := mesh.ReadAllStates(ctx, s.c)
	if err != nil {
		return histEntry{}, err
	}
	msgs, err := mesh.History(ctx, s.c, "bm.>", time.Time{})
	if err != nil {
		return histEntry{}, err
	}
	if msgs == nil {
		msgs = []mesh.Stored{}
	}
	s.hist = histEntry{at: now, nodes: nodes, states: states, msgs: msgs}
	return s.hist, nil
}

// timeline is /api/timeline?since=1h|6h|24h|7d (default 6h), or with
// ?lead=<path> that node's and its children's lanes over its life.
func (s *server) timeline(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	span := windows["6h"]
	if v := q.Get("since"); v != "" {
		d, ok := windows[v]
		if !ok {
			http.Error(w, "since: want 1h, 6h, 24h or 7d", http.StatusBadRequest)
			return
		}
		span = d
	}
	h, err := s.history(r)
	if err != nil {
		fail(w, err)
		return
	}
	if p := q.Get("lead"); p != "" {
		writeJSON(w, LeadTimeline(h.nodes, h.states, h.msgs, p, h.at))
		return
	}
	v := Timeline(h.nodes, h.states, h.msgs, h.at.Add(-span), h.at, s.cfg.Serve.StaleAfter)
	if v.Lanes == nil {
		v.Lanes = []Lane{}
	}
	writeJSON(w, v)
}
