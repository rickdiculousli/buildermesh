package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

// cmdEvent reports started|progress|blocked|ask|done: the move is checked
// against the node's state first, so a refused event publishes nothing. A
// lead's done is checked against its spec; an executor's or an engineer's
// (a round's thread) needs only --commit. An executor's ask, done and blocked
// are also sent to its parent's inbox, since the operator reads only leads'
// events and the lead acts on its executors' from there; a fix lead's (a
// lead in a fixer's family) go to the fixer's, whose path is its family,
// since the fixer, not the operator, hears them. Each prints one
// line on success: `<event> <path>`, or `done <path> <sha8>`; a done for the
// commit a node is already done at prints `already done at <sha8>` and
// publishes nothing.
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
	cur, _, err := mesh.ReadState(ctx, a.client, path)
	if err != nil {
		return err
	}
	if name == "done" && cur.State == "done" {
		if at, ok, err := lastDone(ctx, a.client, p); err != nil {
			return err
		} else if ok && sameCommit(at, *commit) {
			fmt.Fprintf(a.stdout, "already done at %s\n", short8(at))
			return nil
		} else if ok {
			return fail(exitRefused, "refused: %s is already done at %s, not %s", path, short8(at), short8(*commit))
		}
	}
	var n mesh.Node
	if name == "done" || name == "ask" || name == "blocked" {
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
	if !mesh.Legal(cur.State, to) {
		return fail(exitRefused, "refused: %v", mesh.ErrIllegalTransition{From: cur.State, To: to})
	}
	if n.Role == "executor" && (name == "ask" || name == "done" || name == "blocked") {
		parent := n.Parent
		if parent == "" {
			if pp, ok := p.Parent(); ok {
				parent = pp.String()
			}
		}
		if parent == "" {
			return fail(exitError, "executor %s has no parent to %s", path, name)
		}
		if _, err := mesh.Send(ctx, a.client, executorMessage(ctx, name, n, parent, *text, *commit)); err != nil {
			return err
		}
	}
	if _, _, fix := mesh.FixFamily(n.Family); n.Role == "lead" && fix && (name == "ask" || name == "done" || name == "blocked") {
		if _, err := mesh.Send(ctx, a.client, executorMessage(ctx, name, n, n.Family, *text, *commit)); err != nil {
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
	if name == "done" {
		fmt.Fprintf(a.stdout, "done %s %s\n", path, short8(*commit))
	} else {
		fmt.Fprintf(a.stdout, "%s %s\n", name, path)
	}
	return nil
}

// lastDone is the commit of the node's newest done event.
func lastDone(ctx context.Context, c *mesh.Client, p mesh.Path) (string, bool, error) {
	s, ok, err := mesh.Last(ctx, c, mesh.EventSubject(p, "done"))
	if err != nil || !ok {
		return "", false, err
	}
	var ev mesh.Event
	if err := json.Unmarshal(s.Data, &ev); err != nil || ev.Commit == "" {
		return "", false, nil
	}
	return ev.Commit, true, nil
}

// sameCommit reports whether two spellings of a commit name the same one:
// equal, or one an abbreviation (7 or more characters) of the other.
func sameCommit(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if len(a) > len(b) {
		a, b = b, a
	}
	return a == b && a != "" || len(a) >= 7 && strings.HasPrefix(b, a)
}

// executorMessage is what an executor's ask, done or blocked puts in its
// parent's inbox (and a fix lead's in its fixer's). The id is the event itself, so a retry after a failure
// further down (publish, state) is one message in the lead's inbox.
func executorMessage(ctx context.Context, name string, n mesh.Node, parent, text, commit string) mesh.Message {
	path := n.Path
	m := mesh.Message{From: path, To: parent}
	switch name {
	case "ask":
		m.ID, m.Kind, m.Text = "ask-"+path+"-"+textHash(path, text), "message", "ask from "+path+": "+text
	case "blocked":
		m.ID, m.Kind, m.Text = "blocked-"+path+"-"+textHash(path, text), "report", "blocked from "+path
		if text != "" {
			m.Text += ": " + text
		}
		m.Report = &mesh.Report{Event: "blocked", From: path}
	case "done":
		branch := n.Branch
		if n.Worktree != "" {
			// The worktree's current branch, as a console may have renamed it.
			if b, err := gitIn(ctx, n.Worktree, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && b != "HEAD" {
				branch = b
			}
			if full, err := gitIn(ctx, n.Worktree, "rev-parse", "--verify", "--quiet", "--end-of-options", commit+"^{commit}"); err == nil {
				commit = full
			}
		}
		m.ID, m.Kind = "done-"+path+"-"+commit, "report"
		m.Text = "done from " + path + ": branch " + branch + " at " + short8(commit)
		if text != "" {
			m.Text += ": " + text
		}
		m.Report = &mesh.Report{Event: "done", From: path, Branch: branch, Commit: commit}
	}
	return m
}

func textHash(path, text string) string {
	sum := sha256.Sum256([]byte(path + "\x00" + text))
	return hex.EncodeToString(sum[:8])
}

func short8(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// checkSpecDone refuses done unless the node's spec has landed as done with
// merged on main; a lead's spec must also carry a verified line with no
// failing entry, every entry matched by a recorded result for the merged
// commit's tree (any args), or a carried entry by a model result at the
// tree it names whose patch-id is merged's diff from an ancestor on main.
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
		// A carried entry is matched at the tree its result was recorded
		// at, by a model result whose recorded diff is the one merged landed.
		at := tree[:8]
		if e.Carried != "" {
			at = e.Carried
		}
		matched := false
		for _, r := range rs {
			if !strings.HasPrefix(r.Tree, at) || r.OK != e.OK || e.Model != "" && r.Model != e.Model {
				continue
			}
			if e.Carried == "" {
				if matched = r.Tree == tree; matched {
					break
				}
				continue
			}
			if r.Model == "" || r.PatchID == "" {
				continue
			}
			if matched, err = checkout.LandedFrom(ctx, n.RepoPath, d.Merged, r.PatchID); err != nil {
				return err
			} else if matched {
				break
			}
		}
		if !matched {
			return fail(exitRefused, "not done: verified names %s but no result for tree %s", e.Kind, at)
		}
	}
	return nil
}
