package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/brief"
	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/prune"
)

// wipeParts is what a wipe removed of one node off the mesh and its box.
type wipeParts struct {
	mesh.Wiped
	Feedback                int
	Brief, Worktree, Branch bool
}

func (w wipeParts) String() string {
	var out []string
	plural := func(n int, one string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %ss", n, one)
	}
	if w.Events > 0 {
		out = append(out, plural(w.Events, "event"))
	}
	if w.Keys > 0 {
		out = append(out, plural(w.Keys, "key"))
	}
	if w.Inbox {
		out = append(out, "inbox")
	}
	if w.Feedback > 0 {
		out = append(out, fmt.Sprintf("%d feedback", w.Feedback))
	}
	for _, p := range []struct {
		ok   bool
		name string
	}{{w.Brief, "brief"}, {w.Worktree, "worktree"}, {w.Branch, "branch"}} {
		if p.ok {
			out = append(out, p.name)
		}
	}
	if len(out) == 0 {
		return "nothing left"
	}
	return strings.Join(out, ", ")
}

// readDisk sets what the lines of a worktree removal (here, or another
// box's answer to forget-worktree) say went.
func (w *wipeParts) readDisk(lines []string) {
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "removed worktree "):
			w.Worktree = true
		case strings.HasPrefix(l, "deleted branch "):
			w.Branch = true
		case l == "removed brief":
			w.Brief = true
		}
	}
}

// wipeNodes wipes n, already checked as forget takes it, and every node
// under it: first each one's worktree, branch and brief on its own box
// (deepest first here; another box's through forget-worktree jobs sent
// together, the mesh untouched until every box answered), then the mesh
// (mesh.WipeTree), then the feedback of each family no node outside the
// wipe is in. It prints a line per node, n first, and publishes
// `<path> by <who>` on bm.daemon.<this box>.wiped. With dry it changes
// nothing and prints what it would remove: a worktree or branch by the
// node's record, a brief only on this box.
func (a *app) wipeNodes(ctx context.Context, n mesh.Node, nodes []mesh.Node, dry bool, wait time.Duration) error {
	me := a.cfg.HostName()
	local := func(c mesh.Node) bool { return c.Host == "" || c.Host == me }
	set := []mesh.Node{n}
	in := map[string]bool{n.Path: true}
	for _, c := range nodes {
		if strings.HasPrefix(c.Path, n.Path+".") {
			set = append(set, c)
			in[c.Path] = true
		}
	}
	sort.Slice(set, func(i, j int) bool { // deepest first: an executor's branch is judged against its lead's
		di, dj := strings.Count(set[i].Path, "."), strings.Count(set[j].Path, ".")
		if di != dj {
			return di > dj
		}
		return set[i].Path < set[j].Path
	})
	var remote []mesh.Node
	for _, c := range set {
		// A round has no worktree or brief of its own.
		if !local(c) && c.Role != "round" {
			remote = append(remote, c)
		}
	}
	if len(remote) > 0 && !a.cfg.Hosts.Enabled {
		return fail(exitRefused, "refused: %s ran on %s; run it there", remote[0].Path, remote[0].Host)
	}
	who, err := a.identityOrHost(ctx, "node forget")
	if err != nil {
		return err
	}

	parts := map[string]*wipeParts{}
	for _, c := range set {
		parts[c.Path] = &wipeParts{}
	}
	for _, c := range set {
		w := parts[c.Path]
		switch {
		case dry:
			w.Worktree, w.Branch = c.Worktree != "", c.Worktree != "" && c.Branch != ""
			if local(c) {
				w.Worktree, w.Branch = prune.NodeWorktreeLeft(ctx, c)
				if p, err := brief.CachePath(c.Path); err == nil {
					_, err := os.Stat(p)
					w.Brief = err == nil
				}
			}
		case local(c):
			lines, err := prune.RemoveNodeWorktree(ctx, c, nodes, me)
			w.readDisk(lines)
			var ref *prune.Refusal
			if errors.As(err, &ref) {
				return fail(exitRefused, "%s", ref.Text)
			}
			if err != nil {
				return err
			}
			removed, err := brief.RemoveCached(c.Path)
			if err != nil {
				return err
			}
			w.Brief = removed
		}
	}
	if !dry && len(remote) > 0 {
		answered, err := a.wipeRemote(ctx, remote, parts, wait)
		if err != nil || !answered {
			return err
		}
	}

	paths := make([]string, 0, len(set))
	for _, c := range set {
		paths = append(paths, c.Path)
	}
	wiped, err := mesh.WipeTree(ctx, a.client, n.Path, paths, !dry)
	if err != nil {
		return err
	}
	for p, w := range wiped {
		parts[p].Wiped = *w
	}
	shared := map[string]bool{}
	for _, c := range nodes {
		if !in[c.Path] && c.Family != "" {
			shared[c.Family] = true
		}
	}
	seen := map[string]bool{}
	for _, c := range set {
		if c.Family == "" || shared[c.Family] || seen[c.Family] {
			continue
		}
		seen[c.Family] = true
		if parts[c.Path].Feedback, err = mesh.WipeFeedback(ctx, a.client, c.Family, !dry); err != nil {
			return err
		}
	}

	verb := "wiped"
	if dry {
		verb = "would wipe"
	}
	sort.Strings(paths) // n first: every other path starts with it
	for _, p := range paths {
		fmt.Fprintf(a.stdout, "%s %s: %s\n", verb, p, parts[p])
	}
	if dry {
		return nil
	}
	return mesh.PublishEvent(ctx, a.client, mesh.Event{From: "daemon." + me, Event: "wiped", Text: n.Path + " by " + who})
}

