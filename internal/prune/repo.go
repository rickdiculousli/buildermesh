package prune

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/level"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// Actions a row can carry: a dry run says keep or remove; an applied run
// keep, removed or failed.
const (
	Keep    = "keep"
	Remove  = "remove"
	Removed = "removed"
	Failed  = "failed"
)

// Row is one worktree or bmesh/* branch of a checkout, judged.
type Row struct {
	Repo   string
	What   string // a worktree path, or "branch <name>"
	Action string // Keep, Remove, Removed or Failed
	Reason string
	Err    string // why a removal failed
	branch bool
}

// String is the row as one line: `<repo> <what> <action> <reason>`, and
// `  (failed: <err>)` after a failed removal.
func (r Row) String() string {
	s := fmt.Sprintf("%s %s %s %s", r.Repo, r.What, r.Action, r.Reason)
	if r.Err != "" {
		s += "  (failed: " + r.Err + ")"
	}
	return s
}

// Summary counts rows: `N worktrees, B branches, M to remove, K kept`,
// with `, R removed` (and `, F failed` when any did) after an applied run.
func Summary(rows []Row, applied bool) string {
	var wts, branches, remove, kept, removed, failed int
	for _, r := range rows {
		if r.branch {
			branches++
		} else {
			wts++
		}
		switch r.Action {
		case Keep:
			kept++
		case Removed:
			removed++
			remove++
		case Failed:
			failed++
			remove++
		default:
			remove++
		}
	}
	s := fmt.Sprintf("%d worktrees, %d branches, %d to remove, %d kept", wts, branches, remove, kept)
	if applied {
		s += fmt.Sprintf(", %d removed", removed)
		if failed > 0 {
			s += fmt.Sprintf(", %d failed", failed)
		}
	}
	return s
}

// Checkout is one repo's checkout on this box and what prune judges it by.
type Checkout struct {
	Repo      string                // the registry name
	Path      string                // the main checkout
	Self      string                // this box's host name: only its nodes count
	Nodes     []mesh.Node           // every node
	States    map[string]mesh.State // by node path; none is idle
	Worktrees string                // [runner] worktrees, expanded: bmesh's worktrees live under it
	Checkouts string                // [runner] checkouts, expanded: routine checkouts, never listed
	StateDir  string                // where the per-repo lock lives
	Apply     bool                  // remove the remove rows; else a dry run
}

type worktree struct {
	path, head, branch string
	bare, main         bool
}

// worktrees parses `git worktree list --porcelain`; the first is the main
// checkout.
func worktrees(porcelain string) []worktree {
	var out []worktree
	for i, block := range strings.Split(strings.TrimSpace(porcelain), "\n\n") {
		var w worktree
		for _, line := range strings.Split(block, "\n") {
			key, val, _ := strings.Cut(line, " ")
			switch key {
			case "worktree":
				w.path = val
			case "HEAD":
				w.head = val
			case "branch":
				w.branch = strings.TrimPrefix(val, "refs/heads/")
			case "bare":
				w.bare = true
			}
		}
		if w.path == "" {
			continue
		}
		w.main = i == 0
		out = append(out, w)
	}
	return out
}

