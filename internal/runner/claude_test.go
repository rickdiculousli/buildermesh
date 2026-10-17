package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// claudeRepo makes a repo with one commit on main and a Claude runner whose
// worktrees and state live in the test's temp dir.
func claudeRepo(t *testing.T) (Claude, string, string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "demo")
	meshtest.Git(t, root, "init", "-b", "main", repo)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "add", "-A")
	meshtest.Git(t, repo, "commit", "-m", "one")
	meshtest.Git(t, repo, "checkout", "-b", "other")
	brief := filepath.Join(root, "brief.md")
	if err := os.WriteFile(brief, []byte("do the thing"), 0o644); err != nil {
		t.Fatal(err)
	}
	return Claude{Claude: "claude", Worktrees: filepath.Join(root, "wt"), Dir: filepath.Join(root, "state")}, repo, brief
}

func idle(t *testing.T, r Claude, thread string) func() bool {
	return func() bool {
		st, err := r.Status(context.Background(), thread)
		if err != nil {
			t.Fatal(err)
		}
		return !st.Running
	}
}

func lines(t *testing.T, path string) []string {
	t.Helper()
	return strings.Split(strings.TrimSuffix(readLog(t, path), "\n"), "\n")
}

func TestClaudeStartRunsFirstTurn(t *testing.T) {
	log := useFake(t, "BMESH_CLAUDE_COMMAND", "fake-claude.sh", "FAKE_LOG")
	t.Setenv("FAKE_RESULT", "brief read")
	r, repo, brief := claudeRepo(t)
	got, err := r.Start(context.Background(), StartSpec{RepoPath: repo, BriefPath: brief, ThreadID: "th-1", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(filepath.Dir(r.Worktrees), "wt", "demo", "th-1")
	if got != (Started{Thread: "th-1", Branch: "th-1", Worktree: wt}) {
		t.Fatalf("started %+v", got)
	}
	if b := meshtest.Git(t, wt, "rev-parse", "--abbrev-ref", "HEAD"); b != "th-1" {
		t.Fatalf("worktree branch %q", b)
	}
	if meshtest.Git(t, wt, "rev-parse", "HEAD") != meshtest.Git(t, repo, "rev-parse", "main") {
		t.Fatal("worktree not branched from main")
	}
	meshtest.Until(t, "turn 1 to end", idle(t, r, "th-1"))
	if l := readLog(t, log); l != "-p --output-format json --session-id th-1 --model m --dangerously-skip-permissions\n" {
		t.Fatalf("log %q", l)
	}
	if in := readLog(t, log+".stdin"); in != "do the thing" {
		t.Fatalf("stdin %q", in)
	}
	st, err := r.Status(context.Background(), "th-1")
	if err != nil || st != (Status{Known: true, LastReply: "brief read"}) {
		t.Fatalf("status %+v %v", st, err)
	}
}

func TestClaudeStartOmitsEmptyModelAndRefusesBadBase(t *testing.T) {
	log := useFake(t, "BMESH_CLAUDE_COMMAND", "fake-claude.sh", "FAKE_LOG")
	r, repo, brief := claudeRepo(t)
	if _, err := r.Start(context.Background(), StartSpec{RepoPath: repo, BriefPath: brief, ThreadID: "th-2", Branch: "nope"}); err == nil ||
		!strings.Contains(err.Error(), "git worktree add") {
		t.Fatalf("err %v", err)
	}
	if _, err := r.Start(context.Background(), StartSpec{RepoPath: repo, BriefPath: brief, ThreadID: "th-1", Branch: "other"}); err != nil {
		t.Fatal(err)
	}
	meshtest.Until(t, "turn 1 to end", idle(t, r, "th-1"))
	if l := readLog(t, log); l != "-p --output-format json --session-id th-1 --dangerously-skip-permissions\n" {
		t.Fatalf("log %q", l)
	}
}

func TestClaudeSendQueuesWhileRunning(t *testing.T) {
	log := useFake(t, "BMESH_CLAUDE_COMMAND", "fake-claude.sh", "FAKE_LOG")
	t.Setenv("FAKE_SLEEP", "1")
	r, repo, brief := claudeRepo(t)
	if _, err := r.Start(context.Background(), StartSpec{RepoPath: repo, BriefPath: brief, ThreadID: "th-1"}); err != nil {
		t.Fatal(err)
	}
	meshtest.Until(t, "the fake to start", func() bool { return readLog(t, log) != "" })
	if st, err := r.Status(context.Background(), "th-1"); err != nil || !st.Running {
		t.Fatalf("status %+v %v", st, err)
	}
	if err := r.Send(context.Background(), "th-1", "next please"); err != nil {
		t.Fatal(err)
	}
	if n := len(lines(t, log)); n != 1 {
		t.Fatalf("send ran claude mid-turn: %q", readLog(t, log))
	}
	t.Setenv("FAKE_SLEEP", "")
	meshtest.Until(t, "Status to start the queued send", func() bool {
		if _, err := r.Status(context.Background(), "th-1"); err != nil {
			t.Fatal(err)
		}
		return len(lines(t, log)) == 2
	})
	if l := lines(t, log)[1]; l != "-p --output-format json --resume th-1 --dangerously-skip-permissions" {
		t.Fatalf("resume argv %q", l)
	}
	meshtest.Until(t, "turn 2 to end", idle(t, r, "th-1"))
	if in := readLog(t, log+".stdin"); in != "do the thingnext please" {
		t.Fatalf("stdin %q", in)
	}
	if q, _ := os.ReadDir(filepath.Join(r.Dir, "th-1", "queue")); len(q) != 0 {
		t.Fatalf("queue not drained: %v", q)
	}
}

func TestClaudeSendWhenIdleResumes(t *testing.T) {
	log := useFake(t, "BMESH_CLAUDE_COMMAND", "fake-claude.sh", "FAKE_LOG")
	r, repo, brief := claudeRepo(t)
	if _, err := r.Start(context.Background(), StartSpec{RepoPath: repo, BriefPath: brief, ThreadID: "th-1", Model: "m", Effort: "xhigh"}); err != nil {
		t.Fatal(err)
	}
	meshtest.Until(t, "turn 1 to end", idle(t, r, "th-1"))
	t.Setenv("FAKE_RESULT", "second")
	if err := r.Send(context.Background(), "th-1", "wake up"); err != nil {
		t.Fatal(err)
	}
	meshtest.Until(t, "turn 2 to end", func() bool {
		st, err := r.Status(context.Background(), "th-1")
		return err == nil && !st.Running && st.LastReply == "second"
	})
	// The model is named on the resume too: claude does not restore it.
	if l := lines(t, log); len(l) != 2 || l[1] != "-p --output-format json --resume th-1 --model m --effort xhigh --dangerously-skip-permissions" {
		t.Fatalf("log %q", l)
	}
}

// An is_error result ends the turn as a failure the reply names.
func TestClaudeErrorResultShowsInStatus(t *testing.T) {
	useFake(t, "BMESH_CLAUDE_COMMAND", "fake-claude.sh", "FAKE_LOG")
	t.Setenv("FAKE_IS_ERROR", "1")
	t.Setenv("FAKE_RESULT", "session gone")
	r, repo, brief := claudeRepo(t)
	if _, err := r.Start(context.Background(), StartSpec{RepoPath: repo, BriefPath: brief, ThreadID: "th-1"}); err != nil {
		t.Fatal(err)
	}
	meshtest.Until(t, "turn 1 to end", idle(t, r, "th-1"))
	st, err := r.Status(context.Background(), "th-1")
	if err != nil || st.Running || !strings.HasPrefix(st.LastReply, "[error") || !strings.Contains(st.LastReply, "session gone") {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestClaudeUnknownThread(t *testing.T) {
	r := Claude{Dir: t.TempDir()}
	if _, err := r.Status(context.Background(), "th-x"); !errors.Is(err, ErrNoThread) {
		t.Fatalf("status err %v", err)
	}
	if err := r.Send(context.Background(), "th-x", "hi"); !errors.Is(err, ErrNoThread) {
		t.Fatalf("send err %v", err)
	}
}

func TestClaudeInterruptKillsTurn(t *testing.T) {
	log := useFake(t, "BMESH_CLAUDE_COMMAND", "fake-claude.sh", "FAKE_LOG")
	t.Setenv("FAKE_SLEEP", "30")
	r, repo, brief := claudeRepo(t)
	if _, err := r.Start(context.Background(), StartSpec{RepoPath: repo, BriefPath: brief, ThreadID: "th-1"}); err != nil {
		t.Fatal(err)
	}
	meshtest.Until(t, "the fake to start", func() bool { return readLog(t, log) != "" })
	if st, _ := r.Status(context.Background(), "th-1"); !st.Running {
		t.Fatalf("status %+v", st)
	}
	// A send queued behind the turn is dropped with it: nothing restarts
	// a stopped session.
	if err := r.Send(context.Background(), "th-1", "wake"); err != nil {
		t.Fatal(err)
	}
	if err := r.Interrupt(context.Background(), "th-1"); err != nil {
		t.Fatal(err)
	}
	meshtest.Until(t, "the turn to die", idle(t, r, "th-1"))
	if strings.Count(readLog(t, log), "--resume") != 0 {
		t.Fatalf("a queued send ran after the stop: %q", readLog(t, log))
	}
	if err := r.Interrupt(context.Background(), "th-1"); err != nil {
		t.Fatalf("interrupt when idle: %v", err)
	}
}

// A send that arrives after a turn ended but while an earlier send is still
// queued goes behind it, so sends run in the order they came.
func TestClaudeSendKeepsOrderBehindQueue(t *testing.T) {
	log := useFake(t, "BMESH_CLAUDE_COMMAND", "fake-claude.sh", "FAKE_LOG")
	t.Setenv("FAKE_SLEEP", "1")
	r, repo, brief := claudeRepo(t)
	if _, err := r.Start(context.Background(), StartSpec{RepoPath: repo, BriefPath: brief, ThreadID: "th-1"}); err != nil {
		t.Fatal(err)
	}
	meshtest.Until(t, "the fake to start", func() bool { return readLog(t, log) != "" })
	if err := r.Send(context.Background(), "th-1", "A"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_SLEEP", "")
	meshtest.Until(t, "turn 1 to end", func() bool {
		d, _ := r.dir("th-1")
		return !running(d, 1)
	})
	// Turn 1 is over, A is queued, nothing has polled Status: B must queue behind A.
	if err := r.Send(context.Background(), "th-1", "B"); err != nil {
		t.Fatal(err)
	}
	if n := len(lines(t, log)); n != 1 {
		t.Fatalf("B ran ahead of A: %q", readLog(t, log))
	}
	// At least i+2 turns, not exactly: a turn that ends between two polls
	// lets the next poll's Status start the send after it. The stdin check
	// below holds the order.
	for i := 0; i < 2; i++ {
		meshtest.Until(t, "Status to start the next queued send", func() bool {
			if _, err := r.Status(context.Background(), "th-1"); err != nil {
				t.Fatal(err)
			}
			return len(lines(t, log)) >= i+2
		})
		meshtest.Until(t, "the turn to end", idle(t, r, "th-1"))
	}
	if in := readLog(t, log+".stdin"); in != "do the thingAB" {
		t.Fatalf("stdin %q", in)
	}
}

// A turn that exits without a result reads as idle with the failure, not
// as the previous reply.
func TestClaudeFailedTurnShowsInStatus(t *testing.T) {
	useFake(t, "BMESH_CLAUDE_COMMAND", "fake-claude.sh", "FAKE_LOG")
	t.Setenv("FAKE_RESULT", "first")
	r, repo, brief := claudeRepo(t)
	if _, err := r.Start(context.Background(), StartSpec{RepoPath: repo, BriefPath: brief, ThreadID: "th-1"}); err != nil {
		t.Fatal(err)
	}
	meshtest.Until(t, "turn 1 to end", idle(t, r, "th-1"))
	t.Setenv("FAKE_NOJSON", "1")
	t.Setenv("FAKE_EXIT", "1")
	if err := r.Send(context.Background(), "th-1", "again"); err != nil {
		t.Fatal(err)
	}
	meshtest.Until(t, "turn 2 to end", idle(t, r, "th-1"))
	st, err := r.Status(context.Background(), "th-1")
	if err != nil || st.Running || !strings.HasPrefix(st.LastReply, "[turn 2 failed]") {
		t.Fatalf("%+v %v", st, err)
	}
}