// wipeRemote sends forget-worktree <path> wipe for each of nodes to its
// box, all at once, and waits up to wait: true once every box answered
// that it removed them. A refusal exits 3 after the rest; a box that
// doesn't answer in time leaves the mesh untouched, the job queued, and
// says to run the wipe again once it answers.
func (a *app) wipeRemote(ctx context.Context, nodes []mesh.Node, parts map[string]*wipeParts, wait time.Duration) (bool, error) {
	b, err := a.hostsBox()
	if err != nil {
		return false, err
	}
	if err := b.sync(ctx, a); err != nil {
		fmt.Fprintf(a.stderr, "bmesh: roster: %v (using this box's copy)\n", err)
	}
	r := b.local.Roster
	var jobs []hosts.Job
	for _, n := range nodes {
		if _, ok := r.Host(n.Host); !ok {
			return false, fail(exitRefused, "refused: %s ran on %s, which isn't in the roster; run it there", n.Path, n.Host)
		}
		jobs = append(jobs, hosts.NewJob(b.key, b.host, n.Host, "forget-worktree", "", []string{n.Path, "wipe"}, r.Version, time.Now(), forgetTTL))
	}
	replies, err := a.askBoxes(ctx, b, jobs, wait, nil)
	if err != nil {
		return false, err
	}
	var refused, waiting []string
	for i, j := range jobs {
		rep, ok := replies[j.ID]
		switch {
		case !ok:
			waiting = append(waiting, nodes[i].Host)
		case !rep.OK:
			refused = append(refused, fmt.Sprintf("%s: %s", nodes[i].Host, rep.Text))
		default:
			parts[nodes[i].Path].readDisk(strings.Split(rep.Text, "; "))
		}
	}
	if len(refused) > 0 {
		return false, fail(exitRefused, "%s", strings.Join(refused, "\n"))
	}
	if len(waiting) > 0 {
		sort.Strings(waiting)
		fmt.Fprintf(a.stdout, "queued; nothing is wiped from the mesh until %s answers: run it again then\n", strings.Join(slices.Compact(waiting), ", "))
		return false, nil
	}
	return true, nil
}
