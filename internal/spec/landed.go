package spec

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The trailers bmesh land writes on the commit it lands: the spec it
// lands and the verified line composed for the commit's tree.
const (
	SpecTrailer     = "Spec"
	VerifiedTrailer = "Verified"
)

// legacyDoneRE matches the subject of the record commit bmesh land wrote
// before trailers: `NNN: done at <sha8>`.
var legacyDoneRE = regexp.MustCompile(`^([0-9]{3,}): done at ([0-9a-f]{7,40})`)

// idRE is a Spec trailer's value: a spec id. Another value names no
// landing.
var idRE = regexp.MustCompile(`^[0-9]{1,6}$`)

// Landed is what one commit says of a landing. Spec is its Spec trailer's
// id as three digits and Verified its Verified trailer ("" when it has
// none, or a Spec trailer that is no id); Legacy is the id of a
// `NNN: done at <sha8>` subject, and LegacyAt the commit that subject
// names.
type Landed struct {
	Commit, Subject  string
	Spec, Verified   string
	Legacy, LegacyAt string
}

// Landing is one spec landed in a range of commits: its id and the commit
// it landed at (the trailered commit itself, or the merged commit an old
// `NNN: done at` subject names); Legacy marks the old kind.
type Landing struct {
	ID, At string
	Legacy bool
}

// ReadLanded reads rev's subject and trailers in repoPath.
func ReadLanded(ctx context.Context, repoPath, rev string) (Landed, error) {
	out, err := gitCtx(ctx, repoPath, "log", "-1", "--format="+landedFormat, "--end-of-options", rev+"^{commit}")
	if err != nil {
		return Landed{}, err
	}
	return parseLanded(strings.Trim(out, "\n\x1e")), nil
}

// Landings is the specs landed in revs (a git revision range, or a commit
// and its history), oldest first, each id once: a commit's Spec trailer
// names one, and so does an old `NNN: done at` subject.
func Landings(ctx context.Context, dir, revs string) ([]Landing, error) {
	out, err := gitCtx(ctx, dir, "log", "--reverse", "--format="+landedFormat, "--end-of-options", revs)
	if err != nil {
		return nil, err
	}
	var all []Landing
	seen := map[string]bool{}
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		l := parseLanded(rec)
		var x Landing
		switch {
		case l.Spec != "":
			x = Landing{ID: l.Spec, At: l.Commit}
		case l.Legacy != "":
			x = Landing{ID: l.Legacy, At: l.LegacyAt, Legacy: true}
		default:
			continue
		}
		if !seen[x.ID] {
			seen[x.ID] = true
			all = append(all, x)
		}
	}
	return all, nil
}

// landedFormat is one commit as parseLanded reads it: sha, subject and the
// two trailers' values, separated by \x1f, ended by \x1e.
const landedFormat = "%H%x1f%s%x1f%(trailers:key=" + SpecTrailer + ",valueonly,unfold,separator=%x1d)%x1f%(trailers:key=" + VerifiedTrailer + ",valueonly,unfold,separator=%x1d)%x1e"

func parseLanded(rec string) Landed {
	f := strings.Split(rec, "\x1f")
	for len(f) < 4 {
		f = append(f, "")
	}
	l := Landed{Commit: f[0], Subject: f[1], Verified: lastValue(f[3])}
	if v := lastValue(f[2]); idRE.MatchString(v) {
		l.Spec = NormID(v)
	}
	if m := legacyDoneRE.FindStringSubmatch(l.Subject); m != nil {
		l.Legacy, l.LegacyAt = m[1], m[2]
	}
	return l
}

// lastValue is the last of a trailer's values, as git joins them; a
// trailer written twice is read as its last.
func lastValue(s string) string {
	vs := strings.Split(strings.TrimSpace(s), "\x1d")
	return strings.TrimSpace(vs[len(vs)-1])
}

// OnMain reports whether rev names a commit on main: origin/main when the
// repo has an origin (fetched first when fetch is set), else main. It
// names the ref it read.
func OnMain(ctx context.Context, repoPath, rev string, fetch bool) (bool, string, error) {
	ref, err := mainRef(ctx, repoPath, fetch)
	if err != nil {
		return false, ref, err
	}
	return onRef(repoPath, rev, ref), ref, nil
}

// NormID is a spec id as three digits (12 is 012), or as given when it is
// not a number.
func NormID(id string) string {
	if n, err := strconv.Atoi(id); err == nil && n >= 0 {
		return fmt.Sprintf("%03d", n)
	}
	return id
}

// LandingOf is the newest commit on the local origin/main (main without an
// origin) whose Spec trailer names spec id, read without a fetch; false
// when none does.
func LandingOf(ctx context.Context, repoPath, id string) (Landed, bool, error) {
	ref, err := mainRef(ctx, repoPath, false)
	if err != nil {
		return Landed{}, false, err
	}
	out, err := gitCtx(ctx, repoPath, "log", "--regexp-ignore-case", "--grep=^"+SpecTrailer+":", "--format="+landedFormat, "--end-of-options", ref)
	if err != nil {
		return Landed{}, false, err
	}
	id = NormID(id)
	for _, rec := range strings.Split(out, "\x1e") {
		if rec = strings.Trim(rec, "\n"); rec == "" {
			continue
		}
		if l := parseLanded(rec); l.Spec == id {
			return l, true, nil
		}
	}
	return Landed{}, false, nil
}
