package daemon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// ntfyReq is one request the fake ntfy server received.
type ntfyReq struct {
	Method, Title, Priority, Click, Actions, Body string
}

// fakeNtfy is an httptest.Server standing in for ntfy: it records every
// request and answers with status.
type fakeNtfy struct {
	*httptest.Server
	mu     sync.Mutex
	reqs   []ntfyReq
	status int
}

func newFakeNtfy(t *testing.T) *fakeNtfy {
	t.Helper()
	n := &fakeNtfy{status: http.StatusOK}
	n.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		h := r.Header
		n.mu.Lock()
		n.reqs = append(n.reqs, ntfyReq{Method: r.Method, Title: h.Get("Title"), Priority: h.Get("Priority"),
			Click: h.Get("Click"), Actions: h.Get("Actions"), Body: string(b)})
		status := n.status
		n.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(n.Close)
	return n
}

func (n *fakeNtfy) setStatus(s int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.status = s
}

func (n *fakeNtfy) requests() []ntfyReq {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]ntfyReq(nil), n.reqs...)
}

// waitRequests waits until the server holds want requests, then a little
// longer to catch any extra one, and returns them.
func (n *fakeNtfy) waitRequests(t *testing.T, want int) []ntfyReq {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for len(n.requests()) < want && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	got := n.requests()
	if len(got) != want {
		t.Fatalf("ntfy got %d requests, want %d: %+v", len(got), want, got)
	}
	return got
}

// pushSetup is the fixture with ntfy pointed at a fake server, fast retries,
// a map URL and a thread link template.
func pushSetup(t *testing.T) (*fixture, *fakeNtfy) {
	t.Helper()
	f := setup(t)
	n := newFakeNtfy(t)
	f.d.Cfg.Notify.Ntfy = n.URL
	f.d.Cfg.Notify.MapURL = "http://map.test/"
	f.d.Cfg.Console.ThreadURL = "http://t3.test/t/{thread}"
	f.d.PushRetry = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}
	return f, n
}

// startPush runs the push loop for d until the returned stop is called (or
// the test ends); stop waits for the loop to return.
func startPush(t *testing.T, d Deps) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); pushLoop(ctx, d) }()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("push loop did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func (f *fixture) event(t *testing.T, ev mesh.Event) {
	t.Helper()
	if err := mesh.PublishEvent(f.ctx, f.c, ev); err != nil {
		t.Fatal(err)
	}
}

