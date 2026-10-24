// Package t3done is the t3-done add-on: it posts to ntfy when a T3 thread a
// person started on this box is done, failed or asking, read from T3's own
// database since T3's API doesn't say whether a turn left a background task
// pending.
package t3done

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite" // the "sqlite" driver, pure Go

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/notify"
)

// Deps is what Watch runs with.
type Deps struct {
	DB        string        // T3's database; its -wal file beside it
	State     string        // the directory the marks live in
	Every     time.Duration // the tick
	Post      func(ctx context.Context, h notify.Headers, body string) error
	Host      string        // the box, as the body names it
	ThreadURL string        // the thread link template ([console] thread_url); "" = no Click
	MinBusy   time.Duration // a done whose thread was busy less than this posts nothing
	Log       *slog.Logger
	Now       func() time.Time
}

// DefaultDB is this box's T3 database.
func DefaultDB() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".t3", "userdata", "statev2.sqlite")
}

// Run is the add-on as serve starts it: Watch on this box's T3 database,
// posting to [addons.t3-done] ntfy, its marks in stateDir.
func Run(ctx context.Context, cfg *config.Config, host, stateDir string, log *slog.Logger) {
	d := Deps{DB: DefaultDB(), State: stateDir, Every: cfg.Addons.T3Done.Every, MinBusy: cfg.Addons.T3Done.MinBusy,
		Post: notify.Target{Ntfy: cfg.Addons.T3Done.Ntfy}.Post, Host: host, ThreadURL: cfg.Console.ThreadURL, Log: log, Now: time.Now}
	log.Info("t3-done: watching", "db", d.DB, "ntfy", cfg.Addons.T3Done.Ntfy)
	Watch(ctx, d)
}

