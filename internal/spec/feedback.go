package spec

import (
	"regexp"
	"strconv"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// feedbackSeqRE finds the stream sequences a spec's from_feedback names: #<seq>.
var feedbackSeqRE = regexp.MustCompile(`#(\d+)\b`)

// OpenIssues is the feedback in all that names repo, less any whose
// sequence a spec in specs names in its from_feedback (#<seq>), whatever
// the spec's status, in all's order: what is left to rule on. A pass that
// stopped between landing a spec and draining its issue never writes that
// spec twice.
func OpenIssues(all []mesh.Feedback, repo string, specs []*Spec) []mesh.Feedback {
	named := map[uint64]bool{}
	for _, s := range specs {
		for _, m := range feedbackSeqRE.FindAllStringSubmatch(s.Fields["from_feedback"], -1) {
			if n, err := strconv.ParseUint(m[1], 10, 64); err == nil {
				named[n] = true
			}
		}
	}
	var out []mesh.Feedback
	for _, f := range all {
		if f.Repo == repo && !named[f.Seq] {
			out = append(out, f)
		}
	}
	return out
}
