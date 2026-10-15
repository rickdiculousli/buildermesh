// Package web serves the map: a page with the live event list, the node list
// as JSON and the mesh's events as server-sent events.
package web

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

//go:embed index.html
var page []byte

const pingEvery = 15 * time.Second

// row is one node as /api/nodes returns it.
type row struct {
	Node   mesh.Node          `json:"node"`
	State  mesh.State         `json:"state"`
	Runner *mesh.RunnerSample `json:"runner"`
}

// frame is the data of one /events frame.
type frame struct {
	Subject string          `json:"subject"`
	At      string          `json:"at"`
	Payload json.RawMessage `json:"payload"`
}

// Handler serves / (the page), /api/nodes and /events, whose replay starts
// hist before the request unless ?since=RFC3339 says otherwise.
func Handler(c *mesh.Client, hist time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("GET /api/nodes", func(w http.ResponseWriter, r *http.Request) { nodes(w, r, c) })
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) { events(w, r, c, hist) })
	return mux
}

func nodes(w http.ResponseWriter, r *http.Request, c *mesh.Client) {
	ctx := r.Context()
	ns, err := mesh.ListNodes(ctx, c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	states, err := mesh.ReadAllStates(ctx, c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rows := make([]row, 0, len(ns))
	for _, n := range ns {
		rw := row{Node: n, State: mesh.State{State: "idle"}}
		if s, ok := states[n.Path]; ok {
			rw.State = s
		}
		s, ok, err := mesh.GetRunner(ctx, c, n.Path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if ok {
			rw.Runner = &s
		}
		rows = append(rows, rw)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

func events(w http.ResponseWriter, r *http.Request, c *mesh.Client, hist time.Duration) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	since := time.Now().Add(-hist)
	if s := r.URL.Query().Get("since"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			http.Error(w, "since: "+err.Error(), http.StatusBadRequest)
			return
		}
		since = t
	}
	ctx := r.Context()
	// Follow before History so nothing published in between is lost; live
	// messages the replay already sent are skipped by sequence.
	live, err := mesh.Follow(ctx, c, "bm.>")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	past, err := mesh.History(ctx, c, "bm.>", since)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fl.Flush()

	var lastSeq uint64
	for _, m := range past {
		if writeFrame(w, "replay", m) != nil {
			return
		}
		fl.Flush()
		lastSeq = m.Seq
	}
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		case m, ok := <-live:
			if !ok {
				return
			}
			if m.Seq <= lastSeq {
				continue
			}
			if writeFrame(w, "live", m) != nil {
				return
			}
		}
		fl.Flush()
	}
}

func writeFrame(w http.ResponseWriter, event string, m mesh.Stored) error {
	payload := json.RawMessage(m.Data)
	if !json.Valid(m.Data) {
		payload, _ = json.Marshal(string(m.Data))
	}
	data, err := json.Marshal(frame{Subject: m.Subject, At: m.At.UTC().Format(time.RFC3339), Payload: payload})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	return err
}
