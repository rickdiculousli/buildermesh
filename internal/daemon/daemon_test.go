package daemon

import (
	"context"
	"testing"
	"time"
)

// Run's first poll+wake pass happens at once, and Run returns when ctx ends.
func TestRunWakesAndStops(t *testing.T) {
	f := setup(t)
	f.lead(t, "app-001", "th-1", "working")
	f.send(t, "app-001", 1)
	d := f.d
	d.Now = nil
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan error, 1)
	go func() { done <- Run(ctx, d) }()
	deadline := time.Now().Add(10 * time.Second)
	for len(f.sends(t)) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
	if sends := f.sends(t); len(sends) != 1 || sends[0] != wakeLine("th-1", 1) {
		t.Fatalf("sends %q", sends)
	}
	if evs := f.daemonEvents(t, "wake"); len(evs) != 1 {
		t.Fatalf("wake events %+v", evs)
	}
}
