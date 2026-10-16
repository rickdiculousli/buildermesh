package daemon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/web"
)

// ntfyRecorder is an ntfy server that records each post and answers with
// the next of codes (200 once they run out).
type ntfyRecorder struct {
	mu    sync.Mutex
	posts []*http.Request
	body  []string
	codes []int
}

func (n *ntfyRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	n.mu.Lock()
	defer n.mu.Unlock()
	n.posts = append(n.posts, r)
	n.body = append(n.body, string(b))
	if len(n.codes) > 0 {
		code := n.codes[0]
		n.codes = n.codes[1:]
		w.WriteHeader(code)
	}
}

// quotaCommand writes readings to a file and returns the command that
// prints them.
func quotaCommand(t *testing.T, readings string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "quota.json")
	if err := os.WriteFile(p, []byte(readings), 0o644); err != nil {
		t.Fatal(err)
	}
	return "cat " + p
}

func TestRemindOncePerWindowAcrossDaemons(t *testing.T) {
	f := setup(t)
	rec := &ntfyRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	f.now = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	f.d.Cfg.Notify.Ntfy = srv.URL + "/bmesh"
	f.d.Cfg.Quota.RemindTopic = "quota-reset"

	// Two daemons read the same window a few seconds apart: Claude's reset
	// jitters across the minute, codex has no usage.
	a, b := f.d, f.d
	a.Host, b.Host = "box-a", "box-b"
	qa := &web.Quota{Command: quotaCommand(t, `[{"reading":{"provider":"claude","five_hour":12,"week":41,"five_reset":"2026-10-16T10:59:59.734Z","week_reset":"2026-10-20T00:00:00Z"}},
		{"reading":{"provider":"codex","five_hour":0,"week":3,"five_reset":"2026-10-16T12:00:00Z","week_reset":"2026-10-20T00:00:00Z"}}]`)}
	qb := &web.Quota{Command: quotaCommand(t, `[{"reading":{"provider":"claude","five_hour":13,"week":41,"five_reset":"2026-10-16T11:00:00.212+00:00","week_reset":"2026-10-20T00:00:00Z"}}]`)}
	remindOnce(f.ctx, a, qa)
	remindOnce(f.ctx, b, qb)
	f.now = f.now.Add(time.Hour)
	remindOnce(f.ctx, a, qa)
	remindOnce(f.ctx, b, qb)

	if len(rec.posts) != 1 {
		t.Fatalf("posts = %d, want 1 across two daemons and two readings", len(rec.posts))
	}
	p := rec.posts[0]
	reset := time.Date(2026, 10, 16, 11, 0, 0, 0, time.UTC)
	if p.URL.Path != "/quota-reset" || p.Header.Get("At") != strconv.FormatInt(reset.Unix(), 10) ||
		p.Header.Get("Title") != "Claude 5-hour quota reset" || p.Header.Get("Tags") != "hourglass" {
		t.Fatalf("post %s At=%s Title=%q Tags=%q", p.URL.Path, p.Header.Get("At"), p.Header.Get("Title"), p.Header.Get("Tags"))
	}
	if rec.body[0] != "Claude's 5-hour window is fresh. Weekly was 41% used." {
		t.Fatalf("body %q", rec.body[0])
	}
}

func TestRemindFailedPostGivesClaimBack(t *testing.T) {
	f := setup(t)
	rec := &ntfyRecorder{codes: []int{http.StatusInternalServerError}}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	f.now = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	f.d.Cfg.Notify.Ntfy = srv.URL + "/bmesh"
	f.d.Cfg.Quota.RemindTopic = "quota-reset"
	q := &web.Quota{Command: quotaCommand(t, `[{"reading":{"provider":"claude","five_hour":5,"week":1,"five_reset":"2026-10-16T11:00:00Z","week_reset":"2026-10-20T00:00:00Z"}}]`)}
	remindOnce(f.ctx, f.d, q)
	remindOnce(f.ctx, f.d, q)
	remindOnce(f.ctx, f.d, q)
	if len(rec.posts) != 2 {
		t.Fatalf("posts = %d, want the failed one and its retry", len(rec.posts))
	}
}

func TestRemindersSkipUnusedAndPast(t *testing.T) {
	now := time.Date(2026, 10, 16, 11, 0, 0, 0, time.UTC)
	rs := Reminders([]web.Reading{
		{Provider: "claude", FiveHour: 10, FiveReset: "2026-10-16T10:59:40Z"}, // rounds to now: past
		{Provider: "codex", FiveHour: 0, FiveReset: "2026-10-16T13:00:00Z"},   // unused
		{Provider: "bad.name", FiveHour: 10, FiveReset: "2026-10-16T13:00:00Z"},
		{Provider: "codex", FiveHour: 1, FiveReset: ""},
		{Provider: "codex", FiveHour: 1, Week: 9.6, FiveReset: "2026-10-16T13:00:31Z"},
	}, now)
	if len(rs) != 1 || rs[0].Key() != "quota.codex."+strconv.FormatInt(time.Date(2026, 10, 16, 13, 1, 0, 0, time.UTC).Unix(), 10) ||
		rs[0].Body != "Codex's 5-hour window is fresh. Weekly was 10% used." {
		t.Fatalf("%+v", rs)
	}
}

func TestTopicURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://127.0.0.1:2586/bmesh":   "http://127.0.0.1:2586/quota-reset",
		"http://k16:2586/bmesh/":        "http://k16:2586/quota-reset",
		"https://ntfy.sh/a/b?auth=x":    "https://ntfy.sh/a/quota-reset?auth=x",
		"http://k16:2586":               "http://k16:2586/quota-reset",
		"https://user:pw@ntfy.sh/bmesh": "https://user:pw@ntfy.sh/quota-reset",
	} {
		if got, err := TopicURL(in, "quota-reset"); err != nil || got != want {
			t.Errorf("TopicURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := TopicURL("k16/bmesh", "t"); err == nil {
		t.Error("a URL without a scheme is an error")
	}
}
