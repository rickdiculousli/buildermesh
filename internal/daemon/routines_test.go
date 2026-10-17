package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/web"
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
	meshtest.Git(t, repo, "init", "-q", "-b", "main")
	meshtest.CommitFiles(t, repo, files)
	f.d.Cfg.Repos = map[string]config.Repo{"app": {Path: repo}}
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
	f.waitStarted(t, "routine.verify.testhost-cmd-1", 1)

	id1 := f.queue(t, mesh.Job{Kind: "verify"})
	id2 := f.queue(t, mesh.Job{Kind: "verify"})
	f.noReply(t, id1, 2500*time.Millisecond)
	if got := lines(t, log); len(got) != 0 {
		t.Fatalf("ran before batch_idle: %q", got)
	}
	r1 := f.reply(t, id1, 15*time.Second)
	r2 := f.reply(t, id2, 15*time.Second)
	if !r1.OK || r1.Output != "ran "+id1+"\n" || r1.By != "routine.verify.testhost-cmd-1" || !r2.OK {
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
	if n := f.events(t, "routine.verify.testhost-cmd-1", "progress"); n != 8 {
		t.Fatalf("progress events %d, want 8", n)
	}
}

// A running job is kept in progress past AckWait, never handed to a second
// instance; a stop kills it and hands it back, so the next daemon reruns it
// at once rather than after AckWait.
func TestJobDeliveredOnceAndHandedBackOnStop(t *testing.T) {
	runs := filepath.Join(t.TempDir(), "runs")
	alive := filepath.Join(t.TempDir(), "alive")
	f := routineSetup(t, map[string]string{".bmesh/routines/slow.toml": `
mode = "pooled"
pool = 2
timeout = "1m"
batch_idle = "1s"
command = "echo x >> ` + runs + `; if [ $(wc -l < ` + runs + `) -ge 2 ]; then echo second; else sleep 30; echo late >> ` + alive + `; fi; true"
`})
	ackWaitExtra = 3*time.Second - time.Minute // AckWait = timeout + extra = 3s
	stop := f.start(t)
	f.waitStarted(t, "routine.slow.testhost-cmd-1", 1)
	f.waitStarted(t, "routine.slow.testhost-cmd-2", 1)

	id := f.queue(t, mesh.Job{Kind: "slow", Go: true})
	waitFor(t, 10*time.Second, "first run", func() bool { return len(lines(t, runs)) == 1 })
	// Held past AckWait by one instance, never handed to the other.
	deadline := time.Now().Add(3500 * time.Millisecond)
	for time.Now().Before(deadline) {
		working := 0
		for _, p := range []string{"routine.slow.testhost-cmd-1", "routine.slow.testhost-cmd-2"} {
			if f.state(t, p) == "working" {
				working++
			}
		}
		if working != 1 || len(lines(t, runs)) != 1 {
			t.Fatalf("working %d, runs %d", working, len(lines(t, runs)))
		}
		time.Sleep(100 * time.Millisecond)
	}
	if jobs, err := mesh.KindJobs(f.ctx, f.c, "slow"); err != nil || len(jobs) != 1 || jobs[0].ID != id ||
		jobs[0].State != "running" || !strings.HasPrefix(jobs[0].Holder, "routine.slow.testhost-cmd-") {
		t.Fatalf("jobs while running %+v %v", jobs, err)
	}
	stop() // mid-job: the run is killed and the job handed back, unanswered
	f.noReply(t, id, 200*time.Millisecond)
	if jobs, err := mesh.KindJobs(f.ctx, f.c, "slow"); err != nil || len(jobs) != 1 || jobs[0].State != "waiting" {
		t.Fatalf("jobs after stop %+v %v", jobs, err)
	}

	ackWaitExtra = time.Minute // the next daemon's AckWait is 2m: only a NAK explains a quick rerun
	f.start(t)
	r := f.reply(t, id, 20*time.Second)
	if !r.OK || r.Output != "second\n" {
		t.Fatalf("reply %+v", r)
	}
	if n := len(lines(t, runs)); n != 2 {
		t.Fatalf("runs %d, want 2", n)
	}
	if got := lines(t, alive); len(got) != 0 {
		t.Fatalf("the killed run went on: %q", got)
	}
	f.poll(t, time.Second) // a second reply would fail the test
	if jobs, err := mesh.KindJobs(f.ctx, f.c, "slow"); err != nil || len(jobs) != 0 {
		t.Fatalf("jobs after the reply %+v %v", jobs, err)
	}
}

