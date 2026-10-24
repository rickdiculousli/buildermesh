// Package session measures the context of the agent session a bmesh
// command runs in, from what its harness records, and keeps which nudge
// the session last heard (094). A harness bmesh can't read, or a record it
// doesn't recognise, measures nothing: the reader is silent, never wrong.
package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

// Reading is a session's context as its harness last recorded it.
type Reading struct {
	Harness string // "claude"
	ID      string // the harness's session id
	Tokens  int    // the newest model call's input: the context it read
	// Main is that the command runs on the session's own thread, not a
	// subagent's: only then is the session told anything or its state
	// changed.
	Main bool
}

// Short is the session id's first eight characters, as `bmesh context`
// prints it.
func (r Reading) Short() string { return r.ID[:min(8, len(r.ID))] }

// sessionID is what a session id may be before it goes into a path.
var sessionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Measure reads the context of the session this process runs in: Claude
// Code's, which names its session in CLAUDE_CODE_SESSION_ID. getenv is
// os.Getenv. False when no harness names a session it can read.
func Measure(getenv func(string) string) (Reading, bool) {
	id := getenv("CLAUDE_CODE_SESSION_ID")
	if !sessionID.MatchString(id) {
		return Reading{}, false
	}
	dir := getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Reading{}, false
		}
		dir = filepath.Join(home, ".claude")
	}
	path, ok := claudeTranscript(dir, id)
	if !ok {
		return Reading{}, false
	}
	t, ok := claudeTail(path)
	if !ok {
		return Reading{}, false
	}
	return Reading{Harness: "claude", ID: id, Tokens: t.tokens, Main: t.main}, true
}

// claudeTranscript is <dir>/projects/*/<id>.jsonl, the newest when two
// project dirs hold one.
func claudeTranscript(dir, id string) (string, bool) {
	matches, _ := filepath.Glob(filepath.Join(dir, "projects", "*", id+".jsonl"))
	var best string
	var at time.Time
	for _, m := range matches {
		if st, err := os.Stat(m); err == nil && st.Mode().IsRegular() && (best == "" || st.ModTime().After(at)) {
			best, at = m, st.ModTime()
		}
	}
	return best, best != ""
}

// Tail windows: the reader starts with the last tailFirst bytes and widens
// fourfold while it finds nothing, up to tailMax; never the whole of a
// long transcript.
const (
	tailFirst = 64 << 10
	tailMax   = 16 << 20
)

// tail is what the end of a transcript says.
type tail struct {
	tokens int  // the newest assistant message's context
	main   bool // the session's own thread is in a tool call
}

// claudeTail reads the end of a Claude Code transcript (see scan); false
// when no window up to tailMax holds an answer.
func claudeTail(path string) (tail, bool) {
	f, err := os.Open(path)
	if err != nil {
		return tail{}, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return tail{}, false
	}
	size := st.Size()
	for window := int64(tailFirst); ; window *= 4 {
		start := max(0, size-window)
		buf := make([]byte, size-start)
		if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
			return tail{}, false
		}
		if start > 0 {
			// The first line is cut: drop it.
			i := bytes.IndexByte(buf, '\n')
			if i < 0 {
				buf = nil
			} else {
				buf = buf[i+1:]
			}
		}
		if t, found, ok := scan(buf); found {
			return t, ok
		}
		if start == 0 || window >= tailMax {
			return tail{}, false
		}
	}
}

// record is what scan reads of a transcript line.
type record struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	IsSidechain bool   `json:"isSidechain"`
	Message     *struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			Input       int `json:"input_tokens"`
			CacheRead   int `json:"cache_read_input_tokens"`
			CacheCreate int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// block is a content block's type, a tool_use's id and name, and a
// tool_result's tool_use_id.
type block struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	ToolUseID string `json:"tool_use_id"`
}

