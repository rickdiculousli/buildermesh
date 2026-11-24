package main

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// landStatus is where a land run is, for its land events: the step, the
// text, the command it runs and the jobs it waits for. A change of text (or
// of step) queues an event at once, in order; the heartbeat publishes the
// rest (the newest output line, each job's state) as it stands.
type landStatus struct {
	mu      sync.Mutex
	land    mesh.Land
	text    string
	running string // the text while the command runs, put back once its slot is held
	gen     int    // the command the output writer belongs to
	sent    bool   // the command's words went out on an event
	partial []byte // the output after its last newline
	changes chan mesh.Event
}

// landCmdMax and landLastMax are where a command and its output line are
// cut; landPartialMax bounds the output kept while no newline comes.
const landCmdMax, landLastMax, landPartialMax = 200, 160, 4096

func newLandStatus(now time.Time) *landStatus {
	at := rfc(now)
	return &landStatus{land: mesh.Land{Run: at, Step: mesh.LandChecks, Since: at}, text: "checking",
		changes: make(chan mesh.Event, 64)}
}

func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// cut is s cut to n runes, "…" ending one that was longer.
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// step enters step name, when the run is not in it already, saying text.
func (s *landStatus) step(name, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	moved := s.land.Step != name
	if moved {
		s.land.Step, s.land.Since, s.land.Cmd, s.land.Jobs = name, rfc(time.Now()), nil, nil
	}
	s.changed(text, moved)
}

// say sets the text.
func (s *landStatus) say(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changed(text, false)
}

// changed sets the text and queues an event when it or the step moved;
// the caller holds mu. A full queue drops its oldest event: the newer say
// more.
func (s *landStatus) changed(text string, moved bool) {
	if text == s.text && !moved {
		return
	}
	s.text = text
	ev := s.eventLocked()
	for {
		select {
		case s.changes <- ev:
			return
		default:
			select {
			case <-s.changes:
			default:
			}
		}
	}
}

// event is the land event as the run stands, the command's words on it
// when no event before carried them.
func (s *landStatus) event() mesh.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.eventLocked()
}

func (s *landStatus) eventLocked() mesh.Event {
	l := s.land
	l.Jobs = slices.Clone(l.Jobs)
	if l.Cmd != nil {
		c := *l.Cmd
		if c.Wait != nil {
			w := *c.Wait
			c.Wait = &w
		}
		if s.sent {
			c.Command = ""
		}
		s.sent = true
		l.Cmd = &c
	}
	return mesh.Event{Event: "land", Text: s.text, At: rfc(time.Now()), Land: &l}
}

// track starts a command of kind (setup, or an inline kind): the text it
// runs under is the one said now. Its output goes to the writer returned.
func (s *landStatus) track(kind, command string) io.Writer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gen++
	s.land.Cmd = &mesh.LandCmd{Kind: kind, Command: cut(command, landCmdMax), Since: rfc(time.Now())}
	s.sent, s.partial, s.running = false, nil, s.text
	return &landOut{s: s, gen: s.gen}
}

// cmdDone ends the command.
func (s *landStatus) cmdDone() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gen++
	s.land.Cmd = nil
}

// at is a slot wait's poll: an inline kind's slot goes in its cmd, and
// the text names only the slot; the lease (no command runs) names its
// holder.
func (s *landStatus) at(slot, holder string, place int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.land.Cmd; c != nil {
		c.Wait = &mesh.LandWait{Slot: slot, Holder: holder, Place: place}
		s.changed("waiting for the "+slot+" slot for "+c.Kind, false)
		return
	}
	s.changed("waiting for "+slot+" (held by "+holder+")", false)
}

// held is an inline kind holding its slots: its command starts now.
func (s *landStatus) held() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.land.Cmd; c != nil && c.Wait != nil {
		c.Wait, c.Since = nil, rfc(time.Now())
		s.changed(s.running, false)
	}
}

