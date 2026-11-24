package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// landEvents is app-012's land events, oldest first, each with its land
// object.
func landEvents(t *testing.T, c *mesh.Client, ctx context.Context) []mesh.Event {
	t.Helper()
	p, _ := mesh.ParsePath("app-012")
	hist, err := mesh.History(ctx, c, mesh.EventSubject(p, "land"), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var evs []mesh.Event
	for _, m := range hist {
		var ev mesh.Event
		if err := json.Unmarshal(m.Data, &ev); err != nil || ev.Land == nil {
			t.Fatalf("land event %s: %v", m.Data, err)
		}
		evs = append(evs, ev)
	}
	return evs
}

// landSteps is the events' steps, a repeat of the one before dropped; it
// fails when a step's since comes before the one before it, or the events
// are of more than one run.
func landSteps(t *testing.T, evs []mesh.Event) []string {
	t.Helper()
	var steps []string
	since := ""
	for _, ev := range evs {
		if ev.Land.Run != evs[0].Land.Run {
			t.Fatalf("runs %s and %s", evs[0].Land.Run, ev.Land.Run)
		}
		if ev.Land.Since < since {
			t.Fatalf("step %s since %s, before %s", ev.Land.Step, ev.Land.Since, since)
		}
		since = ev.Land.Since
		if len(steps) == 0 || steps[len(steps)-1] != ev.Land.Step {
			steps = append(steps, ev.Land.Step)
		}
	}
	return steps
}

// A clean land's events take its steps in order, each text change at once;
// while verify runs, the heartbeat carries its newest line and the command
// goes out once; the last event says it landed.
func TestLandEventsSayWhereItIs(t *testing.T) {
	c, ctx := setup(t)
	r := newLandRepo(t)
	putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
	landEventEvery = 50 * time.Millisecond
	writeFile(t, os.Getenv("HOOK"), "echo one\necho '  two  '\necho\nsleep 1\necho three\n")
	base := r.originMain(t)
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")
	if out, errs, code := runCLI(t, as("app-012"), "land"); code != 0 {
		t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
	}
	rec := gitOut(t, "rev-parse", "HEAD")
	evs := landEvents(t, c, ctx)
	want := []string{mesh.LandChecks, mesh.LandKinds, mesh.LandJobs, mesh.LandMain, mesh.LandLease, mesh.LandRebase, mesh.LandResults, mesh.LandPush}
	if got := landSteps(t, evs); !slices.Equal(got, want) {
		t.Fatalf("steps %v, want %v", got, want)
	}
	var texts []string
	commands := 0
	var mid *mesh.LandCmd
	for _, ev := range evs {
		if len(texts) == 0 || texts[len(texts)-1] != ev.Text {
			texts = append(texts, ev.Text)
		}
		if c := ev.Land.Cmd; c != nil {
			if c.Command != "" {
				commands++
				if c.Command != `sh "$HOOK"` || c.Kind != "verify" {
					t.Fatalf("cmd %+v", c)
				}
			}
			if c.Last != "" {
				mid = c
			}
		}
	}
	for _, w := range []string{"checking", "running verify on t:" + tree[:8], "no jobs to wait for", "fetching origin/main",
		"taking land.repo", "rebasing onto " + base[:8], "running verify on the rebased tree t:" + tree[:8], "pushing " + rec[:8]} {
		if !slices.Contains(texts, w) {
			t.Fatalf("texts %q lack %q", texts, w)
		}
	}
	// The heartbeat during the sleep: the newest non-empty line, trimmed.
	if commands != 1 || mid == nil || mid.Last != "two" || mid.Lines != 3 || mid.LastAt == "" || mid.LastAt < mid.Since {
		t.Fatalf("commands %d, cmd %+v", commands, mid)
	}
	last := evs[len(evs)-1].Land
	if last.End != "landed "+rec[:8] || last.Cmd != nil || slices.ContainsFunc(evs[:len(evs)-1], func(ev mesh.Event) bool { return ev.Land.End != "" }) {
		t.Fatalf("last %+v", last)
	}
}

// A refused push goes back to the rebase step; a refused land and a timed
// out one end with their own line.
func TestLandEventsAgainAndEnd(t *testing.T) {
	t.Run("main moved", func(t *testing.T) {
		c, ctx := setup(t)
		r := newLandRepo(t)
		putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
		other := filepath.Join(r.root, "other")
		meshtest.Git(t, r.root, "clone", "-q", "-b", "main", r.origin, other)
		runs := filepath.Join(r.root, "runs")
		writeFile(t, os.Getenv("HOOK"), "echo >> '"+runs+"'\nn=$(wc -l < '"+runs+"')\ntest $n -le 2 || exit 0\n"+
			"cd '"+other+"' && echo $n > c.txt && git add c.txt && git commit -qm 'other c' && git push -q origin HEAD:main\n")
		if out, errs, code := runCLI(t, as("app-012"), "land"); code != 0 || !strings.Contains(out, "main moved; again from the fetch\n") {
			t.Fatalf("land: %d\n%s\nerr %q", code, out, errs)
		}
		got := landSteps(t, landEvents(t, c, ctx))
		want := []string{mesh.LandChecks, mesh.LandKinds, mesh.LandJobs, mesh.LandMain, mesh.LandLease,
			mesh.LandRebase, mesh.LandResults, mesh.LandPush, mesh.LandRebase, mesh.LandResults, mesh.LandPush}
		if !slices.Equal(got, want) {
			t.Fatalf("steps %v, want %v", got, want)
		}
	})
	t.Run("refused", func(t *testing.T) {
		c, ctx := setup(t)
		r := newLandRepo(t)
		putNode(t, c, ctx, mesh.Node{Path: "app-012", RepoPath: r.repo, Spec: "012"})
		writeFile(t, os.Getenv("HOOK"), "echo broken\nexit 1\n")
		tree := gitOut(t, "rev-parse", "HEAD^{tree}")
		if _, _, code := runCLI(t, as("app-012"), "land"); code != exitError {
			t.Fatalf("land: %d", code)
		}
		evs := landEvents(t, c, ctx)
		if end := evs[len(evs)-1].Land.End; end != "verify failed at t:"+tree[:8]+"; fix it, commit and run bmesh land again" {
			t.Fatalf("end %q", end)
		}
	})
	t.Run("timed out", func(t *testing.T) {
		c, ctx := setup(t)
		reviewLandRepo(t, c, ctx, "M")
		out, _, code := runCLI(t, as("app-012"), "land", "--wait", "200ms")
		m := queuedRE.FindStringSubmatch(out)
		if code != exitTimeout || m == nil {
			t.Fatalf("land: %d\n%s", code, out)
		}
		evs := landEvents(t, c, ctx)
		if end := evs[len(evs)-1].Land.End; end != "timed out waiting for job "+m[1]+" (review)" {
			t.Fatalf("end %q", end)
		}
	})
}

// A queued review's entry carries its state and the box holding it, read
// again at each heartbeat, and its text names them; once its reply is in,
// land starts again from the checks.
func TestLandEventsNameTheJobsStateAndHost(t *testing.T) {
	c, ctx := setup(t)
	reviewLandRepo(t, c, ctx, "M")
	landEventEvery = 20 * time.Millisecond
	out, errb, res := landBG(t, "app-012")
	j := queuedJob(t, c, ctx, out, 1)
	q := unanswered(t, c, ctx)
	if len(q) != 1 {
		t.Fatalf("jobs %+v", q)
	}
	if err := mesh.PutHolds(ctx, c, mesh.Holds{Kind: "review", Host: "k16", At: time.Now().UTC().Format(time.RFC3339),
		Jobs: []mesh.Hold{{Seq: q[0].Seq, Job: j.ID, From: "app-012", Holder: "routine.review.m1-1", State: "running"}}}); err != nil {
		t.Fatal(err)
	}
	text := "waiting for job " + j.ID + " (review, running on k16)"
	meshtest.Until(t, "an event naming the running job", func() bool {
		return slices.ContainsFunc(landEvents(t, c, ctx), func(ev mesh.Event) bool {
			return ev.Text == text && slices.Equal(ev.Land.Jobs, []mesh.LandJob{{ID: j.ID, Kind: "review", State: "running", Host: "k16"}})
		})
	})
	answer(t, c, ctx, j, true, true, "VERDICT: ok")
	landEnds(t, res, 0, out, errb)
	steps := landSteps(t, landEvents(t, c, ctx))
	if i := slices.Index(steps, mesh.LandJobs); i < 0 || !slices.Contains(steps[i:], mesh.LandChecks) {
		t.Fatalf("steps %v: no checks after the jobs", steps)
	}
}

// While an inline kind waits for its slot, its cmd carries where it waits
// and the text names only the slot; once held, the text is the run's again.
// The lease's wait names its holder.
func TestLandStatusSlotWait(t *testing.T) {
	s := newLandStatus(time.Now())
	s.step(mesh.LandKinds, "running verify on t:4208beec")
	s.track("verify", "./gradlew test")
	s.at("emulator", "bluke-002", 2)
	ev := s.event()
	if ev.Text != "waiting for the emulator slot for verify" || ev.Land.Cmd.Wait == nil ||
		*ev.Land.Cmd.Wait != (mesh.LandWait{Slot: "emulator", Holder: "bluke-002", Place: 2}) {
		t.Fatalf("waiting: %+v %+v", ev, ev.Land.Cmd)
	}
	s.at("emulator", "bluke-003", 1) // a new holder or place changes no text
	s.held()
	if ev := s.event(); ev.Text != "running verify on t:4208beec" || ev.Land.Cmd.Wait != nil || ev.Land.Cmd.Command != "" {
		t.Fatalf("held: %+v %+v", ev, ev.Land.Cmd)
	}
	s.step(mesh.LandLease, "taking land.repo")
	s.at("land.repo", "app-013", 1)
	if ev := s.event(); ev.Text != "waiting for land.repo (held by app-013)" || ev.Land.Cmd != nil {
		t.Fatalf("lease: %+v", ev)
	}
	// The command's words go out once, on the first event after it started.
	var texts []string
	commands := 0
	for len(s.changes) > 0 {
		ev := <-s.changes
		texts = append(texts, ev.Text)
		if c := ev.Land.Cmd; c != nil && c.Command != "" {
			commands++
			if c.Command != "./gradlew test" || ev.Text != "waiting for the emulator slot for verify" {
				t.Fatalf("command on %+v", ev)
			}
		}
	}
	if commands != 1 {
		t.Fatalf("command sent %d times", commands)
	}
	want := []string{"running verify on t:4208beec", "waiting for the emulator slot for verify", "running verify on t:4208beec",
		"taking land.repo", "waiting for land.repo (held by app-013)"}
	if !slices.Equal(texts, want) {
		t.Fatalf("queued %q, want %q", texts, want)
	}
}
