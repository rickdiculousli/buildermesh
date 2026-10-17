package daemon

import (
	"context"
	"fmt"
	"strconv"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/level"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

// checkStepJob runs a received check-step (056): j.Repo is a registry
// repo, j.Args the spec id and the step's number. The step is read from
// this box's checkout's main after a fetch (under the repo's lock), never
// from the job, and its check runs in that checkout; ok on exit 0.
func (h *hostsNode) checkStepJob(ctx context.Context, j *hosts.Job) (bool, string) {
	if len(j.Args) != 2 {
		return false, "refused: want a spec id and a step number"
	}
	id := j.Args[0]
	n, err := strconv.Atoi(j.Args[1])
	if _, ierr := strconv.Atoi(id); ierr != nil || err != nil {
		return false, "refused: want a spec id and a step number"
	}
	path, ok := h.checkoutOf(j.Repo)
	if !ok {
		return false, fmt.Sprintf("refused: no checkout of %s here", cut(j.Repo, 64))
	}
	unlock, err := level.Lock(h.d.StateDir, j.Repo)
	if err != nil {
		return false, "lock: " + err.Error()
	}
	ferr := spec.FetchMain(ctx, path)
	unlock()
	if ferr != nil {
		return false, "fetch: " + ferr.Error()
	}
	ok, text := spec.CheckStep(ctx, path, h.conf().Spec.Glob, h.d.Host, id, n)
	h.d.Log.Info("hosts: check-step", "repo", j.Repo, "spec", id, "step", n, "by", j.By, "ok", ok)
	return ok, text
}
