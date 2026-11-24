// Package hoststat reads a box's load (CPU, memory, swap and contention)
// and its processes, attributes each process to the agent whose worktree
// it runs in (117), and keeps two series of reads: the minute's, published
// to bmusage.host.<host>, and the live one a bmhosts.load.<host> request
// reads. Linux reads /proc; macOS runs ps, vm_stat, sysctl and lsof;
// Windows reads nothing.
package hoststat

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/usage"
)

const (
	// liveMin is how old the previous live read must be to diff against;
	// a request sooner is served the last answer again.
	liveMin = 5 * time.Second
	// top is how many processes the live answer lists by CPU, and again by
	// memory.
	top = 15
	// Other is the agent of what no agent's process accounts for.
	Other = "other"
)

// ErrUnsupported is a read on an OS with no reader (Windows).
var ErrUnsupported = errors.New("hoststat: no reader for this OS")

// Pressure is each resource's PSI `some` average over one window (Linux);
// a resource whose file is missing is nil, never 0.
type Pressure struct {
	CPU *float64 `json:"cpu"`
	Mem *float64 `json:"mem"`
	IO  *float64 `json:"io"`
}

// Use is an agent's CPU in cores and resident memory in bytes.
type Use struct {
	CPU float64 `json:"cpu"`
	RSS int64   `json:"rss"`
}

// Minute is one box's read on bmusage.host.<host>: CPU in cores since the
// series' previous read, memory and swap in bytes, pressure (avg60) or
// macOS's pressure level (1 normal, 2 warn, 4 critical), and each agent's
// use, Other being the box's less the agents'.
type Minute struct {
	Host        string         `json:"host"`
	OS          string         `json:"os"`
	At          string         `json:"at"`
	Cores       int            `json:"cores"`
	CPU         float64        `json:"cpu"`
	Load1       float64        `json:"load1"`
	MemUsed     int64          `json:"mem_used"`
	MemTotal    int64          `json:"mem_total"`
	SwapUsed    int64          `json:"swap_used"`
	Pressure    *Pressure      `json:"pressure"`
	MacPressure *int           `json:"mac_pressure"`
	Agents      map[string]Use `json:"agents"`
}

// Proc is one process in the live answer: its command name (never its
// args), CPU in cores and resident memory in bytes, and its agent.
type Proc struct {
	PID   int     `json:"pid"`
	PPID  int     `json:"ppid"`
	Comm  string  `json:"comm"`
	CPU   float64 `json:"cpu"`
	RSS   int64   `json:"rss"`
	Agent string  `json:"agent"`
}

// Total is an agent's processes, CPU and memory in the live answer.
type Total struct {
	Procs int     `json:"procs"`
	CPU   float64 `json:"cpu"`
	RSS   int64   `json:"rss"`
}

// Live is the answer on bmhosts.load.<host>: the minute's fields, pressure
// over 10 s, the top processes by CPU and by memory (merged, by CPU), and
// every agent's totals, Other's included.
type Live struct {
	Minute
	Pressure10 *Pressure        `json:"pressure10"`
	Procs      []Proc           `json:"procs"`
	Totals     map[string]Total `json:"totals"`
}

// Env is what a sampler reads with.
type Env struct {
	Host      string
	GOOS      string            // linux, darwin; anything else reads nothing
	Claude    string            // Claude Code's config dir, whose sessions/<pid>.json name a session; "" = none
	Checkouts string            // routine instances' checkouts root ([runner] checkouts, expanded)
	Repos     map[string]string // the registry's rows: name to checkout path
	// Nodes is this box's nodes that are not done; read once a minute.
	Nodes func(ctx context.Context) ([]mesh.Node, error)
	Proc  string // Linux's proc root; "" = /proc
	// Run runs a macOS command and returns its output, err on a failed
	// run (with the output it printed); nil runs it.
	Run      func(ctx context.Context, name string, args ...string) ([]byte, error)
	UID      int   // whose working directories lsof lists; 0 = os.Getuid
	PageSize int64 // Linux's page size for a process's rss; 0 = os.Getpagesize
	Now      func() time.Time
}

// Sampler keeps the two series' previous reads and the attribution's
// node list and repo cache.
type Sampler struct {
	env Env

	mu      sync.Mutex
	nodes   []mesh.Node       // the last Nodes read, worktrees resolved
	repoOf  map[string]string // dir -> RepoOf's answer
	minute  *raw              // the minute series' previous read
	live    *raw              // the live series'
	liveAns *Live             // the answer read from live
}

// New is a sampler reading with env.
func New(env Env) *Sampler {
	if env.Now == nil {
		env.Now = time.Now
	}
	if env.Proc == "" {
		env.Proc = "/proc"
	}
	if env.Run == nil {
		env.Run = run
	}
	if env.UID == 0 {
		env.UID = os.Getuid()
	}
	if env.PageSize == 0 {
		env.PageSize = int64(os.Getpagesize())
	}
	return &Sampler{env: env, repoOf: map[string]string{}}
}