// claims returns every daemon's push claims.
func (f *fixture) claims(t *testing.T, event string) []mesh.Event {
	t.Helper()
	hist, err := mesh.History(f.ctx, f.c, "bm.daemon.*."+event, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var out []mesh.Event
	for _, s := range hist {
		ev, err := decodeEvent(s.Data)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

// shortDupWindow shrinks the stream's duplicate window so a test can outlive
// it: a repeated claim id is then stored again, and only the history
// catch-up keeps an event from being pushed twice.
func (f *fixture) shortDupWindow(t *testing.T) {
	t.Helper()
	st, err := f.c.JS.Stream(f.ctx, mesh.StreamMesh)
	if err != nil {
		t.Fatal(err)
	}
	cfg := st.CachedInfo().Config
	cfg.Duplicates = 100 * time.Millisecond
	if _, err := f.c.JS.UpdateStream(f.ctx, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestPushLeadAskBlockedDone(t *testing.T) {
	f, n := pushSetup(t)
	f.lead(t, "a", "th-a", "")
	startPush(t, f.d)
	f.event(t, mesh.Event{From: "a", Event: "ask", Text: "which colour?"})
	f.event(t, mesh.Event{From: "a", Event: "blocked", Text: "no creds"})
	f.event(t, mesh.Event{From: "a", Event: "done", Text: "shipped", Commit: "0123456789abcdef"})
	got := n.waitRequests(t, 3)
	// a has no family, so its done ends a batch of one.
	want := []ntfyReq{
		{"POST", "❓ Asks • a", "high", "http://t3.test/t/th-a", "view, Open map, http://map.test/#/lead/a", "which colour?"},
		{"POST", "⛔ Blocked • a", "high", "http://t3.test/t/th-a", "view, Open map, http://map.test/#/lead/a", "no creds"},
		{"POST", "✅ Landed • a", "default", "http://map.test/", "", "a: shipped"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d =\n%+v\nwant\n%+v", i, got[i], want[i])
		}
	}
	claims := f.daemonEvents(t, "push")
	if len(claims) != 3 {
		t.Fatalf("push events = %+v", claims)
	}
	for i, c := range claims {
		if c.To != "a" || c.Text != want[i].Title || c.Seq == 0 || c.From != "daemon."+testHost {
			t.Errorf("push event %d = %+v", i, c)
		}
	}
}

func TestPushRequest(t *testing.T) {
	cfg := config.NotifyConfig{Push: []string{"ask", "blocked", "done"}}
	lead := mesh.Node{Path: "a", Role: "lead"}
	req, ok := Push(mesh.Event{From: "a", Event: "ask", Text: "q"}, lead, cfg, "")
	if !ok || req.Title != "❓ Asks • a" || req.Click != "" || req.Actions != "" || req.Body != "q" {
		t.Errorf("no map_url, no console: %+v %v", req, ok)
	}
	cfg.MapURL = "http://map.test"
	if req, _ := Push(mesh.Event{From: "a", Event: "blocked", Text: "x"}, lead, cfg, ""); req.Click != "http://map.test#/lead/a" || req.Body != "x" {
		t.Errorf("blocked: %+v", req)
	}
	if _, ok := Push(mesh.Event{From: "a", Event: "done", Text: "x"}, lead, cfg, ""); ok {
		t.Error("a lead's done pushed alone: its batch's end pushes it")
	}
	if _, ok := Push(mesh.Event{From: "a", Event: "blocked", Text: mesh.StoppedBy("operator", "")}, lead, cfg, ""); ok {
		t.Error("a stop pushed")
	}
	if req, _ := Push(mesh.Event{From: "a", Event: "ask"}, lead, cfg, "http://t3.test/t/th-a"); req.Click != "http://t3.test/t/th-a" ||
		req.Actions != "view, Open map, http://map.test#/lead/a" {
		t.Errorf("thread and map: %+v", req)
	}
	cfg.MapURL = ""
	if req, _ := Push(mesh.Event{From: "a", Event: "ask"}, lead, cfg, "http://t3.test/t/th-a"); req.Click != "http://t3.test/t/th-a" || req.Actions != "" {
		t.Errorf("thread, no map: %+v", req)
	}
	cfg.MapURL = "http://map.test"
	if _, ok := Push(mesh.Event{From: "a.x", Event: "ask"}, mesh.Node{Path: "a.x", Role: "executor"}, cfg, ""); ok {
		t.Error("executor pushed")
	}
	fixer := mesh.Node{Path: "bm-fix-1", Role: "fixer", Repo: "bm", Family: "bm-fix-1"}
	if req, ok := Push(mesh.Event{From: "bm-fix-1", Event: "done", Text: "1779: proposed 060", Commit: "abc"}, fixer, cfg, ""); !ok ||
		req.Title != "🔧 Fixed • bm: 0 fixed, 1 waits on you" || req.Body != "1779: proposed 060\ncommit abc" {
		t.Errorf("fixer done: %+v %v", req, ok)
	}
	if req, ok := Push(mesh.Event{From: "bm-fix-1", Event: "ask", Text: "which?"}, fixer, cfg, ""); !ok || req.Title != "❓ Asks • bm-fix-1" {
		t.Errorf("fixer ask: %+v %v", req, ok)
	}
	// A fix lead reports to its fixer, never to the operator.
	fixLead := mesh.Node{Path: "bm-061", Role: "lead", Repo: "bm", Family: "bm-fix-1"}
	for _, e := range []string{"ask", "blocked", "done"} {
		if req, ok := Push(mesh.Event{From: "bm-061", Event: e, Commit: "abc"}, fixLead, cfg, ""); ok {
			t.Errorf("fix lead %s pushed: %+v", e, req)
		}
	}
	if !threaded(fixer) {
		t.Error("a fixer is not woken")
	}
	if _, ok := Push(mesh.Event{From: "a", Event: "progress"}, lead, cfg, ""); ok {
		t.Error("progress pushed")
	}
	if _, ok := Push(mesh.Event{From: "a", Event: "held"}, lead, config.NotifyConfig{Push: []string{"ask", "blocked", "done", "held"}}, ""); ok {
		t.Error("held pushed")
	}
	cfg.Push = []string{"done"}
	if _, ok := Push(mesh.Event{From: "a", Event: "ask"}, lead, cfg, ""); ok {
		t.Error("ask pushed though not listed")
	}
	// The text is cut to 3 KB on a rune boundary.
	long := strings.Repeat("é", 2000) // 4000 bytes
	fixer = mesh.Node{Path: "bm-fix-1", Role: "fixer", Repo: "bm"}
	req, _ = Push(mesh.Event{From: "bm-fix-1", Event: "done", Text: long, Commit: "abc"}, fixer, cfg, "")
	text, commit, _ := strings.Cut(req.Body, "\n")
	if len(text) > 3<<10 || len(text) < 3<<10-1 || !strings.HasPrefix(long, text) || commit != "commit abc" {
		t.Errorf("cut body: %d bytes, commit line %q", len(text), commit)
	}
}

func TestFixerReport(t *testing.T) {
	text := "Waits on you: 1781 names pstack-claude\n1786: fixed by 061 (structure, failing-first)\n1779: proposed 060 (no-new-surface: adds bmesh spec steps)\n" +
		"1790, 1791: fixed by 062 (root)\n- 1795: proposed 063 (reversible: L)\n1800: dropped (drop: 044 covers it)\nreleased bm v0.4.1"
	title, body := FixerReport("bm", text)
	if title != "🔧 Fixed • bm: 2 fixed, 2 wait on you" {
		t.Errorf("title %q", title)
	}
	want := "1779: proposed 060 (no-new-surface: adds bmesh spec steps)\n- 1795: proposed 063 (reversible: L)\nWaits on you: 1781 names pstack-claude\n" +
		"1786: fixed by 061 (structure, failing-first)\n1790, 1791: fixed by 062 (root)\n1800: dropped (drop: 044 covers it)\nreleased bm v0.4.1"
	if body != want {
		t.Errorf("body\n%s\nwant\n%s", body, want)
	}
	if title, body := FixerReport("bm", "nothing to rule on"); title != "🔧 Fixed • bm: 0 fixed, 0 wait on you" || body != "nothing to rule on" {
		t.Errorf("empty pass: %q %q", title, body)
	}
}

// A fix lead's events reach its fixer, not the phone; the fixer's done does.
func TestPushFixLeadQuietFixerDone(t *testing.T) {
	f, n := pushSetup(t)
	for _, node := range []mesh.Node{
		{Path: "bm-fix-1", Role: "fixer", Repo: "bm", Family: "bm-fix-1", Host: testHost},
		{Path: "bm-061", Role: "lead", Repo: "bm", Family: "bm-fix-1", Host: testHost},
	} {
		if err := mesh.PutNode(f.ctx, f.c, node); err != nil {
			t.Fatal(err)
		}
	}
	startPush(t, f.d)
	f.event(t, mesh.Event{From: "bm-061", Event: "ask", Text: "which?"})
	f.event(t, mesh.Event{From: "bm-061", Event: "done", Text: "fixed", Commit: "abc"})
	f.event(t, mesh.Event{From: "bm-fix-1", Event: "done", Text: "1786: fixed by 061 (root)\n1779: proposed 060 (no-new-surface)", Commit: "def"})
	got := n.waitRequests(t, 1)
	if got[0].Title != "🔧 Fixed • bm: 1 fixed, 1 waits on you" || got[0].Body != "1779: proposed 060 (no-new-surface)\n1786: fixed by 061 (root)\ncommit def" {
		t.Errorf("request = %+v", got[0])
	}
}

func TestNoPushForExecutorsOrUnlistedEvents(t *testing.T) {
	f, n := pushSetup(t)
	f.lead(t, "a", "th-a", "")
	f.lead(t, "b", "th-b", "")
	if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: "a.x", Role: "executor", Host: testHost, Parent: "a"}); err != nil {
		t.Fatal(err)
	}
	startPush(t, f.d)
	f.event(t, mesh.Event{From: "a.x", Event: "ask", Text: "which db?"})
	f.event(t, mesh.Event{From: "a.x", Event: "done", Text: "built", Commit: "abc"})
	f.event(t, mesh.Event{From: "a", Event: "progress", Text: "half"})
	f.event(t, mesh.Event{From: "a", Event: "started"})
	f.event(t, mesh.Event{From: "a", Event: "held", Text: "before land by operator: hub move first"}) // the coordinator put it there
	f.event(t, mesh.Event{From: "b", Event: "ask", Text: "sentinel"})                                 // processed after the rest
	if got := n.waitRequests(t, 1); got[0].Title != "❓ Asks • b" {
		t.Errorf("request = %+v", got[0])
	}
}

func TestPushOncePerEventAcrossRestart(t *testing.T) {
	f, n := pushSetup(t)
	f.shortDupWindow(t)
	f.lead(t, "a", "th-a", "")
	f.lead(t, "b", "th-b", "")
	stop := startPush(t, f.d)
	f.event(t, mesh.Event{From: "a", Event: "ask", Text: "q"})
	n.waitRequests(t, 1)
	stop()
	time.Sleep(300 * time.Millisecond) // past the duplicate window: only history guards now

	startPush(t, f.d)
	f.event(t, mesh.Event{From: "b", Event: "ask", Text: "sentinel"})
	got := n.waitRequests(t, 2)
	if got[0].Title != "❓ Asks • a" || got[1].Title != "❓ Asks • b" {
		t.Errorf("requests = %+v", got)
	}
	if c := f.daemonEvents(t, "push"); len(c) != 2 {
		t.Errorf("push events = %+v", c)
	}
}

func TestTwoDaemonsPushOnce(t *testing.T) {
	f, n := pushSetup(t)
	f.lead(t, "a", "th-a", "")
	f.lead(t, "b", "th-b", "")
	c2, err := mesh.Connect(f.ctx, f.c.NC.ConnectedUrl(), "", "hub", "test2", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c2.Close)
	d2 := f.d
	d2.Client, d2.Host = c2, "otherhost"
	startPush(t, f.d)
	startPush(t, d2)
	f.event(t, mesh.Event{From: "a", Event: "ask", Text: "q1"})
	f.event(t, mesh.Event{From: "b", Event: "blocked", Text: "q2"})
	got := n.waitRequests(t, 2)
	titles := map[string]int{}
	for _, r := range got {
		titles[r.Title]++
	}
	if titles["❓ Asks • a"] != 1 || titles["⛔ Blocked • b"] != 1 {
		t.Errorf("requests = %+v", got)
	}
	if c := f.claims(t, "push"); len(c) != 2 {
		t.Errorf("push claims = %+v", c)
	}
}

func TestPushCatchUpSkipsStale(t *testing.T) {
	f, n := pushSetup(t)
	f.lead(t, "a", "th-a", "")
	f.lead(t, "b", "th-b", "")
	f.lead(t, "c", "th-c", "")
	at := func(ago time.Duration) string { return f.now.Add(-ago).UTC().Format(time.RFC3339) }
	f.event(t, mesh.Event{From: "a", Event: "ask", Text: "recent", At: at(5 * time.Minute)})
	f.event(t, mesh.Event{From: "b", Event: "ask", Text: "stale", At: at(2 * time.Hour)})
	startPush(t, f.d)
	f.event(t, mesh.Event{From: "c", Event: "ask", Text: "sentinel"})
	got := n.waitRequests(t, 2)
	if got[0].Title != "❓ Asks • a" || got[1].Title != "❓ Asks • c" {
		t.Errorf("requests = %+v", got)
	}
}

// consumers counts the mesh stream's consumers.
func (f *fixture) consumers(t *testing.T) int {
	t.Helper()
	st, err := f.c.JS.Stream(f.ctx, mesh.StreamMesh)
	if err != nil {
		t.Fatal(err)
	}
	info, err := st.Info(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return info.State.Consumers
}

// stored returns the one stored message on subject.
func (f *fixture) stored(t *testing.T, subject string) mesh.Stored {
	t.Helper()
	hist, err := mesh.History(f.ctx, f.c, subject, time.Time{})
	if err != nil || len(hist) != 1 {
		t.Fatalf("%s: %d messages, %v", subject, len(hist), err)
	}
	return hist[0]
}

// A live frame stored over a minute ago (a reconnect backlog) goes through
// the catch-up rules: past catch_up it is dropped, and another daemon's
// claim for it, though out of the duplicate window, keeps it from a second
// push. A fresh live frame is pushed.
func TestPushLiveBacklogUsesCatchUpRules(t *testing.T) {
	f, n := pushSetup(t)
	f.shortDupWindow(t)
	for _, p := range []string{"a", "b", "c"} {
		f.lead(t, p, "th-"+p, "")
	}
	f.event(t, mesh.Event{From: "a", Event: "ask", Text: "claimed elsewhere"})
	a := f.stored(t, "bm.a.ask")
	if _, err := mesh.PublishEventID(f.ctx, f.c, mesh.Event{From: "daemon.otherhost", Event: "push", To: "a",
		Text: "❓ Asks • a", Seq: a.Seq}, fmt.Sprintf("push-%d", a.Seq)); err != nil {
		t.Fatal(err)
	}
	f.event(t, mesh.Event{From: "b", Event: "ask", Text: "too old", At: f.now.Add(-2 * time.Hour).UTC().Format(time.RFC3339)})
	b := f.stored(t, "bm.b.ask")
	time.Sleep(300 * time.Millisecond) // past the duplicate window: only the claims history guards a
	f.event(t, mesh.Event{From: "c", Event: "ask", Text: "fresh"})
	c := f.stored(t, "bm.c.ask")

	jobs := make(chan pushJob, 8)
	a.At = f.now.Add(-5 * time.Minute) // within catch_up, but a backlog
	b.At = f.now.Add(-5 * time.Minute) // its own at is 2 h old
	for _, s := range []mesh.Stored{a, b, c} {
		pushLive(f.ctx, f.d, s, jobs)
	}
	close(jobs)
	for j := range jobs {
		pushDeliver(f.ctx, f.ctx, f.d, j)
	}
	if got := n.waitRequests(t, 1); got[0].Title != "❓ Asks • c" {
		t.Errorf("requests = %+v", got)
	}
	if c := f.claims(t, "push"); len(c) != 2 {
		t.Errorf("push claims = %+v", c)
	}
}

// An event whose at is unparseable is judged by its stored time: stored
// more than catch_up ago, the catch-up does not push it.
func TestPushCatchUpStoredTimeFallback(t *testing.T) {
	f, n := pushSetup(t)
	f.lead(t, "a", "th-a", "")
	f.lead(t, "b", "th-b", "")
	f.event(t, mesh.Event{From: "a", Event: "ask", Text: "no usable at", At: "soon"})
	f.now = time.Now().Add(2 * time.Hour) // the event was stored 2 h ago
	base := f.consumers(t)
	startPush(t, f.d)
	// The sentinel must arrive live: by this clock the catch-up reads
	// nothing stored now. Follow's consumer is made before the catch-up's.
	deadline := time.Now().Add(10 * time.Second)
	for f.consumers(t) <= base && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	f.event(t, mesh.Event{From: "b", Event: "ask", Text: "sentinel", At: f.now.UTC().Format(time.RFC3339)})
	if got := n.waitRequests(t, 1); got[0].Title != "❓ Asks • b" {
		t.Errorf("requests = %+v", got)
	}
	// The live path falls back to the stored time the same way.
	jobs := make(chan pushJob, 1)
	pushLive(f.ctx, f.d, f.stored(t, "bm.a.ask"), jobs)
	if len(jobs) != 0 {
		t.Errorf("live frame with no usable at, stored 2 h ago: queued %+v", <-jobs)
	}
}

func TestPushFailureRecordedNotRepeated(t *testing.T) {
	f, n := pushSetup(t)
	f.shortDupWindow(t)
	f.lead(t, "a", "th-a", "")
	f.lead(t, "b", "th-b", "")
	n.setStatus(http.StatusInternalServerError)
	stop := startPush(t, f.d)
	f.event(t, mesh.Event{From: "a", Event: "ask", Text: "q"})
	n.waitRequests(t, 3) // the POST and two retries
	deadline := time.Now().Add(10 * time.Second)
	var failed []mesh.Event
	for len(failed) == 0 && time.Now().Before(deadline) {
		failed = f.daemonEvents(t, "push-failed")
		time.Sleep(20 * time.Millisecond)
	}
	if len(failed) != 1 || failed[0].To != "a" || failed[0].Text != "❓ Asks • a" || failed[0].Seq == 0 {
		t.Fatalf("push-failed events = %+v", failed)
	}
	stop()
	time.Sleep(300 * time.Millisecond) // past the duplicate window

	n.setStatus(http.StatusOK)
	startPush(t, f.d)
	f.event(t, mesh.Event{From: "b", Event: "ask", Text: "sentinel"})
	got := n.waitRequests(t, 4)
	if got[3].Title != "❓ Asks • b" {
		t.Errorf("after restart: %+v", got[3])
	}
	if c := f.daemonEvents(t, "push-failed"); len(c) != 1 {
		t.Errorf("push-failed events = %+v", c)
	}
}

// A push still retrying at shutdown is recorded as push-failed with body
// shutdown; its claim stands, so a restart does not send it.
func TestPushShutdownRecordsFailed(t *testing.T) {
	f, n := pushSetup(t)
	f.shortDupWindow(t)
	f.lead(t, "a", "th-a", "")
	f.lead(t, "b", "th-b", "")
	n.setStatus(http.StatusInternalServerError)
	f.d.PushRetry = []time.Duration{time.Minute}
	stop := startPush(t, f.d)
	f.event(t, mesh.Event{From: "a", Event: "ask", Text: "q"})
	n.waitRequests(t, 1) // the first try; the retry waits a minute
	stop()
	failed := f.daemonEvents(t, "push-failed")
	if len(failed) != 1 || failed[0].To != "a" || failed[0].Body != "shutdown" || failed[0].Seq == 0 {
		t.Fatalf("push-failed events = %+v", failed)
	}
	time.Sleep(300 * time.Millisecond) // past the duplicate window

	n.setStatus(http.StatusOK)
	startPush(t, f.d)
	f.event(t, mesh.Event{From: "b", Event: "ask", Text: "sentinel"})
	if got := n.waitRequests(t, 2); got[1].Title != "❓ Asks • b" {
		t.Errorf("after restart: %+v", got)
	}
}

func TestPushCommandGetsHeadersInEnv(t *testing.T) {
	f, n := pushSetup(t)
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-ntfy.sh"))
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "ntfy.log")
	t.Setenv("FAKE_NTFY_LOG", log)
	f.d.Cfg.Notify.Command = "sh " + fake
	f.lead(t, "a", "th-a", "")
	startPush(t, f.d)
	f.event(t, mesh.Event{From: "a", Event: "ask", Text: "which?"})
	want := "❓ Asks • a\nwhich?\nNTFY_TITLE=❓ Asks • a\nNTFY_PRIORITY=high\n" +
		"NTFY_CLICK=http://t3.test/t/th-a\n" +
		"NTFY_ACTIONS=view, Open map, http://map.test/#/lead/a\n---\n"
	var got string
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(got, "---\n") && time.Now().Before(deadline) {
		b, _ := os.ReadFile(log)
		got = string(b)
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if b, _ := os.ReadFile(log); string(b) != want {
		t.Errorf("command log =\n%q\nwant\n%q", b, want)
	}
	if r := n.requests(); len(r) != 0 {
		t.Errorf("command set, yet ntfy got %+v", r)
	}
}

func TestPushEmptyListDisables(t *testing.T) {
	f, n := pushSetup(t)
	f.lead(t, "a", "th-a", "")
	f.event(t, mesh.Event{From: "a", Event: "ask", Text: "q"})
	returns := func(d Deps) bool {
		done := make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { defer close(done); pushLoop(ctx, d) }()
		select {
		case <-done:
			return true
		case <-time.After(2 * time.Second):
			return false
		}
	}
	f.d.Cfg.Notify.Push = []string{}
	if !returns(f.d) {
		t.Error("push = []: the loop runs")
	}
	f.d.Cfg.Notify.Push = []string{"ask"}
	f.d.Cfg.Notify.Ntfy = ""
	if !returns(f.d) {
		t.Error("no ntfy and no command: the loop runs")
	}
	if r := n.requests(); len(r) != 0 {
		t.Errorf("requests = %+v", r)
	}
}

func TestNodeEventRefusesMeshInternal(t *testing.T) {
	for _, s := range []string{"bm.inbox.app", "bm.daemon.k16.started", "bm.feedback.app.x", "bm.hostreply.k16.abc", "bm.inbox.host.k16"} {
		if p, e, ok := nodeEvent(s); ok {
			t.Errorf("%s: node event %q %q", s, p, e)
		}
	}
	if p, e, ok := nodeEvent("bm.app.x.done"); !ok || p != "app.x" || e != "done" {
		t.Fatalf("bm.app.x.done: %q %q %v", p, e, ok)
	}
}

func TestBatch(t *testing.T) {
	cfg := config.NotifyConfig{Push: []string{"ask", "blocked", "done"}, MapURL: "http://map.test/"}
	lead := func(path, spec, family string) mesh.Node {
		return mesh.Node{Path: path, Role: "lead", Spec: spec, Family: family}
	}
	a, b, c := lead("demo-001", "001", "F"), lead("demo-002", "002", "F"), lead("demo-003", "003", "F")
	nodes := []mesh.Node{a, b, c, lead("x", "", ""), lead("fl", "009", "demo-fix-1"), {Path: "demo-001.e", Role: "executor"}}
	stop := mesh.State{State: "blocked", Text: mesh.StoppedBy("operator", "")}
	done := func(text string) mesh.State { return mesh.State{State: "done", Text: text} }
	ev := func(n mesh.Node, event, text string) mesh.Event {
		return mesh.Event{From: n.Path, Event: event, Text: text}
	}

	// A lead still going holds the batch.
	if _, _, ok := Batch(ev(a, "done", "x"), a, nodes, map[string]mesh.State{"demo-002": done("y"), "demo-003": {State: "working"}}, cfg); ok {
		t.Error("pushed while 003 works")
	}
	// The last lead's event ends it, its own state not yet written.
	long := "002 landed: " + strings.Repeat("word ", 40)
	req, key, ok := Batch(ev(a, "done", "001 landed:  a\nb"), a, nodes, map[string]mesh.State{"demo-001": {State: "landing"}, "demo-002": done(long), "demo-003": stop}, cfg)
	if !ok || key != "push.batch.F.3.2" || req.Title != "✅ Landed • F: 2/3" || req.Priority != "default" || req.Click != "http://map.test/" {
		t.Fatalf("%+v %q %v", req, key, ok)
	}
	if want := "001 002; stopped 003\n001 landed: a b\n" + cutRunes(strings.TrimSpace(strings.Join(strings.Fields(long), " ")), batchLineMax); req.Body != want {
		t.Errorf("body\n%s\nwant\n%s", req.Body, want)
	}
	if line := strings.Split(req.Body, "\n")[2]; utf8.RuneCountInString(line) != batchLineMax || !strings.HasSuffix(line, "…") {
		t.Errorf("long line %q", line)
	}
	// Every lead stopped: the operator did it, nothing pushes.
	if req, _, ok := Batch(ev(c, "blocked", mesh.StoppedBy("operator", "")), c, nodes, map[string]mesh.State{"demo-001": stop, "demo-002": stop}, cfg); ok {
		t.Errorf("all stopped pushed: %+v", req)
	}
	// The stopped 003 woken and landed ends the batch again, under a key of its own.
	if req, key, ok := Batch(ev(c, "done", "003 landed"), c, nodes, map[string]mesh.State{"demo-001": done("a"), "demo-002": done("b")}, cfg); !ok ||
		key != "push.batch.F.3.3" || req.Title != "✅ Landed • F: 3/3" {
		t.Errorf("ended again: %+v %q %v", req, key, ok)
	}
	// A lead with no family is a batch of one.
	if req, key, ok := Batch(ev(nodes[3], "done", "shipped"), nodes[3], nodes, nil, cfg); !ok || req.Title != "✅ Landed • x" || req.Body != "x: shipped" || key != "push.batch.x.1.1" {
		t.Errorf("one: %+v %q %v", req, key, ok)
	}
	// A blocked that isn't a stop, a fix lead and an unlisted done end nothing.
	if _, _, ok := Batch(ev(nodes[3], "blocked", "no key"), nodes[3], nodes, nil, cfg); ok {
		t.Error("a block ended a batch")
	}
	if _, _, ok := Batch(ev(nodes[4], "done", "x"), nodes[4], nodes, nil, cfg); ok {
		t.Error("a fix lead's done pushed")
	}
	cfg.Push = []string{"ask", "blocked"}
	if _, _, ok := Batch(ev(nodes[3], "done", "x"), nodes[3], nodes, nil, cfg); ok {
		t.Error("pushed with done unlisted")
	}
}

// A batch pushes once, when its last lead ends: never a lead's own done,
// and once though two daemons follow it.
func TestPushBatchEndOnce(t *testing.T) {
	f, n := pushSetup(t)
	for _, p := range []string{"demo-001", "demo-002"} {
		if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: p, Role: "lead", Spec: strings.TrimPrefix(p, "demo-"), Family: "demo-build-1", Host: testHost}); err != nil {
			t.Fatal(err)
		}
		f.setState(t, p, "working")
	}
	other := f.d
	other.Host = "box-b"
	startPush(t, f.d)
	startPush(t, other)
	f.event(t, mesh.Event{From: "demo-001", Event: "done", Text: "001 landed: a"})
	f.setState(t, "demo-001", "done")
	time.Sleep(300 * time.Millisecond)
	if r := n.requests(); len(r) != 0 {
		t.Fatalf("a lead's own done pushed: %+v", r)
	}
	f.event(t, mesh.Event{From: "demo-002", Event: "done", Text: "002 landed: b"})
	got := n.waitRequests(t, 1)
	if got[0].Title != "✅ Landed • demo-build-1: 2/2" || !strings.HasPrefix(got[0].Body, "001 002\n") {
		t.Fatalf("%+v", got)
	}
}

// A stop ends a batch though blocked isn't listed: done is.
func TestPushBatchEndedByStopWithDoneOnly(t *testing.T) {
	f, n := pushSetup(t)
	f.d.Cfg.Notify.Push = []string{"done"}
	for _, p := range []string{"demo-001", "demo-002"} {
		if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: p, Role: "lead", Spec: strings.TrimPrefix(p, "demo-"), Family: "demo-build-1", Host: testHost}); err != nil {
			t.Fatal(err)
		}
	}
	f.setState(t, "demo-001", "done")
	f.setState(t, "demo-002", "working")
	startPush(t, f.d)
	time.Sleep(200 * time.Millisecond)
	f.event(t, mesh.Event{From: "demo-002", Event: "blocked", Text: mesh.StoppedBy("operator", "")})
	if got := n.waitRequests(t, 1); got[0].Title != "✅ Landed • demo-build-1: 1/2" || !strings.HasPrefix(got[0].Body, "001; stopped 002\n") {
		t.Fatalf("%+v", got)
	}
}
