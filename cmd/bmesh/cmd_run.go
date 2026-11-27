package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/proc"
)

const runUsage = "usage: bmesh run [--timeout D] [--no-stall] -- <cmd...>"

// runTimeout is bmesh run's default bound, a kind's default; runQuitGrace
// is how long a stalled or timed-out group has between SIGQUIT and
// SIGKILL. Tests shorten the grace.
var runTimeout, runQuitGrace = 20 * time.Minute, 10 * time.Second

// cmdRun runs argv in its own process group with a checkup every
// proc.Every(timeout) on stderr, ending it at its timeout or once two
// checkups in a row found it quiet (not on Windows, or with --no-stall):
// SIGQUIT, then SIGKILL runQuitGrace later, exit 5. From a node it
// publishes run events. Otherwise it exits with the command's code.
func (a *app) cmdRun(args []string) error {
	fs := a.flags("run")
	timeout := fs.Duration("timeout", runTimeout, "the bound: past it the group is ended")
	noStall := fs.Bool("no-stall", false, "keep a quiet run going until --timeout (work outside the group)")
	i := slices.Index(args, "--")
	if i < 0 {
		if _, err := parse(fs, args); err != nil { // -h prints the flags
			return err
		}
		return fail(exitUsage, runUsage)
	}
	pos, err := parse(fs, args[:i])
	if err != nil {
		return err
	}
	argv := args[i+1:]
	if len(pos) > 0 || len(argv) == 0 {
		return fail(exitUsage, runUsage)
	}
	if *timeout <= 0 {
		return a.runRefused(exitUsage, "--timeout must be positive")
	}
	if isBmesh(argv[0]) {
		sub := ""
		if len(argv) > 1 {
			sub = " " + argv[1]
		}
		return a.runRefused(exitUsage, "bmesh%s bounds itself; run it as it is", sub)
	}
	ev := a.runEvents(argv)

	cmd := exec.Command(argv[0], argv[1:]...)
	var mu sync.Mutex // guards ended, between the watch, the timer and the signal
	ended := ""       // why bmesh ended the run: its stderr line, after "bmesh run: "
	endKind := ""     // the same for the run event: stalled, timed out
	endText := ""     // and its text: "stalled after 4m"
	var watch *proc.Watch
	var grace *time.Timer // the SIGKILL after a SIGQUIT, stopped once the run is reaped
	exited := false       // the command was reaped: a late stall or timeout changes nothing
	end := func(line, kind, text string) {
		mu.Lock()
		defer mu.Unlock()
		if ended != "" || exited {
			return
		}
		if l := watch.Last(); l != "" {
			line += "; last line: " + l
		}
		ended, endKind, endText = line, kind, text
		proc.QuitGroup(cmd)
		grace = time.AfterFunc(runQuitGrace, func() { proc.KillGroup(cmd) })
	}
	start := time.Now()
	watch = proc.NewWatch(*timeout, func(c proc.Checkup) {
		fmt.Fprintf(a.stderr, "bmesh run %s\n", c)
		ev.checkup(c)
		if c.Stalled() && !*noStall {
			end("stalled: no output and no CPU for "+proc.Dur(c.Quiet), "stalled", "stalled after "+proc.Dur(c.Elapsed))
		}
	})
	cmd.Stdin = a.stdin
	cmd.Stdout, cmd.Stderr = io.MultiWriter(a.stdout, watch), io.MultiWriter(a.stderr, watch)
	cmd.WaitDelay = time.Second // a leftover child may hold the output open
	proc.Group(cmd)

	sigs, stopSigs := leaseSignals()
	defer stopSigs()
	ev.start(start)
	if err := cmd.Start(); err != nil {
		ev.end("exit 1", "exit 1 after "+runDur(time.Since(start)))
		return a.runRefused(exitError, "%v", err)
	}
	// What reads cmd.Process starts once it is set.
	watch.Start(cmd.Process.Pid)
	interrupted := make(chan struct{})
	done := make(chan struct{})
	go func() {
		select {
		case <-sigs:
			close(interrupted)
			proc.KillGroup(cmd)
		case <-done:
		}
	}()
	timer := time.AfterFunc(*timeout, func() {
		end("timed out after "+runDur(*timeout), "timed out", "timed out after "+runDur(*timeout))
	})
	err = cmd.Wait()
	mu.Lock()
	exited = true
	mu.Unlock()
	timer.Stop()
	close(done)
	watch.Stop()
	mu.Lock()
	why, kind, text := ended, endKind, endText
	if grace != nil {
		grace.Stop() // its pid may be another's once this kill is done
	}
	proc.KillGroup(cmd) // nothing of the run outlives it
	mu.Unlock()
	took := time.Since(start)
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState.Success() {
		err = nil
	}
	select {
	case <-interrupted:
		ev.end("interrupted", "interrupted after "+runDur(took))
		return a.runRefused(exitError, "interrupted")
	default:
	}
	if kind != "" {
		ev.end(kind, text)
		return a.runRefused(exitTimeout, "%s", why)
	}
	if err == nil {
		ev.end("ok", "ok after "+runDur(took))
		return nil
	}
	code := exitError
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() >= 0 {
		code = ee.ExitCode()
	}
	ev.end(fmt.Sprintf("exit %d", code), fmt.Sprintf("exit %d after %s", code, runDur(took)))
	return &exitErr{code: code}
}

