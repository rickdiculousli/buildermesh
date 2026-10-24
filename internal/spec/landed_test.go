package spec

import (
	"context"
	"reflect"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// A commit's trailers name its landing; an old record commit's subject
// does; the range lists each id once, oldest first; LandingOf finds the
// newest landing of an id on main.
func TestLandings(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	commit := func(msg string) string {
		t.Helper()
		meshtest.Git(t, repo, "commit", "--allow-empty", "-qm", msg)
		return revParse(t, repo, "HEAD")
	}
	base := commit("base")
	work := commit("old work")
	commit("011: done at " + work[:8])
	a := commit("the work\n\nCo-Authored-By: x <x@x>\nSpec: 12\nVerified: verify ok t:3f2a9c1e")
	commit("unrelated\n\nSpec: twelve, for once")
	commit("again\n\nSpec: 012\nVerified: verify ok t:00000000")

	l, err := ReadLanded(ctx, repo, a)
	if err != nil || l.Commit != a || l.Spec != "012" || l.Verified != "verify ok t:3f2a9c1e" || l.Legacy != "" {
		t.Fatalf("%+v %v", l, err)
	}
	if l, _ := ReadLanded(ctx, repo, a+"~1"); l.Spec != "" || l.Legacy != "011" || l.LegacyAt != work[:8] {
		t.Fatalf("legacy %+v", l)
	}
	got, err := Landings(ctx, repo, base+"..HEAD")
	want := []Landing{{ID: "011", At: work[:8], Legacy: true}, {ID: "012", At: a}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v %v", got, err)
	}
	meshtest.Git(t, repo, "push", "-q", "origin", "main")
	meshtest.Git(t, repo, "fetch", "-q", "origin")
	if l, ok, err := LandingOf(ctx, repo, "12"); err != nil || !ok || l.Commit != revParse(t, repo, "HEAD") || l.Verified != "verify ok t:00000000" {
		t.Fatalf("landing of: %+v %v %v", l, ok, err)
	}
	if _, ok, err := LandingOf(ctx, repo, "013"); err != nil || ok {
		t.Fatalf("none: %v %v", ok, err)
	}
	if on, ref, err := OnMain(ctx, repo, a, false); err != nil || !on || ref != "origin/main" {
		t.Fatalf("on main: %v %q %v", on, ref, err)
	}
}
