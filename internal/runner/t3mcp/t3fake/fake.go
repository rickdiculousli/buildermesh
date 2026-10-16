// Package t3fake is an httptest T3 Code server for tests: the environment
// document, the MCP OAuth endpoints and the MCP tools the t3 runner calls,
// answering with the shapes recorded from a T3 0.1.0 server (kept in
// testdata/t3v2). It holds projects and threads in memory and records every
// tool call.
package t3fake

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Run is one run of a thread.
type Run struct {
	ID, Status string
}

// Item is one timeline item (messages view): user_message or
// assistant_message, with an item status (completed, running ...).
type Item struct {
	Type, Status, Text string
}

// Thread is a fake thread.
type Thread struct {
	ID, ProjectID, Title, Branch, Worktree, Model, RuntimeMode string
	Runs                                                       []Run // oldest first
	ActiveRunID                                                string
	Items                                                      []Item // position = index*2
	Pending                                                    int
	LastErrorClass, UsageLimitResetAt                          string
}

// Project is a fake project.
type Project struct {
	ID, Title, WorkspaceRoot string
}

// Send is one accepted t3_thread_send.
type Send struct {
	Thread, Message, Mode, ClientRequestID string
}

// Call is one tool call: its name and arguments.
type Call struct {
	Tool string
	Args map[string]any
}

type code struct {
	challenge, redirect, origin, access string
}

// Server is the fake. Lock mu (through Do) to change its fields while it
// serves.
type Server struct {
	*httptest.Server

	mu            sync.Mutex
	Protocol      int    // orchestrationProtocolVersion: 2 (default) or 1
	EnvironmentID string // default "env-fake"
	Access        string // the ceiling a sign-in grants and an unsigned token has; default "auto"
	Tokens        map[string]string
	Projects      []Project
	Threads       map[string]*Thread
	order         []string // thread ids, oldest first
	Calls         []Call
	Sends         []Send
	sent          map[string]map[string]any // clientRequestId -> result
	sessions      map[string]bool
	codes         map[string]code
	HangLaunch    bool // launch creates the thread, then never replies
	LoseLaunch    bool // launch never replies and creates nothing
	n             int
}

// New starts a fake that accepts token "tok" at access "auto".
func New() *Server {
	s := &Server{Protocol: 2, EnvironmentID: "env-fake", Access: "auto", Tokens: map[string]string{},
		Threads: map[string]*Thread{}, sent: map[string]map[string]any{}, sessions: map[string]bool{},
		codes: map[string]code{}}
	s.Tokens["tok"] = "auto"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/t3/environment", s.environment)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.metadata)
	mux.HandleFunc("POST /oauth/mcp/register", s.register)
	mux.HandleFunc("GET /oauth/mcp/authorize", s.authorize)
	mux.HandleFunc("POST /oauth/mcp/token", s.token)
	mux.HandleFunc("POST /mcp", s.mcp)
	s.Server = httptest.NewServer(mux)
	return s
}

// Do runs f with the fake locked.
func (s *Server) Do(f func(s *Server)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

// AddThread registers t (its project must exist) and returns it.
func (s *Server) AddThread(t *Thread) *Thread {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Threads[t.ID] = t
	s.order = append(s.order, t.ID)
	return t
}

// ExpireSessions forgets every MCP session, as a restarted server does.
func (s *Server) ExpireSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = map[string]bool{}
}

// ToolCalls returns the names of the tools called so far.
func (s *Server) ToolCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, c := range s.Calls {
		out = append(out, c.Tool)
	}
	return out
}

