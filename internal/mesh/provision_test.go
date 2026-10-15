package mesh_test

import (
	"context"
	. "github.com/rickdiculousli/buildermesh/internal/mesh"
	"testing"
	"time"

	"github.com/rickdiculousli/buildermesh/internal/meshtest"
)

func connect(t *testing.T) (*Client, context.Context) {
	t.Helper()
	url := meshtest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	c, err := Connect(ctx, url, "", "hub", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if err := Provision(ctx, c); err != nil {
		t.Fatal(err)
	}
	return c, ctx
}

func TestProvisionIsIdempotent(t *testing.T) {
	c, ctx := connect(t)
	if err := Provision(ctx, c); err != nil {
		t.Fatal(err)
	}
	s, err := c.JS.Stream(ctx, StreamMesh)
	if err != nil {
		t.Fatal(err)
	}
	cfg := s.CachedInfo().Config
	if len(cfg.Subjects) != 1 || cfg.Subjects[0] != "bm.>" || cfg.MaxAge != 30*24*time.Hour || cfg.Duplicates != 2*time.Minute {
		t.Fatalf("config %+v", cfg)
	}
	for _, b := range []string{BucketNodes, BucketState, BucketRunner} {
		if _, err := c.JS.KeyValue(ctx, b); err != nil {
			t.Fatalf("%s: %v", b, err)
		}
	}
	p, _ := ParsePath("a.b")
	if err := EnsureInbox(ctx, c, p); err != nil {
		t.Fatal(err)
	}
	if err := EnsureInbox(ctx, c, p); err != nil {
		t.Fatal(err)
	}
	if _, err := c.JS.Consumer(ctx, StreamMesh, "inbox-a--b"); err != nil {
		t.Fatal(err)
	}
}

func TestConnectUnreachable(t *testing.T) {
	_, err := Connect(context.Background(), "nats://127.0.0.1:1", "", "hub", "t", false)
	if err == nil || err.Error()[:len("can't reach NATS at nats://127.0.0.1:1: ")] != "can't reach NATS at nats://127.0.0.1:1: " {
		t.Fatal(err)
	}
}
