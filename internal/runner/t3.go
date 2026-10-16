package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp"
)

var (
	ErrNoThread = errors.New("runner: no such thread")
	ErrTimeout  = errors.New("runner: t3 timed out")
)

// StartTimeout is a timed-out start on a runner that names its own
// threads (T3 V2): a thread it made, if any, is the one on Branch.
type StartTimeout struct{ Branch string }

func (e *StartTimeout) Error() string {
	return "runner: t3 start timed out; look for a thread on branch " + e.Branch
}
func (e *StartTimeout) Unwrap() error { return ErrTimeout }

// SignInError is a T3 sign-in that can't serve the call: missing, expired,
// for another server, or below [runner] t3_runtime_mode. Its message says
// what to run; the CLI exits 3 with it.
type SignInError struct{ Msg string }

func (e *SignInError) Error() string { return e.Msg }

// ExecError is any other non-zero exit of t3_dispatch.py.
type ExecError struct {
	Code   int
	Stderr string
}

func (e *ExecError) Error() string { return fmt.Sprintf("t3_dispatch exit %d: %s", e.Code, e.Stderr) }

// T3DefaultTimeout bounds one t3_dispatch.py call, or one MCP call.
const T3DefaultTimeout = 120 * time.Second

// protocolTTL is how long a server's protocol version is trusted.
const protocolTTL = time.Minute

// T3 runs threads in T3 Code, by the protocol Server reports at
// /.well-known/t3/environment (read at most once a minute): protocol 1
// through t3_dispatch.py (`uv run <Dispatch>`; $BMESH_T3_COMMAND, split on
// spaces, replaces `uv run <Dispatch>` for tests), protocol 2 through T3's
// MCP tools with the token Cred gives. Server "" means protocol 1 without
// asking. Dir holds V1 prompt files ("" = <user cache dir>/bmesh/prompts);
// Timeout bounds each call (0 = 120 s); RuntimeMode is the mode threads
// start in ("" = t3_dispatch's default on V1, auto on V2). Send refuses a
// running thread on both paths. Use it through a pointer: it caches.
type T3 struct {
	Dispatch    string
	Dir         string
	Timeout     time.Duration
	Server      string
	RuntimeMode string
	Cred        t3mcp.Credential
	HTTP        *http.Client     // nil = http.DefaultClient
	Now         func() time.Time // nil = time.Now

	mu       sync.Mutex
	proto    int
	protoAt  time.Time
	mcp      *t3mcp.Client
	projects map[string]string // repo path -> project id
	replies  map[string]replyCursor
}

func (r *T3) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *T3) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return T3DefaultTimeout
}

// protocol is Server's orchestrationProtocolVersion (1 or 2), cached for a
// minute; any other version is an error naming it.
func (r *T3) protocol(ctx context.Context) (int, error) {
	if r.Server == "" {
		return 1, nil
	}
	r.mu.Lock()
	if r.proto != 0 && r.now().Sub(r.protoAt) < protocolTTL {
		p := r.proto
		r.mu.Unlock()
		return p, nil
	}
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.Server, "/")+"/.well-known/t3/environment", nil)
	if err != nil {
		return 0, err
	}
	hc := r.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, fmt.Errorf("t3 at %s: %w", r.Server, err)
	}
	defer resp.Body.Close()
	var env struct {
		ServerVersion string `json:"serverVersion"`
		Protocol      int    `json:"orchestrationProtocolVersion"`
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("t3 at %s: environment: HTTP %d", r.Server, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return 0, fmt.Errorf("t3 at %s: environment: %w", r.Server, err)
	}
	if env.Protocol != 1 && env.Protocol != 2 {
		return 0, fmt.Errorf("t3 at %s (T3 %s) speaks orchestration protocol %d; bmesh speaks 1 and 2", r.Server, env.ServerVersion, env.Protocol)
	}
	r.mu.Lock()
	r.proto, r.protoAt = env.Protocol, r.now()
	r.mu.Unlock()
	return env.Protocol, nil
}

// v2Thread reports whether thread is a V2 (MCP) thread id: mcp:<uuid>.
func v2Thread(thread string) bool { return strings.HasPrefix(thread, "mcp:") }

// path picks the path for an existing thread: 2 for V2, 1 for V1, and
// ErrNoThread for a thread the server's protocol doesn't have (it stays on
// the path it started on).
func (r *T3) path(ctx context.Context, thread string) (int, error) {
	p, err := r.protocol(ctx)
	if err != nil {
		return 0, err
	}
	if (p == 2) != v2Thread(thread) {
		return 0, ErrNoThread
	}
	return p, nil
}

// Status reports whether the thread is mid-turn and its latest reply.
func (r *T3) Status(ctx context.Context, thread string) (Status, error) {
	p, err := r.path(ctx, thread)
	if err != nil {
		return Status{}, err
	}
	if p == 2 {
		return r.statusV2(ctx, thread)
	}
	return r.statusV1(ctx, thread)
}

// Send messages an idle thread: status first, ErrRunning when it is
// mid-turn.
func (r *T3) Send(ctx context.Context, thread, text string) error {
	p, err := r.path(ctx, thread)
	if err != nil {
		return err
	}
	if p == 2 {
		return r.sendV2(ctx, thread, text)
	}
	return r.sendV1(ctx, thread, text)
}

// Start starts a thread on the path the server speaks.
func (r *T3) Start(ctx context.Context, s StartSpec) (Started, error) {
	p, err := r.protocol(ctx)
	if err != nil {
		return Started{}, err
	}
	if p == 2 {
		return r.startV2(ctx, s)
	}
	return r.startV1(ctx, s)
}

// Interrupt stops the thread's running turn.
func (r *T3) Interrupt(ctx context.Context, thread string) error {
	p, err := r.path(ctx, thread)
	if err != nil {
		return err
	}
	if p == 2 {
		return r.interruptV2(ctx, thread)
	}
	_, err = r.run(ctx, "stop", thread)
	return err
}

func (r *T3) command() []string { return command("BMESH_T3_COMMAND", "uv", "run", r.Dispatch) }

func (r *T3) run(ctx context.Context, args ...string) ([]byte, error) {
	b := r.command()
	ctx, cancel := context.WithTimeout(ctx, r.timeout())
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

func (r *T3) statusV1(ctx context.Context, thread string) (Status, error) {
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

func (r *T3) dir() (string, error) {
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
func (r *T3) withFile(text string, f func(path string) error) error {
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

// sendV1: when send itself finds the thread running (exit 4), ErrRunning too.
func (r *T3) sendV1(ctx context.Context, thread, text string) error {
	st, err := r.statusV1(ctx, thread)
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

// startV1 creates a thread in a fresh T3 worktree with the brief as its
// prompt, under the caller's thread id so a lost reply can be reconciled.
// With s.Worktree set the thread runs in that checkout instead
// (`--worktree`, with s.Branch as its branch), filed under s.Project's T3
// project when that is set.
func (r *T3) startV1(ctx context.Context, s StartSpec) (Started, error) {
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
	if r.RuntimeMode != "" {
		args = append(args, "--runtime-mode", r.RuntimeMode)
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
