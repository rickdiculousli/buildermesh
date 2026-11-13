package main

import (
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
)

// pickHolds takes an id, a repo or registry, or nothing, leaves expired
// holds to a named id, and refuses several when one is asked for.
func TestPickHolds(t *testing.T) {
	dir := t.TempDir()
	live := hosts.Stamp(time.Now().Add(time.Hour))
	for _, h := range []hosts.Held{
		{ID: "aaaa", Repo: "tools", Commits: 1, Job: hosts.Job{Expires: live}},
		{ID: "bbbb", Repo: "tools", Commits: 2, Job: hosts.Job{Expires: live}},
		{ID: "cccc", Repo: "app", Commits: 1, Job: hosts.Job{Expires: live}},
		{ID: "dddd", Kind: hosts.HeldRegistry, Commits: 1, Job: hosts.Job{By: "k16", Expires: live}},
		{ID: "eeee", Repo: "old", Commits: 1, Job: hosts.Job{Expires: hosts.Stamp(time.Now().Add(-time.Hour))}},
	} {
		if err := hosts.SaveHeld(dir, h); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(xs []hosts.Held) string {
		var s []string
		for _, x := range xs {
			s = append(s, x.ID)
		}
		return strings.Join(s, ",")
	}
	for _, tc := range []struct {
		pos  []string
		one  bool
		want string // the ids, or the refusal (its start, for a list)
	}{
		{nil, false, "aaaa,bbbb,cccc,dddd"},
		{[]string{"tools"}, false, "aaaa,bbbb"},
		{[]string{"old"}, false, "no hold old on this box (`bmesh hosts held` lists them)"},
		{[]string{"eeee"}, true, "eeee"},
		{[]string{"app"}, true, "cccc"},
		{[]string{"registry"}, true, "dddd"},
		{[]string{"bbbb"}, true, "bbbb"},
		{[]string{"tools"}, true, "tools names 2 holds: aaaa (tools +1 commit), bbbb (tools +2 commits); name one by its id"},
		{nil, true, "this box has 4 holds: "},
		{[]string{"nope"}, true, "no hold nope on this box (`bmesh hosts held` lists them)"},
	} {
		xs, err := pickHolds(dir, tc.pos, tc.one)
		got := ids(xs)
		if err != nil {
			got = err.Error()
		}
		if got != tc.want && !(err != nil && strings.HasSuffix(tc.want, ": ") && strings.HasPrefix(got, tc.want)) {
			t.Errorf("pickHolds(%v, %v) = %q, want %q", tc.pos, tc.one, got, tc.want)
		}
	}
	if _, err := pickHolds(t.TempDir(), nil, false); err == nil || err.Error() != "no holds on this box" {
		t.Errorf("no holds: %v", err)
	}
}
