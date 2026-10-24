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
	"github.com/rickdiculousli/buildermesh/internal/notify"
)

// The Stuck report names each stuck thing once, in the pass whose window
// holds the moment it became stuck, and nothing else.
func TestStuckNamesEachThingOnce(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	at := func(ago time.Duration) string { return now.Add(-ago).Format(time.RFC3339) }
	since := now.Add(-time.Hour)
	nodes := []mesh.Node{
		{Path: "a", Role: "lead"}, {Path: "a.x", Role: "executor"}, {Path: "a.y", Role: "executor"},
		{Path: "b", Role: "lead"}, {Path: "c", Role: "lead"}, {Path: "d", Role: "lead"}, {Path: "e", Role: "lead"},
		{Path: "old", Role: "lead"}, {Path: "fl", Role: "lead", Family: "demo-fix-1"},
		{Path: "f1a", Role: "lead", Family: "demo-build-1"}, {Path: "f1b", Role: "lead", Family: "demo-build-1"},
		{Path: "f2a", Role: "lead", Family: "demo-build-2"}, {Path: "f3a", Role: "lead", Family: "demo-build-3"},
	}
	states := map[string]mesh.State{
		"a":                  {State: "working", At: at(30 * time.Minute)},
		"a.x":                {State: "silent", At: at(5 * time.Minute), Text: "quiet for 25m"},
		"a.y":                {State: "asking", At: at(70 * time.Minute), Text: "the lead's to answer"},
		"b":                  {State: "asking", At: at(70 * time.Minute), Text: "which db?"},
		"c":                  {State: "asking", At: at(30 * time.Minute), Text: "not yet an hour"},
		"d":                  {State: "blocked", At: at(90 * time.Minute), Text: "no key"},
		"e":                  {State: "blocked", At: at(90 * time.Minute), Text: mesh.StoppedBy("operator", "")},
		"old":                {State: "asking", At: at(3 * time.Hour), Text: "reminded before"},
		"fl":                 {State: "asking", At: at(70 * time.Minute), Text: "the fixer's to answer"},
		"f1a":                {State: "done", At: at(65 * time.Minute)},
		"f1b":                {State: "blocked", At: at(80 * time.Minute), Text: mesh.StoppedBy("operator", "")},
		"f2a":                {State: "working", At: at(80 * time.Minute)},
		"f3a":                {State: "done", At: at(30 * time.Minute)},
		"routine.verify.c-1": {State: "silent", At: at(time.Minute)},
	}
	held := now.Add(-24*time.Hour - 30*time.Minute)
	holds := []mesh.LeadHold{
		{Path: "a-001", Gate: "land", Text: "hub move first", By: "operator", At: held.Format(time.RFC3339)},
		{Path: "a-002", Gate: "now", Text: "fresh", By: "operator", At: at(4 * time.Hour)},
	}
	feedback := map[string]int{"demo-build-1": 2, "demo-build-2": 1, "demo-build-3": 1}
	title, body, changed := Stuck("h", states, nodes, holds, feedback, since, now)
	if !changed || title != "👀 Stuck • h: a.x silent, +4 more" {
		t.Errorf("title %q changed %v", title, changed)
	}
	want := "a.x silent: quiet for 25m\n" +
		"b asks, open 1h10m: which db?\n" +
		"d blocked 1h30m: no key\n" +
		"a-001: held before land by operator 1d0h ago: hub move first\n" +
		"demo-build-1: 2 feedback pending 1h5m after it finished: bmesh feedback list demo-build-1"
	if body != want {
		t.Errorf("body =\n%s\nwant\n%s", body, want)
	}
	// The next pass names none of them again, only c's ask and the
	// feedback of demo-build-3, whose hour comes in it.
	if _, body, _ := Stuck("h", states, nodes, holds, feedback, now, now.Add(time.Hour)); body !=
		"c asks, open 1h30m: not yet an hour\ndemo-build-3: 1 feedback pending 1h30m after it finished: bmesh feedback list demo-build-3" {
		t.Errorf("next pass:\n%s", body)
	}
	// One item has no "+N more".
	one := map[string]mesh.State{"a": {State: "silent", At: at(time.Minute)}}
	if title, _, _ := Stuck("h", one, nil, nil, nil, since, now); title != "👀 Stuck • h: a silent" {
		t.Errorf("one item: %q", title)
	}
}

