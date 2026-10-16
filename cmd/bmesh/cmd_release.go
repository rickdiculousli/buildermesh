package main

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

const releaseUsage = "usage: bmesh release <repo> [--minor|--patch] [--dry-run]"

// firstRelease is the version a repo with no release tag gets.
const firstRelease = "v0.1.0"

// releaseRE is a release tag: vX.Y.Z.
var releaseRE = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)

// release is a release tag's version.
type release [3]int

func parseRelease(tag string) (release, bool) {
	m := releaseRE.FindStringSubmatch(tag)
	if m == nil {
		return release{}, false
	}
	var r release
	for i := range r {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return release{}, false
		}
		r[i] = n
	}
	return r, true
}

func (r release) String() string { return fmt.Sprintf("v%d.%d.%d", r[0], r[1], r[2]) }

// next is the release after r: the minor version up (patch 0) when minor,
// else the patch version up.
func (r release) next(minor bool) release {
	if minor {
		return release{r[0], r[1] + 1, 0}
	}
	return release{r[0], r[1], r[2] + 1}
}

// released is what a release prints with --json.
type released struct {
	Repo   string   `json:"repo"`
	Tag    string   `json:"tag"`
	Commit string   `json:"commit"`
	Specs  []string `json:"specs"`
	DryRun bool     `json:"dry_run,omitempty"`
	Pushed bool     `json:"pushed"`
}

