package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/brief"
	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

// A cell's layout. The sandbox and its origin sit side by side; everything
// else is the harness's.
const (
	sandboxDir = "shop"       // the sandbox repo, the session's cwd
	originDir  = "origin.git" // the sandbox's origin, a bare repo
	logFile    = "bmesh.log"  // the fake bmesh's log
	blockedLog = "blocked.log"
	cellFile   = "cell.json"
	workTar    = "work.tar.gz"         // the sandbox and what else the session left, once the cell is done
	repoName   = "shop"                // the sandbox's registry row
	leadRef    = "refs/skilltest/lead" // a lead's branch as the session left it, kept in origin.git for scoring
)

// blocked are the tools a cell shadows with a stub: each reaches the real
// mesh (nats, ssh to a box running the real bmesh, the previous bmesh),
// threads (t3, claude and codex sessions of their own, tmux's windows),
// real repos (gh) or people (ntfy).
var blocked = []string{"bmesh.prev", "claude", "codex", "gh", "nats", "ntfy", "scp", "ssh", "t3", "tmux"}

//go:embed fake/bmesh fake/stub
var fakeFS embed.FS

// baseDate is the sandbox commit's date, so one fixture always makes the
// same base commit.
const baseDate = "2026-10-20T12:00:00Z"

// Cell is one case run on one arm, as cell.json keeps it.
type Cell struct {
	Case    string    `json:"case"`
	Arm     string    `json:"arm"`
	Run     int       `json:"run"`
	Model   string    `json:"model"`
	Plugin  string    `json:"plugin"`
	Host    string    `json:"host"`
	Base    string    `json:"base"`              // the sandbox's one commit, origin's main at the start
	Cwd     string    `json:"cwd,omitempty"`     // where the session runs, under the cell: the sandbox when empty
	Routine *Routine  `json:"routine,omitempty"` // a routine's model run, not a session of the plugin
	Turns   []Turn    `json:"turns"`
	Steers  int       `json:"steers"`
	Error   string    `json:"error,omitempty"`
	Left    []string  `json:"left,omitempty"` // worktrees the session made outside the cell, removed after it
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended"`
}

// Turn is one operator turn: the words sent and what the repos looked like
// after it. The reply and the usage are read from its file.
type Turn struct {
	Words   string   `json:"words"`
	Steer   bool     `json:"steer,omitempty"`
	File    string   `json:"file"` // the CLI's JSON, relative to the cell
	Session string   `json:"session,omitempty"`
	Error   string   `json:"error,omitempty"`
	WallMS  int64    `json:"wall_ms"`
	After   Snapshot `json:"after"`
}

// Snapshot is the state after a turn that the end state can't show.
type Snapshot struct {
	Origin  string `json:"origin"`  // origin's main
	Commits int    `json:"commits"` // commits on any ref of the sandbox or origin that the base lacks
	Calls   int    `json:"calls"`   // lines in the fake's log
}

// Env is what a case's canned outputs and setup may name.
type Env struct {
	Sandbox string
	Host    string
	Now     time.Time
}

func (e Env) Ago(d time.Duration) string { return e.Now.Add(-d).UTC().Format(time.RFC3339) }

// cwd is the directory the cell's session runs in.
func (c *Cell) cwd(dir string) string {
	if c.Cwd == "" {
		return filepath.Join(dir, sandboxDir)
	}
	return filepath.Join(dir, c.Cwd)
}

func (c *Cell) lastSession() string {
	for i := len(c.Turns) - 1; i >= 0; i-- {
		if c.Turns[i].Session != "" {
			return c.Turns[i].Session
		}
	}
	return ""
}

func (c *Cell) save(dir string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, cellFile), append(b, '\n'), 0o644)
}

func loadCell(dir string) (*Cell, error) {
	b, err := os.ReadFile(filepath.Join(dir, cellFile))
	if err != nil {
		return nil, err
	}
	var c Cell
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, cellFile), err)
	}
	return &c, nil
}

// gitEnv is the git config every git in a cell runs under, the session's
// included: the cell's own identity and nothing of this box's config (no
// credential helper, no hooks path, no signing).
func gitEnv(dir string) []string {
	return []string{"GIT_CONFIG_GLOBAL=" + filepath.Join(dir, "gitconfig"), "GIT_CONFIG_NOSYSTEM=1"}
}

