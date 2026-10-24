package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/plugin"
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

// At start the daemon writes the plugin its binary carries to the state
// dir, stamped with its label, and leaves a current one alone.
func TestRunWritesThePlugin(t *testing.T) {
	f := setup(t)
	d := f.d
	d.StateDir, d.Label = t.TempDir(), "v9.9.9 (1a2b3c4d)"
	dir := plugin.Dir(d.StateDir)
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithCancel(f.ctx)
		done := make(chan error, 1)
		go func() { done <- Run(ctx, d) }()
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, label, ok := plugin.Written(dir); ok && label == d.Label {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("no plugin dir written")
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			// A file of its own survives only while the stamp is current.
			if err := os.WriteFile(filepath.Join(dir, "mine"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "mine")); err != nil {
		t.Fatal("a current plugin dir was rewritten")
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "lead", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}
