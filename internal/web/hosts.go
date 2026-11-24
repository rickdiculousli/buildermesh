package web

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/hoststat"
	"github.com/rickdiculousli/buildermesh/internal/usage"
)

const (
	hostsFor   = 30 * time.Second // how long one window's history read serves
	liveWait   = 2 * time.Second  // every box's live answer, in all
	liveRecent = 3 * time.Minute  // a box with a minute this recent is asked live
)

// HostPoint is a bucket of a box's minutes: the means over the minutes in
// it (an agent missing from a minute counts 0 there, so the agents and
// other sum to the box), the pressure means over the minutes that have
// one, and the worst macOS pressure level.
type HostPoint struct {
	At          int64                   `json:"at"`
	N           int                     `json:"n"`
	CPU         float64                 `json:"cpu"`
	Load1       float64                 `json:"load1"`
	MemUsed     int64                   `json:"mem_used"`
	SwapUsed    int64                   `json:"swap_used"`
	Pressure    *hoststat.Pressure      `json:"pressure"`
	MacPressure *int                    `json:"mac_pressure"`
	Agents      map[string]hoststat.Use `json:"agents"`
}

// HostBox is one box on /api/hosts: its newest minute in the window, its
// buckets, and with live=1 its live answer (null when it gave none).
type HostBox struct {
	Host   string           `json:"host"`
	Last   *hoststat.Minute `json:"last"`
	Points []HostPoint      `json:"points"`
	Live   json.RawMessage  `json:"live,omitempty"`
}

// HostsView is /api/hosts: the window, its bucket step, this box and every
// box with a minute in the window (this box first, then by name).
type HostsView struct {
	From  int64     `json:"from"`
	To    int64     `json:"to"`
	Step  int64     `json:"step"`
	Host  string    `json:"host"`
	Boxes []HostBox `json:"boxes"`
}

// hostUse is an agent's minutes in a window, for the agents table: its
// CPU summed per minute and its memory per minute, across boxes.
type hostUse struct {
	cpu map[int64]float64
	rss map[int64]int64
}

type hostsEntry struct {
	at   time.Time
	v    HostsView
	uses map[string]*hostUse
}

// hostsData is the window key's history read, once per hostsFor;
// hostsMu, held through the read, lets one read serve every request that
// missed together.
func (s *server) hostsData(ctx context.Context, key string) (hostsEntry, error) {
	s.hostsMu.Lock()
	defer s.hostsMu.Unlock()
	now := s.now()
	if e, ok := s.hostsCache[key]; ok && now.Sub(e.at) < hostsFor {
		return e, nil
	}
	step := roleStep[key]
	from := now.Add(-windows[key])
	ms, err := hoststat.ReadHistory(ctx, s.c, from)
	if err != nil && !errors.Is(err, usage.ErrNoStream) {
		return hostsEntry{}, err
	}
	e := hostsEntry{at: now, uses: map[string]*hostUse{},
		v: HostsView{From: from.Unix(), To: now.Unix(), Step: step, Host: s.host, Boxes: []HostBox{}}}
	rf := from.Unix() / step * step
	type acc struct {
		box     *HostBox
		buckets map[int64]*bucketAcc
	}
	boxes := map[string]*acc{}
	for i := range ms {
		m := &ms[i]
		t := unix(m.At)
		b := boxes[m.Host]
		if b == nil {
			b = &acc{box: &HostBox{Host: m.Host, Points: []HostPoint{}}, buckets: map[int64]*bucketAcc{}}
			boxes[m.Host] = b
		}
		if b.box.Last == nil || m.At > b.box.Last.At {
			b.box.Last = m
		}
		at := rf + (t-rf)/step*step
		if b.buckets[at] == nil {
			b.buckets[at] = &bucketAcc{}
		}
		b.buckets[at].add(m)
		for a, u := range m.Agents {
			if a == hoststat.Other {
				continue
			}
			h := e.uses[a]
			if h == nil {
				h = &hostUse{cpu: map[int64]float64{}, rss: map[int64]int64{}}
				e.uses[a] = h
			}
			h.cpu[t/60] += u.CPU
			h.rss[t/60] += u.RSS
		}
	}
	if _, ok := boxes[s.host]; !ok && s.host != "" {
		boxes[s.host] = &acc{box: &HostBox{Host: s.host, Points: []HostPoint{}}}
	}
	for _, b := range boxes {
		for at, ba := range b.buckets {
			b.box.Points = append(b.box.Points, ba.point(at))
		}
		sort.Slice(b.box.Points, func(i, j int) bool { return b.box.Points[i].At < b.box.Points[j].At })
		e.v.Boxes = append(e.v.Boxes, *b.box)
	}
	sort.Slice(e.v.Boxes, func(i, j int) bool {
		a, b := e.v.Boxes[i].Host, e.v.Boxes[j].Host
		return a == s.host || b != s.host && a < b
	})
	s.hostsCache[key] = e
	return e, nil
}

