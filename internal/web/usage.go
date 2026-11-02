package web

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/usage"
)

const (
	usageFor = 30 * time.Second // how long one /api/usage answer serves its window
	busyGap  = 5 * 60           // a gap this long or longer ends a session's busy run
	liveFor  = 60 * 60          // a session whose last call is this recent is live
)

// roleStep is each window's bucket in seconds.
var roleStep = map[string]int64{"1h": 60, "6h": 600, "24h": 1800, "7d": 10800}

// liveStates are the node states a live agent is in.
var liveStates = map[string]bool{"working": true, "asking": true, "blocked": true, "silent": true, "landing": true, "held": true}

// UsageAgent is one agent's row on the map.
type UsageAgent struct {
	Agent       string       `json:"agent"`
	Role        string       `json:"role"`
	Host        string       `json:"host"`
	Repo        string       `json:"repo"`
	Size        string       `json:"size"`
	State       string       `json:"state"`
	Live        bool         `json:"live"`
	Main        usage.Kinds  `json:"main"`
	Sub         usage.Kinds  `json:"sub"`
	Read        int64        `json:"read"`
	Out         int64        `json:"out"`
	Design      *int64       `json:"design,omitempty"` // a coordinator's read by phase
	Exec        *int64       `json:"exec,omitempty"`
	Peak        int64        `json:"peak"`
	LastCtx     int64        `json:"last_ctx"`
	First       int64        `json:"first"`
	Last        int64        `json:"last"`
	Calls       int          `json:"calls"`
	Compactions int          `json:"compactions"`
	Flags       []usage.Flag `json:"flags"` // the rules it trips (108), the worst first
}

// Roles is the read per bucket of each role series.
type Roles struct {
	Step   int64              `json:"step"`
	From   int64              `json:"from"`
	Series map[string][]int64 `json:"series"`
}

// UsageSession is a coordinator session's busy lane.
type UsageSession struct {
	Agent  string `json:"agent"`
	Host   string `json:"host"`
	ID     string `json:"id"`
	Repo   string `json:"repo"`
	Spans  []Seg  `json:"spans"`
	Design int64  `json:"design"`
	Exec   int64  `json:"exec"`
	Peak   int64  `json:"peak"`
	First  int64  `json:"first"`
	Last   int64  `json:"last"`
}

// Thresholds is this box's [context].
type Thresholds struct {
	NudgeFrom   int `json:"nudge_from"`
	CompactFrom int `json:"compact_from"`
	CompactSoon int `json:"compact_soon"`
}

// UsageView is /api/usage.
type UsageView struct {
	From     int64                `json:"from"`
	To       int64                `json:"to"`
	Read     int64                `json:"read"`
	Out      int64                `json:"out"`
	Hour     int64                `json:"hour"`
	Agents   []UsageAgent         `json:"agents"`
	Roles    Roles                `json:"roles"`
	Sessions []UsageSession       `json:"sessions"`
	Quota    []usage.QuotaReading `json:"quota"`
	Context  Thresholds           `json:"context"`
	Band     []BandRow            `json:"band"`     // the flagged live agents (108)
	Outliers []Outlier            `json:"outliers"` // every lead and fixer with use in 30 days
}

// AgentCall is one call on the context chart; M is minutes since the
// agent's first call, so two agents align on their starts.
type AgentCall struct {
	At    int64   `json:"at"`
	M     float64 `json:"m"`
	Ctx   int64   `json:"ctx"`
	Out   int64   `json:"out"`
	Sub   string  `json:"sub"`
	Phase string  `json:"phase"`
	Cause string  `json:"cause"`
}

// AgentMark is a compaction.
type AgentMark struct {
	At  int64  `json:"at"`
	Sub string `json:"sub"`
}

