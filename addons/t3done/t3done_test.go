package t3done

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/notify"
)

// fixture is a T3 database with T3's layout (testdata/t3-schema.sql) in
// WAL mode, as T3 keeps it, and a watcher on it that posts through
// testdata/fake-ntfy.sh.
type fixture struct {
	t    *testing.T
	db   *sql.DB // read-write, as T3 writes
	w    *watcher
	logs *bytes.Buffer
	ntfy string // fake-ntfy.sh's log
	now  time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "statev2.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	schema, err := os.ReadFile(filepath.Join("testdata", "t3-schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("pragma journal_mode = wal; " + string(schema)); err != nil {
		t.Fatal(err)
	}
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-ntfy.sh"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, db: db, logs: &bytes.Buffer{}, ntfy: filepath.Join(dir, "ntfy.log"),
		now: time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)}
	t.Setenv("FAKE_NTFY_LOG", f.ntfy)
	f.w = &watcher{d: Deps{DB: path, State: filepath.Join(dir, "state"), Every: time.Second,
		Post: notify.Target{Command: fake}.Post, Host: "k16", Log: slog.New(slog.NewTextHandler(f.logs, nil)),
		Now: func() time.Time { return f.now }}}
	f.exec(`insert into projection_projects (project_id, title, workspace_root, scripts_json, created_at, updated_at)
  values ('p1', 'party-buzzer', '/r/party-buzzer', '[]', ?, ?)`, f.at(-60), f.at(-60))
	return f
}

// at is a T3 time s seconds after the fixture's now.
func (f *fixture) at(s int) string {
	return f.now.Add(time.Duration(s) * time.Second).Format(stampLayout)
}

func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.db.Exec(q, args...); err != nil {
		f.t.Fatal(err)
	}
}

// thread adds a thread of project p1 with T3's payload: createdBy as given,
// lineage.parentThreadId parent (null when "").
func (f *fixture) thread(id, title, createdBy, parent string) {
	f.t.Helper()
	p := "null"
	if parent != "" {
		p = `"` + parent + `"`
	}
	payload := `{"createdBy":"` + createdBy + `","creationSource":"web","id":"` + id + `","projectId":"p1","title":"` + title +
		`","lineage":{"parentThreadId":` + p + `,"relationshipToParent":null,"rootThreadId":"` + id + `"},"deletedAt":null}`
	f.exec(`insert into orchestration_v2_projection_threads (thread_id, project_id, title, default_provider, runtime_mode,
  interaction_mode, created_at, updated_at, payload_json) values (?, 'p1', ?, 'claudeAgent', 'auto', 'default', ?, ?, ?)`,
		id, title, f.at(-30), f.at(-30), payload)
	f.exec(`insert into orchestration_v2_projection_provider_threads (provider_thread_id, thread_id, provider, status, updated_at,
  payload_json) values (?, ?, 'claudeAgent', 'idle', ?, '{"id":"pt-`+id+`","status":"idle","pendingBackgroundTasks":[]}')`,
		"pt-"+id, id, f.at(-30))
}

// pending sets thread's provider thread's pendingBackgroundTasks.
func (f *fixture) pending(thread, tasks string) {
	f.exec(`update orchestration_v2_projection_provider_threads set payload_json = ? where provider_thread_id = ?`,
		`{"id":"pt-`+thread+`","status":"idle","pendingBackgroundTasks":`+tasks+`}`, "pt-"+thread)
}

// run adds thread's run n, started by a person 5 s before it ended status
// at s seconds, its assistant reply just before the end.
func (f *fixture) run(thread string, n int, status string, s int, reply string) {
	f.t.Helper()
	f.turn(thread, n, status, s-5, s, reply, false)
}

// turn adds thread's run n, requested at from seconds and ended status at
// to, with the user message that started it (a background task's
// notification when wake) and its assistant reply just before the end.
func (f *fixture) turn(thread string, n int, status string, from, to int, reply string, wake bool) {
	f.t.Helper()
	id := "run:thread:" + thread + ":ordinal:" + string(rune('0'+n))
	user := `{"role":"user","text":"go"}`
	if wake {
		user = `{"role":"user","text":"Background task completed.","notification":{"source":{"kind":"background_task"}}}`
	}
	f.exec(`insert into orchestration_v2_projection_messages (message_id, thread_id, run_id, role, streaming, created_at,
  updated_at, payload_json) values (?, ?, ?, 'user', 0, ?, ?, ?)`, "u-"+id, thread, id, f.at(from), f.at(from), user)
	f.exec(`insert into orchestration_v2_projection_messages (message_id, thread_id, run_id, role, streaming, created_at,
  updated_at, payload_json) values (?, ?, ?, 'assistant', 0, ?, ?, json_object('role', 'assistant', 'text', ?))`,
		"m-"+id, thread, id, f.at(to), f.at(to), reply)
	f.exec(`insert into orchestration_v2_projection_runs (run_id, thread_id, ordinal, provider, provider_thread_id, status,
  requested_at, completed_at, payload_json) values (?, ?, ?, 'claudeAgent', ?, ?, ?, ?, json_object('status', ?, 'userMessageId', ?))`,
		id, thread, n, "pt-"+thread, status, f.at(from), f.at(to), status, "u-"+id)
}

