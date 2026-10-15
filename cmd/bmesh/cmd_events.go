package main

import (
	"errors"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

// cmdEvent reports started|progress|blocked|ask|done: the move is checked
// against the node's state first, so a refused event publishes nothing.
func (a *app) cmdEvent(name string, args []string) error {
	fs := a.flags(name)
	text := fs.String("text", "", "what to say")
	commit := fs.String("commit", "", "commit the event refers to (required for done)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, "%s takes no arguments (use --text)", name)
	}
	if name == "done" && *commit == "" {
		return fail(exitUsage, "done needs --commit SHA")
	}
	to, _ := mesh.StateFor(name)
	ctx, cancel := cmdCtx()
	defer cancel()
	p, err := a.identity(ctx, name)
	if err != nil {
		return err
	}
	path := p.String()
	if name == "done" {
		n, ok, err := mesh.GetNode(ctx, a.client, path)
		if err != nil {
			return err
		}
		if !ok {
			return fail(exitError, "node %s is not registered", path)
		}
		d, err := spec.CheckDone(n.RepoPath, a.cfg.Spec.Glob, n.Spec)
		if err != nil {
			return err
		}
		if !d.OK {
			return fail(exitRefused, "not done: %s", d.Why)
		}
	}
	cur, _, err := mesh.ReadState(ctx, a.client, path)
	if err != nil {
		return err
	}
	if !mesh.Legal(cur.State, to) {
		return fail(exitRefused, "refused: %v", mesh.ErrIllegalTransition{From: cur.State, To: to})
	}
	if err := mesh.PublishEvent(ctx, a.client, mesh.Event{From: path, Event: name, Text: *text, Commit: *commit}); err != nil {
		return err
	}
	if _, err := mesh.WriteState(ctx, a.client, path, mesh.State{State: to, Text: *text, By: "node"}); err != nil {
		var ill mesh.ErrIllegalTransition
		if errors.As(err, &ill) {
			return fail(exitRefused, "refused: %v", err)
		}
		return err
	}
	return nil
}
