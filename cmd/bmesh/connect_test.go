package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/mesh"
	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

// singleBox points BMESH_CONFIG at a single box's config: nats = "" and
// the embedded hub's address as [mesh] listen.
func singleBox(t *testing.T, listen string) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "config.toml")
	body := "[mesh]\nnats = \"\"\nlisten = \"" + listen + "\"\n[host]\nname = \"" + testHost + "\"\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMESH_CONFIG", cfg)
}

func TestCLIDialsTheEmbeddedHubOnListen(t *testing.T) {
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
	singleBox(t, strings.TrimPrefix(url, "nats://"))
	out, errOut, code := runCLI(t, nil, "mesh-status")
	if code != 0 {
		t.Fatalf("mesh-status: code %d, %s%s", code, out, errOut)
	}
}

func TestCLISaysNoHubWhenServeIsNotRunning(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing listens there now
	singleBox(t, addr)
	_, errOut, code := runCLI(t, nil, "mesh-status")
	want := "no hub at " + addr + ": is bmesh serve running?"
	if code == 0 || !strings.Contains(errOut, want) {
		t.Fatalf("code %d, stderr %q, want %q", code, errOut, want)
	}
	// A port serve picks can't be dialled: say so rather than "no hub".
	singleBox(t, "127.0.0.1:0")
	if _, errOut, code := runCLI(t, nil, "mesh-status"); code == 0 || !strings.Contains(errOut, "[mesh] listen 127.0.0.1:0 lets bmesh serve pick a port") {
		t.Fatalf("port 0: code %d, stderr %q", code, errOut)
	}
}
