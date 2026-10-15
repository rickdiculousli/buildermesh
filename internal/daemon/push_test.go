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

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// ntfyReq is one request the fake ntfy server received.
type ntfyReq struct {
	Method, Title, Priority, Tags, Click, Actions, Body string
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
			Tags: h.Get("Tags"), Click: h.Get("Click"), Actions: h.Get("Actions"), Body: string(b)})
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
	want := []ntfyReq{
		{"POST", "a asks", "high", "question", "http://map.test/#/lead/a", "view, Open thread, http://t3.test/t/th-a", "which colour?"},
		{"POST", "a is blocked", "high", "no_entry", "http://map.test/#/lead/a", "view, Open thread, http://t3.test/t/th-a", "no creds"},
		{"POST", "a is done", "default", "white_check_mark", "http://map.test/#/lead/a", "view, Open thread, http://t3.test/t/th-a", "shipped\ncommit 01234567"},
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
	if !ok || req.Title != "a asks" || req.Click != "" || req.Actions != "" || req.Body != "q" {
		t.Errorf("no map_url, no console: %+v %v", req, ok)
	}
	cfg.MapURL = "http://map.test"
	if req, _ := Push(mesh.Event{From: "a", Event: "done", Text: "x"}, lead, cfg, ""); req.Click != "http://map.test#/lead/a" || req.Body != "x" {
		t.Errorf("done without commit: %+v", req)
	}
	if _, ok := Push(mesh.Event{From: "a.x", Event: "ask"}, mesh.Node{Path: "a.x", Role: "executor"}, cfg, ""); ok {
		t.Error("executor pushed")
	}
	if _, ok := Push(mesh.Event{From: "a", Event: "progress"}, lead, cfg, ""); ok {
		t.Error("progress pushed")
	}
	cfg.Push = []string{"done"}
	if _, ok := Push(mesh.Event{From: "a", Event: "ask"}, lead, cfg, ""); ok {
		t.Error("ask pushed though not listed")
	}
	// The text is cut to 3 KB on a rune boundary.
	long := strings.Repeat("é", 2000) // 4000 bytes
	req, _ = Push(mesh.Event{From: "a", Event: "done", Text: long, Commit: "abc"}, lead, cfg, "")
	text, commit, _ := strings.Cut(req.Body, "\n")
	if len(text) > 3<<10 || len(text) < 3<<10-1 || !strings.HasPrefix(long, text) || commit != "commit abc" {
		t.Errorf("cut body: %d bytes, commit line %q", len(text), commit)
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
	f.event(t, mesh.Event{From: "b", Event: "ask", Text: "sentinel"}) // processed after the rest
	if got := n.waitRequests(t, 1); got[0].Title != "b asks" {
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
	if got[0].Title != "a asks" || got[1].Title != "b asks" {
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
	if titles["a asks"] != 1 || titles["b is blocked"] != 1 {
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
	if got[0].Title != "a asks" || got[1].Title != "c asks" {
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
		Text: "a asks", Seq: a.Seq}, fmt.Sprintf("push-%d", a.Seq)); err != nil {
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
	if got := n.waitRequests(t, 1); got[0].Title != "c asks" {
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
	if got := n.waitRequests(t, 1); got[0].Title != "b asks" {
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
	if len(failed) != 1 || failed[0].To != "a" || failed[0].Text != "a asks" || failed[0].Seq == 0 {
		t.Fatalf("push-failed events = %+v", failed)
	}
	stop()
	time.Sleep(300 * time.Millisecond) // past the duplicate window

	n.setStatus(http.StatusOK)
	startPush(t, f.d)
	f.event(t, mesh.Event{From: "b", Event: "ask", Text: "sentinel"})
	got := n.waitRequests(t, 4)
	if got[3].Title != "b asks" {
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
	if got := n.waitRequests(t, 2); got[1].Title != "b asks" {
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
	f.event(t, mesh.Event{From: "a", Event: "done", Text: "shipped", Commit: "0123456789abcdef"})
	want := "a is done\nshipped\ncommit 01234567\nNTFY_TITLE=a is done\nNTFY_PRIORITY=default\n" +
		"NTFY_TAGS=white_check_mark\nNTFY_CLICK=http://map.test/#/lead/a\n" +
		"NTFY_ACTIONS=view, Open thread, http://t3.test/t/th-a\n---\n"
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
