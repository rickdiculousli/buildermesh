package instance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/kind"
)

// baseTools are always allowed to a model instance.
const baseTools = "Read,Grep,Glob,LS"

// Model runs jobs through `claude -p`, one session across jobs: the first job
// of a session sends the kind's prompt with --session-id, later jobs only the
// job text with --resume.
type Model struct {
	k      kind.Kind
	model  string
	claude []string

	mu          sync.Mutex
	session     string
	uses        int
	lastUse     time.Time
	lastContext int
}

// NewModel returns a model instance for k running model; claudeBin is the
// claude command, split on spaces.
func NewModel(k kind.Kind, model, claudeBin string) *Model {
	return &Model{k: k, model: model, claude: strings.Fields(claudeBin), session: uuid4()}
}

// Uses is the number of jobs run on the current session.
func (m *Model) Uses() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.uses
}

// SessionID is the current session's id.
func (m *Model) SessionID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.session
}

// LastContext is the context tokens of the last parsed reply.
func (m *Model) LastContext() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastContext
}

// Recycle starts a new session for the next job.
func (m *Model) Recycle() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.session, m.uses, m.lastUse, m.lastContext = uuid4(), 0, time.Time{}, 0
}

// ShouldRecycle reports whether the session has reached the kind's
// recycle_uses, recycle_context or recycle_idle (each ignored when 0).
func (m *Model) ShouldRecycle(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.k.RecycleUses > 0 && m.uses >= m.k.RecycleUses ||
		m.k.RecycleContext > 0 && m.lastContext > m.k.RecycleContext ||
		m.k.RecycleIdle > 0 && !m.lastUse.IsZero() && now.Sub(m.lastUse) > m.k.RecycleIdle
}

// Run sends j to the session. Runs on one Model must not overlap.
func (m *Model) Run(ctx context.Context, j Job) Reply {
	m.mu.Lock()
	session, first := m.session, m.uses == 0
	m.mu.Unlock()

	tools := baseTools
	if len(m.k.Allow) > 0 {
		tools += "," + strings.Join(m.k.Allow, ",")
	}
	argv := append(append([]string{}, m.claude...), "-p", "--model", m.model, "--output-format", "json")
	text := jobText(j)
	if first {
		argv = append(argv, "--session-id", session)
		text = m.k.Prompt + "\n\n" + text
	} else {
		argv = append(argv, "--resume", session)
	}
	if j.Effort != "" {
		argv = append(argv, "--effort", j.Effort)
	}
	argv = append(argv, "--allowedTools", tools, "--max-turns", "40")

	var stdout, stderr bytes.Buffer
	note, err := run(ctx, m.k.Timeout, j, func(ctx context.Context) *exec.Cmd { return exec.CommandContext(ctx, argv[0], argv[1:]...) },
		strings.NewReader(text), &stdout, &stderr)
	raw := stdout.String() + stderr.String()
	res, parsed := parseResult(stdout.Bytes())

	m.mu.Lock()
	if parsed {
		m.uses++
		m.lastUse = time.Now()
		m.lastContext = res.Usage.InputTokens + res.Usage.CacheReadInputTokens
	} else {
		// The session may not exist, only half, or be broken: the next job
		// starts a new one and sends the prompt again, whatever the kind's
		// recycle settings.
		m.session, m.uses, m.lastUse, m.lastContext = uuid4(), 0, time.Time{}, 0
	}
	m.mu.Unlock()

	// Every reply but the last judges nothing: claude never answered.
	switch {
	case note != "":
		return Reply{Output: raw + newline(raw) + note}
	case err != nil:
		if ee := (*exec.ExitError)(nil); !errors.As(err, &ee) {
			raw += newline(raw) + "[" + err.Error() + "]\n" // claude did not start
		} else if parsed && res.Result != "" {
			return Reply{Output: res.Result} // its error, say a failed sign-in
		}
		return Reply{Output: raw}
	case !parsed:
		return Reply{Output: "[bad claude output]\n" + raw}
	case res.IsError && res.Result == "":
		return Reply{Output: raw}
	case res.IsError:
		return Reply{Output: res.Result}
	}
	// A reply without a verdict passes by its exit code, unless the prompt
	// asked for one (R18).
	ok := !strings.Contains(m.k.Prompt, "VERDICT:")
	if v, found := verdict(res.Result); found {
		ok = strings.EqualFold(v, "ok")
	}
	return Reply{OK: ok, Judged: true, Output: res.Result, Context: res.Usage.InputTokens + res.Usage.CacheReadInputTokens}
}

// jobText is what a model sees of a job: a header line, the text and the args.
func jobText(j Job) string {
	s := "job: " + j.ID + " commit: " + j.Commit + " tree: " + j.Tree + "\n"
	if j.Text != "" {
		s += j.Text + "\n"
	}
	if len(j.Args) > 0 {
		s += "args: " + strings.Join(j.Args, " ") + "\n"
	}
	return s
}

type result struct {
	Type    string `json:"type"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
	Usage   struct {
		InputTokens          int `json:"input_tokens"`
		CacheReadInputTokens int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

// parseResult finds the last result object in claude's JSON output: a single
// object, an array of messages, or a stream of either.
func parseResult(b []byte) (result, bool) {
	var last result
	found := false
	take := func(raw json.RawMessage) {
		var r result
		if json.Unmarshal(raw, &r) == nil && r.Type == "result" {
			last, found = r, true
		}
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if !errors.Is(err, io.EOF) {
				return result{}, false
			}
			return last, found
		}
		if raw = bytes.TrimSpace(raw); len(raw) > 0 && raw[0] == '[' {
			var arr []json.RawMessage
			if json.Unmarshal(raw, &arr) != nil {
				return result{}, false
			}
			for _, e := range arr {
				take(e)
			}
			continue
		}
		take(raw)
	}
}

// decoration is markdown a model may wrap a verdict line in.
const decoration = "*_#`> \t"

// verdict returns the value of the last line starting with "VERDICT:",
// markdown decoration around the key and the value ignored:
// **VERDICT: fail**, **VERDICT:** fail and VERDICT: **ok** all read.
func verdict(text string) (string, bool) {
	v, found := "", false
	for _, line := range strings.Split(text, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimLeft(strings.TrimSpace(line), decoration), "VERDICT:"); ok {
			v, found = strings.Trim(rest, decoration), true
		}
	}
	return v, found
}