func (s *Server) id(prefix string) string {
	s.n++
	return fmt.Sprintf("%s%08d-0000-4000-8000-%012d", prefix, s.n, s.n)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func origin(r *http.Request) string { return "http://" + r.Host }

func (s *Server) environment(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version := "0.1.0"
	if s.Protocol == 1 {
		version = "0.0.45"
	}
	writeJSON(w, 200, map[string]any{"environmentId": s.EnvironmentID, "label": "fake",
		"platform":      map[string]any{"os": "linux", "arch": "x64", "machine": "desktop"},
		"serverVersion": version, "orchestrationProtocolVersion": s.Protocol, "capabilities": map[string]any{}})
}

func (s *Server) metadata(w http.ResponseWriter, r *http.Request) {
	o := origin(r)
	writeJSON(w, 200, map[string]any{"issuer": o, "authorization_endpoint": o + "/oauth/mcp/authorize",
		"token_endpoint": o + "/oauth/mcp/token", "registration_endpoint": o + "/oauth/mcp/register",
		"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code"},
		"code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported": []string{"orchestration:read", "orchestration:operate"}, "authorization_response_iss_parameter_supported": true})
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Name      string   `json:"client_name"`
		Redirects []string `json:"redirect_uris"`
	}
	if json.NewDecoder(r.Body).Decode(&v) != nil || len(v.Redirects) == 0 {
		writeJSON(w, 400, map[string]any{"error": "invalid_redirect_uri", "error_description": "Register between 1 and 5 redirect URIs."})
		return
	}
	b, _ := json.Marshal(map[string]any{"v": 1, "n": v.Name, "r": v.Redirects})
	writeJSON(w, 201, map[string]any{"client_id": base64.RawURLEncoding.EncodeToString(b) + ".sig", "client_name": v.Name,
		"redirect_uris": v.Redirects, "grant_types": []string{"authorization_code"}, "response_types": []string{"code"},
		"token_endpoint_auth_method": "none"})
}

// AuthorizeRedirect is where approving the sign-in at authorizeURL sends
// the browser: the redirect URI with a code, state and iss, granting
// s.Access. It is what a test pastes into the login.
func (s *Server) AuthorizeRedirect(authorizeURL string) (string, error) {
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(authorizeURL)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		return "", fmt.Errorf("authorize: HTTP %d", resp.StatusCode)
	}
	return resp.Header.Get("Location"), nil
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 ||
		q.Get("client_id") == "" || q.Get("redirect_uri") == "" {
		http.Error(w, "This sign-in link is not valid.", 400)
		return
	}
	s.mu.Lock()
	c := s.id("code-")
	s.codes[c] = code{challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), origin: origin(r), access: s.Access}
	s.mu.Unlock()
	u, _ := url.Parse(q.Get("redirect_uri"))
	v := u.Query()
	v.Set("code", c)
	v.Set("state", q.Get("state"))
	v.Set("iss", origin(r))
	u.RawQuery = v.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// MintToken makes a token shaped like T3's (base64url claims, a dot, a
// signature) with ceiling access expiring at exp, and accepts it.
func (s *Server) MintToken(access string, exp time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mintToken(access, exp)
}

