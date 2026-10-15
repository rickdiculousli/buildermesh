package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/brief"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

const specUsage = "usage: bmesh spec list [--repo R] [--status S] [--json] | next-id [--repo R] | " +
	"set --id NNN --status S [--repo R] [--from A,B] [--field K=V]... | template [--repo R] | " +
	"review <repo> \"<reply>\" | verified [--spec NNN] [--write]"

func (a *app) cmdSpec(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "verified":
			return a.cmdSpecVerified(args[1:])
		case "list":
			return a.cmdSpecList(args[1:])
		case "next-id":
			return a.cmdSpecNextID(args[1:])
		case "set":
			return a.cmdSpecSet(args[1:])
		case "template":
			return a.cmdSpecTemplate(args[1:])
		case "review":
			return a.cmdSpecReview(args[1:])
		}
	}
	return fail(exitUsage, specUsage)
}

// fields collects repeated --field KEY=VALUE flags.
type fields []spec.Field

func (f *fields) String() string { return fmt.Sprint(*f) }

func (f *fields) Set(v string) error {
	x, err := spec.ParseField(v)
	if err != nil {
		return err
	}
	*f = append(*f, x)
	return nil
}

// ledgerRepo resolves --repo (a registry name or a path; "" = the current
// directory) to the checkout the ledger is read in.
func (a *app) ledgerRepo(repo string) (string, error) {
	if err := a.loadConfig(); err != nil {
		return "", err
	}
	if repo == "" {
		repo = "."
	}
	_, p, err := a.cfg.RepoPath(repo)
	if err != nil {
		return "", fail(exitUsage, "%v", err)
	}
	return p, nil
}

// ledgerFail maps a ledger error to exit 1 with its message; anything
// else passes through.
func ledgerFail(err error) error {
	var le *spec.LedgerError
	if errors.As(err, &le) {
		return fail(exitError, "%s", le.Msg)
	}
	return err
}

// cmdSpecList prints the ledger: one row per spec, or the frontmatter as
// JSON with --json. --status keeps one status.
func (a *app) cmdSpecList(args []string) error {
	fs := a.flags("spec list")
	repo := fs.String("repo", "", "repo name or path (default the current directory)")
	status := fs.String("status", "", "only specs in this status")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, specUsage)
	}
	if *status != "" && !spec.ValidStatus(*status) {
		return fail(exitUsage, "--status %q is not one of %s", *status, strings.Join(spec.Statuses, ", "))
	}
	repoPath, err := a.ledgerRepo(*repo)
	if err != nil {
		return err
	}
	all, err := spec.Scan(repoPath, a.cfg.Spec.Glob)
	if err != nil {
		return ledgerFail(err)
	}
	var specs []*spec.Spec
	for _, s := range all {
		if *status == "" || s.Status() == *status {
			specs = append(specs, s)
		}
	}
	if a.json {
		b, err := spec.MarshalJSON(specs)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "%s\n", b)
		return nil
	}
	for _, s := range specs {
		fmt.Fprintln(a.stdout, s.Row())
	}
	return nil
}

// cmdSpecNextID prints the next free id, three digits.
func (a *app) cmdSpecNextID(args []string) error {
	fs := a.flags("spec next-id")
	repo := fs.String("repo", "", "repo name or path (default the current directory)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, specUsage)
	}
	repoPath, err := a.ledgerRepo(*repo)
	if err != nil {
		return err
	}
	specs, err := spec.Scan(repoPath, a.cfg.Spec.Glob)
	if err != nil {
		return ledgerFail(err)
	}
	fmt.Fprintf(a.stdout, "%03d\n", spec.NextID(specs))
	return nil
}

// cmdSpecSet sets a spec's status and fields in its file; --from refuses a
// spec whose status is not one of the listed ones, changing nothing.
func (a *app) cmdSpecSet(args []string) error {
	fs := a.flags("spec set")
	repo := fs.String("repo", "", "repo name or path (default the current directory)")
	id := fs.String("id", "", "spec id")
	status := fs.String("status", "", "the new status")
	from := fs.String("from", "", "comma-separated statuses it may move from")
	var extra fields
	fs.Var(&extra, "field", "KEY=VALUE to set (repeatable)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || *id == "" || *status == "" {
		return fail(exitUsage, specUsage)
	}
	n, err := strconv.Atoi(*id)
	if err != nil {
		return fail(exitUsage, "--id %q is not a number", *id)
	}
	if !spec.ValidStatus(*status) {
		return fail(exitUsage, "--status %q is not one of %s", *status, strings.Join(spec.Statuses, ", "))
	}
	var allowed []string
	if *from != "" {
		allowed = strings.Split(*from, ",")
	}
	repoPath, err := a.ledgerRepo(*repo)
	if err != nil {
		return err
	}
	specs, err := spec.Scan(repoPath, a.cfg.Spec.Glob)
	if err != nil {
		return ledgerFail(err)
	}
	s, ok := spec.Find(specs, n)
	if !ok {
		return fail(exitError, "no spec %03d in %s", n, filepath.Join(repoPath, spec.Dir(a.cfg.Spec.Glob)))
	}
	updated, err := spec.SetStatus(s, *status, allowed, extra...)
	if err != nil {
		return ledgerFail(err)
	}
	fmt.Fprintln(a.stdout, updated.Row())
	return nil
}

