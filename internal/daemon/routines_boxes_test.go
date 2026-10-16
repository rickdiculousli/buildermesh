package daemon

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/checkout"
	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// Two daemons share each kind's consumer: these run two on one server,
// each with a host, a registry and checkouts of its own.

// twoBoxes shortens the rescan, so a box whose first scan failed (both
// boxes add worktrees of one repo) retries within the test's waits.
func twoBoxes() { kindRescan = 300 * time.Millisecond }

// withRepos gives a daemon a registry of its own.
func withRepos(repos map[string]config.Repo) func(*Deps) {
	return func(d *Deps) {
		cfg := *d.Cfg
		cfg.Repos = repos
		d.Cfg = &cfg
	}
}

func withLog(log *meshtest.SyncBuffer) func(*Deps) {
	return func(d *Deps) { d.Log = slog.New(slog.NewTextHandler(log, nil)) }
}

func withCheckouts(dir string) func(*Deps) {
	return func(d *Deps) { d.Checkouts = dir }
}

// libRepo is a second repo defining files, so a box without app still has
// a feed for the kind.
func libRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	lib := filepath.Join(t.TempDir(), "lib")
	if err := os.Mkdir(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	meshtest.Git(t, lib, "init", "-q", "-b", "main")
	meshtest.CommitFiles(t, lib, files)
	return lib
}

func TestDeferDelayGrowsAndCaps(t *testing.T) {
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second,
		32 * time.Second, time.Minute, time.Minute}
	for i, w := range want {
		if got := deferDelay(uint64(i + 1)); got != w {
			t.Errorf("deferDelay(%d) = %s, want %s", i+1, got, w)
		}
	}
	if got := deferDelay(1000); got != time.Minute {
		t.Errorf("deferDelay(1000) = %s", got)
	}
}

// A job for a repo only box A has is run by A. Box B, which has the kind
// from another repo but no checkout of app, defers it: no reply, a NAK whose
// delay grows, one log line.
func TestJobForARepoOneBoxLacksIsDeferredThere(t *testing.T) {
	kindFile := map[string]string{".bmesh/routines/verify.toml": "timeout = \"30s\"\ncommand = \"echo ran\"\n"}
	f := routineSetup(t, kindFile)
	twoBoxes()
	deferBase, deferCap = 200*time.Millisecond, 800*time.Millisecond
	t.Cleanup(func() { deferBase, deferCap = 2*time.Second, time.Minute })
	lib := libRepo(t, kindFile)
	var logB meshtest.SyncBuffer
	f.startAs(t, "box-b", "linux", withLog(&logB), withRepos(map[string]config.Repo{
		"app": {Path: filepath.Join(t.TempDir(), "absent")},
		"lib": {Path: lib},
	}))
	f.waitStarted(t, "routine.verify.box-b-cmd-1", 1)

	id := f.queue(t, mesh.Job{Kind: "verify", Go: true})
	start := time.Now()
	f.noReply(t, id, 4*time.Second)
	cons, err := f.c.JS.Consumer(f.ctx, mesh.StreamMesh, mesh.InboxConsumer(mesh.RoutinePath("verify")))
	if err != nil {
		t.Fatal(err)
	}
	info, err := cons.Info(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 200, 400, 800, 800 ms..., less up to a quarter: about 6 deliveries
	// in 4s, where a fixed 200ms would make about 20.
	if n := info.Delivered.Consumer; n < 3 || n > 10 {
		t.Fatalf("%d deliveries in %s", n, time.Since(start))
	}
	if info.NumAckPending != 1 {
		t.Fatalf("ack pending %d, want the deferred job", info.NumAckPending)
	}
	line := "routines: deferred job " + id + ": app has no checkout here"
	if n := strings.Count(logB.String(), line); n != 1 {
		t.Fatalf("deferral logged %d times:\n%s", n, logB.String())
	}

	var logA meshtest.SyncBuffer
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("A:\n%s\nB:\n%s", logA.String(), logB.String())
		}
	})
	f.startAs(t, "box-a", "linux", withLog(&logA))
	r := f.reply(t, id, 20*time.Second)
	if !r.OK || r.By != "routine.verify.box-a-cmd-1" {
		t.Fatalf("reply %+v", r)
	}
	f.poll(t, 2*time.Second) // a second reply fails the test
}

