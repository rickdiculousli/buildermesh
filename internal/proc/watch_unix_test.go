//go:build !windows

package proc

import (
	"os/exec"
	"sync"
	"testing"
	"time"
)

// watchRun runs a shell script grouped and watched at a short cadence and
// returns its first n checkups.
func watchRun(t *testing.T, script string, n int) []Checkup {
	t.Helper()
	return watchArgv(t, 400*time.Millisecond, n, "sh", "-c", script)
}

func watchArgv(t *testing.T, every time.Duration, n int, argv ...string) []Checkup {
	t.Helper()
	orig := Cadence
	Cadence = every
	t.Cleanup(func() { Cadence = orig })
	var mu sync.Mutex
	var got []Checkup
	enough := make(chan struct{})
	w := NewWatch(time.Minute, func(c Checkup) {
		mu.Lock()
		defer mu.Unlock()
		if got = append(got, c); len(got) == n {
			close(enough)
		}
	})
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout, cmd.Stderr = w, w
	Group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	w.Start(cmd.Process.Pid)
	defer func() { KillGroup(cmd); cmd.Wait() }()
	select {
	case <-enough:
	case <-time.After(30 * time.Second):
		t.Fatal("no checkups")
	}
	w.Stop()
	mu.Lock()
	defer mu.Unlock()
	return got[:n]
}

// A grouped sleep is quiet at each checkup.
func TestWatchSleepIsQuiet(t *testing.T) {
	for i, c := range watchArgv(t, time.Second, 3, "sleep", "600") {
		if c.QuietN != i+1 || c.NoCPU || c.Lines != 0 || c.Quiet < time.Duration(i+1)*900*time.Millisecond {
			t.Fatalf("checkup %d: %+v", i, c)
		}
		if i == 1 && (!c.Stalled() || c.String() != c.Head()+"; quiet "+Dur(c.Quiet)) {
			t.Fatalf("second checkup: %q stalled %v", c, c.Stalled())
		}
	}
}

// A busy loop with no output is never quiet: its CPU moves.
func TestWatchBusyLoopIsNeverQuiet(t *testing.T) {
	for i, c := range watchRun(t, "while :; do :; done", 3) {
		if c.QuietN != 0 || c.CPU <= 0 {
			t.Fatalf("checkup %d: %+v", i, c)
		}
	}
}

// A \r progress meter, with no newline and next to no CPU, is output: never
// quiet, its newest frame the newest line.
func TestWatchProgressMeterIsNeverQuiet(t *testing.T) {
	for i, c := range watchRun(t, `i=0; while :; do i=$((i+1)); printf '\r%d%%' $i; sleep 0.1; done`, 3) {
		if c.QuietN != 0 || c.Lines != 0 || c.Bytes == 0 || c.Last == "" {
			t.Fatalf("checkup %d: %+v", i, c)
		}
	}
}