func (r record) blocks() []block {
	var b []block
	if r.Message != nil && len(r.Message.Content) > 0 && r.Message.Content[0] == '[' {
		json.Unmarshal(r.Message.Content, &b)
	}
	return b
}

// scan reads buf's lines from the last, on the session's own thread (a
// line marked isSidechain is a subagent's). It is found at the newest
// assistant message whose usage sums above 0 (a message Claude Code makes
// up has all zeros): ok, with its context, input_tokens +
// cache_read_input_tokens + cache_creation_input_tokens. A compaction newer
// than that is found, not ok. The thread is in a tool call (main) when a
// model call newer than that message is in flight (an api-request line:
// Claude Code writes a message a second or so after its tools start), or
// when the message has a Bash call with no tool_result yet and no Agent or
// Task call without one: a subagent runs while its parent waits on its
// Agent call.
func scan(buf []byte) (t tail, found, ok bool) {
	results := map[string]bool{} // tool_use ids answered after the message
	inflight := false
	msg := ""
	var calls []block
	for len(buf) > 0 {
		var line []byte
		if i := bytes.LastIndexByte(bytes.TrimRight(buf, "\n"), '\n'); i >= 0 {
			line, buf = buf[i+1:], buf[:i]
		} else {
			line, buf = buf, nil
		}
		if !bytes.Contains(line, []byte(`"type"`)) {
			continue
		}
		var r record
		if json.Unmarshal(line, &r) != nil || r.IsSidechain {
			continue
		}
		if msg != "" {
			// Collecting the newest message's lines, one per content block,
			// up to the turn before it.
			if r.Type == "user" || r.Type == "system" || r.Type == "assistant" && (r.Message == nil || r.Message.ID != msg) {
				break
			}
			if r.Type == "assistant" {
				calls = append(calls, r.blocks()...)
			}
			continue
		}
		switch r.Type {
		case "api-request":
			inflight = true
		case "user":
			for _, b := range r.blocks() {
				if b.Type == "tool_result" {
					results[b.ToolUseID] = true
				}
			}
		case "system":
			if r.Subtype == "compact_boundary" {
				return tail{}, true, false
			}
		case "assistant":
			if r.Message == nil || r.Message.Usage == nil {
				continue
			}
			u := r.Message.Usage
			if sum := u.Input + u.CacheRead + u.CacheCreate; sum > 0 {
				t.tokens, msg = sum, r.Message.ID
				calls = r.blocks()
				if msg == "" {
					buf = nil // no id to group lines by: this line alone
				}
			}
		}
	}
	if msg == "" && t.tokens == 0 {
		return tail{}, false, false
	}
	bash, agent := false, false
	for _, c := range calls {
		if c.Type == "tool_use" && !results[c.ID] {
			bash = bash || c.Name == "Bash"
			agent = agent || c.Name == "Agent" || c.Name == "Task"
		}
	}
	t.main = inflight || bash && !agent
	return t, true, true
}

// Level is the highest nudge at or below tokens: nudge_from, every
// nudge_every past it, compact_from and compact_soon; 0 below nudge_from
// or with nudge_from 0.
func Level(c config.ContextConfig, tokens int) int {
	if c.NudgeFrom <= 0 || tokens < c.NudgeFrom {
		return 0
	}
	level := c.NudgeFrom
	if c.NudgeEvery > 0 {
		level += (tokens - c.NudgeFrom) / c.NudgeEvery * c.NudgeEvery
	}
	for _, t := range []int{c.CompactFrom, c.CompactSoon} {
		if t >= c.NudgeFrom && t <= tokens {
			level = max(level, t)
		}
	}
	return level
}

