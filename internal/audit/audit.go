package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Audit is what a Claude Code session's transcript says it did.
type Audit struct {
	Session    string    `json:"session"`
	Transcript string    `json:"transcript"`
	Cwd        string    `json:"cwd,omitempty"`   // where the session ran, from its first line that names one
	Rules      []string  `json:"rules,omitempty"` // the rule sets on the bmesh:rules marker of Cwd's AGENTS.md, filled by the caller
	Calls      int       `json:"calls"`           // the main thread's model calls
	Tokens     int       `json:"tokens"`          // its context at its newest call
	Read       int64     `json:"read"`            // what its calls read, summed
	Subagents  Subagents `json:"subagents"`
	Turns      []Turn    `json:"turns"`
}

// Subagents are the session's subagents, summed apart from the main thread.
type Subagents struct {
	Count int   `json:"count"`
	Calls int   `json:"calls"`
	Read  int64 `json:"read"` // what their calls read, summed
}

// Turn is one request: a message the operator (or a harness on their
// behalf) sent the main thread, and what followed up to the next.
type Turn struct {
	N         int    `json:"n"`
	At        string `json:"at"`
	Words     string `json:"words"` // the message's first line, clipped
	Named     Rung   `json:"named,omitempty"`
	NamedLine string `json:"named_line,omitempty"`
	Implied   Rung   `json:"implied"`
	Rule      string `json:"rule"`
	Mismatch  bool   `json:"mismatch,omitempty"`
	Facts     Facts  `json:"facts"`
	Read      []Read `json:"read"`              // what it read before its first action
	Other     Other  `json:"other"`             // the rest of its tool output before then
	Acted     string `json:"acted,omitempty"`   // its first action that changes something
	Context   int    `json:"context,omitempty"` // the context, in tokens, of the call that took it
}

// Read is one thing a turn read before it acted: a skill it loaded, a file
// (whole or a range), a bmesh command's output.
type Read struct {
	Kind  string `json:"kind"` // skill, file or bmesh
	Name  string `json:"name"`
	Range string `json:"range,omitempty"` // a file's: whole, or lines a-b
	Words int    `json:"words"`
}

// Other is the rest of the tool output a turn read before it acted, summed.
type Other struct {
	Calls int `json:"calls"`
	Words int `json:"words"`
}

