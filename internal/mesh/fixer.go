package mesh

import (
	"regexp"
	"strconv"
)

// fixFamilyRE reads a fixer's path, which is also its family: <repo>-fix-<N>.
var fixFamilyRE = regexp.MustCompile(`^(.*)-fix-(\d+)$`)

// FixFamily splits a fixer's family <repo>-fix-<N> into its repo token
// and N; ok is false for any other name.
func FixFamily(f string) (base string, n int, ok bool) {
	m := fixFamilyRE.FindStringSubmatch(f)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}

// Fixers is what the registry, the states and the pending feedback say of
// one repo's fixers.
type Fixers struct {
	Live      Node  // a fixer neither done nor blocked; Path "" when none
	LiveState State // its state
	Last      Node  // the fixer with the highest N; Path "" when none
	LastState State // its state
	Next      int   // the next fixer's N: one past the highest <repo>-fix-<N> registered (as a path or a family) or holding feedback
}

// ReadFixers reads repo's fixers from nodes, their states and pending
// (every family holding feedback): the live one, the newest, and the next
// N, so a fixer's family is never joined by the next.
func ReadFixers(nodes []Node, states map[string]State, pending []FamilyPending, repo string) Fixers {
	var f Fixers
	base, last := RepoToken(repo), 0
	names := make([]string, 0, 2*len(nodes)+len(pending))
	for _, n := range nodes {
		names = append(names, n.Path, n.Family)
		if n.Role != "fixer" || n.Repo != repo {
			continue
		}
		s := states[n.Path]
		if s.State != "done" && s.State != "blocked" && f.Live.Path == "" {
			f.Live, f.LiveState = n, s
		}
		if _, k, ok := FixFamily(n.Path); ok && (f.Last.Path == "" || k > last) {
			f.Last, f.LastState, last = n, s, k
		}
	}
	for _, p := range pending {
		names = append(names, p.Family)
	}
	for _, name := range names {
		if b, k, ok := FixFamily(name); ok && b == base && k > f.Next {
			f.Next = k
		}
	}
	f.Next++
	return f
}