// Watch looks at d.DB every d.Every until ctx ends.
func Watch(ctx context.Context, d Deps) {
	w := &watcher{d: d}
	t := time.NewTicker(d.Every)
	defer t.Stop()
	for {
		w.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// stamp is a file's modification time and size; the zero stamp is a
// missing file.
type stamp struct {
	mod  time.Time
	size int64
}

func stampOf(path string) stamp {
	st, err := os.Stat(path)
	if err != nil {
		return stamp{}
	}
	return stamp{st.ModTime(), st.Size()}
}

type watcher struct {
	d       Deps
	seen    [2]stamp // the database's and its -wal's at the last read
	read    bool     // a read happened (seen is set)
	again   bool     // the last read stopped short: read next tick whatever the files say
	trouble string   // what was last logged as wrong; "" = nothing
}

// tick reads the database when it or its -wal changed since the last read
// (or that read stopped short), and reports whether it read.
func (w *watcher) tick(ctx context.Context) bool {
	now := [2]stamp{stampOf(w.d.DB), stampOf(w.d.DB + "-wal")}
	if now[0] == (stamp{}) {
		w.say("no T3 database at " + w.d.DB)
		return false
	}
	if w.read && now == w.seen && !w.again {
		return false
	}
	w.seen, w.read = now, true
	err := w.readOnce(ctx)
	// A mismatch waits for the files to change; anything else (a locked
	// database, a failed post) is read again next tick.
	w.again = err != nil && !errors.As(err, new(mismatch))
	switch {
	case errors.As(err, new(mismatch)):
		w.say(fmt.Sprintf("idle: T3's database has no %s (a T3 update changed it); nothing is posted until it does", err))
	case err != nil:
		w.say(err.Error())
	default:
		w.say("")
	}
	return true
}

// say logs msg once until it changes; "" after trouble logs that it reads
// again.
func (w *watcher) say(msg string) {
	if msg == w.trouble {
		return
	}
	if msg == "" {
		w.d.Log.Info("t3-done: reading again", "db", w.d.DB)
	} else {
		w.d.Log.Warn("t3-done: " + msg)
	}
	w.trouble = msg
}

// mismatch names a table, column or payload field the database lacks.
type mismatch string

func (m mismatch) Error() string { return string(m) }

// The T3 tables read, with the columns each query names.
const (
	threads   = "orchestration_v2_projection_threads"
	runs      = "orchestration_v2_projection_runs"
	providers = "orchestration_v2_projection_provider_threads"
	requests  = "orchestration_v2_projection_runtime_requests"
	messages  = "orchestration_v2_projection_messages"
	projects  = "projection_projects"
)

var columns = []struct {
	table string
	cols  []string
}{
	{threads, []string{"thread_id", "project_id", "title", "deleted_at", "payload_json"}},
	{runs, []string{"thread_id", "ordinal", "provider_thread_id", "status", "requested_at", "completed_at", "payload_json"}},
	{providers, []string{"provider_thread_id", "payload_json"}},
	{requests, []string{"thread_id", "status", "created_at"}},
	{messages, []string{"message_id", "thread_id", "role", "created_at", "payload_json"}},
	{projects, []string{"project_id", "title"}},
}

// fields are the payload fields read, each checked on its table's newest
// row (a table with none passes).
var fields = []struct{ table, where, path string }{
	{threads, "1", "$.createdBy"},
	{threads, "1", "$.lineage.parentThreadId"},
	{runs, "1", "$.userMessageId"},
	{providers, "1", "$.pendingBackgroundTasks"},
	{messages, "role = 'assistant'", "$.text"},
}

// check is a mismatch naming the first table, column or payload field the
// queries read that db lacks.
func check(ctx context.Context, db *sql.DB) error {
	for _, t := range columns {
		have := map[string]bool{}
		rows, err := db.QueryContext(ctx, "select name from pragma_table_info(?)", t.table)
		if err != nil {
			return err
		}
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return err
			}
			have[n] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(have) == 0 {
			return mismatch("table " + t.table)
		}
		for _, c := range t.cols {
			if !have[c] {
				return mismatch("column " + t.table + "." + c)
			}
		}
	}
	for _, f := range fields {
		var missing bool
		err := db.QueryRowContext(ctx, "select json_type(payload_json, ?) is null from "+f.table+" where "+f.where+
			" order by rowid desc limit 1", f.path).Scan(&missing)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if missing {
			return mismatch("field " + strings.TrimPrefix(f.path, "$.") + " in " + f.table + ".payload_json")
		}
	}
	return nil
}

// own is the threads a person started: not deleted, no parent, not made
// by an agent (bmesh's leads and rounds, which push on their own).
const own = `t.deleted_at is null
  and json_extract(t.payload_json, '$.lineage.parentThreadId') is null
  and coalesce(json_extract(t.payload_json, '$.createdBy'), '') != 'agent'`

// event is one row read: its time (the mark it moves to), what it posts
// ("" = nothing), the thread's, and when the thread's busy stretch began
// (the row's own time for a request).
type event struct {
	at, kind, thread, title, project, since string
}

// queries read each kind's rows after its mark, oldest first.
var queries = []struct{ mark, sql string }{
	// A run that ends completed while its provider thread still lists a
	// pending background task (a monitor, a background command) is a
	// wake-up, not the end: T3 shows the thread waiting. The busy stretch
	// begins at the newest run up to this one that a person's message
	// started, not a background task's notification.
	{"runs", `select r.completed_at,
    case when r.status = 'failed' then 'failed'
      when r.status = 'completed' and coalesce(json_array_length(pt.payload_json, '$.pendingBackgroundTasks'), 0) = 0 then 'done'
      else '' end,
    t.thread_id, t.title, coalesce(p.title, ''),
    coalesce((select max(r2.requested_at) from ` + runs + ` r2
      left join ` + messages + ` m on m.message_id = json_extract(r2.payload_json, '$.userMessageId')
      where r2.thread_id = r.thread_id and r2.ordinal <= r.ordinal
        and json_extract(m.payload_json, '$.notification') is null), r.requested_at)
  from ` + runs + ` r
  join ` + threads + ` t on t.thread_id = r.thread_id
  left join ` + projects + ` p on p.project_id = t.project_id
  left join ` + providers + ` pt on pt.provider_thread_id = r.provider_thread_id
  where r.completed_at > ? and ` + own + `
  order by r.completed_at`},
	{"requests", `select q.created_at, 'asking', t.thread_id, t.title, coalesce(p.title, ''), q.created_at
  from ` + requests + ` q
  join ` + threads + ` t on t.thread_id = q.thread_id
  left join ` + projects + ` p on p.project_id = t.project_id
  where q.created_at > ? and q.status = 'pending' and ` + own + `
  order by q.created_at`},
}

// readOnce opens d.DB read-only, checks it, and posts each kind's rows
// after its mark, moving the mark past every row read, posted or not. A
// post that fails stops it there, so the next read posts that row again.
func (w *watcher) readOnce(ctx context.Context) error {
	db, err := sql.Open("sqlite", dsn(w.d.DB))
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := check(ctx, db); err != nil {
		return err
	}
	if err := os.MkdirAll(w.d.State, 0o755); err != nil {
		return err
	}
	for _, q := range queries {
		mark, err := w.mark(q.mark)
		if err != nil {
			return err
		}
		evs, err := scan(ctx, db, q.sql, mark)
		if err != nil {
			return fmt.Errorf("read %s: %w", q.mark, err)
		}
		for _, ev := range evs {
			if ev.kind == "done" && busy(ev) < w.d.MinBusy {
				ev.kind = ""
			}
			if ev.kind != "" {
				var reply string
				err := db.QueryRowContext(ctx, `select coalesce(json_extract(payload_json, '$.text'), '') from `+messages+`
  where thread_id = ? and role = 'assistant' and created_at <= ? order by created_at desc limit 1`, ev.thread, ev.at).Scan(&reply)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("read the reply: %w", err)
				}
				h, body := Message(ev.kind, ev.title, ev.project, w.d.Host, reply)
				if w.d.ThreadURL != "" {
					h.Click = config.ThreadLink(w.d.ThreadURL, ev.thread)
				}
				if err := w.d.Post(ctx, h, body); err != nil {
					return fmt.Errorf("post %s of %q: %w", ev.kind, ev.title, err)
				}
			}
			if err := w.setMark(q.mark, ev.at); err != nil {
				return err
			}
		}
	}
	return nil
}

