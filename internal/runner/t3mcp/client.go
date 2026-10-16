// Package t3mcp speaks to T3 Code's MCP endpoint (/mcp: streamable HTTP,
// JSON-RPC, the mcp-session-id header) and holds the sign-in that endpoint
// needs: the OAuth login that mints a token and the file it is kept in.
package t3mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// ProtocolVersion is the MCP revision the client asks for.
const ProtocolVersion = "2025-06-18"

// ErrUnauthorized means the server refused the token (401).
var ErrUnauthorized = errors.New("t3mcp: T3 refused the sign-in token")

// ToolError is a tool result with isError: T3's code and message.
type ToolError struct {
	Tool, Code, Message string
}

func (e *ToolError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("%s: %s", e.Tool, e.Message)
	}
	return fmt.Sprintf("%s: %s: %s", e.Tool, e.Code, e.Message)
}

// Code is err's ToolError code, or "".
func Code(err error) string {
	var te *ToolError
	if errors.As(err, &te) {
		return te.Code
	}
	return ""
}

// Client is one MCP session against URL (the server's /mcp) with a bearer
// token. It initializes on first use and once more when the server has
// forgotten the session (404). Safe for concurrent use.
type Client struct {
	URL   string
	Token string
	HTTP  *http.Client // nil = http.DefaultClient

	mu      sync.Mutex
	session string
	id      int64
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// errSessionGone is a 404 on a request that carried a session id.
var errSessionGone = errors.New("t3mcp: session expired")

// post sends one JSON-RPC message under session (none when "") and returns
// the response carrying id (nil for a notification) and the session id the
// server named.
func (c *Client) post(ctx context.Context, session, method string, params any, id *int64) (json.RawMessage, string, error) {
	body, err := json.Marshal(rpcMsg{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
		req.Header.Set("Mcp-Protocol-Version", ProtocolVersion)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	sid := resp.Header.Get("Mcp-Session-Id")
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, "", ErrUnauthorized
	case resp.StatusCode == http.StatusNotFound && session != "":
		return nil, "", errSessionGone
	case resp.StatusCode == http.StatusAccepted && id == nil:
		return nil, sid, nil
	case resp.StatusCode/100 != 2:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, "", fmt.Errorf("t3mcp: %s: HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if id == nil {
		return nil, sid, nil
	}
	msg, err := readResponse(resp, *id)
	if err != nil {
		return nil, "", fmt.Errorf("t3mcp: %s: %w", method, err)
	}
	if msg.Error != nil {
		return nil, "", fmt.Errorf("t3mcp: %s: JSON-RPC %d: %s", method, msg.Error.Code, msg.Error.Message)
	}
	return msg.Result, sid, nil
}

// readResponse reads the message answering id from a JSON body or an SSE
// stream of messages.
func readResponse(resp *http.Response, id int64) (rpcMsg, error) {
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		var m rpcMsg
		err := json.NewDecoder(resp.Body).Decode(&m)
		return m, err
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	var data strings.Builder
	flush := func() (rpcMsg, bool) {
		defer data.Reset()
		var m rpcMsg
		if data.Len() == 0 || json.Unmarshal([]byte(data.String()), &m) != nil {
			return m, false
		}
		return m, m.ID != nil && *m.ID == id
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if m, ok := flush(); ok {
				return m, nil
			}
			continue
		}
		if v, ok := strings.CutPrefix(line, "data:"); ok {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(v, " "))
		}
	}
	if m, ok := flush(); ok {
		return m, nil
	}
	if err := sc.Err(); err != nil {
		return rpcMsg{}, err
	}
	return rpcMsg{}, errors.New("stream ended without a reply")
}

func (c *Client) nextID() *int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.id++
	id := c.id
	return &id
}

// initialize starts a session (initialize, then notifications/initialized)
// unless one is open, and returns it.
func (c *Client) initialize(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.session != "" {
		s := c.session
		c.mu.Unlock()
		return s, nil
	}
	c.mu.Unlock()
	params := map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "bmesh", "version": "1"}}
	_, sid, err := c.post(ctx, "", "initialize", params, c.nextID())
	if err != nil {
		return "", err
	}
	if sid == "" {
		return "", errors.New("t3mcp: initialize: no mcp-session-id")
	}
	if _, _, err := c.post(ctx, sid, "notifications/initialized", nil, nil); err != nil {
		return "", err
	}
	c.mu.Lock()
	c.session = sid
	c.mu.Unlock()
	return sid, nil
}

type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

// Call runs tool with args and decodes its structured result into out (nil
// = discard). A result with isError is a *ToolError.
func (c *Client) Call(ctx context.Context, tool string, args, out any) error {
	params := map[string]any{"name": tool, "arguments": args}
	var raw json.RawMessage
	for attempt := 0; ; attempt++ {
		sid, err := c.initialize(ctx)
		if err != nil {
			return err
		}
		raw, _, err = c.post(ctx, sid, "tools/call", params, c.nextID())
		if errors.Is(err, errSessionGone) && attempt == 0 {
			c.mu.Lock()
			if c.session == sid {
				c.session = ""
			}
			c.mu.Unlock()
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	var r toolResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("t3mcp: %s: %w", tool, err)
	}
	text := ""
	if len(r.Content) > 0 {
		text = r.Content[0].Text
	}
	if r.IsError {
		te := &ToolError{Tool: tool, Message: text}
		var v struct{ Code, Message string }
		if json.Unmarshal([]byte(text), &v) == nil && (v.Code != "" || v.Message != "") {
			te.Code, te.Message = v.Code, v.Message
		}
		return te
	}
	if out == nil {
		return nil
	}
	data := r.StructuredContent
	if len(data) == 0 || string(data) == "null" {
		data = json.RawMessage(text)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("t3mcp: %s result: %w", tool, err)
	}
	return nil
}