const gitconfig = `[user]
	name = Shop Operator
	email = operator@shop.invalid
[init]
	defaultBranch = main
[commit]
	gpgsign = false
[advice]
	detachedHead = false
`

// cellSetup is what setupCell made: the base commit, where the session
// runs (relative to the cell; the sandbox when empty), its turns' words and,
// in a routine's case, the run.
type cellSetup struct {
	Base    string
	Cwd     string
	Words   []string
	Routine *Routine
}

// setupCell makes a cell in dir for case k on arm a: the fixture copied into
// the sandbox with the case's edits, committed once and pushed to a bare
// origin beside it as main, the fake bmesh and the stubs in bin/, and the
// fake's files. A lead's case gets its worktree of the sandbox on its own
// branch, where the session runs, and its first words are the build brief;
// a round's case the same, with the round brief. A routine's case gets the
// change its job reviews committed on a branch of the sandbox, and its words
// are the arm's kind prompt and the job.
func setupCell(dir string, k *Case, a Arm, fixture, host string, now time.Time) (cellSetup, error) {
	cs := cellSetup{Words: k.Turns}
	for _, d := range []string{"bin", "canned", "state", "tmp", "turns", "bmesh-config"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return cs, err
		}
	}
	sandbox := filepath.Join(dir, sandboxDir)
	origin := filepath.Join(dir, originDir)
	if err := copyTree(fixture, sandbox); err != nil {
		return cs, err
	}
	env := Env{Sandbox: sandbox, Host: host, Now: now}
	if k.Setup != nil {
		if err := k.Setup(env); err != nil {
			return cs, fmt.Errorf("case %s: setup: %w", k.Name, err)
		}
	}
	if err := writeFile(filepath.Join(dir, "gitconfig"), gitconfig); err != nil {
		return cs, err
	}
	genv := append(gitEnv(dir), "GIT_AUTHOR_DATE="+baseDate, "GIT_COMMITTER_DATE="+baseDate)
	steps := [][]string{
		{"-C", sandbox, "init", "-q", "-b", "main"},
		{"-C", sandbox, "add", "-A"},
		{"-C", sandbox, "commit", "-q", "--no-verify", "-m", "The shop as it stands"},
		{"init", "-q", "--bare", "-b", "main", origin},
		{"-C", sandbox, "remote", "add", "origin", origin},
		{"-C", sandbox, "push", "-q", "-u", "origin", "main"},
	}
	if l := k.Lead; l != nil {
		cs.Cwd = l.worktree()
		steps = append(steps, []string{"-C", sandbox, "worktree", "add", "-q", "-b", l.branch(), filepath.Join(dir, cs.Cwd), "main"})
	}
	if rd := k.Round; rd != nil {
		cs.Cwd = rd.worktree()
		steps = append(steps, []string{"-C", sandbox, "worktree", "add", "-q", "-b", rd.branch(), filepath.Join(dir, cs.Cwd), "main"})
	}
	for _, s := range steps {
		if _, err := git(dir, genv, s...); err != nil {
			return cs, err
		}
	}
	base, err := git(sandbox, genv, "rev-parse", "HEAD")
	if err != nil {
		return cs, err
	}
	cs.Base = base
	if k.Routine != nil {
		rt, words, err := routineRun(k, a, sandbox, genv)
		if err != nil {
			return cs, fmt.Errorf("case %s: %w", k.Name, err)
		}
		cs.Routine, cs.Words = rt, []string{words}
	}
	if k.Lead != nil {
		text, err := k.Lead.brief(sandbox, host, now)
		if err != nil {
			return cs, fmt.Errorf("case %s: the build brief: %w", k.Name, err)
		}
		cs.Words = []string{text}
	}
	fn := fakeNode{}
	if l := k.Lead; l != nil {
		fn = fakeNode{Node: l.Node, Worktree: l.worktree(), Spec: l.Spec, StopFirst: l.StopFirst}
	}
	if rd := k.Round; rd != nil {
		text, err := rd.brief(a, dir, sandbox, host, now)
		if err != nil {
			return cs, fmt.Errorf("case %s: the round brief: %w", k.Name, err)
		}
		cs.Words = []string{text}
		fn = fakeNode{Node: roundNode, Worktree: rd.worktree(), Round: true}
		// The mesh's counter holds the round's claims, so a spec next-id
		// the thread runs numbers past them.
		tmpl, err := rd.template(a)
		if err != nil {
			return cs, err
		}
		if err := writeFile(filepath.Join(dir, "state", "next-id"), strconv.Itoa(rd.claimed(tmpl))+"\n"); err != nil {
			return cs, err
		}
	}
	if err := installFake(dir, a, sandbox, host, fn); err != nil {
		return cs, err
	}
	canned, err := defaultCanned(sandbox, host)
	if err != nil {
		return cs, err
	}
	if k.Canned != nil {
		for key, text := range k.Canned(env) {
			canned[key] = text
		}
	}
	for key, text := range canned {
		if err := writeFile(filepath.Join(dir, "canned", key), text); err != nil {
			return cs, err
		}
	}
	return cs, nil
}

