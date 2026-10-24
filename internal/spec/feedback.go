package spec

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// feedbackSeqRE finds the stream sequences a spec's from_feedback names: #<seq>.
var feedbackSeqRE = regexp.MustCompile(`#(\d+)\b`)

// fixSubjectRE is the subject of a commit that fixes feedback with no spec:
// the fixer's `Fix #<seq>: ...` or a coordinator's `Quick fix from <family>
// #<seq>: ...`, several issues as `#<seq>, #<seq>`. Group 1 is the list.
var fixSubjectRE = regexp.MustCompile(`^(?:Fix|Quick fix from \S+) (#\d+(?:, #\d+)*): `)

// FixedSeqs is the feedback sequences the fix commits on ref name in their
// subjects (fixSubjectRE).
func FixedSeqs(repo, ref string) (map[uint64]bool, error) {
	out, err := git(repo, "log", ref, "--format=%s", "-E", "--grep", `^(Fix|Quick fix from [^ ]+) #[0-9]`)
	if err != nil {
		return nil, err
	}
	fixed := map[uint64]bool{}
	for _, subject := range strings.Split(out, "\n") {
		if m := fixSubjectRE.FindStringSubmatch(subject); m != nil {
			addSeqs(fixed, m[1])
		}
	}
	return fixed, nil
}

// OpenIssues is the feedback in all that names repo, less any whose
// sequence a spec in specs names in its from_feedback (#<seq>), whatever
// the spec's status, or a fix commit names (fixed, from FixedSeqs), in
// all's order: what is left to rule on. A pass that stopped between
// landing a spec or a fix and draining its issue never does it twice.
func OpenIssues(all []mesh.Feedback, repo string, specs []*Spec, fixed map[uint64]bool) []mesh.Feedback {
	named := map[uint64]bool{}
	for n := range fixed {
		named[n] = true
	}
	for _, s := range specs {
		addSeqs(named, s.Fields["from_feedback"])
	}
	var out []mesh.Feedback
	for _, f := range all {
		if f.Repo == repo && !named[f.Seq] {
			out = append(out, f)
		}
	}
	return out
}

// addSeqs adds every #<seq> in text to seqs.
func addSeqs(seqs map[uint64]bool, text string) {
	for _, m := range feedbackSeqRE.FindAllStringSubmatch(text, -1) {
		if n, err := strconv.ParseUint(m[1], 10, 64); err == nil {
			seqs[n] = true
		}
	}
}