// howToCompact is how a session compacts, said with the advice to (100):
// the state file, then the two queued messages (the /compact alone when
// the operator asked for a pause), and its effort left alone until the
// compaction has run, since a change before it rereads the whole long
// context with a cache miss.
const howToCompact = "To compact: update your state file (the plan, the order, the ids in flight, what's next, open questions, lessons; in the repo's gitignored scratch dir or elsewhere that outlives the session, never a temp dir or a commit); then, where your harness lets a session queue into its own thread (T3 Code's t3_thread_send, mode queue), queue in one step `/compact <what to keep, naming the state file>` and `Resume: read <state file>, run bmesh mesh-status, and carry on from its Next.`, or the /compact alone when the operator asked you to pause; elsewhere ask the operator to type /compact, then to resume you. Leave your effort as it is until the compaction has run."

// afterCompact is what a session does first once it compacted: read its
// state file, then set its effort for what is next.
const afterCompact = "read your state file before going on, and set your effort for the work ahead now, where your harness lets a session set its own (T3 Code's t3_thread_configure): high to design, medium to run settled work."

// Advice is what a session at level is told to do; "" below nudge_from.
func Advice(c config.ContextConfig, level int) string {
	switch {
	case level <= 0:
		return ""
	case c.CompactSoon > 0 && level >= c.CompactSoon:
		return "Compact at the next safe point. " + howToCompact
	case c.CompactFrom > 0 && level >= c.CompactFrom:
		return "Compact at your next milestone, never while exploring or investigating; about to finish the work in hand, finish it first. " + howToCompact
	case c.NudgeEvery > 0:
		return "You'll hear again every " + K(c.NudgeEvery) + "."
	}
	for _, t := range []int{c.CompactFrom, c.CompactSoon} {
		if t > level {
			return "You'll hear again at " + K(t) + "."
		}
	}
	return ""
}

// K writes tokens in whole thousands, rounded down: 312456 is 312k.
func K(tokens int) string { return strconv.Itoa(tokens/1000) + "k" }

// keep is how long a session's nudge state outlives its last change.
const keep = 7 * 24 * time.Hour

// state is <state dir>/context/<session>: the level the session last
// heard and the context bmesh last read for it at or past nudge_from.
type state struct{ level, tokens int }

// Heard records r for its session and returns the line the session is
// owed, "" for none. A level above the one it last heard is a nudge. A
// level below it on fewer tokens is a compaction (the harness's own
// included): the line says so, once, and the levels are said again as the
// context grows back. A level below it on as many tokens or more is a
// [context] changed since, recorded silently.
// Nothing is said or recorded for a reading off the session's own thread,
// or with nudge_from 0; a line bmesh can't record is never said, so a
// failing disk can't repeat it. dir is the state dir.
func Heard(dir string, c config.ContextConfig, r Reading, now time.Time) (line string, compacted bool) {
	if c.NudgeFrom <= 0 || !r.Main {
		return "", false
	}
	d := filepath.Join(dir, "context")
	p := filepath.Join(d, r.ID)
	var last state
	if b, err := os.ReadFile(p); err == nil {
		fmt.Sscan(string(b), &last.level, &last.tokens)
	}
	cur := state{Level(c, r.Tokens), r.Tokens}
	if cur.level == 0 && last.level == 0 || cur == last {
		return "", false
	}
	if os.MkdirAll(d, 0o755) != nil || writeAtomic(p, []byte(fmt.Sprintf("%d %d\n", cur.level, cur.tokens))) != nil {
		return "", false
	}
	prune(d, now)
	switch {
	case cur.level > last.level:
		return strings.TrimSpace("bmesh: this session's context is " + K(r.Tokens) + ". " + Advice(c, cur.level)), false
	case cur.level < last.level && r.Tokens < last.tokens:
		return "bmesh: this session compacted (" + K(last.tokens) + " -> " + K(r.Tokens) + "); " + afterCompact, true
	}
	return "", false
}

func writeAtomic(p string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), ".context-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// prune removes the sessions in d unchanged for keep.
func prune(d string, now time.Time) {
	entries, _ := os.ReadDir(d)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > keep {
			os.Remove(filepath.Join(d, e.Name()))
		}
	}
}