// cmdSpecTemplate prints the half-pager: the repo's override when it has
// one, else the embedded template.
func (a *app) cmdSpecTemplate(args []string) error {
	fs := a.flags("spec template")
	repo := fs.String("repo", "", "repo whose override to prefer (default the current directory)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, specUsage)
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	repoPath := ""
	if *repo != "" {
		if _, repoPath, err = a.cfg.RepoPath(*repo); err != nil {
			return fail(exitUsage, "%v", err)
		}
	} else if top, err := gitRev("--show-toplevel"); err == nil {
		repoPath = top
	}
	text, _, err := brief.Template(repoPath, "half-pager")
	if err != nil {
		return err
	}
	fmt.Fprint(a.stdout, text)
	return nil
}

// cmdSpecReview applies the operator's reply to a repo's proposed specs:
// bmesh spec review <repo> "<reply>". It levels main, makes a bmesh
// worktree from it, applies each verdict with the ledger's refusals,
// commits `Review: ...`, fast-forwards main, pushes, removes the worktree
// and prints one line per spec and the unmatched items.
func (a *app) cmdSpecReview(args []string) error {
	fs := a.flags("spec review")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return fail(exitUsage, "usage: bmesh spec review <repo> \"<reply>\"")
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	name, repoPath, err := a.cfg.RepoPath(pos[0])
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := levelMain(ctx, repoPath); err != nil {
		return fail(exitRefused, "refused: %v", err)
	}
	tag, err := shortHex()
	if err != nil {
		return err
	}
	branch := name + "-review-" + tag
	wt := filepath.Join(a.cfg.Expand(a.cfg.Runner.Worktrees), name, branch)
	if err := addWorktree(ctx, repoPath, wt, branch, "main"); err != nil {
		return err
	}
	// The worktree always goes; the branch goes with it unless it holds a
	// review commit that did not land, which is kept and named.
	committed, landed := false, false
	defer func() {
		keep := committed && !landed
		if err := removeWorktree(ctx, repoPath, wt, branch, keep); err != nil {
			fmt.Fprintf(a.stderr, "bmesh: %v\n", err)
		}
	}()
	lines, changed, err := a.applyReview(wt, pos[1])
	if err != nil {
		return err
	}
	for _, l := range lines {
		fmt.Fprintln(a.stdout, l)
	}
	if len(changed) == 0 {
		fmt.Fprintln(a.stdout, "nothing to land")
		return nil
	}
	msg := "Review: " + strings.Join(changed, ", ")
	if _, err := gitIn(ctx, wt, "add", "-A", "--", spec.Dir(a.cfg.Spec.Glob)); err != nil {
		return err
	}
	if _, err := gitIn(ctx, wt, "-c", "user.name=bmesh", "-c", "user.email=bmesh@localhost", "commit", "-q", "-m", msg); err != nil {
		return err
	}
	committed = true
	sha, err := gitIn(ctx, wt, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if err := landOnMain(ctx, repoPath, branch); err != nil {
		return fail(exitRefused, "refused: %v; the review is committed on branch %s", err, branch)
	}
	landed = true
	if err := pushMain(ctx, repoPath); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "landed %s on main: %s\n", sha[:8], msg)
	return nil
}

// applyReview parses reply against the ledger in wt and applies it; it
// returns the report lines and, for the commit message, what changed.
func (a *app) applyReview(wt, reply string) (lines, changed []string, err error) {
	specs, err := spec.Scan(wt, a.cfg.Spec.Glob)
	if err != nil {
		return nil, nil, ledgerFail(err)
	}
	r := spec.ParseReply(reply, spec.IDSet(specs), spec.Ranks(specs))
	// One outcome per spec: the last clause with a move wins, any clause
	// may set the judge, so "3 yes ... 3 on opus" is one accepted on opus.
	type outcome struct {
		move, note, judge string
	}
	merged := map[int]*outcome{}
	var order []int
	for _, v := range r.Verdicts {
		o, ok := merged[v.ID]
		if !ok {
			o = &outcome{}
			merged[v.ID] = o
			order = append(order, v.ID)
		}
		if v.Move != "" {
			o.move, o.note = v.Move, v.Note
		}
		if v.Judge != "" {
			o.judge = v.Judge
		}
	}
	date := time.Now().UTC().Format("2006-01-02")
	for _, id := range order {
		o := merged[id]
		s, _ := spec.Find(specs, id)
		old := s.Status()
		if !spec.IsReviewable(old) {
			lines = append(lines, fmt.Sprintf("%03d %s: %s, left unchanged (not proposed, later or adjusting)", s.ID, s.Title(), old))
			continue
		}
		var extra []spec.Field
		if o.judge != "" {
			extra = append(extra, spec.Field{Key: "judge", Value: o.judge})
		}
		to := o.move
		if to == "" {
			to = old
		}
		if to == "rejected" && o.note != "" {
			extra = append(extra, spec.Field{Key: "reason", Value: o.note})
		}
		n, err := spec.SetStatus(s, to, spec.Reviewable, extra...)
		if err != nil {
			return nil, nil, ledgerFail(err)
		}
		if to == "adjusting" {
			if n, err = spec.WriteBody(n, spec.AdjustNote(n.Body, date, o.note)); err != nil {
				return nil, nil, ledgerFail(err)
			}
		}
		line := fmt.Sprintf("%03d %s: %s -> %s", n.ID, n.Title(), old, to)
		if o.judge != "" {
			line += " on " + o.judge
		}
		if o.note != "" {
			line += " (" + o.note + ")"
		}
		lines = append(lines, line)
		changed = append(changed, fmt.Sprintf("%03d %s", n.ID, to))
	}
	for _, u := range r.Unmatched {
		lines = append(lines, "unmatched: "+u)
	}
	if _, err := spec.Scan(wt, a.cfg.Spec.Glob); err != nil {
		return nil, nil, ledgerFail(err)
	}
	return lines, changed, nil
}

// cmdSpecVerified prints the verified line composed from the results for
// the cwd repo's HEAD tree; --write sets it in the spec's file in this
// worktree. The spec defaults to this node's.
func (a *app) cmdSpecVerified(args []string) error {
	fs := a.flags("spec verified")
	id := fs.String("spec", "", "spec id (default this node's spec)")
	write := fs.Bool("write", false, "write the line into the spec's frontmatter")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, specUsage)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	p, err := a.identity(ctx, "spec")
	switch {
	case errors.Is(err, errNoNode):
		p = nil
	case err != nil:
		return err
	}
	top, err := gitRev("--show-toplevel")
	if err != nil {
		return err
	}
	entries, err := spec.ComposeVerified(ctx, a.client, top)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fail(exitRefused, "%v", spec.ErrNoResults)
	}
	fmt.Fprintf(a.stdout, "verified: %s\n", spec.FormatVerified(entries))
	if !*write {
		return nil
	}
	if *id == "" && p != nil {
		n, ok, err := mesh.GetNode(ctx, a.client, p.String())
		if err != nil {
			return err
		}
		if ok {
			*id = n.Spec
		}
	}
	if *id == "" {
		return fail(exitUsage, "spec verified --write needs --spec NNN (no node spec resolves)")
	}
	pattern := filepath.Join(top, filepath.FromSlash(strings.ReplaceAll(a.cfg.Spec.Glob, "{id}", *id)))
	hits, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("bad spec glob %q: %w", a.cfg.Spec.Glob, err)
	}
	if len(hits) != 1 {
		return fail(exitRefused, "spec %s matches %d files in %s", *id, len(hits), top)
	}
	if err := spec.WriteVerified(hits[0], entries); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "wrote %s\n", hits[0])
	return nil
}

// removeWorktree drops a bmesh worktree, whatever its state, and its
// branch unless keepBranch (the branch is the caller's to judge: a landed
// one is merged into main, an abandoned one holds nothing).
func removeWorktree(ctx context.Context, repoPath, wt, branch string, keepBranch bool) error {
	if _, err := os.Stat(wt); err == nil {
		if _, err := gitIn(ctx, repoPath, "worktree", "remove", "--force", wt); err != nil {
			return err
		}
	}
	_, _ = gitIn(ctx, repoPath, "worktree", "prune")
	if keepBranch {
		return nil
	}
	_, err := gitIn(ctx, repoPath, "branch", "-D", branch)
	return err
}
