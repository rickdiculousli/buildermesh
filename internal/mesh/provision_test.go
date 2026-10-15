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
	if err := Provision(ctx, c, 0); err != nil {
		t.Fatal(err)
	}
	return c, ctx
}

func TestProvisionIsIdempotent(t *testing.T) {
	c, ctx := connect(t)
	if err := Provision(ctx, c, 0); err != nil {
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

func TestProvisionAgesResults(t *testing.T) {
	c, ctx := connect(t)
	ttl := func() time.Duration {
		kv, err := c.JS.KeyValue(ctx, BucketResults)
		if err != nil {
			t.Fatal(err)
		}
		st, err := kv.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return st.TTL()
	}
	if got := ttl(); got != 0 {
		t.Fatalf("unaged bucket TTL %s", got)
	}
	// A second Provision (the next serve) applies a changed age.
	if err := Provision(ctx, c, 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if got := ttl(); got != 30*24*time.Hour {
		t.Fatalf("results TTL %s, want 720h", got)
	}
	kv, err := c.JS.KeyValue(ctx, BucketNodes)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := kv.Status(ctx); err != nil || st.TTL() != 0 {
		t.Fatalf("nodes TTL %v %v, want none", st, err)
	}
}

func TestForgetNode(t *testing.T) {
	c, ctx := connect(t)
	p := Path{"app-001"}
	if err := PutNode(ctx, c, Node{Path: "app-001"}); err != nil {
		t.Fatal(err)
	}
	if err := PutRunner(ctx, c, "app-001", RunnerSample{Known: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteState(ctx, c, "app-001", State{State: "working"}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureInbox(ctx, c, p); err != nil {
		t.Fatal(err)
	}
	for range 2 { // the second finds nothing and is no error
		if err := ForgetNode(ctx, c, "app-001"); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok, err := GetNode(ctx, c, "app-001"); err != nil || ok {
		t.Fatalf("node %v %v", ok, err)
	}
	if _, ok, err := GetRunner(ctx, c, "app-001"); err != nil || ok {
		t.Fatalf("runner %v %v", ok, err)
	}
	if s, rev, err := ReadState(ctx, c, "app-001"); err != nil || rev != 0 || s.State != "idle" {
		t.Fatalf("state %+v %d %v", s, rev, err)
	}
	if _, err := c.JS.Consumer(ctx, StreamMesh, InboxConsumer(p)); err == nil {
		t.Fatal("consumer kept")
	}
	if nodes, err := ListNodes(ctx, c); err != nil || len(nodes) != 0 {
		t.Fatalf("nodes %v %v", nodes, err)
	}
}

func TestConnectUnreachable(t *testing.T) {
	_, err := Connect(context.Background(), "nats://127.0.0.1:1", "", "hub", "t", false)
	if err == nil || err.Error()[:len("can't reach NATS at nats://127.0.0.1:1: ")] != "can't reach NATS at nats://127.0.0.1:1: " {
		t.Fatal(err)
	}
}
