package web

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/usage"
)

const (
	landingFor   = 3 * time.Second     // how long one /api/landing answer serves
	landQuiet    = 150 * time.Second   // a run with no end and no event this long is quiet (two heartbeats)
	usualMin     = 3                   // the samples a median needs to be shown
	jobsLookBack = time.Hour           // a job taken before the landing began (queued with the ready) still shows
	landRead     = 30 * 24 * time.Hour // how far back a node's land events are read: the stream's age
)

var (
	jobMsRE       = regexp.MustCompile(`^job (\S+) (ok|fail) (\d+)`)
	leaseHolderRE = regexp.MustCompile(`\(held by (\S+)\)$`)
	kindsTextRE   = regexp.MustCompile(`^checking (.+) on t:`)
)

// LandingView is one node's landing on /api/landing: the land runs since
// it began (its newest ready, or the run after one that ended other than
// by timing out), the newest event's land object (now), its text and at,
// the jobs the runs waited on, the repo's usual time per step and per
// review job, and while it waits for the lease, the holder.
type LandingView struct {
	Path        string        `json:"path"`
	Repo        string        `json:"repo"`
	State       string        `json:"state"`
	ReadyAt     string        `json:"ready_at,omitempty"`
	Text        string        `json:"text"`
	At          string        `json:"at,omitempty"`
	Running     bool          `json:"running"`
	Runs        []LandRun     `json:"runs"`
	Now         *mesh.Land    `json:"now"`
	Jobs        []LandingJob  `json:"jobs"`
	Usual       []Usual       `json:"usual"`
	ReviewUsual *Usual        `json:"review_usual"`
	Lease       *LandingLease `json:"lease"`
}

// LandRun is one land process: its start, how it ended (none while it
// runs or after it was killed), Quiet the seconds since its newest event
// when it has no end and has been silent past landQuiet, and its step rows.
type LandRun struct {
	Run   string     `json:"run"`
	End   string     `json:"end,omitempty"`
	Quiet int64      `json:"quiet,omitempty"`
	Steps []LandStep `json:"steps"`
}

// LandStep is one stay in a step: its label (a kinds row its kinds, a
// jobs row its jobs' kinds or review), when it began and ended (none while
// it is the run's current step), and the text of the event it began with.
type LandStep struct {
	Step  string `json:"step"`
	Label string `json:"label"`
	From  string `json:"from"`
	To    string `json:"to,omitempty"`
	Text  string `json:"text"`
}

