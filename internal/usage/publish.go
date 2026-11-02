package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// passBytes bounds what one pass reads; a var for the tests.
var passBytes int64 = 64 << 20

const (
	// Keep is how far back calls are kept: the stream's age and the first
	// pass's reach.
	Keep = 30 * 24 * time.Hour
	// quiet is how long a file goes unwritten before its newest call is
	// read without a later line to show it finished.
	quiet = time.Minute
)

// Env is what a pass needs.
type Env struct {
	Client    *mesh.Client
	Host      string
	Claude    string            // the Claude Code config dir: CLAUDE_CONFIG_DIR, else ~/.claude
	StateDir  string            // the cursor is <StateDir>/usage/cursor.json
	Checkouts string            // routine instances' checkouts root ([runner] checkouts, expanded)
	Repos     map[string]string // the registry's rows: name to checkout path
	Log       *slog.Logger
	Now       func() time.Time
}

// cursor is what the passes have read, per transcript file under
// <Claude>/projects.
type cursor struct {
	Files map[string]*entry `json:"files"`
}

// entry is one file's: its offset, which agent it is (Agent "" while
// undecided), and its reader's state.
type entry struct {
	Off     int64  `json:"off"`
	Agent   string `json:"agent,omitempty"`
	Role    string `json:"role,omitempty"`
	Repo    string `json:"repo,omitempty"`
	Session string `json:"session,omitempty"`
	Sub     string `json:"sub,omitempty"`
	Desc    string `json:"desc,omitempty"`
	Fixed   string `json:"fixed,omitempty"` // a coordinator's subagent's phase
	Logged  bool   `json:"logged,omitempty"`
	State
}

// pass is one run over the transcripts.
type pass struct {
	env   Env
	cur   *cursor
	nodes []mesh.Node // this box's, read once a pass before any file is decided
}

func (p *pass) cursorPath() string { return filepath.Join(p.env.StateDir, "usage", "cursor.json") }

func (p *pass) projects() string { return filepath.Join(p.env.Claude, "projects") }