// A job held in a batch when the daemon stops goes back to the inbox, and
// the next daemon runs it without waiting out AckWait.
func TestHeldJobHandedBackOnStop(t *testing.T) {
	runs := filepath.Join(t.TempDir(), "runs")
	f := routineSetup(t, map[string]string{".bmesh/routines/slow.toml": `
mode = "pooled"
timeout = "1m"
batch_idle = "2s"
command = "echo $BMESH_JOB >> ` + runs + `"
`})
	ackWaitExtra = time.Minute // AckWait 2m
	stop := f.start(t)
	f.waitStarted(t, "routine.slow.testhost-cmd-1", 1)

	id := f.queue(t, mesh.Job{Kind: "slow"})
	waitFor(t, 5*time.Second, "the job held", func() bool {
		jobs, err := mesh.KindJobs(f.ctx, f.c, "slow")
		return err == nil && len(jobs) == 1 && jobs[0].State == "held" && jobs[0].Holder == "routine.slow.testhost-cmd-1"
	})
	stop()
	if got := lines(t, runs); len(got) != 0 {
		t.Fatalf("ran before the stop: %q", got)
	}
	stopped := time.Now()
	f.start(t)
	r := f.reply(t, id, 20*time.Second)
	if !r.OK || time.Since(stopped) > 15*time.Second {
		t.Fatalf("reply %+v after %s", r, time.Since(stopped))
	}
	if got := lines(t, runs); len(got) != 1 || got[0] != id {
		t.Fatalf("runs %q", got)
	}
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
		waitFor(t, 10*time.Second, out[1]+" removed", func() bool {
			_, err := os.Stat(out[1])
			return errors.Is(err, os.ErrNotExist)
		})
		// The run's node, state and runner sample go with its checkout.
		waitFor(t, 10*time.Second, path+" forgotten", func() bool {
			_, ok, err := mesh.GetNode(f.ctx, f.c, path)
			return err == nil && !ok
		})
		if s, rev, err := mesh.ReadState(f.ctx, f.c, path); err != nil || rev != 0 || s.State != "idle" {
			t.Fatalf("%s state %+v rev %d %v, want none", path, s, rev, err)
		}
		if _, ok, err := mesh.GetRunner(f.ctx, f.c, path); err != nil || ok {
			t.Fatalf("%s runner sample kept: %v %v", path, ok, err)
		}
		// Its events stay for the map.
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

// A fresh model run past recycle_context is not recycled: its session ends
// with the run, and a recycled event would zero its use on the map.
func TestFreshModelRunNeverRecycles(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/review.toml": `
mode = "fresh"
model = "m-a"
prompt = "P"
timeout = "30s"
recycle_context = 10
`})
	t.Setenv("FAKE_RESULT", "VERDICT: ok")
	t.Setenv("FAKE_CACHE", "5000")
	f.start(t)
	id := f.queue(t, mesh.Job{Kind: "review"})
	r := f.reply(t, id, 20*time.Second)
	path := "routine.review.f" + strings.ToLower(id[:8])
	if !r.OK || r.By != path {
		t.Fatalf("reply %+v", r)
	}
	waitFor(t, 10*time.Second, path+" done", func() bool { return f.events(t, path, "done") == 1 })
	hist, err := mesh.History(f.ctx, f.c, "bm."+path+".>", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range hist {
		var ev mesh.Event
		if json.Unmarshal(m.Data, &ev) == nil && ev.Text == "recycled" {
			t.Fatalf("fresh run recycled: %+v", ev)
		}
	}
	if u := web.InstanceUsage(hist); u[path].Uses != 1 {
		t.Fatalf("uses %+v, want 1", u[path])
	}
}

// A queued job's effort reaches the claude it runs; a job without one
// passes none.
func TestModelJobCarriesItsEffort(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/review.toml": `
mode = "fresh"
model = "m-a"
prompt = "P"
timeout = "30s"
`})
	log := filepath.Join(t.TempDir(), "claude.log")
	t.Setenv("FAKE_LOG", log)
	t.Setenv("FAKE_RESULT", "VERDICT: ok")
	f.start(t)
	if r := f.reply(t, f.queue(t, mesh.Job{Kind: "review", Effort: "high", Go: true}), 20*time.Second); !r.OK {
		t.Fatalf("reply %+v", r)
	}
	if b, err := os.ReadFile(log); err != nil || !strings.Contains(string(b), "--effort high ") {
		t.Fatalf("claude argv %q %v: want --effort high", b, err)
	}
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	if r := f.reply(t, f.queue(t, mesh.Job{Kind: "review", Text: "again", Go: true}), 20*time.Second); !r.OK {
		t.Fatalf("reply %+v", r)
	}
	if b, err := os.ReadFile(log); err != nil || strings.Contains(string(b), "--effort") {
		t.Fatalf("claude argv %q %v: want no --effort", b, err)
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
		return err == nil && strings.Contains(string(e.Value()), "routine.play.testhost-cmd-1")
	})
	// The lease runs out (as if renewals had stalled) and another holder
	// takes the place; retried when a renewal lands between the two.
	expired := []byte(`{"holder":"routine.play.testhost-cmd-1","expires_at":"2000-01-01T00:00:00Z"}`)
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
	if r.OK || r.Output != "lease lost" || r.By != "routine.play.testhost-cmd-1" {
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
	meshtest.Git(t, filepath.Dir(other), "clone", "-q", f.repo, other)
	meshtest.Git(t, f.repo, "remote", "add", "origin", other)
	meshtest.Git(t, f.repo, "fetch", "-q", "origin")
	f.start(t)
	f.waitStarted(t, "routine.verify.testhost-cmd-1", 1) // the rescan's fetch is over
	pushed := meshtest.CommitFiles(t, other, map[string]string{"b.txt": "from origin\n"})
	if _, err := exec.Command("git", "-C", f.repo, "cat-file", "-e", pushed+"^{commit}").Output(); err == nil {
		t.Fatal("the daemon's clone already has the commit")
	}
	id := nextID()
	j := mesh.Job{ID: id, Kind: "verify", Repo: "app", Commit: pushed, Go: true}
	if _, err := mesh.Queue(f.ctx, f.c, mesh.Path{requester}, j, other); err != nil {
		t.Fatal(err)
	}
	if r := f.reply(t, id, 20*time.Second); !r.OK || r.Output != "from origin\n" || r.Tree != meshtest.Git(t, other, "rev-parse", pushed+"^{tree}") {
		t.Fatalf("reply %+v", r)
	}
	// A commit no remote has is still refused (sent as is: Queue would
	// refuse it first).
	bad := mesh.Job{ID: nextID(), Kind: "verify", Repo: "app", Commit: strings.Repeat("ab", 20), Args: []string{}, Go: true}
	meshtest.QueueJob(t, f.c, "verify", requester, bad)
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
	tree := meshtest.Git(t, f.repo, "rev-parse", "HEAD^{tree}")
	commit := meshtest.Git(t, f.repo, "rev-parse", "HEAD")
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
	if !r.OK || r.Output != "fresh-run\n" || r.By != "routine.check.testhost-cmd-1" || r.ResultKey != key {
		t.Fatalf("rerun reply %+v", r)
	}
	rec, ok, err := mesh.GetResult(f.ctx, f.c, key)
	if err != nil || !ok || !rec.OK || rec.By != "routine.check.testhost-cmd-1" || rec.Output != "fresh-run\n" || rec.Commit != commit {
		t.Fatalf("record %+v %v %v", rec, ok, err)
	}
}

func TestKindReadFromJobCommit(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/verify.toml": "timeout = \"30s\"\ncommand = \"echo c1\"\n"})
	c1 := meshtest.Git(t, f.repo, "rev-parse", "HEAD")
	c2 := meshtest.CommitFiles(t, f.repo, map[string]string{".bmesh/routines/verify.toml": "timeout = \"30s\"\ncommand = \"echo c2\"\n"})
	f.start(t)
	if r := f.reply(t, f.queue(t, mesh.Job{Kind: "verify", Commit: c1, Go: true}), 20*time.Second); !r.OK || r.Output != "c1\n" {
		t.Fatalf("job at c1: %+v", r)
	}
	if r := f.reply(t, f.queue(t, mesh.Job{Kind: "verify", Commit: c2, Go: true}), 20*time.Second); !r.OK || r.Output != "c2\n" {
		t.Fatalf("job at c2: %+v", r)
	}
	// Every run that names a commit records, deterministic or not (R12).
	tree1 := meshtest.Git(t, f.repo, "rev-parse", c1+"^{tree}")
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
	// A repo this box's registry doesn't name is deferred, never answered:
	// serve reads repos.toml only at start, and another box may have it.
	f.noReply(t, f.queue(t, mesh.Job{Kind: "review", Repo: "elsewhere", Go: true}), time.Second)
	tree := meshtest.Git(t, f.repo, "rev-parse", "HEAD^{tree}")
	id := f.queue(t, mesh.Job{Kind: "review", Model: "m-b", Go: true})
	r = f.reply(t, id, 20*time.Second)
	key := mesh.ResultKey("review", tree, nil, "m-b")
	if !r.OK || r.Output != "VERDICT: ok" || r.By != "routine.review.testhost-m-b-1" || r.ResultKey != key {
		t.Fatalf("m-b reply %+v", r)
	}
	if rec, ok, err := mesh.GetResult(f.ctx, f.c, key); err != nil || !ok || rec.Model != "m-b" || rec.By != "routine.review.testhost-m-b-1" {
		t.Fatalf("record %+v %v %v", rec, ok, err)
	}
	// A model instance's finished-job line carries its context tokens.
	hist, err := mesh.History(f.ctx, f.c, "bm.routine.review.testhost-m-b-1.progress", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, s := range hist {
		var ev mesh.Event
		_ = json.Unmarshal(s.Data, &ev)
		texts = append(texts, ev.Text)
	}
	if len(texts) == 0 || !strings.HasPrefix(texts[len(texts)-1], "job "+id+" ok ") ||
		!strings.HasSuffix(texts[len(texts)-1], " ctx 1000") {
		t.Fatalf("m-b progress %q", texts)
	}
	n, ok, err := mesh.GetNode(f.ctx, f.c, "routine.review.testhost-m-a-1")
	if err != nil || !ok || n.Model != "m-a" || n.Role != "routine" {
		t.Fatalf("m-a node %+v %v %v", n, ok, err)
	}
}

// A model's result records the patch-id of the diff its job text's base
// names; a job with no base records none.
func TestModelResultRecordsReviewedPatch(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/review.toml": `
models = ["m-a"]
prompt = "P"
timeout = "30s"
batch_idle = "1s"
`})
	base := meshtest.Git(t, f.repo, "rev-parse", "HEAD")
	head := meshtest.CommitFiles(t, f.repo, map[string]string{"a.txt": "a\n"})
	tree := meshtest.Git(t, f.repo, "rev-parse", "HEAD^{tree}")
	want, err := checkout.PatchID(f.ctx, f.repo, base, head)
	if err != nil || want == "" {
		t.Fatalf("patch %q %v", want, err)
	}
	t.Setenv("FAKE_RESULT", "VERDICT: ok")
	f.start(t)
	key := mesh.ResultKey("review", tree, nil, "m-a")
	if r := f.reply(t, f.queue(t, mesh.Job{Kind: "review", Go: true, Text: "base: " + base}), 20*time.Second); !r.OK {
		t.Fatalf("reply %+v", r)
	}
	if rec, ok, err := mesh.GetResult(f.ctx, f.c, key); err != nil || !ok || rec.PatchID != want {
		t.Fatalf("record %+v %v %v, want patch %s", rec, ok, err, want)
	}
	if r := f.reply(t, f.queue(t, mesh.Job{Kind: "review", Go: true, Args: []string{"x"}}), 20*time.Second); !r.OK {
		t.Fatalf("reply %+v", r)
	}
	if rec, ok, err := mesh.GetResult(f.ctx, f.c, mesh.ResultKey("review", tree, []string{"x"}, "m-a")); err != nil || !ok || rec.PatchID != "" {
		t.Fatalf("no base: %+v %v %v", rec, ok, err)
	}
}

// A model job whose claude never answered (an error result, output that
// isn't a result, a timeout) is answered and records nothing; a verdict is
// still recorded.
func TestModelRunThatNeverAnsweredRecordsNothing(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/review.toml": `
models = ["m-a"]
prompt = "P"
timeout = "1s"
batch_idle = "1s"
`})
	tree := meshtest.Git(t, f.repo, "rev-parse", "HEAD^{tree}")
	key := mesh.ResultKey("review", tree, nil, "m-a")
	f.start(t)
	for _, c := range []struct {
		name, why string
		env       map[string]string
	}{
		{"no sign-in", "Failed to authenticate: OAuth session expired",
			map[string]string{"FAKE_IS_ERROR": "1", "FAKE_EXIT": "1", "FAKE_RESULT": "Failed to authenticate: OAuth session expired"}},
		{"not json", "[bad claude output]", map[string]string{"FAKE_NOJSON": "1"}},
		{"timeout", "[timeout after 1s]", map[string]string{"FAKE_SLEEP": "5", "FAKE_RESULT": "VERDICT: ok"}},
	} {
		for _, v := range []string{"FAKE_IS_ERROR", "FAKE_EXIT", "FAKE_RESULT", "FAKE_NOJSON", "FAKE_SLEEP"} {
			t.Setenv(v, c.env[v])
		}
		r := f.reply(t, f.queue(t, mesh.Job{Kind: "review", Go: true}), 20*time.Second)
		if want := "model run failed: " + c.why + "; nothing recorded"; r.OK || r.ResultKey != "" || r.Output != want {
			t.Fatalf("%s: reply %+v, want %q", c.name, r, want)
		}
		if rec, ok, err := mesh.GetResult(f.ctx, f.c, key); err != nil || ok {
			t.Fatalf("%s: recorded %+v %v", c.name, rec, err)
		}
	}
	for _, v := range []string{"FAKE_IS_ERROR", "FAKE_EXIT", "FAKE_NOJSON", "FAKE_SLEEP"} {
		t.Setenv(v, "")
	}
	t.Setenv("FAKE_RESULT", "findings\nVERDICT: fail")
	if r := f.reply(t, f.queue(t, mesh.Job{Kind: "review", Go: true}), 20*time.Second); r.OK || r.ResultKey != key {
		t.Fatalf("fail verdict: reply %+v", r)
	}
	if rec, ok, err := mesh.GetResult(f.ctx, f.c, key); err != nil || !ok || rec.OK {
		t.Fatalf("fail verdict: record %+v %v %v", rec, ok, err)
	}
}

// A job fetched while the loop stops is returned to the inbox, not refused.
func TestJobFetchedWhileStoppingStaysQueued(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/verify.toml": "timeout = \"30s\"\ncommand = \"echo ran\"\n"})
	fetchWait = 2 * time.Second // the dispatcher's fetch outlives the stop
	stop := f.start(t)
	f.waitStarted(t, "routine.verify.testhost-cmd-1", 1)
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

// A pooled instance stopped because its kind left main stays done until
// [status] keep_done has passed, then the rescan forgets it; a fresh run's
// node left from before is forgotten the same way, a running one never.
func TestRescanForgetsStoppedInstancesAfterKeepDone(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/verify.toml": "timeout = \"30s\"\ncommand = \"echo ran\"\n"})
	kindRescan = 300 * time.Millisecond
	var offset atomic.Int64 // how far the daemon's clock runs ahead
	f.d.Now = func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }
	f.d.Cfg.Status.KeepDone = time.Hour
	stale := "routine.old.fdeadbeef"
	if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: stale, Role: "routine", Host: testHost}); err != nil {
		t.Fatal(err)
	}
	f.setState(t, stale, "working")
	f.setState(t, stale, "done")
	elsewhere := "routine.old.fcafef00d" // another host's: its own daemon sweeps it
	if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: elsewhere, Role: "routine", Host: "otherhost"}); err != nil {
		t.Fatal(err)
	}
	f.setState(t, elsewhere, "working")
	f.setState(t, elsewhere, "done")

	f.start(t)
	f.waitStarted(t, "routine.verify.testhost-cmd-1", 1)
	meshtest.Git(t, f.repo, "rm", "-q", ".bmesh/routines/verify.toml")
	meshtest.Git(t, f.repo, "commit", "-q", "-m", "drop verify")
	waitFor(t, 10*time.Second, "cmd-1 done", func() bool { return f.state(t, "routine.verify.testhost-cmd-1") == "done" })

	present := func(path string) bool {
		_, ok, err := mesh.GetNode(f.ctx, f.c, path)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	f.poll(t, 2*time.Second) // several rescans, none past keep_done
	for _, p := range []string{"routine.verify.testhost-cmd-1", stale, elsewhere} {
		if !present(p) {
			t.Fatalf("%s forgotten before keep_done", p)
		}
	}

	offset.Store(int64(time.Hour + time.Minute))
	for _, p := range []string{"routine.verify.testhost-cmd-1", stale} {
		waitFor(t, 10*time.Second, p+" forgotten", func() bool { return !present(p) })
		if s, rev, err := mesh.ReadState(f.ctx, f.c, p); err != nil || rev != 0 {
			t.Fatalf("%s state %+v rev %d %v, want none", p, s, rev, err)
		}
	}
	if !present(elsewhere) {
		t.Fatalf("%s on another host forgotten", elsewhere)
	}
	if f.events(t, "routine.verify.testhost-cmd-1", "done") != 1 {
		t.Fatal("cmd-1's done event is gone")
	}
}

func TestRescanSkipsRepoMissingHereLoggingOnce(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/verify.toml": "timeout = \"30s\"\ncommand = \"echo ran\"\n"})
	kindRescan = 200 * time.Millisecond
	gone := filepath.Join(t.TempDir(), "elsewhere")
	f.d.Cfg.Repos["gone"] = config.Repo{Path: gone}
	var log meshtest.SyncBuffer
	f.d.Log = slog.New(slog.NewTextHandler(&log, nil))

	f.start(t)
	f.waitStarted(t, "routine.verify.testhost-cmd-1", 1)
	f.poll(t, time.Second) // several rescans
	out := log.String()
	if n := strings.Count(out, "routines: repo has no checkout on this box; skipped"); n != 1 || !strings.Contains(out, "repo=gone") {
		t.Fatalf("skip logged %d times:\n%s", n, out)
	}
	if strings.Contains(out, "read kinds") || strings.Contains(out, "level=WARN") {
		t.Fatalf("a missing repo warned:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(f.d.Checkouts, "elsewhere")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a checkout was made for the missing repo: %v", err)
	}
	// The repo that is here still runs its kind.
	id := f.queue(t, mesh.Job{Kind: "verify", Commit: meshtest.Git(t, f.repo, "rev-parse", "HEAD"), Go: true})
	if r := f.reply(t, id, 20*time.Second); !r.OK {
		t.Fatalf("reply %+v", r)
	}
}

// startAs runs a second routine loop on the fixture's server as host, its
// kinds matched against goos, with checkouts of its own; each opt then
// changes its Deps.
func (f *rfix) startAs(t *testing.T, host, goos string, opts ...func(*Deps)) (stop func()) {
	t.Helper()
	d := f.d
	d.Host, d.GOOS, d.Checkouts = host, goos, filepath.Join(t.TempDir(), "checkouts")
	for _, opt := range opts {
		opt(&d)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan struct{})
	go func() { routines(ctx, d); close(done) }()
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

func TestOSKindRunsOnlyOnItsOS(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "mark")
	f := routineSetup(t, map[string]string{
		".bmesh/routines/mac-build.toml": "os = \"darwin\"\ntimeout = \"30s\"\ncommand = \"echo $BMESH_JOB >> " + mark + "; echo built\"\n",
		".bmesh/routines/verify.toml":    "mode = \"fresh\"\ntimeout = \"30s\"\ncommand = \"echo ran >> " + mark + "; echo verified\"\n",
	})
	stopMac := f.startAs(t, "mac-box", "darwin")
	f.startAs(t, "linux-box", "linux")
	f.waitStarted(t, "routine.mac-build.mac-box-cmd-1", 1)
	for i := 0; i < 3; i++ {
		r := f.reply(t, f.queue(t, mesh.Job{Kind: "mac-build", Go: true}), 20*time.Second)
		if !r.OK || r.Output != "built\n" || r.By != "routine.mac-build.mac-box-cmd-1" {
			t.Fatalf("mac-build reply %+v", r)
		}
	}
	n, ok, err := mesh.GetNode(f.ctx, f.c, "routine.mac-build.mac-box-cmd-1")
	if err != nil || !ok || n.Host != "mac-box" {
		t.Fatalf("instance %+v %v %v", n, ok, err)
	}
	nodes, err := mesh.ListNodes(f.ctx, f.c)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if strings.HasPrefix(n.Path, "routine.mac-build.") && n.Host != "mac-box" {
			t.Fatalf("mac-build instance on %s: %+v", n.Host, n)
		}
	}

	// A branch moves verify, which main runs anywhere, to darwin: the linux
	// box answers its job without running or recording it.
	stopMac()
	meshtest.Git(t, f.repo, "checkout", "-q", "-b", "side")
	side := meshtest.CommitFiles(t, f.repo, map[string]string{
		".bmesh/routines/verify.toml": "mode = \"fresh\"\nos = \"darwin\"\ntimeout = \"30s\"\ncommand = \"echo ran >> " + mark + "; echo verified\"\n",
	})
	meshtest.Git(t, f.repo, "checkout", "-q", "main")
	before := len(lines(t, mark))
	r := f.reply(t, f.queue(t, mesh.Job{Kind: "verify", Commit: side, Go: true}), 20*time.Second)
	if want := "kind verify at " + side[:8] + " runs on darwin; this box is linux"; r.OK || r.Output != want {
		t.Fatalf("wrong-box reply %+v, want %q", r, want)
	}
	if got := len(lines(t, mark)); got != before {
		t.Fatalf("ran on the wrong box: %d lines, was %d", got, before)
	}
	tree := meshtest.Git(t, f.repo, "rev-parse", side+"^{tree}")
	if _, ok, err := mesh.GetResult(f.ctx, f.c, mesh.ResultKey("verify", tree, nil, "")); err != nil || ok {
		t.Fatalf("recorded on the wrong box: %v %v", ok, err)
	}
	if n, err := mesh.Depth(f.ctx, f.c, "verify"); err != nil || n != 0 {
		t.Fatalf("verify depth %d %v: the answer is acked", n, err)
	}
}

func TestRescanLogsAKindReadFailureOncePerCommit(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/verify.toml": "timeout = \"30s\"\ncommand = \"echo ran\"\n"})
	kindRescan = 200 * time.Millisecond
	master := t.TempDir()
	meshtest.Git(t, master, "init", "-q", "-b", "master")
	meshtest.CommitFiles(t, master, map[string]string{"README": "x\n"})
	f.d.Cfg.Repos["old"] = config.Repo{Path: master}
	var log meshtest.SyncBuffer
	f.d.Log = slog.New(slog.NewTextHandler(&log, nil))

	f.start(t)
	f.waitStarted(t, "routine.verify.testhost-cmd-1", 1)
	f.poll(t, time.Second) // several rescans
	out := log.String()
	if n := strings.Count(out, "routines: read kinds"); n != 1 || !strings.Contains(out, "repo=old") {
		t.Fatalf("read kinds logged %d times:\n%s", n, out)
	}
}

// A command job brings the checkout's installs current after the move:
// again when an input changed, on every job when [setup] names no inputs;
// a model job never runs it, and a failing setup answers ok=false with
// nothing recorded.
func TestCommandJobRunsSetupAfterTheMove(t *testing.T) {
	slog := filepath.Join(t.TempDir(), "setup.log")
	install := `mkdir -p node_modules && touch node_modules/dep && echo setup >> ` + slog
	f := routineSetup(t, map[string]string{
		".gitignore":         "node_modules/\n",
		"web/lock.json":      "1\n",
		".bmesh/config.toml": "[setup]\ncommand = \"" + install + "\"\ninputs = [\"web/lock.json\"]\n",
		".bmesh/routines/check.toml": `
mode = "pooled"
timeout = "30s"
command = "test -f node_modules/dep && echo deps"
`,
		".bmesh/routines/review.toml": `
models = ["m-a"]
prompt = "P"
timeout = "30s"
batch_idle = "1s"
`})
	t.Setenv("FAKE_RESULT", "VERDICT: ok")
	setups := func() int { return len(lines(t, slog)) }
	f.start(t)
	job := func(kind, commit string, want int) mesh.Reply {
		t.Helper()
		r := f.reply(t, f.queue(t, mesh.Job{Kind: kind, Commit: commit, Go: true}), 20*time.Second)
		if !r.OK || setups() != want {
			t.Fatalf("%s at %.8s: %+v setups %d, want %d", kind, commit, r, setups(), want)
		}
		return r
	}
	c1 := meshtest.Git(t, f.repo, "rev-parse", "HEAD")
	if r := job("check", c1, 1); r.Output != "deps\n" {
		t.Fatalf("check %+v", r)
	}
	job("check", meshtest.CommitFiles(t, f.repo, map[string]string{"a.txt": "a\n"}), 1)
	c3 := meshtest.CommitFiles(t, f.repo, map[string]string{"web/lock.json": "2\n"})
	job("check", c3, 2)
	job("review", c3, 2)
	c4 := meshtest.CommitFiles(t, f.repo, map[string]string{".bmesh/config.toml": "[setup]\ncommand = \"" + install + "\"\n"})
	job("check", c4, 3)
	job("check", c4, 4)

	c5 := meshtest.CommitFiles(t, f.repo, map[string]string{".bmesh/config.toml": "[setup]\ncommand = \"echo down; exit 1\"\n"})
	r := f.reply(t, f.queue(t, mesh.Job{Kind: "check", Commit: c5, Go: true}), 20*time.Second)
	if r.OK || r.ResultKey != "" || r.Output != "setup failed: exit status 1; nothing recorded\ndown" {
		t.Fatalf("failing setup: %+v", r)
	}
	tree5 := meshtest.Git(t, f.repo, "rev-parse", c5+"^{tree}")
	if _, ok, err := mesh.GetResult(f.ctx, f.c, mesh.ResultKey("check", tree5, nil, "")); err != nil || ok {
		t.Fatalf("a failed setup recorded: %v %v", ok, err)
	}
}
