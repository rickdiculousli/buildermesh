package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/runner/t3mcp/t3fake"
)

// useFake points BMESH_<kind>_COMMAND at testdata/<script> and returns the log path.
func useFake(t *testing.T, envVar, script, logVar string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", script))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(envVar, "sh "+abs)
	log := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv(logVar, log)
	return log
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// A T3 before 0.1.0 is refused at start, status and send, before any
// tool call; a thread id from before 0.1.0 is no such thread.
func TestT3RefusesProtocol1(t *testing.T) {
	r, fake, _ := v2Setup(t, "auto", "auto")
	now := time.Now()
	r.Now = func() time.Time { return now }
	fake.Do(func(s *t3fake.Server) { s.Protocol = 1 })
	want := "T3 0.0.45 at " + fake.URL + " speaks protocol 1; bmesh needs T3 0.1.0 or later"
	ctx := context.Background()
	_, err := r.Start(ctx, StartSpec{RepoPath: t.TempDir(), Model: "m1", Title: "t", BriefPath: writeBriefFile(t, "b"), ThreadID: "u"})
	if err == nil || err.Error() != want {
		t.Fatalf("start: %v", err)
	}
	if _, err := r.Status(ctx, "mcp:t"); err == nil || err.Error() != want {
		t.Fatalf("status: %v", err)
	}
	if err := r.Send(ctx, "mcp:t", "hello"); err == nil || err.Error() != want {
		t.Fatalf("send: %v", err)
	}
	if err := r.Preflight(ctx); err == nil || err.Error() != want {
		t.Fatalf("preflight: %v", err)
	}
	if calls := fake.ToolCalls(); len(calls) != 0 {
		t.Fatalf("tool calls on a protocol-1 server: %v", calls)
	}
	// Protocol 2 is trusted for a minute.
	fake.Do(func(s *t3fake.Server) { s.Protocol = 2 })
	fake.AddThread(&t3fake.Thread{ID: "mcp:t"})
	if _, err := r.Status(ctx, "mcp:t"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Status(ctx, "th-1"); !errors.Is(err, ErrNoThread) {
		t.Fatalf("pre-0.1.0 id: %v", err)
	}
	fake.Do(func(s *t3fake.Server) { s.Protocol = 3 })
	if _, err := r.Status(ctx, "mcp:t"); err != nil {
		t.Fatalf("cached: %v", err)
	}
	now = now.Add(61 * time.Second)
	if _, err := r.Status(ctx, "mcp:t"); err == nil || !strings.Contains(err.Error(), "protocol 3") {
		t.Fatalf("protocol 3: %v", err)
	}
}
