package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/selfupdate"
	"github.com/rickdiculousli/buildermesh/internal/upgrade"
)

// swapSetup is updateSetup with a state dir and pushes to a fake ntfy, its
// log returned.
func swapSetup(t *testing.T, files map[string]string) (*ufix, string) {
	t.Helper()
	u := updateSetup(t, files)
	u.d.StateDir = t.TempDir()
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-ntfy.sh"))
	if err != nil {
		t.Fatal(err)
	}
	ntfy := filepath.Join(t.TempDir(), "ntfy.log")
	t.Setenv("FAKE_NTFY_LOG", ntfy)
	u.d.Cfg.Notify.Command = "sh " + fake
	return u, ntfy
}

// swapIn puts a binary at the daemon's path as `bmesh update` leaves it: its
// `version` names said (a revision, or "unknown") and is logged to the
// returned file, and its `config check` exits config, saying why, until
// the config file it is given exists.
func (u *ufix) swapIn(t *testing.T, said string, config int) (versions string) {
	t.Helper()
	versions = filepath.Join(t.TempDir(), "versions")
	body := "#!/bin/sh\ncase \"$*\" in\n" +
		"version) echo run >> " + versions + "; echo 'bmesh " + said + " 2026-10-23T00:00:00Z'; exit 0 ;;\n" +
		"*config\\ check*) [ " + strconv.Itoa(config) + " = 0 ] || [ -f \"$2\" ] || { echo 'bmesh: config check failed: config x: [runner] t3_server is empty' >&2; exit " + strconv.Itoa(config) + "; } ;;\n" +
		"esac\nexit 0\n"
	tmp := u.exe + ".new"
	if err := os.WriteFile(tmp, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, u.exe); err != nil {
		t.Fatal(err)
	}
	return versions
}

func count(path, s string) int { b, _ := os.ReadFile(path); return strings.Count(string(b), s) }

// After a hand-run `bmesh update` swapped a newer build in at its path, the
// daemon waits while update.lock is held, reading the binary's version once
// however many scans pass, then drains (a running job goes back to the
// inbox, unanswered) and restarts onto it, publishing no update.
func TestSwappedInBinaryRestartsTheDaemon(t *testing.T) {
	runs := filepath.Join(t.TempDir(), "runs")
	u, _ := swapSetup(t, map[string]string{".bmesh/routines/slow.toml": `
mode = "pooled"
timeout = "1m"
batch_idle = "1s"
command = "echo $BMESH_JOB >> ` + runs + `; sleep 30"
`})
	old := u.d.Revision
	// The release this daemon runs is the one [update] follows, so the
	// commit landed below is no self-update of its own.
	u.d.Cfg.Update.Track = "release"
	u.tag(t, "v0.1.0", old)
	var jobsAtRestart []mesh.QueuedJob
	var repliesAtRestart int
	u.onRestart = func() {
		var err error
		if jobsAtRestart, err = mesh.KindJobs(u.ctx, u.c, "slow"); err != nil {
			t.Error(err)
		}
		hist, _ := mesh.History(u.ctx, u.c, mesh.InboxSubject(mesh.Path{requester}), time.Time{})
		for _, s := range hist {
			var m mesh.Message
			if json.Unmarshal(s.Data, &m) == nil && m.Reply != nil {
				repliesAtRestart++
			}
		}
	}
	u.start(t)
	u.waitStarted(t, "routine.slow.testhost-cmd-1", 1)
	id := u.queue(t, mesh.Job{Kind: "slow", Go: true})
	waitFor(t, 10*time.Second, "the job running", func() bool { return len(lines(t, runs)) == 1 })

	// update holds its lock from before the swap to the end of its walk.
	unlock, err := selfupdate.LockUpdate(u.d.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	next := u.land(t, "a.txt")
	versions := u.swapIn(t, next[:8], 0)
	waitFor(t, 10*time.Second, "the version read", func() bool { return count(versions, "run") == 1 })
	u.noRestart(t, 1500*time.Millisecond) // several scans
	if n := count(versions, "run"); n != 1 {
		t.Fatalf("version run %d times for one binary", n)
	}
	unlock()
	u.restarted(t, 5*time.Second)

	if len(jobsAtRestart) != 1 || jobsAtRestart[0].ID != id || jobsAtRestart[0].State != "waiting" || repliesAtRestart != 0 {
		t.Fatalf("jobs at restart %+v, %d replies; want %s waiting, none", jobsAtRestart, repliesAtRestart, id)
	}
	want := "update: v0.1.0 -> v0.1.0+1 (" + next[:8] + ") (swapped in by bmesh update); restarting"
	if !strings.Contains(u.log.String(), want) {
		t.Fatalf("log lacks %q:\n%s", want, u.log.String())
	}
	if evs := u.daemonEvents(t, "update"); len(evs) != 0 {
		t.Fatalf("update events %+v, want none", evs)
	}
	if l := lines(t, u.goLog); len(l) != 0 {
		t.Fatalf("built: %v", l)
	}
}

// A binary of no revision, an older one, or one on another line leaves the
// daemon as it is.
func TestSwappedInBinaryNotNewerLeftAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		said func(t *testing.T, u *ufix) string
	}{
		{"no revision", func(t *testing.T, u *ufix) string { return "unknown" }},
		{"older", func(t *testing.T, u *ufix) string {
			before := u.d.Revision
			u.d.Revision = u.land(t, "a.txt")
			return before[:8]
		}},
		{"another line", func(t *testing.T, u *ufix) string {
			parent := meshtest.Git(t, u.repo, "rev-parse", "HEAD~1")
			apart := meshtest.Git(t, u.repo, "commit-tree", "-p", parent, "-m", "apart", "HEAD^{tree}")
			meshtest.Git(t, u.repo, "update-ref", "refs/heads/apart", apart)
			return apart[:8]
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, _ := swapSetup(t, map[string]string{})
			u.land(t, "base.txt") // a parent for another line to start from
			u.d.Revision = meshtest.Git(t, u.repo, "rev-parse", "HEAD")
			said := tc.said(t, u)
			u.start(t)
			versions := u.swapIn(t, said, 0)
			waitFor(t, 10*time.Second, "the version read", func() bool { return count(versions, "run") == 1 })
			u.noRestart(t, 1500*time.Millisecond)
		})
	}
}

