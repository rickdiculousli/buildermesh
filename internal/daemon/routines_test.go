package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

const requester = "app-001"

// rfix is a fixture with one configured repo "app" and the routine loop's
// tunables shortened.
type rfix struct {
	*fixture
	repo    string
	replies map[string]mesh.Reply
	inbox   jetstream.Consumer
}

func tgit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitFiles writes files (relative path → content) into repo and commits
// them; it returns the commit.
func commitFiles(t *testing.T, repo string, files map[string]string) string {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		tgit(t, repo, "add", name)
	}
	tgit(t, repo, "commit", "-q", "-m", "files")
	return tgit(t, repo, "rev-parse", "HEAD")
}

func tune(t *testing.T) {
	t.Helper()
	saved := []time.Duration{kindRescan, ackWaitExtra, busyNakDelay, fetchWait, drainWait}
	kindRescan, ackWaitExtra, busyNakDelay, fetchWait, drainWait =
		time.Minute, time.Second, time.Second, 200*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() {
		kindRescan, ackWaitExtra, busyNakDelay, fetchWait, drainWait = saved[0], saved[1], saved[2], saved[3], saved[4]
	})
}

func routineSetup(t *testing.T, files map[string]string) *rfix {
	t.Helper()
	f := setup(t)
	tune(t)
	f.d.Now = time.Now
	repo := filepath.Join(t.TempDir(), "app")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	tgit(t, repo, "init", "-q", "-b", "main")
	commitFiles(t, repo, files)
	f.d.Cfg.Repos = map[string]string{"app": repo}
	f.d.Checkouts = filepath.Join(t.TempDir(), "checkouts")
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-claude.sh"))
	if err != nil {
		t.Fatal(err)
	}
	f.d.ClaudeBin = "sh " + fake
	if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: requester, Role: "lead", Host: testHost}); err != nil {
		t.Fatal(err)
	}
	p := mesh.Path{requester}
	if err := mesh.EnsureInbox(f.ctx, f.c, p); err != nil {
		t.Fatal(err)
	}
	inbox, err := f.c.JS.Consumer(f.ctx, mesh.StreamMesh, mesh.InboxConsumer(p))
	if err != nil {
		t.Fatal(err)
	}
	return &rfix{fixture: f, repo: repo, replies: map[string]mesh.Reply{}, inbox: inbox}
}

// start runs the routine loop until the returned stop is called (or the
// test ends).
func (f *rfix) start(t *testing.T) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan struct{})
	go func() { routines(ctx, f.d); close(done) }()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Error("routines did not return")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func (f *rfix) queue(t *testing.T, j mesh.Job) string {
	t.Helper()
	if j.Repo == "" {
		j.Repo = "app"
	}
	j.ID = nextID()
	if _, err := mesh.Queue(f.ctx, f.c, mesh.Path{requester}, j, f.repo); err != nil {
		t.Fatal(err)
	}
	return j.ID
}

var idSeq struct {
	sync.Mutex
	n int
}

// nextID is a job id whose first 8 characters differ between jobs and
// hold capitals, as a fresh run's path lowercases them.
func nextID() string {
	idSeq.Lock()
	defer idSeq.Unlock()
	idSeq.n++
	return fmt.Sprintf("Job%05d", idSeq.n) + nuid.Next()
}

// poll pulls the requester's inbox for up to d, recording replies.
func (f *rfix) poll(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		m, err := f.inbox.Next(jetstream.FetchMaxWait(100 * time.Millisecond))
		if err == nil {
			var msg mesh.Message
			if jerr := json.Unmarshal(m.Data(), &msg); jerr == nil && msg.Kind == "reply" && msg.Reply != nil {
				if _, dup := f.replies[msg.Reply.Job]; dup {
					t.Errorf("second reply to %s: %+v", msg.Reply.Job, msg.Reply)
				}
				f.replies[msg.Reply.Job] = *msg.Reply
			}
			m.Ack()
		} else if !errors.Is(err, nats.ErrTimeout) && !errors.Is(err, jetstream.ErrNoMessages) {
			t.Fatal(err)
		}
		if !time.Now().Before(deadline) {
			return
		}
	}
}

// reply waits up to d for the reply to job id.
func (f *rfix) reply(t *testing.T, id string, d time.Duration) mesh.Reply {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if r, ok := f.replies[id]; ok {
			return r
		}
		f.poll(t, 100*time.Millisecond)
	}
	if r, ok := f.replies[id]; ok {
		return r
	}
	t.Fatalf("no reply to %s within %s", id, d)
	return mesh.Reply{}
}