// The window is (since, now]: a mark at since was named last pass, one
// after now waits for the next.
func TestStuckWindow(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	since := now.Add(-time.Hour)
	st := func(at time.Time) map[string]mesh.State {
		return map[string]mesh.State{"x": {State: "silent", At: at.Format(time.RFC3339)}}
	}
	if _, _, ch := Stuck("h", st(since), nil, nil, nil, since, now); ch {
		t.Error("silent exactly at since was named again")
	}
	if _, _, ch := Stuck("h", st(now), nil, nil, nil, since, now); !ch {
		t.Error("silent at now was not named")
	}
	if _, _, ch := Stuck("h", st(now.Add(time.Second)), nil, nil, nil, since, now); ch {
		t.Error("silent after now was named early")
	}
	if _, _, ch := Stuck("h", st(now.Add(time.Second)), nil, nil, nil, now, now.Add(time.Hour)); !ch {
		t.Error("silent just after the previous now was lost")
	}
}

func TestDigestLoopPostsOnlyWhenStuck(t *testing.T) {
	f := fixerSetup(t, "", &starts{}) // bm is bmesh's own repo
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

	f.lead(t, "a", "t1", "working")
	stamp("a", "asking")
	// A fresh ask is its own push's: the report is quiet.
	since := f.now.Add(-time.Hour)
	digestOnce(f.ctx, f.d, &since)
	if n := records(); n != 0 {
		t.Fatalf("records = %d, want 0", n)
	}
	// An hour on, it is named once.
	f.now = f.now.Add(time.Hour + time.Minute)
	digestOnce(f.ctx, f.d, &since)
	if n := records(); n != 1 {
		t.Fatalf("records = %d, want 1", n)
	}
	if b := read(); !strings.HasPrefix(b, "👀 Stuck • "+testHost+": a ask open 1h1m\na asks, open 1h1m\n") {
		t.Errorf("log = %q", b)
	}
	if evs := f.daemonEvents(t, "digest"); len(evs) != 1 || evs[0].From != "daemon."+testHost || evs[0].Body != "a asks, open 1h1m" {
		t.Errorf("digest events = %+v", evs)
	}
	if !since.Equal(f.now) {
		t.Errorf("since = %v, want %v", since, f.now)
	}
	f.now = f.now.Add(time.Hour)
	digestOnce(f.ctx, f.d, &since)
	if n := records(); n != 1 {
		t.Fatalf("records after a standing ask = %d, want 1", n)
	}

	// A finished family's feedback is named an hour on; bmesh's own repo's
	// is the fixer's and left out.
	if err := mesh.PutNode(f.ctx, f.c, mesh.Node{Path: "b", Role: "lead", Host: testHost, Family: "demo-build-1"}); err != nil {
		t.Fatal(err)
	}
	stamp("b", "working")
	stamp("b", "done")
	for _, repo := range []string{"bm", "demo"} {
		if _, err := mesh.PublishFeedback(f.ctx, f.c, mesh.Feedback{From: "b", Family: "demo-build-1", Repo: repo, Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	f.now = f.now.Add(time.Hour + time.Minute)
	digestOnce(f.ctx, f.d, &since)
	parts := strings.Split(read(), "---\n")
	if last := parts[len(parts)-2]; !strings.Contains(last, "demo-build-1: 1 feedback pending 1h1m after it finished") {
		t.Errorf("feedback post = %q", last)
	}
}

func TestDigestFailedDeliveryKeepsSince(t *testing.T) {
	f := setup(t)
	f.lead(t, "a", "t1", "silent")
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
	if gotTitle != "👀 Stuck • "+testHost+": a silent" || gotBody != "a silent" {
		t.Errorf("request: title %q body %q", gotTitle, gotBody)
	}
	if err := deliver(f.ctx, f.d, notify.Headers{Title: "t"}, "b"); err == nil {
		t.Error("deliver: non-2xx returned nil")
	}

	status = http.StatusOK
	digestOnce(f.ctx, f.d, &since)
	if !since.Equal(f.now) {
		t.Errorf("success left since at %v", since)
	}
}
