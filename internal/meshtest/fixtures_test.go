package meshtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

func TestGitAndCommitFiles(t *testing.T) {
	repo := t.TempDir()
	Git(t, repo, "init", "-q", "-b", "main")
	sha := CommitFiles(t, repo, map[string]string{"a.txt": "one\n", "sub/dir/b.txt": "two\n"})
	if len(sha) != 40 {
		t.Fatalf("sha %q: want a full sha", sha)
	}
	if head := Git(t, repo, "rev-parse", "HEAD"); head != sha {
		t.Fatalf("HEAD %s, CommitFiles returned %s", head, sha)
	}
	if got := Git(t, repo, "show", "HEAD:sub/dir/b.txt"); got != "two" {
		t.Fatalf("committed b.txt = %q", got)
	}
	if got := Git(t, repo, "log", "-1", "--format=%an <%ae>"); got != "t <t@t>" {
		t.Fatalf("author %q", got)
	}
	if b, err := os.ReadFile(filepath.Join(repo, "a.txt")); err != nil || string(b) != "one\n" {
		t.Fatalf("a.txt = %q, %v", b, err)
	}
	second := CommitFiles(t, repo, map[string]string{"a.txt": "changed\n"})
	if parent := Git(t, repo, "rev-parse", second+"^"); parent != sha {
		t.Fatalf("parent %s, want %s", parent, sha)
	}
}

func TestSyncBufferConcurrentWrites(t *testing.T) {
	var b SyncBuffer
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fmt.Fprintf(&b, "%02d\n", i)
			_ = b.String()
		}()
	}
	wg.Wait()
	if n := len(b.String()); n != 20*3 {
		t.Fatalf("len %d, want 60", n)
	}
}

func client(t *testing.T) (*mesh.Client, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	c, err := mesh.Connect(ctx, Start(t), "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	return c, ctx
}

func TestForceStateBypassesTable(t *testing.T) {
	c, ctx := client(t)
	ForceState(t, c, "app-001", mesh.State{State: "done", Text: "landed"})
	s, rev, err := mesh.ReadState(ctx, c, "app-001")
	if err != nil {
		t.Fatal(err)
	}
	if rev == 0 || s.State != "done" || s.Text != "landed" || s.At == "" {
		t.Fatalf("state %+v rev %d", s, rev)
	}
	ForceState(t, c, "app-001", mesh.State{State: "silent", At: "2026-01-01T00:00:00Z"})
	s, _, _ = mesh.ReadState(ctx, c, "app-001")
	if s.State != "silent" || s.At != "2026-01-01T00:00:00Z" {
		t.Fatalf("state %+v", s)
	}
}

func TestQueueJobCountsTowardDepth(t *testing.T) {
	c, ctx := client(t)
	j := QueueJob(t, c, "verify", "app-001", mesh.Job{Commit: "abc", Text: "check it"})
	if j.Kind != "verify" || j.ID == "" || j.Args == nil {
		t.Fatalf("job %+v", j)
	}
	if d, err := mesh.Depth(ctx, c, "verify"); err != nil || d != 1 {
		t.Fatalf("depth %d, %v", d, err)
	}
	msgs, err := mesh.Pending(ctx, c, mesh.RoutinePath("verify"))
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("%d messages", len(msgs))
	}
	m := msgs[0]
	if m.ID != j.ID || m.Kind != "job" || m.From != "app-001" || m.To != "routine.verify" || m.Text != "check it" || m.Job == nil || m.Job.Commit != "abc" || m.Job.Kind != "verify" {
		t.Fatalf("message %+v job %+v", m, m.Job)
	}
}