// LandingJob is a job the runs waited on: the instance that took it, its
// box, model and effort, its state (waiting, running, ok or fail), and the
// instance's model calls since the job started (to its end, once ended):
// how many, what they read, the newest context, and the newest call's
// cause and time.
type LandingJob struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	State    string `json:"state"`
	Instance string `json:"instance,omitempty"`
	Host     string `json:"host,omitempty"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
	Started  string `json:"started,omitempty"`
	Ended    string `json:"ended,omitempty"`
	Ms       int64  `json:"ms,omitempty"`
	Calls    int    `json:"calls"`
	Read     int64  `json:"read"`
	Ctx      int64  `json:"ctx"`
	Cause    string `json:"cause,omitempty"`
	CauseAt  string `json:"cause_at,omitempty"`
}

// Usual is the median seconds of a step and label over the repo's land
// runs that went past it (N of them), or of its review jobs.
type Usual struct {
	Step  string `json:"step,omitempty"`
	Label string `json:"label,omitempty"`
	Secs  int64  `json:"secs"`
	N     int    `json:"n"`
}

// LandingLease is the land lease's holder and, when it is a node, its
// newest land text.
type LandingLease struct {
	Holder string `json:"holder"`
	Text   string `json:"text,omitempty"`
}

type landingEntry struct {
	at   time.Time
	body any
	code int
}

// landEv is one land event: its at, text and land object (nil from a land
// before 121).
type landEv struct {
	at   time.Time
	text string
	land *mesh.Land
}

func landEvs(msgs []mesh.Stored) []landEv {
	out := make([]landEv, 0, len(msgs))
	for _, m := range msgs {
		var ev mesh.Event
		if json.Unmarshal(m.Data, &ev) != nil {
			continue
		}
		at := m.At
		if t, err := time.Parse(time.RFC3339, ev.At); err == nil {
			at = t
		}
		out = append(out, landEv{at: at, text: ev.Text, land: ev.Land})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

func parseAt(s string, def time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return def
}

// byRun groups evs (with a land object) by run, in order.
func byRun(evs []landEv) [][]landEv {
	var out [][]landEv
	idx := map[string]int{}
	for _, e := range evs {
		if e.land == nil {
			continue
		}
		i, ok := idx[e.land.Run]
		if !ok {
			i = len(out)
			idx[e.land.Run] = i
			out = append(out, nil)
		}
		out[i] = append(out[i], e)
	}
	return out
}

// stepRow is a LandStep with its times and whether the run went past it.
type stepRow struct {
	LandStep
	from, to time.Time
	past     bool
	kinds    []string
	jobs     []string
	waited   bool // a main row: the run waited for main to be pushed
}

// runRows is a run's step rows: a row per stay in a step (a step taken
// twice is two rows), a main row only when the run waited there or is
// there now.
func runRows(evs []landEv) []stepRow {
	var rows []stepRow
	for _, e := range evs {
		l := e.land
		if n := len(rows); n == 0 || rows[n-1].Step != l.Step || rows[n-1].From != l.Since {
			from := parseAt(l.Since, e.at)
			if n > 0 {
				rows[n-1].to, rows[n-1].To, rows[n-1].past = from, l.Since, true
			}
			rows = append(rows, stepRow{LandStep: LandStep{Step: l.Step, From: l.Since, Text: e.text}, from: from})
		}
		r := &rows[len(rows)-1]
		if c := l.Cmd; c != nil && c.Kind != "setup" && !slices.Contains(r.kinds, c.Kind) {
			r.kinds = append(r.kinds, c.Kind)
		}
		for _, j := range l.Jobs {
			if !slices.Contains(r.jobs, j.Kind) {
				r.jobs = append(r.jobs, j.Kind)
			}
		}
		if l.Step == mesh.LandMain && strings.HasPrefix(e.text, "waiting for main") {
			r.waited = true
		}
	}
	if len(rows) == 0 {
		return rows
	}
	last := evs[len(evs)-1]
	if end := last.land.End; end != "" {
		r := &rows[len(rows)-1]
		r.to, r.To, r.past = last.at, last.at.UTC().Format(time.RFC3339), strings.HasPrefix(end, "landed ")
	}
	out := rows[:0]
	for i, r := range rows {
		r.Label = rowLabel(r)
		if r.Step == mesh.LandMain && !r.waited && !(i == len(rows)-1 && last.land.End == "") {
			continue
		}
		out = append(out, r)
	}
	return out
}

// rowLabel is a kinds row's kinds, a jobs row's jobs' kinds (review when
// none is waited on), else the step.
func rowLabel(r stepRow) string {
	switch r.Step {
	case mesh.LandKinds:
		if len(r.kinds) > 0 {
			return strings.Join(r.kinds, ", ")
		}
		if m := kindsTextRE.FindStringSubmatch(r.Text); m != nil {
			return m[1]
		}
	case mesh.LandJobs:
		if len(r.jobs) > 0 {
			return strings.Join(r.jobs, ", ")
		}
		return "review"
	}
	return r.Step
}

// landingRuns is the runs of the node's newest landing among evs: those
// from its newest ready (readyAt, zero for none) on, starting again after
// a run that ended other than by timing out (the lead landing in its own
// turn); fromReady says the landing began at the ready.
func landingRuns(evs []landEv, readyAt time.Time) (runs [][]landEv, fromReady bool) {
	all := byRun(evs)
	from, fromReady := 0, !readyAt.IsZero()
	for i, r := range all {
		if fromReady && parseAt(r[0].land.Run, r[0].at).Before(readyAt.Truncate(time.Second)) {
			from = i + 1
			continue
		}
		if i > from {
			if end := all[i-1][len(all[i-1])-1].land.End; end != "" && !strings.HasPrefix(end, "timed out") {
				from, fromReady = i, false
			}
		}
	}
	return all[from:], fromReady
}

// landing builds path's newest landing from its land events, its ready,
// the routine events and usage, and the history; ok is false when the node
// has neither landed nor asked to.
func (s *server) landing(ctx context.Context, n mesh.Node, st mesh.State, h histEntry) (LandingView, bool, error) {
	now := s.now()
	p, err := mesh.ParsePath(n.Path)
	if err != nil {
		return LandingView{}, false, nil
	}
	msgs, err := mesh.History(ctx, s.c, mesh.EventSubject(p, "land"), now.Add(-landRead))
	if err != nil {
		return LandingView{}, false, err
	}
	evs := landEvs(msgs)
	var readyAt time.Time
	if m, ok, err := mesh.Last(ctx, s.c, mesh.EventSubject(p, "ready")); err != nil {
		return LandingView{}, false, err
	} else if ok {
		readyAt = landEvs([]mesh.Stored{m})[0].at
	}
	if len(evs) == 0 && readyAt.IsZero() {
		return LandingView{}, false, nil
	}
	v := LandingView{Path: n.Path, Repo: n.Repo, State: st.State, Runs: []LandRun{}, Jobs: []LandingJob{}, Usual: []Usual{}}
	if len(evs) > 0 {
		last := evs[len(evs)-1]
		v.Text, v.At = last.text, last.at.UTC().Format(time.RFC3339)
		if last.land == nil { // a land before 121: its text alone
			v.Running = now.Sub(last.at) <= landQuiet
			return v, true, nil
		}
	}
	runs, fromReady := landingRuns(evs, readyAt)
	if fromReady {
		v.ReadyAt = readyAt.UTC().Format(time.RFC3339)
	}
	var waited []mesh.LandJob
	for i, r := range runs {
		lr := LandRun{Run: r[0].land.Run, End: r[len(r)-1].land.End, Steps: []LandStep{}}
		for _, row := range runRows(r) {
			lr.Steps = append(lr.Steps, row.LandStep)
		}
		if i == len(runs)-1 && lr.End == "" {
			if age := now.Sub(r[len(r)-1].at); age > landQuiet {
				lr.Quiet = int64(age / time.Second)
			} else {
				v.Running = true
			}
		}
		for _, e := range r {
			for _, j := range e.land.Jobs {
				if !slices.ContainsFunc(waited, func(w mesh.LandJob) bool { return w.ID == j.ID }) {
					waited = append(waited, j)
				}
			}
		}
		v.Runs = append(v.Runs, lr)
	}
	if len(runs) > 0 {
		r := runs[len(runs)-1]
		l := *r[len(r)-1].land
		if l.Cmd != nil && l.Cmd.Command == "" {
			// land sends the command once, on the event it started in.
			c := *l.Cmd
			for _, e := range r {
				if e.land.Cmd != nil && e.land.Cmd.Kind == c.Kind && e.land.Cmd.Since == c.Since && e.land.Cmd.Command != "" {
					c.Command = e.land.Cmd.Command
				}
			}
			l.Cmd = &c
		}
		v.Now = &l
		v.Text, v.At = r[len(r)-1].text, r[len(r)-1].at.UTC().Format(time.RFC3339)
	}
	began := readyAt
	if !fromReady && len(runs) > 0 {
		began = parseAt(runs[0][0].land.Run, runs[0][0].at)
	}
	if v.Jobs, err = s.landingJobs(ctx, n.Path, began, waited, h); err != nil {
		return LandingView{}, false, err
	}
	v.Usual, v.ReviewUsual = usuals(h, n.Repo)
	if v.Now != nil && v.Now.Step == mesh.LandLease && v.Now.End == "" {
		if m := leaseHolderRE.FindStringSubmatch(v.Text); m != nil {
			v.Lease = &LandingLease{Holder: m[1]}
			if hp, err := mesh.ParsePath(m[1]); err == nil {
				if lm, ok, err := mesh.Last(ctx, s.c, mesh.EventSubject(hp, "land")); err == nil && ok {
					v.Lease.Text = landEvs([]mesh.Stored{lm})[0].text
				}
			}
		}
	}
	return v, true, nil
}

// landingJobs is each job waited on with the routine instance that took
// it ("job <id> started", addressed to path), how it ended ("job <id>
// ok|fail <ms>"), and the instance's calls between.
func (s *server) landingJobs(ctx context.Context, path string, began time.Time, waited []mesh.LandJob, h histEntry) ([]LandingJob, error) {
	out := []LandingJob{}
	if len(waited) == 0 {
		return out, nil
	}
	from := began.Add(-jobsLookBack)
	if began.IsZero() {
		from = s.now().Add(-landRead)
	}
	msgs, err := mesh.History(ctx, s.c, "bm.routine.>", from)
	if err != nil {
		return nil, err
	}
	byID := map[string]*LandingJob{}
	for _, w := range waited {
		out = append(out, LandingJob{ID: w.ID, Kind: w.Kind, State: "waiting"})
	}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	for _, m := range msgs {
		var ev mesh.Event
		if json.Unmarshal(m.Data, &ev) != nil || ev.Event != "progress" || ev.To != path {
			continue
		}
		at := parseAt(ev.At, m.At).UTC().Format(time.RFC3339)
		if sm := jobStartRE.FindStringSubmatch(ev.Text); sm != nil {
			if j := byID[sm[1]]; j != nil {
				j.Instance, j.Started, j.State, j.Ended, j.Ms = ev.From, at, "running", "", 0
			}
		} else if sm := jobMsRE.FindStringSubmatch(ev.Text); sm != nil {
			if j := byID[sm[1]]; j != nil {
				ms, _ := strconv.ParseInt(sm[3], 10, 64)
				j.State, j.Ended, j.Ms = sm[2], at, ms
			}
		}
	}
	nodes := map[string]mesh.Node{}
	for _, n := range h.nodes {
		nodes[n.Path] = n
	}
	for i := range out {
		j := &out[i]
		if job, ok, err := mesh.FindJob(ctx, s.c, j.Kind, j.ID); err == nil && ok {
			j.Effort = job.Effort
		}
		if j.Instance == "" {
			continue
		}
		if n, ok := nodes[j.Instance]; ok {
			j.Host, j.Model = n.Host, n.Model
		}
		start := parseAt(j.Started, time.Time{})
		rep, err := s.readUsage(ctx, start, usage.Filter{Agent: j.Instance})
		if err != nil {
			return nil, err
		}
		end := parseAt(j.Ended, time.Time{})
		for _, c := range rep.Calls {
			at, _ := time.Parse(time.RFC3339Nano, c.At)
			if !end.IsZero() && at.After(end) {
				continue
			}
			j.Calls++
			j.Read += c.In + c.CR + c.CW
			if c.Sub == "" {
				j.Ctx = c.Ctx
			}
			j.Cause, j.CauseAt = c.Cause, at.UTC().Format(time.RFC3339)
		}
	}
	return out, nil
}

// usuals is the repo's median seconds per step and label over its land
// runs in the history that went past it (each run's repeats summed), and
// the median of its review jobs' times; each only once it has usualMin
// samples.
func usuals(h histEntry, repo string) ([]Usual, *Usual) {
	repoOf := map[string]string{}
	for _, n := range h.nodes {
		repoOf[n.Path] = n.Repo
	}
	byPath := map[string][]mesh.Stored{}
	var review []int64
	for _, m := range h.msgs {
		rest, ok := strings.CutPrefix(m.Subject, "bm.")
		if !ok {
			continue
		}
		i := strings.LastIndex(rest, ".")
		if i <= 0 {
			continue
		}
		path, name := rest[:i], rest[i+1:]
		if repoOf[path] != repo {
			continue
		}
		switch {
		case name == "land" && !strings.HasPrefix(path, "routine.") && !strings.HasPrefix(path, "daemon."):
			byPath[path] = append(byPath[path], m)
		case name == "progress" && strings.HasPrefix(path, "routine.review."):
			var ev mesh.Event
			if json.Unmarshal(m.Data, &ev) != nil {
				continue
			}
			if sm := jobMsRE.FindStringSubmatch(ev.Text); sm != nil {
				ms, _ := strconv.ParseInt(sm[3], 10, 64)
				review = append(review, ms)
			}
		}
	}
	type key struct{ step, label string }
	samples := map[key][]int64{}
	var order []key
	for _, msgs := range byPath {
		for _, r := range byRun(landEvs(msgs)) {
			sum := map[key]int64{}
			for _, row := range runRows(r) {
				if !row.past {
					continue
				}
				k := key{row.Step, row.Label}
				sum[k] += int64(row.to.Sub(row.from) / time.Second)
			}
			for k, v := range sum {
				if samples[k] == nil {
					order = append(order, k)
				}
				samples[k] = append(samples[k], v)
			}
		}
	}
	out := []Usual{}
	for _, k := range order {
		if v := samples[k]; len(v) >= usualMin {
			out = append(out, Usual{Step: k.step, Label: k.label, Secs: median(v), N: len(v)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Step < out[j].Step || out[i].Step == out[j].Step && out[i].Label < out[j].Label
	})
	var rv *Usual
	if len(review) >= usualMin {
		rv = &Usual{Secs: (median(review) + 500) / 1000, N: len(review)}
	}
	return out, rv
}

// median is the middle of v (the mean of the two middle ones for an even
// count); v is sorted in place.
func median(v []int64) int64 {
	slices.Sort(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

// landingAPI is /api/landing: every node with a running land run (or a
// quiet one while it is landing), or with ?lead=<path> that node's newest
// landing whatever its state (404 when it has none). Cached landingFor.
func (s *server) landingAPI(w http.ResponseWriter, r *http.Request) {
	lead := r.URL.Query().Get("lead")
	s.landingMu.Lock()
	defer s.landingMu.Unlock()
	now := s.now()
	if e, ok := s.landingCache[lead]; ok && now.Sub(e.at) < landingFor {
		writeLanding(w, e)
		return
	}
	e, err := s.landingRead(r, lead)
	if err != nil {
		fail(w, err)
		return
	}
	e.at = now
	s.landingCache[lead] = e
	writeLanding(w, e)
}

func writeLanding(w http.ResponseWriter, e landingEntry) {
	if e.code == http.StatusNotFound {
		http.Error(w, e.body.(string), http.StatusNotFound)
		return
	}
	writeJSON(w, e.body)
}

func (s *server) landingRead(r *http.Request, lead string) (landingEntry, error) {
	ctx := r.Context()
	h, err := s.history(r)
	if err != nil {
		return landingEntry{}, err
	}
	nodes, err := mesh.ListNodes(ctx, s.c)
	if err != nil {
		return landingEntry{}, err
	}
	states, err := mesh.ReadAllStates(ctx, s.c)
	if err != nil {
		return landingEntry{}, err
	}
	if lead != "" {
		i := slices.IndexFunc(nodes, func(n mesh.Node) bool { return n.Path == lead })
		if i < 0 {
			return landingEntry{code: http.StatusNotFound, body: "no node " + lead}, nil
		}
		v, ok, err := s.landing(ctx, nodes[i], states[lead], h)
		if err != nil {
			return landingEntry{}, err
		}
		if !ok {
			return landingEntry{code: http.StatusNotFound, body: lead + " has not landed"}, nil
		}
		return landingEntry{body: v}, nil
	}
	out := []LandingView{}
	for _, n := range nodes {
		st := states[n.Path]
		if n.Role == "routine" || st.State == "done" {
			continue
		}
		p, err := mesh.ParsePath(n.Path)
		if err != nil {
			continue
		}
		m, ok, err := mesh.Last(ctx, s.c, mesh.EventSubject(p, "land"))
		if err != nil {
			return landingEntry{}, err
		}
		if !ok {
			continue
		}
		e := landEvs([]mesh.Stored{m})[0]
		fresh := s.now().Sub(e.at) <= landQuiet
		if e.land != nil && e.land.End != "" || !fresh && (e.land == nil || st.State != "landing") {
			continue
		}
		v, ok, err := s.landing(ctx, n, st, h)
		if err != nil {
			return landingEntry{}, err
		}
		if ok {
			out = append(out, v)
		}
	}
	return landingEntry{body: out}, nil
}
