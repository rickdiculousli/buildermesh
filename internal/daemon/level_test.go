package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/level"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// levelFixture is the test host joined with repo plug registered: box is
// its checkout, peer another box's clone of the same origin. The clock is
// time.Now plus skew, so a test can make the hosts loop see a wake.
type levelFixture struct {
	*hostsFixture
	origin, box, peer string
	logs              *meshtest.SyncBuffer
	skew              atomic.Int64
}

// levelSetup builds the fixture without starting the hosts loop; secure
// makes the test host secure, with an approve key, and plug a plugin
// marketplace.
func levelSetup(t *testing.T, secure bool) *levelFixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	cl, _ := filepath.Abs("../../testdata/fake-claude-plugin.sh")
	cx, _ := filepath.Abs("../../testdata/fake-codex.sh")
	t.Setenv("BMESH_CLAUDE_COMMAND", "/bin/sh "+cl)
	t.Setenv("BMESH_CODEX_COMMAND", "/bin/sh "+cx)
	t.Setenv("FAKE_PLUGIN_LOG", filepath.Join(t.TempDir(), "plugins.log"))
	f := setup(t)
	l := &levelFixture{logs: &meshtest.SyncBuffer{}}
	f.d.Now = func() time.Time { return time.Now().Add(time.Duration(l.skew.Load())) }
	f.d.Log = slog.New(slog.NewTextHandler(l.logs, nil))
	f.d.StateDir = t.TempDir()
	f.d.ConfigDir = t.TempDir()
	f.d.HostsEvery = 100 * time.Millisecond
	f.d.Revision = "1a2b3c4d5e6f"
	f.d.Cfg.Hosts.Enabled = true
	if err := os.WriteFile(hosts.HostKeyPath(f.d.ConfigDir), []byte(hexs(hostKey(testHost).Seed())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	me := hosts.Host{Name: testHost, Key: hostPub(testHost), Accept: []string{"ping", "level"}}
	if secure {
		me.Secure, me.ApproveKey = true, hostPub("approve")
	}
	r := hosts.Roster{Version: 2, AdminKey: hostPub("admin"), IssuedAt: "2026-10-16T08:00:00Z", Hosts: []hosts.Host{
		{Name: "mac", Key: hostPub("mac"), Secure: true, ApproveKey: hostPub("admin"), Accept: []string{"ping", "level"}}, me,
	}}
	r.Sign(hostKey("admin"))
	if err := hosts.SaveLocal(f.d.StateDir, hosts.Local{Pinned: hostPub("admin"), Roster: r}); err != nil {
		t.Fatal(err)
	}
	hostsNak = 200 * time.Millisecond
	t.Cleanup(func() { hostsNak = time.Minute })
	l.hostsFixture = &hostsFixture{fixture: f, roster: r, stop: func() {}}

	root := t.TempDir()
	l.origin, l.box, l.peer = filepath.Join(root, "origin.git"), filepath.Join(root, "plug"), filepath.Join(root, "peer")
	meshtest.Git(t, root, "init", "-q", "--bare", "-b", "main", l.origin)
	meshtest.Git(t, root, "clone", "-q", l.origin, l.peer)
	files := map[string]string{"a.txt": "one\n"}
	if secure {
		files[".claude-plugin/marketplace.json"] = `{"name":"plug"}`
	}
	meshtest.CommitFiles(t, l.peer, files)
	meshtest.Git(t, l.peer, "push", "-q", "origin", "HEAD:main")
	meshtest.Git(t, root, "clone", "-q", l.origin, l.box)
	f.d.Cfg.Repos = map[string]config.Repo{"plug": {Name: "plug", Path: l.box, Mode: "ship"}}
	return l
}

// startLevel starts the hosts loop and waits for its self-level on start.
func (l *levelFixture) startLevel(t *testing.T) {
	t.Helper()
	l.start(t)
	l.waitLog(t, "self-level done", 1)
}

// waitLog waits until the log holds n lines containing s.
func (l *levelFixture) waitLog(t *testing.T, s string, n int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(l.logs.String(), s) >= n {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("log never had %d of %q:\n%s", n, s, l.logs.String())
}

// land commits on peer and pushes it to origin's main.
func (l *levelFixture) land(t *testing.T, files map[string]string) string {
	t.Helper()
	sha := meshtest.CommitFiles(t, l.peer, files)
	meshtest.Git(t, l.peer, "push", "-q", "origin", "HEAD:main")
	return sha
}

// level is mac's level job for plug at sha, waited for until until.
func (l *levelFixture) level(sha string, until time.Time) hosts.Job {
	return hosts.NewJob(hostKey("mac"), "mac", testHost, "level", "plug",
		level.JobArgs(sha, "main", hosts.Stamp(until)), 2, time.Now(), time.Hour)
}

func TestLevelJobFastForwardsAndRefuses(t *testing.T) {
	l := levelSetup(t, false)
	l.startLevel(t)
	top := l.land(t, map[string]string{"b.txt": "two\n"})
	j := l.level(top, time.Now().Add(time.Minute))
	l.send(t, j)
	if r := l.reply(t, j, 1); !r.OK || r.Text != "fast-forwarded 1" {
		t.Fatalf("reply %+v", r)
	}
	if got := meshtest.Git(t, l.box, "rev-parse", "HEAD"); got != top {
		t.Fatalf("box at %s, want %s", got, top)
	}
	// A side branch's commit can't be named.
	meshtest.Git(t, l.peer, "checkout", "-q", "-b", "side")
	side := meshtest.CommitFiles(t, l.peer, map[string]string{"side.txt": "x\n"})
	meshtest.Git(t, l.peer, "push", "-q", "origin", "side")
	meshtest.Git(t, l.peer, "checkout", "-q", "main")
	j = l.level(side, time.Now().Add(time.Minute))
	l.send(t, j)
	if r := l.reply(t, j, 1); r.OK || r.Text != "refused: "+side[:8]+" not on origin/main" {
		t.Fatalf("side branch: %+v", r)
	}
	// Refused while the caller waits: answered, not pushed. Refused after
	// it stopped waiting (an asleep box answering later): pushed.
	top = l.land(t, map[string]string{"c.txt": "three\n"})
	os.WriteFile(filepath.Join(l.box, "a.txt"), []byte("mine\n"), 0o644)
	j = l.level(top, time.Now().Add(time.Minute))
	l.send(t, j)
	if r := l.reply(t, j, 1); r.OK || r.Text != "refused: dirty" {
		t.Fatalf("dirty: %+v", r)
	}
	if got := l.claims(t, "level"); len(got) != 0 {
		t.Fatalf("a refusal the caller waited for was published: %+v", got)
	}
	j = l.level(top, time.Now().Add(-time.Second))
	l.send(t, j)
	l.reply(t, j, 1)
	got := l.claims(t, "level")
	if len(got) != 1 || got[0].Text != "testhost: plug refused: dirty (job from mac)" {
		t.Fatalf("level events %+v", got)
	}
}

func TestLevelQueuedJobRunsWhenTheBoxStarts(t *testing.T) {
	l := levelSetup(t, false)
	top := l.land(t, map[string]string{"b.txt": "two\n"})
	j := l.level(top, time.Now().Add(time.Minute))
	l.send(t, j) // the box is asleep: nothing runs yet
	l.startLevel(t)
	r := l.reply(t, j, 1)
	// The self-level on start and the job race for the lock: whichever
	// runs second finds the box up to date.
	if !r.OK || (r.Text != "fast-forwarded 1" && r.Text != "up to date") {
		t.Fatalf("reply %+v", r)
	}
	if got := meshtest.Git(t, l.box, "rev-parse", "HEAD"); got != top {
		t.Fatalf("box at %s, want %s", got, top)
	}
}

func TestSelfLevelOnWakeNeverPushes(t *testing.T) {
	l := levelSetup(t, false)
	l.startLevel(t)
	top := l.land(t, map[string]string{"b.txt": "two\n"})
	l.skew.Add(int64(4 * time.Minute)) // the lid opens
	l.waitLog(t, "self-level done", 2)
	if got := meshtest.Git(t, l.box, "rev-parse", "HEAD"); got != top {
		t.Fatalf("a wake didn't fast-forward: box at %s, want %s", got, top)
	}
	// Ahead, with an old commit and a ship row: a job would push; a
	// self-level never does.
	mine := meshtest.CommitFiles(t, l.box, map[string]string{"mine.txt": "x\n"})
	l.skew.Add(int64(time.Hour))
	l.waitLog(t, "self-level done", 3)
	if got := meshtest.Git(t, l.origin, "rev-parse", "main"); got == mine {
		t.Fatal("a self-level pushed")
	}
	l.waitLog(t, "a self-level never pushes", 1)
}

func TestLevelDisabledHostsDoNothing(t *testing.T) {
	l := levelSetup(t, false)
	l.d.Cfg.Hosts.Enabled = false
	l.d.Cfg.Repos = map[string]config.Repo{} // no routines either: only what hosts would do
	l.land(t, map[string]string{"b.txt": "two\n"})
	before := meshtest.Git(t, l.box, "rev-parse", "HEAD")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, l.d) }()
	time.Sleep(time.Second)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := meshtest.Git(t, l.box, "rev-parse", "HEAD"); got != before {
		t.Fatal("the box moved with [hosts] off")
	}
	if _, err := os.Stat(filepath.Join(l.d.StateDir, "locks")); err == nil {
		t.Fatal("a lock was taken with [hosts] off")
	}
}