// A kind on neither box's main of a repo both have is answered at once,
// naming the commit main was read at.
func TestKindNotOnMainAnsweredNamingTheCommit(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/verify.toml": "timeout = \"30s\"\ncommand = \"echo ran\"\n"})
	twoBoxes()
	lib := libRepo(t, map[string]string{".bmesh/routines/lint.toml": "timeout = \"30s\"\ncommand = \"echo linted\"\n"})
	repos := map[string]config.Repo{"app": {Path: f.repo}, "lib": {Path: lib}}
	f.startAs(t, "box-a", "linux", withRepos(repos))
	f.startAs(t, "box-b", "linux", withRepos(repos))
	f.waitStarted(t, "routine.lint.box-a-cmd-1", 1)
	f.waitStarted(t, "routine.lint.box-b-cmd-1", 1)
	main := meshtest.Git(t, f.repo, "rev-parse", "main")
	start := time.Now()
	r := f.reply(t, f.queue(t, mesh.Job{Kind: "lint", Go: true}), 10*time.Second)
	if want := "kind lint is not on app's main (main at " + main[:8] + ")"; r.OK || r.Output != want {
		t.Fatalf("reply %+v, want %q", r, want)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("answered after %s", time.Since(start))
	}
	f.poll(t, time.Second)
}

// With pool 1 on each box, two jobs run at once, one per box, and a third
// waits on the hub: neither box takes more than its one.
func TestEachBoxCapsItsOwnConcurrency(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	f := routineSetup(t, map[string]string{".bmesh/routines/slow.toml": `
pool = 1
timeout = "30s"
batch_idle = "1s"
command = "touch ` + dir + `/started-$BMESH_JOB; while [ ! -f ` + release + ` ]; do sleep 0.1; done; echo done"
`})
	twoBoxes()
	f.startAs(t, "box-a", "linux")
	f.startAs(t, "box-b", "linux")
	f.waitStarted(t, "routine.slow.box-a-cmd-1", 1)
	f.waitStarted(t, "routine.slow.box-b-cmd-1", 1)
	ids := []string{f.queue(t, mesh.Job{Kind: "slow", Go: true}), f.queue(t, mesh.Job{Kind: "slow", Go: true}),
		f.queue(t, mesh.Job{Kind: "slow", Go: true})}
	started := func() int {
		m, _ := filepath.Glob(filepath.Join(dir, "started-*"))
		return len(m)
	}
	waitFor(t, 15*time.Second, "two jobs running", func() bool { return started() == 2 })
	cons, err := f.c.JS.Consumer(f.ctx, mesh.StreamMesh, mesh.InboxConsumer(mesh.RoutinePath("slow")))
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(1500 * time.Millisecond); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if n := started(); n != 2 {
			t.Fatalf("%d jobs started, want 2", n)
		}
		jobs, err := mesh.KindJobs(f.ctx, f.c, "slow")
		if err != nil {
			t.Fatal(err)
		}
		per := map[string]int{} // a box's dispatcher holds as daemon.<host>, its instances as routine.slow.<host>-cmd-1
		for _, j := range jobs {
			if j.Holder != "" {
				host := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(j.Holder, "daemon."), "routine.slow."), "-cmd-1")
				per[host]++
			}
		}
		if per["box-a"] != 1 || per["box-b"] != 1 || len(per) != 2 {
			t.Fatalf("jobs per box %v, want one each: %+v", per, jobs)
		}
		info, err := cons.Info(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if info.NumAckPending != 2 || info.NumPending != 1 {
			t.Fatalf("consumer: %d out, %d waiting; want 2 and 1", info.NumAckPending, info.NumPending)
		}
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	hosts := map[string]bool{}
	for _, id := range ids {
		r := f.reply(t, id, 20*time.Second)
		if !r.OK {
			t.Fatalf("reply %+v", r)
		}
		hosts[strings.TrimSuffix(strings.TrimPrefix(r.By, "routine.slow."), "-cmd-1")] = true
	}
	if !hosts["box-a"] || !hosts["box-b"] {
		t.Fatalf("ran on %v, want both boxes", hosts)
	}
}

