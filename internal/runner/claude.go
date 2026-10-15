package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Claude runs a lead as headless `claude -p` turns in a git worktree it makes
// under Worktrees/<repo>/<thread>, with no T3 and no tmux. The session id is
// the thread id, so each later turn resumes it. Claude is the claude command
// split on spaces ("" = claude; $BMESH_CLAUDE_COMMAND overrides it for
// tests). Dir holds one state dir per thread ("" = <user cache>/bmesh/claude):
// the worktree path, the running turn's pid and number, each turn's
// turn-<n>.in, .json and .err, and queue/ for sends that arrived mid-turn.
type Claude struct {
	Claude, Worktrees, Dir string
}

func (r Claude) dir(thread string) (string, error) {
	d := r.Dir
	if d == "" {
		c, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		d = filepath.Join(c, "bmesh", "claude")
	}
	return filepath.Join(d, thread), nil
}

// known returns the thread's state dir, or ErrNoThread when it has none.
func (r Claude) known(thread string) (string, error) {
	d, err := r.dir(thread)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(d); err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoThread
		}
		return "", err
	}
	return d, nil
}

// Start makes the worktree on a new branch named for the thread (from
// s.Branch, else main), copies the brief in as the first turn and starts it
// without waiting for it. The turn inherits the caller's environment, so
// BMESH_CONFIG reaches the lead when the caller has it set.
func (r Claude) Start(ctx context.Context, s StartSpec) (Started, error) {
	wt, err := filepath.Abs(filepath.Join(r.Worktrees, filepath.Base(s.RepoPath), s.ThreadID))
	if err != nil {
		return Started{}, err
	}
	d, err := r.dir(s.ThreadID)
	if err != nil {
		return Started{}, err
	}
	brief, err := os.ReadFile(s.BriefPath)
	if err != nil {
		return Started{}, err
	}
	base := s.Branch
	if base == "" {
		base = "main"
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", s.RepoPath, "worktree", "add", "-b", s.ThreadID, wt, base).CombinedOutput(); err != nil {
		return Started{}, fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if err := os.MkdirAll(filepath.Join(d, "queue"), 0o755); err != nil {
		return Started{}, err
	}
	if err := os.WriteFile(filepath.Join(d, "worktree"), []byte(wt), 0o644); err != nil {
		return Started{}, err
	}
	args := []string{"--session-id", s.ThreadID}
	if s.Model != "" {
		args = append(args, "--model", s.Model)
	}
	if err := r.turn(d, wt, 1, string(brief), args); err != nil {
		return Started{}, err
	}
	return Started{Thread: s.ThreadID, Branch: s.ThreadID, Worktree: wt}, nil
}

// turn starts turn n in wt, detached, with text on stdin, and records its pid
// and number. args name the session (--session-id or --resume).
func (r Claude) turn(d, wt string, n int, text string, args []string) error {
	name := filepath.Join(d, "turn-"+strconv.Itoa(n))
	if err := os.WriteFile(name+".in", []byte(text), 0o644); err != nil {
		return err
	}
	in, err := os.Open(name + ".in")
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(name + ".json")
	if err != nil {
		return err
	}
	defer out.Close()
	errf, err := os.Create(name + ".err")
	if err != nil {
		return err
	}
	defer errf.Close()
	bin := r.Claude
	if bin == "" {
		bin = "claude"
	}
	c := command("BMESH_CLAUDE_COMMAND", strings.Fields(bin)...)
	argv := append(append(append([]string{}, c[1:]...), "-p", "--output-format", "json"), args...)
	argv = append(argv, "--dangerously-skip-permissions")
	cmd := exec.Command(c[0], argv...)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = wt, in, out, errf
	cmd.SysProcAttr = detached()
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reap the child when it exits, so a long-lived caller (the daemon)
	// never keeps a zombie whose pid would read as alive.
	go cmd.Wait()
	// The turn number first: a running() check between the two writes
	// must not pair the new pid with the old turn's finished output.
	if err := os.WriteFile(filepath.Join(d, "turn"), []byte(strconv.Itoa(n)), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(d, "pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
}

// lockStale is how old a lock file may be before it is taken over: a
// holder that crashed between taking it and removing it.
const lockStale = 10 * time.Second

// locked runs f holding the thread's lock file, so a Send and a Status (or
// two Sends) never both see "not running" and start turn n+1 twice.
func locked(d string, f func() error) error {
	lock := filepath.Join(d, "lock")
	for end := time.Now().Add(lockStale); ; {
		fh, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fh.Close()
			break
		}
		if !os.IsExist(err) {
			return err
		}
		if st, serr := os.Stat(lock); serr == nil && time.Since(st.ModTime()) > lockStale {
			os.Remove(lock)
			continue
		}
		if time.Now().After(end) {
			return fmt.Errorf("claude runner: %s is held", lock)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer os.Remove(lock)
	return f()
}

// queued is the queue's files, oldest first.
func queued(d string) ([]string, error) {
	ents, err := os.ReadDir(filepath.Join(d, "queue"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// enqueue puts text at the back of the queue.
func enqueue(d, text string) error {
	q := filepath.Join(d, "queue")
	if err := os.MkdirAll(q, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(q, strconv.FormatInt(time.Now().UnixNano(), 10)+".txt"), []byte(text), 0o644)
}

// drain starts the oldest queued send as turn n+1 when no turn is running;
// it reports whether one was started.
func (r Claude) drain(d, thread string) (bool, error) {
	n := readInt(filepath.Join(d, "turn"))
	if running(d, n) {
		return false, nil
	}
	names, err := queued(d)
	if err != nil || len(names) == 0 {
		return false, err
	}
	path := filepath.Join(d, "queue", names[0])
	text, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if err := r.resume(d, thread, n+1, string(text)); err != nil {
		return false, err
	}
	return true, os.Remove(path)
}

// readInt reads a file holding one integer, 0 when it is missing or garbled.
func readInt(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// running reports whether turn n is mid-turn: its process is alive and its
// output is not yet a result. Both, so a reaped or recycled pid never reads
// as running forever.
func running(d string, n int) bool {
	pid := readInt(filepath.Join(d, "pid"))
	if pid <= 0 || !alive(pid) {
		return false
	}
	_, ok := turnResult(d, n)
	return !ok
}

// turnResult is the result text of turn n, false until its output parses.
func turnResult(d string, n int) (string, bool) {
	b, err := os.ReadFile(filepath.Join(d, "turn-"+strconv.Itoa(n)+".json"))
	if err != nil {
		return "", false
	}
	return claudeResult(b)
}

// claudeResult finds the last result object in claude's JSON output: a
// single object, an array of messages, or a stream of either.
func claudeResult(b []byte) (string, bool) {
	var last string
	found := false
	take := func(raw json.RawMessage) {
		var r struct {
			Type   string `json:"type"`
			Result string `json:"result"`
		}
		if json.Unmarshal(raw, &r) == nil && r.Type == "result" {
			last, found = r.Result, true
		}
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if !errors.Is(err, io.EOF) {
				return "", false
			}
			return last, found
		}
		if raw = bytes.TrimSpace(raw); len(raw) > 0 && raw[0] == '[' {
			var arr []json.RawMessage
			if json.Unmarshal(raw, &arr) != nil {
				return "", false
			}
			for _, e := range arr {
				take(e)
			}
			continue
		}
		take(raw)
	}
}

// Status reads the thread's turn state and the newest reply. When no turn is
// running and sends are queued, it starts the oldest as the next turn: the
// daemon polls Status, so this is how queued sends drain.
func (r Claude) Status(ctx context.Context, thread string) (Status, error) {
	d, err := r.known(thread)
	if err != nil {
		return Status{}, err
	}
	n := readInt(filepath.Join(d, "turn"))
	st := Status{Known: true, Running: running(d, n)}
	if reply, ok := turnResult(d, n); ok {
		st.LastReply = reply
	} else if !st.Running && n > 0 {
		// The turn ended without a result (a failed --resume, an auth
		// error): say so with its stderr rather than show an older reply.
		errb, _ := os.ReadFile(filepath.Join(d, "turn-"+strconv.Itoa(n)+".err"))
		st.LastReply = fmt.Sprintf("[turn %d failed] %s", n, strings.TrimSpace(string(errb)))
	}
	if st.Running {
		return st, nil
	}
	err = locked(d, func() error {
		started, err := r.drain(d, thread)
		st.Running = st.Running || started
		return err
	})
	return st, err
}

// resume starts turn n of the thread's session with text.
func (r Claude) resume(d, thread string, n int, text string) error {
	wt, err := os.ReadFile(filepath.Join(d, "worktree"))
	if err != nil {
		return err
	}
	return r.turn(d, strings.TrimSpace(string(wt)), n, text, []string{"--resume", thread})
}

// Send starts the next turn with text, or queues it under queue/ when a turn
// is running or sends are already queued (so sends keep their order); Status
// starts queued sends once the turn ends.
func (r Claude) Send(ctx context.Context, thread, text string) error {
	d, err := r.known(thread)
	if err != nil {
		return err
	}
	return locked(d, func() error {
		names, err := queued(d)
		if err != nil {
			return err
		}
		n := readInt(filepath.Join(d, "turn"))
		if running(d, n) || len(names) > 0 {
			return enqueue(d, text)
		}
		return r.resume(d, thread, n+1, text)
	})
}

// Interrupt kills the running turn; nothing running is not an error.
func (r Claude) Interrupt(ctx context.Context, thread string) error {
	d, err := r.known(thread)
	if err != nil {
		return err
	}
	if !running(d, readInt(filepath.Join(d, "turn"))) {
		return nil
	}
	return kill(readInt(filepath.Join(d, "pid")))
}
