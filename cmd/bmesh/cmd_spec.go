package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/brief"
	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/gitfetch"
	"github.com/rickdiculousli/buildermesh/internal/gitwork"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

const specUsage = "usage: bmesh spec list [--repo R] [--status S] [--json] | next-id [--repo R] [--count N] [--peek] | " +
	"set --id NNN --status S [--repo R] [--from A,B] [--field K=V]... | template [--repo R] | " +
	"verified [--spec NNN] [--commit C] [--write] | steps [<NNN>] [--repo R] [--tick N,N [--note W] | --check [--wait D]]"

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
		case "steps":
			return a.cmdSpecSteps(args[1:])
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

// cmdSpecNextID claims the repo's next spec id (--count N: a block of N)
// on the mesh's counter and prints it, three digits, or the block as
// NNN-NNN. --peek prints what the next claim would get and claims nothing.
// With no mesh it prints main's next id and says on stderr that nothing is
// claimed.
func (a *app) cmdSpecNextID(args []string) error {
	fs := a.flags("spec next-id")
	repo := fs.String("repo", "", "repo name or path (default the current directory)")
	count := fs.Int("count", 1, "how many ids to claim, a contiguous block")
	peek := fs.Bool("peek", false, "print what the next claim would get, claiming nothing")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, specUsage)
	}
	if *count < 1 {
		return fail(exitUsage, "--count %d: want at least 1", *count)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	name, repoPath, err := a.ledgerRepoNamed(ctx, *repo)
	if err != nil {
		return err
	}
	highest, err := a.mainHighest(ctx, repoPath)
	if err != nil {
		return err
	}
	ids := func(first int) string {
		if *count == 1 {
			return fmt.Sprintf("%03d", first)
		}
		return fmt.Sprintf("%03d-%03d", first, first+*count-1)
	}
	if err := a.connect(ctx, "spec-next-id"); err != nil {
		if *count == 1 {
			fmt.Fprintf(a.stderr, "next-id: no mesh, so %s is not claimed: another writer may take it\n", ids(highest+1))
		} else {
			fmt.Fprintf(a.stderr, "next-id: no mesh, so %s are not claimed: another writer may take them\n", ids(highest+1))
		}
		fmt.Fprintln(a.stdout, ids(highest+1))
		return nil
	}
	var first int
	if *peek {
		first, err = mesh.PeekSpecID(ctx, a.client, name, highest)
	} else {
		first, err = mesh.ClaimSpecIDs(ctx, a.client, name, highest, *count)
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, ids(first))
	return nil
}

