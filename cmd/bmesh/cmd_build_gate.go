package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/kind"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

// redTail is how many lines of a failing verify a refused build prints.
const redTail = 30

// buildHold is what holds a build: a build family of the repo's, not the
// build's own, with a lead still running or feedback not yet drained.
type buildHold struct {
	who  string // the family, or the lead's path when it has none
	verb string // "is" (running) or "has" (feedback)
	why  string
}

// line is the refusal a single build prints: held: <who> is|has <why>.
func (h buildHold) line() string { return fmt.Sprintf("held: %s %s %s", h.who, h.verb, h.why) }

// heldBy reads the repo's leads on every host and the mesh's pending
// feedback and names what holds a build in family: the first other family
// (by name) with a lead of repo that is not done or blocked (a lead with
// no family holds as itself), else the first other family of the repo's
// (one a lead of repo is in, or named <repo>-build-<N>) with feedback
// pending. Nil when nothing does. A release passes no family: every
// family is another.
func (a *app) heldBy(ctx context.Context, repo, family string) (*buildHold, error) {
	nodes, err := mesh.ListNodes(ctx, a.client)
	if err != nil {
		return nil, err
	}
	states, err := mesh.ReadAllStates(ctx, a.client)
	if err != nil {
		return nil, err
	}
	pending, err := mesh.PendingFeedback(ctx, a.client)
	if err != nil {
		return nil, err
	}
	running := map[string][]mesh.Node{}
	repoFamilies := map[string]bool{}
	for _, n := range nodes {
		if n.Role != "lead" || n.Repo != repo {
			continue
		}
		repoFamilies[n.Family] = true
		if s := states[n.Path].State; (family != "" && n.Family == family) || s == "done" || s == "blocked" {
			continue
		}
		who := cmp.Or(n.Family, n.Path)
		running[who] = append(running[who], n)
	}
	if len(running) > 0 {
		who := slices.Sorted(maps.Keys(running))[0]
		var what, paths []string
		for _, n := range running[who] {
			st := cmp.Or(states[n.Path].State, "idle")
			if n.Path != who {
				st = n.Path + " " + st
			}
			what = append(what, st)
			paths = append(paths, n.Path)
		}
		return &buildHold{who: who, verb: "is", why: fmt.Sprintf("still running (%s); watch it with bmesh watch --nodes %s",
			strings.Join(what, ", "), strings.Join(paths, ","))}, nil
	}
	base := mesh.RepoToken(repo)
	for _, p := range pending {
		if family != "" && p.Family == family {
			continue
		}
		if m := familyNRE.FindStringSubmatch(p.Family); !repoFamilies[p.Family] && (m == nil || m[1] != base) {
			continue
		}
		return &buildHold{who: p.Family, verb: "has", why: fmt.Sprintf("%d feedback pending; drain it with bmesh feedback list/drain %s",
			p.Pending, p.Family)}, nil
	}
	return nil, nil
}

// codeHead is the newest commit of ref that changes anything outside the
// spec directory (ref itself when only specs ever changed), so ledger
// commits don't count as code: the commit a build verifies and a release
// tags.
func (a *app) codeHead(repoPath, ref string) (string, error) {
	return selfupdate.CodeHead(repoPath, ref, a.specDir())
}

// specDir is the spec directory, as a git pathspec.
func (a *app) specDir() string { return filepath.ToSlash(spec.Dir(a.cfg.Spec.Glob)) }

// verifyMain checks sha, main's code head (codeHead), before a lead
// branches from it or a release tags it. A verify result recorded for its
// tree answers; else the repo's verify kind, as that commit defines it,
// runs inline in a bmesh worktree there and records, as land runs it. A
// repo with no verify kind says so and passes; a failing verify refuses
// (exit 3) with fix and the tail of its output. With force, a fail or a
// missing result is said and passed, and nothing runs. Each line goes to
// note.
func (a *app) verifyMain(ctx context.Context, row config.Repo, repoPath, sha string, note io.Writer, force bool, fix string) error {
	tree, err := gitIn(ctx, repoPath, "rev-parse", sha+"^{tree}")
	if err != nil {
		return err
	}
	subject, err := gitIn(ctx, repoPath, "log", "-1", "--format=%s", sha)
	if err != nil {
		return err
	}
	// A verify kind runs a command, so its result is keyed without a
	// model; the kind file is read only when nothing is recorded.
	model := ""
	r, recorded, err := mesh.GetResult(ctx, a.client, mesh.ResultKey("verify", tree, nil, model))
	if err != nil {
		return err
	}
	ok, output, how := r.OK, r.Output, "answered from the record"
	if !recorded {
		tag, err := shortHex()
		if err != nil {
			return err
		}
		branch := mesh.RepoToken(row.Name) + "-verify-" + tag
		wt := filepath.Join(a.cfg.Expand(a.cfg.Runner.Worktrees), row.Name, branch)
		if err := addWorktree(ctx, repoPath, wt, branch, sha); err != nil {
			return err
		}
		defer func() {
			if err := removeWorktree(context.WithoutCancel(ctx), repoPath, wt, branch, false); err != nil {
				fmt.Fprintf(a.stderr, "bmesh: %v\n", err)
			}
		}()
		kinds, err := kind.LoadDir(wt)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(kinds, func(k kind.Kind) bool { return k.Name == "verify" })
		if i < 0 {
			fmt.Fprintf(note, "verify: %s defines none\n", row.Name)
			return nil
		}
		k := kinds[i]
		if k.IsModel() {
			model = k.ModelsList()[0]
			if r, recorded, err = mesh.GetResult(ctx, a.client, mesh.ResultKey("verify", tree, nil, model)); err != nil {
				return err
			}
			ok, output = r.OK, r.Output
		}
		switch {
		case recorded:
		case force:
			fmt.Fprintf(note, "--force: past verify main %s: no result for t:%s\n", sha[:8], tree[:8])
			return nil
		default:
			fmt.Fprintf(note, "verify main %s: running at t:%s\n", sha[:8], tree[:8])
			if ok, output, how, err = a.runInline(ctx, k, wt, sha, tree, a.cfg.HostName(), model, "", "", nil, false); err != nil {
				return err
			}
		}
	}
	if ok {
		fmt.Fprintf(note, "verify main %s: ok t:%s (%s)\n", sha[:8], tree[:8], how)
		return nil
	}
	red := fmt.Sprintf("main is red at %s %s: verify fail t:%s (%s)", sha[:8], subject, tree[:8], how)
	if force {
		fmt.Fprintf(note, "--force: past %s\n", red)
		return nil
	}
	return fail(exitRefused, "%s; %s\n%s", red, fix, lastLines(output, redTail))
}

// lastLines is the last n lines of s, without a trailing newline.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return strings.Join(lines[max(len(lines)-n, 0):], "\n")
}