func scan(ctx context.Context, db *sql.DB, query, mark string) ([]event, error) {
	rows, err := db.QueryContext(ctx, query, mark)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var evs []event
	for rows.Next() {
		var ev event
		if err := rows.Scan(&ev.at, &ev.kind, &ev.thread, &ev.title, &ev.project, &ev.since); err != nil {
			return nil, err
		}
		evs = append(evs, ev)
	}
	return evs, rows.Err()
}

// busy is how long ev's thread was busy: from its stretch's start to the
// row's time. A time that doesn't parse counts as busy for long.
func busy(ev event) time.Duration {
	from, err1 := time.Parse(time.RFC3339Nano, ev.since)
	to, err2 := time.Parse(time.RFC3339Nano, ev.at)
	if err1 != nil || err2 != nil {
		return time.Duration(1<<63 - 1)
	}
	return to.Sub(from)
}

// dsn opens path read-only, waiting up to 2 s on T3's writes.
func dsn(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // C:/x on Windows is file:///C:/x
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro&_pragma=busy_timeout(2000)"}
	return u.String()
}

// stampLayout is how T3 writes a time.
const stampLayout = "2006-01-02T15:04:05.000Z"

// mark is kind's mark: the time of the last row read, else (a fresh start)
// now, which it writes.
func (w *watcher) mark(kind string) (string, error) {
	b, err := os.ReadFile(filepath.Join(w.d.State, kind))
	if err == nil {
		return strings.TrimSpace(string(b)), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	now := w.d.Now().UTC().Format(stampLayout)
	return now, w.setMark(kind, now)
}

func (w *watcher) setMark(kind, at string) error {
	p := filepath.Join(w.d.State, kind)
	if err := os.WriteFile(p+".tmp", []byte(at+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

// excerptMax is how much of the last reply the body carries, in runes.
const excerptMax = 140

var says = map[string]struct{ emoji, status string }{
	"done":   {"✅", "Done"},
	"failed": {"❌", "Failed"},
	"asking": {"🙋", "Asks"},
}

// Message is the post for kind (done, failed or asking) in a thread titled
// title: the title `<emoji> <status> • <thread title>` (notify.Title), and
// the body the start of reply with its markdown taken out, then `· <project>
// on <host>` (no project part without a project; the place alone without a
// reply).
func Message(kind, title, project, host, reply string) (notify.Headers, string) {
	s := says[kind]
	where := host
	if project != "" {
		where = project + " on " + host
	}
	body := where
	if r := excerpt(plain(reply)); r != "" {
		body = r + " · " + where
	}
	return notify.Headers{Title: notify.Title(s.emoji, s.status, strings.Join(strings.Fields(title), " "))}, body
}

// mdLead is a markdown line's heading, list or quote marker; mdInline its
// emphasis and code marks; mdLink a link, whose text stays.
var (
	mdLead   = regexp.MustCompile(`(?m)^\s*(?:#{1,6}\s+|[-*+]\s+|\d+[.)]\s+|>\s*)`)
	mdInline = regexp.MustCompile("\\*\\*|`")
	mdLink   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

// plain is markdown s as a phone shows it best: no heading, list or quote
// markers, ** or code marks, and links as their text.
func plain(s string) string {
	s = mdLead.ReplaceAllString(s, "")
	s = mdLink.ReplaceAllString(s, "$1")
	return mdInline.ReplaceAllString(s, "")
}

// excerpt is s on one line, cut to excerptMax runes with an ellipsis.
func excerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= excerptMax {
		return s
	}
	return string([]rune(s)[:excerptMax-1]) + "…"
}