func (s *Server) mintToken(access string, exp time.Time) string {
	scopes := []string{"orchestration:read", "orchestration:operate"}
	if access == "full-access" {
		scopes = append(scopes, "settings:write", "source-control:write")
	}
	b, _ := json.Marshal(map[string]any{"v": 1, "kind": "session", "sid": "sid", "sub": "mcp-client", "scopes": scopes,
		"method": "bearer-access-token", "rtc": access, "iat": time.Now().UnixMilli(), "exp": exp.UnixMilli()})
	t := base64.RawURLEncoding.EncodeToString(b) + ".sig"
	s.Tokens[t] = access
	return t
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "authorization_code" {
		writeJSON(w, 400, map[string]any{"error": "unsupported_grant_type", "error_description": "Only authorization_code is supported."})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.codes[r.PostForm.Get("code")]
	delete(s.codes, r.PostForm.Get("code"))
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !ok || c.redirect != r.PostForm.Get("redirect_uri") || c.origin != origin(r) ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge {
		writeJSON(w, 400, map[string]any{"error": "invalid_grant", "error_description": "The code is invalid or expired."})
		return
	}
	exp := time.Now().Add(30 * 24 * time.Hour)
	t := s.mintToken(c.access, exp)
	scope := "orchestration:read orchestration:operate"
	if c.access == "full-access" {
		scope += " settings:write source-control:write"
	}
	writeJSON(w, 200, map[string]any{"access_token": t, "token_type": "Bearer", "expires_in": int64(30 * 24 * 3600), "scope": scope})
}

type rpc struct {
	ID     *json.RawMessage `json:"id"`
	Method string           `json:"method"`
	Params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	access, ok := s.Tokens[tok]
	s.mu.Unlock()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		writeJSON(w, 401, map[string]any{"error": "invalid_mcp_credential", "message": "A valid T3 Code MCP credential is required."})
		return
	}
	var m rpc
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		writeJSON(w, 400, map[string]any{"jsonrpc": "2.0", "error": map[string]any{"code": -32700, "message": "parse error"}})
		return
	}
	reply := func(result any) {
		writeJSON(w, 200, map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
	}
	if m.Method == "initialize" {
		s.mu.Lock()
		sid := s.id("session-")
		s.sessions[sid] = true
		s.mu.Unlock()
		w.Header().Set("Mcp-Session-Id", sid)
		reply(map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{"name": "T3 Code", "version": "0.1.0"}})
		return
	}
	sid := r.Header.Get("Mcp-Session-Id")
	if sid == "" {
		writeJSON(w, 400, map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32600,
			"message": "MCP-Session-Id header is required; send initialize to start a session"}})
		return
	}
	s.mu.Lock()
	known := s.sessions[sid]
	s.mu.Unlock()
	if !known {
		w.WriteHeader(404)
		return
	}
	if m.ID == nil {
		w.WriteHeader(202)
		return
	}
	if m.Method != "tools/call" {
		writeJSON(w, 200, map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32601, "message": "method not found"}})
		return
	}
	s.mu.Lock()
	s.Calls = append(s.Calls, Call{Tool: m.Params.Name, Args: m.Params.Arguments})
	hang := m.Params.Name == "t3_thread_launch" && s.HangLaunch
	if m.Params.Name == "t3_thread_launch" && s.LoseLaunch {
		s.mu.Unlock()
		<-r.Context().Done()
		return
	}
	result, err := s.tool(m.Params.Name, m.Params.Arguments, access)
	s.mu.Unlock()
	if hang && err == nil {
		<-r.Context().Done() // the thread exists; the reply is lost
		return
	}
	if err != nil {
		text, _ := json.Marshal(map[string]any{"_tag": "OrchestratorMcpFailure", "code": err.code, "message": err.message})
		reply(map[string]any{"content": []any{map[string]any{"type": "text", "text": string(text)}}, "isError": true})
		return
	}
	text, _ := json.Marshal(result)
	reply(map[string]any{"content": []any{map[string]any{"type": "text", "text": string(text)}}, "structuredContent": result, "isError": false})
}

type toolErr struct{ code, message string }

func str(a map[string]any, k string) string {
	v, _ := a[k].(string)
	return v
}

func num(a map[string]any, k string, def int) int {
	if v, ok := a[k].(float64); ok {
		return int(v)
	}
	return def
}

var rank = map[string]int{"approval-required": 0, "auto-accept-edits": 1, "auto": 2, "full-access": 3}

func (t *Thread) status() string {
	if len(t.Runs) == 0 {
		return "idle"
	}
	return t.Runs[len(t.Runs)-1].Status
}

func (t *Thread) summary() map[string]any {
	latest := any(nil)
	if len(t.Runs) > 0 {
		latest = t.Runs[len(t.Runs)-1].ID
	}
	return map[string]any{"threadId": t.ID, "title": t.Title, "createdBy": "agent", "creationSource": "mcp",
		"status": t.status(), "latestRunId": latest, "providerInstanceId": "claudeAgent", "model": t.Model,
		"runtimeMode": t.RuntimeMode, "interactionMode": "default", "linkedPullRequest": nil, "settled": false,
		"settledAt": nil, "snoozed": false, "snoozedUntil": nil, "parentThreadId": nil, "relationshipToParent": nil,
		"itemCount": len(t.Items), "createdAt": "2026-10-16T02:21:56.900Z", "updatedAt": "2026-10-16T02:22:49.959Z"}
}

func (s *Server) project(id string) *Project {
	for i := range s.Projects {
		if s.Projects[i].ID == id {
			return &s.Projects[i]
		}
	}
	return nil
}

