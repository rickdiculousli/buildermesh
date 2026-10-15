package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

const (
	window     = 24 * time.Hour   // how far back the timeline and instance uses look
	cacheFor   = 60 * time.Second // spec and kind file reads
	fetchEvery = 60 * time.Second // background fetches of a repo's origin main
	inboxShow  = 50
)

// fetchMain is the background fetch of a repo's origin main.
var fetchMain = spec.FetchMain

// server holds what the JSON endpoints share: the client, the config and
// the caches.
type server struct {
	c     *mesh.Client
	cfg   *config.Config
	host  string
	now   func() time.Time
	quota *Quota
	log   *slog.Logger

	mu      sync.Mutex
	specs   map[string]specEntry   // by lead path
	ledgers map[string]ledgerEntry // by repo path
	kinds   kindEntry
	fetched map[string]time.Time // when each repo's last background fetch started
}

type specEntry struct {
	key string // repo and spec id the entry was read for
	at  time.Time
	sp  *Spec
}

type ledgerEntry struct {
	at    time.Time
	specs []*spec.Spec
}

type kindEntry struct {
	at    time.Time
	byKey map[string]kind.Kind
}

// Spec is a lead's spec as read from its repo's main.
type Spec struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Merged   string `json:"merged"`
	Verified string `json:"verified"`
	Text     string `json:"text"`
}