// A newer binary whose walk is unfinished, or whose config check fails, is
// refused with one log line, one push and the notice, then nothing more
// until the walk record, the binary or the config changes; then the daemon
// restarts.
func TestSwappedInBinaryRefusedUntilItsWalkAndConfigPass(t *testing.T) {
	for _, tc := range []struct {
		name, why, record string
		config            int
		fix               func(t *testing.T, u *ufix, next string)
	}{
		{"walk pending", "its walk is unfinished (upgrade notes pending since v0.1.0: run bmesh update)", "v0.1.0", 0,
			func(t *testing.T, u *ufix, next string) {
				if err := upgrade.WriteInstalled(u.d.StateDir, upgrade.Record{Version: "v0.2.0", Revision: next, At: "2026-10-23T06:00:00Z"}); err != nil {
					t.Fatal(err)
				}
			}},
		{"config check", "its config check failed: config x: [runner] t3_server is empty", "v0.2.0", 3,
			func(t *testing.T, u *ufix, next string) { u.swapIn(t, next[:8], 0) }},
		{"config fixed", "its config check failed: config x: [runner] t3_server is empty", "v0.2.0", 3,
			func(t *testing.T, u *ufix, next string) {
				if err := os.WriteFile(u.d.Cfg.Path(), []byte("# fixed\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, ntfy := swapSetup(t, map[string]string{})
			u.tag(t, "v0.1.0", u.d.Revision)
			next := u.land(t, "a.txt")
			u.tag(t, "v0.2.0", next)
			if err := upgrade.WriteInstalled(u.d.StateDir, upgrade.Record{Version: tc.record, At: "2026-10-23T05:00:00Z"}); err != nil {
				t.Fatal(err)
			}
			u.start(t)
			u.swapIn(t, next[:8], tc.config)
			line := "update: v0.2.0 is at " + u.exe + " but " + tc.why + "; not restarting onto it"
			waitFor(t, 10*time.Second, "the refusal", func() bool { return strings.Contains(u.log.String(), line) })
			u.noRestart(t, 1500*time.Millisecond) // several scans
			if n := strings.Count(u.log.String(), line); n != 1 {
				t.Fatalf("%d refusals logged:\n%s", n, u.log.String())
			}
			if n := count(ntfy, "---\n"); n != 1 {
				t.Fatalf("%d pushes", n)
			}
			if got := selfupdate.ReadNotice(u.d.StateDir); !strings.Contains(got, strings.TrimPrefix(line, "update: ")) {
				t.Fatalf("notice %q", got)
			}
			tc.fix(t, u, next)
			u.restarted(t, 5*time.Second)
			if got := selfupdate.ReadNotice(u.d.StateDir); got != "" {
				t.Fatalf("notice left: %q", got)
			}
		})
	}
}

// A daemon says in its started that it restarts itself only when it can:
// a binary to restart, a state dir, a history to read and, on Windows,
// serve --restart.
func TestStartedSaysItRestarts(t *testing.T) {
	u, _ := swapSetup(t, map[string]string{})
	can := func() []string {
		announce(u.ctx, u.d)
		evs := u.daemonEvents(t, "started")
		return evs[len(evs)-1].Can
	}
	if got := can(); strings.Join(got, ",") != "land,"+CanRestart {
		t.Fatalf("can %v", got)
	}
	u.d.GOOS = "windows"
	if got := can(); strings.Join(got, ",") != "land" {
		t.Fatalf("unsupervised windows: can %v", got)
	}
	u.d.Supervised = true
	if got := can(); strings.Join(got, ",") != "land,"+CanRestart {
		t.Fatalf("supervised windows: can %v", got)
	}
	u.d.GOOS, u.d.StateDir = "linux", ""
	if got := can(); strings.Join(got, ",") != "land" {
		t.Fatalf("no state dir: can %v", got)
	}
}