// line is what the reader takes of a transcript line.
type line struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	IsSidechain      bool            `json:"isSidechain"`
	IsMeta           bool            `json:"isMeta"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	UUID             string          `json:"uuid"`
	Timestamp        string          `json:"timestamp"`
	Cwd              string          `json:"cwd"`
	SessionID        string          `json:"sessionId"`
	SourceToolUseID  string          `json:"sourceToolUseID"`
	ToolUseResult    json.RawMessage `json:"toolUseResult"`
	Message          *struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			Input       int `json:"input_tokens"`
			CacheRead   int `json:"cache_read_input_tokens"`
			CacheCreate int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type content struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func (l line) blocks() ([]content, string) {
	if l.Message == nil || len(l.Message.Content) == 0 {
		return nil, ""
	}
	if l.Message.Content[0] == '"' {
		var s string
		json.Unmarshal(l.Message.Content, &s)
		return nil, s
	}
	var b []content
	json.Unmarshal(l.Message.Content, &b)
	return b, ""
}

func (l line) context() int {
	if l.Message == nil || l.Message.Usage == nil {
		return 0
	}
	u := l.Message.Usage
	return u.Input + u.CacheRead + u.CacheCreate
}

// skipped are the openings of a user message that is no request: a slash
// command's record, a background task's notice, an interrupt.
var skipped = []string{"<command-name>", "<command-message>", "<local-command-", "<task-notification>", "[Request interrupted"}

// request is the words of a message that starts a turn, ok false for any
// other line.
func (l line) request() (string, bool) {
	if l.Type != "user" || l.IsMeta || l.IsSidechain || l.IsCompactSummary {
		return "", false
	}
	blocks, s := l.blocks()
	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			return "", false
		case "text":
			s += b.Text
		}
	}
	s = strings.TrimSpace(s)
	for _, p := range skipped {
		if strings.HasPrefix(s, p) {
			return "", false
		}
	}
	return s, s != ""
}

// use is a tool call waiting on its result.
type use struct {
	name    string
	input   map[string]any
	before  bool // called before the turn's first action
	context int
	skill   int    // a skill's read in its turn's Read, plus one; its words come with its text
	acts    string // what it changes, "" for a read; a refused call changed nothing
}

// File reads a transcript, the main thread's from path and its subagents'
// from <dir>/<session>/subagents/*.jsonl beside it.
func File(path string) (*Audit, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	a, err := Transcript(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	a.Transcript = path
	if a.Session == "" {
		a.Session = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents", "*.jsonl"))
	for _, s := range subs {
		sf, err := os.Open(s)
		if err != nil {
			continue
		}
		calls, read := spend(sf)
		sf.Close()
		a.Subagents.Count++
		a.Subagents.Calls += calls
		a.Subagents.Read += read
	}
	return a, nil
}

// each calls f with each line of r that parses, once per uuid: a resumed
// session's transcript writes earlier lines again.
func each(r io.Reader, f func(line)) error {
	br := bufio.NewReaderSize(r, 1<<20)
	seen := map[string]bool{}
	for {
		b, err := br.ReadBytes('\n')
		if b = bytes.TrimSpace(b); len(b) > 0 {
			var l line
			if json.Unmarshal(b, &l) == nil && !seen[l.UUID] {
				seen[l.UUID] = l.UUID != ""
				f(l)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// spend is a subagent transcript's model calls and what they read.
func spend(r io.Reader) (calls int, read int64) {
	seen := map[string]bool{}
	each(r, func(l line) {
		if l.Type == "assistant" && l.context() > 0 && !seen[l.Message.ID] {
			seen[l.Message.ID] = l.Message.ID != ""
			calls++
			read += int64(l.context())
		}
	})
	return calls, read
}

// Transcript reads a Claude Code transcript: the turns of its main thread
// (lines marked isSidechain are a subagent's, summed apart).
func Transcript(r io.Reader) (*Audit, error) {
	a := &Audit{Turns: []Turn{}}
	var t *Turn
	var texts []string
	uses := map[string]*use{}
	seen := map[string]bool{}
	ctx := 0
	end := func() {
		if t == nil {
			return
		}
		finish(t, texts)
		a.Turns = append(a.Turns, *t)
		t, texts = nil, nil
	}
	err := each(r, func(l line) {
		if a.Cwd == "" && l.Cwd != "" {
			a.Cwd = l.Cwd
		}
		if a.Session == "" && l.SessionID != "" {
			a.Session = l.SessionID
		}
		if l.Type == "assistant" && l.context() > 0 && !seen[l.Message.ID] {
			seen[l.Message.ID] = l.Message.ID != ""
			if l.IsSidechain {
				a.Subagents.Calls++
				a.Subagents.Read += int64(l.context())
			} else {
				a.Calls++
				a.Read += int64(l.context())
				a.Tokens = l.context()
			}
		}
		if l.IsSidechain {
			return
		}
		if l.Type == "assistant" && l.context() > 0 {
			ctx = l.context()
		}
		if words, ok := l.request(); ok {
			end()
			first, _, _ := strings.Cut(words, "\n")
			t = &Turn{N: len(a.Turns) + 1, At: l.Timestamp, Words: clip(first, 100), Read: []Read{}}
			return
		}
		if t == nil {
			return
		}
		blocks, _ := l.blocks()
		switch l.Type {
		case "assistant":
			for _, b := range blocks {
				switch b.Type {
				case "text":
					texts = append(texts, b.Text)
				case "tool_use":
					u := &use{name: b.Name, before: t.Acted == "", context: ctx}
					json.Unmarshal(b.Input, &u.input)
					u.acts = acts(u)
					uses[b.ID] = u
				}
			}
		case "user":
			if l.IsMeta && l.SourceToolUseID != "" {
				// A skill's text, sent after its call's result.
				if u := uses[l.SourceToolUseID]; u != nil && u.skill > 0 && u.skill <= len(t.Read) {
					_, s := l.blocks()
					for _, b := range blocks {
						s += b.Text
					}
					t.Read[u.skill-1].Words += len(strings.Fields(s))
				}
				return
			}
			for _, b := range blocks {
				if b.Type != "tool_result" {
					continue
				}
				if u := uses[b.ToolUseID]; u != nil {
					result(t, u, b, l.ToolUseResult)
				}
			}
		}
	})
	end()
	return a, err
}

// acts is what a tool call changes, "" when it only reads: an Edit or
// Write, a commit, a push, a bmesh command that starts or changes
// something.
func acts(u *use) string {
	switch u.name {
	case "Edit", "Write", "NotebookEdit", "MultiEdit":
		p, _ := u.input["file_path"].(string)
		return u.name + " " + p
	case "Bash":
		cmd, _ := u.input["command"].(string)
		return shellActs(cmd)
	}
	return ""
}

// result takes a tool call's result into its turn: the first action, when
// it is one that succeeded; the facts its success sets; and, for a call
// before the first action, what it read.
func result(t *Turn, u *use, b content, raw json.RawMessage) {
	words := len(strings.Fields(resultText(b.Content)))
	if u.acts != "" {
		if !b.IsError && t.Acted == "" {
			t.Acted, t.Context = u.acts, u.context
		}
		if !b.IsError {
			u.before = false
		}
	}
	cmd, _ := u.input["command"].(string)
	if u.name == "Bash" && !b.IsError {
		for _, c := range bmeshCalls(cmd) {
			switch {
			case c.cmd == "build" || c.cmd == "lead" && c.sub() == "start":
				t.Facts.Leads = true
			case c.cmd == "ideate":
				t.Facts.Round = true
			}
		}
		if pushesMain(cmd) {
			t.Facts.PushedMain = true
		}
		if strings.Contains(cmd, "docs/proposals/") && newSpec(cmd) {
			t.Facts.NewSpec = true
		}
	}
	if u.name == "Write" && !b.IsError {
		p, _ := u.input["file_path"].(string)
		if strings.Contains(filepath.ToSlash(p), "docs/proposals/") && strings.Contains(resultText(b.Content), "created") {
			t.Facts.NewSpec = true
		}
	}
	if !u.before {
		return
	}
	switch u.name {
	case "Skill":
		name, _ := u.input["skill"].(string)
		t.Read = append(t.Read, Read{Kind: "skill", Name: name})
		u.skill = len(t.Read)
	case "Read":
		p, _ := u.input["file_path"].(string)
		r := Read{Kind: "file", Name: p, Range: "whole", Words: words}
		var res struct {
			File *struct {
				Content    string `json:"content"`
				StartLine  int    `json:"startLine"`
				NumLines   int    `json:"numLines"`
				TotalLines int    `json:"totalLines"`
			} `json:"file"`
		}
		if json.Unmarshal(raw, &res) == nil && res.File != nil {
			r.Words = len(strings.Fields(res.File.Content))
			if f := res.File; f.StartLine > 1 || f.NumLines < f.TotalLines {
				r.Range = fmt.Sprintf("lines %d-%d", f.StartLine, f.StartLine+f.NumLines-1)
			}
		} else if _, off := u.input["offset"]; off {
			r.Range = "a range"
		} else if _, lim := u.input["limit"]; lim {
			r.Range = "a range"
		}
		t.Read = append(t.Read, r)
	case "Bash":
		if calls := bmeshCalls(cmd); len(calls) > 0 {
			names := make([]string, len(calls))
			for i, c := range calls {
				names[i] = c.String()
			}
			t.Read = append(t.Read, Read{Kind: "bmesh", Name: strings.Join(names, "; "), Words: words})
			return
		}
		if f, rng, ok := fileRead(cmd); ok {
			t.Read = append(t.Read, Read{Kind: "file", Name: f, Range: rng, Words: words})
			return
		}
		t.Other.Calls++
		t.Other.Words += words
	default:
		t.Other.Calls++
		t.Other.Words += words
	}
}

// specWrite is a redirect into a spec file under docs/proposals/.
var specWrite = regexp.MustCompile(`>\s*['"]?[^\s'";&|]*docs/proposals/[^\s'";&|/]+\.md`)

// newSpec reports whether a command line writes a file under
// docs/proposals/ through a redirect, cp or mv, as a new spec is made from
// `bmesh spec template`.
func newSpec(s string) bool {
	if specWrite.MatchString(s) && !strings.Contains(specWrite.FindString(s), "README.md") {
		return true
	}
	for _, words := range commands(s) {
		if n := len(words); n > 2 && (filepath.Base(words[0]) == "cp" || filepath.Base(words[0]) == "mv") &&
			strings.Contains(words[n-1], "docs/proposals/") && strings.HasSuffix(words[n-1], ".md") {
			return true
		}
	}
	return false
}

func resultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		json.Unmarshal(raw, &s)
		return s
	}
	var bs []content
	json.Unmarshal(raw, &bs)
	var b strings.Builder
	for _, c := range bs {
		b.WriteString(c.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

// finish sets a turn's named rung, from the first of its replies that names
// one, and its implied rung, from its facts and its last reply.
func finish(t *Turn, texts []string) {
	for _, s := range texts {
		if r, l, ok := Named(s); ok {
			t.Named, t.NamedLine = r, clip(l, 120)
			break
		}
	}
	if len(texts) > 0 {
		t.Facts.EndsAsking = EndsAsking(texts[len(texts)-1])
	}
	t.Implied, t.Rule = Implied(t.Facts)
	t.Mismatch = t.Named != "" && t.Named != t.Implied
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}
