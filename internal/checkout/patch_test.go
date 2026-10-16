package checkout

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

func TestBaseFromText(t *testing.T) {
	for text, want := range map[string]string{
		"base: 0123abcd":                     "0123abcd",
		"look closely\nbase:0123abcdef456\n": "0123abcdef456",
		"the database: 0123abcd":             "",
		"base: 0123ab":                       "",
		"base: main":                         "",
		"":                                   "",
	} {
		if got := BaseFromText(text); got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
}

// The same change made on two bases has one patch-id; a changed hunk has
// another; an empty diff has none.
func TestPatchIDFollowsTheDiffNotTheBase(t *testing.T) {
	ctx := context.Background()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "init", "-q", "-b", "main")
	base1 := meshtest.CommitFiles(t, repo, map[string]string{"a.txt": "one\ntwo\nthree\nfour\nfive\nsix\nseven\n"})
	work1 := meshtest.CommitFiles(t, repo, map[string]string{"a.txt": "ONE\ntwo\nthree\nfour\nfive\nsix\nseven\n"})
	meshtest.Git(t, repo, "checkout", "-q", base1)
	base2 := meshtest.CommitFiles(t, repo, map[string]string{"c.txt": "c\n"})
	work2 := meshtest.CommitFiles(t, repo, map[string]string{"a.txt": "ONE\ntwo\nthree\nfour\nfive\nsix\nseven\n"})
	work3 := meshtest.CommitFiles(t, repo, map[string]string{"a.txt": "ONE\ntwo\nthree\nfour\nfive\nsix\nSEVEN\n"})

	p1, err := PatchID(ctx, repo, base1, work1)
	if err != nil || p1 == "" {
		t.Fatalf("p1 %q %v", p1, err)
	}
	if p2, err := PatchID(ctx, repo, base2, work2); err != nil || p2 != p1 {
		t.Fatalf("same diff on a new base: %q %v, want %q", p2, err, p1)
	}
	if p3, err := PatchID(ctx, repo, base2, work3); err != nil || p3 == p1 {
		t.Fatalf("changed hunk: %q %v", p3, err)
	}
	// Whitespace is part of the diff: a re-indented line is another one.
	work4 := meshtest.CommitFiles(t, repo, map[string]string{"a.txt": "ONE\ntwo\nthree\nfour\nfive\nsix\n  SEVEN\n"})
	p3, _ := PatchID(ctx, repo, base2, work3)
	if p4, err := PatchID(ctx, repo, base2, work4); err != nil || p4 == p3 {
		t.Fatalf("re-indented hunk: %q %v", p4, err)
	}
	if p, err := PatchID(ctx, repo, work1, work1); err != nil || p != "" {
		t.Fatalf("empty diff: %q %v", p, err)
	}
	if p, err := ReviewedPatch(ctx, repo, "please review\nbase: "+base2[:10], work2); err != nil || p != p1 {
		t.Fatalf("reviewed patch: %q %v", p, err)
	}
	if p, err := ReviewedPatch(ctx, repo, "no base here", work2); err != nil || p != "" {
		t.Fatalf("no base: %q %v", p, err)
	}
	if _, err := ReviewedPatch(ctx, repo, "base: 0000000000", work2); err == nil {
		t.Fatal("unknown base: no error")
	}
	// work2's diff from base2 is p1; work3 on top of it lands another diff.
	if ok, err := LandedFrom(ctx, repo, work2, p1); err != nil || !ok {
		t.Fatalf("landed from base2: %v %v", ok, err)
	}
	if ok, err := LandedFrom(ctx, repo, work3, p1); err != nil || ok {
		t.Fatalf("work3 landed p1: %v %v", ok, err)
	}
	if ok, err := LandedFrom(ctx, repo, work2, ""); err != nil || ok {
		t.Fatalf("empty patch: %v %v", ok, err)
	}
}
