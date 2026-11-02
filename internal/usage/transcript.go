package usage

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/audit"
)

// Phases of a coordinator's calls.
const (
	Design = "design"
	Exec   = "exec"
)

// State is what a file's reader carries from one pass to the next.
type State struct {
	LastID  string   `json:"last_id,omitempty"` // the newest call's message id: a later line of it is no new call
	Phase   string   `json:"phase,omitempty"`   // a coordinator's phase now
	Uses    []string `json:"uses,omitempty"`    // the newest message's tool uses, the next call's cause
	Pending string   `json:"pending,omitempty"` // message, wake or compact: the next call's cause, over Uses
	// Spawned is each Agent or Task call's tool_use id with the phase its
	// session was in when it was made: its subagent's phase.
	Spawned  map[string]string `json:"spawned,omitempty"`
	Prompted bool              `json:"prompted,omitempty"` // a subagent file's first user message was read
}

// how is how a file is read: Track moves a coordinator's main thread
// between phases; Fixed is the one phase of a coordinator's subagent; Sub
// is a subagent file's id, set on its calls and marks; Since drops older
// calls and marks.
type how struct {
	Track bool
	Fixed string
	Sub   string
	Since time.Time
}

// read is what one segment of a file gives: its calls and marks, the
// offset it read up to and the lines it does not read.
type read struct {
	calls   []Call
	marks   []Mark
	end     int64
	skipped int
}

// parsed is one line of a buffer, with its offsets in the file.
type parsed struct {
	off, end int64
	l        audit.Line
	ok       bool // it parsed
	request  bool // an api-request* line, not parsed further
}

// lines splits buf, which starts at file offset off and ends at a newline,
// into its lines, parsed. An api-request line is only marked: its blob is
// most of a transcript's bytes and none of its use.
func lines(buf []byte, off int64) []parsed {
	var out []parsed
	for len(buf) > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			i = len(buf) - 1
		}
		b := bytes.TrimSpace(buf[:i+1])
		p := parsed{off: off, end: off + int64(i+1)}
		switch {
		case len(b) == 0:
			p.ok = true
		case bytes.HasPrefix(b, []byte(`{"type":"api-request`)):
			p.ok, p.request = true, true
		default:
			p.ok = json.Unmarshal(b, &p.l) == nil
		}
		out = append(out, p)
		off, buf = p.end, buf[i+1:]
	}
	return out
}

// isCall reports whether l is a model call's line: an assistant message
// whose usage sums above 0 (a message Claude Code makes up has all zeros).
func isCall(l audit.Line) bool {
	if l.Type != "assistant" || l.Message == nil || l.Message.Usage == nil {
		return false
	}
	u := l.Message.Usage
	return u.Input+u.CacheRead+u.CacheCreate+u.Output > 0
}

func callID(l audit.Line) string { return cmpOr(l.Message.ID, l.UUID) }

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// cut is how many of ls a pass may read: all of them, unless the newest
// call began among them and nothing after it shows it finished (a later
// call, an api-request, a compaction) while the file is fresh; then up to
// that call's first line, so its tool uses and phase are read whole. last
// is the newest call id read in an earlier pass.
func cut(ls []parsed, last string, quiet bool) int {
	if quiet {
		return len(ls)
	}
	for i := len(ls) - 1; i >= 0; i-- {
		p := ls[i]
		if p.request || p.ok && p.l.Type == "system" && p.l.Subtype == "compact_boundary" {
			return len(ls)
		}
		if p.ok && isCall(p.l) {
			id := callID(p.l)
			if id == last {
				return len(ls)
			}
			first := i
			for j := i - 1; j >= 0; j-- {
				if q := ls[j]; q.ok && isCall(q.l) && callID(q.l) == id {
					first = j
				} else if q.request || q.ok && isCall(q.l) {
					break
				}
			}
			return first
		}
	}
	return len(ls)
}

// segment reads ls in order into st until maxCalls calls are read and
// another would start; its end is the offset after the last line read.
func (st *State) segment(ls []parsed, h how, end int64) (read, []parsed) {
	r := read{end: end}
	cur := -1 // the index in r.calls of the call whose lines are being read
	for i, p := range ls {
		if !p.ok {
			r.skipped++
			continue
		}
		l := p.l
		switch {
		case p.request:
		case l.Type == "system" && l.Subtype == "compact_boundary":
			st.Pending = "compact"
			if at, ok := keep(l.Timestamp, h.Since); ok {
				r.marks = append(r.marks, Mark{At: at, Kind: "compact", Sub: h.Sub})
			}
		case l.Type == "user":
			st.user(l, h)
		case isCall(l):
			id := callID(l)
			if id != st.LastID {
				if len(r.calls) == maxCalls {
					r.end = p.off
					return r, ls[i:]
				}
				st.LastID, cur = id, -1
				c := st.call(l, h)
				if _, ok := keep(c.At, h.Since); ok {
					r.calls = append(r.calls, c)
					cur = len(r.calls) - 1
				}
			}
			st.uses(l, h, r.calls, cur)
		case l.Type == "assistant" && l.Message != nil && l.Message.Usage == nil:
			r.skipped++ // a model's message with no usage: a format this reader doesn't know
		}
	}
	return r, nil
}