// runRefused prints "bmesh run: <msg>" and exits code.
func (a *app) runRefused(code int, format string, args ...any) error {
	fmt.Fprintf(a.stderr, "bmesh run: "+format+"\n", args...)
	return &exitErr{code: code}
}

// runDur is d in seconds, its zero tail dropped: 3m12s, 4m, 40s.
func runDur(d time.Duration) string {
	s := d.Round(time.Second).String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}

// isBmesh reports whether argv0 is bmesh: named so, or the same file as
// this executable or the bmesh on PATH.
func isBmesh(argv0 string) bool {
	if strings.TrimSuffix(filepath.Base(argv0), ".exe") == "bmesh" {
		return true
	}
	p, err := exec.LookPath(argv0)
	if err != nil {
		return false
	}
	st, err := os.Stat(p)
	if err != nil {
		return false
	}
	self, _ := os.Executable()
	onPath, _ := exec.LookPath("bmesh")
	for _, q := range []string{self, onPath} {
		if q == "" {
			continue
		}
		if o, err := os.Stat(q); err == nil && os.SameFile(st, o) {
			return true
		}
	}
	return false
}

// runEv publishes a node's run events; with no node or no hub it
// publishes nothing.
type runEv struct {
	a    *app
	node string
	run  mesh.Run
}

// runEvents resolves the node the run is under: none when no node resolves
// or the hub can't be reached.
func (a *app) runEvents(argv []string) *runEv {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := a.identity(ctx, "run")
	if err != nil {
		return nil
	}
	return &runEv{a: a, node: p.String(), run: mesh.Run{Prog: runProg(argv[0]), Key: runKey(argv)}}
}

// runProg is the program's base name, or as typed when it names a path
// in the cwd (./gradlew).
func runProg(argv0 string) string {
	if strings.HasPrefix(argv0, "./") || runtime.GOOS == "windows" && strings.HasPrefix(argv0, `.\`) {
		return argv0
	}
	return filepath.Base(argv0)
}

// runKey is a sha256 of argv and the cwd relative to the worktree's top
// (the cwd itself outside one), so a repeat of one command in one place
// shares it and nothing of its args is told.
func runKey(argv []string) string {
	cwd, _ := os.Getwd()
	rel := cwd
	if top, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		if r, err := filepath.Rel(strings.TrimSpace(string(top)), cwd); err == nil {
			rel = filepath.ToSlash(r)
		}
	}
	h := sha256.New()
	for _, s := range argv {
		fmt.Fprintf(h, "%s\x00", s)
	}
	fmt.Fprintf(h, "cwd %s", rel)
	return hex.EncodeToString(h.Sum(nil))
}

func (e *runEv) start(at time.Time) {
	if e == nil {
		return
	}
	e.run.Since = rfc(at)
	e.publish(e.run.Prog + " started")
}

func (e *runEv) checkup(c proc.Checkup) {
	if e == nil {
		return
	}
	e.run.Lines, e.run.CPU, e.run.Quiet = c.Lines, int64(c.CPU/time.Second), int64(c.Quiet/time.Second)
	e.publish(e.run.Prog + " " + c.Head() + c.Tail())
}

func (e *runEv) end(end, text string) {
	if e == nil {
		return
	}
	e.run.End = end
	e.publish(e.run.Prog + ": " + text)
}

func (e *runEv) publish(text string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := e.run
	if err := mesh.PublishEvent(ctx, e.a.client, mesh.Event{From: e.node, Event: "run", Text: text, Run: &r}); err != nil {
		fmt.Fprintf(e.a.stderr, "bmesh run: event: %v\n", err)
	}
}
