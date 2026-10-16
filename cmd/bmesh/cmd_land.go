package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

const landUsage = "usage: bmesh land [--spec NNN] [--ttl D] [--wait D]"

// landPoll is how often land tries a held lease again; tests shorten it.
var landPoll = 2 * time.Second

// landRepoRE is what a repo name must look like to name a lease key.
var landRepoRE = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*$`)

// lander is one `bmesh land`: a lead's worktree landing its spec on main.
type lander struct {
	a        *app
	top      string // the lead's worktree
	repoPath string // the main checkout
	id       int    // the spec
	holder   string // the node (or host) holding the lease and recording results
	branch   string
	origin   bool
	base     string // origin/main, or main without an origin

	oldHead, oldTree, oldBase string                 // the branch before land touched it
	oldResults                map[string]mesh.Result // per kind at oldTree: its newest result, a failing one when any model's newest fails
	required                  []string               // the kinds the landed tree must carry

	landed string // the commit main points at after the push
}

// cmdLand lands the worktree's branch on main under the lease land.<repo>:
// fetch, rebase onto main, results for the rebased tree, the spec's verified
// line, done with merged and branch, a commit, a fast-forward push. A push
// main refuses (it moved) starts again from the fetch, once.
func (a *app) cmdLand(args []string) error {
	fs := a.flags("land")
	id := fs.String("spec", "", "spec id (default this node's spec)")
	ttl := fs.Duration("ttl", defaultTTL, "how long the lease lasts unrenewed")
	wait := fs.Duration("wait", 30*time.Minute, "how long to wait for a held lease (exit 5 after)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, landUsage)
	}
	if *ttl < minTTL {
		return fail(exitUsage, "land: --ttl must be at least %s", minTTL)
	}
	top, err := gitRev("--show-toplevel")
	if err != nil {
		return err
	}
	ctx, cancel := cmdCtx()
	defer cancel()
	p, err := a.identity(ctx, "land")
	switch {
	case errors.Is(err, errNoNode):
		p = nil
	case err != nil:
		return err
	}
	l := &lander{a: a, top: top, holder: a.cfg.HostName()}
	repo := a.repoNameAt(top)
	if p != nil {
		l.holder = p.String()
		if repo, err = a.repoName(ctx, p, top); err != nil {
			return err
		}
		if *id == "" {
			n, ok, err := mesh.GetNode(ctx, a.client, p.String())
			if err != nil {
				return err
			}
			if ok {
				*id = n.Spec
			}
		}
	}
	if *id == "" {
		return fail(exitUsage, "land needs --spec NNN (no node spec resolves)")
	}
	if l.id, err = strconv.Atoi(*id); err != nil {
		return fail(exitUsage, "--spec %q is not a number", *id)
	}
	if !landRepoRE.MatchString(repo) {
		return fail(exitUsage, "repo name %q can't name a lease", repo)
	}
	common, ok := checkout.CommonDir(top)
	if !ok || filepath.Base(common) != ".git" {
		return fail(exitRefused, "refused: %s is not a worktree of a repo with a main checkout", top)
	}
	l.repoPath = filepath.Dir(common)
	if err := l.prepare(ctx); err != nil {
		return err
	}

	slot := "land." + repo
	key, err := a.acquireWait(slot, l.holder, *ttl, *wait)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "lease %s\n", slot)
	if err := a.holdLeases([]string{key}, l.holder, *ttl, l.land); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "released %s\nlanded %s\n", slot, l.landed[:8])
	return nil
}

// acquireWait takes the one place of slot for holder, trying again every
// landPoll while someone else holds it; it names the holder once per holder
// and gives up after wait (exit 5).
func (a *app) acquireWait(slot, holder string, ttl, wait time.Duration) (string, error) {
	deadline := time.Now().Add(wait)
	said := ""
	for {
		ctx, cancel := cmdCtx()
		key, err := mesh.AcquireLease(ctx, a.client, slot, holder, ttl, 1, time.Now())
		if !errors.Is(err, mesh.ErrBusy) {
			cancel()
			return key, err
		}
		by := "someone"
		if leases, err := mesh.ListLeases(ctx, a.client); err == nil {
			for _, l := range leases {
				if l.Slot == slot {
					by = l.Holder
				}
			}
		}
		cancel()
		if by != said {
			fmt.Fprintf(a.stdout, "waiting for %s (held by %s)\n", slot, by)
			said = by
		}
		if time.Now().After(deadline) {
			return "", fail(exitTimeout, "timed out waiting for %s, held by %s", slot, by)
		}
		time.Sleep(landPoll)
	}
}

// prepare refuses a worktree land can't start from and reads the branch as
// it stands: its head, tree and the results recorded there, which name the
// kinds the landed tree must carry (verify always, when the repo has it).
func (l *lander) prepare(ctx context.Context) error {
	if err := refuseDirty(); err != nil {
		return err
	}
	b, err := gitIn(ctx, l.top, "branch", "--show-current")
	if err != nil {
		return err
	}
	if b == "" || b == "main" {
		return fail(exitRefused, "refused: land runs on a lead's branch, not %s", dash(b))
	}
	l.branch = b
	l.origin = hasOrigin(ctx, l.top)
	l.base = "main"
	if l.origin {
		l.base = "origin/main"
	}
	if l.oldHead, err = gitIn(ctx, l.top, "rev-parse", "HEAD"); err != nil {
		return err
	}
	if l.oldTree, err = gitIn(ctx, l.top, "rev-parse", "HEAD^{tree}"); err != nil {
		return err
	}
	all, err := mesh.ListAllResults(ctx, l.a.client)
	if err != nil {
		return err
	}
	// The newest result per (kind, model), as spec verified composes them;
	// a kind is as good as its worst model.
	type group struct{ kind, model string }
	newest := map[group]mesh.Result{}
	for _, r := range all {
		g := group{r.Kind, r.Model}
		if old, ok := newest[g]; r.Tree == l.oldTree && (!ok || spec.Newer(r.At, old.At)) {
			newest[g] = r
		}
	}
	l.oldResults = map[string]mesh.Result{}
	for g, r := range newest {
		if old, ok := l.oldResults[g.kind]; !ok || old.OK && !r.OK {
			l.oldResults[g.kind] = r
		}
	}
	for k := range l.oldResults {
		l.required = append(l.required, k)
	}
	_, kinds, err := cwdKinds()
	if err != nil {
		return err
	}
	if slices.ContainsFunc(kinds, func(k kind.Kind) bool { return k.Name == "verify" }) && !slices.Contains(l.required, "verify") {
		l.required = append(l.required, "verify")
	}
	sort.Strings(l.required)
	return nil
}

// land runs the steps under the lease, starting again from the fetch once
// when main moved before the push.
func (l *lander) land(ctx context.Context) error {
	for attempt := 1; ; attempt++ {
		moved, err := l.once(ctx)
		if err != nil || !moved {
			return err
		}
		if attempt == 2 {
			return fail(exitRefused, "refused: main moved again before the push; nothing is pushed, run bmesh land again")
		}
		fmt.Fprintln(l.a.stdout, "main moved; again from the fetch")
	}
}

// once is one pass from fetch to push; moved reports a push main refused
// because it moved, with the record commit taken back off the branch.
func (l *lander) once(ctx context.Context) (moved bool, err error) {
	out := l.a.stdout
	git := func(args ...string) (string, error) { return gitIn(ctx, l.top, args...) }
	// Cleanup runs on its own context: an interrupt or a lost lease cancels
	// ctx, and the worktree must still be left as land found it.
	cleanup := func(args ...string) (string, error) {
		c, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return gitIn(c, l.top, args...)
	}
	if l.origin {
		if _, err := git("fetch", "--quiet", "origin", "main"); err != nil {
			return false, err
		}
		if n, err := git("rev-list", "--count", "origin/main..refs/heads/main"); err == nil && n != "0" {
			return false, fail(exitRefused, "refused: main in %s has %s commits origin/main doesn't: someone's unpushed work; "+
				"wait for it to be pushed (never push it yourself), then run bmesh land again", l.repoPath, n)
		}
	}
	baseSHA, err := git("rev-parse", l.base)
	if err != nil {
		return false, err
	}
	fmt.Fprintf(out, "fetch %s %s\n", l.base, baseSHA[:8])
	if l.oldBase == "" {
		if l.oldBase, err = git("merge-base", l.oldHead, baseSHA); err != nil {
			return false, err
		}
	}

	if _, err := git("merge-base", "--is-ancestor", baseSHA, "HEAD"); err == nil {
		fmt.Fprintf(out, "rebase: on %s already\n", baseSHA[:8])
	} else if _, err := git("rebase", "--quiet", baseSHA); err != nil {
		files, _ := cleanup("diff", "--name-only", "--diff-filter=U")
		if _, aerr := cleanup("rebase", "--abort"); aerr != nil {
			return false, fmt.Errorf("rebase onto %s failed (%v) and did not abort: %v", baseSHA[:8], err, aerr)
		}
		what := "fails"
		if files != "" {
			what = "conflicts in " + strings.Join(strings.Fields(files), ", ")
		}
		return false, fail(exitRefused, "refused: rebase onto %s %s; it is aborted and nothing is pushed. "+
			"Rebase yourself, resolve it, re-validate, then run bmesh land again", l.base, what)
	} else {
		n, _ := git("rev-list", "--count", baseSHA+"..HEAD")
		fmt.Fprintf(out, "rebase onto %s: %s commits\n", baseSHA[:8], n)
	}
	if n, err := git("rev-list", "--count", baseSHA+"..HEAD"); err != nil {
		return false, err
	} else if n == "0" {
		return false, fail(exitRefused, "refused: nothing to land: HEAD is on %s", l.base)
	}
	merged, err := git("rev-parse", "HEAD")
	if err != nil {
		return false, err
	}
	tree, err := git("rev-parse", "HEAD^{tree}")
	if err != nil {
		return false, err
	}

	specs, err := spec.Scan(l.top, l.a.cfg.Spec.Glob)
	if err != nil {
		return false, ledgerFail(err)
	}
	s, ok := spec.Find(specs, l.id)
	if !ok {
		return false, fail(exitRefused, "refused: no spec %03d in %s", l.id, l.top)
	}
	if s.Status() != "building" {
		return false, fail(exitRefused, "refused: spec %03d reads %s, not building", l.id, s.Status())
	}

	entries, err := l.results(ctx, merged, tree, baseSHA)
	if err != nil {
		return false, err
	}

	rel, err := filepath.Rel(l.top, s.Path)
	if err != nil {
		return false, err
	}
	restore := func() { _, _ = cleanup("checkout", "HEAD", "--", rel) }
	if _, err := spec.SetStatus(s, "done", []string{"building"},
		spec.Field{Key: "merged", Value: merged}, spec.Field{Key: "branch", Value: l.branch}); err != nil {
		restore()
		return false, ledgerFail(err)
	}
	if err := spec.WriteVerified(s.Path, entries); err != nil {
		restore()
		return false, err
	}
	fmt.Fprintf(out, "verified: %s\n", spec.FormatVerified(entries))
	fmt.Fprintf(out, "spec %03d done: merged %s, branch %s\n", l.id, merged[:8], l.branch)
	msg := fmt.Sprintf("%03d: done at %s", l.id, merged[:8])
	if _, err := git("add", "--", rel); err != nil {
		restore()
		return false, err
	}
	if _, err := git("commit", "--quiet", "-m", msg); err != nil {
		_, _ = cleanup("reset", "--quiet", "--", rel)
		restore()
		return false, err
	}
	rec, err := git("rev-parse", "HEAD")
	if err != nil {
		return false, err
	}
	fmt.Fprintf(out, "commit %s %s\n", rec[:8], msg)
	undo := func() { _, _ = cleanup("reset", "--quiet", "--keep", "HEAD~1") }

	if l.origin {
		if _, err := git("push", "--quiet", "origin", "HEAD:refs/heads/main"); err != nil {
			undo()
			if l.mainMoved(ctx, baseSHA) {
				return true, nil
			}
			return false, fmt.Errorf("push main: %v", err)
		}
		fmt.Fprintf(out, "push origin main %s\n", rec[:8])
		// origin has it; the main checkout follows when it can.
		if err := landOnMain(ctx, l.repoPath, l.branch); err != nil {
			fmt.Fprintf(l.a.stderr, "bmesh: %v; origin/main has %s\n", err, rec[:8])
		} else {
			fmt.Fprintf(out, "main %s\n", rec[:8])
		}
	} else {
		if err := landOnMain(ctx, l.repoPath, l.branch); err != nil {
			undo()
			if l.mainMoved(ctx, baseSHA) {
				return true, nil
			}
			return false, err
		}
		fmt.Fprintf(out, "main %s\n", rec[:8])
	}
	l.landed = rec
	return false, nil
}

// mainMoved reports whether main (origin's, fetched again, when there is
// an origin) no longer points at baseSHA.
func (l *lander) mainMoved(ctx context.Context, baseSHA string) bool {
	if l.origin {
		if _, err := gitIn(ctx, l.top, "fetch", "--quiet", "origin", "main"); err != nil {
			return false
		}
	}
	now, err := gitIn(ctx, l.top, "rev-parse", l.base)
	return err == nil && now != baseSHA
}

// results makes sure the rebased tree carries every required kind: an
// inline kind runs (a deterministic one is answered from its record);
// another must have its own result there or one carried to it by patch-id
// (spec.ComposeVerified), unless the branch had an ok one before the rebase
// that recorded no patch-id and the rebase left the diff unchanged, which is
// said and left off the line. It returns the verified entries for the tree,
// refusing a failing one.
func (l *lander) results(ctx context.Context, merged, tree, baseSHA string) ([]spec.Entry, error) {
	out := l.a.stdout
	_, kinds, err := cwdKinds()
	if err != nil {
		return nil, err
	}
	byName := map[string]kind.Kind{}
	for _, k := range kinds {
		byName[k.Name] = k
	}
	for _, name := range l.required {
		k, ok := byName[name]
		if !ok || k.Mode != "inline" {
			continue
		}
		model := ""
		if k.IsModel() {
			model = k.ModelsList()[0]
		}
		ok, output, how, err := l.a.runInline(ctx, k, l.top, merged, tree, l.holder, model, "", nil, false)
		if err != nil {
			return nil, err
		}
		line := spec.Entry{Kind: name, OK: ok, Tree8: tree[:8], Model: model}.String()
		if !ok {
			if output != "" && !strings.HasSuffix(output, "\n") {
				output += "\n"
			}
			fmt.Fprintf(out, "%s%s (%s)\n", output, line, how)
			return nil, fail(exitError, "%s failed at t:%s; fix it, commit and run bmesh land again", name, tree[:8])
		}
		fmt.Fprintf(out, "%s (%s)\n", line, how)
	}
	composeCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	entries, err := spec.ComposeVerified(composeCtx, l.a.client, l.top)
	if err != nil {
		return nil, err
	}
	for _, name := range l.required {
		if k, ok := byName[name]; ok && k.Mode == "inline" {
			continue
		}
		var found []spec.Entry
		for _, e := range entries {
			if e.Kind == name {
				found = append(found, e)
			}
		}
		for _, e := range found {
			if e.Carried != "" {
				fmt.Fprintf(out, "%s (carried from t:%s: the diff is unchanged)\n", e, e.Carried)
			} else {
				fmt.Fprintf(out, "%s (recorded)\n", e)
			}
		}
		if len(found) > 0 {
			continue
		}
		old := l.oldResults[name]
		if !old.OK {
			return nil, fail(exitRefused, "refused: %s failed at t:%s, before the rebase; fix what it found, commit and run bmesh land again",
				name, l.oldTree[:8])
		}
		// A result that recorded the diff it read and did not carry read
		// another diff than this one.
		same := old.PatchID == ""
		if same {
			if same, err = samePatch(ctx, l.top, l.oldBase, l.oldHead, baseSHA, merged); err != nil {
				return nil, err
			}
		}
		if !same {
			return nil, fail(exitRefused, "refused: %s has a result for t:%s but none for t:%s, and the rebase changed the diff: "+
				"run it again on this tree (for review: bmesh routine queue review --again --go --text \"base: %s\", then routine wait), "+
				"then run bmesh land again", name, l.oldTree[:8], tree[:8], baseSHA)
		}
		fmt.Fprintf(out, "%s ok t:%s carried: the rebase left the diff unchanged (not on the verified line)\n", name, l.oldTree[:8])
	}
	if len(entries) == 0 {
		return nil, fail(exitRefused, "refused: %v", spec.ErrNoResults)
	}
	for _, e := range entries {
		if !e.OK {
			return nil, fail(exitRefused, "refused: %s fails at t:%s; fix it, commit and run bmesh land again", e.Kind, e.Tree8)
		}
	}
	return entries, nil
}

// samePatch reports whether the diff a..b has the patch-id of c..d.
func samePatch(ctx context.Context, dir, a, b, c, d string) (bool, error) {
	x, err := checkout.PatchID(ctx, dir, a, b)
	if err != nil {
		return false, err
	}
	y, err := checkout.PatchID(ctx, dir, c, d)
	if err != nil {
		return false, err
	}
	return x == y, nil
}