// waitFor enters the jobs step waiting for jobs, or, with none, saying
// line (what land found of the review). In the step already, a job it knows
// keeps the state and host read last: the caller's may be older.
func (s *landStatus) waitFor(jobs []mesh.LandJob, line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	moved := s.land.Step != mesh.LandJobs
	if moved {
		s.land.Step, s.land.Since, s.land.Cmd = mesh.LandJobs, rfc(time.Now()), nil
	} else {
		jobs = slices.Clone(jobs)
		for i, j := range jobs {
			if k := slices.IndexFunc(s.land.Jobs, func(o mesh.LandJob) bool { return o.ID == j.ID }); k >= 0 {
				jobs[i] = s.land.Jobs[k]
			}
		}
	}
	s.land.Jobs = jobs
	if len(jobs) > 0 {
		line = jobsText(jobs)
	}
	s.changed(line, moved)
}

// jobs is the jobs waited for now: none outside the jobs step.
func (s *landStatus) jobs() []mesh.LandJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.land.Step != mesh.LandJobs {
		return nil
	}
	return slices.Clone(s.land.Jobs)
}

// update takes the jobs' states and hosts read again, when land still waits
// for the same jobs; it reports a queued event (their text changed).
func (s *landStatus) update(jobs []mesh.LandJob) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	same := s.land.Step == mesh.LandJobs && slices.EqualFunc(s.land.Jobs, jobs, func(a, b mesh.LandJob) bool { return a.ID == b.ID })
	if !same {
		return false
	}
	s.land.Jobs = jobs
	text := jobsText(jobs)
	if text == s.text {
		return false
	}
	s.changed(text, false)
	return true
}

// end is how the run ended, for its last event.
func (s *landStatus) end(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.land.End, s.land.Cmd = line, nil
}

// jobsText is "waiting for job <id> (<kind>, <state>[ on <host>]), ...".
func jobsText(jobs []mesh.LandJob) string {
	said := make([]string, len(jobs))
	for i, j := range jobs {
		where := j.State
		if j.Host != "" {
			where += " on " + j.Host
		}
		said[i] = fmt.Sprintf("job %s (%s, %s)", j.ID, j.Kind, where)
	}
	return "waiting for " + strings.Join(said, ", ")
}

// landJobs is the jobs as land events name them.
func landJobs(jobs []waitJob) []mesh.LandJob {
	out := make([]mesh.LandJob, len(jobs))
	for i, j := range jobs {
		out[i] = mesh.LandJob{ID: j.ID, Kind: j.kind, State: cmp.Or(j.State, "waiting"), Host: cmp.Or(j.Host, j.Holder)}
	}
	return out
}

// landEnd is a run's ending as its last event says it: landed <sha8>, or
// its error's first line up to "; nothing is pushed".
func landEnd(err error, landed string) string {
	if err == nil {
		return "landed " + short8(landed)
	}
	line, _, _ := strings.Cut(err.Error(), "\n")
	line, _, _ = strings.Cut(line, "; nothing is pushed")
	return cut(cmp.Or(strings.TrimSpace(line), "failed"), landCmdMax)
}

// landOut takes a command's output for its cmd: the lines it printed and
// the newest non-empty one, with when it was printed. A writer of an
// earlier command writes nowhere.
type landOut struct {
	s   *landStatus
	gen int
}

func (o *landOut) Write(p []byte) (int, error) {
	s := o.s
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.land.Cmd
	if o.gen != s.gen || c == nil {
		return len(p), nil
	}
	s.partial = append(s.partial, p...)
	for {
		i := bytes.IndexByte(s.partial, '\n')
		if i < 0 {
			break
		}
		c.Lines++
		// A progress line rewritten with \r reads as its last state.
		line := s.partial[:i]
		if j := bytes.LastIndexByte(bytes.TrimRight(line, "\r"), '\r'); j >= 0 {
			line = line[j+1:]
		}
		if t := strings.TrimSpace(string(line)); t != "" {
			c.Last, c.LastAt = cut(t, landLastMax), rfc(time.Now())
		}
		s.partial = s.partial[i+1:]
	}
	if n := len(s.partial); n > landPartialMax {
		s.partial = slices.Clone(s.partial[n-landPartialMax:])
	}
	return len(p), nil
}
