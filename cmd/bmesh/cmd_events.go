package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/session"
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
			if err := a.checkSpecDone(ctx, n, *commit); err != nil {
				return err
			}
		}
	}
	if !mesh.Legal(cur.State, to) {
		return illegalMove(cur.State, to, "bmesh "+name)
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
			return illegalMove(ill.From, ill.To, "bmesh "+name)
		}
		return err
	}
	if name == "started" {
		if err := a.recordSession(ctx, path); err != nil {
			return err
		}
	}
	if name == "done" {
		fmt.Fprintf(a.stdout, "done %s %s\n", path, short8(*commit))
	} else {
		fmt.Fprintf(a.stdout, "%s %s\n", name, path)
	}
	return nil
}

// recordSession keeps the Claude Code session started ran from on the
// node's record (101), so its transcript can be found on its box. A node
// with a parent records none, as it hears no context line: its session may
// be its lead's. A node never registered has no record to keep it on.
func (a *app) recordSession(ctx context.Context, path string) error {
	id, ok := session.ID(os.Getenv)
	if !ok || strings.Contains(path, ".") {
		return nil
	}
	n, ok, err := mesh.GetNode(ctx, a.client, path)
	if err != nil || !ok || n.Session == id {
		return err
	}
	n.Session = id
	return mesh.PutNode(ctx, a.client, n)
}

