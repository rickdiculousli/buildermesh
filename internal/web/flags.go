package web

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/usage"
)

// baselineFor is how long one read of the stream's 30 days serves the
// baselines and the outliers (108).
const baselineFor = time.Hour

type baseEntry struct {
	at time.Time
	br usage.BaselineRead
}

// BandRow is a flagged live agent in the needs-you band.
type BandRow struct {
	Agent string       `json:"agent"`
	Role  string       `json:"role"`
	Level string       `json:"level"`
	Flags []usage.Flag `json:"flags"`
}

// Outlier is one lead or fixer with use in the stream's 30 days, a point
// on the outliers chart; Vs is the done run of its baseline nearest that
// baseline's median read, for the lead page's compare.
type Outlier struct {
	Agent string       `json:"agent"`
	Role  string       `json:"role"`
	Repo  string       `json:"repo"`
	Size  string       `json:"size"`
	Live  bool         `json:"live"`
	Read  int64        `json:"read"`
	Wall  int64        `json:"wall"` // seconds
	Peak  int64        `json:"peak"`
	Turns int          `json:"turns"` // main-thread calls
	Level string       `json:"level"`
	Flags []usage.Flag `json:"flags"`
	Vs    string       `json:"vs"`
}

// baselines is usage.LoadBaselines, read at most once per baselineFor; a
// hub with no bm-usage has none.
func (s *server) baselines(ctx context.Context) (usage.BaselineRead, error) {
	s.baseMu.Lock()
	defer s.baseMu.Unlock()
	now := s.now()
	if !s.base.at.IsZero() && now.Sub(s.base.at) < baselineFor {
		return s.base.br, nil
	}
	br, err := usage.LoadBaselines(ctx, s.c, now, func(n mesh.Node) string { return s.specSize(ctx, n) })
	if errors.Is(err, usage.ErrNoStream) {
		br, err = usage.BaselineRead{Baselines: usage.Baselines{}, Report: usage.Report{ByAgent: map[string][]usage.Call{}}}, nil
	}
	if err != nil {
		return br, err
	}
	s.base = baseEntry{at: now, br: br}
	return br, nil
}

// turns is a's main-thread calls.
func turns(a usage.Agent) int {
	n := a.Calls
	for _, sb := range a.Subs {
		n -= sb.Calls
	}
	return n
}