// approve asks the daemon to level hold x, signed with key.
func (l *levelFixture) approve(t *testing.T, x hosts.Held, key string) hosts.ApproveAnswer {
	t.Helper()
	a := hosts.NewApprove(hostKey(key), x, testHost, time.Now())
	b, _ := json.Marshal(a)
	m, err := l.c.NC.Request(hosts.ApproveSubject(testHost), b, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var ans hosts.ApproveAnswer
	if err := json.Unmarshal(m.Data, &ans); err != nil {
		t.Fatal(err)
	}
	return ans
}

func (l *levelFixture) held(t *testing.T) []hosts.Held {
	t.Helper()
	all, err := hosts.ListHeld(l.d.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	return all
}

func TestHoldApproveAndMovedTarget(t *testing.T) {
	l := levelSetup(t, true)
	l.startLevel(t)
	before := meshtest.Git(t, l.box, "rev-parse", "HEAD")
	top := l.land(t, map[string]string{"skill.md": "new\n"})
	j := l.level(top, time.Now().Add(time.Minute))
	l.send(t, j)
	if r := l.reply(t, j, 1); !r.OK || r.Text != "held for approval (+1 commit)" {
		t.Fatalf("reply %+v", r)
	}
	all := l.held(t)
	if len(all) != 1 || all[0].ID != j.ID || all[0].Target != top || !strings.Contains(all[0].Log, "skill.md") {
		t.Fatalf("holds %+v", all)
	}
	if got := meshtest.Git(t, l.box, "rev-parse", "HEAD"); got != before {
		t.Fatal("a held level moved the box")
	}
	if ev := l.claims(t, "held"); len(ev) != 1 || ev[0].Text != "testhost holds: plug +1 commit (mac)" {
		t.Fatalf("held events %+v", ev)
	}
	// Signed by another key: refused, still held.
	if ans := l.approve(t, all[0], "mac"); ans.OK || !strings.Contains(ans.Text, "approve key") {
		t.Fatalf("a forged approval: %+v", ans)
	}
	// Origin moved since the operator read the log: the new log, again.
	newer := l.land(t, map[string]string{"more.md": "x\n"})
	ans := l.approve(t, all[0], "approve")
	if ans.OK || ans.Moved == nil || ans.Moved.Target != newer || !strings.Contains(ans.Moved.Log, "more.md") {
		t.Fatalf("moved target: %+v", ans)
	}
	if got := meshtest.Git(t, l.box, "rev-parse", "HEAD"); got != before {
		t.Fatal("an approval of an old target moved the box")
	}
	ans = l.approve(t, *ans.Moved, "approve")
	if !ans.OK || ans.Text != "fast-forwarded 2 (approved)" {
		t.Fatalf("approved: %+v", ans)
	}
	if got := meshtest.Git(t, l.box, "rev-parse", "HEAD"); got != newer {
		t.Fatalf("box at %s, want %s", got, newer)
	}
	if r := l.reply(t, j, 2); !r.OK || r.Text != "fast-forwarded 2 (approved)" {
		t.Fatalf("the caller's second reply %+v", r)
	}
	if len(l.held(t)) != 0 {
		t.Fatal("the hold outlived its approval")
	}
}

func TestHoldExpiresWithItsJob(t *testing.T) {
	l := levelSetup(t, true)
	l.startLevel(t)
	top := l.land(t, map[string]string{"skill.md": "new\n"})
	j := hosts.NewJob(hostKey("mac"), "mac", testHost, "level", "plug",
		level.JobArgs(top, "main", hosts.Stamp(time.Now())), 2, time.Now(), 3*time.Second)
	l.send(t, j)
	if r := l.reply(t, j, 1); r.Text != "held for approval (+1 commit)" {
		t.Fatalf("reply %+v", r)
	}
	x := l.held(t)[0]
	deadline := time.Now().Add(10 * time.Second)
	for len(l.held(t)) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the hold never expired")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if ans := l.approve(t, x, "approve"); ans.OK || ans.Text != "no hold "+x.ID {
		t.Fatalf("approving an expired hold: %+v", ans)
	}
}

func TestSelfLevelIsHeldOnASecureBox(t *testing.T) {
	l := levelSetup(t, true)
	top := l.land(t, map[string]string{"skill.md": "new\n"})
	l.startLevel(t)
	all := l.held(t)
	if len(all) != 1 || !all[0].Self || all[0].Target != top || all[0].Job.By != testHost {
		t.Fatalf("holds %+v", all)
	}
	if ev := l.claims(t, "held"); len(ev) != 1 || ev[0].Text != "testhost holds: plug +1 commit (self-level)" {
		t.Fatalf("held events %+v", ev)
	}
	// A wake doesn't hold it twice.
	l.skew.Add(int64(4 * time.Minute))
	l.waitLog(t, "self-level done", 2)
	if n := len(l.held(t)); n != 1 {
		t.Fatalf("%d holds after a wake", n)
	}
	if ans := l.approve(t, all[0], "approve"); !ans.OK || ans.Text != "fast-forwarded 1 (approved)" {
		t.Fatalf("approved: %+v", ans)
	}
	if got := meshtest.Git(t, l.box, "rev-parse", "HEAD"); got != top {
		t.Fatalf("box at %s, want %s", got, top)
	}
}

func TestPushHoldsAndUnansweredLevels(t *testing.T) {
	f, n := pushSetup(t)
	f.d.Cfg.Notify.Push = []string{}
	f.d.Cfg.Hosts.Enabled = true
	startPush(t, f.d)
	time.Sleep(200 * time.Millisecond)
	f.event(t, mesh.Event{From: "daemon.mac", Event: "held", Body: "job1", Text: "mac holds: pstack-claude +3 commits (k16)"})
	f.event(t, mesh.Event{From: "daemon.mac", Event: "level", Body: "plug", Text: "mac: plug refused: diverged (job from k16)"})
	got := n.waitRequests(t, 2)
	if got[0].Title != "mac holds: pstack-claude +3 commits (k16)" || got[1].Title != "mac: plug refused: diverged (job from k16)" ||
		got[1].Priority != "high" {
		t.Fatalf("pushes %+v", got)
	}
}
