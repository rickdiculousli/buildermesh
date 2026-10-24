package daemon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func TestRemindOncePerWeekMarkAcrossDaemons(t *testing.T) {
	f := setup(t)
	rec := &ntfyRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	f.now = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	f.d.Cfg.Notify.Ntfy = srv.URL + "/bmesh"
	f.d.Cfg.Quota.RemindTopic = "quota-reset"
	reading := func(week, jitter string) string {
		return `[{"reading":{"provider":"claude","five_hour":12,"week":` + week + `,"five_reset":"2026-10-16T11:00:00Z","week_reset":"2026-10-20T00:00:` + jitter + `"}},
			{"reading":{"provider":"codex","five_hour":40,"week":3,"five_reset":"2026-10-16T12:00:00Z","week_reset":"2026-10-20T00:00:00Z"}}]`
	}
	// Two daemons read the same week, Claude's reset jittering in the minute.
	pass := func(week string) {
		a, b := f.d, f.d
		a.Host, b.Host = "box-a", "box-b"
		remindOnce(f.ctx, a, &web.Quota{Command: quotaCommand(t, reading(week, "00.734Z"))})
		remindOnce(f.ctx, b, &web.Quota{Command: quotaCommand(t, reading(week, "29.2Z"))})
		f.now = f.now.Add(time.Hour)
	}
	pass("41")
	if len(rec.posts) != 0 {
		t.Fatalf("posts at 41%% used = %d", len(rec.posts))
	}
	pass("55")
	pass("60")
	pass("80")
	pass("80")
	if len(rec.posts) != 2 {
		t.Fatalf("posts = %d, want one at 50%% used and one at 75%%, across two daemons", len(rec.posts))
	}
	for i, want := range []struct{ title, body string }{
		{"🔋 50% left • Claude weekly", "resets in 3d 14h"},
		{"🪫 25% left • Claude weekly", "resets in 3d 12h"},
	} {
		p := rec.posts[i]
		if p.URL.Path != "/quota-reset" || p.Header.Get("At") != "" || p.Header.Get("Title") != want.title || rec.body[i] != want.body {
			t.Errorf("post %d: %s At=%q Title=%q body %q", i, p.URL.Path, p.Header.Get("At"), p.Header.Get("Title"), rec.body[i])
		}
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
	q := &web.Quota{Command: quotaCommand(t, `[{"reading":{"provider":"claude","five_hour":5,"week":60,"five_reset":"2026-10-16T11:00:00Z","week_reset":"2026-10-20T00:00:00Z"}}]`)}
	remindOnce(f.ctx, f.d, q)
	remindOnce(f.ctx, f.d, q)
	remindOnce(f.ctx, f.d, q)
	if len(rec.posts) != 2 {
		t.Fatalf("posts = %d, want the failed one and its retry", len(rec.posts))
	}
}

func TestRemindersWeekMarks(t *testing.T) {
	now := time.Date(2026, 10, 16, 11, 0, 0, 0, time.UTC)
	rs := Reminders([]web.Reading{
		{Provider: "claude", Week: 99, WeekReset: "2026-10-16T10:59:40Z"},  // rounds to now: past
		{Provider: "codex", Week: 49.9, WeekReset: "2026-10-18T13:00:00Z"}, // under every mark
		{Provider: "bad.name", Week: 80, WeekReset: "2026-10-18T13:00:00Z"},
		{Provider: "codex", Week: 80, WeekReset: ""},
		{Provider: "a", Week: 50, WeekReset: "2026-10-18T13:00:31Z"},
		{Provider: "b", Week: 75, WeekReset: "2026-10-16T13:05:00Z"},
		{Provider: "c", Week: 97, WeekReset: "2026-10-16T11:25:00Z"},
	}, now)
	var got []string
	for _, r := range rs {
		got = append(got, r.Key()+" | "+r.Title+" | "+r.Body)
	}
	unix := func(h, m int) string {
		return strconv.FormatInt(time.Date(2026, 10, 16, h, m, 0, 0, time.UTC).Unix(), 10)
	}
	want := []string{
		"quota.a.week." + strconv.FormatInt(time.Date(2026, 10, 18, 13, 1, 0, 0, time.UTC).Unix(), 10) + ".50 | 🔋 50% left • A weekly | resets in 2d 2h",
		"quota.b.week." + unix(13, 5) + ".75 | 🪫 25% left • B weekly | resets in 2h 5m",
		"quota.c.week." + unix(11, 25) + ".90 | 🪫 10% left • C weekly | resets in 25m",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
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