// resolved is p with symlinks resolved, else cleaned.
func resolved(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// under: p is inside root (both resolved).
func under(p, root string) bool {
	if root == "" {
		return false
	}
	return strings.HasPrefix(resolved(p), resolved(root)+string(filepath.Separator))
}

// plan is a worktree's judgement before any removal.
type plan struct {
	w      worktree
	row    Row
	force  bool // the branch goes with -D: its merge was proven another way
	delete bool // a branch to delete with the worktree
}

// Repo judges c's linked worktrees and the bmesh/* branches no worktree
// holds, and with c.Apply removes the remove rows under the repo's lock.
// A bmesh worktree (under c.Worktrees) goes when its node on this box is
// done, it is clean and its HEAD has landed in main, or in its lead's
// branch; any other worktree (T3's, anyone's) is kept. A bmesh/* branch
// goes when `git branch -d` takes it.
func Repo(ctx context.Context, c Checkout) ([]Row, error) {
	if c.Apply {
		unlock, err := level.Lock(c.StateDir, c.Repo)
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	porcelain, err := git(ctx, c.Path, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	all := worktrees(porcelain)
	held := map[string]bool{} // a removed worktree's branch goes with its row
	var plans []plan
	for _, w := range all {
		if w.branch != "" {
			held[w.branch] = true
		}
		if w.main || w.bare || under(w.path, c.Checkouts) {
			continue
		}
		plans = append(plans, c.judge(ctx, w))
	}
	var rows []Row
	for _, p := range plans {
		if c.Apply && p.row.Action == Remove {
			p.row.Action = Removed
			if err := c.remove(ctx, p); err != nil {
				p.row.Action, p.row.Err = Failed, err.Error()
			}
		}
		rows = append(rows, p.row)
	}
	sweep, err := c.sweep(ctx, held)
	return append(rows, sweep...), err
}

// judge decides one linked worktree.
func (c Checkout) judge(ctx context.Context, w worktree) plan {
	p := plan{w: w, row: Row{Repo: c.Repo, What: w.path, Action: Keep}}
	if !under(w.path, c.Worktrees) {
		p.row.Reason = "not bmesh's (T3 cleans its own)"
		return p
	}
	var node, parent *mesh.Node
	for i, n := range c.Nodes {
		if n.Host == c.Self && n.Worktree != "" && resolved(n.Worktree) == resolved(w.path) {
			node = &c.Nodes[i]
		}
	}
	if node == nil {
		p.row.Reason = "no bmesh node"
		return p
	}
	for i, n := range c.Nodes {
		if node.Parent != "" && n.Path == node.Parent {
			parent = &c.Nodes[i]
		}
	}
	state := "idle"
	if s, ok := c.States[node.Path]; ok && s.State != "" {
		state = s.State
	}
	if state != "done" {
		p.row.Reason = "node " + state
		return p
	}
	if dirty, err := git(ctx, w.path, "status", "--porcelain"); err != nil || dirty != "" {
		p.row.Reason = "uncommitted changes"
		return p
	}
	p.delete = w.branch != ""
	switch {
	case w.head != "" && ancestor(ctx, c.Path, w.head, "refs/heads/main"):
		p.row.Action, p.row.Reason = Remove, "node done, merged"
	case w.head != "" && cherried(ctx, c.Path, w.head, "refs/heads/main"):
		// Replayed into main under new SHAs (a lead's rebase): -d won't see it.
		p.row.Action, p.row.Reason, p.force = Remove, "node done, merged", true
	case parent != nil && parent.Branch != "" && w.head != "" && hasBranch(ctx, c.Path, parent.Branch) &&
		(ancestor(ctx, c.Path, w.head, "refs/heads/"+parent.Branch) || cherried(ctx, c.Path, w.head, "refs/heads/"+parent.Branch)):
		p.row.Action, p.row.Reason, p.force = Remove, "node done, merged into "+parent.Branch, true
	default:
		p.row.Reason = "unmerged"
	}
	return p
}

// cherried: each of rev's commits not in target has an equivalent there.
func cherried(ctx context.Context, repo, rev, target string) bool {
	out, err := git(ctx, repo, "cherry", target, rev)
	return err == nil && !strings.Contains("\n"+out, "\n+")
}

// remove takes p's worktree away (never forced) and then its branch: -d,
// or -D when its merge was proven by its lead's branch or git cherry.
func (c Checkout) remove(ctx context.Context, p plan) error {
	if _, err := git(ctx, c.Path, "worktree", "remove", p.w.path); err != nil {
		return err
	}
	if !p.delete {
		return nil
	}
	flag := "-d"
	if p.force {
		flag = "-D"
	}
	_, err := git(ctx, c.Path, "branch", flag, p.w.branch)
	return err
}

// sweep judges every local bmesh/* branch no worktree holds: merged as
// `git branch -d` judges it (into its upstream, else the main checkout's
// HEAD) is removed with -d, never -D; anything else is kept.
func (c Checkout) sweep(ctx context.Context, held map[string]bool) ([]Row, error) {
	out, err := git(ctx, c.Path, "for-each-ref", "--format=%(refname:short)\t%(upstream)", "refs/heads/bmesh/")
	if err != nil {
		return nil, err
	}
	var rows []Row
	for _, line := range strings.Split(out, "\n") {
		name, upstream, _ := strings.Cut(line, "\t")
		if name == "" || held[name] {
			continue
		}
		r := Row{Repo: c.Repo, What: "branch " + name, Action: Keep, Reason: "unmerged", branch: true}
		into := "HEAD"
		if upstream != "" {
			if _, err := git(ctx, c.Path, "rev-parse", "--verify", "--quiet", upstream); err == nil {
				into = upstream
			}
		}
		if ancestor(ctx, c.Path, "refs/heads/"+name, into) {
			r.Action, r.Reason = Remove, "merged"
			if c.Apply {
				if _, err := git(ctx, c.Path, "branch", "-d", name); err != nil {
					r.Action, r.Reason = Keep, "not merged (git branch -d refused)"
				} else {
					r.Action = Removed
				}
			}
		}
		rows = append(rows, r)
	}
	return rows, nil
}