// AgentEvent is a node's event drawn on its context chart.
type AgentEvent struct {
	At   int64  `json:"at"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// AgentSub is one subagent's totals.
type AgentSub struct {
	ID    string `json:"id"`
	Desc  string `json:"desc"`
	Phase string `json:"phase"`
	Calls int    `json:"calls"`
	Read  int64  `json:"read"`
	Out   int64  `json:"out"`
}

// AgentVs is the agent a lead is compared with.
type AgentVs struct {
	Agent  UsageAgent   `json:"agent"`
	Calls  []AgentCall  `json:"calls"`
	Marks  []AgentMark  `json:"marks"`
	Events []AgentEvent `json:"events"`
}

// AgentView is /api/usage/agent.
type AgentView struct {
	Agent   UsageAgent   `json:"agent"`
	Calls   []AgentCall  `json:"calls"`
	Marks   []AgentMark  `json:"marks"`
	Events  []AgentEvent `json:"events"`
	Context Thresholds   `json:"context"`
	Subs    []AgentSub   `json:"subs"`
	Spans   []Seg        `json:"spans,omitempty"`
	Vs      *AgentVs     `json:"vs,omitempty"`
}

type usageEntry struct {
	at time.Time
	v  UsageView
}

func unix(rfc string) int64 {
	t, err := time.Parse(time.RFC3339Nano, rfc)
	if err != nil {
		return 0
	}
	return t.Unix()
}

// phaseOf is a coordinator call's phase; a call with none counts as design.
func phaseOf(c usage.Call) string {
	if c.Phase == usage.Exec {
		return usage.Exec
	}
	return usage.Design
}

// series is the role chart's series a call of an agent of role falls in.
func series(role string, c usage.Call) string {
	switch role {
	case usage.Coordinator:
		return phaseOf(c)
	case "routine":
		return "routine"
	case "executor":
		return "sub"
	}
	if c.Sub != "" {
		return "sub"
	}
	return "lead"
}

// spans is a session's busy spans: runs of calls under busyGap apart, each
// split where the phase changes.
func spans(calls []usage.Call) []Seg {
	out := []Seg{}
	prev := int64(0)
	for i, c := range calls {
		at, ph := unix(c.At), phaseOf(c)
		n := len(out)
		switch {
		case i == 0 || at-prev >= busyGap:
			out = append(out, Seg{at, at, ph})
		case out[n-1].State != ph:
			out[n-1].To = at
			out = append(out, Seg{at, at, ph})
		default:
			out[n-1].To = at
		}
		prev = at
	}
	return out
}

func (s *server) thresholds() Thresholds {
	c := s.cfg.Context
	return Thresholds{NudgeFrom: c.NudgeFrom, CompactFrom: c.CompactFrom, CompactSoon: c.CompactSoon}
}

// sinceParam is ?since= as a span, def when absent; ok is false on a value
// that is no window, the 400 already written.
func sinceParam(w http.ResponseWriter, r *http.Request, def time.Duration) (string, time.Duration, bool) {
	v := r.URL.Query().Get("since")
	if v == "" {
		return "", def, true
	}
	d, ok := windows[v]
	if !ok {
		http.Error(w, "since: want 1h, 6h, 24h or 7d", http.StatusBadRequest)
	}
	return v, d, ok
}

// readUsage is usage.Read with a hub that has no bm-usage read as no use.
func (s *server) readUsage(ctx context.Context, since time.Time, f usage.Filter) (usage.Report, error) {
	rep, err := usage.Read(ctx, s.c, since, f)
	if errors.Is(err, usage.ErrNoStream) {
		return usage.Report{Since: since, Agents: []usage.Agent{}, ByAgent: map[string][]usage.Call{}}, nil
	}
	return rep, err
}

// agentRow is a's row: its node's state and spec size, or a session's
// liveness at to.
func (s *server) agentRow(ctx context.Context, a usage.Agent, calls []usage.Call, h histEntry, to int64) UsageAgent {
	u := UsageAgent{Agent: a.Agent, Role: a.Role, Host: a.Host, Repo: a.Repo, Main: a.Main, Sub: a.Sub,
		Read: a.Read(), Out: a.Main.Out + a.Sub.Out, Peak: a.Peak, LastCtx: a.LastCtx,
		First: unix(a.First), Last: unix(a.Last), Calls: a.Calls, Compactions: a.Compactions, Flags: []usage.Flag{}}
	if a.Role == usage.Coordinator {
		var d, e int64
		for _, c := range calls {
			if phaseOf(c) == usage.Exec {
				e += c.In + c.CR + c.CW
			} else {
				d += c.In + c.CR + c.CW
			}
		}
		u.Design, u.Exec = &d, &e
	}
	if strings.HasPrefix(a.Agent, "session.") {
		if to-u.Last <= liveFor {
			u.State, u.Live = "live", true
		}
		return u
	}
	u.State = "idle"
	if st, ok := h.states[a.Agent]; ok && st.State != "" {
		u.State = st.State
	}
	u.Live = liveStates[u.State]
	for _, n := range h.nodes {
		if n.Path == a.Agent {
			u.Size = s.specSize(ctx, n)
			break
		}
	}
	return u
}

// specSize is n's spec's frontmatter size from its repo's ledger; "" when
// it has none or the ledger can't be read.
func (s *server) specSize(ctx context.Context, n mesh.Node) string {
	id, err := strconv.Atoi(n.Spec)
	if err != nil {
		return ""
	}
	for _, sp := range s.ledger(ctx, n.Repo) {
		if sp.ID == id {
			return sp.Fields["size"]
		}
	}
	return ""
}

// usageAPI is /api/usage?since=1h|6h|24h|7d (default 6h), one read per window
// per usageFor; usageMu, held through the read, lets one read serve every
// request that missed together.
func (s *server) usageAPI(w http.ResponseWriter, r *http.Request) {
	key, span, ok := sinceParam(w, r, windows["6h"])
	if !ok {
		return
	}
	if key == "" {
		key = "6h"
	}
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	now := s.now()
	if e, ok := s.usageCache[key]; ok && now.Sub(e.at) < usageFor {
		writeJSON(w, e.v)
		return
	}
	v, err := s.usageView(r, now, span, roleStep[key])
	if err != nil {
		fail(w, err)
		return
	}
	s.usageCache[key] = usageEntry{at: now, v: v}
	writeJSON(w, v)
}

func (s *server) usageView(r *http.Request, now time.Time, span time.Duration, step int64) (UsageView, error) {
	ctx := r.Context()
	h, err := s.history(r)
	if err != nil {
		return UsageView{}, err
	}
	from := now.Add(-span)
	rep, err := s.readUsage(ctx, from, usage.Filter{})
	if err != nil {
		return UsageView{}, err
	}
	quota, err := usage.ReadQuota(ctx, s.c, from)
	if err != nil && !errors.Is(err, usage.ErrNoStream) {
		return UsageView{}, err
	}
	f, t := from.Unix(), now.Unix()
	rf := f / step * step
	n := (t - rf + step - 1) / step
	v := UsageView{From: f, To: t, Agents: []UsageAgent{}, Sessions: []UsageSession{}, Quota: quota, Context: s.thresholds(),
		Roles: Roles{Step: step, From: rf, Series: map[string][]int64{}}}
	if v.Quota == nil {
		v.Quota = []usage.QuotaReading{}
	}
	for _, name := range []string{"lead", "sub", usage.Design, usage.Exec, "routine"} {
		v.Roles.Series[name] = make([]int64, n)
	}
	for _, a := range rep.Agents {
		calls := rep.ByAgent[a.Agent]
		row := s.agentRow(ctx, a, calls, h, t)
		v.Agents = append(v.Agents, row)
		for _, c := range calls {
			at, read := unix(c.At), c.In+c.CR+c.CW
			v.Read, v.Out = v.Read+read, v.Out+c.Out
			if at >= t-3600 {
				v.Hour += read
			}
			if i := (at - rf) / step; at >= rf && i < n {
				v.Roles.Series[series(a.Role, c)][i] += read
			}
		}
		if rest, ok := strings.CutPrefix(a.Agent, "session."); ok && a.Role == usage.Coordinator {
			host, id, _ := strings.Cut(rest, ".")
			v.Sessions = append(v.Sessions, UsageSession{Agent: a.Agent, Host: host, ID: id, Repo: a.Repo, Spans: spans(calls),
				Design: *row.Design, Exec: *row.Exec, Peak: a.Peak, First: row.First, Last: row.Last})
		}
	}
	sort.SliceStable(v.Sessions, func(i, j int) bool { return v.Sessions[i].First < v.Sessions[j].First })
	if v.Band, v.Outliers, err = s.flagView(ctx, h, rep, v.Agents, now); err != nil {
		return UsageView{}, err
	}
	return v, nil
}

// agentCalls is calls on the context chart, minutes counted from first.
func agentCalls(calls []usage.Call, first int64) []AgentCall {
	out := make([]AgentCall, 0, len(calls))
	for _, c := range calls {
		at := unix(c.At)
		out = append(out, AgentCall{At: at, M: math.Round(float64(at-first)/6) / 10, Ctx: c.Ctx, Out: c.Out,
			Sub: c.Sub, Phase: c.Phase, Cause: c.Cause})
	}
	return out
}

func agentMarks(marks []usage.Mark) []AgentMark {
	out := make([]AgentMark, 0, len(marks))
	for _, m := range marks {
		out = append(out, AgentMark{At: unix(m.At), Sub: m.Sub})
	}
	return out
}

// agentEvents is path's progress, ready, refused and done in [from, to];
// [] for a session.
func agentEvents(h histEntry, path string, from, to int64) []AgentEvent {
	out := []AgentEvent{}
	if strings.HasPrefix(path, "session.") {
		return out
	}
	for _, e := range laneEvents(h.msgs) {
		k, at := tickKind[e.name], e.at.Unix()
		if e.path != path || k == "" || k == "job" || at < from || at > to {
			continue
		}
		out = append(out, AgentEvent{At: at, Kind: k, Text: e.text})
	}
	return out
}

// usageAgent is /api/usage/agent?agent=<path|session.<host>.<id>>
// [&since=1h|6h|24h|7d][&vs=<agent>]: one agent's calls (the last
// usage.Keep without since), and with vs a second agent's beside them.
func (s *server) usageAgent(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("agent")
	if name == "" {
		http.Error(w, "agent: required", http.StatusBadRequest)
		return
	}
	_, span, ok := sinceParam(w, r, usage.Keep)
	if !ok {
		return
	}
	ctx := r.Context()
	h, err := s.history(r)
	if err != nil {
		fail(w, err)
		return
	}
	now := s.now()
	from := now.Add(-span)
	read := func(agent string) (usage.Report, bool, error) {
		rep, err := s.readUsage(ctx, from, usage.Filter{Agent: agent})
		return rep, err == nil && len(rep.Agents) > 0, err
	}
	rep, found, err := read(name)
	if err != nil {
		fail(w, err)
		return
	}
	if !found {
		http.Error(w, fmt.Sprintf("no usage for %s since %s", name, from.UTC().Format(time.RFC3339)), http.StatusNotFound)
		return
	}
	a := rep.Agents[0]
	row := s.agentRow(ctx, a, rep.Calls, h, now.Unix())
	v := AgentView{Agent: row, Calls: agentCalls(rep.Calls, row.First), Marks: agentMarks(rep.Marks),
		Events: agentEvents(h, a.Agent, from.Unix(), now.Unix()), Context: s.thresholds(), Subs: []AgentSub{}}
	for _, sb := range a.Subs {
		x := AgentSub{ID: sb.ID, Desc: sb.Desc, Calls: sb.Calls, Read: sb.Read(), Out: sb.Out}
		for _, c := range rep.Calls {
			if c.Sub == sb.ID {
				x.Phase = c.Phase
				break
			}
		}
		v.Subs = append(v.Subs, x)
	}
	if strings.HasPrefix(a.Agent, "session.") {
		v.Spans = spans(rep.Calls)
	}
	if vs := r.URL.Query().Get("vs"); vs != "" {
		vrep, found, err := read(vs)
		if err != nil {
			fail(w, err)
			return
		}
		if found {
			b := vrep.Agents[0]
			brow := s.agentRow(ctx, b, vrep.Calls, h, now.Unix())
			v.Vs = &AgentVs{Agent: brow, Calls: agentCalls(vrep.Calls, brow.First), Marks: agentMarks(vrep.Marks),
				Events: agentEvents(h, b.Agent, from.Unix(), now.Unix())}
		}
	}
	writeJSON(w, v)
}