// bucketAcc sums a bucket's minutes.
type bucketAcc struct {
	n         int
	cpu, load float64
	mem, swap int64
	pr        [3]float64
	pn        [3]int
	mac       *int
	agents    map[string]hoststat.Use
}

func (b *bucketAcc) add(m *hoststat.Minute) {
	b.n++
	b.cpu += m.CPU
	b.load += m.Load1
	b.mem += m.MemUsed
	b.swap += m.SwapUsed
	if p := m.Pressure; p != nil {
		for i, v := range []*float64{p.CPU, p.Mem, p.IO} {
			if v != nil {
				b.pr[i] += *v
				b.pn[i]++
			}
		}
	}
	if m.MacPressure != nil && (b.mac == nil || *m.MacPressure > *b.mac) {
		v := *m.MacPressure
		b.mac = &v
	}
	if b.agents == nil {
		b.agents = map[string]hoststat.Use{}
	}
	for a, u := range m.Agents {
		x := b.agents[a]
		x.CPU += u.CPU
		x.RSS += u.RSS
		b.agents[a] = x
	}
}

func (b *bucketAcc) point(at int64) HostPoint {
	n := float64(b.n)
	p := HostPoint{At: at, N: b.n, CPU: round2(b.cpu / n), Load1: round2(b.load / n), MemUsed: b.mem / int64(b.n),
		SwapUsed: b.swap / int64(b.n), MacPressure: b.mac, Agents: map[string]hoststat.Use{}}
	var pr hoststat.Pressure
	for i, f := range []**float64{&pr.CPU, &pr.Mem, &pr.IO} {
		if b.pn[i] > 0 {
			v := round2(b.pr[i] / float64(b.pn[i]))
			*f = &v
		}
	}
	if pr.CPU != nil || pr.Mem != nil || pr.IO != nil {
		p.Pressure = &pr
	}
	for a, u := range b.agents {
		p.Agents[a] = hoststat.Use{CPU: round2(u.CPU / n), RSS: u.RSS / int64(b.n)}
	}
	return p
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// hostsAPI is /api/hosts?since=1h|6h|24h|7d (default 6h)[&live=1]: each
// box's history, and with live=1 each live answer, asked in parallel of
// this box and every box with a minute in the last liveRecent, liveWait in
// all; one that doesn't answer (asleep, an older release) is null.
func (s *server) hostsAPI(w http.ResponseWriter, r *http.Request) {
	key, _, ok := sinceParam(w, r, windows["6h"])
	if !ok {
		return
	}
	if key == "" {
		key = "6h"
	}
	e, err := s.hostsData(r.Context(), key)
	if err != nil {
		fail(w, err)
		return
	}
	v := e.v
	if r.URL.Query().Get("live") == "1" {
		v.Boxes = append([]HostBox(nil), v.Boxes...)
		s.askLive(r.Context(), v.Boxes)
	}
	writeJSON(w, v)
}

// askLive fills each box's Live: this box's and those with a recent minute
// asked, the rest null.
func (s *server) askLive(ctx context.Context, boxes []HostBox) {
	ctx, cancel := context.WithTimeout(ctx, liveWait)
	defer cancel()
	now := s.now()
	var wg sync.WaitGroup
	for i := range boxes {
		b := &boxes[i]
		b.Live = json.RawMessage("null")
		recent := b.Last != nil && now.Sub(time.Unix(unix(b.Last.At), 0)) <= liveRecent
		if b.Host != s.host && !recent {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			msg, err := s.c.NC.RequestWithContext(ctx, hoststat.LoadSubject(b.Host), nil)
			if err != nil {
				return
			}
			var l *hoststat.Live
			if json.Unmarshal(msg.Data, &l) != nil || l == nil || l.Host != b.Host {
				return
			}
			if out, err := json.Marshal(l); err == nil {
				b.Live = out
			}
		}()
	}
	wg.Wait()
}

// stats is the agents table's cpu (the mean over the minutes u has) and
// peak mem; nil for an agent with no minutes.
func (u *hostUse) stats() (*float64, *int64) {
	if u == nil || len(u.cpu) == 0 {
		return nil, nil
	}
	var sum float64
	for _, c := range u.cpu {
		sum += c
	}
	var peak int64
	for _, m := range u.rss {
		peak = max(peak, m)
	}
	mean := round2(sum / float64(len(u.cpu)))
	return &mean, &peak
}
