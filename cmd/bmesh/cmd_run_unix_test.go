//go:build !windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/proc"
)

// fastCheckups shortens the checkup cadence and the SIGQUIT grace.
func fastCheckups(t *testing.T) {
	t.Helper()
	cad, grace := proc.Cadence, runQuitGrace
	proc.Cadence, runQuitGrace = 500*time.Millisecond, 2*time.Second
	t.Cleanup(func() { proc.Cadence, runQuitGrace = cad, grace })
}

// A quiet run is ended after two quiet checkups in a row: SIGQUIT first
// (its trap prints), then the group goes, its background child too, and
// bmesh run exits 5 saying it stalled, with the last line.
func TestRunEndsAStall(t *testing.T) {
	fastCheckups(t)
	t.Chdir(t.TempDir())
	pidFile := filepath.Join(t.TempDir(), "pid")
	out, errs, code := runCLI(t, nil, "run", "--", "sh", "-c",
		`trap 'echo got quit; exit 9' QUIT; echo waiting; sleep 600 & echo $! > "$0"; wait`, pidFile)
	if code != 5 || out != "waiting\ngot quit\n" ||
		!strings.Contains(errs, "bmesh run: stalled: no output and no CPU for ") ||
		!strings.HasSuffix(errs, "; last line: waiting\n") || !strings.Contains(errs, "; quiet ") {
		t.Fatalf("run: %d %q %q", code, out, errs)
	}
	b, _ := os.ReadFile(pidFile)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := meshtest.Deadline(t)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d survived", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// --no-stall keeps a quiet run until its timeout; the command's own code
// comes back otherwise; bmesh itself is refused.
func TestRunTimeoutCodeAndRefusal(t *testing.T) {
	fastCheckups(t)
	t.Chdir(t.TempDir())
	start := time.Now()
	_, errs, code := runCLI(t, nil, "run", "--no-stall", "--timeout", "3s", "--", "sleep", "600")
	if code != 5 || !strings.HasPrefix(errs, "bmesh run 1s: 0 lines, cpu 0s") ||
		!strings.HasSuffix(errs, "bmesh run: timed out after 3s\n") || time.Since(start) < 3*time.Second {
		t.Fatalf("no-stall: %d %q", code, errs)
	}
	if out, errs, code := runCLI(t, nil, "run", "--", "sh", "-c", "echo out; exit 3"); code != 3 || out != "out\n" || errs != "" {
		t.Fatalf("exit 3: %d %q %q", code, out, errs)
	}
	if _, errs, code := runCLI(t, nil, "run", "--", "bmesh", "land"); code != 2 || errs != "bmesh run: bmesh land bounds itself; run it as it is\n" {
		t.Fatalf("bmesh: %d %q", code, errs)
	}
	self, _ := os.Executable()
	if _, errs, code := runCLI(t, nil, "run", "--", self, "watch"); code != 2 || !strings.Contains(errs, "bounds itself") {
		t.Fatalf("self: %d %q", code, errs)
	}
	for _, args := range [][]string{{"run"}, {"run", "sleep", "1"}, {"run", "x", "--", "true"}, {"run", "--"}} {
		if _, errs, code := runCLI(t, nil, args...); code != 2 || !strings.Contains(errs, "usage: bmesh run") {
			t.Fatalf("%v: %d %q", args, code, errs)
		}
	}
}

// From a node, a run publishes run events (never its args or output), the
// last carrying how it ended, and watch prints only the stalled one.
func TestRunEventsAndWatch(t *testing.T) {
	fastCheckups(t)
	c, ctx := setup(t)
	putNode(t, c, ctx, mesh.Node{Path: "a"})
	t.Chdir(t.TempDir())
	since := time.Now()
	if _, errs, code := runCLI(t, as("a"), "run", "--", "sh", "-c", "echo secret-out; sleep 600", "secret-arg"); code != 5 {
		t.Fatalf("stall: %d %q", code, errs)
	}
	if _, errs, code := runCLI(t, as("a"), "run", "--", "sh", "-c", "exit 0"); code != 0 {
		t.Fatalf("ok: %d %q", code, errs)
	}
	p, _ := mesh.ParsePath("a")
	hist, err := mesh.History(ctx, c, mesh.EventSubject(p, "run"), since)
	if err != nil {
		t.Fatal(err)
	}
	var ends, texts []string
	for _, m := range hist {
		if strings.Contains(string(m.Data), "secret") {
			t.Fatalf("an arg or output line went out: %s", m.Data)
		}
		var ev mesh.Event
		if err := json.Unmarshal(m.Data, &ev); err != nil || ev.Run == nil || ev.Run.Prog != "sh" || len(ev.Run.Key) != 64 || ev.Run.Since == "" {
			t.Fatalf("event %s", m.Data)
		}
		texts = append(texts, ev.Text)
		if ev.Run.End != "" {
			ends = append(ends, ev.Run.End)
		}
	}
	if strings.Join(ends, ",") != "stalled,ok" || texts[0] != "sh started" || !strings.HasPrefix(texts[len(texts)-1], "sh: ok after ") {
		t.Fatalf("ends %v texts %q", ends, texts)
	}
	out, errs, code := runCLI(t, nil, "watch", "--nodes", "a", "--since", since.UTC().Format(time.RFC3339), "--timeout", "1s")
	if code != 5 || strings.Count(out, " a run ") != 1 || !strings.Contains(out, " a run sh: stalled after ") {
		t.Fatalf("watch: %d\n%s\nerr %q", code, out, errs)
	}
}

// An inline kind whose command sleeps prints checkup lines on stderr, the
// last ending quiet, and records its run as before.
func TestRoutineRunPrintsCheckups(t *testing.T) {
	fastCheckups(t)
	c, ctx := setup(t)
	kindRepo(t, map[string]string{"nap": "mode = \"inline\"\ncommand = \"echo napping; sleep 2\"\n"})
	tree := gitOut(t, "rev-parse", "HEAD^{tree}")
	out, errs, code := runCLI(t, nil, "routine", "run", "nap")
	if code != 0 || out != "napping\nnap ok t:"+tree[:8]+" (recorded)\n" {
		t.Fatalf("run: %d %q %q", code, out, errs)
	}
	lines := strings.Split(strings.TrimSpace(errs), "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(lines[0], "bmesh: nap ") || !strings.Contains(last, ": napping; quiet ") {
		t.Fatalf("checkups %q", errs)
	}
	if r, ok, err := mesh.GetResult(ctx, c, mesh.ResultKey("nap", tree, nil, "")); err != nil || !ok || !r.OK || r.Output != "napping\n" {
		t.Fatalf("result %+v %v %v", r, ok, err)
	}
}
