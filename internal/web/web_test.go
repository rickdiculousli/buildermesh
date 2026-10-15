package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	srv := httptest.NewServer(Handler(c, time.Hour))
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
	srv := httptest.NewServer(Handler(c, time.Hour))
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
