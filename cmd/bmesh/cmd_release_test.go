package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

// landRelease lands spec id of kind on the demo repo's main as a lead would:
// a code commit, then the spec's `NNN: done at` commit, both pushed. It
// returns the code commit.
func landRelease(t *testing.T, repo, id, kind string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "code-"+id+".txt"), []byte(id), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-qm", "build "+id)
	code := gitOut(t, "-C", repo, "rev-parse", "HEAD")
	text := "---\nid: " + id + "\ntitle: Spec " + id + "\nkind: " + kind + "\nsize: S\nstatus: done\nmerged: " + code + "\n---\n\n## Proposal\nx\n"
	if err := os.WriteFile(filepath.Join(repo, "docs", "proposals", id+"-spec.md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-qm", id+": done at "+code[:8])
	meshtest.Git(t, repo, "push", "-q", "origin", "main")
	return code
}

func TestRelease(t *testing.T) {
	c, repo, _ := proposeSetup(t, "")
	ctx := t.Context()
	t.Setenv("VERIFY_LOG", filepath.Join(t.TempDir(), "verify.log"))
	originTags := func() string {
		return gitOut(t, "-C", repo, "ls-remote", "--tags", "origin")
	}
	verifyKind(t, repo, "echo all good", "Add the verify kind")
	code := landRelease(t, repo, "020", "feature")

	if _, errs, code := runCLI(t, nil, "release", "demo", "--minor", "--patch"); code != exitUsage {
		t.Fatalf("--minor --patch: %d %q", code, errs)
	}

	// An untagged repo gets v0.1.0; --dry-run tags and pushes nothing.
	out, errs, exit := runCLI(t, nil, "release", "demo", "--dry-run")
	if exit != 0 || !strings.HasPrefix(out, "would release demo v0.1.0 at "+code[:8]+" (020); --dry-run tags nothing\n\ndemo v0.1.0\n\n020 Spec 020\n") {
		t.Fatalf("dry run: %d %q %q", exit, out, errs)
	}
	if tags := gitOut(t, "-C", repo, "tag", "--list"); tags != "" || originTags() != "" {
		t.Fatalf("dry run tagged: %q %q", tags, originTags())
	}

	// A lead still working, then a family's pending feedback, hold it.
	putNode(t, c, ctx, mesh.Node{Path: "demo-030", Repo: "demo", Host: "elsewhere", Family: "demo-build-4"})
	if _, err := mesh.WriteState(ctx, c, "demo-030", mesh.State{State: "working", By: "node"}); err != nil {
		t.Fatal(err)
	}
	if _, errs, exit := runCLI(t, nil, "release", "demo"); exit != exitRefused ||
		errs != "bmesh: held: demo-build-4 is still running (demo-030 working); watch it with bmesh watch --nodes demo-030\n" {
		t.Fatalf("working lead: %d %q", exit, errs)
	}
	meshtest.ForceState(t, c, "demo-030", mesh.State{State: "done", By: "node"})
	if _, err := mesh.PublishFeedback(ctx, c, mesh.Feedback{From: "demo-030", Family: "demo-build-4", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, errs, exit := runCLI(t, nil, "release", "demo"); exit != exitRefused ||
		errs != "bmesh: held: demo-build-4 has 1 feedback pending; drain it with bmesh feedback list/drain demo-build-4\n" {
		t.Fatalf("pending feedback: %d %q", exit, errs)
	}
	if _, errs, exit := runCLI(t, nil, "feedback", "drain", "demo-build-4"); exit != 0 {
		t.Fatalf("drain: %d %q", exit, errs)
	}
	if originTags() != "" {
		t.Fatal("a held release tagged")
	}

	// The release: an annotated tag on the code head, listing the specs,
	// pushed to origin.
	out, errs, exit = runCLI(t, nil, "release", "demo")
	if exit != 0 || out != "released demo v0.1.0 at "+code[:8]+" (020)\n" {
		t.Fatalf("release: %d %q %q", exit, out, errs)
	}
	if got := originTags(); !strings.Contains(got, "refs/tags/v0.1.0^{}") || gitOut(t, "-C", repo, "rev-parse", "v0.1.0^{commit}") != code {
		t.Fatalf("origin tags %q", got)
	}
	if typ := gitOut(t, "-C", repo, "cat-file", "-t", "v0.1.0"); typ != "tag" {
		t.Fatalf("v0.1.0 is a %s", typ)
	}
	if msg := gitOut(t, "-C", repo, "tag", "--list", "--format=%(contents)", "v0.1.0"); msg != "demo v0.1.0\n\n020 Spec 020" {
		t.Fatalf("message %q", msg)
	}

	// Run again, with 020's done commit after the tag: the release on the
	// code head is reported, not cut again.
	if out, errs, exit := runCLI(t, nil, "release", "demo"); exit != 0 || out != "already released demo v0.1.0 at "+code[:8]+"\n" {
		t.Fatalf("again: %d %q %q", exit, out, errs)
	}

	// A refinement bumps the patch version, a feature the minor; --patch
	// and --minor say otherwise.
	landRelease(t, repo, "021", "refinement")
	if out, errs, exit := runCLI(t, nil, "release", "demo"); exit != 0 || !strings.HasPrefix(out, "released demo v0.1.1 at ") || !strings.HasSuffix(out, " (021)\n") {
		t.Fatalf("refinement: %d %q %q", exit, out, errs)
	}
	landRelease(t, repo, "022", "feature")
	landRelease(t, repo, "023", "refinement")
	if out, _, exit := runCLI(t, nil, "release", "demo", "--dry-run", "--patch"); exit != 0 || !strings.HasPrefix(out, "would release demo v0.1.2 at ") {
		t.Fatalf("--patch: %d %q", exit, out)
	}
	if out, errs, exit := runCLI(t, nil, "release", "demo"); exit != 0 || !strings.HasPrefix(out, "released demo v0.2.0 at ") || !strings.HasSuffix(out, " (022, 023)\n") {
		t.Fatalf("feature: %d %q %q", exit, out, errs)
	}
	landRelease(t, repo, "024", "refinement")
	if out, _, exit := runCLI(t, nil, "release", "demo", "--dry-run", "--minor"); exit != 0 || !strings.HasPrefix(out, "would release demo v0.3.0 at ") {
		t.Fatalf("--minor: %d %q", exit, out)
	}

	// A red code head refuses.
	red := verifyKind(t, repo, "echo '--- FAIL: TestRed'\nexit 1", "Break it")
	if _, errs, exit := runCLI(t, nil, "release", "demo"); exit != exitRefused || !strings.Contains(errs, "\nbmesh: main is red at "+red[:8]+" Break it: verify fail t:") ||
		!strings.HasSuffix(errs, "; release once main is green\n--- FAIL: TestRed\n") {
		t.Fatalf("red: %d %q", exit, errs)
	}
	if strings.Contains(originTags(), "v0.2.1") {
		t.Fatal("a red main was released")
	}

	// Fixed by hand, with no spec: a patch, which tags main's head itself,
	// so a second run reports it.
	fixed := verifyKind(t, repo, "echo fixed", "Fix it")
	if out, errs, exit := runCLI(t, nil, "release", "demo"); exit != 0 || out != "released demo v0.2.1 at "+fixed[:8]+" (024)\n" {
		t.Fatalf("fixed: %d %q %q", exit, out, errs)
	}
	if out, errs, exit := runCLI(t, nil, "release", "demo"); exit != 0 || out != "already released demo v0.2.1 at "+fixed[:8]+"\n" {
		t.Fatalf("again: %d %q %q", exit, out, errs)
	}

	// The binary's stamp: the tag on it, then +N past it.
	if got := selfupdate.Describe(repo, fixed); got != "v0.2.1" {
		t.Fatalf("describe at the tag: %q", got)
	}
	past := landRelease(t, repo, "025", "refinement")
	if got := selfupdate.Describe(repo, "main"); got != "v0.2.1+2" {
		t.Fatalf("describe past the tag: %q (code %s)", got, past[:8])
	}
	v := selfupdate.Version{Revision: past, Time: "2026-10-16T22:38:04Z", Release: "v0.2.1+2"}
	if v.String() != "v0.2.1+2 ("+past[:8]+" 2026-10-16T22:38:04Z)" {
		t.Fatalf("version %q", v.String())
	}

	// A tag cut by hand on a ledger commit, then only specs since it:
	// nothing to release.
	if err := os.WriteFile(filepath.Join(repo, "docs", "proposals", "026-spec.md"), []byte("---\nid: 026\ntitle: Spec 026\nkind: feature\nsize: S\nstatus: proposed\n---\n\n## Proposal\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-qm", "Propose 026")
	meshtest.Git(t, repo, "tag", "-a", "-m", "by hand", "v0.2.2")
	meshtest.Git(t, repo, "commit", "-q", "--allow-empty", "-m", "Review: 026 accepted")
	meshtest.Git(t, repo, "push", "-q", "origin", "main", "v0.2.2")
	if _, errs, exit := runCLI(t, nil, "release", "demo"); exit != exitRefused || errs != "bmesh: nothing to release since v0.2.2: only specs changed\n" {
		t.Fatalf("spec-only: %d %q", exit, errs)
	}
}
