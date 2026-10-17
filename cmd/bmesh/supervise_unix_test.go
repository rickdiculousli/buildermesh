//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// fakeServe is a supervisor of testdata/fake-serve.sh with short waits, and
// the file its runs append to.
func fakeServe(t *testing.T, exit string) (*supervisor, *meshtest.SyncBuffer, string) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-serve.sh"))
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "serve.runs")
	t.Setenv("FAKE_SERVE_LOG", log)
	t.Setenv("FAKE_SERVE_EXIT", exit)
	t.Setenv("FAKE_SERVE_HOLD", "")
	out := &meshtest.SyncBuffer{}
	s := newSupervisor(sh, []string{fake, "serve", "--config", "/c d/config.toml", "--listen", "127.0.0.1:0"}, out)
	s.wait, s.backoff, s.quick, s.grace = 20*time.Millisecond, time.Hour, time.Hour, 5*time.Second
	return s, out, log
}

// runs is the lines the fake's runs appended.
func runs(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(b)), " ", "_"))
}

// supervise runs s until stop is called, which waits for it to return.
func supervise(t *testing.T, s *supervisor) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.run(ctx) }()
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("supervisor did not stop")
		}
	}
	t.Cleanup(stop)
	return stop
}

func waitRuns(t *testing.T, log string, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(runs(t, log)) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d runs, want %d: %v", len(runs(t, log)), n, runs(t, log))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSuperviseRestartsWhateverTheExit(t *testing.T) {
	for _, exit := range []string{"0", "1"} {
		t.Run("exit "+exit, func(t *testing.T) {
			s, out, log := fakeServe(t, exit)
			s.quickN = 100
			stop := supervise(t, s)
			waitRuns(t, log, 3)
			stop()
			want := "run_serve_--config_/c_d/config.toml_--listen_127.0.0.1:0_supervised=1"
			for _, r := range runs(t, log) {
				if r != want {
					t.Fatalf("run %q, want %q", r, want)
				}
			}
			o := out.String()
			if !strings.Contains(o, "fake serve output") || !strings.Contains(o, "serve exited (exit "+exit+")") ||
				!strings.Contains(o, "starting again in 20ms") {
				t.Fatalf("output:\n%s", o)
			}
		})
	}
}

func TestSuperviseBacksOffAfterQuickExits(t *testing.T) {
	s, out, log := fakeServe(t, "1")
	supervise(t, s)
	waitRuns(t, log, 5)
	time.Sleep(500 * time.Millisecond)
	if n := len(runs(t, log)); n != 5 {
		t.Fatalf("%d runs before the backoff ran out, want 5", n)
	}
	if !strings.Contains(out.String(), "starting again in 1h0m0s") {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestSuperviseLongRunResetsBackoff(t *testing.T) {
	s, _, log := fakeServe(t, "1")
	s.quick = 0 // no run is quick
	supervise(t, s)
	waitRuns(t, log, 8)
}

func TestSuperviseStopsChildOnSignal(t *testing.T) {
	s, out, log := fakeServe(t, "0")
	t.Setenv("FAKE_SERVE_HOLD", "1")
	stop := supervise(t, s)
	waitRuns(t, log, 1)
	stop()
	if r := runs(t, log); len(r) != 2 || r[1] != "term" {
		t.Fatalf("runs %v", r)
	}
	if !strings.Contains(out.String(), "stopped; serve exited (exit 0)") {
		t.Fatalf("output:\n%s", out.String())
	}
}

// A child that exited 0 reads as exit 0 even when a process it left held
// the output past WaitDelay.
func TestExitTextPrefersTheExitCode(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 3 & exit 0")
	cmd.Stdout = &strings.Builder{}
	cmd.WaitDelay = 100 * time.Millisecond
	err := cmd.Run()
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("Run: %v, want ErrWaitDelay", err)
	}
	if got := exitText(err, cmd.ProcessState); got != "exit 0" {
		t.Fatalf("exitText %q", got)
	}
}

// A child that refused its config (exit 78) is not started again, and the
// supervisor exits 78 itself.
func TestSuperviseStopsOnConfigExit(t *testing.T) {
	s, out, log := fakeServe(t, "78")
	s.quickN = 100
	done := make(chan error, 1)
	go func() { done <- s.run(context.Background()) }()
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("supervisor kept restarting a child that exited 78")
	}
	var ee *exitErr
	if !errors.As(err, &ee) || ee.code != exitConfig {
		t.Fatalf("run: %v, want exit %d", err, exitConfig)
	}
	if n := len(runs(t, log)); n != 1 {
		t.Fatalf("%d runs, want 1", n)
	}
	if o := out.String(); !strings.Contains(o, "serve exited 78 (config check failed); not restarting: fix the config and start it again") ||
		strings.Contains(o, "starting again") {
		t.Fatalf("output:\n%s", o)
	}
}
