package mesh_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// gitRepo makes a repo with one commit and returns its path.
func gitRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, repo, "init", "-b", "main")
	meshtest.CommitFiles(t, repo, map[string]string{"a.txt": "a\n"})
	return repo
}

func TestQueueFillsTreeAndDedupesByID(t *testing.T) {
	c, ctx := connect(t)
	repo := gitRepo(t)
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD^{tree}").Output()
	if err != nil {
		t.Fatal(err)
	}
	tree := strings.TrimSpace(string(out))
	// A daemon's consumer settings survive a requester's Queue.
	if _, err := KindConsumer(ctx, c, "verify", 3, time.Minute); err != nil {
		t.Fatal(err)
	}
	from, _ := ParsePath("app-001")
	j := Job{ID: "job-1", Kind: "verify"}
	if dup, err := Queue(ctx, c, from, j, repo); err != nil || dup {
		t.Fatal(dup, err)
	}
	if dup, err := Queue(ctx, c, from, j, repo); err != nil || !dup {
		t.Fatal(dup, err)
	}
	if n, err := Depth(ctx, c, "verify"); err != nil || n != 1 {
		t.Fatalf("depth %d %v", n, err)
	}
	cons, err := c.JS.Consumer(ctx, StreamMesh, InboxConsumer(RoutinePath("verify")))
	if err != nil {
		t.Fatal(err)
	}
	info, err := cons.Info(ctx)
	if err != nil || info.Config.MaxAckPending != 3 || info.Config.AckWait != time.Minute {
		t.Fatalf("consumer overwritten: %+v %v", info.Config, err)
	}
	msgs, err := Fetch(ctx, c, RoutinePath("verify"), true)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("fetch %v %v", msgs, err)
	}
	m := msgs[0]
	if m.Kind != "job" || m.From != "app-001" || m.To != "routine.verify" || m.ID != "job-1" || m.Job == nil {
		t.Fatalf("message %+v", m)
	}
	if m.Job.Tree != tree || len(m.Job.Commit) != 40 || m.Job.Repo != "demo" || m.Job.Args == nil {
		t.Fatalf("job %+v", *m.Job)
	}
	if n, err := Depth(ctx, c, "lint"); err != nil || n != 0 {
		t.Fatalf("depth without consumer %d %v", n, err)
	}
	if _, err := Queue(ctx, c, from, Job{Kind: "Bad"}, repo); err == nil {
		t.Fatal("bad kind queued")
	}
	if _, err := Queue(ctx, c, from, Job{Kind: "verify", Commit: "--output=x"}, repo); err == nil {
		t.Fatal("option-like commit queued")
	}
}

func TestSendReplyTruncatesAndFetchMatchLeavesOthers(t *testing.T) {
	c, ctx := connect(t)
	me, _ := ParsePath("app-001")
	if _, err := Send(ctx, c, Message{From: "x", To: "app-001", Text: "other"}); err != nil {
		t.Fatal(err)
	}
	r := Reply{Job: "j1", Kind: "verify", OK: true, Output: "first line\n" + strings.Repeat("x", 70<<10)}
	if err := SendReply(ctx, c, RoutinePath("verify"), me, r); err != nil {
		t.Fatal(err)
	}
	m, ok, err := FetchMatch(ctx, c, me, func(m Message) bool { return m.Reply != nil && m.Reply.Job == "j1" })
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if m.Kind != "reply" || m.Text != "first line" || !strings.HasSuffix(m.Reply.Output, "\n[truncated]") {
		t.Fatalf("reply %q %q", m.Kind, m.Text)
	}
	if n, err := Unread(ctx, c, me); err != nil || n != 1 {
		t.Fatalf("unread %d %v", n, err)
	}
}
