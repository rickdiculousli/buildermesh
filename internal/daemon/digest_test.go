package daemon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

func TestDigestTextAndChanged(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	at := func(ago time.Duration) string { return now.Add(-ago).Format(time.RFC3339) }
	states := map[string]mesh.State{
		"a":     {State: "working", At: at(time.Minute)},
		"a.b":   {State: "asking", At: at(5 * time.Minute), Text: "which db?"},
		"a.c":   {State: "silent", At: at(2*time.Hour + 10*time.Minute)},
		"a.d":   {State: "done", At: at(time.Minute)},
		"a.e":   {State: "blocked", At: at(20 * time.Minute)},
		"a.old": {State: "done", At: at(3 * time.Hour)},
	}
	since := now.Add(-time.Hour)
	title, body, changed := Digest("h", states, nil, since, now)
	if title != "bmesh h: 1 working, 1 asking, 1 silent" {
		t.Errorf("title = %q", title)
	}
	if !changed {
		t.Error("changed = false")
	}
	for _, want := range []string{"a.b: asking since 5m: which db?", "a.e: blocked since 20m\n", "a.d: done"} {
		if !strings.Contains(body+"\n", want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	// a.c has been silent since before the window: posted then, not again.
	if strings.Contains(body, "a.old") || strings.Contains(body, "a: ") || strings.Contains(body, "a.c") {
		t.Errorf("body has unrelated lines:\n%s", body)
	}
	// A window that holds a.c's At lists it, with the hours form of the age.
	if _, body, _ := Digest("h", states, nil, now.Add(-3*time.Hour), now); !strings.Contains(body+"\n", "a.c: silent since 2h10m\n") {
		t.Errorf("wide window missing a.c:\n%s", body)
	}

	calm := map[string]mesh.State{"a": {State: "working", At: at(time.Minute)}, "b": {State: "idle"}}
	if _, _, changed := Digest("h", calm, nil, since, now); changed {
		t.Error("all working: changed = true")
	}
	standing := map[string]mesh.State{"x": {State: "asking", At: at(2 * time.Hour)}}
	if _, _, changed := Digest("h", standing, nil, since, now); changed {
		t.Error("asking since before the window: changed = true")
	}
}

func TestDigestLoopPostsOnlyWhenChanged(t *testing.T) {
	f := setup(t)
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-ntfy.sh"))
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "ntfy.log")
	t.Setenv("FAKE_NTFY_LOG", log)
	f.d.Cfg.Notify.Command = "sh " + fake
	read := func() string {
		b, err := os.ReadFile(log)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return string(b)
	}
	records := func() int { return strings.Count(read(), "---\n") }
	stamp := func(path, state string) {
		t.Helper()
		if _, err := mesh.WriteState(f.ctx, f.c, path, mesh.State{State: state, By: "node",
			At: f.now.UTC().Format(time.RFC3339)}); err != nil {
			t.Fatal(err)
		}
	}

	f.lead(t, "a", "t1", "asking")
	f.now = f.now.Add(time.Minute) // the state's At (wall clock, whole seconds) is inside the window
	since := f.now.Add(-time.Hour)
	digestOnce(f.ctx, f.d, &since)
	if n := records(); n != 1 {
		t.Fatalf("records = %d, want 1", n)
	}
	if b := read(); !strings.HasPrefix(b, "bmesh "+testHost+": 0 working, 1 asking, 0 silent\n") {
		t.Errorf("log = %q", b)
	}
	if evs := f.daemonEvents(t, "digest"); len(evs) != 1 || evs[0].From != "daemon."+testHost {
		t.Errorf("digest events = %+v", evs)
	}
	if !since.Equal(f.now) {
		t.Errorf("since = %v, want %v", since, f.now)
	}

	// a is still asking: a standing state was posted once and is not news.
	f.now = f.now.Add(time.Minute)
	digestOnce(f.ctx, f.d, &since)
	if n := records(); n != 1 {
		t.Fatalf("records after standing tick = %d, want 1", n)
	}

	// b enters blocked after since: posted, and a is not repeated.
	f.now = f.now.Add(time.Minute)
	f.lead(t, "b", "t2", "working")
	stamp("b", "blocked")
	digestOnce(f.ctx, f.d, &since)
	if n := records(); n != 2 {
		t.Fatalf("records after blocked = %d, want 2", n)
	}
	parts := strings.Split(read(), "---\n")
	last := parts[len(parts)-2]
	if !strings.Contains(last, "b: blocked since 0m") || strings.Contains(last, "a: asking") {
		t.Errorf("blocked post = %q", last)
	}

	f.now = f.now.Add(time.Minute)
	stamp("b", "done")
	digestOnce(f.ctx, f.d, &since)
	if n := records(); n != 3 {
		t.Fatalf("records after done = %d, want 3", n)
	}
}

func TestDigestDoneWindow(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	since := now.Add(-time.Hour)
	st := func(at time.Time) map[string]mesh.State {
		return map[string]mesh.State{"x": {State: "done", At: at.Format(time.RFC3339)}}
	}
	if _, _, ch := Digest("h", st(since), nil, since, now); ch {
		t.Error("done exactly at since was reported again")
	}
	if _, _, ch := Digest("h", st(now), nil, since, now); !ch {
		t.Error("done at now was not reported")
	}
	if _, _, ch := Digest("h", st(now.Add(time.Second)), nil, since, now); ch {
		t.Error("done after now was reported early")
	}
	// Next pass: since = old now, so the late done is reported and the one at old now is not.
	next := now.Add(time.Hour)
	if _, _, ch := Digest("h", st(now), nil, now, next); ch {
		t.Error("done at the previous now was reported twice")
	}
	if _, _, ch := Digest("h", st(now.Add(time.Second)), nil, now, next); !ch {
		t.Error("done just after the previous now was lost")
	}
}

func TestDigestFailedDeliveryKeepsSince(t *testing.T) {
	f := setup(t)
	f.lead(t, "a", "t1", "asking")
	f.now = f.now.Add(time.Minute) // the state's At is inside the window
	orig := f.now.Add(-time.Hour)

	f.d.Cfg.Notify.Command = "exit 1"
	since := orig
	digestOnce(f.ctx, f.d, &since)
	if !since.Equal(orig) {
		t.Errorf("command failure moved since to %v", since)
	}
	if evs := f.daemonEvents(t, "digest"); len(evs) != 0 {
		t.Errorf("event published after failed delivery: %+v", evs)
	}

	var gotBody, gotTitle string
	status := http.StatusInternalServerError
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotTitle = string(b), r.Header.Get("Title")
		w.WriteHeader(status)
	}))
	defer srv.Close()
	f.d.Cfg.Notify.Command = ""
	f.d.Cfg.Notify.Ntfy = srv.URL
	digestOnce(f.ctx, f.d, &since)
	if !since.Equal(orig) {
		t.Errorf("non-2xx moved since to %v", since)
	}
	if gotTitle != "bmesh "+testHost+": 0 working, 1 asking, 0 silent" || !strings.HasPrefix(gotBody, "a: asking since") {
		t.Errorf("request: title %q body %q", gotTitle, gotBody)
	}
	if err := deliver(f.ctx, f.d, "t", "b"); err == nil {
		t.Error("deliver: non-2xx returned nil")
	}

	status = http.StatusOK
	digestOnce(f.ctx, f.d, &since)
	if !since.Equal(f.now) {
		t.Errorf("success left since at %v", since)
	}
}
