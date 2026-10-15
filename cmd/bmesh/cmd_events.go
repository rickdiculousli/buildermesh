package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

// cmdEvent reports started|progress|blocked|ask|done: the move is checked
// against the node's state first, so a refused event publishes nothing. A
// lead's done is checked against its spec; an executor's or an engineer's
// (a round's thread) needs only --commit. An executor's ask is also sent to its parent's inbox, since the
// operator reads only leads' asks.
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
	var n mesh.Node
	if name == "done" || name == "ask" {
		var ok bool
		n, ok, err = mesh.GetNode(ctx, a.client, path)
		if err != nil {
			return err
		}
		if !ok && name == "done" {
			return fail(exitError, "node %s is not registered", path)
		}
		if name == "done" && (n.Role == "lead" || n.Role == "") {
			if err := a.checkSpecDone(ctx, n); err != nil {
				return err
			}
		}
	}
	cur, _, err := mesh.ReadState(ctx, a.client, path)
	if err != nil {
		return err
	}
	if !mesh.Legal(cur.State, to) {
		return fail(exitRefused, "refused: %v", mesh.ErrIllegalTransition{From: cur.State, To: to})
	}
	if name == "ask" && n.Role == "executor" {
		parent := n.Parent
		if parent == "" {
			if pp, ok := p.Parent(); ok {
				parent = pp.String()
			}
		}
		if parent == "" {
			return fail(exitError, "executor %s has no parent to ask", path)
		}
		// The id is the ask itself, so a retry after a failure further
		// down (publish, state) is one message in the lead's inbox.
		sum := sha256.Sum256([]byte(path + "\x00" + *text))
		m := mesh.Message{ID: "ask-" + path + "-" + hex.EncodeToString(sum[:8]), From: path, To: parent, Kind: "message",
			Text: "ask from " + path + ": " + *text}
		if _, err := mesh.Send(ctx, a.client, m); err != nil {
			return err
		}
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

// checkSpecDone refuses done unless the node's spec has landed as done with
// merged on main; a lead's spec must also carry a verified line with no
// failing entry, every entry matched by a recorded result for the merged
// commit's tree (any args).
func (a *app) checkSpecDone(ctx context.Context, n mesh.Node) error {
	d, err := spec.CheckDone(n.RepoPath, a.cfg.Spec.Glob, n.Spec)
	if err != nil {
		return err
	}
	if !d.OK {
		return fail(exitRefused, "not done: %s", d.Why)
	}
	if n.Role != "lead" && n.Role != "" {
		return nil
	}
	if len(d.Verified) == 0 {
		return fail(exitRefused, "not done: verified is empty")
	}
	for _, e := range d.Verified {
		if !e.OK {
			return fail(exitRefused, "not done: verified has a failing entry %s", e.Kind)
		}
	}
	tree, err := spec.Tree(n.RepoPath, d.Merged+"^{commit}")
	if err != nil {
		return fail(exitRefused, "not done: merged %s is not a commit", d.Merged)
	}
	for _, e := range d.Verified {
		if e.Tree8 != tree[:8] {
			return fail(exitRefused, "not done: verified entry %s is for tree %s, merged tree is %s", e.Kind, e.Tree8, tree[:8])
		}
		rs, err := mesh.ListResults(ctx, a.client, e.Kind)
		if err != nil {
			return err
		}
		matched := false
		for _, r := range rs {
			if r.Tree == tree && r.OK == e.OK && (e.Model == "" || r.Model == e.Model) {
				matched = true
				break
			}
		}
		if !matched {
			return fail(exitRefused, "not done: verified names %s but no result for tree %s", e.Kind, e.Tree8)
		}
	}
	return nil
}
