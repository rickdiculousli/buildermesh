package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp"
)

// The T3 V2 path: T3 Code 0.1.0's MCP tools, signed in with the token
// `bmesh t3 login` keeps.

const signInHint = "T3 sign-in missing or expired; run bmesh t3 login"

// activeRun are the run states of a turn that hasn't ended.
var activeRun = map[string]bool{"preparing": true, "queued": true, "starting": true, "running": true, "waiting": true}

// terminalItem are the timeline item states whose text is final.
var terminalItem = map[string]bool{"completed": true, "failed": true, "cancelled": true, "interrupted": true}

// replyCursor is how far a thread's timeline has been read: the position
// after which the next read starts (-1 = from the start) and the newest
// assistant message seen.
type replyCursor struct {
	after int
	reply string
}

// token reads the sign-in and refuses a missing, expired or foreign one.
func (r *T3) token(ctx context.Context) (t3mcp.Token, error) {
	if r.Cred == nil {
		return t3mcp.Token{}, &SignInError{signInHint}
	}
	tok, err := r.Cred.Token(ctx)
	if errors.Is(err, t3mcp.ErrNoToken) {
		return t3mcp.Token{}, &SignInError{signInHint}
	}
	if err != nil {
		return t3mcp.Token{}, err
	}
	if !tok.ExpiresAt.After(r.now()) {
		return t3mcp.Token{}, &SignInError{signInHint}
	}
	if !t3mcp.SameServer(tok.Server, r.Server) {
		return t3mcp.Token{}, &SignInError{fmt.Sprintf("T3 sign-in is for %s, not [runner] t3_server %s; run bmesh t3 login", tok.Server, r.Server)}
	}
	return tok, nil
}

// client is the MCP session for the current token.
func (r *T3) client(ctx context.Context) (*t3mcp.Client, error) {
	tok, err := r.token(ctx)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mcp == nil || r.mcp.Token != tok.Token {
		r.mcp = &t3mcp.Client{URL: strings.TrimRight(r.Server, "/") + "/mcp", Token: tok.Token, HTTP: r.HTTP}
	}
	return r.mcp, nil
}

