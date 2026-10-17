package daemon

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
	"github.com/rickdiculousli/buildermesh/internal/upgrade"
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
	u.land(t, "main.txt") // main moved on without it
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

	f.d.Revision, f.d.RevTime = old, "2026-10-15T09:00:00Z"
	announce(f.ctx, f.d)
	if evs := f.daemonEvents(t, "started"); len(evs) != 1 || evs[0].Text != old || evs[0].Body != "2026-10-15T09:00:00Z" {
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

// upstreamSetup is updateSetup without [update] repo: app stands in for the
// module example.com/app, whose main is checked every 300ms, and the notice
// goes to a state dir of the test's.
func upstreamSetup(t *testing.T, upstream bool) *ufix {
	t.Helper()
	u := updateSetup(t, map[string]string{})
	u.d.Cfg.Update = config.UpdateConfig{CheckEvery: 300 * time.Millisecond, Upstream: upstream}
	u.d.Module = "example.com/app"
	u.d.StateDir = t.TempDir()
	t.Setenv("BMESH_UPSTREAM_URL", u.repo)
	t.Setenv("BMESH_CACHE_DIR", t.TempDir())
	return u
}

func (u *ufix) notice() string { return selfupdate.ReadNotice(u.d.StateDir) }

// landSpec lands spec 001 on app's main as a lead would: its code, then
// the spec with its `001: done at` ledger commit. It returns the code
// commit, main's code head.
func (u *ufix) landSpec(t *testing.T) string {
	t.Helper()
	code := u.land(t, "first.txt")
	meshtest.CommitFiles(t, u.repo, map[string]string{"docs/proposals/001-first.md": "---\nid: 001\ntitle: The first\n---\n"})
	meshtest.Git(t, u.repo, "commit", "-q", "--allow-empty", "-m", "001: done at "+code[:8])
	return code
}

const behindOne = "bmesh: 1 commit behind main (landed since: 001 The first); run bmesh update, then update the buildermesh plugin"

func TestNoticeWrittenWhenBehindAndRemovedWhenCurrent(t *testing.T) {
	u := upstreamSetup(t, false)
	if err := selfupdate.WriteNotice(u.d.StateDir, "stale"); err != nil {
		t.Fatal(err)
	}
	u.start(t)
	waitFor(t, 20*time.Second, "the stale notice removed", func() bool { return u.notice() == "" })
	next := u.landSpec(t)
	waitFor(t, 20*time.Second, "the notice", func() bool { return u.notice() == behindOne })
	// A `bmesh update` swapped in a binary at main: the next check reads it
	// as current, though this daemon still runs the old one.
	if err := os.WriteFile(u.exe, []byte("#!/bin/sh\necho bmesh "+next[:8]+" 2026-10-16T00:00:00Z\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "the notice removed", func() bool { return u.notice() == "" })
	u.noRestart(t, 500*time.Millisecond)
	if l := lines(t, u.goLog); len(l) != 0 {
		t.Fatalf("installed without upstream: %v", l)
	}
}

func TestNoticeOffWithCheckEveryZero(t *testing.T) {
	u := upstreamSetup(t, false)
	u.d.Cfg.Update.CheckEvery = 0
	if err := selfupdate.WriteNotice(u.d.StateDir, "stale"); err != nil {
		t.Fatal(err)
	}
	u.land(t, "a.txt")
	u.start(t)
	waitFor(t, 20*time.Second, "the stale notice removed", func() bool { return u.notice() == "" })
	time.Sleep(time.Second) // several scans
	if u.notice() != "" {
		t.Fatalf("notice %q", u.notice())
	}
}

func TestUpstreamInstallsAndRestarts(t *testing.T) {
	u := upstreamSetup(t, true)
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
	if l := lines(t, u.goLog); len(l) != 2 || l[1] != "example.com/app@"+next {
		t.Fatalf("go log %v", l)
	}
	text := old[:8] + " -> " + next[:8]
	if evs := u.daemonEvents(t, "update"); len(evs) != 1 || evs[0].Text != text || evs[0].Commit != next {
		t.Fatalf("update events %+v", evs)
	}
	if u.notice() != "" {
		t.Fatalf("notice %q", u.notice())
	}
}

func TestUpstreamFailedInstallLoggedOnceAndRetried(t *testing.T) {
	u := upstreamSetup(t, true)
	t.Setenv("FAKE_GO_EXIT", "1")
	u.start(t)
	u.landSpec(t)
	waitFor(t, 20*time.Second, "the notice", func() bool { return u.notice() == behindOne })
	waitFor(t, 20*time.Second, "a second try", func() bool { return len(lines(t, u.goLog)) >= 4 })
	if n := strings.Count(u.log.String(), "update: install failed"); n != 1 {
		t.Fatalf("logged %d times:\n%s", n, u.log.String())
	}
	if b, _ := os.ReadFile(u.exe); string(b) != "old" {
		t.Fatalf("binary %q", b)
	}
	// The module proxy caught up: the same commit installs on the next check.
	t.Setenv("FAKE_GO_EXIT", "")
	u.restarted(t, 20*time.Second)
}

func TestUpstreamRefusedOnUnsupervisedWindows(t *testing.T) {
	u := upstreamSetup(t, true)
	u.d.GOOS = "windows"
	u.start(t)
	u.landSpec(t)
	waitFor(t, 20*time.Second, "the notice", func() bool { return u.notice() == behindOne })
	u.noRestart(t, time.Second)
	if n := strings.Count(u.log.String(), "[update] upstream needs bmesh serve --restart on Windows"); n != 1 {
		t.Fatalf("logged %d times:\n%s", n, u.log.String())
	}
	if l := lines(t, u.goLog); len(l) != 0 {
		t.Fatalf("installed on Windows: %v", l)
	}
}

func TestUpstreamInstallsOnSupervisedWindows(t *testing.T) {
	u := upstreamSetup(t, true)
	u.d.GOOS, u.d.Supervised = "windows", true
	u.start(t)
	u.landSpec(t)
	u.restarted(t, 20*time.Second)
	if strings.Contains(u.log.String(), "needs bmesh serve --restart") {
		t.Fatalf("refused:\n%s", u.log.String())
	}
}

func TestUpdateRepoRefusedOnUnsupervisedWindows(t *testing.T) {
	u := updateSetup(t, map[string]string{})
	u.d.GOOS = "windows"
	u.start(t)
	u.land(t, "a.txt")
	u.noRestart(t, 1500*time.Millisecond) // several scans
	if n := strings.Count(u.log.String(), "[update] repo needs bmesh serve --restart on Windows"); n != 1 {
		t.Fatalf("logged %d times:\n%s", n, u.log.String())
	}
	if l := lines(t, u.goLog); len(l) != 0 {
		t.Fatalf("built on Windows: %v", l)
	}
}

func TestUpdateRepoOnSupervisedWindows(t *testing.T) {
	u := updateSetup(t, map[string]string{})
	u.d.GOOS, u.d.Supervised = "windows", true
	u.start(t)
	u.land(t, "a.txt")
	u.restarted(t, 20*time.Second)
}

// tag tags commit in app as release name.
func (u *ufix) tag(t *testing.T, name, commit string) {
	t.Helper()
	meshtest.Git(t, u.repo, "tag", "-a", "-m", name, name, commit)
}

// ledger commits a spec file alone to app's main, as dispatch and done do.
func (u *ufix) ledger(t *testing.T, name string) string {
	t.Helper()
	return meshtest.CommitFiles(t, u.repo, map[string]string{"docs/proposals/" + name + ".md": name})
}

func (u *ufix) runs(t *testing.T, want string) {
	t.Helper()
	if out, err := exec.Command(u.exe).Output(); err != nil || strings.TrimSpace(string(out)) != "fake bmesh "+want {
		t.Fatalf("binary: %q %v, want %s", out, err, want[:8])
	}
}

func TestUpdateFollowsReleases(t *testing.T) {
	u := updateSetup(t, map[string]string{})
	u.d.Cfg.Update.Track = "release"
	u.tag(t, "v0.1.0", u.d.Revision)
	u.start(t)
	u.ledger(t, "001-a")
	u.land(t, "a.txt") // code, but no release
	u.ledger(t, "002-b")
	u.noRestart(t, 1500*time.Millisecond) // several scans
	if l := lines(t, u.goLog); len(l) != 0 {
		t.Fatalf("built: %v", l)
	}
	next := u.land(t, "b.txt")
	u.tag(t, "v0.2.0", next)
	u.ledger(t, "003-c") // the ledger after the release
	u.restarted(t, 20*time.Second)
	u.runs(t, next)
	if evs := u.daemonEvents(t, "update"); len(evs) != 1 || evs[0].Text != "v0.1.0 -> v0.2.0" || evs[0].Commit != next {
		t.Fatalf("update events %+v", evs)
	}
	if !strings.Contains(u.log.String(), "update: v0.1.0 -> v0.2.0") || strings.Contains(u.log.String(), "no release tag") {
		t.Fatalf("log:\n%s", u.log.String())
	}
	if l := strings.Join(lines(t, u.goLog), " "); l != "build "+next {
		t.Fatalf("builds %v", l)
	}
}

func TestUpdateLatestTakesCodeNotLedger(t *testing.T) {
	u := updateSetup(t, map[string]string{})
	u.d.Cfg.Update.Track = "latest"
	u.tag(t, "v0.1.0", u.d.Revision)
	u.start(t)
	u.ledger(t, "001-a")
	u.noRestart(t, 1500*time.Millisecond)
	next := u.land(t, "a.txt")
	u.ledger(t, "002-b")
	u.restarted(t, 20*time.Second)
	u.runs(t, next)
	if evs := u.daemonEvents(t, "update"); len(evs) != 1 || evs[0].Text != "v0.1.0 -> v0.1.0+2 ("+next[:8]+")" {
		t.Fatalf("update events %+v", evs)
	}
}

// A repo with no release tag follows its code head under release, and says
// so once.
func TestUpdateWithoutTagsFollowsCodeHead(t *testing.T) {
	u := updateSetup(t, map[string]string{})
	u.d.Cfg.Update.Track = "release"
	u.start(t)
	u.ledger(t, "001-a")
	u.noRestart(t, 1500*time.Millisecond)
	next := u.land(t, "a.txt")
	u.restarted(t, 20*time.Second)
	u.runs(t, next)
	if n := strings.Count(u.log.String(), "update: no release tag on main yet; following its code head"); n != 1 {
		t.Fatalf("said %d times:\n%s", n, u.log.String())
	}
}

// A binary past the newest release (built from main, or by update
// --latest) keeps running: it never goes back.
func TestUpdateNeverMovesBack(t *testing.T) {
	u := updateSetup(t, map[string]string{})
	u.d.Cfg.Update.Track = "release"
	u.tag(t, "v0.1.0", u.d.Revision)
	u.d.Revision = u.land(t, "a.txt")
	u.start(t)
	u.ledger(t, "001-a")
	u.noRestart(t, 1500*time.Millisecond)
	if l := lines(t, u.goLog); len(l) != 0 {
		t.Fatalf("built: %v", l)
	}
	if strings.Contains(u.log.String(), "left alone") {
		t.Fatalf("log:\n%s", u.log.String())
	}
}

// A `go install …@v0.1.0` binary names no commit: its tag is resolved on
// main, and it updates to the next release.
func TestUpdateTaggedInstallResolvesItsCommit(t *testing.T) {
	u := updateSetup(t, map[string]string{})
	u.d.Cfg.Update.Track = "release"
	u.tag(t, "v0.1.0", u.d.Revision)
	u.d.Revision, u.d.Tag = "", "v0.1.0"
	u.start(t)
	u.noRestart(t, time.Second) // it is v0.1.0
	next := u.land(t, "a.txt")
	u.tag(t, "v0.2.0", next)
	u.restarted(t, 20*time.Second)
	u.runs(t, next)
	if evs := u.daemonEvents(t, "update"); len(evs) != 1 || evs[0].Text != "v0.1.0 -> v0.2.0" {
		t.Fatalf("update events %+v", evs)
	}
}

// A tag main doesn't have is said once, never silently left.
func TestUpdateUnknownTagSaidOnce(t *testing.T) {
	u := updateSetup(t, map[string]string{})
	u.d.Revision, u.d.Tag = "", "v9.9.9"
	u.start(t)
	u.land(t, "a.txt")
	waitFor(t, 20*time.Second, "the unknown release logged", func() bool {
		return strings.Contains(u.log.String(), "update: this binary's release is not on main; left alone")
	})
	u.noRestart(t, 1500*time.Millisecond)
	if n := strings.Count(u.log.String(), "is not on main"); n != 1 || !strings.Contains(u.log.String(), "running=v9.9.9") {
		t.Fatalf("logged %d times:\n%s", n, u.log.String())
	}
}

// The upstream check installs the newest release by its tag.
func TestUpstreamInstallsTheRelease(t *testing.T) {
	u := upstreamSetup(t, true)
	u.tag(t, "v0.1.0", u.d.Revision)
	u.start(t)
	u.land(t, "a.txt") // untagged
	u.noRestart(t, 1500*time.Millisecond)
	next := u.land(t, "b.txt")
	u.tag(t, "v0.2.0", next)
	u.restarted(t, 20*time.Second)
	if l := strings.Join(lines(t, u.goLog), " "); l != "install example.com/app@v0.2.0" {
		t.Fatalf("go log %v", l)
	}
	if evs := u.daemonEvents(t, "update"); len(evs) != 1 || evs[0].Text != "v0.1.0 -> v0.2.0" || evs[0].Commit != next {
		t.Fatalf("update events %+v", evs)
	}
}

// The notice names the release and the specs it carries.
func TestNoticeNamesTheRelease(t *testing.T) {
	u := upstreamSetup(t, false)
	u.tag(t, "v0.1.0", u.d.Revision)
	u.start(t)
	code := u.landSpec(t)
	u.noRestart(t, time.Second)
	if u.notice() != "" {
		t.Fatalf("notice before the release: %q", u.notice())
	}
	u.tag(t, "v0.2.0", code)
	want := "bmesh: v0.2.0 is out (you run v0.1.0: 001); run bmesh update, then update the buildermesh plugin"
	waitFor(t, 20*time.Second, "the release notice", func() bool { return u.notice() == want })
}

// A binary `bmesh update --latest` swapped in past the release, before
// this daemon restarted into it, is not built over with the older release.
func TestUpdateKeepsANewerBinaryOnDisk(t *testing.T) {
	u := updateSetup(t, map[string]string{})
	u.d.Cfg.Update.Track = "release"
	rel := u.land(t, "a.txt")
	u.tag(t, "v0.2.0", rel)
	head := u.land(t, "b.txt")
	if err := os.WriteFile(u.exe, []byte("#!/bin/sh\necho bmesh "+head[:8]+" 2026-10-16T00:00:00Z\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	u.start(t) // this daemon still runs the revision before the release
	u.noRestart(t, 1500*time.Millisecond)
	if l := lines(t, u.goLog); len(l) != 0 {
		t.Fatalf("built: %v", l)
	}
}

// fakeNew makes the binary the update builds answer `config check` and
// `update --plan --json` from a script: exit config's code (printing why),
// and print plan as the plan.
func fakeNew(t *testing.T, config int, plan string) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "new.sh")
	body := "#!/bin/sh\ncase \"$*\" in\n*config\\ check*) [ " + strconv.Itoa(config) + " = 0 ] || { echo 'bmesh: config check failed: config x: [runner] t3_server is empty' >&2; exit " + strconv.Itoa(config) + "; } ;;\n" +
		"*update\\ --plan*) printf '%s\\n' '" + plan + "' ;;\nesac\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_GO_BMESH", "sh "+script)
}

// The new binary's checks hold the swap: a config it rejects, or notes
// with an ask; the daemon logs it once and leaves it as the notice.
func TestUpdateHeldByTheNewBinary(t *testing.T) {
	for _, tc := range []struct {
		name, plan, want string
		config           int
	}{
		{"config", `{}`, "config check failed: config x: [runner] t3_server is empty; keeping this binary", 3},
		{"asks", `{"asks":2,"steps":1,"confirm":1}`, "waits on bmesh update (2 asks, 1 step that asks first)", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := updateSetup(t, map[string]string{})
			u.d.StateDir = t.TempDir()
			fakeNew(t, tc.config, tc.plan)
			u.start(t)
			next := u.land(t, "a.txt")
			deadline := time.Now().Add(20 * time.Second)
			for !strings.Contains(u.log.String(), tc.want) && time.Now().Before(deadline) {
				time.Sleep(100 * time.Millisecond)
			}
			u.noRestart(t, 500*time.Millisecond)
			if b, _ := os.ReadFile(u.exe); string(b) != "old" {
				t.Fatalf("binary %q; log:\n%s", b, u.log.String())
			}
			if n := strings.Count(u.log.String(), tc.want); n != 1 {
				t.Fatalf("%d lines with %q; log:\n%s", n, tc.want, u.log.String())
			}
			if got := selfupdate.ReadNotice(u.d.StateDir); !strings.Contains(got, next[:8]) || !strings.Contains(got, tc.want) {
				t.Fatalf("notice %q", got)
			}
		})
	}
}

// A swap whose walk has nothing to do records the new binary as installed
// and the old one as previous.
func TestUpdateSwapRecordsInstalledAndPrevious(t *testing.T) {
	u := updateSetup(t, map[string]string{})
	u.d.StateDir = t.TempDir()
	old := u.d.Revision
	fakeNew(t, 0, `{"asks":0,"steps":0}`)
	u.start(t)
	next := u.land(t, "a.txt")
	u.restarted(t, 20*time.Second)
	if got := upgrade.ReadPrevious(u.d.StateDir); got != old {
		t.Fatalf("previous %q, want %s", got, old)
	}
	if rec, ok := upgrade.ReadInstalled(u.d.StateDir); !ok || rec.Revision != next {
		t.Fatalf("installed %+v %v", rec, ok)
	}
}
