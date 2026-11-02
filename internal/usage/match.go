package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/audit"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// worktreeWait is how long a main file whose cwd is a linked git worktree
// waits for a node to name it (a lead's started), from its first line.
const worktreeWait = 10 * time.Minute

// first is what a transcript's first lines say: the first cwd, the first
// line's time and the first user message's text.
type first struct {
	cwd, at, prompt string
}

// head reads path's first lines, up to the first cwd and user message.
func head(path string) first {
	var f first
	fh, err := os.Open(path)
	if err != nil {
		return f
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 1<<16), 16<<20)
	for n := 0; n < 200 && sc.Scan(); n++ {
		var l audit.Line
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		if f.at == "" {
			f.at = l.Timestamp
		}
		if f.cwd == "" {
			f.cwd = l.Cwd
		}
		if f.prompt == "" && l.Type == "user" && !l.IsMeta {
			f.prompt = text(l)
		}
		if f.cwd != "" && f.prompt != "" && f.at != "" {
			break
		}
	}
	return f
}

// assignMain decides which agent main file path is (rules 2 to 5), false
// while it waits for a node to name it.
func (p *pass) assignMain(path string, e *entry) bool {
	id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	h := head(path)
	if h.at == "" {
		return false // nothing written yet
	}
	e.Session = id
	if n, ok := p.nodeFor(id, h); ok {
		e.Agent, e.Role, e.Repo = n.Path, n.Role, n.Repo
		return true
	}
	if path, repo, ok := p.routine(h.cwd); ok {
		e.Agent, e.Role, e.Repo = path, "routine", repo
		return true
	}
	if at, err := time.Parse(time.RFC3339Nano, h.at); err == nil && p.env.Now().Sub(at) < worktreeWait && linked(h.cwd) {
		return false // a lead's started may name it yet
	}
	e.Agent, e.Role, e.Repo = SessionAgent(mesh.RepoToken(p.env.Host), id), Coordinator, p.repoOf(h.cwd)
	return true
}

// nodeFor is the node on this box whose session is id (101), else the
// newest whose worktree is the file's first cwd, started at or before its
// first line.
func (p *pass) nodeFor(id string, h first) (mesh.Node, bool) {
	nodes := p.here()
	for _, n := range nodes {
		if n.Session == id {
			return n, true
		}
	}
	at, err := time.Parse(time.RFC3339Nano, h.at)
	if h.cwd == "" || err != nil {
		return mesh.Node{}, false
	}
	var best mesh.Node
	var bestAt time.Time
	for _, n := range nodes {
		started, err := time.Parse(time.RFC3339, n.StartedAt)
		if err != nil || n.Worktree == "" || started.After(at) || !mesh.SameDir(n.Worktree, h.cwd) {
			continue
		}
		if best.Path == "" || started.After(bestAt) {
			best, bestAt = n, started
		}
	}
	return best, best.Path != ""
}

// routine is the routine instance whose checkout is cwd:
// <checkouts>/<repo>/routine.<kind>.<model-short>-<n> is
// routine.<kind>.<host>-<model-short>-<n>, and a fresh instance's
// routine.<kind>.f<id8> is its own path.
func (p *pass) routine(cwd string) (path, repo string, ok bool) {
	if p.env.Checkouts == "" || cwd == "" {
		return "", "", false
	}
	rel, err := filepath.Rel(resolve(p.env.Checkouts), resolve(cwd))
	if err != nil {
		return "", "", false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 || parts[0] == ".." {
		return "", "", false
	}
	name, ok := strings.CutPrefix(parts[1], "routine.")
	kind, rest, found := strings.Cut(name, ".")
	if !ok || !found || kind == "" || rest == "" {
		return "", "", false
	}
	path = parts[1]
	if strings.Contains(rest, "-") {
		path = "routine." + kind + "." + mesh.RepoToken(p.env.Host) + "-" + rest
	}
	repo = parts[0]
	for _, n := range p.here() {
		if n.Path == path && n.Repo != "" {
			repo = n.Repo
		}
	}
	return path, repo, true
}

// assignSub decides which agent subagent file path is: the executor its
// first message names (rule 1), else its main file's agent with its id and
// description (rule 6). False while its main file is undecided or, for a
// coordinator's, not yet read past the call that started it.
func (p *pass) assignSub(path string, e *entry) bool {
	h := head(path)
	if h.at == "" {
		return false
	}
	if rest, ok := strings.CutPrefix(strings.TrimSpace(h.prompt), "You are executor "); ok {
		exe, _, _ := strings.Cut(rest, " ")
		for _, n := range p.here() {
			if n.Path == exe {
				e.Agent, e.Role, e.Repo = n.Path, n.Role, n.Repo
				e.Session = filepath.Base(filepath.Dir(filepath.Dir(path)))
				return true
			}
		}
	}
	mainPath := filepath.Dir(filepath.Dir(path)) + ".jsonl"
	parent := p.cur.Files[p.rel(mainPath)]
	if parent == nil || parent.Agent == "" {
		return false
	}
	var meta struct {
		Description string `json:"description"`
		ToolUseID   string `json:"toolUseId"`
	}
	if b, err := os.ReadFile(strings.TrimSuffix(path, ".jsonl") + ".meta.json"); err == nil {
		json.Unmarshal(b, &meta)
	}
	if parent.Role == Coordinator {
		phase, ok := parent.Spawned[meta.ToolUseID]
		if !ok {
			st, err := os.Stat(mainPath)
			if err == nil && parent.Off < st.Size() {
				return false // the call that started it is still to be read
			}
			phase = cmpOr(parent.Phase, Design)
		}
		e.Fixed = phase
	}
	e.Agent, e.Role, e.Repo, e.Session = parent.Agent, parent.Role, parent.Repo, parent.Session
	e.Sub = strings.TrimPrefix(strings.TrimSuffix(filepath.Base(path), ".jsonl"), "agent-")
	e.Desc = clip(meta.Description, 100)
	return true
}

// linked reports whether dir is in a git worktree other than a main
// checkout.
func linked(dir string) bool {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir").Output()
	if err != nil {
		return false
	}
	f := strings.Fields(string(out))
	return len(f) == 2 && f[0] != f[1]
}

// repoOf is the registry row whose checkout holds dir's git common dir,
// so a worktree counts as its repo; else the row whose checkout holds dir;
// else other.
func (p *pass) repoOf(dir string) string {
	if dir == "" {
		return "other"
	}
	at := dir
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output(); err == nil {
		at = filepath.Dir(strings.TrimSpace(string(out)))
	}
	at = resolve(at)
	for name, rp := range p.env.Repos {
		if r := resolve(rp); at == r || strings.HasPrefix(at, r+string(filepath.Separator)) {
			return name
		}
	}
	return "other"
}

func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}