// A consumer an old daemon rewrote to MaxAckPending 1 reads -1 after the
// next rescan, with the AckWait it had. A job running past an AckWait
// another box shortened is not redelivered: it beats every ackWaitExtra/3.
func TestConsumerConvergesAndHeartbeatOutpacesAckWait(t *testing.T) {
	runs := filepath.Join(t.TempDir(), "runs")
	f := routineSetup(t, map[string]string{".bmesh/routines/slow.toml": `
pool = 2
timeout = "30s"
batch_idle = "1s"
command = "echo x >> ` + runs + `; sleep 3; echo done"
`})
	kindRescan = 300 * time.Millisecond
	f.start(t)
	f.waitStarted(t, "routine.slow.testhost-cmd-1", 1)
	consumer := func() (int, time.Duration) {
		cons, err := f.c.JS.Consumer(f.ctx, mesh.StreamMesh, mesh.InboxConsumer(mesh.RoutinePath("slow")))
		if err != nil {
			t.Fatal(err)
		}
		info, err := cons.Info(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		return info.Config.MaxAckPending, info.Config.AckWait
	}
	if n, wait := consumer(); n != -1 || wait != 31*time.Second {
		t.Fatalf("consumer %d %s, want -1 31s", n, wait)
	}
	if _, err := mesh.KindConsumer(f.ctx, f.c, "slow", 1, 31*time.Second); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "MaxAckPending -1", func() bool { n, _ := consumer(); return n == -1 })
	if _, wait := consumer(); wait != 31*time.Second {
		t.Fatalf("AckWait %s after converging", wait)
	}

	// Another box's kind is a second long: its AckWait is 1s + extra (1s).
	if _, err := mesh.KindConsumer(f.ctx, f.c, "slow", -1, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	r := f.reply(t, f.queue(t, mesh.Job{Kind: "slow", Go: true}), 20*time.Second)
	if !r.OK || r.Output != "done\n" {
		t.Fatalf("reply %+v", r)
	}
	f.poll(t, time.Second) // a second reply fails the test
	if n := len(lines(t, runs)); n != 1 {
		t.Fatalf("ran %d times, want once", n)
	}
	if _, wait := consumer(); wait != 2*time.Second {
		t.Fatalf("AckWait %s: this box wrote over it though its own didn't change", wait)
	}
}

// A go is one message: the box that fetches it isn't the one holding the
// batch, which still starts.
func TestGoFetchedByOneBoxStartsTheOthersBatch(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/verify.toml": `
pool = 1
timeout = "30s"
batch_idle = "30s"
command = "echo ran"
`})
	twoBoxes()
	f.startAs(t, "box-a", "linux")
	f.startAs(t, "box-b", "linux")
	f.waitStarted(t, "routine.verify.box-a-cmd-1", 1)
	f.waitStarted(t, "routine.verify.box-b-cmd-1", 1)
	id := f.queue(t, mesh.Job{Kind: "verify"})
	var holder string
	waitFor(t, 10*time.Second, "the job held", func() bool {
		jobs, err := mesh.KindJobs(f.ctx, f.c, "verify")
		if err == nil && len(jobs) == 1 && jobs[0].State == "held" && strings.HasPrefix(jobs[0].Holder, "routine.verify.") {
			holder = jobs[0].Holder
		}
		return holder != ""
	})
	// The holder is at its capacity of one, so it fetches nothing: the
	// other box takes the go.
	start := time.Now()
	if _, err := mesh.Send(f.ctx, f.c, mesh.Message{Kind: "message", Text: "go", From: requester, To: "routine.verify"}); err != nil {
		t.Fatal(err)
	}
	r := f.reply(t, id, 10*time.Second)
	if !r.OK || r.By != holder || time.Since(start) > 5*time.Second {
		t.Fatalf("reply %+v by %s after %s", r, holder, time.Since(start))
	}
}

// Each box's pooled instances are nodes of their own, named with the host,
// and each still runs in the checkout its host-less name had.
func TestPooledInstancesCarryTheHost(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/verify.toml": "timeout = \"30s\"\ncommand = \"pwd\"\n"})
	twoBoxes()
	roots := map[string]string{"box-a": filepath.Join(t.TempDir(), "a"), "box-b": filepath.Join(t.TempDir(), "b")}
	warm := map[string]string{}
	for host, root := range roots {
		dir, err := checkout.Ensure(f.repo, root, "routine.verify.cmd-1")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "warm"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		warm[host] = dir
		f.startAs(t, host, "linux", withCheckouts(root))
	}
	for host := range roots {
		path := "routine.verify." + host + "-cmd-1"
		f.waitStarted(t, path, 1)
		n, ok, err := mesh.GetNode(f.ctx, f.c, path)
		if err != nil || !ok || n.Host != host {
			t.Fatalf("%s: %+v %v %v", path, n, ok, err)
		}
		if _, err := os.Stat(filepath.Join(warm[host], "warm")); err != nil {
			t.Fatalf("%s's checkout was made anew: %v", host, err)
		}
	}
	nodes, err := mesh.ListNodes(f.ctx, f.c)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Path == "routine.verify.cmd-1" {
			t.Fatalf("a host-less instance: %+v", n)
		}
	}
	for range 4 {
		r := f.reply(t, f.queue(t, mesh.Job{Kind: "verify", Go: true}), 20*time.Second)
		host := strings.TrimSuffix(strings.TrimPrefix(r.By, "routine.verify."), "-cmd-1")
		if dir, ok := warm[host]; !r.OK || !ok || strings.TrimSpace(r.Output) != dir {
			t.Fatalf("reply %+v, want it run in %v", r, warm)
		}
	}
}

// A repo whose main gives a kind another OS has that kind's jobs deferred
// on this box, though another repo here runs the kind anywhere.
func TestKindOfAnotherOSIsDeferred(t *testing.T) {
	f := routineSetup(t, map[string]string{".bmesh/routines/build.toml": "os = \"darwin\"\ntimeout = \"30s\"\ncommand = \"echo mac\"\n"})
	lib := libRepo(t, map[string]string{".bmesh/routines/build.toml": "timeout = \"30s\"\ncommand = \"echo built\"\n"})
	var log meshtest.SyncBuffer
	f.startAs(t, "linux-box", "linux", withLog(&log), withRepos(map[string]config.Repo{"app": {Path: f.repo}, "lib": {Path: lib}}))
	f.waitStarted(t, "routine.build.linux-box-cmd-1", 1)
	id := f.queue(t, mesh.Job{Kind: "build", Go: true})
	f.noReply(t, id, 2*time.Second)
	if line := "routines: deferred job " + id + ": app runs build on darwin, not here"; strings.Count(log.String(), line) != 1 {
		t.Fatalf("want %q once:\n%s", line, log.String())
	}
	libJob := mesh.Job{ID: nextID(), Kind: "build", Repo: "lib", Go: true}
	if _, err := mesh.Queue(f.ctx, f.c, mesh.Path{requester}, libJob, lib); err != nil {
		t.Fatal(err)
	}
	r := f.reply(t, libJob.ID, 20*time.Second)
	if !r.OK || r.Output != "built\n" {
		t.Fatalf("lib reply %+v", r)
	}
}

// A go another box fetched starts this box's held batch though this box
// has since fetched a later job: the peek starts after the oldest held job,
// not after the last fetched.
func TestGoBehindALaterFetchStillStartsTheBatch(t *testing.T) {
	release := filepath.Join(t.TempDir(), "release")
	f := routineSetup(t, map[string]string{".bmesh/routines/verify.toml": `
pool = 2
timeout = "30s"
batch_idle = "30s"
command = "if [ \"$1\" = block ]; then while [ ! -f ` + release + ` ]; do sleep 0.1; done; fi; echo ran"
`})
	drainWait = time.Millisecond // a job fetched after the blockers is held, not run in their drain
	f.start(t)
	f.waitStarted(t, "routine.verify.testhost-cmd-1", 1)
	f.waitStarted(t, "routine.verify.testhost-cmd-2", 1)
	// Two running blockers fill this box's capacity: it fetches nothing.
	blockers := []string{f.queue(t, mesh.Job{Kind: "verify", Args: []string{"block"}, Go: true}),
		f.queue(t, mesh.Job{Kind: "verify", Args: []string{"block", "2"}, Go: true})}
	waitFor(t, 10*time.Second, "both blockers running", func() bool {
		jobs, err := mesh.KindJobs(f.ctx, f.c, "verify")
		return err == nil && len(jobs) == 2 && jobs[0].State == "running" && jobs[1].State == "running"
	})
	id1 := f.queue(t, mesh.Job{Kind: "verify"})
	if _, err := mesh.Send(f.ctx, f.c, mesh.Message{Kind: "message", Text: "go", From: requester, To: "routine.verify"}); err != nil {
		t.Fatal(err)
	}
	id2 := f.queue(t, mesh.Job{Kind: "verify"})
	// Another box fetches the first job and the go: it acks the go and
	// hands the job back.
	cons, err := f.c.JS.Consumer(f.ctx, mesh.StreamMesh, mesh.InboxConsumer(mesh.RoutinePath("verify")))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := cons.FetchNoWait(2)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for m := range batch.Messages() {
		var msg mesh.Message
		if err := json.Unmarshal(m.Data(), &msg); err != nil {
			t.Fatal(err)
		}
		if isGo(msg) {
			got = append(got, "go")
			m.Ack()
		} else {
			got = append(got, msg.Job.ID)
			m.Nak()
		}
	}
	if strings.Join(got, " ") != id1+" go" {
		t.Fatalf("fetched %q", got)
	}
	// The blockers end: this box fetches the first job again and the later
	// second, holds both, and finds the go that sits between them.
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range blockers {
		if r := f.reply(t, id, 10*time.Second); !r.OK {
			t.Fatalf("blocker %+v", r)
		}
	}
	start := time.Now()
	for _, id := range []string{id1, id2} {
		if r := f.reply(t, id, 10*time.Second); !r.OK || time.Since(start) > 8*time.Second {
			t.Fatalf("reply %+v after %s", r, time.Since(start))
		}
	}
}
