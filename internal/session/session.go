// Package session measures the context of the agent session a bmesh
// command runs in, from what its harness records, and keeps which nudge
// the session last heard (094). A harness bmesh can't read, or a record it
// doesn't recognise, measures nothing: the reader is silent, never wrong.
package session

import (
	"bytes"
	"encoding/json"
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
	n, ok := claudeTokens(path)
	if !ok {
		return Reading{}, false
	}
	return Reading{Harness: "claude", ID: id, Tokens: n}, true
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

// claudeTokens is the context of the newest assistant message on the
// session's own thread: its usage's input_tokens + cache_read_input_tokens
// + cache_creation_input_tokens. A compaction newer than any such message
// measures nothing, as does a message whose usage sums to 0 (one Claude
// Code makes up, not the model's).
func claudeTokens(path string) (int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, false
	}
	size := st.Size()
	for window := int64(tailFirst); ; window *= 4 {
		start := max(0, size-window)
		buf := make([]byte, size-start)
		if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
			return 0, false
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
		if n, found, ok := newestUsage(buf); found {
			return n, ok
		}
		if start == 0 || window >= tailMax {
			return 0, false
		}
	}
}

// record is what claudeTokens reads of a transcript line.
type record struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	IsSidechain bool   `json:"isSidechain"`
	Message     *struct {
		Usage *struct {
			Input       int `json:"input_tokens"`
			CacheRead   int `json:"cache_read_input_tokens"`
			CacheCreate int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// newestUsage scans buf's lines from the last: found once it reaches an
// assistant message with usage (ok, its context) or a compaction (not
// ok).
func newestUsage(buf []byte) (n int, found, ok bool) {
	for len(buf) > 0 {
		var line []byte
		if i := bytes.LastIndexByte(bytes.TrimRight(buf, "\n"), '\n'); i >= 0 {
			line, buf = buf[i+1:], buf[:i]
		} else {
			line, buf = buf, nil
		}
		if !bytes.Contains(line, []byte("assistant")) && !bytes.Contains(line, []byte("compact_boundary")) {
			continue
		}
		var r record
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		switch {
		case r.Type == "system" && r.Subtype == "compact_boundary" && !r.IsSidechain:
			return 0, true, false
		case r.Type == "assistant" && !r.IsSidechain && r.Message != nil && r.Message.Usage != nil:
			u := r.Message.Usage
			if sum := u.Input + u.CacheRead + u.CacheCreate; sum > 0 {
				return sum, true, true
			}
		}
	}
	return 0, false, false
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

// Advice is what a session at level is told to do; "" below nudge_from.
func Advice(c config.ContextConfig, level int) string {
	switch {
	case level <= 0:
		return ""
	case c.CompactSoon > 0 && level >= c.CompactSoon:
		return "Compact at the next safe point, after updating your state file."
	case c.CompactFrom > 0 && level >= c.CompactFrom:
		return "Compact at your next milestone, after updating your state file; never while exploring or investigating."
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

// Heard records that the session has heard r and returns the line it is
// owed: "" unless r's level is above the one it last heard. A level below
// the last (a compaction) is recorded silently, so each crossing is said
// again after it. The state is <dir>/context/<session>, dir the state dir;
// a level it can't record is never said, so a failing disk can't repeat it.
func Heard(dir string, c config.ContextConfig, r Reading, now time.Time) string {
	if c.NudgeFrom <= 0 {
		return ""
	}
	level := Level(c, r.Tokens)
	d := filepath.Join(dir, "context")
	p := filepath.Join(d, r.ID)
	last := 0
	if b, err := os.ReadFile(p); err == nil {
		last, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	if level == last {
		return ""
	}
	if os.MkdirAll(d, 0o755) != nil || writeAtomic(p, []byte(strconv.Itoa(level)+"\n")) != nil {
		return ""
	}
	prune(d, now)
	if level < last {
		return ""
	}
	return strings.TrimSpace("bmesh: this session's context is " + K(r.Tokens) + ". " + Advice(c, level))
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