func (f *rfix) noReply(t *testing.T, id string, d time.Duration) {
	t.Helper()
	f.poll(t, d)
	if r, ok := f.replies[id]; ok {
		t.Fatalf("unexpected reply to %s: %+v", id, r)
	}
}

// waitFor polls cond every 50ms for up to d.
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func lines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(b))
}

func (f *rfix) state(t *testing.T, path string) string {
	t.Helper()
	s, _, err := mesh.ReadState(f.ctx, f.c, path)
	if err != nil {
		t.Fatal(err)
	}
	return s.State
}

func (f *rfix) events(t *testing.T, path, event string) int {
	t.Helper()
	p, err := mesh.ParsePath(path)
	if err != nil {
		t.Fatal(err)
	}
	hist, err := mesh.History(f.ctx, f.c, mesh.EventSubject(p, event), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return len(hist)
}

// waitStarted waits until the instance at path has announced itself.
func (f *rfix) waitStarted(t *testing.T, path string, n int) {
	t.Helper()
	waitFor(t, 20*time.Second, path+" started", func() bool { return f.events(t, path, "started") >= n })
	// started is followed by the reset to idle; then the instance waits.
	waitFor(t, 5*time.Second, path+" idle", func() bool { return f.state(t, path) == "idle" })
}

func TestPooledKindDrainsOnGoAndOnIdle(t *testing.T) {
	log := filepath.Join(t.TempDir(), "runs")
	f := routineSetup(t, map[string]string{".bmesh/routines/verify.toml": `
mode = "pooled"
timeout = "30s"
batch_idle = "5s"
command = "echo $BMESH_JOB >> ` + log + `; echo ran $BMESH_JOB"
`})
	f.start(t)
	f.waitStarted(t, "routine.verify.cmd-1", 1)

	id1 := f.queue(t, mesh.Job{Kind: "verify"})
	id2 := f.queue(t, mesh.Job{Kind: "verify"})
	f.noReply(t, id1, 2500*time.Millisecond)
	if got := lines(t, log); len(got) != 0 {
		t.Fatalf("ran before batch_idle: %q", got)
	}
	r1 := f.reply(t, id1, 15*time.Second)
	r2 := f.reply(t, id2, 15*time.Second)
	if !r1.OK || r1.Output != "ran "+id1+"\n" || r1.By != "routine.verify.cmd-1" || !r2.OK {
		t.Fatalf("replies %+v %+v", r1, r2)
	}
	if got := lines(t, log); strings.Join(got, " ") != id1+" "+id2 {
		t.Fatalf("runs %q, want %s %s", got, id1, id2)
	}

	// A job with go runs at once.
	start := time.Now()
	id3 := f.queue(t, mesh.Job{Kind: "verify", Go: true})
	if r := f.reply(t, id3, 10*time.Second); !r.OK || time.Since(start) > 3500*time.Millisecond {
		t.Fatalf("go job: %+v after %s", r, time.Since(start))
	}

	// A go message starts a waiting job, even while the held job keeps the
	// consumer at MaxAckPending.
	f.poll(t, time.Second) // let the drain after id3 end
	start = time.Now()
	id4 := f.queue(t, mesh.Job{Kind: "verify"})
	if _, err := mesh.Send(f.ctx, f.c, mesh.Message{Kind: "message", Text: "go", From: requester, To: "routine.verify"}); err != nil {
		t.Fatal(err)
	}
	if r := f.reply(t, id4, 10*time.Second); !r.OK || time.Since(start) > 3500*time.Millisecond {
		t.Fatalf("go message: %+v after %s", r, time.Since(start))
	}
	if got := lines(t, log); len(got) != 4 || got[2] != id3 || got[3] != id4 {
		t.Fatalf("runs %q", got)
	}
	// Each job: "job <id> started" and "job <id> ok <ms>".
	if n := f.events(t, "routine.verify.cmd-1", "progress"); n != 8 {
		t.Fatalf("progress events %d, want 8", n)
	}
}

func TestJobDeliveredOnceAndRedeliveredAfterDeath(t *testing.T) {
	runs := filepath.Join(t.TempDir(), "runs")
	f := routineSetup(t, map[string]string{".bmesh/routines/slow.toml": `
mode = "pooled"
pool = 2
timeout = "1m"
batch_idle = "1s"
command = "echo x >> ` + runs + `; if [ $(wc -l < ` + runs + `) -ge 2 ]; then echo second; else sleep 30; fi; true"
`})
	ackWaitExtra = 3*time.Second - time.Minute // AckWait = timeout + extra = 3s
	stop := f.start(t)
	f.waitStarted(t, "routine.slow.cmd-1", 1)
	f.waitStarted(t, "routine.slow.cmd-2", 1)

	id := f.queue(t, mesh.Job{Kind: "slow", Go: true})
	waitFor(t, 10*time.Second, "first run", func() bool { return len(lines(t, runs)) == 1 })
	// Held past AckWait by one instance, never handed to the other.
	deadline := time.Now().Add(3500 * time.Millisecond)
	for time.Now().Before(deadline) {
		working := 0
		for _, p := range []string{"routine.slow.cmd-1", "routine.slow.cmd-2"} {
			if f.state(t, p) == "working" {
				working++
			}
		}
		if working != 1 || len(lines(t, runs)) != 1 {
			t.Fatalf("working %d, runs %d", working, len(lines(t, runs)))
		}
		time.Sleep(100 * time.Millisecond)
	}
	died := time.Now()
	stop() // the instance dies mid-job: no ack, no reply
	f.noReply(t, id, 200*time.Millisecond)

	// Redelivered only after AckWait (3s, the last heartbeat at most 1s
	// before the death), not at once as a NAK would.
	f.start(t)
	for time.Since(died) < 1800*time.Millisecond {
		if n := len(lines(t, runs)); n != 1 {
			t.Fatalf("rerun %s after the death, before AckWait", time.Since(died))
		}
		time.Sleep(50 * time.Millisecond)
	}
	r := f.reply(t, id, 20*time.Second)
	if !r.OK || r.Output != "second\n" {
		t.Fatalf("reply %+v", r)
	}
	if n := len(lines(t, runs)); n != 2 {
		t.Fatalf("runs %d, want 2", n)
	}
	f.poll(t, time.Second) // a second reply would fail the test
}

func TestFreshKindSpawnsPerJob(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/build.toml": `
mode = "fresh"
pool = 2
timeout = "30s"
command = "echo $BMESH_FROM; pwd"
`})
	f.start(t)
	id1 := f.queue(t, mesh.Job{Kind: "build"})
	id2 := f.queue(t, mesh.Job{Kind: "build"})
	for _, id := range []string{id1, id2} {
		r := f.reply(t, id, 20*time.Second)
		path := "routine.build.f" + strings.ToLower(id[:8])
		out := strings.Fields(r.Output)
		if !r.OK || r.By != path || len(out) != 2 || out[0] != path {
			t.Fatalf("reply %+v, want by %s", r, path)
		}
		waitFor(t, 10*time.Second, path+" done", func() bool { return f.state(t, path) == "done" })
		waitFor(t, 10*time.Second, out[1]+" removed", func() bool {
			_, err := os.Stat(out[1])
			return errors.Is(err, os.ErrNotExist)
		})
		n, ok, err := mesh.GetNode(f.ctx, f.c, path)
		if err != nil || !ok || n.Role != "routine" || n.Repo != "app" || n.Host != testHost {
			t.Fatalf("node %+v %v %v", n, ok, err)
		}
		if f.events(t, path, "started") != 1 || f.events(t, path, "done") != 1 {
			t.Fatalf("%s events: started/done not 1 each", path)
		}
		// Progress names the requester, so its tree stays alive.
		p, _ := mesh.ParsePath(path)
		hist, err := mesh.History(f.ctx, f.c, mesh.EventSubject(p, "progress"), time.Time{})
		if err != nil || len(hist) != 2 {
			t.Fatalf("%s progress %d %v, want 2", path, len(hist), err)
		}
		for i, want := range []string{"job " + id + " started", "job " + id + " ok "} {
			var ev mesh.Event
			if err := json.Unmarshal(hist[i].Data, &ev); err != nil || ev.To != requester || !strings.HasPrefix(ev.Text, want) {
				t.Fatalf("%s progress %d: %+v %v, want %q to %s", path, i, ev, err, want, requester)
			}
		}
	}
}

func TestLeaseLostKillsJob(t *testing.T) {
	f := routineSetup(t, map[string]string{
		".bmesh/config.toml": "[slots]\ngame = 1\n",
		".bmesh/routines/play.toml": `
mode = "pooled"
slots = ["game"]
timeout = "6s"
command = "sleep 30"
`})
	ackWaitExtra = 0 // lease ttl 6s, renewed every 2s
	f.start(t)
	id := f.queue(t, mesh.Job{Kind: "play", Go: true})
	kv, err := f.c.JS.KeyValue(f.ctx, mesh.BucketLeases)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "lease game.0", func() bool {
		e, err := kv.Get(f.ctx, "game.0")
		return err == nil && strings.Contains(string(e.Value()), "routine.play.cmd-1")
	})
	// The lease runs out (as if renewals had stalled) and another holder
	// takes the place; retried when a renewal lands between the two.
	expired := []byte(`{"holder":"routine.play.cmd-1","expires_at":"2000-01-01T00:00:00Z"}`)
	waitFor(t, 10*time.Second, "other to take game.0", func() bool {
		if _, err := kv.Put(f.ctx, "game.0", expired); err != nil {
			t.Fatal(err)
		}
		key, err := mesh.AcquireLease(f.ctx, f.c, "game", "other", time.Hour, 1, time.Now())
		if err != nil && !errors.Is(err, mesh.ErrBusy) {
			t.Fatal(err)
		}
		return key == "game.0"
	})
	r := f.reply(t, id, 15*time.Second)
	if r.OK || r.Output != "lease lost" || r.By != "routine.play.cmd-1" {
		t.Fatalf("reply %+v", r)
	}
	// The instance released nothing of other's.
	ls, err := mesh.ListLeases(f.ctx, f.c)
	if err != nil || len(ls) != 1 || ls[0].Key != "game.0" || ls[0].Holder != "other" {
		t.Fatalf("leases after loss %+v %v", ls, err)
	}
	if rs, err := mesh.ListResults(f.ctx, f.c, "play"); err != nil || len(rs) != 0 {
		t.Fatalf("a lost job recorded %+v %v", rs, err)
	}
}

// A job at a commit the daemon's clone lacks (pushed to origin from another
// box) runs after one fetch.
func TestJobCommitFetchedFromOrigin(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/verify.toml": "timeout = \"30s\"\ncommand = \"cat b.txt\"\n"})
	other := filepath.Join(t.TempDir(), "other")
	tgit(t, filepath.Dir(other), "clone", "-q", f.repo, other)
	tgit(t, f.repo, "remote", "add", "origin", other)
	tgit(t, f.repo, "fetch", "-q", "origin")
	f.start(t)
	f.waitStarted(t, "routine.verify.cmd-1", 1) // the rescan's fetch is over
	pushed := commitFiles(t, other, map[string]string{"b.txt": "from origin\n"})
	if _, err := exec.Command("git", "-C", f.repo, "cat-file", "-e", pushed+"^{commit}").Output(); err == nil {
		t.Fatal("the daemon's clone already has the commit")
	}
	id := nextID()
	j := mesh.Job{ID: id, Kind: "verify", Repo: "app", Commit: pushed, Go: true}
	if _, err := mesh.Queue(f.ctx, f.c, mesh.Path{requester}, j, other); err != nil {
		t.Fatal(err)
	}
	if r := f.reply(t, id, 20*time.Second); !r.OK || r.Output != "from origin\n" || r.Tree != tgit(t, other, "rev-parse", pushed+"^{tree}") {
		t.Fatalf("reply %+v", r)
	}
	// A commit no remote has is still refused (sent as is: Queue would
	// refuse it first).
	bad := mesh.Job{ID: nextID(), Kind: "verify", Repo: "app", Commit: strings.Repeat("ab", 20), Args: []string{}, Go: true}
	if _, err := mesh.Send(f.ctx, f.c, mesh.Message{ID: bad.ID, Kind: "job", From: requester, To: "routine.verify", Job: &bad}); err != nil {
		t.Fatal(err)
	}
	r := f.reply(t, bad.ID, 20*time.Second)
	if r.OK || !strings.Contains(r.Output, "not a commit") {
		t.Fatalf("unknown commit reply %+v", r)
	}
}

func TestDeterministicAnsweredFromRecordInDaemon(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "mark")
	f := routineSetup(t, map[string]string{".bmesh/routines/check.toml": `
mode = "pooled"
deterministic = true
timeout = "30s"
command = "echo ran >> ` + mark + `; echo fresh-run"
`})
	tree := tgit(t, f.repo, "rev-parse", "HEAD^{tree}")
	commit := tgit(t, f.repo, "rev-parse", "HEAD")
	if err := mesh.PutResult(f.ctx, f.c, mesh.Result{Kind: "check", Tree: tree, Commit: commit,
		OK: false, Output: "from record", By: "someone"}); err != nil {
		t.Fatal(err)
	}
	f.start(t)
	key := mesh.ResultKey("check", tree, nil, "")
	r := f.reply(t, f.queue(t, mesh.Job{Kind: "check", Go: true}), 20*time.Second)
	if r.OK || r.Output != "from record" || r.By != "someone" || r.ResultKey != key || r.Tree != tree {
		t.Fatalf("reply %+v", r)
	}
	if _, err := os.Stat(mark); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("ran despite a record")
	}

	r = f.reply(t, f.queue(t, mesh.Job{Kind: "check", Go: true, Rerun: true}), 20*time.Second)
	if !r.OK || r.Output != "fresh-run\n" || r.By != "routine.check.cmd-1" || r.ResultKey != key {
		t.Fatalf("rerun reply %+v", r)
	}
	rec, ok, err := mesh.GetResult(f.ctx, f.c, key)
	if err != nil || !ok || !rec.OK || rec.By != "routine.check.cmd-1" || rec.Output != "fresh-run\n" || rec.Commit != commit {
		t.Fatalf("record %+v %v %v", rec, ok, err)
	}
}

func TestKindReadFromJobCommit(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/verify.toml": "timeout = \"30s\"\ncommand = \"echo c1\"\n"})
	c1 := tgit(t, f.repo, "rev-parse", "HEAD")
	c2 := commitFiles(t, f.repo, map[string]string{".bmesh/routines/verify.toml": "timeout = \"30s\"\ncommand = \"echo c2\"\n"})
	f.start(t)
	if r := f.reply(t, f.queue(t, mesh.Job{Kind: "verify", Commit: c1, Go: true}), 20*time.Second); !r.OK || r.Output != "c1\n" {
		t.Fatalf("job at c1: %+v", r)
	}
	if r := f.reply(t, f.queue(t, mesh.Job{Kind: "verify", Commit: c2, Go: true}), 20*time.Second); !r.OK || r.Output != "c2\n" {
		t.Fatalf("job at c2: %+v", r)
	}
	// Every run that names a commit records, deterministic or not (R12).
	tree1 := tgit(t, f.repo, "rev-parse", c1+"^{tree}")
	if rec, ok, err := mesh.GetResult(f.ctx, f.c, mesh.ResultKey("verify", tree1, nil, "")); err != nil || !ok || rec.Output != "c1\n" {
		t.Fatalf("record at c1 %+v %v %v", rec, ok, err)
	}
}

func TestUnknownModelReplies(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/review.toml": `
models = ["m-a", "m-b"]
prompt = "P"
timeout = "30s"
batch_idle = "1s"
`})
	t.Setenv("FAKE_RESULT", "VERDICT: ok")
	f.start(t)
	r := f.reply(t, f.queue(t, mesh.Job{Kind: "review", Model: "m-x", Go: true}), 20*time.Second)
	if r.OK || r.Output != "unknown model m-x" {
		t.Fatalf("unknown model reply %+v", r)
	}
	r = f.reply(t, f.queue(t, mesh.Job{Kind: "review", Repo: "elsewhere", Go: true}), 20*time.Second)
	if r.OK || r.Output != "unknown repo elsewhere" {
		t.Fatalf("unknown repo reply %+v", r)
	}
	tree := tgit(t, f.repo, "rev-parse", "HEAD^{tree}")
	r = f.reply(t, f.queue(t, mesh.Job{Kind: "review", Model: "m-b", Go: true}), 20*time.Second)
	key := mesh.ResultKey("review", tree, nil, "m-b")
	if !r.OK || r.Output != "VERDICT: ok" || r.By != "routine.review.m-b-1" || r.ResultKey != key {
		t.Fatalf("m-b reply %+v", r)
	}
	if rec, ok, err := mesh.GetResult(f.ctx, f.c, key); err != nil || !ok || rec.Model != "m-b" || rec.By != "routine.review.m-b-1" {
		t.Fatalf("record %+v %v %v", rec, ok, err)
	}
	n, ok, err := mesh.GetNode(f.ctx, f.c, "routine.review.m-a-1")
	if err != nil || !ok || n.Model != "m-a" || n.Role != "routine" {
		t.Fatalf("m-a node %+v %v %v", n, ok, err)
	}
}

// A job fetched while the loop stops is returned to the inbox, not refused.
func TestJobFetchedWhileStoppingStaysQueued(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/verify.toml": "timeout = \"30s\"\ncommand = \"echo ran\"\n"})
	fetchWait = 2 * time.Second // the dispatcher's fetch outlives the stop
	stop := f.start(t)
	f.waitStarted(t, "routine.verify.cmd-1", 1)
	time.Sleep(300 * time.Millisecond) // the dispatcher is inside a fetch
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	id := f.queue(t, mesh.Job{Kind: "verify", Go: true})
	<-done
	f.noReply(t, id, 500*time.Millisecond)
	if n, err := mesh.Depth(f.ctx, f.c, "verify"); err != nil || n != 1 {
		t.Fatalf("depth after stop %d %v, want the job still queued", n, err)
	}
}
