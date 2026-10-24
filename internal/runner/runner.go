// Package runner starts and observes the agent sessions behind nodes: a T3
// Code thread (T3), claude in a tmux window (Tmux) or headless claude -p
// turns (Claude).
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp"
)

// ErrRunning means the thread is mid-turn, so nothing was sent.
var ErrRunning = errors.New("runner: thread is running, nothing sent")

// StartSpec is what a runner needs to start a session. Branch is the base to
// branch from ("" = the repo's HEAD); runners that make their own worktree
// (T3) ignore it. Worktree, when set, is an existing checkout the session
// runs in (its branch is Branch): no runner makes one then.
type StartSpec struct {
	RepoPath, Model, Effort, Title, BriefPath, ThreadID, Branch, Worktree string
}

// Started is the session a runner started.
type Started struct {
	Thread, Branch, Worktree string
}

// Status is one reading of a session. Known is false when the runner cannot
// tell whether the session is mid-turn (tmux). Waiting is a turn held on a
// question or approval; UsageLimit a newest turn that failed on a usage
// limit, LimitResetAt when it resets ("" = unknown). Stopped is a newest
// turn the user ended (an interrupt), StoppedRun naming it. Only T3 sets
// them.
type Status struct {
	Running, Known bool
	LastReply      string
	Waiting        bool
	UsageLimit     bool
	LimitResetAt   string
	Stopped        bool
	StoppedRun     string
}

// Runner starts and talks to agent sessions.
type Runner interface {
	Start(ctx context.Context, s StartSpec) (Started, error)
	Status(ctx context.Context, thread string) (Status, error)
	Send(ctx context.Context, thread, text string) error
	Interrupt(ctx context.Context, thread string) error
}

// Settler is a runner that can settle a thread, dropping it from its
// active list (T3); the thread wakes on a new message. Settling a settled
// thread changes nothing.
type Settler interface {
	Settle(ctx context.Context, thread string) error
}

// Preflighter is a runner that can tell, before a start is recorded
// anywhere, that the start would be refused.
type Preflighter interface {
	Preflight(ctx context.Context) error
}

// Preflight runs r's preflight when it has one.
func Preflight(ctx context.Context, r Runner) error {
	if p, ok := r.(Preflighter); ok {
		return p.Preflight(ctx)
	}
	return nil
}

// New returns the runner [runner] kind names. It takes the whole config so
// path-valued keys go through Config.Expand.
func New(cfg *config.Config) (Runner, error) {
	switch cfg.Runner.Kind {
	case "t3":
		r := &T3{Server: cfg.Runner.T3Server, RuntimeMode: cfg.Runner.T3Runtime, Cred: t3mcp.FileCredential{Path: cfg.T3TokenPath()}}
		if v := os.Getenv("BMESH_T3_TIMEOUT"); v != "" { // test hook
			d, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("BMESH_T3_TIMEOUT: %w", err)
			}
			r.Timeout = d
		}
		return r, nil
	case "tmux":
		return Tmux{Session: cfg.Runner.TmuxSession, Worktrees: cfg.Expand(cfg.Runner.Worktrees)}, nil
	case "claude":
		return Claude{Claude: cfg.Runner.Claude, Worktrees: cfg.Expand(cfg.Runner.Worktrees)}, nil
	}
	return nil, fmt.Errorf("unknown runner kind %s", cfg.Runner.Kind)
}

// command is env split on spaces when set (a test hook), else def.
func command(env string, def ...string) []string {
	if v := strings.Fields(os.Getenv(env)); len(v) > 0 {
		return v
	}
	return def
}

// worktreeFor is where a session that makes its own checkout runs: s.Worktree
// and s.Branch when the caller made one, else a new worktree on a branch
// named for the thread at <root>/<repo>/<thread>, from s.Branch, else from
// base (what the runner branches from by default).
func worktreeFor(ctx context.Context, root string, s StartSpec, base string) (wt, branch string, err error) {
	if s.Worktree != "" {
		if abs, err := filepath.Abs(s.Worktree); err == nil {
			s.Worktree = abs
		}
		return s.Worktree, s.Branch, nil
	}
	wt, err = filepath.Abs(filepath.Join(root, filepath.Base(s.RepoPath), s.ThreadID))
	if err != nil {
		return "", "", err
	}
	if s.Branch != "" {
		base = s.Branch
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", s.RepoPath, "worktree", "add", "-b", s.ThreadID, wt, base).CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return wt, s.ThreadID, nil
}