// fakeNode is the node a lead's or a round's cell runs as: its path, its
// worktree (relative to the cell), a lead's spec and stop, and whether it is
// a round's thread (whose spec list reads its checkout). Node "" is none.
type fakeNode struct {
	Node, Worktree, Spec string
	StopFirst, Round     bool
}

// installFake writes the fake bmesh, the stubs, the arm's commands and the
// fake's env file (with the cell's node, worktree and a lead's spec), and
// an inert bmesh config: the real binary, called by its path, finds no mesh
// to reach.
func installFake(dir string, a Arm, sandbox, host string, n fakeNode) error {
	fake, err := fakeFS.ReadFile("fake/bmesh")
	if err != nil {
		return err
	}
	stub, err := fakeFS.ReadFile("fake/stub")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "bmesh"), fake, 0o755); err != nil {
		return err
	}
	for _, name := range blocked {
		if err := os.WriteFile(filepath.Join(dir, "bin", name), stub, 0o755); err != nil {
			return err
		}
	}
	if err := writeFile(filepath.Join(dir, "commands.txt"), strings.Join(a.Commands, "\n")+"\n"); err != nil {
		return err
	}
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	fakeEnv := "repo=" + q(repoName) + "\nrepo_path=" + q(sandbox) + "\nhost=" + q(host) + "\n"
	if n.Node != "" {
		stop, round := "0", "0"
		if n.StopFirst {
			stop = "1"
		}
		if n.Round {
			round = "1"
		}
		fakeEnv += "node=" + q(n.Node) + "\nworktree=" + q(filepath.Join(dir, n.Worktree)) + "\nlead_spec=" + q(n.Spec) + "\nstop_first=" + stop +
			"\nround=" + round + "\n"
	}
	if err := writeFile(filepath.Join(dir, "fake.env"), fakeEnv); err != nil {
		return err
	}
	inert := "# skilltest: an inert config. A real bmesh run by its path in this cell reads it\n" +
		"# and dials a port nothing listens on, so it reaches no mesh.\n" +
		"[mesh]\nnats = \"nats://127.0.0.1:9\"\n"
	if err := writeFile(filepath.Join(dir, "bmesh-config", "config.toml"), inert); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "bmesh-config", ".setup-complete"), baseDate+"\n")
}

// row is the sandbox's registry row, as the fake's repos commands print it.
func row(sandbox, host string) config.Repo {
	return config.Repo{Name: repoName, Path: sandbox, Mode: "ship", Remote: "origin", Host: host, UXHost: host,
		Verify: []string{"go test ./..."}, Surfaces: []string{}, Routines: []string{"verify", "review"}, Code: []string{},
		SetupInputs: []string{}}
}

