package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
// threads (T3): a thread it made, if any, is the one on Branch.
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

// T3DefaultTimeout bounds one MCP call.
const T3DefaultTimeout = 120 * time.Second

// protocolTTL is how long a server's protocol version is trusted.
const protocolTTL = time.Minute

// T3 runs threads in T3 Code 0.1.0 through its MCP tools at Server, with
// the token Cred gives. Every call first reads Server's
// /.well-known/t3/environment (at most once a minute) and refuses any
// orchestration protocol but 2. Timeout bounds each call (0 = 120 s);
// RuntimeMode is the mode threads start in ("" = auto). Send refuses a
// running thread. Use it through a pointer: it caches.
type T3 struct {
	Timeout     time.Duration
	Server      string
	RuntimeMode string
	Cred        t3mcp.Credential
	HTTP        *http.Client     // nil = http.DefaultClient
	Now         func() time.Time // nil = time.Now

	mu       sync.Mutex
	protoAt  time.Time // when Server last reported protocol 2
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

// protocol refuses a Server whose orchestrationProtocolVersion isn't 2:
// protocol 1 is T3 before 0.1.0, any other an error naming it. A 2 is
// trusted for a minute.
func (r *T3) protocol(ctx context.Context) error {
	r.mu.Lock()
	ok := !r.protoAt.IsZero() && r.now().Sub(r.protoAt) < protocolTTL
	r.mu.Unlock()
	if ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.Server, "/")+"/.well-known/t3/environment", nil)
	if err != nil {
		return err
	}
	hc := r.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("t3 at %s: %w", r.Server, err)
	}
	defer resp.Body.Close()
	var env struct {
		ServerVersion string `json:"serverVersion"`
		Protocol      int    `json:"orchestrationProtocolVersion"`
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("t3 at %s: environment: HTTP %d", r.Server, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("t3 at %s: environment: %w", r.Server, err)
	}
	switch env.Protocol {
	case 2:
	case 1:
		return fmt.Errorf("T3 %s at %s speaks protocol 1; bmesh needs T3 0.1.0 or later", env.ServerVersion, r.Server)
	default:
		return fmt.Errorf("t3 at %s (T3 %s) speaks orchestration protocol %d; bmesh speaks 2", r.Server, env.ServerVersion, env.Protocol)
	}
	r.mu.Lock()
	r.protoAt = r.now()
	r.mu.Unlock()
	return nil
}

// v2Thread reports whether thread is a T3 0.1.0 thread id: mcp:<uuid>.
func v2Thread(thread string) bool { return strings.HasPrefix(thread, "mcp:") }

// check is protocol for a call on an existing thread: a thread id from
// before T3 0.1.0 is ErrNoThread.
func (r *T3) check(ctx context.Context, thread string) error {
	if err := r.protocol(ctx); err != nil {
		return err
	}
	if !v2Thread(thread) {
		return ErrNoThread
	}
	return nil
}

// Status reports whether the thread is mid-turn and its latest reply.
func (r *T3) Status(ctx context.Context, thread string) (Status, error) {
	if err := r.check(ctx, thread); err != nil {
		return Status{}, err
	}
	return r.status(ctx, thread)
}

// Send messages an idle thread: status first, ErrRunning when it is
// mid-turn.
func (r *T3) Send(ctx context.Context, thread, text string) error {
	if err := r.check(ctx, thread); err != nil {
		return err
	}
	return r.send(ctx, thread, text)
}

// Start starts a thread.
func (r *T3) Start(ctx context.Context, s StartSpec) (Started, error) {
	if err := r.protocol(ctx); err != nil {
		return Started{}, err
	}
	return r.start(ctx, s)
}

// Interrupt stops the thread's running turn.
func (r *T3) Interrupt(ctx context.Context, thread string) error {
	if err := r.check(ctx, thread); err != nil {
		return err
	}
	return r.interrupt(ctx, thread)
}
