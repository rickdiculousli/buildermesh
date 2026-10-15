package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Tmux runs claude in a window of one tmux session, in a git worktree it
// makes under Worktrees/<repo>/<thread>. Tmux is the binary ("" = tmux;
// $BMESH_TMUX_COMMAND, split on spaces, overrides it for tests). It cannot
// tell whether claude is mid-turn, so Status is never Known.
type Tmux struct {
	Session, Worktrees, Tmux string
}

// How long Start waits for claude's prompt before typing the brief anyway,
// and how often it looks.
const (
	promptWait = 90 * time.Second
	promptPoll = 500 * time.Millisecond
)

func (r Tmux) tmux(ctx context.Context, args ...string) (string, error) {
	bin := r.Tmux
	if bin == "" {
		bin = "tmux"
	}
	b := command("BMESH_TMUX_COMMAND", strings.Fields(bin)...)
	out, err := exec.CommandContext(ctx, b[0], append(append([]string{}, b[1:]...), args...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %v: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (r Tmux) target(thread string) string { return r.Session + ":" + thread }

// Start makes the worktree on a new branch named for the thread, opens a
// window running claude there, dismisses claude's start-up dialogs and types
// the brief in once the prompt is up. The window inherits BMESH_CONFIG when
// the caller has it set, so the lead reports to the mesh that started it.
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
	if _, err := r.tmux(ctx, "has-session", "-t", r.Session); err != nil {
		if _, err := r.tmux(ctx, "new-session", "-d", "-s", r.Session, "-c", wt); err != nil {
			return Started{}, err
		}
	}
	args := []string{"new-window", "-t", r.Session, "-n", s.ThreadID, "-c", wt}
	if cfg := os.Getenv("BMESH_CONFIG"); cfg != "" {
		args = append(args, "-e", "BMESH_CONFIG="+cfg)
	}
	if _, err := r.tmux(ctx, append(args, "claude")...); err != nil {
		return Started{}, err
	}
	if err := r.awaitPrompt(ctx, s.ThreadID); err != nil {
		return Started{}, err
	}
	if err := r.Send(ctx, s.ThreadID, string(brief)); err != nil {
		return Started{}, err
	}
	return Started{Thread: s.ThreadID, Branch: s.ThreadID, Worktree: wt}, nil
}

// awaitPrompt watches the window until claude shows its prompt, answering
// the dialogs a fresh worktree raises on the way: the folder-trust question
// (whose default is to exit) with "yes", the first-run theme and security
// screens with Enter, an offer to try something with Escape, any other
// confirmation with Enter. After promptWait it gives up and lets the brief
// be typed anyway.
func (r Tmux) awaitPrompt(ctx context.Context, thread string) error {
	deadline := time.Now().Add(promptWait)
	for time.Now().Before(deadline) {
		pane, err := r.tmux(ctx, "capture-pane", "-p", "-t", r.target(thread))
		if err != nil {
			return err
		}
		keys := dialogKeys(pane)
		if keys == nil {
			return nil
		}
		for _, k := range keys {
			if k == "" {
				continue
			}
			if _, err := r.tmux(ctx, "send-keys", "-t", r.target(thread), k); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(promptPoll):
		}
	}
	return nil
}

// dialogKeys returns the keys that answer the dialog on the pane, "" when
// nothing is on it yet (keep waiting), and nil once the prompt is up.
func dialogKeys(pane string) []string {
	switch {
	case strings.Contains(pane, "trust this folder"):
		return []string{"Down", "Enter"}
	case strings.Contains(pane, "Try the new"):
		return []string{"Escape"}
	case strings.Contains(pane, "Press Enter to continue"), strings.Contains(pane, "Choose the text style"),
		strings.Contains(pane, "Enter to confirm"):
		return []string{"Enter"}
	case strings.Contains(pane, "shift+tab to cycle"), strings.Contains(pane, "? for shortcuts"):
		return nil
	}
	return []string{""}
}

// Status is never Known: tmux can't see claude's turn state.
func (r Tmux) Status(ctx context.Context, thread string) (Status, error) { return Status{}, nil }

// Send types text into the window and presses Enter.
func (r Tmux) Send(ctx context.Context, thread, text string) error {
	if _, err := r.tmux(ctx, "send-keys", "-t", r.target(thread), "-l", text); err != nil {
		return err
	}
	_, err := r.tmux(ctx, "send-keys", "-t", r.target(thread), "Enter")
	return err
}

// Interrupt presses Escape in the window.
func (r Tmux) Interrupt(ctx context.Context, thread string) error {
	_, err := r.tmux(ctx, "send-keys", "-t", r.target(thread), "Escape")
	return err
}