// Supported reports whether the box's OS has a reader.
func (s *Sampler) Supported() bool { return s.env.GOOS == "linux" || s.env.GOOS == "darwin" }

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

// raw is one read of the box before any diff. busy and total are Linux's
// /proc/stat ticks (all CPUs); total 0 makes the box's CPU its processes'
// sum (macOS).
type raw struct {
	at                          time.Time
	cores                       int
	busy, total                 float64
	load1                       float64
	memUsed, memTotal, swapUsed int64
	p10, p60                    *Pressure
	mac                         *int
	procs                       map[int]*rproc
}

// rproc is one process as read: t is its CPU seconds, its own and its
// reaped children's; start tells a reused pid apart (0: unknown).
type rproc struct {
	ppid   int
	comm   string
	start  uint64
	t      float64
	rss    int64
	cwd    string
	claude bool // Claude Code's process
}

// same reports whether p is o read again, not a reused pid: by its start
// time where it is known (a kernel worker's comm changes with its work),
// else by its comm.
func (p *rproc) same(o *rproc) bool {
	if p.start != 0 || o.start != 0 {
		return p.start == o.start
	}
	return p.comm == o.comm
}

func (s *Sampler) read(ctx context.Context) (*raw, error) {
	switch s.env.GOOS {
	case "linux":
		return readLinux(s.env.Proc, s.env.PageSize, s.env.Now())
	case "darwin":
		return readDarwin(ctx, s.env.Run, s.env.UID, s.env.Now())
	}
	return nil, ErrUnsupported
}

