package web

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
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
	if err := mesh.Provision(ctx, c); err != nil {
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

func TestNodesAndPage(t *testing.T) {
	url := meshtest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := mesh.Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := mesh.Provision(ctx, c); err != nil {
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
	if page := get("/"); !strings.Contains(page, `EventSource("/events")`) {
		t.Fatalf("page %q", page)
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
	if err := mesh.Provision(ctx, c); err != nil {
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

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes files (relative path to content) into repo and commits them.
func commit(t *testing.T, repo string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, repo, "add", name)
	}
	git(t, repo, "commit", "-q", "-m", "files")
}

func TestTreeEndpoint(t *testing.T) {
	ctx, c := connect(t)
	for _, n := range []mesh.Node{
		{Path: "a", Role: "lead"},
		{Path: "a.x", Role: "executor", Parent: "a"},
		{Path: "b.x", Role: "executor", Parent: "b"},
		{Path: "routine.review.m-1", Role: "routine", Repo: "app"},
	} {
		if err := mesh.PutNode(ctx, c, n); err != nil {
			t.Fatal(err)
		}
	}
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
	srv := httptest.NewServer(Handler(c, time.Hour, Options{}))
	defer srv.Close()
	var v TreeView
	getJSON(t, srv, "/api/tree", 200, &v)
	if len(v.Leads) != 1 || v.Leads[0].Runner == nil || len(v.Leads[0].Children) != 1 ||
		v.Leads[0].Children[0].Unread != 1 || v.Leads[0].Children[0].State.State != "idle" {
		t.Fatalf("leads %+v", v.Leads)
	}
	if len(v.Orphans) != 1 || v.Orphans[0].Node.Path != "b.x" {
		t.Fatalf("orphans %+v", v.Orphans)
	}
	if len(v.Routines) != 1 || v.Routines[0].Kind != "review" || v.Routines[0].Repo != "app" ||
		v.Routines[0].Instances[0].Uses != 2 || v.Routines[0].Instances[0].Context != 800 {
		t.Fatalf("routines %+v", v.Routines)
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
	// origin holds the spec; the configured clone is behind it, and the
	// lead's own repo_path is another box's path.
	root := t.TempDir()
	origin, clone := filepath.Join(root, "origin"), filepath.Join(root, "clone")
	git(t, root, "init", "-q", "-b", "main", "origin")
	commit(t, origin, map[string]string{"docs/proposals/0042-map.md": specV1})
	git(t, root, "clone", "-q", origin, clone)
	specV2 := strings.Replace(strings.Replace(specV1, "status: active", "status: done", 1), "first", "second", 1)
	commit(t, origin, map[string]string{"docs/proposals/0042-map.md": specV2})

	cfg := &config.Config{
		Repos:   map[string]string{"demo": clone},
		Spec:    config.SpecConfig{Glob: "docs/proposals/{id}-*.md"},
		Console: config.ConsoleConfig{ThreadURL: "http://here/t/{thread}", Hosts: map[string]string{"other": "http://other/t/{thread}"}},
	}
	for _, n := range []mesh.Node{
		{Path: "a", Role: "lead", Repo: "demo", RepoPath: "/elsewhere/demo", Spec: "0042", Host: "other",
			Runner: mesh.NodeRunner{Kind: "t3", Thread: "th-1"}},
		{Path: "b", Role: "lead", Repo: "nope", Spec: "0042", Host: "box"},
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
	if p.Spec == nil || p.Spec.ID != "0042" || p.Spec.Title != "Map v1" || p.Spec.Status != "done" ||
		!strings.Contains(p.Spec.Text, "second") {
		t.Fatalf("spec %+v", p.Spec)
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

	// Within 60 s the spec comes from the cache; after, from origin again.
	specV3 := strings.Replace(specV2, "# Map v1", "# Map v3", 1)
	commit(t, origin, map[string]string{"docs/proposals/0042-map.md": specV3})
	getJSON(t, srv, "/api/lead/a", 200, &p)
	if p.Spec == nil || p.Spec.Title != "Map v1" {
		t.Fatalf("cached spec %+v", p.Spec)
	}
	now = now.Add(61 * time.Second)
	getJSON(t, srv, "/api/lead/a", 200, &p)
	if p.Spec == nil || p.Spec.Title != "Map v3" {
		t.Fatalf("refreshed spec %+v", p.Spec)
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
	getJSON(t, srv, "/api/lead/a*b", 404, nil)
}

func TestRoutinesEndpointListsLeases(t *testing.T) {
	ctx, c := connect(t)
	repo := filepath.Join(t.TempDir(), "app")
	git(t, filepath.Dir(repo), "init", "-q", "-b", "main", "app")
	commit(t, repo, map[string]string{
		".bmesh/routines/verify.toml": "mode = \"fresh\"\ndeterministic = true\ncommand = \"echo ok\"\n",
		".bmesh/routines/review.toml": "model = \"m\"\nprompt = \"P\"\n",
		".bmesh/routines/fmt.toml":    "mode = \"inline\"\ndeterministic = true\ncommand = \"gofmt -l .\"\n",
	})
	cfg := &config.Config{Repos: map[string]string{"app": repo, "gone": filepath.Join(repo, "missing")}}
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
