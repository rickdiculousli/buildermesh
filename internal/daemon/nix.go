package daemon

import (
	"context"
	"slices"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/level"
	"github.com/rickdiculousli/buildermesh/internal/nix"
)

// nixJob runs a received nix: the job (signed by the admin key, checked
// in Check) names a repo and its registry origin, never a path. The path
// is this box's own registry row's; the box refuses, touching nothing,
// what nix.Nix refuses, and otherwise removes the checkout and its
// routine checkouts. A repo with no row here is absent.
func (h *hostsNode) nixJob(ctx context.Context, j *hosts.Job) (bool, string) {
	if len(j.Args) == 0 {
		return false, "refused: nix names no origin"
	}
	cfg := h.conf()
	row, ok := cfg.Repos[j.Repo]
	if !ok {
		return true, "absent: " + cut(j.Repo, 64) + " is not in this box's registry"
	}
	_, path, err := cfg.RepoPath(j.Repo)
	if err != nil {
		return false, "refused: " + err.Error()
	}
	unlock, err := level.Lock(h.d.StateDir, j.Repo)
	if err != nil {
		return false, "failed: " + err.Error()
	}
	defer unlock()
	o := nix.Nix(ctx, nix.Request{Repo: j.Repo, Path: path, RowPath: row.Path, RowRemote: row.Remote, Origin: j.Args[0],
		Checkouts: h.d.Checkouts, IgnoredOK: slices.Contains(j.Args[1:], "ignored-ok")})
	h.d.Log.Info("hosts: nix", "repo", j.Repo, "by", j.By, "result", o.Text)
	return o.OK(), o.Text
}
