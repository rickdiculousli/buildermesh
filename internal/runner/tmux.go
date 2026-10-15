package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Tmux runs claude in a window of one tmux session, in a git worktree it
// makes under Worktrees/<repo>/<thread>. Tmux is the binary ("" = tmux;
// $BMESH_TMUX_COMMAND, split on spaces, overrides it for tests). It cannot
// tell whether claude is mid-turn, so Status is never Known.
type Tmux struct {
	Session, Worktrees, Tmux string
}

func (r Tmux) tmux(ctx context.Context, args ...string) error {
	bin := r.Tmux
	if bin == "" {
		bin = "tmux"
	}
	b := command("BMESH_TMUX_COMMAND", strings.Fields(bin)...)
	out, err := exec.CommandContext(ctx, b[0], append(append([]string{}, b[1:]...), args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %s: %v: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (r Tmux) target(thread string) string { return r.Session + ":" + thread }

// Start makes the worktree on a new branch named for the thread, opens a
// window running claude there and types the brief into it.
func (r Tmux) Start(ctx context.Context, s StartSpec) (Started, error) {
	brief, err := os.ReadFile(s.BriefPath)
	if err != nil {
		return Started{}, err
	}
	wt, err := filepath.Abs(filepath.Join(r.Worktrees, filepath.Base(s.RepoPath), s.ThreadID))
	if err != nil {
		return Started{}, err
	}
	base := s.Branch
	if base == "" {
		base = "HEAD"
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", s.RepoPath, "worktree", "add", "-b", s.ThreadID, wt, base).CombinedOutput(); err != nil {
		return Started{}, fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if r.tmux(ctx, "has-session", "-t", r.Session) != nil {
		if err := r.tmux(ctx, "new-session", "-d", "-s", r.Session, "-c", wt); err != nil {
			return Started{}, err
		}
	}
	if err := r.tmux(ctx, "new-window", "-t", r.Session, "-n", s.ThreadID, "-c", wt, "claude"); err != nil {
		return Started{}, err
	}
	if err := r.Send(ctx, s.ThreadID, string(brief)); err != nil {
		return Started{}, err
	}
	return Started{Thread: s.ThreadID, Branch: s.ThreadID, Worktree: wt}, nil
}

// Status is never Known: tmux can't see claude's turn state.
func (r Tmux) Status(ctx context.Context, thread string) (Status, error) { return Status{}, nil }

// Send types text into the window and presses Enter.
func (r Tmux) Send(ctx context.Context, thread, text string) error {
	if err := r.tmux(ctx, "send-keys", "-t", r.target(thread), "-l", text); err != nil {
		return err
	}
	return r.tmux(ctx, "send-keys", "-t", r.target(thread), "Enter")
}

// Interrupt presses Escape in the window.
func (r Tmux) Interrupt(ctx context.Context, thread string) error {
	return r.tmux(ctx, "send-keys", "-t", r.target(thread), "Escape")
}