func newServer(c *mesh.Client, o Options) *server {
	cfg := o.Cfg
	if cfg == nil {
		cfg = &config.Config{}
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	log := o.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &server{c: c, cfg: cfg, host: o.Host, now: now, quota: &Quota{Command: cfg.Quota.Command}, log: log,
		specs: map[string]specEntry{}, ledgers: map[string]ledgerEntry{}, fetched: map[string]time.Time{}}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// usage reads the routine instances' uses from the last day of events.
func (s *server) usage(ctx context.Context) (map[string]Usage, error) {
	hist, err := mesh.History(ctx, s.c, "bm.routine.>", s.now().Add(-window))
	if err != nil {
		return nil, err
	}
	return InstanceUsage(hist), nil
}

// routineKinds returns the defined kinds and the kinds of the routine
// nodes, sorted.
func routineKinds(nodes []mesh.Node, defs map[string]kind.Kind) []string {
	seen := map[string]bool{}
	var out []string
	add := func(k string) {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for name := range defs {
		add(name)
	}
	for _, n := range nodes {
		if n.Role != "routine" {
			continue
		}
		if k, ok := kindOf(n.Path); ok {
			add(k)
		}
	}
	sort.Strings(out)
	return out
}

func kindOf(path string) (string, bool) {
	p, err := mesh.ParsePath(path)
	if err != nil {
		return "", false
	}
	return mesh.KindFromPath(p)
}

func (s *server) depths(ctx context.Context, kinds []string) (map[string]int, error) {
	out := map[string]int{}
	for _, k := range kinds {
		d, err := mesh.Depth(ctx, s.c, k)
		if err != nil {
			return nil, err
		}
		out[k] = d
	}
	return out, nil
}

func (s *server) tree(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodes, err := mesh.ListNodes(ctx, s.c)
	if err != nil {
		fail(w, err)
		return
	}
	states, err := mesh.ReadAllStates(ctx, s.c)
	if err != nil {
		fail(w, err)
		return
	}
	x := Extras{Runners: map[string]mesh.RunnerSample{}, Unread: map[string]int{}}
	for _, n := range nodes {
		if n.Role == "routine" {
			continue
		}
		rs, ok, err := mesh.GetRunner(ctx, s.c, n.Path)
		if err != nil {
			fail(w, err)
			return
		}
		if ok {
			x.Runners[n.Path] = rs
		}
		if p, err := mesh.ParsePath(n.Path); err == nil {
			u, err := mesh.Unread(ctx, s.c, p)
			if err != nil {
				fail(w, err)
				return
			}
			x.Unread[n.Path] = u
		}
	}
	if x.Usage, err = s.usage(ctx); err != nil {
		fail(w, err)
		return
	}
	defs := s.kindDefs()
	x.Modes = map[string]string{}
	for name, d := range defs {
		x.Modes[name] = d.Mode
	}
	if x.Depth, err = s.depths(ctx, routineKinds(nodes, defs)); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, Tree(nodes, states, x))
}

// leadPage is /api/lead/{path}. Specs is null except for a round.
type leadPage struct {
	Node       mesh.Node      `json:"node"`
	State      mesh.State     `json:"state"`
	Spec       *Spec          `json:"spec"`
	Children   []Child        `json:"children"`
	Specs      []RoundSpec    `json:"specs"`
	ConsoleURL string         `json:"console_url"`
	Inbox      []mesh.Message `json:"inbox"`
	Timeline   []frame        `json:"timeline"`
}

// Child is one of a node's direct children in the tree /api/tree draws.
type Child struct {
	Node   mesh.Node  `json:"node"`
	State  mesh.State `json:"state"`
	Unread int        `json:"unread"`
}

// RoundSpec is a spec one of a round's engineers proposed, as it stands on
// the repo's main.
type RoundSpec struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Text   string `json:"text"`
}

func (s *server) lead(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path := r.PathValue("path")
	if _, err := mesh.ParsePath(path); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	n, ok, err := mesh.GetNode(ctx, s.c, path)
	if err != nil {
		fail(w, err)
		return
	}
	if !ok {
		http.Error(w, "no node "+path, http.StatusNotFound)
		return
	}
	st, _, err := mesh.ReadState(ctx, s.c, path)
	if err != nil {
		fail(w, err)
		return
	}
	if st.State == "" {
		st = stateOf(nil, path) // the same default as /api/tree
	}
	since := s.now().Add(-window)
	pg := leadPage{Node: n, State: st, Spec: s.spec(ctx, n), ConsoleURL: ConsoleURL(s.cfg.Console, s.host, n),
		Inbox: []mesh.Message{}, Timeline: []frame{}}
	if pg.Children, err = s.children(ctx, path); err != nil {
		fail(w, err)
		return
	}
	if n.Role == "round" {
		pg.Specs = s.roundSpecs(ctx, n, pg.Children)
	}

	// The inbox has no time limit: the newest inboxShow messages the stream keeps.
	msgs, err := mesh.History(ctx, s.c, "bm.inbox."+path, time.Time{})
	if err != nil {
		fail(w, err)
		return
	}
	for i := len(msgs) - 1; i >= 0 && len(pg.Inbox) < inboxShow; i-- {
		var m mesh.Message
		if json.Unmarshal(msgs[i].Data, &m) != nil {
			m = mesh.Message{Kind: "malformed", Text: string(msgs[i].Data)}
		}
		pg.Inbox = append(pg.Inbox, m)
	}
	evs, err := mesh.History(ctx, s.c, "bm."+path+".>", since)
	if err != nil {
		fail(w, err)
		return
	}
	for _, m := range evs {
		pg.Timeline = append(pg.Timeline, frameOf(m))
	}
	writeJSON(w, pg)
}

// children is path's direct children in the tree /api/tree builds, with
// their states and unread counts.
func (s *server) children(ctx context.Context, path string) ([]Child, error) {
	nodes, err := mesh.ListNodes(ctx, s.c)
	if err != nil {
		return nil, err
	}
	states, err := mesh.ReadAllStates(ctx, s.c)
	if err != nil {
		return nil, err
	}
	// Only a node whose parent is path can be drawn under it, so only those
	// need an unread count.
	unread := map[string]int{}
	for _, n := range nodes {
		if n.Parent != path || n.Role == "routine" {
			continue
		}
		p, err := mesh.ParsePath(n.Path)
		if err != nil {
			continue
		}
		if unread[n.Path], err = mesh.Unread(ctx, s.c, p); err != nil {
			return nil, err
		}
	}
	v := Tree(nodes, states, Extras{Unread: unread})
	out := []Child{}
	if t := findTree(append(v.Leads, v.Orphans...), path); t != nil {
		for _, c := range t.Children {
			out = append(out, Child{Node: c.Node, State: c.State, Unread: c.Unread})
		}
	}
	return out, nil
}

func findTree(ts []TreeNode, path string) *TreeNode {
	for i := range ts {
		if ts[i].Node.Path == path {
			return &ts[i]
		}
		if t := findTree(ts[i].Children, path); t != nil {
			return t
		}
	}
	return nil
}

// roundSpecs is the specs on the round's repo's main whose proposed_by is
// one of its children's threads, by id; [] when there are none or the
// ledger can't be read.
func (s *server) roundSpecs(ctx context.Context, n mesh.Node, children []Child) []RoundSpec {
	out := []RoundSpec{}
	threads := map[string]bool{}
	for _, c := range children {
		if c.Node.Runner.Thread != "" {
			threads[c.Node.Runner.Thread] = true
		}
	}
	if len(threads) == 0 {
		return out
	}
	for _, sp := range s.ledger(ctx, n.Repo) {
		if threads[sp.Fields["proposed_by"]] {
			out = append(out, RoundSpec{ID: sp.IDString(), Title: sp.Title(), Status: sp.Status(), Text: sp.Text})
		}
	}
	return out
}

// ledger reads repo's ledger from its local origin/main (main without an
// origin) as spec does: never fetching, cached for a minute per repo, nil
// when the repo is not configured here or the read failed (not cached).
func (s *server) ledger(ctx context.Context, repo string) []*spec.Spec {
	if repo == "" {
		return nil
	}
	_, repoPath, err := s.cfg.RepoPath(repo)
	if err != nil {
		return nil
	}
	now := s.now()
	s.mu.Lock()
	e, ok := s.ledgers[repoPath]
	s.mu.Unlock()
	if ok && now.Sub(e.at) < cacheFor {
		return e.specs
	}
	specs, err := spec.ScanMainLocal(ctx, repoPath, s.cfg.Spec.Glob)
	s.fetchSoon(repoPath)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	s.ledgers[repoPath] = ledgerEntry{at: now, specs: specs}
	s.mu.Unlock()
	return specs
}

// spec reads n's spec from its repo's local origin/main (main without an
// origin) through [repos], never fetching, cached for a minute per node;
// nil when the node names no spec, the repo is not configured here, the
// spec is not on main or the read failed (which is not cached).
func (s *server) spec(ctx context.Context, n mesh.Node) *Spec {
	if n.Spec == "" {
		return nil
	}
	key := n.Repo + "\x00" + n.Spec
	now := s.now()
	s.mu.Lock()
	e, ok := s.specs[n.Path]
	s.mu.Unlock()
	if ok && e.key == key && now.Sub(e.at) < cacheFor {
		return e.sp
	}
	sp, ok := s.readSpec(ctx, n)
	if ok {
		s.mu.Lock()
		s.specs[n.Path] = specEntry{key: key, at: now, sp: sp}
		s.mu.Unlock()
	}
	return sp
}

// readSpec reads n's spec without a fetch, then starts a background fetch
// of the repo; ok is false when the read failed.
func (s *server) readSpec(ctx context.Context, n mesh.Node) (sp *Spec, ok bool) {
	if n.Repo == "" {
		return nil, true
	}
	_, repoPath, err := s.cfg.RepoPath(n.Repo)
	if err != nil {
		return nil, true
	}
	_, text, err := spec.FindOnMainLocal(ctx, repoPath, s.cfg.Spec.Glob, n.Spec)
	s.fetchSoon(repoPath)
	if err != nil {
		return nil, spec.IsNotFound(err)
	}
	sp = &Spec{ID: n.Spec, Text: text}
	if fm, err := spec.Frontmatter(text); err == nil {
		sp.Title, sp.Status, sp.Merged, sp.Verified = fm["title"], fm["status"], fm["merged"], fm["verified"]
	}
	if sp.Title == "" {
		for _, line := range strings.Split(text, "\n") {
			if t, ok := strings.CutPrefix(line, "# "); ok {
				sp.Title = strings.TrimSpace(t)
				break
			}
		}
	}
	return sp, true
}

// fetchSoon starts a background fetch of repoPath's origin main unless one
// started in the last fetchEvery, so the local origin/main the lead page
// reads keeps up; no request waits for it, and a failure is only logged.
func (s *server) fetchSoon(repoPath string) {
	now := s.now()
	s.mu.Lock()
	last, ok := s.fetched[repoPath]
	due := !ok || now.Sub(last) >= fetchEvery
	if due {
		s.fetched[repoPath] = now
	}
	s.mu.Unlock()
	if !due {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := fetchMain(ctx, repoPath); err != nil {
			s.log.Warn("map: fetch origin main", "repo", repoPath, "err", err)
		}
	}()
}

// LeaseRow is one bm-leases entry.
type LeaseRow struct {
	Key       string `json:"key"`
	Holder    string `json:"holder"`
	ExpiresAt string `json:"expires_at"`
}

// RoutineKind is one kind on /api/routines: its definition (from the kind
// file of a configured repo; mode "" when none has it), instances, queue
// depth, result count and the leases its instances hold.
type RoutineKind struct {
	Kind          string     `json:"kind"`
	Mode          string     `json:"mode"`
	Deterministic bool       `json:"deterministic"`
	Instances     []Instance `json:"instances"`
	Depth         int        `json:"depth"`
	Results       int        `json:"results"`
	Leases        []LeaseRow `json:"leases"`
}

// RoutinesView is /api/routines; Leases holds the leases no routine
// instance holds.
type RoutinesView struct {
	Routines []RoutineKind `json:"routines"`
	Leases   []LeaseRow    `json:"leases"`
}

// kindDefs returns the kind files of every configured repo by name (the
// first repo by name wins), read at most once a minute.
func (s *server) kindDefs() map[string]kind.Kind {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kinds.byKey != nil && now.Sub(s.kinds.at) < cacheFor {
		return s.kinds.byKey
	}
	names := make([]string, 0, len(s.cfg.Repos))
	for name := range s.cfg.Repos {
		names = append(names, name)
	}
	sort.Strings(names)
	defs := map[string]kind.Kind{}
	for _, name := range names {
		_, p, err := s.cfg.RepoPath(name)
		if err != nil {
			continue
		}
		ks, err := kind.LoadDir(p)
		if err != nil {
			continue
		}
		for _, k := range ks {
			if _, ok := defs[k.Name]; !ok {
				defs[k.Name] = k
			}
		}
	}
	s.kinds = kindEntry{at: now, byKey: defs}
	return defs
}

func (s *server) routines(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodes, err := mesh.ListNodes(ctx, s.c)
	if err != nil {
		fail(w, err)
		return
	}
	states, err := mesh.ReadAllStates(ctx, s.c)
	if err != nil {
		fail(w, err)
		return
	}
	usage, err := s.usage(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	leases, err := mesh.ListLeases(ctx, s.c)
	if err != nil {
		fail(w, err)
		return
	}
	results, err := mesh.ListAllResults(ctx, s.c)
	if err != nil {
		fail(w, err)
		return
	}
	kinds := map[string]*RoutineKind{}
	at := func(k string) *RoutineKind {
		rk, ok := kinds[k]
		if !ok {
			rk = &RoutineKind{Kind: k, Instances: []Instance{}, Leases: []LeaseRow{}}
			kinds[k] = rk
		}
		return rk
	}
	// Every defined kind is listed, with or without instances (inline kinds
	// and idle fresh kinds have none).
	defs := s.kindDefs()
	for name := range defs {
		at(name)
	}
	for _, n := range nodes {
		if k, ok := kindOf(n.Path); ok && n.Role == "routine" {
			u := usage[n.Path]
			rk := at(k)
			rk.Instances = append(rk.Instances, Instance{Node: n, State: stateOf(states, n.Path), Uses: u.Uses, Context: u.Context})
		}
	}
	for _, res := range results {
		at(res.Kind).Results++
	}
	v := RoutinesView{Routines: []RoutineKind{}, Leases: []LeaseRow{}}
	for _, l := range leases {
		row := LeaseRow{Key: l.Key, Holder: l.Holder, ExpiresAt: l.ExpiresAt.UTC().Format(time.RFC3339)}
		if k, ok := kindOf(l.Holder); ok {
			rk := at(k)
			rk.Leases = append(rk.Leases, row)
		} else {
			v.Leases = append(v.Leases, row)
		}
	}
	for name, rk := range kinds {
		if d, ok := defs[name]; ok {
			rk.Mode, rk.Deterministic = d.Mode, d.Deterministic
		}
		if rk.Depth, err = mesh.Depth(ctx, s.c, name); err != nil {
			fail(w, err)
			return
		}
		v.Routines = append(v.Routines, *rk)
	}
	sort.Slice(v.Routines, func(i, j int) bool { return v.Routines[i].Kind < v.Routines[j].Kind })
	writeJSON(w, v)
}

// digest is the newest daemon digest.
type digest struct {
	At    string `json:"at"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

type stripView struct {
	Quota  []Reading `json:"quota"`
	Digest *digest   `json:"digest"`
}

func (s *server) strip(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	v := stripView{Quota: s.quota.Get(ctx, s.now())}
	hist, err := mesh.History(ctx, s.c, "bm.daemon.*.digest", time.Time{})
	if err != nil {
		fail(w, err)
		return
	}
	if len(hist) > 0 {
		m := hist[len(hist)-1]
		var ev mesh.Event
		if json.Unmarshal(m.Data, &ev) == nil {
			if ev.At == "" {
				ev.At = m.At.UTC().Format(time.RFC3339)
			}
			v.Digest = &digest{At: ev.At, Title: ev.Text, Body: ev.Body}
		}
	}
	writeJSON(w, v)
}