// illegalMove refuses a state move the table doesn't allow, naming the
// next step: none out of done; cancelling the ready request out of
// landing; else an event that makes the node working first, then again.
func illegalMove(from, to, again string) error {
	why := mesh.ErrIllegalTransition{From: from, To: to}
	switch {
	case from == "done":
		return fail(exitRefused, "refused: %v; nothing leaves done", why)
	case from == "landing":
		return fail(exitRefused, "refused: %v; your box's daemon is landing you: run bmesh ready --cancel first, then %s again", why, again)
	case from == "idle" || from == "":
		return fail(exitRefused, "refused: %v; run bmesh started --text \"<one line>\" first, then %s again", why, again)
	}
	return fail(exitRefused, "refused: %v; run bmesh progress --text \"<one line>\" first, then %s again", why, again)
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

// passLanded is the next step of a lead's done refused for its commit:
// the one land printed, reworded to carry the trailers.
const passLanded = "pass bmesh done --commit the commit bmesh land printed as landed (git rev-parse HEAD in your worktree after it)"

// checkSpecDone refuses a lead's done unless commit is its landing: on
// main (origin/main when the repo has an origin), with a Verified trailer
// of no failing entry, every entry matched by a recorded result for the
// commit's tree (any args), or a carried entry by a model result at the
// tree it names whose patch-id is the commit's diff from an ancestor on
// main, or an entry left behind by later commits (at:) by a result at its
// own tree, and a Spec trailer naming the node's spec. A commit whose subject
// is the old `NNN: done at <sha8>` record is checked as it was: against
// the spec on main (checkLegacyDone).
func (a *app) checkSpecDone(ctx context.Context, n mesh.Node, commit string) error {
	l, lerr := spec.ReadLanded(ctx, n.RepoPath, commit)
	if lerr == nil && l.Spec == "" && l.Legacy != "" {
		return a.checkLegacyDone(ctx, n)
	}
	on, ref, err := spec.OnMain(ctx, n.RepoPath, commit, true)
	if err != nil {
		return err
	}
	if lerr != nil || !on {
		return fail(exitRefused, "not done: %s is not on %s; %s, or land first (bmesh ready, or bmesh land)", short8(commit), ref, passLanded)
	}
	if l.Verified == "" {
		return fail(exitRefused, "not done: %s has no Verified trailer; %s, which carries it", short8(l.Commit), passLanded)
	}
	entries, err := spec.ParseVerified(l.Verified)
	if err != nil {
		return fail(exitRefused, "not done: %s's Verified trailer: %v; %s", short8(l.Commit), err, passLanded)
	}
	if err := a.checkEntries(ctx, n.RepoPath, l.Commit, entries); err != nil {
		return err
	}
	if want := spec.NormID(n.Spec); l.Spec != want {
		if l.Spec == "" {
			return fail(exitRefused, "not done: %s has no Spec trailer; %s", short8(l.Commit), passLanded)
		}
		return fail(exitRefused, "not done: %s landed spec %s, not %s; %s for spec %s", short8(l.Commit), l.Spec, want, passLanded, want)
	}
	return nil
}

// checkLegacyDone is done's check of a lead that landed with the release
// before, whose land wrote a `NNN: done at` record commit: the node's
// spec on main reads done with merged, and its verified line passes
// checkEntries for merged.
func (a *app) checkLegacyDone(ctx context.Context, n mesh.Node) error {
	d, err := spec.CheckDone(n.RepoPath, a.cfg.Spec.Glob, n.Spec)
	if err != nil {
		return err
	}
	if !d.OK {
		return fail(exitRefused, "not done: %s", d.Why)
	}
	return a.checkEntries(ctx, n.RepoPath, d.Merged, d.Verified)
}

// checkEntries refuses a verified line that is empty, has a failing entry,
// or names an entry no recorded result matches for merged's tree. An entry
// left behind by later commits (at:) records what ran at its own tree: it
// is matched there, verdict and all, and neither passes nor fails merged.
func (a *app) checkEntries(ctx context.Context, repoPath, merged string, entries []spec.Entry) error {
	if !slices.ContainsFunc(entries, func(e spec.Entry) bool { return !e.Behind() }) {
		return fail(exitRefused, "not done: verified is empty")
	}
	for _, e := range entries {
		if !e.OK && !e.Behind() {
			return fail(exitRefused, "not done: verified has a failing entry %s; %s, whose entries all pass", e.Kind, passLanded)
		}
	}
	tree, err := spec.Tree(repoPath, merged+"^{commit}")
	if err != nil {
		return fail(exitRefused, "not done: merged %s is not a commit", merged)
	}
	for _, e := range entries {
		if e.Tree8 != tree[:8] && !e.Behind() {
			return fail(exitRefused, "not done: verified entry %s is for tree %s, merged tree is %s; %s", e.Kind, e.Tree8, tree[:8], passLanded)
		}
		rs, err := mesh.ListResults(ctx, a.client, e.Kind)
		if err != nil {
			return err
		}
		// A carried entry is matched at the tree its result was recorded
		// at, by a model result whose recorded diff is the one merged landed.
		at := tree[:8]
		switch {
		case e.Carried != "":
			at = e.Carried
		case e.Behind():
			at = e.Tree8
		}
		matched := false
		for _, r := range rs {
			if !strings.HasPrefix(r.Tree, at) || r.OK != e.OK || e.Model != "" && r.Model != e.Model {
				continue
			}
			if e.Behind() {
				matched = true
				break
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
			if matched, err = checkout.LandedFrom(ctx, repoPath, merged, r.PatchID); err != nil {
				return err
			} else if matched {
				break
			}
		}
		if !matched {
			missing := fmt.Sprintf("not done: verified names %s but no result for tree %s", e.Kind, at)
			queue := fmt.Sprintf("bmesh routine queue %s --commit %s --go", e.Kind, short8(merged))
			switch {
			case e.Behind():
				// Its result was recorded before the commits that followed it.
				return fail(exitRefused, "%s; it was recorded at %s, before later commits, so no run can record it again: report it with bmesh blocked --text \"<this line>\"", missing, e.At)
			case e.Carried != "":
				// Its result was recorded for an older tree; no run now records it there.
				return fail(exitRefused, "%s; it was carried, so no run can record it again: report it with bmesh blocked --text \"<this line>\"", missing)
			case e.Model != "":
				return fail(exitRefused, "%s; record it there (%s --model %s), then run bmesh done again", missing, queue, e.Model)
			}
			return fail(exitRefused, "%s; record it there (bmesh routine run %s in a checkout of %s, or %s), then run bmesh done again",
				missing, e.Kind, short8(merged), queue)
		}
	}
	return nil
}
