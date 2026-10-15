package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

// quotaOK is one reading in the quota command's output format.
const quotaOK = `[{"reading":{"provider":"claude","five_hour":42.5,"week":17,` +
	`"five_reset":"2026-10-15T10:59:59.986712+00:00","week_reset":"2026-10-20T08:00:00+02:00"}}]`

type strip struct {
	Quota  []Reading `json:"quota"`
	Digest *struct {
		At    string `json:"at"`
		Title string `json:"title"`
		Body  string `json:"body"`
	} `json:"digest"`
}

func TestQuotaKeepsLastReadingOnError(t *testing.T) {
	ctx, c := connect(t)
	counter := filepath.Join(t.TempDir(), "runs")
	// Succeeds on the first run, exits 1 after.
	cmd := `n=$(cat ` + counter + ` 2>/dev/null || echo 0); echo $((n+1)) > ` + counter + `; ` +
		`[ "$n" -eq 0 ] || exit 1; echo '` + quotaOK + `'`
	runs := func() string {
		b, _ := os.ReadFile(counter)
		return strings.TrimSpace(string(b))
	}
	start := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	now := start
	cfg := &config.Config{Quota: config.QuotaConfig{Command: cmd}}
	srv := httptest.NewServer(Handler(c, time.Hour, Options{Cfg: cfg, Host: "box", Now: func() time.Time { return now }}))
	defer srv.Close()

	var s strip
	getJSON(t, srv, "/api/strip", 200, &s)
	want := Reading{Provider: "claude", FiveHour: 42.5, Week: 17, FiveReset: "2026-10-15T10:59:59Z",
		WeekReset: "2026-10-20T06:00:00Z", ReadAt: "2026-10-15T09:00:00Z"}
	if len(s.Quota) != 1 || s.Quota[0] != want || s.Digest != nil {
		t.Fatalf("first strip %+v", s)
	}
	now = start.Add(time.Minute) // inside 5 minutes: no run
	getJSON(t, srv, "/api/strip", 200, &s)
	if runs() != "1" || len(s.Quota) != 1 || s.Quota[0] != want {
		t.Fatalf("cached strip %+v, runs %s", s, runs())
	}
	now = start.Add(6 * time.Minute) // the command fails: the good reading stays
	getJSON(t, srv, "/api/strip", 200, &s)
	if runs() != "2" || len(s.Quota) != 1 || s.Quota[0] != want {
		t.Fatalf("strip after a failure %+v, runs %s", s, runs())
	}
	now = start.Add(8 * time.Minute) // the failure counts as a poll: no run yet
	getJSON(t, srv, "/api/strip", 200, &s)
	if runs() != "2" || len(s.Quota) != 1 || s.Quota[0].ReadAt != want.ReadAt {
		t.Fatalf("strip %+v, runs %s", s, runs())
	}

	for _, ev := range []mesh.Event{
		{From: "daemon.box", Event: "digest", Text: "old", Body: "a: done"},
		{From: "daemon.other", Event: "digest", Text: "bmesh other: 1 working", Body: "b: asking since 5m", At: "2026-10-15T09:05:00Z"},
	} {
		if err := mesh.PublishEvent(ctx, c, ev); err != nil {
			t.Fatal(err)
		}
	}
	getJSON(t, srv, "/api/strip", 200, &s)
	if s.Digest == nil || s.Digest.Title != "bmesh other: 1 working" || s.Digest.Body != "b: asking since 5m" ||
		s.Digest.At != "2026-10-15T09:05:00Z" {
		t.Fatalf("digest %+v", s.Digest)
	}
}

func TestQuotaWithoutCommandOrGoodRun(t *testing.T) {
	ctx, c := connect(t)
	_ = ctx
	srv := httptest.NewServer(Handler(c, time.Hour, Options{}))
	defer srv.Close()
	if body := getJSON(t, srv, "/api/strip", 200, nil); strings.TrimSpace(body) != `{"quota":[],"digest":null}` {
		t.Fatalf("empty strip %s", body)
	}
	q := &Quota{Command: "exit 1"}
	if got := q.Get(ctx, time.Now()); got == nil || len(got) != 0 {
		t.Fatalf("failing command with no good run: %#v", got)
	}
	q = &Quota{Command: "echo not json"}
	if got := q.Get(ctx, time.Now()); len(got) != 0 {
		t.Fatalf("bad json: %#v", got)
	}
}
