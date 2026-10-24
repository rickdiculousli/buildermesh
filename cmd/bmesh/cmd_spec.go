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
	"set --id NNN[,NNN...] --status S [--repo R] [--from A,B] [--field K=V]... [--land] | template [--repo R] | " +
	"verified [--commit C] | steps [<NNN>] [--repo R] [--tick N,N [--note W] | --check [--wait D]]"

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

// cmdSpecSet sets the status and fields of one spec or several (--id
// NNN,MMM) in their files; --from refuses a spec whose status is not one of
// the listed ones, and any refusal changes none of them. It edits the
// checkout --repo names, or with --land lands the edit on main in one
// commit from a bmesh worktree.
func (a *app) cmdSpecSet(args []string) error {
	fs := a.flags("spec set")
	repo := fs.String("repo", "", "repo name or path (default the current directory)")
	id := fs.String("id", "", "spec id, or a comma list")
	status := fs.String("status", "", "the new status")
	from := fs.String("from", "", "comma-separated statuses it may move from")
	land := fs.Bool("land", false, "land the edit on main in one commit, from a bmesh worktree")
	var extra fields
	fs.Var(&extra, "field", "KEY=VALUE to set (repeatable)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || *id == "" || *status == "" {
		return fail(exitUsage, specUsage)
	}
	var ids []int
	for _, x := range strings.Split(*id, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return fail(exitUsage, "--id %q is not a number", x)
		}
		if !slices.Contains(ids, n) {
			ids = append(ids, n)
		}
	}
	slices.Sort(ids)
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
	set := func(dir, where string) ([]*spec.Spec, bool, error) {
		return a.setSpecs(dir, where, ids, *status, allowed, extra)
	}
	if !*land {
		updated, _, err := set(repoPath, "in "+filepath.Join(repoPath, spec.Dir(a.cfg.Spec.Glob)))
		if err != nil {
			return err
		}
		for _, u := range updated {
			fmt.Fprintln(a.stdout, u.Row())
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// A worktree's edit lands through its main checkout.
	if common, err := gitIn(ctx, repoPath, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil && filepath.Base(common) == ".git" {
		repoPath = filepath.Dir(common)
	}
	name, _, err := a.cfg.RepoPath(repoPath)
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	msg := fmt.Sprintf("Spec %03d: %s", ids[0], *status)
	if len(ids) > 1 {
		nums := make([]string, len(ids))
		for i, n := range ids {
			nums[i] = fmt.Sprintf("%03d", n)
		}
		msg = fmt.Sprintf("Specs %s: %s", strings.Join(nums, ", "), *status)
	}
	sha, err := a.editOnMain(ctx, name, repoPath, "spec", func(wt string) (string, error) {
		updated, changed, err := set(wt, "on main in "+repoPath)
		if err != nil {
			return "", err
		}
		for _, u := range updated {
			fmt.Fprintln(a.stdout, u.Row())
		}
		if !changed {
			return "", nil
		}
		return msg, nil
	})
	if r := (*gitwork.Refused)(nil); errors.As(err, &r) {
		return fail(exitRefused, "%s; run bmesh spec set --land again once main in %s is level with origin (it levels main first)", r.Msg, repoPath)
	}
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

// setSpecs sets status and extra on the specs ids names in the ledger in
// dir, all or none: a missing id (said with where) or a status --from does
// not allow refuses before anything is written. It returns the specs as
// written and whether any file changed.
func (a *app) setSpecs(dir, where string, ids []int, status string, allowed []string, extra []spec.Field) ([]*spec.Spec, bool, error) {
	specs, err := spec.Scan(dir, a.cfg.Spec.Glob)
	if err != nil {
		return nil, false, landFail(err)
	}
	var todo []*spec.Spec
	for _, n := range ids {
		s, ok := spec.Find(specs, n)
		if !ok {
			return nil, false, fail(exitError, "no spec %03d %s; bmesh spec list shows the ids", n, where)
		}
		if err := spec.CheckFrom(s, allowed); err != nil {
			return nil, false, ledgerFail(err)
		}
		todo = append(todo, s)
	}
	var out []*spec.Spec
	changed := false
	for _, s := range todo {
		u, err := spec.SetStatus(s, status, allowed, extra...)
		if err != nil {
			return nil, false, ledgerFail(err)
		}
		changed = changed || u.Text != s.Text
		out = append(out, u)
	}
	return out, changed, nil
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

// mainCommands is the commands this binary's `bmesh help` lists: main's,
// on a box whose bmesh is up to date.
func mainCommands() []string { return spec.UsageCommands(usage) }

// cmdSpecVerified prints the verified line composed from the results for
// the cwd repo's HEAD tree, or --commit's. It writes nothing: land puts the
// line on the commit it lands, as its Verified trailer.
func (a *app) cmdSpecVerified(args []string) error {
	fs := a.flags("spec verified")
	commit := fs.String("commit", "HEAD", "compose from the results for this commit's tree")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, specUsage)
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	if err := a.connect(ctx, "spec"); err != nil {
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
	return nil
}
