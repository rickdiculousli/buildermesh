package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/runner"
)

func (a *app) cmdLead(args []string) error {
	if len(args) == 0 || args[0] != "start" {
		return fail(exitUsage, "usage: bmesh lead start --repo R --spec NNN --brief FILE [--model M] [--title T] [--path P]")
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
	title := fs.String("title", "", "thread title (default \"[build] <repo> <spec>\")")
	pathFlag := fs.String("path", "", "node path (default <repo>-<spec>)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 || *repo == "" || *spec == "" || *brief == "" {
		return fail(exitUsage, "usage: bmesh lead start --repo R --spec NNN --brief FILE [--model M] [--title T] [--path P]")
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	name, repoPath, err := a.cfg.RepoPath(*repo)
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	p := *pathFlag
	if p == "" {
		p = name + "-" + *spec
	}
	pp, err := mesh.ParsePath(p)
	if err != nil {
		return fail(exitUsage, "path %q: %v", p, err)
	}
	if _, ok := mesh.KindFromPath(pp); ok {
		return fail(exitRefused, "refused: path %s: the first token routine is for the routine pool", p)
	}
	briefPath, err := filepath.Abs(*brief)
	if err != nil {
		return err
	}
	r, err := runner.New(a.cfg)
	if err != nil {
		return err
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "lead-start"); err != nil {
		return err
	}
	_, exists, err := mesh.GetNode(ctx, a.client, p)
	if err != nil {
		return err
	}
	st, _, err := mesh.ReadState(ctx, a.client, p)
	if err != nil {
		return err
	}
	if exists {
		if st.State != "done" && st.State != "blocked" {
			return fail(exitRefused, "refused: path %s is taken (state %s); use --path %s-2", p, st.State, p)
		}
	}
	if *model == "" {
		*model = a.cfg.Models.Lead
	}
	if *title == "" {
		*title = fmt.Sprintf("[build] %s %s", name, *spec)
	}
	thread, err := uuid4()
	if err != nil {
		return err
	}
	// The runner bounds its own calls (T3: 120 s), so the start doesn't share
	// the CLI's one-minute deadline, and registration gets a fresh one.
	cancel()
	started, err := r.Start(context.Background(), runner.StartSpec{RepoPath: repoPath, Model: *model, Title: *title,
		BriefPath: briefPath, ThreadID: thread})
	if errors.Is(err, runner.ErrTimeout) {
		return fmt.Errorf("start timed out; thread id %s may exist", thread)
	}
	if err != nil {
		return fmt.Errorf("runner start (thread id %s): %w", thread, err)
	}
	ctx, cancel = cmdCtx()
	defer cancel()
	if err := a.register(ctx, started, mesh.Node{Path: p, Role: "lead", Repo: name, RepoPath: repoPath, Spec: *spec,
		Host: a.cfg.HostName(), Model: *model}, st.State); err != nil {
		return fmt.Errorf("thread %s started (branch %s, worktree %s) but registering the node failed: %w",
			started.Thread, started.Branch, started.Worktree, err)
	}
	line, err := json.Marshal(map[string]string{"path": p, "thread": started.Thread, "branch": started.Branch,
		"worktree": started.Worktree})
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%s\n", line)
	return nil
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