// cmdRelease tags the repo's code head on main (codeHead, read from
// origin/main after a fetch of it and its tags) as the next vX.Y.Z, an
// annotated tag listing the specs landed since the last release, and
// pushes the tag. The first release is v0.1.0; after it the minor version
// goes up when a spec landed since is a feature, else the patch version,
// unless --minor or --patch says which. A release already on the code
// head is reported and left; it refuses (exit 3) when only specs changed
// since a release on another commit, while a lead of the repo is still working or a family of
// the repo has feedback pending (heldBy, with no family of its own), and
// while verify at the code head isn't ok (verifyMain). --dry-run prints
// the tag and its message and tags nothing.
func (a *app) cmdRelease(args []string) error {
	fs := a.flags("release")
	minor := fs.Bool("minor", false, "bump the minor version, whatever kinds the specs are")
	patch := fs.Bool("patch", false, "bump the patch version, whatever kinds the specs are")
	dry := fs.Bool("dry-run", false, "print the tag and its message; tag and push nothing")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || (*minor && *patch) {
		return fail(exitUsage, releaseUsage)
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	name, repoPath, err := a.cfg.RepoPath(pos[0])
	if err != nil {
		return fail(exitUsage, "%v", err)
	}
	row, ok := a.cfg.Repos[name]
	if !ok {
		return fail(exitUsage, "%s is not in repos.toml; a release needs its row (verify, remote)", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	ref, origin := "main", hasOrigin(ctx, repoPath)
	if origin {
		if _, err := gitIn(ctx, repoPath, "fetch", "--quiet", "--tags", "origin"); err != nil {
			return err
		}
		ref = "origin/main"
	}
	head, err := a.codeHead(ctx, repoPath, ref)
	if err != nil {
		return err
	}
	last, err := lastRelease(ctx, repoPath, ref)
	if err != nil {
		return err
	}
	revs := ref
	if last != "" {
		revs = last + ".." + ref
		code, err := gitIn(ctx, repoPath, "rev-list", "-1", revs, "--", ".", ":(exclude)"+a.specDir())
		if err != nil {
			return err
		}
		if code == "" {
			tagged, err := gitIn(ctx, repoPath, "rev-parse", "--verify", last+"^{commit}")
			if err != nil {
				return err
			}
			if tagged == head {
				fmt.Fprintf(a.stdout, "already released %s %s at %s\n", row.Name, last, head[:8])
				return nil
			}
			return fail(exitRefused, "nothing to release since %s: only specs changed", last)
		}
	}
	ids, err := selfupdate.Landed(repoPath, revs)
	if err != nil {
		return err
	}
	specs, err := spec.ScanRef(repoPath, a.cfg.Spec.Glob, ref)
	if err != nil {
		return ledgerFail(err)
	}
	// A release tags the code head, so the `done at` commit of a spec it
	// carries comes after the tag: a spec merged in the last release's
	// history is in that release, not this one.
	landed := make([]*spec.Spec, 0, len(ids))
	ids = slices.DeleteFunc(ids, func(id string) bool {
		n, _ := strconv.Atoi(id)
		s, ok := spec.Find(specs, n)
		if !ok {
			return false
		}
		if m := s.Fields["merged"]; last != "" && m != "" {
			if _, err := gitIn(ctx, repoPath, "merge-base", "--is-ancestor", m, last); err == nil {
				return true
			}
		}
		landed = append(landed, s)
		return false
	})
	tag := firstRelease
	if r, ok := parseRelease(last); ok {
		feature := slices.ContainsFunc(landed, func(s *spec.Spec) bool { return s.Fields["kind"] == "feature" })
		tag = r.next(*minor || (feature && !*patch)).String()
	}
	if err := a.connect(ctx, "release"); err != nil {
		return err
	}
	hold, err := a.heldBy(ctx, row.Name, "")
	if err != nil {
		return err
	}
	if hold != nil {
		return fail(exitRefused, "%s", hold.line())
	}
	if err := a.verifyMain(ctx, row, repoPath, head, a.stderr, false, "release once main is green"); err != nil {
		return err
	}
	msg := releaseMessage(row.Name, tag, last, landed)
	out := released{Repo: row.Name, Tag: tag, Commit: head, Specs: ids, DryRun: *dry}
	if out.Specs == nil {
		out.Specs = []string{}
	}
	if *dry {
		if a.json {
			return a.printJSON(out)
		}
		fmt.Fprintf(a.stdout, "would release %s %s at %s (%s); --dry-run tags nothing\n\n%s\n", row.Name, tag, head[:8], specList(ids), msg)
		return nil
	}
	if _, err := gitIn(ctx, repoPath, "tag", "--annotate", "--message", msg, "--end-of-options", tag, head); err != nil {
		return err
	}
	if origin {
		if _, err := gitIn(ctx, repoPath, "push", "--quiet", "origin", "refs/tags/"+tag); err != nil {
			// A tag origin doesn't have isn't a release; the next run
			// starts again.
			_, _ = gitIn(context.WithoutCancel(ctx), repoPath, "tag", "--delete", tag)
			return fmt.Errorf("pushing %s: %v", tag, err)
		}
		out.Pushed = true
	}
	if a.json {
		return a.printJSON(out)
	}
	pushed := ""
	if !origin {
		pushed = "; no origin, so it is local only"
	}
	fmt.Fprintf(a.stdout, "released %s %s at %s (%s)%s\n", row.Name, tag, head[:8], specList(ids), pushed)
	return nil
}

// lastRelease is the highest release tag in ref's history, "" when it has
// none.
func lastRelease(ctx context.Context, repoPath, ref string) (string, error) {
	tags, err := gitIn(ctx, repoPath, "tag", "--list", "v*", "--merged", ref)
	if err != nil {
		return "", err
	}
	var last string
	var top release
	for _, t := range strings.Fields(tags) {
		if r, ok := parseRelease(t); ok && (last == "" || slices.Compare(r[:], top[:]) > 0) {
			last, top = t, r
		}
	}
	return last, nil
}

// releaseMessage is the tag's message: `<repo> <tag>`, then one line per
// spec landed since last, `NNN <title>`.
func releaseMessage(repo, tag, last string, landed []*spec.Spec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n\n", repo, tag)
	if len(landed) == 0 {
		if last == "" {
			b.WriteString("no specs landed yet\n")
		} else {
			fmt.Fprintf(&b, "no specs landed since %s\n", last)
		}
	}
	for _, s := range landed {
		fmt.Fprintf(&b, "%s %s\n", s.IDString(), s.Title())
	}
	return strings.TrimRight(b.String(), "\n")
}

// specList is the ids joined for a line, or `no specs`.
func specList(ids []string) string {
	if len(ids) == 0 {
		return "no specs"
	}
	return strings.Join(ids, ", ")
}