// defaultCanned is what the fake answers when a case says nothing else: a
// quiet mesh, the sandbox's row, its ledger and the half-pager.
func defaultCanned(sandbox, host string) (map[string]string, error) {
	r := row(sandbox, host)
	out := map[string]string{
		"mesh-status":      table([]string{"PATH", "ROLE", "STATE", "SINCE", "RUNNER", "UNREAD"}),
		"mesh-status.json": "[]\n",
	}
	j, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	out["repos-show.json"] = string(j) + "\n"
	out["repos.json"] = "[" + string(j) + "]\n"
	out["repos"] = table([]string{"NAME", "MODE", "REMOTE", "VERIFY", "HOST", "UX_HOST", "SURFACES", "PIN"},
		[]string{r.Name, r.Mode, r.Remote, strings.Join(r.Verify, "; "), r.Host, r.UXHost, "", ""})
	out["repos-show"] = fmt.Sprintf("name: %s\npath: %s\nmode: %s\nremote: %s\nhost: %s\nux_host: %s\nverify: %s\nsurfaces: \npin: \nroutines: %s\ncode: (everything but the spec files)\nsetup: (none)\n",
		r.Name, r.Path, r.Mode, r.Remote, r.Host, r.UXHost, strings.Join(r.Verify, "; "), strings.Join(r.Routines, ", "))
	specs, err := spec.Scan(sandbox, "docs/proposals/{id}-*.md")
	if err != nil {
		return nil, fmt.Errorf("the fixture's ledger: %w", err)
	}
	for _, st := range append([]string{""}, spec.Statuses...) {
		var rows []string
		objs := []map[string]string{}
		for _, s := range specs {
			if st == "" || s.Status() == st {
				rows = append(rows, s.Row())
				objs = append(objs, s.JSON())
			}
		}
		key := "spec-list"
		if st != "" {
			key += "." + st
		}
		out[key] = strings.Join(rows, "\n")
		if len(rows) > 0 {
			out[key] += "\n"
		}
		b, err := json.Marshal(objs)
		if err != nil {
			return nil, err
		}
		out[key+".json"] = string(b) + "\n"
	}
	half, _, err := brief.Template("", "half-pager")
	if err != nil {
		return nil, err
	}
	out["spec-template"] = half
	return out, nil
}

// table lays rows out as bmesh's tables are: columns two spaces apart.
func table(rows ...[]string) string {
	var b bytes.Buffer
	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
	return b.String()
}

// snapshot records the state after a turn.
func snapshot(dir, base string) Snapshot {
	var s Snapshot
	origin := filepath.Join(dir, originDir)
	s.Origin, _ = git(dir, gitEnv(dir), "--git-dir", origin, "rev-parse", "--verify", "-q", "refs/heads/main")
	seen := map[string]bool{}
	for _, repo := range [][]string{{"-C", filepath.Join(dir, sandboxDir)}, {"--git-dir", origin}} {
		out, err := git(dir, gitEnv(dir), append(repo, "rev-list", "--all", "--not", base)...)
		if err != nil {
			continue
		}
		for _, c := range strings.Fields(out) {
			seen[c] = true
		}
	}
	s.Commits = len(seen)
	if b, err := os.ReadFile(filepath.Join(dir, logFile)); err == nil {
		s.Calls = bytes.Count(b, []byte("\n"))
	}
	return s
}

// leftovers are the entries of a finished cell that are the session's work
// rather than the harness's: they go into work.tar.gz.
func leftovers(dir string) ([]string, error) {
	keep := map[string]bool{"bin": true, "canned": true, "state": true, "turns": true, "bmesh-config": true,
		originDir: true, logFile: true, blockedLog: true, cellFile: true, workTar: true, "commands.txt": true,
		"fake.env": true, "gitconfig": true, "claude-project": true}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if !keep[e.Name()] {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// keepLeadBranch keeps a lead's branch, as the worktree's HEAD stands, in
// origin.git as leadRef: the worktree goes into the cell's archive, and
// scoring reads only what the cell keeps. A cell that is no lead's keeps
// nothing.
func keepLeadBranch(dir string, c *Cell) error {
	if c.Cwd == "" {
		return nil
	}
	_, err := git(dir, gitEnv(dir), "-C", c.cwd(dir), "push", "-q", "--force", "origin", "HEAD:"+leadRef)
	return err
}

// pruneWorktrees removes the worktrees of the sandbox that the session made
// outside the cell, and returns their paths: what a session leaves on the
// box goes with its cell.
func pruneWorktrees(dir string) []string {
	sandbox := filepath.Join(dir, sandboxDir)
	out, err := git(sandbox, gitEnv(dir), "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	real := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	cell := real(dir)
	var left []string
	for _, line := range strings.Split(out, "\n") {
		p, ok := strings.CutPrefix(line, "worktree ")
		if !ok || real(p) == real(sandbox) || under(real(p), cell) {
			continue
		}
		left = append(left, p)
		_, _ = git(sandbox, gitEnv(dir), "worktree", "remove", "--force", p)
	}
	_, _ = git(sandbox, gitEnv(dir), "worktree", "prune")
	return left
}
