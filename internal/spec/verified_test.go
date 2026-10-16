package spec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

func TestParseFormatVerifiedRoundTrip(t *testing.T) {
	line := "verify ok t:3f2a9c1e; review ok t:3f2a9c1e m:claude-opus-5-5"
	got, err := ParseVerified(line)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Kind: "verify", OK: true, Tree8: "3f2a9c1e"},
		{Kind: "review", OK: true, Tree8: "3f2a9c1e", Model: "claude-opus-5-5"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parse: %+v", got)
	}
	if s := FormatVerified(got); s != line {
		t.Fatalf("format: %q", s)
	}
	if got, err := ParseVerified("  game fail t:00ff00ff  "); err != nil || len(got) != 1 || got[0].OK || got[0].Kind != "game" {
		t.Fatalf("fail entry: %+v %v", got, err)
	}
	carried := "review ok t:3f2a9c1e m:claude-opus-5-5 c:0011aabb; game ok t:3f2a9c1e c:0011aabb"
	if got, err := ParseVerified(carried); err != nil || len(got) != 2 ||
		got[0] != (Entry{Kind: "review", OK: true, Tree8: "3f2a9c1e", Model: "claude-opus-5-5", Carried: "0011aabb"}) ||
		got[1] != (Entry{Kind: "game", OK: true, Tree8: "3f2a9c1e", Carried: "0011aabb"}) {
		t.Fatalf("carried: %+v %v", got, err)
	} else if s := FormatVerified(got); s != carried {
		t.Fatalf("format carried: %q", s)
	}
	if got, err := ParseVerified("   "); got != nil || err != nil {
		t.Fatalf("empty: %+v %v", got, err)
	}
	for _, bad := range []string{
		"verify ok",
		"verify maybe t:3f2a9c1e",
		"verify ok t:3f2a9c1",
		"verify ok t:3F2A9C1E",
		"verify ok 3f2a9c1e",
		"Verify ok t:3f2a9c1e",
		"review ok t:3f2a9c1e m:",
		"review ok t:3f2a9c1e x:y",
		"review ok t:3f2a9c1e m:a b",
		"verify ok t:3f2a9c1e; ; review ok t:3f2a9c1e",
		"review ok t:3f2a9c1e c:0011aabb m:claude-opus-5-5",
		"review ok t:3f2a9c1e m:claude-opus-5-5 c:",
		"review ok t:3f2a9c1e m:claude-opus-5-5 c:0011AABB",
		"review ok t:3f2a9c1e m:claude-opus-5-5 c:0011aabb c:0011aabb",
		"review ok t:3f2a9c1e m:a m:b",
	} {
		if _, err := ParseVerified(bad); err == nil {
			t.Fatalf("%q: no error", bad)
		}
	}
	if _, err := ParseVerified("verify ok t:3f2a9c1e; review maybe t:3f2a9c1e"); err == nil || !strings.Contains(err.Error(), `"review maybe t:3f2a9c1e"`) {
		t.Fatalf("error names the entry: %v", err)
	}
}

