package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestT3SendRefusesRunning(t *testing.T) {
	log := useFake(t, "BMESH_T3_COMMAND", "fake-t3.sh", "FAKE_T3_LOG")
	t.Setenv("FAKE_T3_STATUS", "running")
	r := T3{Dispatch: "t3_dispatch.py", Dir: t.TempDir()}
	err := r.Send(context.Background(), "th-1", "hello")
	if !errors.Is(err, ErrRunning) {
		t.Fatalf("err %v, want ErrRunning", err)
	}
	got := readLog(t, log)
	if !strings.Contains(got, "argv status th-1") || strings.Contains(got, "send") {
		t.Fatalf("log %q", got)
	}
	// Idle: the send goes through with the text in a prompt file.
	t.Setenv("FAKE_T3_STATUS", "idle")
	if err := r.Send(context.Background(), "th-1", "hello"); err != nil {
		t.Fatal(err)
	}
	if got := readLog(t, log); !strings.Contains(got, "send th-1 hello\n") {
		t.Fatalf("log %q", got)
	}
	// Exit 4 from send (became running in between) is ErrRunning too.
	t.Setenv("FAKE_T3_SEND_EXIT", "4")
	if err := r.Send(context.Background(), "th-1", "again"); !errors.Is(err, ErrRunning) {
		t.Fatalf("exit 4: %v", err)
	}
	st, err := r.Status(context.Background(), "th-1")
	if err != nil || st.Running || !st.Known {
		t.Fatalf("status %+v %v", st, err)
	}
	t.Setenv("FAKE_T3_MISSING", "1")
	if _, err := r.Status(context.Background(), "th-1"); !errors.Is(err, ErrNoThread) {
		t.Fatalf("missing: %v", err)
	}
}

func TestT3StartPassesThreadIDAndPromptFile(t *testing.T) {
	log := useFake(t, "BMESH_T3_COMMAND", "fake-t3.sh", "FAKE_T3_LOG")
	brief := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(brief, []byte("build spec 012"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := T3{Dispatch: "t3_dispatch.py", Dir: t.TempDir()}
	got, err := r.Start(context.Background(), StartSpec{RepoPath: "/r/demo", Model: "m1", Title: "[build] demo 012",
		BriefPath: brief, ThreadID: "uuid-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got != (Started{Thread: "uuid-1", Branch: "t3code/uuid-1", Worktree: "/wt/uuid-1"}) {
		t.Fatalf("started %+v", got)
	}
	want := "argv start --repo /r/demo --provider claude --model m1 --title [build] demo 012 --prompt-file " + brief +
		" --thread-id uuid-1\nstart build spec 012\n"
	if l := readLog(t, log); l != want {
		t.Fatalf("log %q\nwant %q", l, want)
	}
	if err := r.Interrupt(context.Background(), "uuid-1"); err != nil {
		t.Fatal(err)
	}
	if l := readLog(t, log); !strings.HasSuffix(l, "stop uuid-1\n") {
		t.Fatalf("log %q", l)
	}
}

func TestT3StartInAnExistingWorktreeUnderAProject(t *testing.T) {
	log := useFake(t, "BMESH_T3_COMMAND", "fake-t3.sh", "FAKE_T3_LOG")
	brief := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(brief, []byte("propose"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := T3{Dispatch: "t3_dispatch.py", Dir: t.TempDir()}
	got, err := r.Start(context.Background(), StartSpec{RepoPath: "/r/demo", Model: "m1", Title: "[propose] demo round 1: engineer",
		BriefPath: brief, ThreadID: "uuid-2", Branch: "demo-round-1-eng", Worktree: "/bm/wt/demo/demo-round-1-eng", Project: "/r/bm"})
	if err != nil {
		t.Fatal(err)
	}
	if got != (Started{Thread: "uuid-2", Branch: "demo-round-1-eng", Worktree: "/bm/wt/demo/demo-round-1-eng"}) {
		t.Fatalf("started %+v", got)
	}
	want := "argv start --repo /r/bm --provider claude --model m1 --title [propose] demo round 1: engineer --prompt-file " + brief +
		" --thread-id uuid-2 --worktree /bm/wt/demo/demo-round-1-eng --branch demo-round-1-eng\n"
	if l := readLog(t, log); !strings.HasPrefix(l, want) {
		t.Fatalf("log %q\nwant prefix %q", l, want)
	}
}
