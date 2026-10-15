package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// serveOnce runs serve with the config body, checks /api/nodes answers [],
// then cancels and waits for serve to return.
func serveOnce(t *testing.T, body, wantMode string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	var stderr bytes.Buffer
	res := make(chan error, 1)
	go func() { res <- serve(ctx, cfg, "127.0.0.1:0", &stderr, ready, "") }()
	var addr string
	select {
	case addr = <-ready:
	case err := <-res:
		t.Fatalf("serve returned early: %v %s", err, stderr.String())
	case <-time.After(15 * time.Second):
		t.Fatal("serve not ready")
	}
	resp, err := http.Get("http://" + addr + "/api/nodes")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || strings.TrimSpace(string(b)) != "[]" {
		t.Fatalf("/api/nodes: %d %q", resp.StatusCode, b)
	}
	cancel()
	select {
	case err := <-res:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop")
	}
	if log := stderr.String(); !strings.Contains(log, "bmesh serve: nats nats://127.0.0.1:") ||
		!strings.Contains(log, " ("+wantMode+"), http "+addr+", host "+testHost+"\n") {
		t.Fatalf("log %q", log)
	}
}

func TestServeEmbeddedThenHub(t *testing.T) {
	store := filepath.Join(t.TempDir(), "js")
	serveOnce(t, "[mesh]\nnats = \"\"\nstore = \""+store+"\"\nlisten = \"127.0.0.1:0\"\n[host]\nname = \""+testHost+"\"\n", "embedded")
	url := meshtest.Start(t)
	serveOnce(t, "[mesh]\nnats = \""+url+"\"\n[host]\nname = \""+testHost+"\"\n[routines]\nkeep_results = \"7d\"\n", "hub")

	// serve provisioned bm-results with [routines] keep_results.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := mesh.Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	kv, err := c.JS.KeyValue(ctx, mesh.BucketResults)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := kv.Status(ctx); err != nil || st.TTL() != 7*24*time.Hour {
		t.Fatalf("bm-results TTL %v %v, want 168h", st, err)
	}
}

func TestServeUpdatesAndAsksForReexec(t *testing.T) {
	exe, shas := updateBox(t)
	withRevision(t, shas[0])
	url := meshtest.Start(t)
	path := os.Getenv("BMESH_CONFIG")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, []byte("[mesh]\nnats = \""+url+"\"\n[host]\nname = \""+testHost+"\"\n[update]\nrepo = \"bm\"\n")...)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var stderr syncWriter
	res := make(chan error, 1)
	go func() { res <- serve(context.Background(), cfg, "127.0.0.1:0", &stderr, nil, exe) }()
	select {
	case err := <-res:
		if !errors.Is(err, errReexec) {
			t.Fatalf("serve: %v\n%s", err, stderr.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("serve did not stop for the update\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "update: "+shas[0][:8]+" -> "+shas[1][:8]) {
		t.Fatalf("log %s", stderr.String())
	}
	if b, _ := os.ReadFile(exe); !strings.Contains(string(b), "fake bmesh "+shas[1]) {
		t.Fatalf("binary %q", b)
	}
}

// syncWriter is a bytes.Buffer safe for serve's goroutines and the test.
type syncWriter struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}
