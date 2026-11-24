package hoststat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/usage"
)

// ancestors bounds the walk up a process's parents.
const ancestors = 64

// repoCacheMax is how many directories the repo cache holds before it
// starts over.
const repoCacheMax = 1000

// attribute names each process's agent: its own (match), else its nearest
// ancestor's, else Other.
func attribute(procs map[int]*rproc, match func(int, *rproc) string) map[int]string {
	own := make(map[int]string, len(procs))
	for pid, p := range procs {
		own[pid] = match(pid, p)
	}
	out := make(map[int]string, len(procs))
	for pid := range procs {
		a := Other
		for at, n := pid, 0; n < ancestors; n++ {
			if o := own[at]; o != "" {
				a = o
				break
			}
			p := procs[at]
			if p == nil || p.ppid == at || p.ppid <= 0 {
				break
			}
			at = p.ppid
		}
		out[pid] = a
	}
	return out
}

// match is a process's own agent, named as usage names agents: the node on
// this box, not done, whose worktree is its working directory or holds it
// (the deepest, then the newest); else the routine instance whose checkout
// holds it; else, for Claude Code, session.<host>.<id> from its
// sessions/<pid>.json, else repo:<repo>; else "" (its ancestor's). s.mu is
// held.
func (s *Sampler) match(pid int, p *rproc) string {
	if p.cwd == "" {
		return ""
	}
	var best mesh.Node
	var bestAt time.Time
	for _, n := range s.nodes {
		w := n.Worktree
		if w == "" || p.cwd != w && !strings.HasPrefix(p.cwd, strings.TrimSuffix(w, string(filepath.Separator))+string(filepath.Separator)) {
			continue
		}
		at, _ := time.Parse(time.RFC3339, n.StartedAt)
		if best.Path == "" || len(w) > len(best.Worktree) || len(w) == len(best.Worktree) && at.After(bestAt) {
			best, bestAt = n, at
		}
	}
	if best.Path != "" {
		return best.Path
	}
	if path, _, ok := usage.RoutineOf(s.env.Checkouts, s.env.Host, s.nodes, p.cwd); ok {
		return path
	}
	if !p.claude {
		return ""
	}
	if id := s.session(pid, p); id != "" {
		return usage.SessionAgent(mesh.RepoToken(s.env.Host), id)
	}
	repo, ok := s.repoOf[p.cwd]
	if !ok {
		if len(s.repoOf) >= repoCacheMax {
			s.repoOf = map[string]string{}
		}
		repo = usage.RepoOf(s.env.Repos, p.cwd)
		s.repoOf[p.cwd] = repo
	}
	if repo != "other" {
		return "repo:" + repo
	}
	return ""
}

// session is the sessionId of Claude Code's <config dir>/sessions/<pid>.json,
// "" when there is none or its procStart names another process.
func (s *Sampler) session(pid int, p *rproc) string {
	if s.env.Claude == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(s.env.Claude, "sessions", strconv.Itoa(pid)+".json"))
	if err != nil {
		return ""
	}
	var f struct {
		SessionID string `json:"sessionId"`
		ProcStart string `json:"procStart"`
	}
	if json.Unmarshal(b, &f) != nil {
		return ""
	}
	if p.start != 0 && f.ProcStart != "" && f.ProcStart != strconv.FormatUint(p.start, 10) {
		return "" // a file left by an earlier process with this pid
	}
	return f.SessionID
}

// LiveNodes is host's nodes that are not done: the nodes a sampler's
// processes are matched against.
func LiveNodes(ctx context.Context, c *mesh.Client, host string) ([]mesh.Node, error) {
	nodes, err := mesh.ListNodes(ctx, c)
	if err != nil {
		return nil, err
	}
	states, err := mesh.ReadAllStates(ctx, c)
	if err != nil {
		return nil, err
	}
	var out []mesh.Node
	for _, n := range nodes {
		if n.Host == host && states[n.Path].State != "done" {
			out = append(out, n)
		}
	}
	return out, nil
}
