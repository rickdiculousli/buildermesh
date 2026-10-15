package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

func (a *app) cmdLead(args []string) error {
	if len(args) == 0 || args[0] != "start" {
		return fail(exitUsage, "usage: bmesh lead start --repo R --spec NNN --brief FILE [--model M] [--effort E] [--title T] [--path P]")
	}
	return a.cmdLeadStart(args[1:])
}

// cmdLeadStart starts a lead through the configured runner and registers it.
func (a *app) cmdLeadStart(args []string) error {
	fs := a.flags("lead start")
	repo := fs.String("repo", "", "repo name ([repos]) or path")
	spec := fs.String("spec", "", "spec id, e.g. 012")
	brief := fs.String("brief", "", "file holding the lead's first prompt")
	model := fs.String("model", "", "model (default [models] lead)")
	effort := fs.String("effort", "", "effort: low, medium, high, xhigh or max (default [models] lead_effort)")
	title := fs.String("title", "", "thread title (default \"[build] <repo> <spec>\")")
	pathFlag := fs.String("path", "", "node path (default <repo>-<spec>)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 || *repo == "" || *spec == "" || *brief == "" {
		return fail(exitUsage, "usage: bmesh lead start --repo R --spec NNN --brief FILE [--model M] [--effort E] [--title T] [--path P]")
	}
	if err := checkEffort(*effort); err != nil {
		return err
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	name, repoPath, err := a.cfg.RepoPath(*repo)
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	briefPath, err := filepath.Abs(*brief)
	if err != nil {
		return err
	}
	started, err := a.startLead(leadStart{Name: name, RepoPath: repoPath, Spec: *spec, BriefPath: briefPath, Model: *model, Effort: *effort,
		Title: *title, Path: *pathFlag})
	if err != nil {
		return err
	}
	return a.printJSON(started)
}

// leadStart is what starting a lead takes: the repo (its registry name and
// checkout), the spec, the brief and, optionally, the model (default
// [models] lead), the title (default "[build] <repo> <spec>"), the node
// path (default <repo>-<spec>) and the thread id (default a fresh UUID).
type leadStart struct {
	Name, RepoPath, Spec, BriefPath, Model, Effort, Title, Path, Thread string
}

// leadStarted is what startLead prints: the node and its session.
type leadStarted struct {
	Path     string `json:"path"`
	Thread   string `json:"thread"`
	Branch   string `json:"branch"`
	Worktree string `json:"worktree"`
}

// leadPath is the node path a lead start registers: --path, else
// <repo>-<spec>. It refuses the routine pool's paths.
func leadPath(name, spec, flag string) (string, error) {
	p := flag
	if p == "" {
		p = name + "-" + spec
	}
	pp, err := mesh.ParsePath(p)
	if err != nil {
		return "", fail(exitUsage, "path %q: %v", p, err)
	}
	if _, ok := mesh.KindFromPath(pp); ok {
		return "", fail(exitRefused, "refused: path %s: the first token routine is for the routine pool", p)
	}
	return p, nil
}

// leadFree checks that path can take a lead now: a node there must be done
// or blocked (its previous state is returned), and [runner] max_leads is
// not reached on this host.
func (a *app) leadFree(ctx context.Context, path string) (prev string, err error) {
	_, exists, err := mesh.GetNode(ctx, a.client, path)
	if err != nil {
		return "", err
	}
	st, _, err := mesh.ReadState(ctx, a.client, path)
	if err != nil {
		return "", err
	}
	if exists && st.State != "done" && st.State != "blocked" {
		return "", fail(exitRefused, "refused: path %s is taken (state %s); use --path %s-2", path, st.State, path)
	}
	if max := a.cfg.Runner.MaxLeads; max > 0 {
		n, err := a.liveLeads(ctx)
		if err != nil {
			return "", err
		}
		if n >= max {
			return "", fail(exitRefused, "%d leads already working on this host", n)
		}
	}
	return st.State, nil
}

// startLead starts a lead through the configured runner and registers it
// as a node (role lead) at its path.
func (a *app) startLead(o leadStart) (leadStarted, error) {
	p, err := leadPath(o.Name, o.Spec, o.Path)
	if err != nil {
		return leadStarted{}, err
	}
	r, err := runner.New(a.cfg)
	if err != nil {
		return leadStarted{}, err
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "lead-start"); err != nil {
		return leadStarted{}, err
	}
	prev, err := a.leadFree(ctx, p)
	if err != nil {
		return leadStarted{}, err
	}
	if a.configPath != "" {
		// The runner's session must report to the mesh this CLI was told about.
		if abs, err := filepath.Abs(a.configPath); err == nil {
			os.Setenv("BMESH_CONFIG", abs)
		}
	}
	if o.Model == "" {
		o.Model = a.cfg.Models.Lead
	}
	if o.Effort == "" {
		o.Effort = a.cfg.Models.LeadEffort
	}
	if o.Title == "" {
		o.Title = fmt.Sprintf("[build] %s %s", o.Name, o.Spec)
	}
	thread := o.Thread
	if thread == "" {
		if thread, err = uuid4(); err != nil {
			return leadStarted{}, err
		}
	}
	// The runner bounds its own calls (T3: 120 s), so the start doesn't share
	// the CLI's one-minute deadline, and registration gets a fresh one.
	cancel()
	started, err := r.Start(context.Background(), runner.StartSpec{RepoPath: o.RepoPath, Model: o.Model, Effort: o.Effort, Title: o.Title,
		BriefPath: o.BriefPath, ThreadID: thread})
	if errors.Is(err, runner.ErrTimeout) {
		return leadStarted{}, fmt.Errorf("start timed out; thread id %s may exist", thread)
	}
	if err != nil {
		return leadStarted{}, fmt.Errorf("runner start (thread id %s): %w", thread, err)
	}
	ctx, cancel = cmdCtx()
	defer cancel()
	if err := a.register(ctx, started, mesh.Node{Path: p, Role: "lead", Repo: o.Name, RepoPath: o.RepoPath, Spec: o.Spec,
		Host: a.cfg.HostName(), Model: o.Model, Effort: o.Effort}, prev); err != nil {
		return leadStarted{}, fmt.Errorf("thread %s started (branch %s, worktree %s) but registering the node failed: %w",
			started.Thread, started.Branch, started.Worktree, err)
	}
	return leadStarted{Path: p, Thread: started.Thread, Branch: started.Branch, Worktree: started.Worktree}, nil
}

// liveLeads counts the leads registered on this host that are not done or
// blocked: the ones [runner] max_leads bounds.
func (a *app) liveLeads(ctx context.Context) (int, error) {
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return 0, err
	}
	states, err := mesh.ReadAllStates(ctx, a.client)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, node := range nodes {
		if node.Role != "lead" || node.Host != a.cfg.HostName() {
			continue
		}
		if s := states[node.Path].State; s == "done" || s == "blocked" {
			continue
		}
		n++
	}
	return n, nil
}

// register records the started session in n and puts it. A fresh path
// already reads idle; a reused done or blocked one (prev) is reset to idle.
// (idle -> idle is not a transition, so nothing is written for it.)
func (a *app) register(ctx context.Context, started runner.Started, n mesh.Node, prev string) error {
	n.Branch, n.Worktree = started.Branch, started.Worktree
	n.Runner = mesh.NodeRunner{Kind: a.cfg.Runner.Kind, Thread: started.Thread}
	n.StartedAt = time.Now().UTC().Format(time.RFC3339)
	if err := mesh.PutNode(ctx, a.client, n); err != nil {
		return err
	}
	if prev != "idle" {
		return mesh.ResetState(ctx, a.client, n.Path)
	}
	return nil
}

// uuid4 returns a random RFC 4122 version 4 UUID.
func uuid4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
