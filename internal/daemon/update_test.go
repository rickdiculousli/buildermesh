package daemon

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
)

// ufix is a routine fixture whose repo "app" is also the [update] repo: the
// binary is a file holding "old" at revision (app's main when set up), and
// Restart is recorded instead of ending anything.
type ufix struct {
	*rfix
	exe       string
	log       *meshtest.SyncBuffer
	goLog     string
	restarts  chan struct{}
	onRestart func()
}

func updateSetup(t *testing.T, files map[string]string) *ufix {
	t.Helper()
	files[".gitignore"] = "x\n" // a commit even when no kind is defined
	f := routineSetup(t, files)
	kindRescan = 300 * time.Millisecond
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-go.sh"))
	if err != nil {
		t.Fatal(err)
	}
	u := &ufix{rfix: f, log: &meshtest.SyncBuffer{}, goLog: filepath.Join(t.TempDir(), "go.log"), restarts: make(chan struct{}, 4)}
	t.Setenv("BMESH_GO_COMMAND", "sh "+fake)
	t.Setenv("FAKE_GO_LOG", u.goLog)
	t.Setenv("FAKE_GO_EXIT", "")
	t.Setenv("FAKE_GO_REVISION", "")
	t.Setenv("INVOCATION_ID", "")
	u.exe = filepath.Join(t.TempDir(), "bin", "bmesh")
	if err := os.MkdirAll(filepath.Dir(u.exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(u.exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.d.Cfg.Update.Repo = "app"
	f.d.Revision = meshtest.Git(t, f.repo, "rev-parse", "HEAD")
	f.d.Exe = u.exe
	f.d.Log = slog.New(slog.NewTextHandler(u.log, nil))
	f.d.Restart = func() {
		if u.onRestart != nil {
			u.onRestart()
		}
		u.restarts <- struct{}{}
	}
	return u
}

// land commits a change to app's main and returns it.
func (u *ufix) land(t *testing.T, name string) string {
	t.Helper()
	return meshtest.CommitFiles(t, u.repo, map[string]string{name: name})
}

func (u *ufix) restarted(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-u.restarts:
	case <-time.After(d):
		t.Fatalf("no restart within %s; log:\n%s", d, u.log.String())
	}
}

func (u *ufix) noRestart(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-u.restarts:
		t.Fatalf("restarted; log:\n%s", u.log.String())
	case <-time.After(d):
	}
}

func TestUpdateSwapsBinaryAndRestarts(t *testing.T) {
	u := updateSetup(t, map[string]string{})
	old := u.d.Revision
	u.start(t)
	u.noRestart(t, time.Second) // main is this revision
	next := u.land(t, "a.txt")
	u.restarted(t, 20*time.Second)

	out, err := exec.Command(u.exe).Output()
	if err != nil || strings.TrimSpace(string(out)) != "fake bmesh "+next {
		t.Fatalf("binary: %q %v", out, err)
	}
	if b, _ := os.ReadFile(selfupdate.Prev(u.exe)); string(b) != "old" {
		t.Fatalf("bmesh.prev %q", b)
	}
	text := old[:8] + " -> " + next[:8]
	if evs := u.daemonEvents(t, "update"); len(evs) != 1 || evs[0].Text != text || evs[0].Commit != next {
		t.Fatalf("update events %+v", evs)
	}
	if !strings.Contains(u.log.String(), "update: "+text) {
		t.Fatalf("log:\n%s", u.log.String())
	}
	// Drained for good: later scans build nothing more.
	u.land(t, "b.txt")
	u.noRestart(t, 1500*time.Millisecond)
	if n := len(lines(t, u.goLog)) / 2; n != 1 {
		t.Fatalf("%d builds", n)
	}
}

func TestUpdateFailedBuildLoggedOnce(t *testing.T) {
	u := updateSetup(t, map[string]string{})
	t.Setenv("FAKE_GO_EXIT", "1")
	u.start(t)
	next := u.land(t, "a.txt")
	waitFor(t, 20*time.Second, "a failed build", func() bool { return strings.Contains(u.log.String(), "build failed") })
	u.noRestart(t, 1500*time.Millisecond) // several scans
	if n := strings.Count(u.log.String(), "update: build failed"); n != 1 {
		t.Fatalf("logged %d times:\n%s", n, u.log.String())
	}
	if !strings.Contains(u.log.String(), "main="+next[:8]) || !strings.Contains(u.log.String(), "fake build failed") {
		t.Fatalf("log:\n%s", u.log.String())
	}
	if b, _ := os.ReadFile(u.exe); string(b) != "old" {
		t.Fatalf("binary %q", b)
	}
	if evs := u.daemonEvents(t, "update"); len(evs) != 0 {
		t.Fatalf("update events %+v", evs)
	}
	// The next commit is tried again.
	t.Setenv("FAKE_GO_EXIT", "")
	u.land(t, "b.txt")
	u.restarted(t, 20*time.Second)
}

func TestUpdateRefusedBuildKeepsBinary(t *testing.T) {
	u := updateSetup(t, map[string]string{})
	t.Setenv("FAKE_GO_REVISION", "eeeeeeee")
	u.start(t)
	next := u.land(t, "a.txt")
	want := "update: built binary reports eeeeeeee, not " + next[:8] + "; keeping this binary"
	waitFor(t, 20*time.Second, "a refused build", func() bool { return strings.Contains(u.log.String(), want) })
	u.noRestart(t, 1500*time.Millisecond) // several scans
	if n := strings.Count(u.log.String(), "built binary reports"); n != 1 {
		t.Fatalf("logged %d times:\n%s", n, u.log.String())
	}
	if b, _ := os.ReadFile(u.exe); string(b) != "old" {
		t.Fatalf("binary %q", b)
	}
	if _, err := os.Stat(selfupdate.Prev(u.exe)); !os.IsNotExist(err) {
		t.Fatalf("bmesh.prev written: %v", err)
	}
	if evs := u.daemonEvents(t, "update"); len(evs) != 0 {
		t.Fatalf("update events %+v", evs)
	}
	// The next commit, built right, goes in.
	t.Setenv("FAKE_GO_REVISION", "")
	u.land(t, "b.txt")
	u.restarted(t, 20*time.Second)
}

func TestUpdateLeavesRollbackAlone(t *testing.T) {
	u := updateSetup(t, map[string]string{})
	// This binary was built from a commit main does not hold.
	meshtest.Git(t, u.repo, "checkout", "-q", "-b", "side")
	side := u.land(t, "side.txt")
	meshtest.Git(t, u.repo, "checkout", "-q", "main")
	u.d.Revision = side
	u.start(t)
	waitFor(t, 20*time.Second, "the rollback logged", func() bool {
		return strings.Contains(u.log.String(), "does not contain this revision")
	})
	u.noRestart(t, 1500*time.Millisecond)
	if n := strings.Count(u.log.String(), "does not contain"); n != 1 {
		t.Fatalf("logged %d times", n)
	}
	if got := lines(t, u.goLog); len(got) != 0 {
		t.Fatalf("built: %q", got)
	}
}

// An update kills a running job and hands it back before the restart; the
// restarted daemon runs it at once, not after AckWait.
func TestUpdateHandsRunningJobBack(t *testing.T) {
	runs := filepath.Join(t.TempDir(), "runs")
	alive := filepath.Join(t.TempDir(), "alive")
	u := updateSetup(t, map[string]string{".bmesh/routines/slow.toml": `
mode = "pooled"
timeout = "1m"
batch_idle = "1s"
command = "echo $BMESH_JOB >> ` + runs + `; if [ $(wc -l < ` + runs + `) -ge 2 ]; then echo slow $BMESH_JOB; else sleep 30; echo late >> ` + alive + `; fi"
`})
	ackWaitExtra = time.Minute // AckWait 2m
	inbox := mesh.InboxSubject(mesh.Path{requester})
	var atRestart []string // reply texts in the requester's inbox when Restart is called
	var jobsAtRestart []mesh.QueuedJob
	u.onRestart = func() {
		hist, err := mesh.History(u.ctx, u.c, inbox, time.Time{})
		if err != nil {
			t.Error(err)
		}
		for _, s := range hist {
			var m mesh.Message
			if json.Unmarshal(s.Data, &m) == nil && m.Reply != nil {
				atRestart = append(atRestart, m.Reply.Job)
			}
		}
		if jobsAtRestart, err = mesh.KindJobs(u.ctx, u.c, "slow"); err != nil {
			t.Error(err)
		}
	}
	u.start(t)
	u.waitStarted(t, "routine.slow.testhost-cmd-1", 1)

	id1 := u.queue(t, mesh.Job{Kind: "slow", Go: true})
	waitFor(t, 10*time.Second, "the job running", func() bool { return len(lines(t, runs)) == 1 })
	u.land(t, "a.txt")
	u.restarted(t, 15*time.Second) // well before the run's 30s

	if len(atRestart) != 0 {
		t.Fatalf("replies at restart %v, want none", atRestart)
	}
	if len(jobsAtRestart) != 1 || jobsAtRestart[0].ID != id1 || jobsAtRestart[0].State != "waiting" {
		t.Fatalf("jobs at restart %+v, want %s waiting", jobsAtRestart, id1)
	}
	// The restarted daemon (no update to make) runs it at once.
	u.d.Cfg.Update.Repo = ""
	u.start(t)
	if r := u.reply(t, id1, 20*time.Second); !r.OK || r.Output != "slow "+id1+"\n" {
		t.Fatalf("reply %+v", r)
	}
	if got := lines(t, alive); len(got) != 0 {
		t.Fatalf("the killed run went on: %q", got)
	}
}

func TestAnnounceStartedAndFailedUpdate(t *testing.T) {
	f := setup(t)
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-ntfy.sh"))
	if err != nil {
		t.Fatal(err)
	}
	ntfy := filepath.Join(t.TempDir(), "ntfy.log")
	t.Setenv("FAKE_NTFY_LOG", ntfy)
	f.d.Cfg.Notify.Command = "sh " + fake
	old, next := strings.Repeat("a", 40), strings.Repeat("b", 40)
	read := func() string { b, _ := os.ReadFile(ntfy); return string(b) }

	f.d.Revision = old
	announce(f.ctx, f.d)
	if evs := f.daemonEvents(t, "started"); len(evs) != 1 || evs[0].Text != old {
		t.Fatalf("started %+v", evs)
	}
	// An update to next, then the binary restored by hand starts as old.
	publishUpdate := func() {
		if err := mesh.PublishEvent(f.ctx, f.c, mesh.Event{From: "daemon." + testHost, Event: "update",
			Text: "aaaaaaaa -> bbbbbbbb", Commit: next}); err != nil {
			t.Fatal(err)
		}
	}
	publishUpdate()
	announce(f.ctx, f.d)
	if got := read(); !strings.HasPrefix(got, "bmesh update failed on "+testHost+"\nupdate aaaaaaaa -> bbbbbbbb did not start; this is aaaaaaaa\n") ||
		!strings.Contains(got, "NTFY_PRIORITY=high") {
		t.Fatalf("push %q", got)
	}
	// A start after that start pushes nothing more.
	announce(f.ctx, f.d)
	if n := strings.Count(read(), "---\n"); n != 1 {
		t.Fatalf("%d pushes", n)
	}
	// The updated binary's own start is no failure.
	publishUpdate()
	f.d.Revision = next
	announce(f.ctx, f.d)
	if n := strings.Count(read(), "---\n"); n != 1 {
		t.Fatalf("%d pushes", n)
	}
	if evs := f.daemonEvents(t, "started"); len(evs) != 4 || evs[3].Text != next {
		t.Fatalf("started %+v", evs)
	}
	f.d.Revision = ""
	announce(f.ctx, f.d)
	if evs := f.daemonEvents(t, "started"); evs[len(evs)-1].Text != "unknown" {
		t.Fatalf("started %+v", evs[len(evs)-1])
	}
}

func TestDigestTitleCarriesRevision(t *testing.T) {
	f := setup(t)
	f.d.Cfg.Notify.Command = "cat > /dev/null"
	f.d.Revision = "1a2b3c4d5e6f"
	f.lead(t, "a", "t1", "asking")
	f.now = f.now.Add(time.Minute)
	since := f.now.Add(-time.Hour)
	digestOnce(f.ctx, f.d, &since)
	if evs := f.daemonEvents(t, "digest"); len(evs) != 1 || evs[0].Text != "bmesh "+testHost+" 1a2b3c4d: 0 working, 1 asking, 0 silent" {
		t.Fatalf("digest %+v", evs)
	}
}
