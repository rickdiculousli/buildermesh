package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	go func() { res <- serve(ctx, cfg, "127.0.0.1:0", &stderr, ready) }()
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
