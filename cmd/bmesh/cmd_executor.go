package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

const executorUsage = "usage: bmesh executor new --name N [--base REF] [--model M] [--json] | list [--json] | remove <name> [--force]"

// executorHeader is pasted at the top of an executor's Agent prompt. It
// names the node on every bmesh command rather than exporting BMESH_NODE:
// the harness's shell keeps no environment between tool calls, and a bmesh
// run from the lead's worktree would otherwise act as the lead.
const executorHeader = "You are executor %s in buildermesh. Work only in %s on branch %s: every file path you touch is under it, " +
	"and every shell command starts with `cd %s &&` (the working directory and environment do not persist between tool calls). " +
	"Every bmesh command carries --node %s. " +
	"Before anything else run `cd %s && bmesh --node %s started --text \"<what you will do>\"`. " +
	"Report the finished piece with `bmesh --node %s done --commit <sha> --text \"<one line>\"` alone: " +
	"the mesh puts it, with your branch, in your lead's inbox. " +
	"Read your brief below, then invoke the `buildermesh:executor` skill.\n"

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func (a *app) cmdExecutor(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "new":
			return a.cmdExecutorNew(args[1:])
		case "list":
			return a.cmdExecutorList(args[1:])
		case "remove":
			return a.cmdExecutorRemove(args[1:])
		}
	}
	return fail(exitUsage, executorUsage)
}