func projectJSON(p Project) map[string]any {
	return map[string]any{"id": p.ID, "title": p.Title, "workspaceRoot": p.WorkspaceRoot, "repositoryIdentity": nil,
		"faviconPath": nil, "projectIcon": nil, "defaultModelSelection": nil, "defaultThreadEnvMode": nil,
		"autoPull": false, "scripts": []any{}, "createdAt": "2026-10-16T02:21:04.966Z",
		"updatedAt": "2026-10-16T02:21:04.966Z", "deletedAt": nil}
}

func (s *Server) newRun(t *Thread, status string) Run {
	r := Run{ID: fmt.Sprintf("run:thread:%s:ordinal:%d", url.PathEscape(t.ID), len(t.Runs)+1), Status: status}
	t.Runs = append(t.Runs, r)
	return r
}

func (s *Server) tool(name string, a map[string]any, access string) (map[string]any, *toolErr) {
	switch name {
	case "t3_project_list":
		var ps []any
		for _, p := range s.Projects {
			ps = append(ps, projectJSON(p))
		}
		return map[string]any{"projects": ps, "nextCursor": nil}, nil
	case "t3_project_create":
		if access != "full-access" {
			return nil, &toolErr{"capability_denied", "Registering a project needs a full-access client."}
		}
		root := str(a, "workspaceRoot")
		for _, p := range s.Projects {
			if p.WorkspaceRoot == root {
				return nil, &toolErr{"project_exists", "That workspace is already registered."}
			}
		}
		p := Project{ID: s.id("mcp:"), Title: str(a, "title"), WorkspaceRoot: root}
		s.Projects = append(s.Projects, p)
		return projectJSON(p), nil
	case "t3_thread_launch":
		p := s.project(str(a, "projectId"))
		if p == nil {
			return nil, &toolErr{"project_not_found", "Project " + str(a, "projectId") + " is not available."}
		}
		mode := str(a, "runtimeMode")
		if rank[mode] > rank[access] {
			return nil, &toolErr{"runtime_mode_escalation_denied", fmt.Sprintf("Child runtime mode %s is broader than parent mode %s.", mode, access)}
		}
		ms, _ := a["modelSelection"].(map[string]any)
		ws, _ := a["workspaceStrategy"].(map[string]any)
		t := &Thread{ID: s.id("mcp:"), ProjectID: p.ID, Title: str(a, "title"), Model: str(ms, "model"), RuntimeMode: mode,
			Branch: str(ws, "branch")}
		switch str(ws, "type") {
		case "worktree":
			t.Worktree = filepath.Join("/fake-t3/worktrees", filepath.Base(p.WorkspaceRoot), t.Branch)
		case "existing_worktree":
			t.Worktree = str(ws, "worktreePath")
		}
		r := s.newRun(t, "preparing")
		t.ActiveRunID = r.ID
		if msg := str(a, "message"); msg != "" {
			t.Items = append(t.Items, Item{Type: "user_message", Status: "completed", Text: msg})
		}
		s.Threads[t.ID] = t
		s.order = append(s.order, t.ID)
		return map[string]any{"threadId": t.ID, "projectId": p.ID,
			"modelSelection": map[string]any{"instanceId": "claudeAgent", "model": t.Model, "options": ms["options"]},
			"runId":          r.ID, "status": "preparing"}, nil
	case "t3_thread_list":
		if str(a, "projectId") == "" {
			return nil, &toolErr{"target_required", "Pass projectId: this MCP client is not running inside a T3 thread."}
		}
		limit := num(a, "limit", 50)
		var ts []any
		for i := len(s.order) - 1; i >= 0 && len(ts) < limit; i-- {
			t := s.Threads[s.order[i]]
			if t.ProjectID != str(a, "projectId") || !strings.Contains(t.Title, str(a, "titleContains")) {
				continue
			}
			ts = append(ts, t.summary())
		}
		return map[string]any{"projectId": str(a, "projectId"), "currentThreadId": nil, "threads": ts, "nextCursor": nil, "total": len(ts)}, nil
	case "t3_thread_read":
		t := s.Threads[str(a, "threadId")]
		if t == nil {
			return nil, &toolErr{"thread_not_found", "Thread " + str(a, "threadId") + " is no longer available."}
		}
		th := t.summary()
		var active any
		if t.ActiveRunID != "" {
			active = t.ActiveRunID
		}
		var wt, br any
		if t.Worktree != "" {
			wt = t.Worktree
		}
		if t.Branch != "" {
			br = t.Branch
		}
		for k, v := range map[string]any{"projectId": t.ProjectID, "activeRunId": active, "titleRegeneration": nil,
			"branch": br, "worktreePath": wt, "runCount": len(t.Runs), "pendingRequestCount": t.Pending, "archived": false} {
			th[k] = v
		}
		if t.LastErrorClass != "" {
			th["lastErrorClass"] = t.LastErrorClass
		}
		if t.UsageLimitResetAt != "" {
			th["usageLimitResetAt"] = t.UsageLimitResetAt
		}
		var runs []any
		for i := len(t.Runs) - 1; i >= 0 && len(runs) < num(a, "runLimit", 10); i-- {
			runs = append(runs, map[string]any{"runId": t.Runs[i].ID, "ordinal": i + 1, "status": t.Runs[i].Status,
				"providerInstanceId": "claudeAgent", "model": t.Model, "requestedAt": "2026-10-16T02:22:49.100Z",
				"startedAt": nil, "completedAt": nil})
		}
		after, limit := num(a, "afterPosition", -1), num(a, "limit", 50)
		var items []any
		next := any(nil)
		more := false
		for i, it := range t.Items {
			pos := i * 2
			if pos <= after {
				continue
			}
			if len(items) == limit {
				more = true
				break
			}
			items = append(items, map[string]any{"position": pos, "visibility": "local", "sourceThreadId": t.ID,
				"itemId": fmt.Sprintf("item-%d", pos), "runId": nil, "messageId": nil, "createdBy": "agent",
				"creationSource": "provider", "type": it.Type, "status": it.Status, "title": nil, "text": it.Text,
				"textTruncated": false, "nextTextOffset": nil, "updatedAt": "2026-10-16T02:22:03.958Z"})
			next = pos
		}
		return map[string]any{"thread": th, "recentRuns": runs, "items": items, "nextPosition": next, "hasMore": more}, nil
	case "t3_thread_send":
		t := s.Threads[str(a, "threadId")]
		if t == nil {
			return nil, &toolErr{"thread_not_found", "Thread " + str(a, "threadId") + " is no longer available."}
		}
		if id := str(a, "clientRequestId"); id != "" {
			if prev, ok := s.sent[id]; ok {
				return prev, nil
			}
		}
		status, delivery := "starting", "started"
		if t.ActiveRunID != "" && str(a, "mode") == "queue" {
			status, delivery = "queued", "queued"
		}
		r := s.newRun(t, status)
		if t.ActiveRunID == "" {
			t.ActiveRunID = r.ID
		}
		t.Items = append(t.Items, Item{Type: "user_message", Status: "completed", Text: str(a, "message")})
		s.Sends = append(s.Sends, Send{Thread: t.ID, Message: str(a, "message"), Mode: str(a, "mode"), ClientRequestID: str(a, "clientRequestId")})
		res := map[string]any{"threadId": t.ID, "messageId": s.id("message:"), "runId": r.ID, "status": status, "delivery": delivery}
		if id := str(a, "clientRequestId"); id != "" {
			s.sent[id] = res
		}
		return res, nil
	case "t3_thread_interrupt":
		t := s.Threads[str(a, "threadId")]
		if t == nil {
			return nil, &toolErr{"thread_not_found", "Thread " + str(a, "threadId") + " is no longer available."}
		}
		if t.ActiveRunID == "" {
			return map[string]any{"threadId": t.ID, "runId": nil, "status": "no_active_run"}, nil
		}
		id := t.ActiveRunID
		for i := range t.Runs {
			if t.Runs[i].ID == id {
				t.Runs[i].Status = "interrupted"
			}
		}
		t.ActiveRunID = ""
		return map[string]any{"threadId": t.ID, "runId": id, "status": "interrupt_requested"}, nil
	}
	return nil, &toolErr{"unknown_tool", "no tool " + name}
}
