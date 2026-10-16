package web

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
	"github.com/rickdiculousli/buildermesh/internal/spec"
)

// frame reads one SSE frame (up to a blank line), skipping comments.
func readFrame(t *testing.T, r *bufio.Reader) (event, data string) {
	t.Helper()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v (event %q data %q)", err, event, data)
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case line == "" && event != "":
			return event, data
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
	}
}

func TestEventsStreamReplaysThenFollows(t *testing.T) {
	url := meshtest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := mesh.Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "app-001", Event: "started", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(c, time.Hour, Options{}))
	defer srv.Close()

	reqCtx, stop := context.WithCancel(ctx)
	defer stop()
	req, _ := http.NewRequestWithContext(reqCtx, "GET", srv.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	r := bufio.NewReader(resp.Body)
	type msg struct {
		Subject string          `json:"subject"`
		At      string          `json:"at"`
		Payload json.RawMessage `json:"payload"`
	}
	ev, data := readFrame(t, r)
	var m msg
	if err := json.Unmarshal([]byte(data), &m); err != nil || ev != "replay" || m.Subject != "bm.app-001.started" ||
		!strings.Contains(string(m.Payload), `"text":"first"`) || m.At == "" {
		t.Fatalf("replay frame %q %q %v", ev, data, err)
	}

	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "app-001", Event: "progress", Text: "second"}); err != nil {
		t.Fatal(err)
	}
	got := make(chan [2]string, 1)
	go func() { e, d := readFrame(t, r); got <- [2]string{e, d} }()
	select {
	case f := <-got:
		if err := json.Unmarshal([]byte(f[1]), &m); err != nil || f[0] != "live" || m.Subject != "bm.app-001.progress" ||
			!strings.Contains(string(m.Payload), `"text":"second"`) {
			t.Fatalf("live frame %q %q %v", f[0], f[1], err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no live frame within 2 s")
	}
	stop()
}

func TestEventsStreamSkipsHostTraffic(t *testing.T) {
	url := meshtest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := mesh.Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	host := func() {
		t.Helper()
		for _, s := range []string{"bm.inbox.host.k16", "bm.hostreply.k16.x"} {
			if _, err := c.JS.Publish(ctx, s, []byte(`{"text":"host"}`)); err != nil {
				t.Fatal(err)
			}
		}
	}
	host()
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "app-001", Event: "started", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(c, time.Hour, Options{}))
	defer srv.Close()
	reqCtx, stop := context.WithCancel(ctx)
	defer stop()
	req, _ := http.NewRequestWithContext(reqCtx, "GET", srv.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)
	if ev, data := readFrame(t, r); ev != "replay" || !strings.Contains(data, `"subject":"bm.app-001.started"`) {
		t.Fatalf("first replay frame %q %q", ev, data)
	}
	host()
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "app-001", Event: "progress", Text: "second"}); err != nil {
		t.Fatal(err)
	}
	got := make(chan [2]string, 1)
	go func() { e, d := readFrame(t, r); got <- [2]string{e, d} }()
	select {
	case f := <-got:
		if f[0] != "live" || !strings.Contains(f[1], `"subject":"bm.app-001.progress"`) {
			t.Fatalf("first live frame %q %q", f[0], f[1])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no live frame within 2 s")
	}
	stop()
}

