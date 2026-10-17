package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/brief"
	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/daemon"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

const ideateUsage = "usage: bmesh ideate <repo> [--idea] [--model M] [--effort E] [--context <text|->] [--force]"

// proposedCap is how many proposed specs a repo may hold before a round is
// refused: they are waiting on the operator's review.
const proposedCap = 5

// Children are a round's two threads: eng (the engineer) and ux (the UI/UX
// engineer), each a node under the round.
var children = []string{"eng", "ux"}

var roundRE = regexp.MustCompile(`^(.*)-round-(\d+)$`)

// childStarted is what starting one child prints: the node and its session.
type childStarted struct {
	Path     string `json:"path"`
	Thread   string `json:"thread"`
	Branch   string `json:"branch"`
	Worktree string `json:"worktree"`
	Host     string `json:"host"`
}

// cmdIdeate runs a proposal round on a ship repo: bmesh ideate <repo>
// [--idea] [--model M] [--effort E] [--context <text|->] [--force]. With
// --idea the round is the operator's one idea, given in --context: only
// the engineer starts, and it writes that idea's spec. It refuses five or more proposed specs and,
// without --force, a round of the repo's that is still open; levels main, picks
// the next round number from the nodes bucket, registers the round node
// and starts the two children through the runner on the row's host and
// ux_host (a remote host runs this command's child form over ssh). The
// child form, --child eng|ux with --round, --next-id and --thread-id, makes
// one child's worktree from main, renders its brief, starts it and
// registers its node; the full form calls it for a child on another box.
func (a *app) cmdIdeate(args []string) error {
	fs := a.flags("ideate")
	model := fs.String("model", "", "model the threads run on (default [models] proposer)")
	effort := fs.String("effort", "", "the threads' effort (default [models] proposer_effort)")
	child := fs.String("child", "", "start one child only: eng or ux (the child form)")
	round := fs.Int("round", 0, "child form: the round number")
	nextID := fs.Int("next-id", 0, "child form: the first spec id")
	thread := fs.String("thread-id", "", "child form: the thread's UUID")
	focus := fs.String("context", "", "the operator's words for both briefs (- reads stdin)")
	force := fs.Bool("force", false, "start a round while another of the repo's is still open")
	idea := fs.Bool("idea", false, "one idea, the operator's, in --context: start only the engineer, which writes that idea's spec")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail(exitUsage, ideateUsage)
	}
	if err := checkModel(*model); err != nil {
		return err
	}
	if err := checkEffort(*effort); err != nil {
		return err
	}
	if *focus == "-" {
		b, err := io.ReadAll(a.stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		*focus = string(b)
	}
	*focus = strings.TrimSpace(*focus)
	if err := a.loadConfig(); err != nil {
		return err
	}
	name, repoPath, err := a.cfg.RepoPath(pos[0])
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	row, ok := a.cfg.Repos[name]
	if !ok {
		return fail(exitUsage, "%s is not in repos.toml; a round needs its row (host, ux_host, verify, surfaces)", name)
	}
	if row.Mode != "ship" {
		return fail(exitRefused, "refused: %s is %s; proposal rounds are for ship repos only", name, row.Mode)
	}
	if *model == "" {
		*model = a.cfg.Models.Proposer
	}
	if *effort == "" {
		*effort = a.cfg.Models.ProposerEffort
	}
	if a.configPath != "" {
		if abs, err := filepath.Abs(a.configPath); err == nil {
			os.Setenv("BMESH_CONFIG", abs)
		}
	}
	if *child != "" {
		if (*child != "eng" && *child != "ux") || *round <= 0 || *nextID <= 0 || *thread == "" {
			return fail(exitUsage, "the child form needs --child eng|ux, --round N, --next-id N and --thread-id UUID")
		}
		if *idea && *child != "eng" {
			return fail(exitUsage, "--idea starts only the engineer (--child eng)")
		}
		if *idea && *focus == "" {
			return fail(exitUsage, "--idea needs the idea itself in --context")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		started, err := a.startChild(ctx, row, repoPath, *round, *child, *nextID, *thread, *model, *effort, *focus, *idea)
		if err != nil {
			return err
		}
		return a.printJSON(started)
	}
	if *idea && *focus == "" {
		return fail(exitUsage, "--idea needs the idea itself in --context")
	}
	return a.runRound(row, repoPath, *model, *effort, *focus, *force, *idea)
}

// runRound is the full form. An idea round starts only the engineer.
func (a *app) runRound(row config.Repo, repoPath, model, effort, focus string, force, idea bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	me := a.cfg.HostName()
	hosts := map[string]string{"eng": row.Host, "ux": row.UXHost}
	if hosts["eng"] == "" {
		hosts["eng"] = me
	}
	if hosts["ux"] == "" {
		hosts["ux"] = hosts["eng"]
	}
	kids := children
	if idea {
		kids = []string{"eng"}
		delete(hosts, "ux")
	}
	for _, c := range kids {
		if h := hosts[c]; h != me {
			if err := reachable(ctx, h); err != nil {
				return fail(exitRefused, "refused: host %s (the %s thread's) is unreachable from %s: %v", h, c, me, err)
			}
		}
	}
	if hosts["eng"] == me || hosts["ux"] == me {
		r, err := runner.New(a.cfg)
		if err != nil {
			return err
		}
		if err := a.preflight(ctx, r); err != nil {
			return err
		}
	}
	if err := levelMain(ctx, repoPath); err != nil {
		return fail(exitRefused, "refused: %v", err)
	}
	specs, err := spec.ScanRef(repoPath, a.cfg.Spec.Glob, "main")
	if err != nil {
		return ledgerFail(err)
	}
	var proposed []string
	for _, s := range specs {
		if s.Status() == "proposed" {
			proposed = append(proposed, s.IDString())
		}
	}
	if len(proposed) >= proposedCap {
		return fail(exitRefused, "refused: %s has %d proposed specs waiting on review (%s); review them first",
			row.Name, len(proposed), strings.Join(proposed, ", "))
	}
	next := spec.NextID(specs)
	if err := a.connect(ctx, "ideate"); err != nil {
		return err
	}
	if !force {
		open, err := openRound(ctx, a.client, row.Name)
		if err != nil {
			return err
		}
		if open != "" {
			return fail(exitRefused, "refused: %s; wait for it, or pass --force to start another", open)
		}
	}
	n, err := nextRound(ctx, a.client, row.Name)
	if err != nil {
		return err
	}
	roundPath := fmt.Sprintf("%s-round-%d", mesh.RepoToken(row.Name), n)
	if _, err := mesh.ParsePath(roundPath); err != nil {
		return fail(exitUsage, "round path: %v", err)
	}
	threads := map[string]string{}
	for _, c := range kids {
		if threads[c], err = uuid4(); err != nil {
			return err
		}
	}
	round := mesh.Node{Path: roundPath, Role: "round", Repo: row.Name, RepoPath: repoPath, Host: me, Model: model, Effort: effort,
		Idea: idea, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := mesh.PutNode(ctx, a.client, round); err != nil {
		return err
	}
	rp, _ := mesh.ParsePath(roundPath)
	if err := mesh.EnsureInbox(ctx, a.client, rp); err != nil {
		return err
	}
	text := fmt.Sprintf("round %d on %s: eng on %s, ux on %s", n, row.Name, hosts["eng"], hosts["ux"])
	if idea {
		text = fmt.Sprintf("round %d on %s: one idea, eng on %s", n, row.Name, hosts["eng"])
	}
	if err := mesh.PublishEvent(ctx, a.client, mesh.Event{From: roundPath, Event: "started", Text: text}); err != nil {
		return err
	}
	if _, err := mesh.WriteState(ctx, a.client, roundPath, mesh.State{State: "working", Text: text, By: "node"}); err != nil {
		return err
	}
	out := map[string]any{"path": roundPath, "round": n}
	ids := map[string]int{"eng": next, "ux": next + 5}
	var failed []string
	for _, c := range kids {
		var started childStarted
		if hosts[c] == me {
			started, err = a.startChild(ctx, row, repoPath, n, c, ids[c], threads[c], model, effort, focus, idea)
		} else {
			started, err = a.startRemoteChild(ctx, hosts[c], row.Name, n, c, ids[c], threads[c], model, effort, focus, idea)
		}
		if err != nil {
			// The other child is still started: the one that did not is
			// blocked, so the round reads blocked naming it and the daemon
			// resettles it with the other's pitch when that one finishes.
			why := fmt.Sprintf("did not start: %v", err)
			child := roundPath + "." + c
			_, _ = mesh.WriteState(ctx, a.client, child, mesh.State{State: "working", By: "node"})
			_, _ = mesh.WriteState(ctx, a.client, child, mesh.State{State: "blocked", Text: why, By: "node"})
			failed = append(failed, fmt.Sprintf("the %s thread %s", c, why))
			continue
		}
		out[c] = started
	}
	if len(failed) > 0 {
		if states, err := mesh.ReadAllStates(ctx, a.client); err == nil {
			if state, text, ok := daemon.SettleRound(daemon.RoundStates(round, states)); ok {
				if _, err := mesh.WriteState(ctx, a.client, roundPath, mesh.State{State: state, Text: text, By: "node"}); err == nil {
					_ = mesh.PublishEvent(ctx, a.client, mesh.Event{From: roundPath, Event: state, Text: text})
				}
			}
		}
		return fmt.Errorf("round %s: %s", roundPath, strings.Join(failed, "; "))
	}
	return a.printJSON(out)
}

// nextRound is one past the highest round number registered for repo.
func nextRound(ctx context.Context, c *mesh.Client, repo string) (int, error) {
	nodes, err := mesh.ListNodes(ctx, c)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, node := range nodes {
		m := roundRE.FindStringSubmatch(node.Path)
		if m == nil || m[1] != mesh.RepoToken(repo) {
			continue
		}
		if k, err := strconv.Atoi(m[2]); err == nil && k > n {
			n = k
		}
	}
	return n + 1, nil
}

// openRound describes the repo's newest round that is not settled (its
// state neither done nor blocked) with its children's states, or is "" when
// every round of the repo is settled. A second round from the same main
// would number its specs from the same id and run the same tests at once.
func openRound(ctx context.Context, c *mesh.Client, repo string) (string, error) {
	nodes, err := mesh.ListNodes(ctx, c)
	if err != nil {
		return "", err
	}
	states, err := mesh.ReadAllStates(ctx, c)
	if err != nil {
		return "", err
	}
	open, newest := "", 0
	for _, node := range nodes {
		m := roundRE.FindStringSubmatch(node.Path)
		if m == nil || m[1] != mesh.RepoToken(repo) {
			continue
		}
		k, err := strconv.Atoi(m[2])
		if err != nil || k <= newest {
			continue
		}
		switch states[node.Path].State {
		case "", "done", "blocked":
			continue
		}
		var kids []string
		for _, child := range children {
			if st := states[node.Path+"."+child].State; st != "" {
				kids = append(kids, child+" "+st)
			}
		}
		open, newest = node.Path+" is still open", k
		if len(kids) > 0 {
			open += " (" + strings.Join(kids, ", ") + ")"
		}
	}
	return open, nil
}

// startChild makes one child's bmesh worktree from main, renders its brief,
// starts it through this box's runner and registers its node.
func (a *app) startChild(ctx context.Context, row config.Repo, repoPath string, round int, child string, nextID int,
	thread, model, effort, focus string, idea bool) (childStarted, error) {
	if r, err := runner.New(a.cfg); err != nil {
		return childStarted{}, err
	} else if err := a.preflight(ctx, r); err != nil {
		return childStarted{}, err
	}
	if err := levelMain(ctx, repoPath); err != nil {
		return childStarted{}, fail(exitRefused, "refused: %v", err)
	}
	roundPath := fmt.Sprintf("%s-round-%d", mesh.RepoToken(row.Name), round)
	path := roundPath + "." + child
	if _, err := mesh.ParsePath(path); err != nil {
		return childStarted{}, fail(exitUsage, "%v", err)
	}
	if err := a.connect(ctx, "ideate"); err != nil {
		return childStarted{}, err
	}
	_, exists, err := mesh.GetNode(ctx, a.client, path)
	if err != nil {
		return childStarted{}, err
	}
	st, _, err := mesh.ReadState(ctx, a.client, path)
	if err != nil {
		return childStarted{}, err
	}
	if exists && st.State != "done" && st.State != "blocked" {
		return childStarted{}, fail(exitRefused, "refused: node %s exists (state %s)", path, st.State)
	}
	branch := fmt.Sprintf("%s-round-%d-%s", mesh.RepoToken(row.Name), round, child)
	wt := filepath.Join(a.cfg.Expand(a.cfg.Runner.Worktrees), row.Name, branch)
	if abs, err := filepath.Abs(wt); err == nil {
		wt = abs
	}
	if err := addWorktree(ctx, repoPath, wt, branch, "main"); err != nil {
		return childStarted{}, err
	}
	undo := func() {
		_ = removeWorktree(context.WithoutCancel(ctx), repoPath, wt, branch, false)
	}
	tmpl, title := "engineer", "engineer"
	if child == "ux" {
		tmpl, title = "ux-engineer", "UI/UX"
	}
	values := brief.RowValues(row, repoPath)
	for k, v := range map[string]string{
		"node": path, "round": roundPath, "round_n": strconv.Itoa(round), "worktree": wt, "branch": branch,
		"thread_id": thread, "next_id": fmt.Sprintf("%03d", nextID), "date": time.Now().UTC().Format("2006-01-02"),
		"operator_context": operatorSection(focus, idea),
	} {
		values[k] = v
	}
	maps.Copy(values, brief.RoundValues(idea))
	text, err := brief.Render(repoPath, tmpl, values)
	if err != nil {
		undo()
		return childStarted{}, err
	}
	if section := values["operator_context"]; section != "" && !strings.Contains(text, section) {
		// A repo's own template without {{operator_context}} still gets
		// the operator's words.
		text = strings.TrimRight(text, "\n") + "\n\n" + strings.TrimRight(section, "\n") + "\n"
	}
	briefPath, err := writeBrief(roundPath+"-"+child, text)
	if err != nil {
		undo()
		return childStarted{}, err
	}
	r, err := runner.New(a.cfg)
	if err != nil {
		undo()
		return childStarted{}, err
	}
	started, err := r.Start(context.WithoutCancel(ctx), runner.StartSpec{RepoPath: repoPath, Model: model, Effort: effort,
		Title: fmt.Sprintf("[ideate] %s round %d: %s", row.Name, round, title), BriefPath: briefPath, ThreadID: thread,
		Branch: branch, Worktree: wt})
	var to *runner.StartTimeout
	if errors.As(err, &to) {
		return childStarted{}, fmt.Errorf("start timed out; a thread on branch %s may exist (worktree %s)", to.Branch, wt)
	}
	if errors.Is(err, runner.ErrTimeout) {
		return childStarted{}, fmt.Errorf("start timed out; thread id %s may exist (worktree %s)", thread, wt)
	}
	var se *runner.SignInError
	if errors.As(err, &se) {
		undo()
		return childStarted{}, fail(exitRefused, "%s", se.Msg)
	}
	if err != nil {
		undo()
		return childStarted{}, fmt.Errorf("runner start (thread id %s): %w", thread, err)
	}
	node := mesh.Node{Path: path, Role: "engineer", Parent: roundPath, Repo: row.Name, RepoPath: repoPath,
		Branch: branch, Worktree: wt, Host: a.cfg.HostName(), Model: model, Effort: effort,
		Runner:    nodeRunner(a.cfg.Runner.Kind, started.Thread, thread),
		StartedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := mesh.PutNode(ctx, a.client, node); err != nil {
		return childStarted{}, fmt.Errorf("thread %s started (worktree %s) but registering %s failed: %w", started.Thread, wt, path, err)
	}
	p, _ := mesh.ParsePath(path)
	if err := mesh.EnsureInbox(ctx, a.client, p); err != nil {
		return childStarted{}, err
	}
	if exists {
		if err := mesh.ResetState(ctx, a.client, path); err != nil {
			return childStarted{}, err
		}
	}
	return childStarted{Path: path, Thread: started.Thread, Branch: branch, Worktree: wt, Host: a.cfg.HostName()}, nil
}

// operatorSection is the brief's section for the operator's words: their
// focus for a round, or, for an idea round, the one idea to write up.
func operatorSection(focus string, idea bool) string {
	if idea {
		return brief.OperatorIdea(focus)
	}
	return brief.OperatorContext(focus)
}

// writeBrief writes a rendered brief under <user cache dir>/bmesh/briefs,
// where it stays for reading.
func writeBrief(name, text string) (string, error) {
	c, err := config.CacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(c, "bmesh", "briefs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(dir, name+".md")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		return "", err
	}
	return p, nil
}

// sshCommand is ssh, or $BMESH_SSH_COMMAND split on spaces (a test hook).
func sshCommand() []string {
	if v := strings.Fields(os.Getenv("BMESH_SSH_COMMAND")); len(v) > 0 {
		return v
	}
	return []string{"ssh"}
}

// ssh runs a shell command line on host through that box's login shell and
// returns its stdout.
func ssh(ctx context.Context, host, line string) ([]byte, error) {
	s := sshCommand()
	args := append(append([]string{}, s[1:]...), "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", host, "bash", "-lc", shellQuote(line))
	cmd := exec.CommandContext(ctx, s[0], args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return out.Bytes(), nil
}

// reachable checks that host answers over ssh and has bmesh on its PATH.
func reachable(ctx context.Context, host string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := ssh(ctx, host, "bmesh help >/dev/null")
	return err
}

// startRemoteChild runs the child form on host and reads what it printed.
func (a *app) startRemoteChild(ctx context.Context, host, repo string, round int, child string, nextID int,
	thread, model, effort, focus string, idea bool) (childStarted, error) {
	parts := []string{"bmesh", "ideate", repo, "--child", child, "--round", strconv.Itoa(round),
		"--next-id", strconv.Itoa(nextID), "--thread-id", thread, "--model", model}
	if effort != "" {
		parts = append(parts, "--effort", effort)
	}
	if focus != "" {
		parts = append(parts, "--context", focus)
	}
	if idea {
		parts = append(parts, "--idea")
	}
	for i, p := range parts {
		parts[i] = shellQuote(p)
	}
	out, err := ssh(ctx, host, strings.Join(parts, " "))
	if err != nil {
		return childStarted{}, fmt.Errorf("on %s: %w", host, err)
	}
	var started childStarted
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &started); err != nil {
		return childStarted{}, fmt.Errorf("on %s: bmesh ideate printed %q", host, strings.TrimSpace(string(out)))
	}
	if started.Host == "" {
		started.Host = host
	}
	return started, nil
}
