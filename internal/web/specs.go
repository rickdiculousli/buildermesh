package web

import (
	"context"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/spec"
)

const specDays = 30

// specLife is one spec's days in the repo's history, as unix day numbers
// (UTC); landed 0 = not landed.
type specLife struct {
	id               int
	proposed, landed int64
	status           string
}

var specRE = regexp.MustCompile(`^(\d+)-.*\.md$`)
var specTrailerRE = regexp.MustCompile(`(?mi)^Spec:\s*(\d+)\s*$`)

type specLogEntry struct {
	sha   string
	lives []specLife
}

var (
	specLogMu    sync.Mutex
	specLogCache = map[string]specLogEntry{} // by repo path
)

// specLives reads the spec dir's history on the repo's local main, cached
// per head sha. shortcut: tracked by id from the file name, no renames
// (--no-renames: a moved file is a delete and an add, its first add stays
// its proposed day); a deleted spec keeps its last state.
func specLives(ctx context.Context, repoPath, dir string) ([]specLife, error) {
	ref, git, err := spec.LocalMain(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	sha, err := git("rev-parse", ref)
	if err != nil {
		return nil, err
	}
	sha = strings.TrimSpace(sha)
	specLogMu.Lock()
	defer specLogMu.Unlock()
	if e, ok := specLogCache[repoPath]; ok && e.sha == sha {
		return e.lives, nil
	}
	out, err := git("log", "-p", "--reverse", "--no-renames", "--format=%x01%ct%x02%B%x02", ref, "--", dir+"/")
	if err != nil {
		return nil, err
	}
	byID := map[int]*specLife{}
	trailer := map[int]int64{} // first commit with a Spec: trailer
	doneAt := map[int]int64{}  // first commit adding "+status: done"
	for _, c := range strings.Split(out, "\x01")[1:] {
		parts := strings.SplitN(c, "\x02", 3)
		if len(parts) < 3 {
			continue
		}
		ct, _ := strconv.ParseInt(parts[0], 10, 64)
		day := ct - ct%86400
		for _, m := range specTrailerRE.FindAllStringSubmatch(parts[1], -1) {
			if id, _ := strconv.Atoi(m[1]); trailer[id] == 0 {
				trailer[id] = day
			}
		}
		var cur *specLife
		isNew := false
		for _, line := range strings.Split(parts[2], "\n") {
			switch {
			case strings.HasPrefix(line, "diff --git "):
				cur, isNew = nil, false
			case strings.HasPrefix(line, "new file mode"):
				isNew = true
			case strings.HasPrefix(line, "+++ b/"):
				m := specRE.FindStringSubmatch(path.Base(line[6:]))
				if m == nil {
					continue
				}
				id, _ := strconv.Atoi(m[1])
				if byID[id] == nil {
					byID[id] = &specLife{id: id}
				}
				cur = byID[id]
				if isNew && cur.proposed == 0 {
					cur.proposed = day
				}
			case cur != nil && strings.HasPrefix(line, "+status:"):
				cur.status = strings.TrimSpace(line[len("+status:"):])
				if cur.status == "done" && doneAt[cur.id] == 0 {
					doneAt[cur.id] = day
				}
			}
		}
	}
	lives := []specLife{}
	for id, l := range byID {
		if l.proposed == 0 || l.status == "rejected" {
			continue
		}
		if l.landed = trailer[id]; l.landed == 0 {
			l.landed = doneAt[id]
		}
		lives = append(lives, *l)
	}
	sort.Slice(lives, func(i, j int) bool { return lives[i].id < lives[j].id })
	specLogCache[repoPath] = specLogEntry{sha: sha, lives: lives}
	return lives, nil
}

type specDay struct {
	Day    string `json:"day"`
	Landed []int  `json:"landed"`
	Open   int    `json:"open"`
}

type specRepo struct {
	Repo string    `json:"repo"`
	Days []specDay `json:"days"`
}

type specLead struct {
	Path    string `json:"path"`
	Spec    string `json:"spec"`
	Started int64  `json:"started"`
	Done    int64  `json:"done"`
}

type specFamily struct {
	Family  string     `json:"family"`
	Repo    string     `json:"repo"`
	Start   int64      `json:"start"`
	Live    bool       `json:"live"`
	Leads   []specLead `json:"leads"`
	Steps   [][2]int64 `json:"steps"`
	Refused [][3]any   `json:"refused"`
}

// SpecsView is /api/specs.
type SpecsView struct {
	From     int64        `json:"from"`
	To       int64        `json:"to"`
	Repos    []specRepo   `json:"repos"`
	Families []specFamily `json:"families"`
}

// days is lives as the last specDays UTC days up to now.
func specDaysOf(lives []specLife, now time.Time) []specDay {
	today := now.Unix() - now.Unix()%86400
	out := make([]specDay, specDays)
	for i := range out {
		d := today - int64(specDays-1-i)*86400
		out[i] = specDay{Day: time.Unix(d, 0).UTC().Format("2006-01-02"), Landed: []int{}}
		for _, l := range lives {
			if l.landed == d {
				out[i].Landed = append(out[i].Landed, l.id)
			}
			if l.proposed <= d && (l.landed == 0 || l.landed > d) {
				out[i].Open++
			}
		}
	}
	return out
}

// specsAPI is /api/specs[?repo=R]: each repo's burndown days from git and the
// build families of the last 30 days from the mesh.
func (s *server) specsAPI(w http.ResponseWriter, r *http.Request) {
	names := s.cfg.RepoNames()
	sort.Strings(names)
	if q := r.URL.Query().Get("repo"); q != "" {
		if _, _, err := s.cfg.RepoPath(q); err != nil || !contains(names, q) {
			http.Error(w, "repo not configured", http.StatusNotFound)
			return
		}
		names = []string{q}
	}
	now := s.now()
	today := now.Unix() - now.Unix()%86400
	v := SpecsView{From: today - (specDays-1)*86400, To: now.Unix(), Repos: []specRepo{}, Families: []specFamily{}}
	dir := path.Dir(s.cfg.Spec.Glob)
	for _, name := range names {
		_, repoPath, err := s.cfg.RepoPath(name)
		days := []specDay{}
		if err == nil {
			s.fetchSoon(repoPath)
			var lives []specLife
			if lives, err = specLives(r.Context(), repoPath, dir); err == nil {
				days = specDaysOf(lives, now)
			}
		}
		if err != nil {
			s.log.Warn("specs: git read failed", "repo", name, "err", err)
		}
		v.Repos = append(v.Repos, specRepo{Repo: name, Days: days})
	}
	h, err := s.history(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	v.Families = specFamilies(h, now, r.URL.Query().Get("repo"))
	writeJSON(w, v)
}

func contains(l []string, x string) bool {
	for _, y := range l {
		if y == x {
			return true
		}
	}
	return false
}

func specFamilies(h histEntry, now time.Time, repo string) []specFamily {
	byFam := map[string]*specFamily{}
	famOf := map[string]*specFamily{} // lead path -> family
	for _, n := range h.nodes {
		if n.Role != "lead" || n.Family == "" || (repo != "" && n.Repo != repo) {
			continue
		}
		t, err := time.Parse(time.RFC3339, n.StartedAt)
		if err != nil {
			continue
		}
		f := byFam[n.Family]
		if f == nil {
			f = &specFamily{Family: n.Family, Repo: n.Repo, Start: t.Unix(), Leads: []specLead{}, Refused: [][3]any{}}
			byFam[n.Family] = f
		}
		l := specLead{Path: n.Path, Spec: n.Spec, Started: t.Unix()}
		if st := h.states[n.Path]; st.State == "done" {
			if dt, err := time.Parse(time.RFC3339, st.At); err == nil {
				l.Done = dt.Unix()
			}
		}
		f.Leads = append(f.Leads, l)
		f.Start = min(f.Start, l.Started)
		famOf[n.Path] = f
	}
	out := []specFamily{}
	cutoff := now.Add(-specDays * 24 * time.Hour).Unix()
	for _, f := range byFam {
		if f.Start < cutoff {
			delete(byFam, f.Family)
			continue
		}
		sort.Slice(f.Leads, func(i, j int) bool { return f.Leads[i].Path < f.Leads[j].Path })
		var dones []int64
		for _, l := range f.Leads {
			if l.Done == 0 {
				f.Live = true
			} else {
				dones = append(dones, l.Done)
			}
		}
		sort.Slice(dones, func(i, j int) bool { return dones[i] < dones[j] })
		open := int64(len(f.Leads))
		f.Steps = [][2]int64{{f.Start, open}}
		for _, d := range dones {
			open--
			f.Steps = append(f.Steps, [2]int64{d, open})
		}
		out = append(out, *f)
	}
	for _, e := range laneEvents(h.msgs) {
		if e.name != "land-refused" {
			continue
		}
		if f := famOf[e.path]; f != nil && byFam[f.Family] != nil {
			for i := range out {
				if out[i].Family == f.Family {
					out[i].Refused = append(out[i].Refused, [3]any{e.at.Unix(), e.path, e.text})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start > out[j].Start })
	return out
}