func (p *pass) rel(path string) string {
	r, err := filepath.Rel(p.projects(), path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}

// here is the nodes on this box.
func (p *pass) here() []mesh.Node { return p.nodes }

// readNodes reads the nodes on this box; an error leaves every file
// undecided for the next pass, never decided without them.
func (p *pass) readNodes(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	nodes, err := mesh.ListNodes(ctx, p.env.Client)
	if err != nil {
		return fmt.Errorf("usage: read nodes: %w", err)
	}
	for _, n := range nodes {
		if n.Host == p.env.Host {
			p.nodes = append(p.nodes, n)
		}
	}
	return nil
}

// Pass reads what grew in this box's transcripts and publishes it: each
// file from its offset to its last newline, the files modified in the last
// Keep on the first pass (oldest first), at most passBytes a pass. The
// cursor moves only after the stream acks a message.
func Pass(ctx context.Context, env Env) error {
	if env.Client == nil {
		return nil // no hub: nothing to publish to
	}
	if env.Now == nil {
		env.Now = time.Now
	}
	if env.Log == nil {
		env.Log = slog.New(slog.DiscardHandler)
	}
	p := &pass{env: env, cur: &cursor{Files: map[string]*entry{}}}
	if _, err := os.Stat(p.projects()); err != nil {
		return nil // no transcripts on this box: the cursor is left alone
	}
	if b, err := os.ReadFile(p.cursorPath()); err == nil {
		if err := json.Unmarshal(b, p.cur); err != nil {
			return fmt.Errorf("usage: %s: %w", p.cursorPath(), err)
		}
		if p.cur.Files == nil {
			p.cur.Files = map[string]*entry{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	now := env.Now()
	var mains, subs []file
	seen := map[string]bool{}
	for _, glob := range []string{"*/*.jsonl", "*/*/subagents/*.jsonl"} {
		paths, _ := filepath.Glob(filepath.Join(p.projects(), glob))
		for _, path := range paths {
			st, err := os.Stat(path)
			if err != nil || !st.Mode().IsRegular() {
				continue
			}
			rel := p.rel(path)
			seen[rel] = true
			e := p.cur.Files[rel]
			if e == nil && now.Sub(st.ModTime()) > Keep || e != nil && e.Off >= st.Size() {
				continue
			}
			f := file{path, st.Size(), st.ModTime()}
			if glob == "*/*.jsonl" {
				mains = append(mains, f)
			} else {
				subs = append(subs, f)
			}
		}
	}
	for rel := range p.cur.Files {
		if !seen[rel] {
			delete(p.cur.Files, rel)
		}
	}
	for _, fs := range [][]file{mains, subs} {
		sort.Slice(fs, func(i, j int) bool { return fs[i].mod.Before(fs[j].mod) })
	}
	for _, f := range append(mains, subs...) {
		if e := p.cur.Files[p.rel(f.path)]; e == nil || e.Agent == "" {
			if err := p.readNodes(ctx); err != nil {
				return err
			}
			break
		}
	}
	budget := passBytes
	for i, f := range append(mains, subs...) {
		if budget <= 0 || ctx.Err() != nil {
			break
		}
		rel := p.rel(f.path)
		e := p.cur.Files[rel]
		if e == nil {
			e = &entry{}
			p.cur.Files[rel] = e
		}
		if e.Agent == "" {
			ok := false
			if i < len(mains) {
				ok = p.assignMain(f.path, e)
			} else {
				ok = p.assignSub(f.path, e)
			}
			if !ok {
				continue
			}
		}
		n, err := p.file(ctx, f.path, rel, e, f.size, now.Sub(f.mod) >= quiet, budget)
		budget -= n
		if err != nil {
			return err
		}
	}
	return p.save()
}

// file reads one file from its offset, at most budget bytes (more only to
// end a line that began within them), and publishes it in messages of at
// most maxCalls calls; it returns the bytes read.
func (p *pass) file(ctx context.Context, path, rel string, e *entry, size int64, quiet bool, budget int64) (int64, error) {
	fh, err := os.Open(path)
	if err != nil {
		return 0, nil // gone since the glob
	}
	defer fh.Close()
	want := min(size-e.Off, budget)
	buf := make([]byte, want)
	n, err := fh.ReadAt(buf, e.Off)
	if err != nil && err != io.EOF {
		return int64(n), err
	}
	buf = buf[:n]
	for bytes.LastIndexByte(buf, '\n') < 0 && e.Off+int64(len(buf)) < size {
		// One line longer than the budget: read on to its end.
		more := make([]byte, min(size-e.Off-int64(len(buf)), 1<<20))
		m, err := fh.ReadAt(more, e.Off+int64(len(buf)))
		buf = append(buf, more[:m]...)
		if m == 0 || err != nil && err != io.EOF {
			break
		}
	}
	read := int64(len(buf))
	i := bytes.LastIndexByte(buf, '\n')
	if i < 0 {
		return read, nil // a half-written line waits for the next pass
	}
	ls := lines(buf[:i+1], e.Off)
	ls = ls[:cut(ls, e.LastID, quiet)]
	h := how{Track: e.Role == Coordinator && e.Sub == "", Fixed: e.Fixed, Sub: e.Sub, Since: p.env.Now().Add(-Keep)}
	for len(ls) > 0 {
		st := e.State
		r, rest := st.segment(ls, h, ls[len(ls)-1].end)
		if r.skipped > 0 && !e.Logged {
			p.env.Log.Warn(fmt.Sprintf("usage: %s: skipped %d lines it does not read", rel, r.skipped))
			e.Logged = true
		}
		if len(r.calls) > 0 || len(r.marks) > 0 {
			m := Message{Agent: e.Agent, Role: e.Role, Host: p.env.Host, Repo: e.Repo, Session: e.Session,
				File: rel, Sub: e.Sub, Desc: e.Desc, Calls: r.calls, Marks: r.marks}
			if err := p.publish(ctx, m, fmt.Sprintf("%s/%s/%d", p.env.Host, rel, r.end)); err != nil {
				return read, err
			}
		}
		e.Off, e.State = r.end, st
		if err := p.save(); err != nil {
			return read, err
		}
		ls = rest
	}
	return read, nil
}

func (p *pass) publish(ctx context.Context, m Message, id string) error {
	subj := subject(m.Agent)
	if subj == "" {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := p.env.Client.JS.Publish(ctx, subj, data, jetstream.WithMsgID(id), jetstream.WithExpectStream(mesh.StreamUsage)); err != nil {
		return fmt.Errorf("usage: publish %s: %w", subj, err)
	}
	return nil
}

// save writes the cursor through a temp file and a rename.
func (p *pass) save() error {
	b, err := json.Marshal(p.cur)
	if err != nil {
		return err
	}
	path := p.cursorPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cursor-*")
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
	return os.Rename(tmp.Name(), path)
}