func TestNodesAndPage(t *testing.T) {
	url := meshtest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := mesh.Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(c, time.Hour, Options{}))
	defer srv.Close()
	get := func(path string) string {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var b strings.Builder
		_, _ = bufio.NewReader(resp.Body).WriteTo(&b)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, b.String())
		}
		return b.String()
	}
	if got := strings.TrimSpace(get("/api/nodes")); got != "[]" {
		t.Fatalf("empty nodes %q", got)
	}
	for _, p := range []string{"b", "a"} {
		if err := mesh.PutNode(ctx, c, mesh.Node{Path: p, Role: "lead"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := mesh.PutRunner(ctx, c, "a", mesh.RunnerSample{Running: true, Known: true}); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Node   mesh.Node          `json:"node"`
		State  mesh.State         `json:"state"`
		Runner *mesh.RunnerSample `json:"runner"`
	}
	if err := json.Unmarshal([]byte(get("/api/nodes")), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Node.Path != "a" || rows[0].Runner == nil || !rows[0].Runner.Running ||
		rows[1].Node.Path != "b" || rows[1].Runner != nil || rows[1].State.State != "idle" {
		t.Fatalf("%+v", rows)
	}
	if page := get("/"); !strings.Contains(page, `src="/map.js"`) || !strings.Contains(page, `href="/map.css"`) {
		t.Fatalf("page %q", page)
	}
	if js := get("/map.js"); !strings.Contains(js, `EventSource("/events")`) {
		t.Fatalf("map.js %q", js)
	}
}

func TestAssets(t *testing.T) {
	_, c := connect(t)
	srv := httptest.NewServer(Handler(c, time.Hour, Options{}))
	defer srv.Close()
	for path, want := range map[string]string{
		"/":        "text/html; charset=utf-8",
		"/map.js":  "text/javascript; charset=utf-8",
		"/map.css": "text/css; charset=utf-8",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != want || len(b) == 0 {
			t.Fatalf("%s: %d %q, %d bytes; want 200 %q", path, resp.StatusCode, resp.Header.Get("Content-Type"), len(b), want)
		}
	}
}

func TestAssetsFromDir(t *testing.T) {
	_, c := connect(t)
	dir := t.TempDir()
	srv := httptest.NewServer(Handler(c, time.Hour, Options{Dir: dir}))
	defer srv.Close()
	get := func(path string) (int, string, string) {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), string(b)
	}
	if code, _, _ := get("/map.js"); code != http.StatusInternalServerError {
		t.Fatalf("missing map.js: %d", code)
	}
	for name, body := range map[string]string{"index.html": "page v1", "map.js": "js v1", "map.css": "css v1"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if code, ct, b := get("/"); code != 200 || ct != "text/html; charset=utf-8" || b != "page v1" {
		t.Fatalf("/: %d %q %q", code, ct, b)
	}
	if code, _, b := get("/map.css"); code != 200 || b != "css v1" {
		t.Fatalf("/map.css: %d %q", code, b)
	}
	if err := os.WriteFile(filepath.Join(dir, "map.js"), []byte("js v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, b := get("/map.js"); code != 200 || b != "js v2" {
		t.Fatalf("/map.js after an edit: %d %q", code, b)
	}
}

// connect starts a mesh and returns a provisioned client.
func connect(t *testing.T) (context.Context, *mesh.Client) {
	t.Helper()
	url := meshtest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	c, err := mesh.Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if err := mesh.Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	return ctx, c
}

// getJSON GETs path from srv, wants status and decodes the body into v (when not nil).
func getJSON(t *testing.T, srv *httptest.Server, path string, status int, v any) string {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != status {
		t.Fatalf("%s: %d %s, want %d", path, resp.StatusCode, b, status)
	}
	if v != nil {
		if err := json.Unmarshal(b, v); err != nil {
			t.Fatalf("%s: %v: %s", path, err, b)
		}
	}
	return string(b)
}

func TestTreeEndpoint(t *testing.T) {
	ctx, c := connect(t)
	repo := filepath.Join(t.TempDir(), "app")
	meshtest.Git(t, filepath.Dir(repo), "init", "-q", "-b", "main", "app")
	meshtest.CommitFiles(t, repo, map[string]string{
		".bmesh/routines/review.toml": "model = \"m\"\nprompt = \"P\"\n",
		".bmesh/routines/verify.toml": "mode = \"inline\"\ndeterministic = true\ncommand = \"echo ok\"\n",
	})
	cfg := &config.Config{Repos: map[string]config.Repo{"app": {Path: repo}}}
	for _, n := range []mesh.Node{
		{Path: "a", Role: "lead"},
		{Path: "a.x", Role: "executor", Parent: "a"},
		{Path: "b.x", Role: "executor", Parent: "b"},
		{Path: "routine.review.m-1", Role: "routine", Repo: "app"},
		{Path: "routine.review.m-2", Role: "routine", Repo: "other"},
		{Path: "routine.review.fabc12345", Role: "routine", Repo: "app"},
	} {
		if err := mesh.PutNode(ctx, c, n); err != nil {
			t.Fatal(err)
		}
	}
	meshtest.ForceState(t, c, "routine.review.fabc12345", mesh.State{State: "done"})
	meshtest.QueueJob(t, c, "review", "a", mesh.Job{Text: "review"})
	if err := mesh.PutRunner(ctx, c, "a", mesh.RunnerSample{Running: true, Known: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := mesh.Send(ctx, c, mesh.Message{From: "a", To: "a.x", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"job j1 ok 5 ctx 700", "job j2 ok 5 ctx 800"} {
		if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "routine.review.m-1", Event: "progress", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(Handler(c, time.Hour, Options{Cfg: cfg}))
	defer srv.Close()
	var v TreeView
	body := getJSON(t, srv, "/api/tree", 200, &v)
	if len(v.Leads) != 1 || v.Leads[0].Runner == nil || len(v.Leads[0].Children) != 1 ||
		v.Leads[0].Children[0].Unread != 1 || v.Leads[0].Children[0].State.State != "idle" {
		t.Fatalf("leads %+v", v.Leads)
	}
	if len(v.Orphans) != 1 || v.Orphans[0].Node.Path != "b.x" {
		t.Fatalf("orphans %+v", v.Orphans)
	}
	// One group per defined kind, each with its depth once and its
	// instances across repos; verify, inline, has none.
	if len(v.Routines) != 2 {
		t.Fatalf("routines %s", body)
	}
	rev, ver := v.Routines[0], v.Routines[1]
	if rev.Kind != "review" || rev.Mode != "pooled" || rev.Depth != 1 || len(rev.Instances) != 3 {
		t.Fatalf("review %+v", rev)
	}
	byKey := map[string]Instance{}
	for _, in := range rev.Instances {
		byKey[in.Node.Path+"@"+in.Node.Repo] = in
	}
	if in := byKey["routine.review.m-1@app"]; in.Uses != 2 || in.Context != 800 {
		t.Fatalf("review instances %+v", rev.Instances)
	}
	if in := byKey["routine.review.fabc12345@app"]; in.State.State != "done" {
		t.Fatalf("review instances %+v", rev.Instances)
	}
	if _, ok := byKey["routine.review.m-2@other"]; !ok {
		t.Fatalf("review instances %+v", rev.Instances)
	}
	if ver.Kind != "verify" || ver.Mode != "inline" || ver.Depth != 0 || len(ver.Instances) != 0 {
		t.Fatalf("verify %+v", ver)
	}
	if !strings.Contains(body, `{"kind":"verify","mode":"inline","instances":[],"depth":0}`) {
		t.Fatalf("verify's shape: %s", body)
	}
	if page := getJSON(t, srv, "/", 200, nil); !strings.Contains(page, "<title>bmesh</title>") {
		t.Fatalf("page %q", page)
	}
}

const specV1 = `---
id: 0042
status: active
merged:
verified:
---
# Map v1

first
`

func TestLeadPageReadsSpecFromOrigin(t *testing.T) {
	ctx, c := connect(t)
	// The background fetch waits for gate and reports on fetched, so the
	// test can show the request never waits for it.
	gate, fetched := make(chan struct{}), make(chan error, 8)
	fetchMain = func(ctx context.Context, repo string) error {
		<-gate
		err := spec.FetchMain(ctx, repo)
		fetched <- err
		return err
	}
	t.Cleanup(func() { fetchMain = spec.FetchMain })
	waitFetch := func() error {
		t.Helper()
		select {
		case err := <-fetched:
			return err
		case <-time.After(30 * time.Second):
			t.Fatal("no background fetch")
			return nil
		}
	}
	// origin holds the spec; the configured clone is behind it, and the
	// lead's own repo_path is another box's path.
	root := t.TempDir()
	origin, clone := filepath.Join(root, "origin"), filepath.Join(root, "clone")
	meshtest.Git(t, root, "init", "-q", "-b", "main", "origin")
	meshtest.CommitFiles(t, origin, map[string]string{"docs/proposals/0042-map.md": specV1})
	meshtest.Git(t, root, "clone", "-q", origin, clone)
	specV2 := strings.Replace(strings.Replace(specV1, "status: active", "status: done", 1), "first", "second", 1)
	meshtest.CommitFiles(t, origin, map[string]string{"docs/proposals/0042-map.md": specV2})

	later := filepath.Join(root, "later") // configured, but not cloned yet
	cfg := &config.Config{
		Repos:   map[string]config.Repo{"demo": {Path: clone}, "later": {Path: later}},
		Spec:    config.SpecConfig{Glob: "docs/proposals/{id}-*.md"},
		Console: config.ConsoleConfig{ThreadURL: "http://here/t/{thread}", Hosts: map[string]string{"other": "http://other/t/{thread}"}},
	}
	for _, n := range []mesh.Node{
		{Path: "a", Role: "lead", Repo: "demo", RepoPath: "/elsewhere/demo", Spec: "0042", Host: "other",
			Runner: mesh.NodeRunner{Kind: "t3", Thread: "th-1"}},
		{Path: "b", Role: "lead", Repo: "nope", Spec: "0042", Host: "box"},
		{Path: "c", Role: "lead", Repo: "later", Spec: "0042", Host: "box"},
	} {
		if err := mesh.PutNode(ctx, c, n); err != nil {
			t.Fatal(err)
		}
	}
	for _, text := range []string{"one", "two"} {
		if _, err := mesh.Send(ctx, c, mesh.Message{From: "a.x", To: "a", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	for _, ev := range []mesh.Event{{From: "a", Event: "started"}, {From: "a.x", Event: "progress", Text: "halfway"}} {
		if err := mesh.PublishEvent(ctx, c, ev); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mesh.WriteState(ctx, c, "a", mesh.State{State: "working", By: "a"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	srv := httptest.NewServer(Handler(c, time.Hour, Options{Cfg: cfg, Host: "box", Now: func() time.Time { return now }}))
	defer srv.Close()

	type page struct {
		Node       mesh.Node  `json:"node"`
		State      mesh.State `json:"state"`
		Spec       *Spec      `json:"spec"`
		ConsoleURL string     `json:"console_url"`
		Inbox      []mesh.Message
		Timeline   []struct {
			Subject string          `json:"subject"`
			At      string          `json:"at"`
			Payload json.RawMessage `json:"payload"`
		}
	}
	var p page
	getJSON(t, srv, "/api/lead/a", 200, &p)
	if p.Node.Path != "a" || p.State.State != "working" || p.ConsoleURL != "http://other/t/th-1" {
		t.Fatalf("page %+v", p)
	}
	// The spec is the clone's own origin/main, read without a fetch: the
	// background fetch is still held at the gate.
	if p.Spec == nil || p.Spec.ID != "0042" || p.Spec.Title != "Map v1" || p.Spec.Status != "active" ||
		!strings.Contains(p.Spec.Text, "first") {
		t.Fatalf("spec %+v", p.Spec)
	}
	if _, err := os.Stat(filepath.Join(clone, ".git", "FETCH_HEAD")); !os.IsNotExist(err) {
		t.Fatalf("the clone was fetched in the request: %v", err)
	}
	close(gate)
	if err := waitFetch(); err != nil {
		t.Fatal(err)
	}
	if len(p.Inbox) != 2 || p.Inbox[0].Text != "two" || p.Inbox[1].Text != "one" {
		t.Fatalf("inbox %+v", p.Inbox)
	}
	if n, err := mesh.Unread(ctx, c, mesh.Path{"a"}); err != nil || n != 2 {
		t.Fatalf("unread after the page %d %v, want 2", n, err)
	}
	if len(p.Timeline) != 2 || p.Timeline[0].Subject != "bm.a.started" || p.Timeline[1].Subject != "bm.a.x.progress" ||
		!strings.Contains(string(p.Timeline[1].Payload), `"text":"halfway"`) || p.Timeline[0].At == "" {
		t.Fatalf("timeline %+v", p.Timeline)
	}

	// Within 60 s the spec comes from the cache (and no fetch starts);
	// after, from origin/main as the last background fetch left it, which
	// starts the next one.
	specV3 := strings.Replace(specV2, "# Map v1", "# Map v3", 1)
	meshtest.CommitFiles(t, origin, map[string]string{"docs/proposals/0042-map.md": specV3})
	getJSON(t, srv, "/api/lead/a", 200, &p)
	if p.Spec == nil || p.Spec.Status != "active" {
		t.Fatalf("cached spec %+v", p.Spec)
	}
	now = now.Add(61 * time.Second)
	getJSON(t, srv, "/api/lead/a", 200, &p)
	if p.Spec == nil || p.Spec.Title != "Map v1" || p.Spec.Status != "done" || !strings.Contains(p.Spec.Text, "second") {
		t.Fatalf("spec after the first fetch %+v", p.Spec)
	}
	if err := waitFetch(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(61 * time.Second)
	getJSON(t, srv, "/api/lead/a", 200, &p)
	if p.Spec == nil || p.Spec.Title != "Map v3" {
		t.Fatalf("spec after the second fetch %+v", p.Spec)
	}
	if err := waitFetch(); err != nil {
		t.Fatal(err)
	}

	// A failed read is not cached: once the repo exists, the next view
	// shows the spec.
	var pc page
	getJSON(t, srv, "/api/lead/c", 200, &pc)
	if pc.Spec != nil {
		t.Fatalf("spec in a missing repo %+v", pc.Spec)
	}
	_ = waitFetch()
	meshtest.Git(t, root, "init", "-q", "-b", "main", "later")
	meshtest.CommitFiles(t, later, map[string]string{"docs/proposals/0042-map.md": specV1})
	getJSON(t, srv, "/api/lead/c", 200, &pc)
	if pc.Spec == nil || pc.Spec.Title != "Map v1" {
		t.Fatalf("spec once the repo exists %+v", pc.Spec)
	}

	// An unknown repo leaves the spec null and the rest of the page whole.
	var pb page
	body := getJSON(t, srv, "/api/lead/b", 200, &pb)
	if pb.Spec != nil || pb.Node.Path != "b" || pb.ConsoleURL != "" || !strings.Contains(body, `"spec":null`) ||
		pb.State.State != "idle" {
		t.Fatalf("page b %s", body)
	}
	getJSON(t, srv, "/api/lead/nobody", 404, nil)

	// A day later the timeline is empty but the inbox still holds its
	// messages: it has no time limit.
	now = time.Now().Add(25 * time.Hour)
	getJSON(t, srv, "/api/lead/a", 200, &p)
	if len(p.Inbox) != 2 || p.Inbox[0].Text != "two" || len(p.Timeline) != 0 {
		t.Fatalf("a day later: inbox %+v timeline %+v", p.Inbox, p.Timeline)
	}
	if err := waitFetch(); err != nil {
		t.Fatal(err)
	}
	getJSON(t, srv, "/api/lead/a*b", 404, nil)
}

func TestRoutinesEndpointListsLeases(t *testing.T) {
	ctx, c := connect(t)
	repo := filepath.Join(t.TempDir(), "app")
	meshtest.Git(t, filepath.Dir(repo), "init", "-q", "-b", "main", "app")
	meshtest.CommitFiles(t, repo, map[string]string{
		".bmesh/routines/verify.toml": "mode = \"fresh\"\ndeterministic = true\ncommand = \"echo ok\"\n",
		".bmesh/routines/review.toml": "model = \"m\"\nprompt = \"P\"\n",
		".bmesh/routines/fmt.toml":    "mode = \"inline\"\ndeterministic = true\ncommand = \"gofmt -l .\"\n",
	})
	cfg := &config.Config{Repos: map[string]config.Repo{"app": {Path: repo}, "gone": {Path: filepath.Join(repo, "missing")}}}
	for _, n := range []mesh.Node{
		{Path: "a", Role: "lead"},
		{Path: "routine.verify.cmd-1", Role: "routine", Repo: "app"},
		{Path: "routine.review.m-1", Role: "routine", Repo: "app"},
	} {
		if err := mesh.PutNode(ctx, c, n); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if _, err := mesh.AcquireLease(ctx, c, "db", "routine.verify.cmd-1", time.Hour, 1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := mesh.AcquireLease(ctx, c, "gpu", "a", time.Hour, 1, now); err != nil {
		t.Fatal(err)
	}
	for _, r := range []mesh.Result{{Kind: "verify", Tree: "t1", OK: true}, {Kind: "verify", Tree: "t2"}, {Kind: "lint", Tree: "t1"}} {
		if err := mesh.PutResult(ctx, c, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := mesh.PublishEvent(ctx, c, mesh.Event{From: "routine.verify.cmd-1", Event: "progress", Text: "job j1 ok 4"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(c, time.Hour, Options{Cfg: cfg, Host: "box"}))
	defer srv.Close()
	var v RoutinesView
	body := getJSON(t, srv, "/api/routines", 200, &v)
	byKind := map[string]RoutineKind{}
	var kinds []string
	for _, k := range v.Routines {
		byKind[k.Kind] = k
		kinds = append(kinds, k.Kind)
	}
	if strings.Join(kinds, ",") != "fmt,lint,review,verify" {
		t.Fatalf("kinds %v: %s", kinds, body)
	}
	ver, rev, lint := byKind["verify"], byKind["review"], byKind["lint"]
	if ver.Mode != "fresh" || !ver.Deterministic || ver.Results != 2 || len(ver.Instances) != 1 || ver.Instances[0].Uses != 1 ||
		len(ver.Leases) != 1 || ver.Leases[0].Key != "db.0" || ver.Leases[0].Holder != "routine.verify.cmd-1" ||
		ver.Leases[0].ExpiresAt != now.Add(time.Hour).UTC().Format(time.RFC3339) {
		t.Fatalf("verify %+v", ver)
	}
	if rev.Mode != "pooled" || rev.Deterministic || rev.Results != 0 || len(rev.Leases) != 0 || len(rev.Instances) != 1 {
		t.Fatalf("review %+v", rev)
	}
	if lint.Mode != "" || lint.Deterministic || lint.Results != 1 || len(lint.Instances) != 0 {
		t.Fatalf("lint %+v", lint)
	}
	if f := byKind["fmt"]; f.Mode != "inline" || !f.Deterministic || f.Results != 0 || len(f.Instances) != 0 || len(f.Leases) != 0 {
		t.Fatalf("fmt (defined, never run) %+v", f)
	}
	if len(v.Leases) != 1 || v.Leases[0].Key != "gpu.0" || v.Leases[0].Holder != "a" {
		t.Fatalf("unowned leases %+v", v.Leases)
	}
	if !strings.Contains(body, `"instances":[]`) {
		t.Fatalf("lint's instances not []: %s", body)
	}
}

func ledgerSpec(id, title, status, by string) string {
	return "---\nid: " + id + "\ntitle: " + title + "\nstatus: " + status + "\nproposed_by: " + by + "\n---\n\n## Opportunity\n" + title + " body\n"
}

func TestLeadPageChildrenAndRoundSpecs(t *testing.T) {
	ctx, c := connect(t)
	repo := filepath.Join(t.TempDir(), "app")
	meshtest.Git(t, filepath.Dir(repo), "init", "-q", "-b", "main", "app")
	meshtest.CommitFiles(t, repo, map[string]string{
		"docs/proposals/README.md":      "# the lifecycle\n",
		"docs/proposals/003-other.md":   ledgerSpec("003", "Other round", "done", "th-old"),
		"docs/proposals/004-broken.md":  "no frontmatter\n",
		"docs/proposals/005-pending.md": ledgerSpec("005", "Not landed", "proposed", "th-eng"),
	})
	// Only main as it stands counts: 005 left it, 006 is on a branch. A
	// malformed file is left out.
	meshtest.Git(t, repo, "checkout", "-q", "-b", "side")
	meshtest.CommitFiles(t, repo, map[string]string{"docs/proposals/006-side.md": ledgerSpec("006", "Side", "proposed", "th-ux")})
	meshtest.Git(t, repo, "checkout", "-q", "main")
	meshtest.Git(t, repo, "rm", "-q", "docs/proposals/005-pending.md")
	meshtest.Git(t, repo, "commit", "-q", "-m", "drop 005")
	cfg := &config.Config{Repos: map[string]config.Repo{"app": {Path: repo}}, Spec: config.SpecConfig{Glob: "docs/proposals/{id}-*.md"}}
	for _, n := range []mesh.Node{
		{Path: "a", Role: "lead", Repo: "app"},
		{Path: "a.x", Role: "executor", Parent: "a", Branch: "a-x"},
		{Path: "a.y", Role: "executor", Parent: "a", Branch: "a-y"},
		{Path: "a.x.z", Role: "executor", Parent: "a.x", Branch: "a-x-z"},
		{Path: "b", Role: "lead", Repo: "app"},
		{Path: "app-round-1", Role: "round", Repo: "app"},
		{Path: "app-round-1.eng", Role: "engineer", Parent: "app-round-1", Runner: mesh.NodeRunner{Thread: "th-eng"}},
		// A T3 V2 thread: T3 named it, and its specs carry the id bmesh chose.
		{Path: "app-round-1.ux", Role: "engineer", Parent: "app-round-1", Runner: mesh.NodeRunner{Thread: "mcp:ux", Ref: "th-ux"}},
	} {
		if err := mesh.PutNode(ctx, c, n); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mesh.WriteState(ctx, c, "a.x", mesh.State{State: "working"}); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"one", "two"} {
		if _, err := mesh.Send(ctx, c, mesh.Message{From: "a", To: "a.x", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	srv := httptest.NewServer(Handler(c, time.Hour, Options{Cfg: cfg, Host: "box", Now: func() time.Time { return now }}))
	defer srv.Close()

	type page struct {
		Children []Child     `json:"children"`
		Specs    []RoundSpec `json:"specs"`
	}
	// A lead's children are its direct ones, sorted by path, with their
	// states and unread counts; a lead has no specs.
	var p page
	body := getJSON(t, srv, "/api/lead/a", 200, &p)
	if len(p.Children) != 2 || p.Children[0].Node.Path != "a.x" || p.Children[0].State.State != "working" ||
		p.Children[0].Unread != 2 || p.Children[0].Node.Branch != "a-x" ||
		p.Children[1].Node.Path != "a.y" || p.Children[1].State.State != "idle" || p.Children[1].Unread != 0 {
		t.Fatalf("children of a: %s", body)
	}
	if !strings.Contains(body, `"specs":null`) {
		t.Fatalf("a lead's specs: %s", body)
	}
	// An executor's page has its own children; a node without any has [].
	getJSON(t, srv, "/api/lead/a.x", 200, &p)
	if len(p.Children) != 1 || p.Children[0].Node.Path != "a.x.z" {
		t.Fatalf("children of a.x %+v", p.Children)
	}
	if body := getJSON(t, srv, "/api/lead/b", 200, nil); !strings.Contains(body, `"children":[]`) {
		t.Fatalf("b: %s", body)
	}

	// Before its engineers land anything, a round's specs are [].
	body = getJSON(t, srv, "/api/lead/app-round-1", 200, &p)
	if len(p.Children) != 2 || p.Children[0].Node.Path != "app-round-1.eng" || p.Children[1].Node.Path != "app-round-1.ux" ||
		!strings.Contains(body, `"specs":[]`) {
		t.Fatalf("round before landing: %s", body)
	}

	// Once they land, the round shows its children's specs by id, read
	// from main's ledger: cached for a minute, then read again.
	meshtest.CommitFiles(t, repo, map[string]string{
		"docs/proposals/007-eng.md": ledgerSpec("007", "Faster tests", "proposed", "th-eng"),
		"docs/proposals/012-ux.md":  ledgerSpec("012", "Clearer map", "accepted", "th-ux"),
	})
	getJSON(t, srv, "/api/lead/app-round-1", 200, &p)
	if len(p.Specs) != 0 {
		t.Fatalf("specs within the cache minute %+v", p.Specs)
	}
	now = now.Add(61 * time.Second)
	getJSON(t, srv, "/api/lead/app-round-1", 200, &p)
	if len(p.Specs) != 2 || p.Specs[0] != (RoundSpec{ID: "007", Title: "Faster tests", Status: "proposed",
		Text: ledgerSpec("007", "Faster tests", "proposed", "th-eng")}) ||
		p.Specs[1].ID != "012" || p.Specs[1].Title != "Clearer map" || p.Specs[1].Status != "accepted" {
		t.Fatalf("round specs %+v", p.Specs)
	}
}
