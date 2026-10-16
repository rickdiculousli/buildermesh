package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/rickdiculousli/buildermesh/internal/hosts"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

func TestWakeAcrossAWallClockJump(t *testing.T) {
	var w wakeClock
	t0 := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	if _, woke := w.observe(t0); woke {
		t.Fatal("the first tick is a wake")
	}
	if _, woke := w.observe(t0.Add(time.Minute)); woke {
		t.Fatal("a minute's tick is a wake")
	}
	gap, woke := w.observe(t0.Add(time.Minute + 3*time.Minute))
	if !woke || gap != 3*time.Minute {
		t.Fatalf("a 3m jump: gap %v woke %v", gap, woke)
	}
	// A reading with a monotonic clock is compared on the wall clock: a
	// laptop's monotonic time stops while it sleeps.
	now := time.Now()
	w = wakeClock{last: now.Add(-10 * time.Minute).Round(0)}
	if _, woke := w.observe(now); !woke {
		t.Fatal("a 10m wall-clock gap with a monotonic reading: no wake")
	}
}

func hostKey(name string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("daemon test " + name))
	return ed25519.NewKeyFromSeed(seed[:])
}

func hostPub(name string) string { return hosts.KeyHex(hostKey(name).Public().(ed25519.PublicKey)) }

// hostsFixture is the test host joined to a roster where mac is the admin
// and testhost accepts accept, with the hosts loop running.
type hostsFixture struct {
	*fixture
	roster hosts.Roster
	stop   func()
}

func testRoster(version int64, accept ...string) hosts.Roster {
	r := hosts.Roster{Version: version, AdminKey: hostPub("admin"), IssuedAt: "2026-10-16T08:00:00Z", Hosts: []hosts.Host{
		{Name: "mac", Key: hostPub("mac"), Secure: true, ApproveKey: hostPub("admin"), Accept: []string{"ping"}},
		{Name: testHost, Key: hostPub(testHost), Accept: accept},
	}}
	r.Sign(hostKey("admin"))
	return r
}

func hostsSetup(t *testing.T, accept ...string) *hostsFixture {
	t.Helper()
	f := setup(t)
	f.now = time.Now()
	f.d.Now = time.Now
	f.d.StateDir = t.TempDir()
	f.d.ConfigDir = t.TempDir()
	f.d.HostsEvery = 100 * time.Millisecond
	f.d.Revision = "1a2b3c4d5e6f"
	f.d.Cfg.Hosts.Enabled = true
	seed := hostKey(testHost).Seed()
	if err := os.WriteFile(hosts.HostKeyPath(f.d.ConfigDir), []byte(hexs(seed)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := testRoster(2, accept...)
	if err := hosts.SaveLocal(f.d.StateDir, hosts.Local{Pinned: hostPub("admin"), Roster: r}); err != nil {
		t.Fatal(err)
	}
	hostsNak = 200 * time.Millisecond
	t.Cleanup(func() { hostsNak = time.Minute })
	h := &hostsFixture{fixture: f, roster: r}
	h.start(t)
	return h
}

func hexs(b []byte) string { return hosts.KeyHex(ed25519.PublicKey(b)) }

func (h *hostsFixture) start(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); hostsRun(ctx, h.d) }()
	var once sync.Once
	h.stop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("hosts loop did not stop")
			}
		})
	}
	t.Cleanup(h.stop)
}

func (h *hostsFixture) send(t *testing.T, j hosts.Job) {
	t.Helper()
	b, _ := json.Marshal(j)
	// A fresh msg id each time, so a test can replay a job past the
	// stream's duplicate window.
	if _, err := h.c.JS.Publish(h.ctx, hosts.JobSubject(j.To), b, jetstream.WithMsgID(j.ID+"-"+hosts.NewSalt())); err != nil {
		t.Fatal(err)
	}
}

func (h *hostsFixture) ping(by string) hosts.Job {
	return hosts.NewJob(hostKey(by), by, testHost, "ping", "", nil, 2, time.Now(), time.Hour)
}