// keep is at, when it is a time at or after since.
func keep(at string, since time.Time) (string, bool) {
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return "", false
	}
	return at, !t.Before(since)
}

// user reads a user line: the operator's request starts a turn in design,
// a background task's notice starts one in exec; either resets the cause.
// A subagent's first message is its request.
func (st *State) user(l audit.Line, h how) {
	if h.Sub != "" {
		if st.Prompted {
			return
		}
		l.IsSidechain = false // Request reads a main thread's lines
		if _, ok := l.Request(); ok {
			st.Prompted, st.Pending, st.Uses = true, "message", nil
		}
		return
	}
	if _, ok := l.Request(); ok {
		st.Pending, st.Uses = "message", nil
		if h.Track {
			st.Phase = Design
		}
		return
	}
	if !l.IsMeta && strings.HasPrefix(strings.TrimSpace(text(l)), "<task-notification>") {
		st.Pending, st.Uses = "wake", nil
		if h.Track {
			st.Phase = Exec
		}
	}
}

// text is a user line's text, its blocks' joined.
func text(l audit.Line) string {
	blocks, s := l.Blocks()
	for _, b := range blocks {
		if b.Type == "text" {
			s += b.Text
		}
	}
	return s
}

// call is l as a new call, with its cause and phase, and resets the cause.
func (st *State) call(l audit.Line, h how) Call {
	u := l.Message.Usage
	c := Call{At: l.Timestamp, ID: callID(l), Model: l.Message.Model,
		In: int64(u.Input), CR: int64(u.CacheRead), CW: int64(u.CacheCreate), Out: int64(u.Output), Sub: h.Sub}
	c.Ctx = c.In + c.CR + c.CW
	c.Cause = st.Pending
	if c.Cause == "" && len(st.Uses) > 0 {
		c.Cause = st.Uses[0]
		if len(st.Uses) > 1 {
			c.Cause += " +" + strconv.Itoa(len(st.Uses)-1)
		}
	}
	c.Cause = clip(c.Cause, 60)
	st.Pending, st.Uses = "", nil
	switch {
	case h.Track:
		c.Phase = cmpOr(st.Phase, Design)
	case h.Fixed != "":
		c.Phase = h.Fixed
	}
	return c
}

// uses takes l's tool uses: each is a word of the next call's cause; on a
// coordinator's main thread the first that changes something moves it to
// exec, the call making it included, and each Agent call keeps the phase
// its subagent takes.
func (st *State) uses(l audit.Line, h how, calls []Call, cur int) {
	blocks, _ := l.Blocks()
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		var in map[string]any
		json.Unmarshal(b.Input, &in)
		st.Uses = append(st.Uses, cause(b.Name, in))
		if !h.Track {
			continue
		}
		if st.Phase != Exec && changes(b.Name, in) {
			st.Phase = Exec
			if cur >= 0 {
				calls[cur].Phase = Exec
			}
		}
		if b.Name == "Agent" || b.Name == "Task" {
			if st.Spawned == nil {
				st.Spawned = map[string]string{}
			}
			st.Spawned[b.ID] = cmpOr(st.Phase, Design)
		}
	}
}

// cause names one tool use, never a command's text: the tool and a file's
// base name, a shell command's description, a skill's name, an Agent
// call's description.
func cause(name string, in map[string]any) string {
	s := func(k string) string { v, _ := in[k].(string); return strings.TrimSpace(v) }
	switch name {
	case "Read", "Write", "Edit", "NotebookEdit", "MultiEdit":
		p := cmpOr(s("file_path"), s("notebook_path"))
		if p == "" {
			return name
		}
		return name + " " + filepath.Base(p)
	case "Bash":
		if d := s("description"); d != "" {
			return "Bash: " + d
		}
		return "Bash"
	case "Skill":
		return strings.TrimSpace("Skill " + s("skill"))
	case "Agent", "Task":
		return strings.TrimSpace("Agent " + s("description"))
	}
	return name
}

// changes reports whether a tool call changes something, as the phase
// rule reads it: a shell command audit reads as acting, or an edit of a
// file git tracks or would, outside docs/proposals/ and docs/design/.
func changes(name string, in map[string]any) bool {
	if audit.Acts(name, in) == "" {
		return false
	}
	switch name {
	case "Edit", "Write", "NotebookEdit", "MultiEdit":
		p, _ := in["file_path"].(string)
		return tracked(p)
	}
	return true
}

// tracked reports whether p is a file inside a git work tree that git
// doesn't ignore, outside docs/proposals/ and docs/design/. A path whose
// dir is gone counts as tracked.
func tracked(p string) bool {
	slash := filepath.ToSlash(p)
	if p == "" || strings.Contains(slash, "docs/proposals/") || strings.Contains(slash, "docs/design/") {
		return false
	}
	dir := filepath.Dir(p)
	if _, err := os.Stat(dir); err != nil {
		return true
	}
	if exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Run() != nil {
		return false
	}
	// check-ignore exits 0 for an ignored path.
	return exec.Command("git", "-C", dir, "check-ignore", "-q", p).Run() != nil
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
