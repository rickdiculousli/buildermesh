package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/hoststat"
)

// Cadence is how often a run is looked at, LongCadence for a run whose
// timeout is an hour or more. Tests shorten them.
var Cadence, LongCadence = 2 * time.Minute, 5 * time.Minute

// lineMax is where a checkup's newest line is cut, in characters;
// partialMax bounds the bytes kept of a line still being printed.
const lineMax, partialMax = 160, 4 * lineMax

// Every is the checkup cadence of a run bounded at timeout (0: none).
func Every(timeout time.Duration) time.Duration {
	if timeout >= time.Hour {
		return LongCadence
	}
	return Cadence
}

// Checkup is one look at a running process group: its time so far, the
// lines and bytes it printed and the CPU its group used since it started,
// its newest non-empty line, and, while nothing moved, how long and for
// how many checkups in a row.
type Checkup struct {
	Elapsed      time.Duration
	Lines, Bytes int64
	CPU          time.Duration
	// NoCPU is a run on an OS whose CPU isn't read (Windows): quiet there
	// is judged on output alone, reported and never acted on.
	NoCPU  bool
	Last   string
	Quiet  time.Duration
	QuietN int
}

// Head is "4m: 37 lines, cpu 61s" (no cpu where it isn't read).
func (c Checkup) Head() string {
	s := fmt.Sprintf("%s: %d lines", Dur(c.Elapsed), c.Lines)
	if !c.NoCPU {
		s += fmt.Sprintf(", cpu %ds", int64(c.CPU.Round(time.Second)/time.Second))
	}
	return s
}

// Tail is "; quiet 4m" while the run is quiet, else "".
func (c Checkup) Tail() string {
	if c.QuietN == 0 {
		return ""
	}
	return "; quiet " + Dur(c.Quiet)
}

// String is the head, the newest line and the tail:
// "4m: 37 lines, cpu 61s: ok internal/mesh 16.0s; quiet 4m".
func (c Checkup) String() string {
	s := c.Head()
	if c.Last != "" {
		s += ": " + c.Last
	}
	return s + c.Tail()
}

// Stalled reports two quiet checkups in a row where CPU is read.
func (c Checkup) Stalled() bool { return c.QuietN >= 2 && !c.NoCPU }

// Dur is d in whole minutes from a minute up ("4m"), else in seconds.
func Dur(d time.Duration) string {
	if d >= time.Minute {
		return fmt.Sprintf("%dm", d.Round(time.Minute)/time.Minute)
	}
	return d.Round(time.Second).String()
}

// Watch counts a run's output (it is the run's output writer) and, once
// started on the run's process group, looks at it every Every(timeout):
// each checkup goes to report. Quiet is no new output byte and under 1% of
// one core's time since the last checkup. Its methods are safe on nil.
type Watch struct {
	every  time.Duration
	report func(Checkup)

	mu           sync.Mutex
	lines, bytes int64
	cur          []byte // the line being printed, after the last \n or \r
	last         string // the newest non-empty line ended
	stop         chan struct{}
	done         chan struct{}
}

// NewWatch is a watch for a run bounded at timeout, reporting to report.
func NewWatch(timeout time.Duration, report func(Checkup)) *Watch {
	return &Watch{every: Every(timeout), report: report}
}

// Write counts p: a \r progress meter is output as a newline is, and ends
// a line for the newest line's sake.
func (w *Watch) Write(p []byte) (int, error) {
	n := len(p)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bytes += int64(n)
	w.lines += int64(bytes.Count(p, []byte{'\n'}))
	for len(p) > 0 {
		i := bytes.IndexAny(p, "\r\n")
		if i < 0 {
			w.keep(p)
			break
		}
		w.keep(p[:i])
		if s := strings.TrimSpace(string(w.cur)); s != "" {
			w.last = cutLine(s)
		}
		w.cur, p = w.cur[:0], p[i+1:]
	}
	return n, nil
}

func (w *Watch) keep(b []byte) {
	if n := partialMax - len(w.cur); n > 0 {
		w.cur = append(w.cur, b[:min(n, len(b))]...)
	}
}

// Last is the run's newest non-empty line, cut at 160 characters.
func (w *Watch) Last() string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.newest()
}

func (w *Watch) newest() string {
	if s := strings.TrimSpace(string(w.cur)); s != "" {
		return cutLine(s)
	}
	return w.last
}

func cutLine(s string) string {
	r := []rune(s)
	if len(r) <= lineMax {
		return s
	}
	return string(r[:lineMax-1]) + "…"
}

// Start begins the checkups of process group pgid, the run's (Group makes
// its leader's pid the group's), whose command started now.
func (w *Watch) Start(pgid int) {
	if w == nil {
		return
	}
	w.stop, w.done = make(chan struct{}), make(chan struct{})
	go w.loop(pgid, time.Now())
}

// Stop ends the checkups; none is reported after it returns.
func (w *Watch) Stop() {
	if w == nil || w.stop == nil {
		return
	}
	close(w.stop)
	<-w.done
}

func (w *Watch) loop(pgid int, start time.Time) {
	defer close(w.done)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-w.stop; cancel() }()
	read := func() (*hoststat.Group, error) {
		rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		return hoststat.ReadGroup(rctx, pgid)
	}
	prev, _ := read()
	t := time.NewTicker(w.every)
	defer t.Stop()
	var cpu time.Duration
	var lastBytes int64
	at, moved, quietN := start, start, 0
	for {
		select {
		case <-w.stop:
			return
		case now := <-t.C:
			g, err := read()
			noCPU := errors.Is(err, hoststat.ErrUnsupported)
			busy := false
			if err == nil {
				d := time.Duration(g.CPUSince(prev) * float64(time.Second))
				cpu += d
				busy = d*100 >= now.Sub(at) // 1% of one core
				prev = g
			} else if !noCPU {
				busy = true // a failed read is never taken for quiet
			}
			w.mu.Lock()
			c := Checkup{Elapsed: now.Sub(start), Lines: w.lines, Bytes: w.bytes, CPU: cpu, NoCPU: noCPU, Last: w.newest()}
			if w.bytes != lastBytes || busy {
				moved, quietN = now, 0
			} else {
				quietN++
			}
			lastBytes = w.bytes
			w.mu.Unlock()
			at = now
			c.QuietN = quietN
			if quietN > 0 {
				c.Quiet = now.Sub(moved)
			}
			select {
			case <-w.stop:
				return
			default:
			}
			w.report(c)
		}
	}
}
