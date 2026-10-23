package main

import (
	"context"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/gitwork"
)

// landOnMain fast-forwards main to branch (gitwork.LandOnMain) under the
// repo's lock when [hosts] is enabled, so a level of the same checkout, or
// the daemon's rescaffold, never runs at once.
func (a *app) landOnMain(ctx context.Context, repoPath, branch string) error {
	return gitwork.LandOnMain(ctx, repoPath, branch, gitwork.RepoLock(a.cfg, config.StateDir(), repoPath))
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!*?[](){}<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