// request adds a runtime request of thread with status at s seconds.
func (f *fixture) request(thread, id, status string, s int) {
	f.exec(`insert into orchestration_v2_projection_runtime_requests (runtime_request_id, thread_id, node_id, kind, status,
  created_at, payload_json) values (?, ?, 'n', 'user_input', ?, ?, json_object('kind', 'user_input', 'status', ?))`,
		id, thread, status, f.at(s), status)
}

func (f *fixture) tick() bool { return f.w.tick(context.Background()) }

// posts is what fake-ntfy.sh got, one string per post.
func (f *fixture) posts() []string {
	b, err := os.ReadFile(f.ntfy)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, p := range strings.Split(string(b), "---\n") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (f *fixture) wantPosts(n int) []string {
	f.t.Helper()
	p := f.posts()
	if len(p) != n {
		f.t.Fatalf("%d posts, want %d: %q\nlog: %s", len(p), n, p, f.logs)
	}
	return p
}

func TestCompletedRunWithNothingPendingPostsOnce(t *testing.T) {
	f := newFixture(t)
	f.w.d.ThreadURL = "http://k16:3773/{thread}"
	f.thread("t1", "Fix the buzzer", "user", "")
	f.tick() // a fresh start: its marks are now
	f.run("t1", 1, "completed", 5, "The buzzer   fixed.\nTests pass.")
	f.tick()
	f.tick()
	got := f.wantPosts(1)[0]
	want := "Fix the buzzer\ndone in party-buzzer on k16: The buzzer fixed. Tests pass.\n" +
		"NTFY_TITLE=Fix the buzzer\nNTFY_PRIORITY=\nNTFY_TAGS=white_check_mark\nNTFY_CLICK=http://k16:3773/t1\nNTFY_ACTIONS=\n"
	if got != want {
		t.Fatalf("post:\n%s\nwant:\n%s", got, want)
	}
}

func TestRunWithAPendingMonitorIsAWakeUp(t *testing.T) {
	f := newFixture(t)
	f.thread("t1", "Build 085", "user", "")
	f.tick()
	f.pending("t1", `[{"taskId":"b3bu0sn2s","description":"Watch lead 085","kind":"command"}]`)
	f.run("t1", 1, "completed", 5, "Still watching.")
	f.tick()
	f.wantPosts(0)
	// The monitor ends: the list empties, and the wake-up stays silent.
	f.pending("t1", `[]`)
	f.tick()
	f.wantPosts(0)
	// The run after it, with nothing pending, is the done.
	f.run("t1", 2, "completed", 9, "085 landed.")
	f.tick()
	if p := f.wantPosts(1)[0]; !strings.HasPrefix(p, "Build 085\ndone in party-buzzer on k16: 085 landed.\n") {
		t.Fatalf("post %q", p)
	}
}

func TestADoneUnderMinBusyIsQuiet(t *testing.T) {
	f := newFixture(t)
	f.w.d.MinBusy = 5 * time.Minute
	f.thread("t1", "One", "user", "")
	f.tick()
	f.run("t1", 1, "completed", 10, "quick")
	f.tick()
	f.wantPosts(0)
	f.turn("t1", 2, "completed", 20, 320, "long", false)
	f.tick()
	if p := f.wantPosts(1)[0]; !strings.HasPrefix(p, "One\ndone in party-buzzer on k16: long\n") {
		t.Fatalf("post %q", p)
	}
	// A person's ask that armed a monitor, then a quick wake-up: busy from
	// the ask, so it posts.
	f.pending("t1", `[{"taskId":"m","kind":"command"}]`)
	f.turn("t1", 3, "completed", 400, 410, "watching", false)
	f.tick()
	f.pending("t1", `[]`)
	f.turn("t1", 4, "completed", 700, 710, "landed", true)
	f.tick()
	if p := f.wantPosts(2)[1]; !strings.HasPrefix(p, "One\ndone in party-buzzer on k16: landed\n") {
		t.Fatalf("post %q", p)
	}
	// Failures and questions post however short.
	f.run("t1", 5, "failed", 800, "")
	f.request("t1", "q1", "pending", 801)
	f.tick()
	f.wantPosts(4)
	// A quick ask after the long ones counts from itself.
	f.run("t1", 6, "completed", 900, "quick again")
	f.tick()
	f.wantPosts(4)
}

func TestFailedPostsInterruptedDoesntRequestAsks(t *testing.T) {
	f := newFixture(t)
	f.thread("t1", "One", "user", "")
	f.thread("t2", "Two", "user", "")
	f.tick()
	f.pending("t1", `[{"taskId":"x","kind":"command"}]`) // a failed run posts whatever is pending
	f.run("t1", 1, "failed", 2, "")
	for i, s := range []string{"interrupted", "cancelled", "rolled_back"} {
		f.run("t2", i+1, s, 3+i, "stopped")
	}
	f.request("t2", "r1", "pending", 8)
	f.request("t2", "r2", "resolved", 9)
	f.tick()
	p := f.wantPosts(2)
	if !strings.HasPrefix(p[0], "One\nfailed in party-buzzer on k16\n") || !strings.Contains(p[0], "NTFY_TAGS=x\n") {
		t.Errorf("failed: %q", p[0])
	}
	if !strings.HasPrefix(p[1], "Two\nasking for input in party-buzzer on k16: stopped\n") || !strings.Contains(p[1], "NTFY_TAGS=raising_hand\n") ||
		!strings.Contains(p[1], "NTFY_CLICK=\n") {
		t.Errorf("asking: %q", p[1])
	}
}

func TestChildAndAgentThreadsDontPost(t *testing.T) {
	f := newFixture(t)
	f.thread("child", "Child", "user", "parent")
	f.thread("mcp:lead", "buildermesh-085", "agent", "")
	f.tick()
	f.run("child", 1, "completed", 2, "")
	f.run("mcp:lead", 1, "failed", 3, "")
	f.request("mcp:lead", "r1", "pending", 4)
	f.tick()
	f.wantPosts(0)
}

func TestAMissingColumnIsIdle(t *testing.T) {
	f := newFixture(t)
	f.thread("t1", "One", "user", "")
	f.tick()
	f.exec(`alter table orchestration_v2_projection_runs rename column completed_at to finished_at`)
	f.exec(`insert into orchestration_v2_projection_runs (run_id, thread_id, ordinal, provider, provider_thread_id, status,
  requested_at, finished_at, payload_json) values ('r', 't1', 1, 'claudeAgent', 'pt-t1', 'completed', ?, ?, '{}')`, f.at(1), f.at(2))
	f.request("t1", "q1", "pending", 3)
	if !f.tick() {
		t.Fatal("a changed database wasn't read")
	}
	f.tick()
	f.wantPosts(0)
	if n := strings.Count(f.logs.String(), "idle: T3's database has no column orchestration_v2_projection_runs.completed_at"); n != 1 {
		t.Fatalf("%d idle lines, want 1:\n%s", n, f.logs)
	}
}

func TestAMissingPayloadFieldIsIdle(t *testing.T) {
	f := newFixture(t)
	f.thread("t1", "One", "user", "")
	f.tick()
	f.exec(`update orchestration_v2_projection_provider_threads set payload_json = '{"id":"pt-t1","backgroundTasks":[]}'`)
	f.run("t1", 1, "completed", 2, "")
	f.tick()
	f.wantPosts(0)
	if !strings.Contains(f.logs.String(), "no field pendingBackgroundTasks in orchestration_v2_projection_provider_threads.payload_json") {
		t.Fatalf("no idle line:\n%s", f.logs)
	}
}

func TestAnUnchangedFileRunsNoQuery(t *testing.T) {
	f := newFixture(t)
	f.thread("t1", "One", "user", "")
	if !f.tick() {
		t.Fatal("the first tick didn't read")
	}
	if f.tick() || f.tick() {
		t.Fatal("an unchanged database was read")
	}
	f.run("t1", 1, "completed", 2, "")
	if !f.tick() {
		t.Fatal("a changed database wasn't read")
	}
	if f.tick() {
		t.Fatal("an unchanged database was read")
	}
	f.wantPosts(1)
}

func TestMarksSurviveARestartAndAFreshStartBeginsNow(t *testing.T) {
	f := newFixture(t)
	f.thread("t1", "One", "user", "")
	f.run("t1", 1, "completed", -10, "") // before the fresh start: never posted
	f.tick()
	f.wantPosts(0)
	f.run("t1", 2, "completed", 2, "")
	f.tick()
	f.wantPosts(1)
	// A new watcher on the same state carries on from the marks.
	f.w = &watcher{d: f.w.d}
	f.now = f.now.Add(time.Hour)
	f.tick()
	f.wantPosts(1)
	f.run("t1", 3, "completed", 5, "")
	f.tick()
	f.wantPosts(2)
}

func TestAFailedPostIsRetried(t *testing.T) {
	f := newFixture(t)
	f.thread("t1", "One", "user", "")
	f.tick()
	post := f.w.d.Post
	f.w.d.Post = func(context.Context, notify.Headers, string) error { return os.ErrDeadlineExceeded }
	f.run("t1", 1, "completed", 2, "")
	f.tick()
	f.wantPosts(0)
	f.w.d.Post = post
	if !f.tick() {
		t.Fatal("the failed post wasn't read again")
	}
	f.wantPosts(1)
}

func TestMessageExcerpt(t *testing.T) {
	long := strings.Repeat("é", 200)
	h, body := Message("done", "  A\ntitle ", "", "k16", long)
	if h.Title != "A title" || h.Tags != "white_check_mark" {
		t.Errorf("headers %+v", h)
	}
	if want := "done on k16: " + strings.Repeat("é", excerptMax-1) + "…"; body != want {
		t.Errorf("body %q", body)
	}
}
