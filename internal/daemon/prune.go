package daemon

import (
	"context"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
)

// pruneJob runs a received prune (023).
func (h *hostsNode) pruneJob(ctx context.Context, j *hosts.Job) (bool, string) {
	return false, "prune: not built yet"
}

// forgetWorktreeJob runs a received forget-worktree (023).
func (h *hostsNode) forgetWorktreeJob(ctx context.Context, j *hosts.Job) (bool, string) {
	return false, "forget-worktree: not built yet"
}