// call runs one tool under the runner's timeout, mapping T3's answers to
// the runner's errors.
func (r *T3) call(ctx context.Context, tool string, args, out any) error {
	c, err := r.client(ctx)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	err = c.Call(cctx, tool, args, out)
	switch {
	case err == nil:
		return nil
	case t3mcp.Code(err) == "thread_not_found":
		return ErrNoThread
	case errors.Is(err, t3mcp.ErrUnauthorized):
		return &SignInError{"T3 refused the sign-in token; run bmesh t3 login"}
	case errors.Is(cctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil:
		return ErrTimeout
	}
	return err
}

type v2Read struct {
	Thread struct {
		ActiveRunID       string `json:"activeRunId"`
		Branch            string `json:"branch"`
		WorktreePath      string `json:"worktreePath"`
		PendingRequests   int    `json:"pendingRequestCount"`
		LastErrorClass    string `json:"lastErrorClass"`
		UsageLimitResetAt string `json:"usageLimitResetAt"`
	} `json:"thread"`
	RecentRuns []struct {
		Status string `json:"status"`
	} `json:"recentRuns"`
	Items []struct {
		Position int     `json:"position"`
		Type     string  `json:"type"`
		Status   string  `json:"status"`
		Text     *string `json:"text"`
	} `json:"items"`
	HasMore bool `json:"hasMore"`
}

// read is one t3_thread_read page of the messages view after position
// after (-1 = from the start).
func (r *T3) read(ctx context.Context, thread string, after, limit int) (v2Read, error) {
	args := map[string]any{"threadId": thread, "view": "messages", "runLimit": 1, "limit": limit, "maxCharsPerItem": 20000}
	if after >= 0 {
		args["afterPosition"] = after
	}
	var v v2Read
	err := r.call(ctx, "t3_thread_read", args, &v)
	return v, err
}

// statusFrom maps a read onto Status: running while a run is active or the
// newest run hasn't ended.
func statusFrom(v v2Read) Status {
	s := Status{Known: true}
	newest := ""
	if len(v.RecentRuns) > 0 {
		newest = v.RecentRuns[0].Status
	}
	s.Running = v.Thread.ActiveRunID != "" || activeRun[newest]
	s.Waiting = newest == "waiting" && v.Thread.PendingRequests > 0
	if newest == "failed" && v.Thread.LastErrorClass == "usage_limit" {
		s.UsageLimit, s.LimitResetAt = true, v.Thread.UsageLimitResetAt
	}
	return s
}

// statusV2 reads the thread and its timeline from where the last read
// stopped, so a long thread costs one page a minute. The cursor never
// passes an item whose text may still grow (a streaming reply).
func (r *T3) statusV2(ctx context.Context, thread string) (Status, error) {
	r.mu.Lock()
	cur, ok := r.replies[thread]
	r.mu.Unlock()
	if !ok {
		cur = replyCursor{after: -1}
	}
	var st Status
	for page := 0; page < 50; page++ {
		v, err := r.read(ctx, thread, cur.after, 100)
		if err != nil {
			return Status{}, err
		}
		if page == 0 {
			st = statusFrom(v)
		}
		held := false
		for _, it := range v.Items {
			if it.Type == "assistant_message" && it.Text != nil {
				cur.reply = *it.Text
			}
			if !held && !terminalItem[it.Status] {
				held = true
				cur.after = it.Position - 1
			}
			if !held {
				cur.after = it.Position
			}
		}
		if held || !v.HasMore {
			break
		}
	}
	st.LastReply = cur.reply
	r.mu.Lock()
	if r.replies == nil {
		r.replies = map[string]replyCursor{}
	}
	r.replies[thread] = cur
	r.mu.Unlock()
	return st, nil
}

// sendV2 refuses a running thread, then queues the text: queue, never
// steer, so a turn that starts between the check and the send gets the
// message after it. The request id is the thread, text and minute, so a
// retried wake isn't sent twice.
func (r *T3) sendV2(ctx context.Context, thread, text string) error {
	st, err := r.statusV2(ctx, thread)
	if err != nil {
		return err
	}
	if st.Running {
		return ErrRunning
	}
	return r.call(ctx, "t3_thread_send", map[string]any{"threadId": thread, "message": text, "mode": "queue",
		"clientRequestId": sendRequestID(thread, text, r.now())}, nil)
}

// sendRequestID is bmesh-<sha256(thread, text, minute)>.
func sendRequestID(thread, text string, now time.Time) string {
	sum := sha256.Sum256([]byte(thread + "\x00" + text + "\x00" + now.UTC().Truncate(time.Minute).Format(time.RFC3339)))
	return "bmesh-" + hex.EncodeToString(sum[:])
}

func (r *T3) interruptV2(ctx context.Context, thread string) error {
	err := r.call(ctx, "t3_thread_interrupt", map[string]any{"threadId": thread}, nil)
	if t3mcp.Code(err) == "no_active_run" {
		return nil
	}
	return err
}

// samePath compares two directories with symlinks resolved.
func samePath(a, b string) bool {
	return resolvedPath(a) == resolvedPath(b)
}

func resolvedPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

type v2Project struct {
	ID            string `json:"id"`
	WorkspaceRoot string `json:"workspaceRoot"`
}

// findProject is the id of the project whose workspaceRoot is repo, or "".
func (r *T3) findProject(ctx context.Context, repo string) (string, error) {
	cursor := -1
	for page := 0; page < 100; page++ {
		args := map[string]any{"limit": 100}
		if cursor >= 0 {
			args["cursor"] = cursor
		}
		var v struct {
			Projects   []v2Project `json:"projects"`
			NextCursor *int        `json:"nextCursor"`
		}
		if err := r.call(ctx, "t3_project_list", args, &v); err != nil {
			return "", err
		}
		for _, p := range v.Projects {
			if samePath(p.WorkspaceRoot, repo) {
				return p.ID, nil
			}
		}
		if v.NextCursor == nil {
			break
		}
		cursor = *v.NextCursor
	}
	return "", nil
}

// project is the T3 project of repo, registered when T3 has none.
func (r *T3) project(ctx context.Context, repo string) (string, error) {
	r.mu.Lock()
	id := r.projects[resolvedPath(repo)]
	r.mu.Unlock()
	if id != "" {
		return id, nil
	}
	id, err := r.findProject(ctx, repo)
	if err != nil {
		return "", err
	}
	if id == "" {
		var p v2Project
		err = r.call(ctx, "t3_project_create", map[string]any{"title": filepath.Base(repo), "workspaceRoot": repo}, &p)
		switch {
		case t3mcp.Code(err) == "capability_denied":
			return "", fmt.Errorf("T3 has no project for %s and this sign-in can't register one (that takes full access): add the repo as a project in T3, then start again", repo)
		case err != nil:
			// Someone may have registered it meanwhile.
			if again, lerr := r.findProject(ctx, repo); lerr == nil && again != "" {
				id = again
			} else {
				return "", err
			}
		default:
			id = p.ID
		}
	}
	r.mu.Lock()
	if r.projects == nil {
		r.projects = map[string]string{}
	}
	r.projects[resolvedPath(repo)] = id
	r.mu.Unlock()
	return id, nil
}

// startable checks the sign-in can start a thread in the runtime mode
// and returns that mode.
func (r *T3) startable(ctx context.Context) (string, error) {
	mode := r.RuntimeMode
	if mode == "" {
		mode = "auto"
	}
	tok, err := r.token(ctx)
	if err != nil {
		return "", err
	}
	if t3mcp.RuntimeRank(tok.Access) < t3mcp.RuntimeRank(mode) {
		return "", &SignInError{fmt.Sprintf("T3 sign-in allows %s, below [runner] t3_runtime_mode %s; run bmesh t3 login and approve %s",
			tok.Access, mode, mode)}
	}
	return mode, nil
}

// Preflight refuses, before anything is recorded, a start the server or
// the sign-in can't serve: an unknown protocol, and on V2 a sign-in
// that is missing, expired, foreign or below the runtime mode.
func (r *T3) Preflight(ctx context.Context) error {
	p, err := r.protocol(ctx)
	if err != nil || p != 2 {
		return err
	}
	_, err = r.startable(ctx)
	return err
}

// Protocol is the server's orchestration protocol (1 or 2).
func (r *T3) Protocol(ctx context.Context) (int, error) { return r.protocol(ctx) }

// startV2 launches the thread under the repo's own project ([runner]
// t3_project has no effect on V2: T3 binds only a project's own
// worktrees), in a new worktree on bmesh/<ThreadID> from s.Branch (else
// main), or in s.Worktree on s.Branch. Launch has no retry key, so a lost
// or timed-out reply is reconciled by branch before any error is returned.
func (r *T3) startV2(ctx context.Context, s StartSpec) (Started, error) {
	mode, err := r.startable(ctx)
	if err != nil {
		return Started{}, err
	}
	brief, err := os.ReadFile(s.BriefPath)
	if err != nil {
		return Started{}, err
	}
	project, err := r.project(ctx, s.RepoPath)
	if err != nil {
		return Started{}, err
	}
	model := map[string]any{"provider": "claudeAgent", "model": s.Model}
	if s.Effort != "" {
		model["options"] = map[string]any{"effort": s.Effort}
	}
	started := Started{Branch: s.Branch, Worktree: s.Worktree}
	var ws map[string]any
	if s.Worktree != "" {
		if abs, err := filepath.Abs(s.Worktree); err == nil {
			started.Worktree = abs
		}
		ws = map[string]any{"type": "existing_worktree", "worktreePath": started.Worktree, "branch": s.Branch}
	} else {
		base := s.Branch
		if base == "" {
			base = "main"
		}
		started.Branch = "bmesh/" + s.ThreadID
		ws = map[string]any{"type": "worktree", "baseRef": base, "branch": started.Branch}
	}
	var out struct {
		ThreadID string `json:"threadId"`
	}
	err = r.call(ctx, "t3_thread_launch", map[string]any{"projectId": project, "title": s.Title, "modelSelection": model,
		"runtimeMode": mode, "workspaceStrategy": ws, "message": string(brief)}, &out)
	var te *t3mcp.ToolError
	var se *SignInError
	if err != nil && !errors.As(err, &te) && !errors.As(err, &se) {
		if id := r.reconcile(ctx, project, s.Title, started.Branch); id != "" {
			out.ThreadID, err = id, nil
		}
	}
	if err != nil {
		return Started{}, err
	}
	if out.ThreadID == "" {
		return Started{}, errors.New("t3_thread_launch returned no threadId")
	}
	started.Thread = out.ThreadID
	if s.Worktree == "" {
		started.Worktree = r.waitWorktree(ctx, out.ThreadID)
	}
	return started, nil
}

// reconcile finds a thread a lost launch made: the newest thread of project
// titled title whose branch is branch; "" when there is none.
func (r *T3) reconcile(ctx context.Context, project, title, branch string) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.timeout())
	defer cancel()
	var v struct {
		Threads []struct {
			ThreadID string `json:"threadId"`
		} `json:"threads"`
	}
	if err := r.call(ctx, "t3_thread_list", map[string]any{"projectId": project, "titleContains": title, "limit": 20}, &v); err != nil {
		return ""
	}
	for _, t := range v.Threads {
		var rd v2Read
		if err := r.call(ctx, "t3_thread_read", map[string]any{"threadId": t.ThreadID, "limit": 1, "runLimit": 1}, &rd); err != nil {
			continue
		}
		if rd.Thread.Branch == branch {
			return t.ThreadID
		}
	}
	return ""
}

// waitWorktree is the worktree T3 prepares for a new thread, read until it
// is named (up to 30 s); "" when it isn't by then.
func (r *T3) waitWorktree(ctx context.Context, thread string) string {
	deadline := time.Now().Add(30 * time.Second)
	for {
		var rd v2Read
		if err := r.call(ctx, "t3_thread_read", map[string]any{"threadId": thread, "limit": 1, "runLimit": 1}, &rd); err == nil &&
			rd.Thread.WorktreePath != "" {
			return rd.Thread.WorktreePath
		}
		if time.Now().After(deadline) {
			return ""
		}
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(500 * time.Millisecond):
		}
	}
}
