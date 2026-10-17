package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh"
	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/daemon"
	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/scaffold"
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
	top      string    // the lead's worktree
	repoPath string    // the main checkout
	id       int       // the spec
	holder   string    // the node (or host) holding the lease and recording results
	node     mesh.Path // the landing node, whose inbox is read; nil outside a node
	repo     string    // the registry row's name, else the lease's
	branch   string
	origin   bool
	base     string // origin/main, or main without an origin

	before   map[string]*source // per kind with a result before land touched the branch: where it is
	required []string           // the kinds the landed tree must carry

	landed string // the commit main points at after the push
}

// source is where a required kind's result was before land touched the
// branch: HEAD as it stood, or the head a rebase by hand rewrote.
type source struct {
	head, tree string
	base       string        // head's fork point from main, set at the first fetch
	rebase     string        // the commit the rebase made of head; "" for HEAD as it stood
	result     mesh.Result   // the kind's newest result at tree, a failing one when any model's newest fails
	newest     []mesh.Result // the kind's newest result per model at tree
}

// String says where the result is, for land's required line.
func (s *source) String() string {
	if s.rebase == "" {
		return "t:" + s.tree[:8] + " at HEAD"
	}
	return "t:" + s.tree[:8] + " before the rebase at " + s.rebase[:8]
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
		l.node = p
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
	l.repo = repo
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
// landPoll while someone else holds it (at once when it was released
// between the try and the look); it names the holder once per holder and
// gives up after wait (exit 5).
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
			i := slices.IndexFunc(leases, func(l mesh.Lease) bool { return l.Slot == slot })
			if i < 0 {
				// Released since the try: try again at once.
				cancel()
				continue
			}
			by = leases[i].Holder
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

// prepare refuses a worktree land can't start from and reads the results
// recorded for the branch, which name the kinds the landed tree must carry
// (verify always, when the repo has it): those at HEAD's tree and, when HEAD
// is a commit a rebase made, those at the tree of the head it rewrote, as if
// land had done that rebase itself. A kind takes the nearest head with a
// result for it. It prints the kinds and where each came from.
func (l *lander) prepare(ctx context.Context) error {
	if err := l.gate(ctx); err != nil {
		return err
	}
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
	if err := l.mail(ctx, true); err != nil {
		return err
	}
	l.origin = hasOrigin(ctx, l.top)
	l.base = "main"
	if l.origin {
		l.base = "origin/main"
	}
	head, err := gitIn(ctx, l.top, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	heads := append([]source{{head: head}}, rebasedFrom(ctx, l.top, b, head)...)
	all, err := mesh.ListAllResults(ctx, l.a.client)
	if err != nil {
		return err
	}
	l.before = map[string]*source{}
	for _, h := range heads {
		if h.tree, err = gitIn(ctx, l.top, "rev-parse", h.head+"^{tree}"); err != nil {
			return err
		}
		// The newest result per (kind, model), as spec verified composes
		// them; a kind is as good as its worst model.
		type group struct{ kind, model string }
		newest := map[group]mesh.Result{}
		for _, r := range all {
			g := group{r.Kind, r.Model}
			if old, ok := newest[g]; r.Tree == h.tree && (!ok || spec.Newer(r.At, old.At)) {
				newest[g] = r
			}
		}
		at := map[string]*source{}
		for g, r := range newest {
			if _, ok := l.before[g.kind]; ok {
				continue
			}
			src, ok := at[g.kind]
			if !ok {
				src = &source{head: h.head, tree: h.tree, rebase: h.rebase, result: r}
				at[g.kind] = src
			}
			src.newest = append(src.newest, r)
			if src.result.OK && !r.OK {
				src.result = r
			}
		}
		for k, src := range at {
			l.before[k] = src
		}
	}
	for k := range l.before {
		l.required = append(l.required, k)
	}
	_, kinds, err := cwdKinds()
	if err != nil {
		return err
	}
	repoVerify := slices.ContainsFunc(kinds, func(k kind.Kind) bool { return k.Name == "verify" })
	if repoVerify && !slices.Contains(l.required, "verify") {
		l.required = append(l.required, "verify")
	}
	sort.Strings(l.required)
	var said []string
	for _, k := range l.required {
		switch {
		case k == "verify" && repoVerify:
			said = slices.Insert(said, 0, "verify (the repo's verify kind)")
		default:
			said = append(said, fmt.Sprintf("%s (%s)", k, l.before[k]))
		}
	}
	if len(said) == 0 {
		said = []string{"nothing"}
	}
	fmt.Fprintf(l.a.stdout, "required: %s\n", strings.Join(said, ", "))
	return nil
}

// gate refuses the landing while the node is held (bm-holds), at either
// gate: nothing is pushed, and the lead runs bmesh held and waits for the
// release. Land run outside a node has no hold to read.
func (l *lander) gate(ctx context.Context) error {
	if l.node == nil {
		return nil
	}
	h, ok, err := mesh.GetLeadHold(ctx, l.a.client, l.node.String())
	if err != nil || !ok {
		return err
	}
	return fail(exitRefused, "%s; nothing is pushed: run bmesh held and end your turn, and run bmesh land again once the release wakes you", h.Line(time.Now()))
}

// mail peeks the node's inbox, holding and acking nothing: a message the
// operator sent with bmesh send refuses the landing, since it may change
// what landing needs; anything else (a report, a reply, another node's
// message) is named on one line when say, so the lead sees it before the
// push. Land run outside a node has no inbox and skips it.
func (l *lander) mail(ctx context.Context, say bool) error {
	if l.node == nil {
		return nil
	}
	pending, err := mesh.Pending(ctx, l.a.client, l.node)
	if err != nil {
		return err
	}
	operator := 0
	var others []string
	for _, m := range pending {
		if m.Kind == "message" && m.From == "operator" {
			operator++
			continue
		}
		others = append(others, daemon.WakeItem(m))
	}
	if operator > 0 {
		return fail(exitRefused, "refused: %d unread message(s) from the operator; run bmesh inbox, act on them, then run bmesh land again", operator)
	}
	if say && len(others) > 0 {
		fmt.Fprintf(l.a.stdout, "unread: %s\n", strings.Join(others, "; "))
	}
	return nil
}

// rebaseRE matches a reflog entry a finished rebase wrote on the branch
// (rebase, rebase -i, pull --rebase; "rebase finished" from older git).
var rebaseRE = regexp.MustCompile(`^(rebase|pull)\b[^:]*\(finish\): |^rebase finished: `)

// rebasedFrom reads the branch's reflog, newest first, for a rebase that
// finished at head and returns the head it rewrote (the entry just before
// it), and so on back while that head was itself made by a rebase: the
// heads land would have rebased itself, nearest first. A head with commits
// on top of the rebase, or a branch with no reflog, returns none.
func rebasedFrom(ctx context.Context, dir, branch, head string) []source {
	out, err := gitIn(ctx, dir, "reflog", "show", "--format=%H%x00%gs", "refs/heads/"+branch, "--")
	if err != nil {
		return nil
	}
	type entry struct{ sha, subject string }
	var log []entry
	for _, line := range strings.Split(out, "\n") {
		if sha, subject, ok := strings.Cut(line, "\x00"); ok {
			log = append(log, entry{sha, subject})
		}
	}
	var heads []source
	for i, cur := 0, head; ; {
		k := slices.IndexFunc(log[i:], func(e entry) bool { return e.sha == cur && rebaseRE.MatchString(e.subject) })
		if k < 0 || i+k+1 >= len(log) || log[i+k+1].sha == cur {
			return heads
		}
		i += k + 1
		heads = append(heads, source{head: log[i].sha, rebase: cur})
		cur = log[i].sha
	}
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
	// Read again under the lease: the operator may have held or written
	// while land waited for it.
	if err := l.gate(ctx); err != nil {
		return false, err
	}
	if err := l.mail(ctx, false); err != nil {
		return false, err
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
	for _, src := range l.before {
		if src.base == "" {
			if src.base, err = git("merge-base", src.head, baseSHA); err != nil {
				return false, err
			}
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

	if err := l.scaffolded(ctx, baseSHA); err != nil {
		return false, err
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
		if err := l.a.landOnMain(ctx, l.repoPath, l.branch); err != nil {
			fmt.Fprintf(l.a.stderr, "bmesh: %v; origin/main has %s\n", err, rec[:8])
		} else {
			fmt.Fprintf(out, "main %s\n", rec[:8])
		}
	} else {
		if err := l.a.landOnMain(ctx, l.repoPath, l.branch); err != nil {
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

// landDriftLines is how much of the scaffold drift a refusal prints.
const landDriftLines = 20

// ownModuleRepo reports whether the checkout at repoPath is bmesh's own
// repo: its go.mod declares the module this binary was built from.
func ownModuleRepo(repoPath string) bool {
	bi, ok := readBuildInfo()
	return ok && bi != nil && bi.Main.Path != "" && config.ModulePath(filepath.Join(repoPath, "go.mod")) == bi.Main.Path
}

// scaffolded refuses a rebased tree whose scaffolded text the branch changed
// to read otherwise than scaffold writes it from the repo's ship row (see
// scaffold.Drift): the row's verify is the source of the verify kind's
// command and of AGENTS.md's blocks. A repo with no ship row, or never
// scaffolded, is not checked. A tree of this binary's own module renders its
// own blocks, which this binary may not, so there only the verify kind is.
func (l *lander) scaffolded(ctx context.Context, baseSHA string) error {
	row, ok := l.a.cfg.Repos[l.repo]
	if !ok || row.Mode != "ship" {
		return nil
	}
	own := ownModuleRepo(l.top)
	at := func(rel string) (string, bool, error) {
		obj := baseSHA + ":" + rel
		if exec.CommandContext(ctx, "git", "-C", l.top, "cat-file", "-e", obj).Run() != nil {
			return "", false, nil
		}
		b, err := exec.CommandContext(ctx, "git", "-C", l.top, "cat-file", "blob", obj).Output()
		if err != nil {
			return "", false, fmt.Errorf("git cat-file %s: %w", obj, err)
		}
		return string(b), true, nil
	}
	diff, done, err := scaffold.Drift(l.top, row, buildermesh.AgentsSnippet, !own, at)
	if err != nil {
		return err
	}
	switch {
	case !done:
		fmt.Fprintln(l.a.stdout, "scaffold: not scaffolded, not checked")
		return nil
	case diff != "":
		lines := strings.SplitAfter(diff, "\n")
		if len(lines) > landDriftLines {
			lines = append(lines[:landDriftLines], "...\n")
		}
		fmt.Fprint(l.a.stdout, strings.Join(lines, ""))
		return fail(exitRefused, "refused: the branch's scaffolded files differ from what bmesh scaffold writes from %s's row; "+
			"the row's verify is the source: bmesh repos propose with the new verify, or restore the scaffolded files", l.repo)
	case own:
		fmt.Fprintln(l.a.stdout, "scaffold: the verify kind follows the row (AGENTS.md is rendered by this tree's own bmesh, not checked)")
	default:
		fmt.Fprintln(l.a.stdout, "scaffold: follows the row")
	}
	return nil
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
// (land's, or the one by hand its source names) that recorded no patch-id
// and the rebase left the diff unchanged, which is carried. It returns the verified entries for the tree,
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
		ok, output, how, err := l.a.runInline(ctx, k, l.top, merged, tree, l.holder, model, "", "", nil, false)
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
	entries, err := spec.ComposeVerified(composeCtx, l.a.client, l.top, "HEAD")
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
		src := l.before[name]
		if src == nil {
			return nil, fail(exitRefused, "refused: %s has no result for t:%s; run it, then run bmesh land again", name, tree[:8])
		}
		if !src.result.OK {
			return nil, fail(exitRefused, "refused: %s failed at t:%s, before the rebase; fix what it found, commit and run bmesh land again",
				name, src.tree[:8])
		}
		// A model's result that recorded the diff it read and did not
		// carry read another diff than this one, whichever model's result
		// is the kind's worst.
		same := !slices.ContainsFunc(src.newest, func(r mesh.Result) bool { return r.PatchID != "" })
		if same {
			if same, err = samePatch(ctx, l.top, src.base, src.head, baseSHA, merged); err != nil {
				return nil, err
			}
		}
		if !same {
			return nil, fail(exitRefused, "refused: %s has a result for t:%s but none for t:%s, and the rebase changed the diff: "+
				"run it again on this tree (for review: bmesh routine queue review --again --go --text \"base: %s\", then routine wait), "+
				"then run bmesh land again", name, src.tree[:8], tree[:8], baseSHA)
		}
		carried, err := l.carry(ctx, src, tree)
		if err != nil {
			return nil, err
		}
		if len(carried) == 0 {
			fmt.Fprintf(out, "%s ok t:%s carried: the rebase left the diff unchanged (not on the verified line)\n", name, src.tree[:8])
		}
		for _, e := range carried {
			fmt.Fprintf(out, "%s (carried from t:%s: the rebase left the diff unchanged)\n", e, e.Carried)
		}
		entries = append(entries, carried...)
	}
	slices.SortFunc(entries, func(a, b spec.Entry) int {
		return cmp.Or(strings.Compare(a.Kind, b.Kind), strings.Compare(a.Model, b.Model))
	})
	if len(entries) == 0 {
		return nil, fail(exitRefused, "refused: %v", spec.ErrNoResults)
	}
	for _, e := range entries {
		if !e.OK {
			at := e.Tree8
			if e.Carried != "" {
				at = e.Carried + ", the same diff"
			}
			return nil, fail(exitRefused, "refused: %s fails at t:%s; fix it, commit and run bmesh land again", e.Kind, at)
		}
	}
	return entries, nil
}

// carry puts a kind's model results at its source's tree, which read the
// diff the rebase left unchanged and recorded no patch-id, on the line for
// tree as carried entries: each is stored again with the patch-id of the
// diff it read (the fork point to the source's head), so done matches it as
// it matches any carried entry. A result with no model has nothing for done
// to match and stays off the line.
func (l *lander) carry(ctx context.Context, src *source, tree string) ([]spec.Entry, error) {
	var out []spec.Entry
	patch := ""
	for _, r := range src.newest {
		if r.Model == "" || !r.OK || r.PatchID != "" {
			continue
		}
		if patch == "" {
			var err error
			if patch, err = checkout.PatchID(ctx, l.top, src.base, src.head); err != nil {
				return nil, err
			}
			if patch == "" {
				return nil, nil
			}
		}
		r.PatchID = patch
		if err := mesh.PutResult(ctx, l.a.client, r); err != nil {
			return nil, err
		}
		out = append(out, spec.Entry{Kind: r.Kind, OK: true, Tree8: tree[:8], Model: r.Model, Carried: src.tree[:8]})
	}
	return out, nil
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