func headTree(t *testing.T, repo string) string {
	t.Helper()
	tree, err := Tree(repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestComposeFromResultsAtHead(t *testing.T) {
	url := meshtest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	c, err := mesh.Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "r")
	meshtest.Git(t, filepath.Dir(repo), "init", "-b", "main", repo)
	commitSpec(t, repo, "building", "")
	tree := headTree(t, repo)

	if got, err := ComposeVerified(ctx, c, repo); err != nil || len(got) != 0 {
		t.Fatalf("no results: %+v %v", got, err)
	}
	for _, r := range []mesh.Result{
		{Kind: "verify", Tree: tree, OK: true, At: "2026-10-01T10:00:00Z"},
		{Kind: "review", Tree: tree, OK: true, Model: "claude-opus-5-5", At: "2026-10-02T10:00:00Z"},
		{Kind: "review", Tree: tree, OK: false, Model: "claude-opus-5-5", Args: []string{"x"}, At: "2026-10-01T10:00:00Z"},
		{Kind: "verify", Tree: strings.Repeat("0", 40), OK: false, At: "2026-10-03T10:00:00Z"},
	} {
		r.By = "h"
		if err := mesh.PutResult(ctx, c, r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ComposeVerified(ctx, c, repo)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Kind: "review", OK: true, Tree8: tree[:8], Model: "claude-opus-5-5"},
		{Kind: "verify", OK: true, Tree8: tree[:8]},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	// A newer failure for the same kind and model wins over the older pass.
	if err := mesh.PutResult(ctx, c, mesh.Result{Kind: "review", Tree: tree, Model: "claude-opus-5-5", Args: []string{"y"}, At: "2026-10-04T10:00:00Z", By: "h"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := ComposeVerified(ctx, c, repo); len(got) != 2 || got[0].OK {
		t.Fatalf("newest wins: %+v", got)
	}
}

// A model's result recorded with the patch-id of the diff it read carries
// to a tree a rebase made from the same diff, and to no other.
func TestComposeCarriesAReviewOfTheSameDiff(t *testing.T) {
	url := meshtest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	c, err := mesh.Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "r")
	meshtest.Git(t, filepath.Dir(repo), "init", "-b", "main", repo)
	meshtest.CommitFiles(t, repo, map[string]string{"a.txt": "one\ntwo\nthree\nfour\nfive\nsix\nseven\n"})
	meshtest.Git(t, repo, "checkout", "-q", "-b", "lead")
	reviewed := meshtest.CommitFiles(t, repo, map[string]string{"a.txt": "ONE\ntwo\nthree\nfour\nfive\nsix\nseven\n"})
	reviewedTree := headTree(t, repo)
	patch, err := checkout.PatchID(ctx, repo, "main", reviewed)
	if err != nil || patch == "" {
		t.Fatalf("patch %q %v", patch, err)
	}
	put := func(r mesh.Result) {
		t.Helper()
		r.By = "h"
		if err := mesh.PutResult(ctx, c, r); err != nil {
			t.Fatal(err)
		}
	}
	put(mesh.Result{Kind: "review", Tree: reviewedTree, Commit: reviewed, OK: true, Model: "claude-opus-5-5", PatchID: patch, At: "2026-10-01T10:00:00Z"})
	// A command kind's result is about its tree: it never carries.
	put(mesh.Result{Kind: "verify", Tree: reviewedTree, Commit: reviewed, OK: true, PatchID: patch, At: "2026-10-01T10:00:00Z"})

	// Main moves in another file; the lead rebases: a new tree, the same diff.
	meshtest.Git(t, repo, "checkout", "-q", "main")
	meshtest.CommitFiles(t, repo, map[string]string{"c.txt": "c\n"})
	meshtest.Git(t, repo, "checkout", "-q", "lead")
	meshtest.Git(t, repo, "rebase", "-q", "main")
	tree := headTree(t, repo)
	if tree == reviewedTree {
		t.Fatal("the rebase left the tree")
	}
	put(mesh.Result{Kind: "verify", Tree: tree, OK: true, At: "2026-10-02T10:00:00Z"})
	got, err := ComposeVerified(ctx, c, repo)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Kind: "review", OK: true, Tree8: tree[:8], Model: "claude-opus-5-5", Carried: reviewedTree[:8]},
		{Kind: "verify", OK: true, Tree8: tree[:8]},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("same diff: %+v", got)
	}

	// A result at HEAD's own tree wins over a carried one.
	put(mesh.Result{Kind: "review", Tree: tree, OK: true, Model: "claude-opus-5-5", At: "2026-09-01T10:00:00Z"})
	if got, _ := ComposeVerified(ctx, c, repo); len(got) != 2 || got[0].Carried != "" || got[0].Tree8 != tree[:8] {
		t.Fatalf("own tree wins: %+v", got)
	}

	// A changed hunk is another diff: nothing carries.
	meshtest.CommitFiles(t, repo, map[string]string{"a.txt": "ONE\ntwo\nthree\nfour\nfive\nsix\nSEVEN\n"})
	if got, _ := ComposeVerified(ctx, c, repo); len(got) != 0 {
		t.Fatalf("changed hunk: %+v", got)
	}
	// On main there is no diff to carry a review to.
	meshtest.Git(t, repo, "checkout", "-q", "main")
	if got, _ := ComposeVerified(ctx, c, repo); len(got) != 0 {
		t.Fatalf("on main: %+v", got)
	}
}

func TestWriteVerifiedInPlaceAndAppended(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "012-thing.md")
	entries := []Entry{{Kind: "verify", OK: true, Tree8: "3f2a9c1e"}}

	in := "---\nid: 12\n# verified: not this one\nverified: old\nstatus: done\n---\nbody\nverified: body line\n"
	if err := os.WriteFile(p, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteVerified(p, entries); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if want := strings.Replace(in, "verified: old\n", "verified: verify ok t:3f2a9c1e\n", 1); string(b) != want {
		t.Fatalf("in place:\n%s", b)
	}

	in = "---\nid: 12\nstatus: done\n\n---\n\nbody\n"
	if err := os.WriteFile(p, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteVerified(p, append(entries, Entry{Kind: "review", Tree8: "3f2a9c1e", Model: "m"})); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if want := "---\nid: 12\nstatus: done\n\nverified: verify ok t:3f2a9c1e; review fail t:3f2a9c1e m:m\n---\n\nbody\n"; string(b) != want {
		t.Fatalf("appended:\n%s", b)
	}

	if err := WriteVerified(p, nil); !errors.Is(err, ErrNoResults) {
		t.Fatalf("empty: %v", err)
	}
	if err := os.WriteFile(p, []byte("---\nid: 12\nbare\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteVerified(p, entries); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("malformed: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "---\nid: 12\nbare\n---\n" {
		t.Fatalf("malformed file changed: %q", b)
	}
}

func TestCheckDoneParsesVerified(t *testing.T) {
	repo := newRepo(t)
	dir := filepath.Join(repo, "docs", "proposals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "commit", "--allow-empty", "-m", "work")
	merged := revParse(t, repo, "HEAD")
	write := func(verified string) {
		text := "---\nid: 12\nstatus: done\nmerged: " + merged + "\nverified: " + verified + "\n---\n"
		if err := os.WriteFile(filepath.Join(dir, "012-thing.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		meshtest.Git(t, repo, "add", "-A")
		meshtest.Git(t, repo, "commit", "-m", "spec")
		meshtest.Git(t, repo, "push", "origin", "main")
	}
	write("verify ok t:3f2a9c1e")
	d, err := CheckDone(repo, glob, "012")
	if err != nil || !d.OK || len(d.Verified) != 1 || d.Verified[0].Tree8 != "3f2a9c1e" {
		t.Fatalf("%+v %v", d, err)
	}
	write("verify sure t:3f2a9c1e")
	if _, err := CheckDone(repo, glob, "012"); err == nil || !strings.Contains(err.Error(), "verified") {
		t.Fatalf("bad verified: %v", err)
	}
}

// An entry that would not read back as itself (a model with ";" or a
// space) is refused unwritten.
func TestWriteVerifiedRefusesUnparsableLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "012-thing.md")
	in := "---\nid: 12\n---\n"
	if err := os.WriteFile(p, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"m; verify ok t:3f2a9c1e", "m x"} {
		err := WriteVerified(p, []Entry{{Kind: "review", OK: true, Tree8: "3f2a9c1e", Model: model}})
		if err == nil || !strings.Contains(err.Error(), "left unchanged") {
			t.Fatalf("%q: err %v", model, err)
		}
		if b, _ := os.ReadFile(p); string(b) != in {
			t.Fatalf("%q: file changed: %q", model, b)
		}
	}
}