// reply waits for the n-th reply (1-based) to job j on its caller's subject.
func (h *hostsFixture) reply(t *testing.T, j hosts.Job, n int) hosts.Reply {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		hist, err := mesh.History(h.ctx, h.c, hosts.ReplySubject(j.By, j.ID), time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if len(hist) >= n {
			var r hosts.Reply
			if err := json.Unmarshal(hist[n-1].Data, &r); err != nil {
				t.Fatal(err)
			}
			return r
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no reply %d to %s", n, j.ID)
	return hosts.Reply{}
}

func (h *hostsFixture) noReply(t *testing.T, j hosts.Job, wait time.Duration) {
	t.Helper()
	time.Sleep(wait)
	hist, err := mesh.History(h.ctx, h.c, hosts.ReplySubject(j.By, j.ID), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 0 {
		t.Fatalf("job %s answered: %s", j.ID, hist[0].Data)
	}
}

func (h *hostsFixture) refusals(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, ev := range h.claims(t, "refused") {
		out = append(out, ev.Body)
	}
	return out
}

func TestHostsPingAnswersSigned(t *testing.T) {
	h := hostsSetup(t, "ping")
	j := h.ping("mac")
	if err := hosts.SendJob(h.ctx, h.c.JS, j); err != nil {
		t.Fatal(err)
	}
	r := h.reply(t, j, 1)
	if err := hosts.CheckReply(&r, &j, &h.roster); err != nil {
		t.Fatal(err)
	}
	if !r.OK || r.Text != "1a2b3c4d, roster v2" {
		t.Fatalf("reply: %+v", r)
	}
}

func TestHostsRefusals(t *testing.T) {
	h := hostsSetup(t, "ping")
	changed := h.ping("mac")
	changed.Args = []string{"tampered"}
	wrongBox := hosts.NewJob(hostKey("mac"), "mac", "mac", "ping", "", nil, 2, time.Now(), time.Hour)
	rogue := hosts.NewJob(hostKey("rogue"), "rogue", testHost, "ping", "", nil, 2, time.Now(), time.Hour)
	old := hosts.NewJob(hostKey("mac"), "mac", testHost, "ping", "", nil, 2, time.Now().Add(-25*time.Hour), time.Hour)
	unaccepted := hosts.NewJob(hostKey("mac"), "mac", testHost, "prune", "", nil, 2, time.Now(), time.Hour)
	for _, c := range []struct {
		job   hosts.Job
		check string
	}{{changed, "signature"}, {rogue, "roster"}, {old, "expired"}, {unaccepted, "accept"}} {
		h.send(t, c.job)
		r := h.reply(t, c.job, 1)
		if r.OK || r.Text != "refused: "+c.check {
			t.Errorf("%s: reply %+v", c.check, r)
		}
	}
	// A job to another box isn't on this box's subject; delivered here
	// anyway (a token holder can publish anywhere), it is refused.
	b, _ := json.Marshal(wrongBox)
	if _, err := h.c.JS.Publish(h.ctx, hosts.JobSubject(testHost), b); err != nil {
		t.Fatal(err)
	}
	if r := h.reply(t, wrongBox, 1); r.Text != "refused: to" {
		t.Errorf("to: reply %+v", r)
	}
	got := strings.Join(h.refusals(t), ",")
	if got != "mac signature,rogue roster,mac to" {
		t.Errorf("refused events %q: want signature, roster and to (expired and accept are logged only)", got)
	}
}

func TestHostsReplayGetsTheStoredReply(t *testing.T) {
	h := hostsSetup(t, "ping")
	j := h.ping("mac")
	h.send(t, j)
	first := h.reply(t, j, 1)
	h.send(t, j) // replayed
	again := h.reply(t, j, 2)
	if again.Sig != first.Sig || again.Text != first.Text {
		t.Fatalf("replay answered %+v, want the stored %+v", again, first)
	}
	// The same job again is a redelivery as much as a replay: nothing runs,
	// so nothing is pushed. Another job reusing the salt is refused.
	thief := j
	thief.ID = "thief"
	thief.Sign(hostKey("mac"))
	h.send(t, thief)
	if r := h.reply(t, thief, 1); r.OK || r.Text != "refused: replay" {
		t.Fatalf("a reused salt: %+v", r)
	}
	if got := h.refusals(t); len(got) != 1 || got[0] != "mac replay" {
		t.Fatalf("refused events %q: want only the reused salt's", got)
	}
}

func TestHostsInterruptedAfterARestart(t *testing.T) {
	h := hostsSetup(t, "ping", "nix")
	h.stop()
	j := hosts.NewJob(hostKey("admin"), "mac", testHost, "nix", "", nil, 2, time.Now(), time.Hour)
	// The daemon died mid-job: the record says started.
	if err := hosts.OpenSalts(h.d.StateDir).Put(j.Salt, hosts.SaltRecord{State: hosts.SaltStarted, JobID: j.ID, Op: j.Op, Expires: j.Expires}); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	h.send(t, j)
	if r := h.reply(t, j, 1); r.OK || r.Text != "interrupted" {
		t.Fatalf("reply %+v", r)
	}
	rec, _, _ := hosts.OpenSalts(h.d.StateDir).Get(j.Salt)
	if rec.State != hosts.SaltDone {
		t.Fatalf("record %+v", rec)
	}
}

func TestHostsNakUntilTheRosterCatchesUp(t *testing.T) {
	h := hostsSetup(t, "ping")
	j := hosts.NewJob(hostKey("mac"), "mac", testHost, "ping", "", nil, 3, time.Now(), time.Hour)
	h.send(t, j)
	h.noReply(t, j, 600*time.Millisecond)
	kv, err := hosts.Bucket(h.ctx, h.c.JS, hosts.BucketRoster)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(testRoster(3, "ping"))
	if _, err := kv.Put(h.ctx, hosts.RosterKey, b); err != nil {
		t.Fatal(err)
	}
	if r := h.reply(t, j, 1); !r.OK || r.Text != "1a2b3c4d, roster v3" {
		t.Fatalf("reply %+v", r)
	}
}

func TestHostsRePutsADeletedRosterAndWritesPresence(t *testing.T) {
	h := hostsSetup(t, "ping")
	kv, err := hosts.Bucket(h.ctx, h.c.JS, hosts.BucketRoster)
	if err != nil {
		t.Fatal(err)
	}
	waitVersion := func(want int64) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if r, ok, _ := hosts.HubRoster(h.ctx, kv); ok && r.Version == want {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("hub roster never at v%d", want)
	}
	waitVersion(2)
	if err := kv.Delete(h.ctx, hosts.RosterKey); err != nil {
		t.Fatal(err)
	}
	waitVersion(2)
	pkv, err := hosts.Bucket(h.ctx, h.c.JS, hosts.BucketHosts)
	if err != nil {
		t.Fatal(err)
	}
	seen, err := hosts.ReadPresence(h.ctx, pkv, h.roster, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range seen {
		want := hosts.Absent
		if s.Host.Name == testHost {
			want = hosts.Awake
		}
		if s.State != want {
			t.Errorf("%s: %s, want %s", s.Host.Name, s.State, want)
		}
	}
}

func TestPushRefusedOncePerSenderAndCheck(t *testing.T) {
	f, n := pushSetup(t)
	f.d.Cfg.Notify.Push = []string{}
	f.d.Cfg.Hosts.Enabled = true
	startPush(t, f.d)
	time.Sleep(200 * time.Millisecond)
	refuse := func(body string) {
		f.event(t, mesh.Event{From: "daemon.k16", Event: "refused", Body: body, Text: "refused " + body})
	}
	refuse("mac signature")
	refuse("mac signature") // within ten minutes: quiet
	refuse("mac to")
	got := n.waitRequests(t, 2)
	if got[0].Title != "k16 refused mac: signature" || got[1].Title != "k16 refused mac: to" {
		t.Fatalf("pushes %+v", got)
	}
}

func TestPushRefusedNeedsHosts(t *testing.T) {
	f, n := pushSetup(t)
	f.d.Cfg.Notify.Push = []string{"ask"}
	startPush(t, f.d)
	time.Sleep(200 * time.Millisecond)
	f.event(t, mesh.Event{From: "daemon.k16", Event: "refused", Body: "mac signature"})
	n.waitRequests(t, 0)
}

func TestHostsWritesNotesAndTheSessionLine(t *testing.T) {
	h := hostsSetup(t, "ping")
	if err := os.WriteFile(hosts.NotesPath(h.d.ConfigDir), []byte("Run commands here directly.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := "this box is testhost; secure: no; on the mesh: mac (absent, secure, admin); notes: `bmesh hosts --notes`\n"
	deadline := time.Now().Add(5 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(hosts.HerePath(h.d.StateDir))
		if got = string(b); got == want {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got != want {
		t.Fatalf("this-box.txt = %q, want %q", got, want)
	}
	pkv, err := hosts.Bucket(h.ctx, h.c.JS, hosts.BucketHosts)
	if err != nil {
		t.Fatal(err)
	}
	seen, err := hosts.ReadPresence(h.ctx, pkv, h.roster, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range seen {
		if s.Host.Name == testHost && (s.State != hosts.Awake || s.Presence.Notes != "Run commands here directly.") {
			t.Fatalf("presence %s %q", s.State, s.Presence.Notes)
		}
	}
}