// gitIn runs git in dir and returns its trimmed output; a failure names
// the subcommand and carries git's stderr.
func gitIn(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

// parentNode resolves the node this command acts for (the lead, or an
// executor fanning out again): its registration, its worktree on this host
// and its branch.
func (a *app) parentNode(ctx context.Context, cmd string) (mesh.Node, error) {
	p, err := a.identity(ctx, cmd)
	if err != nil {
		return mesh.Node{}, err
	}
	if _, ok := mesh.KindFromPath(p); ok {
		return mesh.Node{}, fail(exitRefused, "refused: %s is a routine path; executors belong to leads", p)
	}
	n, ok, err := mesh.GetNode(ctx, a.client, p.String())
	if err != nil {
		return mesh.Node{}, err
	}
	if !ok {
		return mesh.Node{}, fail(exitError, "node %s is not registered", p)
	}
	if n.Worktree == "" {
		top, err := gitRev("--show-toplevel")
		if err != nil {
			return mesh.Node{}, fail(exitError, "node %s has no worktree and the cwd is not a git checkout", p)
		}
		n.Worktree = top
	}
	// The worktree's current branch wins over the one recorded at start:
	// a console may have renamed it since.
	if b, err := gitIn(ctx, n.Worktree, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && b != "HEAD" {
		n.Branch = b
	} else if n.Branch == "" {
		if err != nil {
			return mesh.Node{}, err
		}
		n.Branch = n.Path // detached: name the branch after the node
	}
	if n.RepoPath == "" {
		common, err := gitIn(ctx, n.Worktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil {
			return mesh.Node{}, err
		}
		n.RepoPath = filepath.Dir(common)
	}
	if n.Repo == "" {
		n.Repo = filepath.Base(n.RepoPath)
	}
	return n, nil
}

// cmdExecutorNew makes a worktree on a new branch for one executor under the
// calling node and registers it: bmesh executor new --name N [--base REF] [--model M].
func (a *app) cmdExecutorNew(args []string) error {
	fs := a.flags("executor new")
	name := fs.String("name", "", "executor name ([a-z0-9-], unique under this node)")
	base := fs.String("base", "", "commit to branch from (default this node's HEAD)")
	model := fs.String("model", "", "model the executor runs on")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || *name == "" {
		return fail(exitUsage, executorUsage)
	}
	if !nameRE.MatchString(*name) || strings.Contains(*name, "--") {
		return fail(exitUsage, "name %q: want lowercase letters, digits and hyphens, no \"--\"", *name)
	}
	if err := checkModel(*model); err != nil {
		return err
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	parent, err := a.parentNode(ctx, "executor new")
	if err != nil {
		return err
	}
	path := parent.Path + "." + *name
	if _, err := mesh.ParsePath(path); err != nil {
		return fail(exitUsage, "%v", err)
	}
	_, exists, err := mesh.GetNode(ctx, a.client, path)
	if err != nil {
		return err
	}
	st, _, err := mesh.ReadState(ctx, a.client, path)
	if err != nil {
		return err
	}
	if exists && st.State != "done" && st.State != "blocked" {
		return fail(exitRefused, "refused: executor %s exists (state %s); remove it or pick another name", path, st.State)
	}
	branch := parent.Branch + "-" + *name
	wt := filepath.Join(a.cfg.Expand(a.cfg.Runner.Worktrees), parent.Repo, path)
	if abs, err := filepath.Abs(wt); err == nil {
		wt = abs
	}
	start := *base
	if start == "" {
		start = "HEAD"
	}
	sha, err := gitIn(ctx, parent.Worktree, "rev-parse", "--verify", "--quiet", "--end-of-options", start+"^{commit}")
	if err != nil {
		return fail(exitUsage, "--base %q is not a commit in %s", start, parent.Worktree)
	}
	// The branch must be ours to roll back: refuse one that already exists.
	if _, err := gitIn(ctx, parent.Worktree, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return fail(exitRefused, "refused: branch %s already exists; remove it or pick another name", branch)
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", parent.Worktree, "worktree", "add", "-b", branch, wt, sha).CombinedOutput(); err != nil {
		// Nothing is registered; drop whatever git left behind (a worktree
		// made before a hook failed holds the branch, so it goes first).
		_, _ = gitIn(ctx, parent.Worktree, "worktree", "remove", "--force", wt)
		_, _ = gitIn(ctx, parent.Worktree, "worktree", "prune")
		_, _ = gitIn(ctx, parent.Worktree, "branch", "-D", branch)
		return fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	n := mesh.Node{Path: path, Role: "executor", Parent: parent.Path, Repo: parent.Repo, RepoPath: parent.RepoPath,
		Spec: parent.Spec, Branch: branch, Worktree: wt, Host: a.cfg.HostName(), Model: *model,
		StartedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := mesh.PutNode(ctx, a.client, n); err != nil {
		return err
	}
	p, _ := mesh.ParsePath(path)
	if err := mesh.EnsureInbox(ctx, a.client, p); err != nil {
		return err
	}
	if exists {
		if err := mesh.ResetState(ctx, a.client, path); err != nil {
			return err
		}
	}
	line, err := json.Marshal(map[string]string{"path": path, "branch": branch, "worktree": wt})
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%s\n", line)
	if !a.json {
		fmt.Fprintf(a.stdout, executorHeader, path, wt, branch, wt, path, wt, path, path)
	}
	return nil
}

type executorRow struct {
	Path   string `json:"path"`
	State  string `json:"state"`
	Branch string `json:"branch"`
	Head   string `json:"head"`
	Unread int    `json:"unread"`
}

// cmdExecutorList prints the calling node's lead's tree: every node under
// the lead, with state, branch, head (when its worktree is on this host)
// and unread count.
func (a *app) cmdExecutorList(args []string) error {
	pos, err := parse(a.flags("executor list"), args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, executorUsage)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	me, err := a.identity(ctx, "executor list")
	if err != nil {
		return err
	}
	lead := me.Lead()
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return err
	}
	states, err := mesh.ReadAllStates(ctx, a.client)
	if err != nil {
		return err
	}
	rows := []executorRow{}
	for _, n := range nodes {
		if !strings.HasPrefix(n.Path, lead+".") {
			continue
		}
		r := executorRow{Path: n.Path, State: "idle", Branch: n.Branch, Head: "-"}
		if s, ok := states[n.Path]; ok {
			r.State = s.State
		}
		if n.Host == a.cfg.HostName() && n.Worktree != "" {
			if h, err := gitIn(ctx, n.Worktree, "rev-parse", "--short", "HEAD"); err == nil {
				r.Head = h
			}
		}
		p, err := mesh.ParsePath(n.Path)
		if err != nil {
			return fmt.Errorf("node %s: %w", n.Path, err)
		}
		if r.Unread, err = mesh.Unread(ctx, a.client, p); err != nil {
			return err
		}
		rows = append(rows, r)
	}
	if a.json {
		return a.printJSON(rows)
	}
	tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PATH\tSTATE\tBRANCH\tHEAD\tUNREAD")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\n", r.Path, r.State, dash(r.Branch), r.Head, r.Unread)
	}
	return tw.Flush()
}

// cmdExecutorRemove removes an executor's worktree and branch once the
// calling node has merged it (or with --force), forgets the worktree in its
// node and marks it done.
func (a *app) cmdExecutorRemove(args []string) error {
	fs := a.flags("executor remove")
	force := fs.Bool("force", false, "remove an unmerged or dirty executor")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fail(exitUsage, executorUsage)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	parent, err := a.parentNode(ctx, "executor remove")
	if err != nil {
		return err
	}
	path := parent.Path + "." + pos[0]
	n, ok, err := mesh.GetNode(ctx, a.client, path)
	if err != nil {
		return err
	}
	// A removed executor keeps its node (and branch name) but no worktree.
	if !ok || n.Role != "executor" || n.Worktree == "" || n.Branch == "" {
		return fail(exitRefused, "refused: no executor %s under %s with a worktree", path, parent.Path)
	}
	if !*force {
		merged, err := gitIn(ctx, parent.Worktree, "branch", "--list", "--merged", "HEAD", n.Branch)
		if err != nil {
			return err
		}
		if merged == "" {
			return fail(exitRefused, "refused: branch %s is not merged into %s; merge it or pass --force", n.Branch, parent.Branch)
		}
	}
	if n.Host == a.cfg.HostName() {
		wtArgs := []string{"worktree", "remove"}
		if *force {
			wtArgs = append(wtArgs, "--force")
		}
		if _, err := gitIn(ctx, parent.Worktree, append(wtArgs, n.Worktree)...); err != nil {
			// Already gone from disk: let git forget it.
			if _, perr := gitIn(ctx, parent.Worktree, "worktree", "prune"); perr != nil || dirExists(n.Worktree) {
				return err
			}
		}
	}
	del := "-d"
	if *force {
		del = "-D"
	}
	if _, err := gitIn(ctx, parent.Worktree, "branch", del, n.Branch); err != nil {
		return err
	}
	n.Worktree = ""
	if err := mesh.PutNode(ctx, a.client, n); err != nil {
		return err
	}
	// Its unread mail would otherwise reach the next executor of this name.
	if p, err := mesh.ParsePath(path); err == nil {
		if _, err := mesh.Fetch(ctx, a.client, p, true); err != nil {
			return err
		}
	}
	if err := markDone(ctx, a.client, path, "removed by "+parent.Path); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "removed %s\n", path)
	return nil
}

// markDone writes done for path unless it reads done already. An executor
// that never started is idle, from which done is not a move, so it passes
// through working first.
func markDone(ctx context.Context, c *mesh.Client, path, text string) error {
	if cur, _, err := mesh.ReadState(ctx, c, path); err != nil || cur.State == "done" {
		return err
	}
	done := mesh.State{State: "done", Text: text, By: "node"}
	_, err := mesh.WriteState(ctx, c, path, done)
	var ill mesh.ErrIllegalTransition
	if errors.As(err, &ill) && ill.From == "idle" {
		if _, err := mesh.WriteState(ctx, c, path, mesh.State{State: "working", By: "node"}); err != nil {
			return err
		}
		_, err = mesh.WriteState(ctx, c, path, done)
	}
	return err
}

func dirExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
