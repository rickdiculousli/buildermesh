package main

import (
	"crypto/rand"
	"encoding/json"
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
	if _, err := mesh.ParsePath(p); err != nil {
		return fail(exitUsage, "path %q: %v", p, err)
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
	started, err := r.Start(ctx, runner.StartSpec{RepoPath: repoPath, Model: *model, Title: *title,
		BriefPath: briefPath, ThreadID: thread})
	if err != nil {
		return fmt.Errorf("runner start: %w", err)
	}
	n := mesh.Node{Path: p, Role: "lead", Repo: name, RepoPath: repoPath, Spec: *spec, Branch: started.Branch,
		Worktree: started.Worktree, Host: a.cfg.HostName(),
		Runner: mesh.NodeRunner{Kind: a.cfg.Runner.Kind, Thread: started.Thread},
		Model:  *model, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := mesh.PutNode(ctx, a.client, n); err != nil {
		return err
	}
	// A fresh path already reads idle; a reused done or blocked one is reset
	// to idle. (idle -> idle is not a transition, so nothing is written.)
	if st.State != "idle" {
		if err := mesh.ResetState(ctx, a.client, p); err != nil {
			return err
		}
	}
	line, err := json.Marshal(map[string]string{"path": p, "thread": started.Thread, "branch": started.Branch,
		"worktree": started.Worktree})
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%s\n", line)
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