// ledgerRepoNamed is ledgerRepo with the registry name the repo's spec ids
// are claimed under: a worktree's is its main checkout's.
func (a *app) ledgerRepoNamed(ctx context.Context, repo string) (name, path string, err error) {
	if path, err = a.ledgerRepo(repo); err != nil {
		return "", "", err
	}
	if repo == "" {
		repo = "."
	}
	name, _, _ = a.cfg.RepoPath(repo)
	if _, ok := a.cfg.Repos[name]; ok {
		return name, path, nil
	}
	if common, err := gitIn(ctx, path, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil && filepath.Base(common) == ".git" {
		if n, _, err := a.cfg.RepoPath(filepath.Dir(common)); err == nil {
			name = n
		}
	}
	return name, path, nil
}

// mainHighest is the highest spec id on the repo's main: origin/main's
// after a fetch when the repo has an origin, else the checkout's ledger.
func (a *app) mainHighest(ctx context.Context, repoPath string) (int, error) {
	read := func() ([]*spec.Spec, error) { return spec.Scan(repoPath, a.cfg.Spec.Glob) }
	if gitwork.HasOrigin(ctx, repoPath) {
		if _, err := gitIn(ctx, repoPath, gitfetch.Args("--quiet", "origin", "main")...); err == nil {
			read = func() ([]*spec.Spec, error) { return spec.ScanRef(repoPath, a.cfg.Spec.Glob, "origin/main") }
		}
	}
	specs, err := read()
	if err != nil {
		return 0, ledgerFail(err)
	}
	return spec.NextID(specs) - 1, nil
}

// landFail is ledgerFail for a command about to push the ledger: a
// duplicate id is refused (exit 3) naming both files, so a ledger nobody
// can read never reaches main.
func landFail(err error) error {
	var le *spec.LedgerError
	if errors.As(err, &le) && le.Shared[0] != "" {
		return fail(exitRefused, "refused: %s and %s share id %03d; renumber the newer with bmesh spec next-id",
			le.Shared[0], le.Shared[1], le.ID)
	}
	return ledgerFail(err)
}

// checkLedger refuses the ledger in dir that is about to be pushed when it
// doesn't read, as landFail says it.
func (a *app) checkLedger(dir string) error {
	if _, err := spec.Scan(dir, a.cfg.Spec.Glob); err != nil {
		return landFail(err)
	}
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
		return landFail(err)
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
// one, else the embedded template. In bmesh's own repo (ownModuleRepo) it
// carries an `## Upgrading` section before `## Needs from the operator`.
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
	if repoPath != "" && ownModuleRepo(repoPath) {
		text = brief.WithUpgrading(text)
	}
	fmt.Fprint(a.stdout, text)
	return nil
}

// cmdChooseIdeas applies the operator's reply to a repo's proposed specs:
// bmesh choose-ideas <repo> "<reply>". It levels main, makes a bmesh
// worktree from it, applies each verdict with the ledger's refusals,
// commits `Review: ...`, fast-forwards main, pushes, removes the worktree
// and prints one line per spec and the unmatched items.
func (a *app) cmdChooseIdeas(args []string) error {
	fs := a.flags("choose-ideas")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return fail(exitUsage, "usage: bmesh choose-ideas <repo> \"<reply>\"")
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
	var msg string
	sha, err := a.editOnMain(ctx, name, repoPath, "review", func(wt string) (string, error) {
		lines, changed, err := a.applyReview(ctx, wt, name, pos[1])
		if err != nil {
			return "", err
		}
		for _, l := range lines {
			fmt.Fprintln(a.stdout, l)
		}
		if len(changed) == 0 {
			return "", nil
		}
		msg = "Review: " + strings.Join(changed, ", ")
		return msg, nil
	})
	if err != nil {
		return err
	}
	if sha == "" {
		fmt.Fprintln(a.stdout, "nothing to land")
		return nil
	}
	fmt.Fprintf(a.stdout, "landed %s on main: %s\n", sha[:8], msg)
	return nil
}

// editOnMain lands a ledger edit on main with gitwork.EditOnMain: a bmesh
// worktree from levelled main on a one-off branch, `<repo token>-<what>-
// <hex>`, where edit runs. When edit returns a commit message, the spec
// dir's changes are committed, checked, pushed and fast-forwarded onto
// main, returning the commit; "" when edit had nothing to land. A refusal
// keeps a commit that did not land on its branch and names it.
func (a *app) editOnMain(ctx context.Context, name, repoPath, what string, edit func(wt string) (string, error)) (string, error) {
	dir := spec.Dir(a.cfg.Spec.Glob)
	return gitwork.EditOnMain(ctx, gitwork.Edit{
		Repo: name, RepoPath: repoPath, What: what,
		Worktrees: a.cfg.Expand(a.cfg.Runner.Worktrees),
		Lock:      gitwork.RepoLock(a.cfg, config.StateDir(), repoPath),
		Check:     a.checkLedger,
		Warn:      func(err error) { fmt.Fprintf(a.stderr, "bmesh: %v\n", err) },
	}, func(wt string) (string, []string, error) {
		msg, err := edit(wt)
		return msg, []string{dir}, err
	})
}

// applyReview parses reply against the ledger in wt and applies it; it
// returns the report lines and, for the commit message, what changed. The
// ranks are the lines of the pitch the operator saw last (the newest
// settled round's text on the mesh); without the mesh, or a settled round,
// only ids count.
func (a *app) applyReview(ctx context.Context, wt, repo, reply string) (lines, changed []string, err error) {
	specs, err := spec.Scan(wt, a.cfg.Spec.Glob)
	if err != nil {
		return nil, nil, landFail(err)
	}
	ranks, err := a.latestRanks(ctx, repo, specs)
	if err != nil {
		lines = append(lines, "note: the mesh is unreachable, so numbers are spec ids only ("+err.Error()+")")
	} else if len(ranks) == 0 {
		lines = append(lines, "note: no settled round with a pitch, so numbers are spec ids only")
	}
	r := spec.ParseReply(reply, spec.IDSet(specs), ranks)
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
	accepted := map[int]bool{}
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
		if o.move == "adjusting" && o.note != "" {
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
		if to == "accepted" && old != "accepted" {
			accepted[n.ID] = true
		}
	}
	for _, u := range r.Unmatched {
		lines = append(lines, "unmatched: "+u)
	}
	after, err := spec.Scan(wt, a.cfg.Spec.Glob)
	if err != nil {
		return nil, nil, ledgerFail(err)
	}
	for _, w := range acceptClashes(after, accepted) {
		lines = append(lines, "warning: "+w)
	}
	return lines, changed, nil
}

// acceptClashes is the command clashes (spec.CommandClashes) of each spec
// this review accepted, against main's commands and the ledger as the
// review left it. Two specs accepted together that define one command
// warn once, from the higher id, naming the lower.
func acceptClashes(specs []*spec.Spec, accepted map[int]bool) []string {
	var out []string
	for _, s := range specs {
		if !accepted[s.ID] {
			continue
		}
		others := slices.DeleteFunc(slices.Clone(specs), func(o *spec.Spec) bool { return accepted[o.ID] && o.ID > s.ID })
		out = append(out, spec.CommandClashes(s, others, mainCommands())...)
	}
	return out
}

// mainCommands is the commands this binary's `bmesh help` lists: main's,
// on a box whose bmesh is up to date.
func mainCommands() []string { return spec.UsageCommands(usage) }

// latestRanks is the pitch the operator saw last, as ranks: the text of
// the repo's newest settled round (state done or blocked, which is what was
// pushed) that names any spec; a settled round whose text names none (one
// blocked before a pitch) is passed over for an older one. An open round's
// children, even a finished one, are not a pitch yet; nil when no settled
// round has one.
func (a *app) latestRanks(ctx context.Context, repo string, specs []*spec.Spec) ([]int, error) {
	if err := a.connect(ctx, "choose-ideas"); err != nil {
		return nil, err
	}
	n, err := nextRound(ctx, a.client, repo)
	if err != nil {
		return nil, err
	}
	for k := n - 1; k >= 1; k-- {
		st, _, err := mesh.ReadState(ctx, a.client, fmt.Sprintf("%s-round-%d", mesh.RepoToken(repo), k))
		if err != nil {
			return nil, err
		}
		if st.State != "done" && st.State != "blocked" {
			continue
		}
		if ranks := spec.PitchRanks(st.Text, specs); len(ranks) > 0 {
			return ranks, nil
		}
	}
	return nil, nil
}

// cmdSpecVerified prints the verified line composed from the results for
// the cwd repo's HEAD tree, or --commit's; --write sets it in the spec's
// file in this worktree. The spec defaults to this node's.
func (a *app) cmdSpecVerified(args []string) error {
	fs := a.flags("spec verified")
	id := fs.String("spec", "", "spec id (default this node's spec)")
	commit := fs.String("commit", "HEAD", "compose from the results for this commit's tree")
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
	tree, err := spec.Tree(top, *commit)
	if err != nil {
		return fail(exitUsage, "--commit %q is not a commit in %s", *commit, top)
	}
	entries, err := spec.ComposeVerified(ctx, a.client, top, *commit)
	if err != nil {
		return err
	}
	switch {
	case len(entries) == 0 && *commit == "HEAD":
		return fail(exitRefused, "%v", spec.ErrNoResults)
	case len(entries) == 0:
		return fail(exitRefused, "no results for %s's tree t:%s", *commit, tree[:8])
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