// Minute reads the box and the nodes and diffs against the minute series'
// previous read: nil (and no error) on the first. A failed node read keeps
// the last node list.
func (s *Sampler) Minute(ctx context.Context) (*Minute, error) {
	if !s.Supported() {
		return nil, ErrUnsupported
	}
	var nodes []mesh.Node
	var nerr error
	if s.env.Nodes != nil {
		nodes, nerr = s.env.Nodes(ctx)
		for i := range nodes {
			nodes[i].Worktree = resolve(nodes[i].Worktree)
		}
	}
	cur, err := s.read(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.env.Nodes != nil && nerr == nil {
		s.nodes = nodes
	}
	prev := s.minute
	s.minute = cur
	if prev == nil || !cur.at.After(prev.at) {
		return nil, nerr
	}
	m, _, _ := s.diff(prev, cur)
	return &m, nerr
}

// Live is the live answer: a fresh read diffed against the newest previous
// read of either series at least liveMin old (else the oldest younger one),
// or, within liveMin of the last answer, that answer again. With no
// previous read at all it reads one and answers nil.
func (s *Sampler) Live(ctx context.Context) (*Live, error) {
	if !s.Supported() {
		return nil, ErrUnsupported
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.env.Now()
	if s.liveAns != nil && now.Sub(s.live.at) < liveMin {
		return s.liveAns, nil
	}
	cur, err := s.read(ctx)
	if err != nil {
		return nil, err
	}
	var old, young *raw
	for _, r := range []*raw{s.live, s.minute} {
		switch {
		case r == nil || !cur.at.After(r.at):
		case cur.at.Sub(r.at) >= liveMin:
			if old == nil || r.at.After(old.at) {
				old = r
			}
		case young == nil || r.at.Before(young.at):
			young = r
		}
	}
	prev := cmp.Or(old, young)
	s.live = cur
	if prev == nil {
		s.liveAns = nil
		return nil, nil
	}
	m, procs, totals := s.diff(prev, cur)
	s.liveAns = &Live{Minute: m, Pressure10: cur.p10, Procs: procs, Totals: totals}
	return s.liveAns, nil
}

// diff is cur against prev: the minute's fields, the top processes and
// every agent's totals. s.mu is held.
func (s *Sampler) diff(prev, cur *raw) (Minute, []Proc, map[string]Total) {
	dt := cur.at.Sub(prev.at).Seconds()
	cpus := procCPU(prev, cur, dt)
	agents := attribute(cur.procs, s.match)
	m := Minute{Host: s.env.Host, OS: s.env.GOOS, At: cur.at.UTC().Format(time.RFC3339), Cores: cur.cores,
		Load1: cur.load1, MemUsed: cur.memUsed, MemTotal: cur.memTotal, SwapUsed: cur.swapUsed,
		Pressure: cur.p60, MacPressure: cur.mac, Agents: map[string]Use{}}
	totals := map[string]Total{}
	var all []Proc
	var sum float64
	for pid, p := range cur.procs {
		c := cpus[pid]
		sum += c
		a := agents[pid]
		all = append(all, Proc{PID: pid, PPID: p.ppid, Comm: p.comm, CPU: round2(c), RSS: p.rss, Agent: a})
		t := totals[a]
		t.Procs, t.CPU, t.RSS = t.Procs+1, t.CPU+c, t.RSS+p.rss
		totals[a] = t
	}
	if cur.total > 0 && cur.total > prev.total {
		m.CPU = (cur.busy - prev.busy) / (cur.total - prev.total) * float64(cur.cores)
	} else {
		m.CPU = sum // shortcut: kernel time isn't counted, upgrade with a cgo host_statistics read if it misleads
	}
	m.CPU = round2(max(m.CPU, 0))
	var aCPU float64
	var aRSS int64
	for a, t := range totals {
		t.CPU = round2(t.CPU)
		totals[a] = t
		if a == Other {
			continue
		}
		m.Agents[a] = Use{CPU: t.CPU, RSS: t.RSS}
		aCPU, aRSS = aCPU+t.CPU, aRSS+t.RSS
	}
	m.Agents[Other] = Use{CPU: round2(max(m.CPU-aCPU, 0)), RSS: max(m.MemUsed-aRSS, 0)}
	return m, topProcs(all), totals
}

// procCPU is each process's CPU in cores over dt seconds: its time less
// its previous read's, and less the last time read of each child that has
// gone since (a reaped child's time moves into its parent's); a process
// not read before counts all its time.
func procCPU(prev, cur *raw, dt float64) map[int]float64 {
	reaped := map[int]float64{}
	for pid, o := range prev.procs {
		if p, ok := cur.procs[pid]; !ok || !p.same(o) {
			reaped[o.ppid] += o.t
		}
	}
	out := make(map[int]float64, len(cur.procs))
	for pid, p := range cur.procs {
		d := p.t
		if o, ok := prev.procs[pid]; ok && p.same(o) {
			d = p.t - o.t - reaped[pid]
		}
		if dt > 0 && d > 0 {
			out[pid] = d / dt
		}
	}
	return out
}

// topProcs is the top processes by CPU and the top by memory, merged and
// ordered by CPU, then memory.
func topProcs(all []Proc) []Proc {
	keep := map[int]bool{}
	sort.Slice(all, func(i, j int) bool {
		return all[i].RSS > all[j].RSS || all[i].RSS == all[j].RSS && all[i].PID < all[j].PID
	})
	for _, p := range all[:min(top, len(all))] {
		keep[p.PID] = true
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].CPU > all[j].CPU })
	for _, p := range all[:min(top, len(all))] {
		keep[p.PID] = true
	}
	out := []Proc{}
	for _, p := range all {
		if keep[p.PID] {
			out = append(out, p)
		}
	}
	return out
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func resolve(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// token is what a host must be to name a subject.
var token = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Subject is where a box's minutes go: bmusage.host.<host>, on bm-usage.
func Subject(host string) string { return "bmusage.host." + host }

// LoadSubject is where a box's daemon answers live requests over core NATS
// request/reply: bmhosts.load.<host>, outside bm.>, never stored.
func LoadSubject(host string) string { return "bmhosts.load." + host }

// Publish puts m on Subject(m.Host), once per host and at; a host that is
// no subject token publishes nothing.
func Publish(ctx context.Context, c *mesh.Client, m *Minute) error {
	if !token.MatchString(m.Host) {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err := c.JS.Publish(ctx, Subject(m.Host), data,
		jetstream.WithMsgID(m.Host+"/host/"+m.At), jetstream.WithExpectStream(mesh.StreamUsage)); err != nil {
		return fmt.Errorf("hoststat: publish %s: %w", Subject(m.Host), err)
	}
	return nil
}

// ReadHistory reads every box's minutes at or after since, oldest first;
// usage.ErrNoStream when the hub has no bm-usage. A message that doesn't
// decode is skipped.
func ReadHistory(ctx context.Context, c *mesh.Client, since time.Time) ([]Minute, error) {
	var out []Minute
	err := usage.Fetch(ctx, c, since, []string{"bmusage.host.>"}, func(data []byte) {
		var m Minute
		if json.Unmarshal(data, &m) != nil || m.Host == "" {
			return
		}
		if t, err := time.Parse(time.RFC3339, m.At); err != nil || t.Before(since) {
			return
		}
		out = append(out, m)
	})
	return out, err
}

// IsClaude reports whether a process is Claude Code's: comm claude, or
// node whose args name Claude Code's CLI.
func IsClaude(comm string, args []string) bool {
	if comm == "claude" {
		return true
	}
	if comm != "node" {
		return false
	}
	for _, a := range args {
		if strings.Contains(a, "claude-code") || filepath.Base(a) == "claude" {
			return true
		}
	}
	return false
}