// flagView is what the flags add to /api/usage: each window row's flags
// (set in place), the band and the outliers. A live lead's or fixer's
// subject is read fresh over its life; a done one's from the 30 days'
// read; every other agent's from the window, which holds its last hour.
func (s *server) flagView(ctx context.Context, h histEntry, rep usage.Report, rows []UsageAgent, now time.Time) ([]BandRow, []Outlier, error) {
	br, err := s.baselines(ctx)
	if err != nil {
		return nil, nil, err
	}
	refused := usage.Refusals(h.msgs)
	soon := s.cfg.Context.CompactSoon
	node := map[string]mesh.Node{}
	for _, n := range h.nodes {
		node[n.Path] = n
	}
	life := map[string]usage.Agent{} // the subject's agent over its life
	lifeCalls := map[string][]usage.Call{}
	for _, a := range br.Report.Agents {
		life[a.Agent], lifeCalls[a.Agent] = a, br.Report.ByAgent[a.Agent]
	}
	live := func(path string) bool {
		_, ok := node[path]
		return ok && h.states[path].State != "done"
	}
	worker := func(role string) bool { return role == "lead" || role == "fixer" }
	for _, n := range h.nodes {
		if !worker(n.Role) || !live(n.Path) {
			continue
		}
		from := now.Add(-usage.Keep)
		if t, err := time.Parse(time.RFC3339, n.StartedAt); err == nil && t.After(from) {
			from = t
		}
		lr, err := s.readUsage(ctx, from, usage.Filter{Agent: n.Path})
		if err != nil {
			return nil, nil, err
		}
		if len(lr.Agents) > 0 {
			life[n.Path], lifeCalls[n.Path] = lr.Agents[0], lr.Calls
		}
	}
	subject := func(a usage.Agent, calls []usage.Call) usage.Subject {
		size := ""
		if n, ok := node[a.Agent]; ok {
			a.Role, a.Repo = n.Role, n.Repo
			size = s.specSize(ctx, n)
		}
		return usage.SubjectOf(a, calls, size, refused[a.Agent], now)
	}
	flagged := map[string][]usage.Flag{}
	judge := func(a usage.Agent, calls []usage.Call) []usage.Flag {
		if fs, ok := flagged[a.Agent]; ok {
			return fs
		}
		fs := usage.Flags(subject(a, calls), br.Baselines, soon)
		if fs == nil {
			fs = []usage.Flag{}
		}
		flagged[a.Agent] = fs
		return fs
	}
	byAgent := map[string]usage.Agent{}
	for _, a := range rep.Agents {
		byAgent[a.Agent] = a
	}
	band := []BandRow{}
	for i := range rows {
		a, calls := byAgent[rows[i].Agent], rep.ByAgent[rows[i].Agent]
		if l, ok := life[a.Agent]; ok && worker(rows[i].Role) && l.Read() >= a.Read() {
			a, calls = l, lifeCalls[a.Agent]
			if !live(a.Agent) {
				// a done run's context and last hour are the window's
				a.LastCtx, calls = byAgent[a.Agent].LastCtx, rep.ByAgent[a.Agent]
			}
		}
		rows[i].Flags = judge(a, calls)
		isLive := rows[i].Live || !strings.HasPrefix(a.Agent, "session.") && live(a.Agent)
		if isLive && len(rows[i].Flags) > 0 {
			band = append(band, BandRow{Agent: a.Agent, Role: rows[i].Role, Level: usage.Worst(rows[i].Flags), Flags: rows[i].Flags})
		}
	}
	// A live lead or fixer quiet through the window still holds its band row.
	for path, a := range life {
		if _, inWindow := byAgent[path]; inWindow || !live(path) || !worker(node[path].Role) {
			continue
		}
		if fs := judge(a, lifeCalls[path]); len(fs) > 0 {
			band = append(band, BandRow{Agent: path, Role: node[path].Role, Level: usage.Worst(fs), Flags: fs})
		}
	}
	sort.SliceStable(band, func(i, j int) bool {
		if (band[i].Level == usage.Runaway) != (band[j].Level == usage.Runaway) {
			return band[i].Level == usage.Runaway
		}
		return band[i].Agent < band[j].Agent
	})
	return band, s.outliers(ctx, br, h, life, judge, lifeCalls, now), nil
}

// outliers are the chart's points: every lead and fixer with use in the
// 30 days.
func (s *server) outliers(ctx context.Context, br usage.BaselineRead, h histEntry, life map[string]usage.Agent,
	judge func(usage.Agent, []usage.Call) []usage.Flag, lifeCalls map[string][]usage.Call, now time.Time) []Outlier {
	out := []Outlier{}
	// vs per baseline: the done run nearest its median read.
	vs, near := map[string]string{}, map[string]int64{}
	key := func(role, repo, size string) (usage.Median, string, bool) {
		m, scope, ok := br.Baselines.For(role, repo, size)
		return m, role + "/" + size + "/" + scope, ok
	}
	for _, r := range br.Runs {
		m, k, ok := key(r.Role, r.Repo, r.Size)
		if !ok {
			continue
		}
		if d := abs(r.Read - m.Read); vs[k] == "" || d < near[k] {
			vs[k], near[k] = r.Agent, d
		}
	}
	for _, n := range h.nodes {
		a, ok := life[n.Path]
		if !ok || n.Role != "lead" && n.Role != "fixer" {
			continue
		}
		st := h.states[n.Path]
		o := Outlier{Agent: n.Path, Role: n.Role, Repo: n.Repo, Live: st.State != "done", Read: a.Read(), Peak: a.Peak,
			Turns: turns(a), Flags: judge(a, lifeCalls[n.Path])}
		if n.Role == "lead" {
			o.Size = s.specSize(ctx, n)
		}
		o.Wall = usage.Wall(n, a, st)
		if o.Live {
			if t, err := time.Parse(time.RFC3339, n.StartedAt); err == nil {
				o.Wall = max(0, int64(now.Sub(t).Seconds()))
			}
		}
		o.Level = usage.Worst(o.Flags)
		if _, k, ok := key(n.Role, n.Repo, o.Size); ok {
			o.Vs = vs[k]
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
