package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var (
	ErrNoThread = errors.New("runner: no such thread")
	ErrTimeout  = errors.New("runner: t3_dispatch timed out")
)

// ExecError is any other non-zero exit of t3_dispatch.py.
type ExecError struct {
	Code   int
	Stderr string
}

func (e *ExecError) Error() string { return fmt.Sprintf("t3_dispatch exit %d: %s", e.Code, e.Stderr) }

// T3DefaultTimeout bounds one t3_dispatch.py call.
const T3DefaultTimeout = 120 * time.Second

// T3 runs threads in T3 Code through t3_dispatch.py (`uv run <Dispatch>`;
// $BMESH_T3_COMMAND, split on spaces, replaces `uv run <Dispatch>` for
// tests). Dir holds prompt files ("" = <user cache dir>/bmesh/prompts);
// Timeout bounds each call (0 = 120 s). It never forces, queues or waits:
// Send refuses a running thread.
type T3 struct {
	Dispatch string
	Dir      string
	Timeout  time.Duration
}

func (r T3) command() []string { return command("BMESH_T3_COMMAND", "uv", "run", r.Dispatch) }

func (r T3) run(ctx context.Context, args ...string) ([]byte, error) {
	b := r.command()
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = T3DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, b[0], append(append([]string{}, b[1:]...), args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	cmd.WaitDelay = time.Second // grandchildren holding the pipes must not outlive the timeout
	err := cmd.Run()
	if err == nil {
		return out.Bytes(), nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, ErrTimeout
	}
	var xe *exec.ExitError
	if errors.As(err, &xe) {
		switch xe.ExitCode() {
		case 3:
			return nil, ErrNoThread
		case 4:
			return nil, ErrRunning
		case 5:
			return nil, ErrTimeout
		}
		return nil, &ExecError{Code: xe.ExitCode(), Stderr: errb.String()}
	}
	return nil, err
}

// Status reports whether the thread is mid-turn and its latest reply.
func (r T3) Status(ctx context.Context, thread string) (Status, error) {
	out, err := r.run(ctx, "status", thread)
	if err != nil {
		return Status{}, err
	}
	var v struct {
		Session struct {
			Status       string `json:"status"`
			ActiveTurnID string `json:"activeTurnId"`
		} `json:"session"`
		LatestTurn struct {
			State string `json:"state"`
		} `json:"latestTurn"`
		LastReply any `json:"lastReply"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return Status{}, fmt.Errorf("t3_dispatch status: %w", err)
	}
	s := Status{Known: true}
	s.Running = v.Session.Status == "starting" || v.Session.Status == "running" ||
		v.Session.ActiveTurnID != "" || v.LatestTurn.State == "running"
	if reply, ok := v.LastReply.(string); ok {
		s.LastReply = reply
	}
	return s, nil
}

func (r T3) dir() (string, error) {
	if r.Dir != "" {
		return r.Dir, nil
	}
	c, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(c, "bmesh", "prompts"), nil
}

// withFile writes text to an owner-only prompt file for the length of f.
func (r T3) withFile(text string, f func(path string) error) error {
	dir, err := r.dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	fh, err := os.CreateTemp(dir, "prompt-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(fh.Name())
	if err := fh.Chmod(0o600); err != nil {
		fh.Close()
		return err
	}
	if _, err := fh.WriteString(text); err != nil {
		fh.Close()
		return err
	}
	if err := fh.Close(); err != nil {
		return err
	}
	return f(fh.Name())
}

// Send messages an idle thread: status first, ErrRunning when it is mid-turn
// (and when send itself finds it running, exit 4).
func (r T3) Send(ctx context.Context, thread, text string) error {
	st, err := r.Status(ctx, thread)
	if err != nil {
		return err
	}
	if st.Running {
		return ErrRunning
	}
	return r.withFile(text, func(p string) error {
		_, err := r.run(ctx, "send", thread, "--prompt-file", p)
		return err
	})
}

// Start creates a thread in a fresh T3 worktree with the brief as its prompt,
// under the caller's thread id so a lost reply can be reconciled. With
// s.Worktree set the thread runs in that checkout instead (`--worktree`,
// with s.Branch as its branch), filed under s.Project's T3 project when
// that is set.
func (r T3) Start(ctx context.Context, s StartSpec) (Started, error) {
	project := s.Project
	if project == "" {
		project = s.RepoPath
	}
	args := []string{"start", "--repo", project, "--provider", "claude", "--model", s.Model,
		"--title", s.Title, "--prompt-file", s.BriefPath, "--thread-id", s.ThreadID}
	if s.Worktree != "" {
		args = append(args, "--worktree", s.Worktree)
		if s.Branch != "" {
			args = append(args, "--branch", s.Branch)
		}
	}
	if s.Effort != "" {
		args = append(args, "--effort", s.Effort)
	}
	out, err := r.run(ctx, args...)
	if err != nil {
		return Started{}, err
	}
	var v struct {
		ThreadID     string `json:"threadId"`
		Branch       string `json:"branch"`
		WorktreePath string `json:"worktreePath"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return Started{}, fmt.Errorf("t3_dispatch start: %w", err)
	}
	if v.ThreadID == "" {
		return Started{}, errors.New("t3_dispatch start returned no threadId")
	}
	return Started{Thread: v.ThreadID, Branch: v.Branch, Worktree: v.WorktreePath}, nil
}

// Interrupt stops the thread's running turn.
func (r T3) Interrupt(ctx context.Context, thread string) error {
	_, err := r.run(ctx, "stop", thread)
	return err
}
