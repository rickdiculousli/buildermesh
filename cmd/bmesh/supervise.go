package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// supervisor runs `bmesh serve` as a child and starts it again from the
// same path whenever it exits, whatever the exit code: after an update on
// Windows, where serve exits 0 for the new binary to be started, and after
// a crash anywhere.
type supervisor struct {
	exe  string   // the binary, read once at start: an update moves the running file aside
	args []string // the child's arguments
	out  io.Writer

	wait    time.Duration // between starts (RestartSec=5)
	backoff time.Duration // between starts after quickN quick exits in a row
	quick   time.Duration // a run shorter than this is a quick exit
	quickN  int
	grace   time.Duration // how long a stopped child has to exit before it is killed
}

func newSupervisor(exe string, args []string, out io.Writer) *supervisor {
	return &supervisor{exe: exe, args: args, out: out,
		wait: 5 * time.Second, backoff: time.Minute, quick: time.Minute, quickN: 5, grace: 30 * time.Second}
}

// logf writes one of the supervisor's own lines beside the child's output.
func (s *supervisor) logf(format string, a ...any) {
	fmt.Fprintf(s.out, "%s bmesh serve --restart: %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, a...))
}

// run starts the child until ctx ends, then stops it: a stop signal on
// Unix, a kill on Windows, a kill on Unix after grace.
func (s *supervisor) run(ctx context.Context) error {
	out := s.out
	if _, ok := out.(*os.File); !ok {
		// The child's output is copied in by exec while the supervisor
		// writes its own lines.
		out = &lockedWriter{w: out}
		s.out = out
	}
	quick := 0
	for {
		start := time.Now()
		cmd := exec.Command(s.exe, s.args...)
		cmd.Env = append(os.Environ(), "BMESH_SUPERVISED=1")
		cmd.Stdout, cmd.Stderr = out, out
		cmd.WaitDelay = time.Second // a process serve left running may hold the pipe
		childAttr(cmd)
		if err := cmd.Start(); err != nil {
			s.logf("start %s: %v", s.exe, err)
		} else {
			s.logf("started %s (pid %d)", s.exe, cmd.Process.Pid)
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				s.logf("serve exited (%s)", exitText(err, cmd.ProcessState))
			case <-ctx.Done():
				s.stop(cmd, done)
				return nil
			}
		}
		if time.Since(start) < s.quick {
			quick++
		} else {
			quick = 0
		}
		d := s.wait
		if quick >= s.quickN {
			d = s.backoff
		}
		s.logf("starting again in %s", d)
		select {
		case <-ctx.Done():
			s.logf("stopped")
			return nil
		case <-time.After(d):
		}
	}
}

// stop ends the running child, killing it when it outlives grace.
func (s *supervisor) stop(cmd *exec.Cmd, done <-chan error) {
	if err := stopChild(cmd.Process); err != nil {
		cmd.Process.Kill()
	}
	select {
	case err := <-done:
		s.logf("stopped; serve exited (%s)", exitText(err, cmd.ProcessState))
	case <-time.After(s.grace):
		cmd.Process.Kill()
		err := <-done
		s.logf("stopped; serve killed after %s (%s)", s.grace, exitText(err, cmd.ProcessState))
	}
}

// exitText is how a child's end reads in the log: its exit code when it
// exited, even when Wait also says a process it left held the output past
// WaitDelay; else Wait's error (a signal, on Unix).
func exitText(err error, st *os.ProcessState) string {
	switch {
	case st != nil && st.Exited():
		return fmt.Sprintf("exit %d", st.ExitCode())
	case err == nil:
		return "exit 0"
	}
	return err.Error()
}

// childArgs is serve's own command line for the child: --restart and
// --detach dropped, and a --config given before `serve` put back.
func childArgs(configPath string, rest []string) []string {
	var own []string
	hasConfig := false
	for _, a := range rest {
		name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch {
		case !strings.HasPrefix(a, "-"):
		case name == "restart" || name == "detach":
			continue
		case name == "config":
			hasConfig = true
		}
		own = append(own, a)
	}
	args := []string{"serve"}
	if configPath != "" && !hasConfig {
		args = append(args, "--config", configPath)
	}
	return append(args, own...)
}

// lockedWriter serialises writes to w.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// serveLogMax is the size at which serve.log moves to serve.prev.log.
const serveLogMax = 10 << 20

// rotLog appends to path and, before a write would take it past max, moves
// it to prevPath(path), replacing the one there, and starts it afresh. A
// log it cannot rename (open elsewhere on Windows) is truncated instead,
// and one it cannot open drops the writes: the supervisor keeps running.
type rotLog struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

func openRotLog(path string, max int64) (*rotLog, error) {
	l := &rotLog{path: path, max: max}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	l.f = f
	if st, err := f.Stat(); err == nil {
		l.size = st.Size()
	}
	return l, nil
}

// prevPath is where rotLog keeps the previous log: serve.log's is
// serve.prev.log.
func prevPath(path string) string {
	if p, ok := strings.CutSuffix(path, ".log"); ok {
		return p + ".prev.log"
	}
	return path + ".prev"
}

func (l *rotLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size > 0 && l.size+int64(len(p)) > l.max {
		l.rotate()
	}
	if l.f == nil {
		return len(p), nil
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

func (l *rotLog) rotate() {
	if l.f != nil {
		l.f.Close()
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if err := os.Rename(l.path, prevPath(l.path)); err != nil {
		flags |= os.O_TRUNC
	}
	l.f, l.size = nil, 0
	if f, err := os.OpenFile(l.path, flags, 0o644); err == nil {
		l.f = f
	}
}

func (l *rotLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	return l.f.Close()
}
