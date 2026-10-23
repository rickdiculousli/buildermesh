//go:build !windows

package daemon

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A cancelled step gets an interrupt and cleans up before it exits, as
// land does on its own context.
func TestExecStepInterruptsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	script := `trap 'echo cleaned up; exit 7' INT; echo started; while :; do sleep 0.05; done`
	out, code, err := execStep(ctx, "sh", "", t.TempDir(), []string{"-c", script})
	if err != nil || code != 7 || !strings.Contains(out, "cleaned up") {
		t.Fatalf("out %q code %d err %v", out, code, err)
	}
}
