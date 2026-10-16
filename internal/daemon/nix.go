package daemon

import (
	"context"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
)

// nixJob runs a received nix (023).
func (h *hostsNode) nixJob(ctx context.Context, j *hosts.Job) (bool, string) {
	return false, "nix: not built yet"
}

// proposeJob holds a registry proposal for the admin (023).
func (h *hostsNode) proposeJob(ctx context.Context, j *hosts.Job) (bool, string) {
	return false, "propose: not built yet"
}
