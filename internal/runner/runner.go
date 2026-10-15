// Package runner starts and observes the agent sessions behind nodes: a T3
// Code thread (T3) or claude in a tmux window (Tmux).
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

// ErrRunning means the thread is mid-turn, so nothing was sent.
var ErrRunning = errors.New("runner: thread is running, nothing sent")

// StartSpec is what a runner needs to start a session. Branch is the base to
// branch from ("" = the repo's HEAD); runners that make their own worktree
// (T3) ignore it.
type StartSpec struct {
	RepoPath, Model, Title, BriefPath, ThreadID, Branch string
}

// Started is the session a runner started.
type Started struct {
	Thread, Branch, Worktree string
}

// Status is one reading of a session. Known is false when the runner cannot
// tell whether the session is mid-turn (tmux).
type Status struct {
	Running, Known bool
	LastReply      string
}

// Runner starts and talks to agent sessions.
type Runner interface {
	Start(ctx context.Context, s StartSpec) (Started, error)
	Status(ctx context.Context, thread string) (Status, error)
	Send(ctx context.Context, thread, text string) error
	Interrupt(ctx context.Context, thread string) error
}

// New returns the runner [runner] kind names. It takes the whole config so
// path-valued keys go through Config.Expand.
func New(cfg *config.Config) (Runner, error) {
	switch cfg.Runner.Kind {
	case "t3":
		return T3{Dispatch: cfg.Expand(cfg.Runner.T3Dispatch)}, nil
	case "tmux":
		return Tmux{Session: cfg.Runner.TmuxSession, Worktrees: cfg.Expand(cfg.Runner.Worktrees)}, nil
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
